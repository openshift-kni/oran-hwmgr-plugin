/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metal3

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

// GetNodeList retrieves the node list
func (a *Adaptor) GetNodeList(ctx context.Context) (*hwmgmtv1alpha1.NodeList, error) {

	nodeList := &hwmgmtv1alpha1.NodeList{}
	if err := a.Client.List(ctx, nodeList); err != nil {
		return nodeList, fmt.Errorf("failed to list nodes: %w", err)
	}

	return nodeList, nil
}

// CreateNode creates a Node CR with specified attributes
func (a *Adaptor) CreateNode(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool, cloudID, nodename, nodeId, nodeNs, groupname, hwprofile string) error {
	a.Logger.InfoContext(ctx, "Creating node",
		slog.String("nodegroup name", groupname),
		slog.String("nodename", nodename),
		slog.String("nodeId", nodeId))

	blockDeletion := true
	node := &hwmgmtv1alpha1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodename,
			Namespace: a.Namespace,
			Labels:    map[string]string{BmhNamespaceLabel: nodeNs},
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
