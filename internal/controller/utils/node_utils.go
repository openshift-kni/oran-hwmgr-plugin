/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	HwMgrNodeId         = "hwmgrNodeId"
	NodeSpecNodePoolKey = "spec.nodePool"
)

// GetNode get a node resource for a provided name
func GetNode(
	ctx context.Context,
	logger *slog.Logger,
	c client.Client,
	namespace, nodename string) (*hwmgmtv1alpha1.Node, error) {

	logger.InfoContext(ctx, "Getting Node", slog.String("nodename", nodename))

	node := &hwmgmtv1alpha1.Node{}

	if err := RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return c.Get(ctx, types.NamespacedName{Name: nodename, Namespace: namespace}, node)
	}); err != nil {
		return node, fmt.Errorf("failed to get Node for update: %w", err)
	}
	return node, nil
}

// GenerateNodeName
func GenerateNodeName() string {
	return uuid.NewString()
}

func FindNodeInList(nodelist hwmgmtv1alpha1.NodeList, hwMgrId, nodeId string) string {
	for _, node := range nodelist.Items {
		if node.Spec.HwMgrId == hwMgrId && node.Spec.HwMgrNodeId == nodeId {
			return node.Name
		}
	}
	return ""
}

// GetChildNodes gets a list of nodes allocated to a NodePool
func GetChildNodes(
	ctx context.Context,
	logger *slog.Logger,
	c client.Client,
	nodepool *hwmgmtv1alpha1.NodePool) (*hwmgmtv1alpha1.NodeList, error) {

	nodelist := &hwmgmtv1alpha1.NodeList{}

	opts := []client.ListOption{
		client.MatchingFields{"spec.nodePool": nodepool.Name},
	}

	if err := RetryOnConflictOrRetriableOrNotFound(retry.DefaultRetry, func() error {
		return c.List(ctx, nodelist, opts...)
	}); err != nil {
		logger.InfoContext(ctx, "Unable to query node list", slog.String("error", err.Error()))
		return nil, fmt.Errorf("failed to query node list: %w", err)
	}

	return nodelist, nil
}

// FindNodeUpdateInProgress scans the nodelist to find the first node with jobId annotation
func FindNodeUpdateInProgress(nodelist *hwmgmtv1alpha1.NodeList) *hwmgmtv1alpha1.Node {
	for _, node := range nodelist.Items {
		if GetJobId(&node) != "" {
			return &node
		}
	}

	return nil
}

// FindNextNodeToUpdate scans the nodelist to find the first node with stale HwProfile
func FindNextNodeToUpdate(nodelist *hwmgmtv1alpha1.NodeList, groupname, newHwProfile string) *hwmgmtv1alpha1.Node {
	for _, node := range nodelist.Items {
		if groupname == node.Spec.GroupName && newHwProfile != node.Spec.HwProfile {
			return &node
		}
	}

	return nil
}

// FindNodeInProgress scans the nodelist to find the first node in InProgress
func FindNodeInProgress(nodelist *hwmgmtv1alpha1.NodeList) *hwmgmtv1alpha1.Node {
	for _, node := range nodelist.Items {
		condition := meta.FindStatusCondition(node.Status.Conditions, (string(hwmgmtv1alpha1.Provisioned)))
		if condition != nil {
			if condition.Status == metav1.ConditionFalse && condition.Reason == string(hwmgmtv1alpha1.InProgress) {
				return &node
			}
		}
	}

	return nil
}
