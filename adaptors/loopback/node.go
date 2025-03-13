/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package loopback

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/yaml"
)

// AllocateNode processes a NodePool CR, allocating a free node for each specified nodegroup as needed
func (a *Adaptor) AllocateNode(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) error {
	cloudID := nodepool.Spec.CloudID

	// Inject a delay before allocating node
	time.Sleep(10 * time.Second)

	cm, resources, allocations, err := a.GetCurrentResources(ctx)
	if err != nil {
		return fmt.Errorf("unable to get current resources: %w", err)
	}

	var cloud *cmAllocatedCloud
	for i, iter := range allocations.Clouds {
		if iter.CloudID == cloudID {
			cloud = &allocations.Clouds[i]
			break
		}
	}
	if cloud == nil {
		// The cloud wasn't found in the list, so create a new entry
		allocations.Clouds = append(allocations.Clouds, cmAllocatedCloud{CloudID: cloudID, Nodegroups: make(map[string][]cmAllocatedNode)})
		cloud = &allocations.Clouds[len(allocations.Clouds)-1]
	}

	// Check available resources
	for _, nodegroup := range nodepool.Spec.NodeGroup {
		used := cloud.Nodegroups[nodegroup.NodePoolData.Name]
		remaining := nodegroup.Size - len(used)
		if remaining <= 0 {
			// This group is allocated
			a.Logger.InfoContext(ctx, "nodegroup is fully allocated", slog.String("nodegroup", nodegroup.NodePoolData.Name))
			continue
		}

		freenodes := getFreeNodesInPool(resources, allocations, nodegroup.NodePoolData.ResourcePoolId)
		if remaining > len(freenodes) {
			return fmt.Errorf("not enough free resources remaining in resource pool %s", nodegroup.NodePoolData.ResourcePoolId)
		}

		nodename := utils.GenerateNodeName()

		// Grab the first node
		nodeId := freenodes[0]

		nodeinfo, exists := resources.Nodes[nodeId]
		if !exists {
			return fmt.Errorf("unable to find nodeinfo for %s", nodeId)
		}

		if err := a.CreateBMCSecret(ctx, nodepool, nodename, nodeinfo.BMC.UsernameBase64, nodeinfo.BMC.PasswordBase64); err != nil {
			return fmt.Errorf("failed to create bmc-secret when allocating node %s, nodeId %s: %w", nodename, nodeId, err)
		}

		cloud.Nodegroups[nodegroup.NodePoolData.Name] = append(cloud.Nodegroups[nodegroup.NodePoolData.Name], cmAllocatedNode{NodeName: nodename, NodeId: nodeId})

		// Update the configmap
		yamlString, err := yaml.Marshal(&allocations)
		if err != nil {
			return fmt.Errorf("unable to marshal allocated data: %w", err)
		}
		cm.Data[allocationsKey] = string(yamlString)
		if err := a.Client.Update(ctx, cm); err != nil {
			return fmt.Errorf("failed to update configmap: %w", err)
		}

		if err := a.CreateNode(ctx, nodepool, cloudID, nodename, nodeId, nodegroup.NodePoolData.Name, nodegroup.NodePoolData.HwProfile); err != nil {
			return fmt.Errorf("failed to create allocated node (%s): %w", nodename, err)
		}

		if err := a.UpdateNodeStatus(ctx, nodename, nodeinfo, nodegroup.NodePoolData.HwProfile); err != nil {
			return fmt.Errorf("failed to update node status (%s): %w", nodename, err)
		}
	}

	return nil
}

func bmcSecretName(nodename string) string {
	return fmt.Sprintf("%s-bmc-secret", nodename)
}

// CreateBMCSecret creates the bmc-secret for a node
func (a *Adaptor) CreateBMCSecret(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool, nodename, usernameBase64, passwordBase64 string) error {
	a.Logger.InfoContext(ctx, "Creating bmc-secret:", slog.String("nodename", nodename))

	secretName := bmcSecretName(nodename)

	username, err := base64.StdEncoding.DecodeString(usernameBase64)
	if err != nil {
		return fmt.Errorf("failed to decode usernameBase64 string (%s) for node %s: %w", usernameBase64, nodename, err)
	}

	password, err := base64.StdEncoding.DecodeString(passwordBase64)
	if err != nil {
		return fmt.Errorf("failed to decode usernameBase64 string (%s) for node %s: %w", passwordBase64, nodename, err)
	}

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
			"username": username,
			"password": password,
		},
	}

	if err = utils.CreateOrUpdateK8sCR(ctx, a.Client, bmcSecret, nil, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create bmc-secret for node %s: %w", nodename, err)
	}

	return nil
}

// CreateNode creates a Node CR with specified attributes
func (a *Adaptor) CreateNode(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool, cloudID, nodename, nodeId, groupname, hwprofile string) error {
	a.Logger.InfoContext(ctx, "Creating node",
		slog.String("nodegroup name", groupname),
		slog.String("nodename", nodename),
		slog.String("nodeId", nodeId))

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
			NodePool:    cloudID,
			GroupName:   groupname,
			HwProfile:   hwprofile,
			HwMgrId:     nodepool.Spec.HwMgrId,
			HwMgrNodeId: nodeId,
		},
	}

	if err := a.Client.Create(ctx, node); err != nil {
		return fmt.Errorf("failed to create Node: %w", err)
	}

	return nil
}

// UpdateNodeStatus updates a Node CR status field with additional node information from the nodelist configmap
func (a *Adaptor) UpdateNodeStatus(ctx context.Context, nodename string, info cmNodeInfo, hwprofile string) error {
	a.Logger.InfoContext(ctx, "Updating node", slog.String("nodename", nodename))

	node := &hwmgmtv1alpha1.Node{}

	if err := utils.RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return a.Get(ctx, types.NamespacedName{Name: nodename, Namespace: a.Namespace}, node)
	}); err != nil {
		return fmt.Errorf("failed to get Node for update: %w", err)
	}

	a.Logger.InfoContext(ctx, "Adding info to node",
		slog.String("nodename", nodename),
		slog.Any("info", info))
	node.Status.BMC = &hwmgmtv1alpha1.BMC{
		Address:         info.BMC.Address,
		CredentialsName: bmcSecretName(nodename),
	}
	node.Status.Interfaces = info.Interfaces

	utils.SetStatusCondition(&node.Status.Conditions,
		string(hwmgmtv1alpha1.Provisioned),
		string(hwmgmtv1alpha1.Completed),
		metav1.ConditionTrue,
		"Provisioned")
	node.Status.HwProfile = hwprofile
	if err := utils.UpdateK8sCRStatus(ctx, a.Client, node); err != nil {
		return fmt.Errorf("failed to update status for node %s: %w", nodename, err)
	}

	return nil
}
