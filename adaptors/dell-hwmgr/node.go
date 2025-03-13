/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package dellhwmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	hwmgrapi "github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/generated"
	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/hwmgrclient"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/logging"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

const (
	ExtensionsNics = "O2-nics"
	ExtensionsNads = "nads"

	ExtensionsRemoteManagement = "RemoteManagement"
	ExtensionsVirtualMediaUrl  = "virtualMediaUrl"

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

type BMCCredentials struct {
	Username string `json:"bmc_username"`
	Password string `json:"bmc_password"`
}

func bmcSecretName(nodename string) string {
	return fmt.Sprintf("%s-bmc-secret", nodename)
}

// AllocateNode processes a NodePool CR, allocating a free node for each specified nodegroup as needed
func (a *Adaptor) AllocateNode(
	ctx context.Context,
	hwmgrClient *hwmgrclient.HardwareManagerClient,
	nodepool *hwmgmtv1alpha1.NodePool,
	resource hwmgrapi.RhprotoResource,
	nodegroupName string) (string, error) {
	nodename := utils.GenerateNodeName()
	ctx = logging.AppendCtx(ctx, slog.String("nodename", nodename))

	if err := a.ValidateNodeConfig(ctx, resource); err != nil {
		return "", fmt.Errorf("failed to validate resource configuration: %w", err)
	}

	if err := a.CreateBMCSecret(ctx, hwmgrClient, nodepool, nodename, resource); err != nil {
		return "", fmt.Errorf("failed to create bmc-secret when allocating node %s: %w", nodename, err)
	}

	if err := a.CreateNode(ctx, nodepool, nodename, resource, nodegroupName); err != nil {
		return "", fmt.Errorf("failed to create allocated node (%s): %w", *resource.Id, err)
	}

	if err := a.SetInitialNodeStatus(ctx, nodename, resource); err != nil {
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
		return nil, fmt.Errorf("resource structure missing required extensions field: %s", ExtensionsNics)
	}

	nads, exists := nics[ExtensionsNads]
	if !exists {
		return nil, fmt.Errorf("resource structure missing required extensions field: %s.%s", ExtensionsNics, ExtensionsNads)
	}

	data, err := json.Marshal(nads)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resource data from extensions field: %s.%s: %w", ExtensionsNics, ExtensionsNads, err)
	}

	var interfaces []ExtensionInterface
	if err := json.Unmarshal(data, &interfaces); err != nil {
		return nil, fmt.Errorf("resource structure contains invalid nic data format: %s.%s", ExtensionsNics, ExtensionsNads)
	}

	return interfaces, nil
}

// parseExtensionVirtualMediaUrl parses the Extensions object in the resource to get the virtualMediaUrl
func (a *Adaptor) parseExtensionVirtualMediaUrl(resource hwmgrapi.RhprotoResource) (string, error) {
	if resource.Extensions == nil {
		return "", fmt.Errorf("resource structure missing required extensions field")
	}

	remoteManagement, exists := (*resource.Extensions)[ExtensionsRemoteManagement]
	if !exists {
		return "", fmt.Errorf("resource structure missing required extensions field: %s", ExtensionsRemoteManagement)
	}

	virtualMediaUrlIntf, exists := remoteManagement[ExtensionsVirtualMediaUrl]
	if !exists {
		return "", fmt.Errorf("resource structure missing required extensions field: %s.%s", ExtensionsRemoteManagement, ExtensionsVirtualMediaUrl)
	}

	virtualMediaUrl, ok := virtualMediaUrlIntf.(string)
	if !ok {
		return "", fmt.Errorf("resource structure has invalid field, expected string: %s.%s", ExtensionsRemoteManagement, ExtensionsVirtualMediaUrl)
	}

	return virtualMediaUrl, nil
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
func (a *Adaptor) ValidateNodeConfig(ctx context.Context, resource hwmgrapi.RhprotoResource) error {
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

	if _, err := a.parseExtensionVirtualMediaUrl(resource); err != nil {
		return fmt.Errorf("unable to parse %s from resource", ExtensionsVirtualMediaUrl)
	}

	return nil
}

// CreateBMCSecret creates the bmc-secret for a node
func (a *Adaptor) CreateBMCSecret(
	ctx context.Context,
	hwmgrClient *hwmgrclient.HardwareManagerClient,
	nodepool *hwmgmtv1alpha1.NodePool,
	nodename string,
	resource hwmgrapi.RhprotoResource) error {
	a.Logger.InfoContext(ctx, "Creating bmc-secret")

	remoteSecretKey := *resource.ResourceAttribute.Compute.Lom.Password
	remoteSecret, err := hwmgrClient.GetSecret(ctx, remoteSecretKey)
	if err != nil {
		return fmt.Errorf("failed to retrieve BMC credentials (%s): %w", remoteSecretKey, err)
	}

	creds := BMCCredentials{}
	if err := json.Unmarshal([]byte(*remoteSecret.Secret.Value), &creds); err != nil {
		return fmt.Errorf("unable to parse BMC credentials (%s)", remoteSecretKey)
	}

	secretName := bmcSecretName(nodename)

	blockDeletion := true
	bmcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: a.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         nodepool.APIVersion,
				Kind:               nodepool.Kind,
				Name:               nodepool.Name,
				UID:                nodepool.UID,
				BlockOwnerDeletion: &blockDeletion,
			}},
		},
		Data: map[string][]byte{
			"username": []byte(creds.Username),
			"password": []byte(creds.Password),
		},
	}

	if err = utils.CreateOrUpdateK8sCR(ctx, a.Client, bmcSecret, nil, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create bmc-secret for node %s: %w", nodename, err)
	}

	return nil
}

