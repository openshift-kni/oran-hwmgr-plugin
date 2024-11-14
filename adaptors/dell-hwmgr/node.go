/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dellhwmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	hwmgrapi "github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/generated"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/logging"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

const (
	IdracUrlPrefix = "idrac-virtualmedia+"
	IdracUrlSuffix = "/redfish/v1/Systems/System.Embedded.1" // TODO: Hardware manager should return the full URL
	ExtensionsNics = "O2-nics"
	ExtensionsNads = "nads"

	LabelNameKey  = "name"
	LabelLabelKey = "label"
)

type ExtensionsLabel struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

type ExtensionPort struct {
	MACAddress string            `json:"mac,omitempty"`
	MBPS       int               `json:"mbps,omitempty"`
	Labels     []ExtensionsLabel `json:"Labels,omitempty"`
}

type ExtensionInterface struct {
	Model string          `json:"model,omitempty"`
	Name  string          `json:"name,omitempty"`
	Ports []ExtensionPort `json:"ports,omitempty"`
}

// AllocateNode processes a NodePool CR, allocating a free node for each specified nodegroup as needed
func (a *Adaptor) AllocateNode(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool, resource hwmgrapi.RhprotoResource) (string, error) {
	nodename := *resource.Id
	ctx = logging.AppendCtx(ctx, slog.String("nodenameCtx", nodename))

	// TODO: Need to be able to create a BMC secret, but we don't have access?
	// if err := a.CreateBMCSecret(ctx, nodename, nodeinfo.BMC.UsernameBase64, nodeinfo.BMC.PasswordBase64); err != nil {
	// 	return fmt.Errorf("failed to create bmc-secret when allocating node %s: %w", nodename, err)
	// }
	if err := a.ValidateNodeConfig(resource); err != nil {
		return "", fmt.Errorf("failed to validate resource configuration: %w", err)
	}

	if err := a.CreateNode(ctx, nodepool, resource); err != nil {
		return "", fmt.Errorf("failed to create allocated node (%s): %w", *resource.Id, err)
	}

	if err := a.UpdateNodeStatus(ctx, resource); err != nil {
		return nodename, fmt.Errorf("failed to update node status (%s): %w", *resource.Id, err)
	}

	return nodename, nil
}

// parseExtensionInterfaces parses interface data from the Extensions object in the resource
func (a *Adaptor) parseExtensionInterfaces(resource hwmgrapi.RhprotoResource) ([]ExtensionInterface, error) {
	if resource.Extensions == nil {
		return nil, fmt.Errorf("resource structure missing required extensions field")
	}

	nics, exists := (*resource.Extensions)[ExtensionsNics]
	if !exists {
		return nil, fmt.Errorf("resource structure missing required extensions nics field")
	}

	nads, exists := nics[ExtensionsNads]
	if !exists {
		return nil, fmt.Errorf("resource structure missing required extensions nads field")
	}

	data, err := json.Marshal(nads)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resource data: %w", err)
	}

	var interfaces []ExtensionInterface
	if err := json.Unmarshal(data, &interfaces); err != nil {
		return nil, fmt.Errorf("resource structure contains invalid nic data format")
	}

	return interfaces, nil
}

// getNodeInterfaces translates the interface data from the resource object into the o2ims-defined data structure for the Node CR
func (a *Adaptor) getNodeInterfaces(resource hwmgrapi.RhprotoResource) ([]*hwmgmtv1alpha1.Interface, error) {
	extensionInterfaces, err := a.parseExtensionInterfaces(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to parse interface data: %w", err)
	}

	interfaces := []*hwmgmtv1alpha1.Interface{}
	for _, extIntf := range extensionInterfaces {
		for _, port := range extIntf.Ports {
			intf := hwmgmtv1alpha1.Interface{
				MACAddress: port.MACAddress,
			}
			for _, label := range port.Labels {
				switch label.Key {
				case LabelNameKey:
					intf.Name = label.Value
				case LabelLabelKey:
					intf.Label = label.Value
				}
			}
			if intf.Name == "" {
				// Unnamed ports are ignored
				continue
			}
			interfaces = append(interfaces, &intf)
		}
	}

	return interfaces, nil
}

// ValidateNodeConfig performs basic data structure validation on the resource
func (a *Adaptor) ValidateNodeConfig(resource hwmgrapi.RhprotoResource) error {
	// Check required fields
	if resource.ResourceAttribute == nil ||
		resource.ResourceAttribute.Compute == nil ||
		resource.ResourceAttribute.Compute.Lom == nil ||
		resource.ResourceAttribute.Compute.Lom.IpAddress == nil ||
		resource.ResourceAttribute.Compute.Lom.Password == nil {
		return fmt.Errorf("resource structure missing required resource attribute field")
	}

	if _, err := a.parseExtensionInterfaces(resource); err != nil {
		return fmt.Errorf("invalid interface list: %w", err)
	}

	return nil
}

// CreateNode creates a Node CR with specified attributes
func (a *Adaptor) CreateNode(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool, resource hwmgrapi.RhprotoResource) error {
	nodename := *resource.Id
	groupname := *resource.ResourcePoolId
	hwprofile := *resource.ResourceProfileID

	a.Logger.InfoContext(ctx, "Creating node")

	blockDeletion := true
	node := &hwmgmtv1alpha1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodename,
			Namespace: a.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         nodepool.APIVersion,
				Kind:               nodepool.Kind,
				Name:               nodepool.Name,
				UID:                nodepool.UID,
				BlockOwnerDeletion: &blockDeletion,
			}},
		},
		Spec: hwmgmtv1alpha1.NodeSpec{
			NodePool:  nodepool.Name,
			GroupName: groupname,
			HwProfile: hwprofile,
		},
	}

	if err := a.Client.Create(ctx, node); err != nil {
		return fmt.Errorf("failed to create Node: %w", err)
	}

	return nil
}

// UpdateNodeStatus updates a Node CR status field with additional node information from the nodelist configmap
func (a *Adaptor) UpdateNodeStatus(ctx context.Context, resource hwmgrapi.RhprotoResource) error {
	nodename := *resource.Id

	a.Logger.InfoContext(ctx, "Updating node")

	node := &hwmgmtv1alpha1.Node{}

	if err := utils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return a.Get(ctx, types.NamespacedName{Name: nodename, Namespace: a.Namespace}, node)
	}); err != nil {
		return fmt.Errorf("failed to get Node for update: %w", err)
	}

	node.Status.BMC = &hwmgmtv1alpha1.BMC{
		Address:         IdracUrlPrefix + *resource.ResourceAttribute.Compute.Lom.IpAddress + IdracUrlSuffix,
		CredentialsName: *resource.ResourceAttribute.Compute.Lom.Password,
	}
	node.Status.Hostname = *resource.Name // TODO: Define how the hostname is set

	var parseErr error
	if node.Status.Interfaces, parseErr = a.getNodeInterfaces(resource); parseErr != nil {
		return fmt.Errorf("invalid interface list: %w", parseErr)
	}

	utils.SetStatusCondition(&node.Status.Conditions,
		string(hwmgmtv1alpha1.Provisioned),
		string(hwmgmtv1alpha1.Completed),
		metav1.ConditionTrue,
		"Provisioned")

	if err := utils.UpdateK8sCRStatus(ctx, a.Client, node); err != nil {
		return fmt.Errorf("failed to update status for node %s: %w", nodename, err)
	}

	return nil
}