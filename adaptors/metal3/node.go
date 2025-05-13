/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metal3

import (
	"context"
	"fmt"
	"log/slog"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

// GetBMHToNodeMap get a list of nodes, mapped to BMH namespace/name
func (a *Adaptor) GetBMHToNodeMap(ctx context.Context) (map[string]hwmgmtv1alpha1.Node, error) {
	nodes := make(map[string]hwmgmtv1alpha1.Node)

	nodelist, err := utils.GetNodeList(ctx, a.Client)
	if err != nil {
		a.Logger.InfoContext(ctx, "Unable to query node list", slog.String("error", err.Error()))
		return nodes, fmt.Errorf("failed to query node list: %w", err)
	}

	for _, node := range nodelist.Items {
		bmhName := node.Spec.HwMgrNodeId
		bmhNamespace := node.Spec.HwMgrNodeNs

		if bmhName != "" && bmhNamespace != "" {
			nodes[bmhNamespace+"/"+bmhName] = node
		}
	}

	return nodes, nil
}

func (a *Adaptor) GetNodeForBMH(nodes map[string]hwmgmtv1alpha1.Node, bmh *metal3v1alpha1.BareMetalHost) *hwmgmtv1alpha1.Node {
	bmhName := bmh.Name
	bmhNamespace := bmh.Namespace

	if node, exists := nodes[bmhNamespace+"/"+bmhName]; exists {
		return &node
	}
	return nil
}

// CreateNode creates a Node CR with specified attributes
func (a *Adaptor) CreateNode(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool, cloudID, nodename, nodeId, nodeNs, groupname, hwprofile string) error {
	a.Logger.InfoContext(ctx, "Ensuring node exists",
		slog.String("nodegroup name", groupname),
		slog.String("nodename", nodename),
		slog.String("nodeId", nodeId))

	nodeKey := types.NamespacedName{
		Name:      nodename,
		Namespace: a.Namespace,
	}

	existing := &hwmgmtv1alpha1.Node{}
	err := a.Client.Get(ctx, nodeKey, existing)
	if err == nil {
		a.Logger.InfoContext(ctx, "Node already exists, skipping create", slog.String("nodename", nodename))
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check if node exists: %w", err)
	}

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
			HwMgrNodeNs: nodeNs,
			HwMgrNodeId: nodeId,
		},
	}

	if err := a.Client.Create(ctx, node); err != nil {
		return fmt.Errorf("failed to create Node: %w", err)
	}

	a.Logger.InfoContext(ctx, "Node created", slog.String("nodename", nodename))
	return nil
}

// UpdateNodeStatus updates a Node CR status field with additional node information
func (a *Adaptor) UpdateNodeStatus(ctx context.Context, info bmhNodeInfo, nodename, hwprofile string, updating bool) error {
	a.Logger.InfoContext(ctx, "Updating node", slog.String("nodename", nodename))
	// nolint:wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		node := &hwmgmtv1alpha1.Node{}

		if err := a.Get(ctx, types.NamespacedName{Name: nodename, Namespace: a.Namespace}, node); err != nil {
			return fmt.Errorf("failed to fetch Node: %w", err)
		}

		a.Logger.InfoContext(ctx, "Retrying update for Node", slog.String("nodename", nodename))

		a.Logger.InfoContext(ctx, "Adding info to node",
			slog.String("nodename", nodename),
			slog.Any("info", info))

		node.Status.BMC = &hwmgmtv1alpha1.BMC{
			Address:         info.BMC.Address,
			CredentialsName: info.BMC.CredentialsName,
		}
		node.Status.Interfaces = info.Interfaces

		reason := hwmgmtv1alpha1.Completed
		message := "Provisioned"
		status := metav1.ConditionTrue
		if updating {
			reason = hwmgmtv1alpha1.InProgress
			message = "Hardware configuration in progess"
			status = metav1.ConditionFalse
		}
		utils.SetStatusCondition(&node.Status.Conditions,
			string(hwmgmtv1alpha1.Provisioned),
			string(reason),
			status,
			message)

		node.Status.HwProfile = hwprofile

		return a.Client.Status().Update(ctx, node)

	})
}

func (a *Adaptor) ApplyPostConfigUpdates(ctx context.Context, bmhName types.NamespacedName, node *hwmgmtv1alpha1.Node) error {

	if err := a.clearBMHNetworkData(ctx, bmhName); err != nil {
		return fmt.Errorf("failed to clearBMHNetworkData bmh (%+v): %w", bmhName, err)
	}
	// nolint:wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		updatedNode := &hwmgmtv1alpha1.Node{}

		if err := a.Get(ctx, types.NamespacedName{Name: node.Name, Namespace: node.Namespace}, updatedNode); err != nil {
			return fmt.Errorf("failed to fetch Node: %w", err)
		}

		utils.RemoveConfigAnnotation(updatedNode)
		if err := a.Client.Update(ctx, updatedNode); err != nil {
			return fmt.Errorf("failed to remove annotation for node %s/%s: %w", updatedNode.Name, updatedNode.Namespace, err)
		}

		utils.SetStatusCondition(&updatedNode.Status.Conditions,
			string(hwmgmtv1alpha1.Provisioned),
			string(hwmgmtv1alpha1.Completed),
			metav1.ConditionTrue,
			"Provisioned")
		if err := a.Client.Status().Update(ctx, updatedNode); err != nil {
			return fmt.Errorf("failed to update node status: %w", err)
		}

		return nil
	})
}

func (a *Adaptor) SetNodeFailedStatus(
	ctx context.Context,
	node *hwmgmtv1alpha1.Node,
	conditionType string,
	message string,
) error {

	utils.SetStatusCondition(&node.Status.Conditions, conditionType, string(hwmgmtv1alpha1.Failed), metav1.ConditionFalse, message)

	if err := a.Client.Status().Update(ctx, node); err != nil {
		a.Logger.ErrorContext(ctx, "Failed to update node status with failure",
			slog.String("node", node.Name),
			slog.String("conditionType", conditionType),
			slog.String("error", err.Error()))
		return fmt.Errorf("failed to set node failed status: %w", err)
	}

	a.Logger.InfoContext(ctx, "Node status set to failed",
		slog.String("node", node.Name),
		slog.String("conditionType", conditionType),
		slog.String("reason", string(hwmgmtv1alpha1.Failed)))
	return nil
}