// CreateNode creates a Node CR with specified attributes
func (a *Adaptor) CreateNode(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool, nodename string, resource hwmgrapi.RhprotoResource, nodegroupName string) error {
	// TODO: remove this casuistic when the hwprofile returned by the Dell hwmgr is not empty (not supported yet)
	//
	var hwprofile string
	isHwProfileEmpty := resource.ResourceProfileID == nil || *resource.ResourceProfileID == ""
	if isHwProfileEmpty {
		found := false
		for _, ng := range nodepool.Spec.NodeGroup {
			if ng.NodePoolData.Name == nodegroupName {
				hwprofile = ng.NodePoolData.HwProfile
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("failed to assign hwprofile from the nodepool cr for nodegroup name:%s",
				nodegroupName)
		}
	} else {
		hwprofile = *resource.ResourceProfileID
	}

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
			NodePool:    nodepool.Name,
			GroupName:   nodegroupName,
			HwProfile:   hwprofile,
			HwMgrId:     nodepool.Spec.HwMgrId,
			HwMgrNodeId: *resource.Id,
		},
	}

	if err := a.Client.Create(ctx, node); err != nil {
		return fmt.Errorf("failed to create Node: %w", err)
	}

	return nil
}

// SetInitialNodeStatus updates a Node CR status field with additional node information from the RhprotoResource
func (a *Adaptor) SetInitialNodeStatus(ctx context.Context, nodename string, resource hwmgrapi.RhprotoResource) error {
	a.Logger.InfoContext(ctx, "Updating node")

	node := &hwmgmtv1alpha1.Node{}

	if err := utils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return a.Get(ctx, types.NamespacedName{Name: nodename, Namespace: a.Namespace}, node)
	}); err != nil {
		return fmt.Errorf("failed to get Node for update: %w", err)
	}

	virtualMediaUrl, err := a.parseExtensionVirtualMediaUrl(resource)
	if err != nil {
		return fmt.Errorf("unable to parse %s from resource", ExtensionsVirtualMediaUrl)
	}

	node.Status.BMC = &hwmgmtv1alpha1.BMC{
		Address:         virtualMediaUrl,
		CredentialsName: bmcSecretName(nodename),
	}

	var parseErr error
	if node.Status.Interfaces, parseErr = a.getNodeInterfaces(resource); parseErr != nil {
		return fmt.Errorf("invalid interface list: %w", parseErr)
	}

	utils.SetStatusCondition(&node.Status.Conditions,
		string(hwmgmtv1alpha1.Provisioned),
		string(hwmgmtv1alpha1.Completed),
		metav1.ConditionTrue,
		"Provisioned")

	node.Status.HwProfile = node.Spec.HwProfile

	if err := utils.UpdateK8sCRStatus(ctx, a.Client, node); err != nil {
		return fmt.Errorf("failed to update status for node %s: %w", nodename, err)
	}

	return nil
}
