/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"context"
	"fmt"
	"log/slog"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	NodepoolFinalizer = "oran-hwmgr-plugin/nodepool-finalizer"
	ResourceTypeIdKey = "resourceTypeId"
)

var nodepoolGVK schema.GroupVersionKind

func InitNodepoolUtils(scheme *runtime.Scheme) error {
	nodepool := &hwmgmtv1alpha1.NodePool{}
	gvks, unversioned, err := scheme.ObjectKinds(nodepool)
	if err != nil {
		return fmt.Errorf("failed to query scheme to get GVK for nodepool CR: %w", err)
	}
	if unversioned || len(gvks) != 1 {
		return fmt.Errorf("expected a single versioned item in ObjectKinds response, got %d with unversioned=%t", len(gvks), unversioned)
	}

	nodepoolGVK = gvks[0]

	return nil
}

func GetNodePool(ctx context.Context, client client.Reader, key client.ObjectKey, nodepool *hwmgmtv1alpha1.NodePool) error {
	if err := client.Get(ctx, key, nodepool); err != nil {
		return fmt.Errorf("failed to get CR: %w", err)
	}

	if nodepool.Kind == "" {
		// The non-caching query doesn't set the GVK for the CR, so do it now
		nodepool.SetGroupVersionKind(nodepoolGVK)
	}

	return nil
}

func GetResourceTypeId(nodepool *hwmgmtv1alpha1.NodePool) string {
	return nodepool.Spec.Extensions[ResourceTypeIdKey]
}

func GetNodePoolProvisionedCondition(nodepool *hwmgmtv1alpha1.NodePool) *metav1.Condition {
	return meta.FindStatusCondition(
		nodepool.Status.Conditions,
		string(hwmgmtv1alpha1.Provisioned))
}

func IsNodePoolProvisionedCompleted(nodepool *hwmgmtv1alpha1.NodePool) bool {
	provisionedCondition := GetNodePoolProvisionedCondition(nodepool)
	if provisionedCondition != nil && provisionedCondition.Status == metav1.ConditionTrue {
		return true
	}

	return false
}

func IsNodePoolProvisionedFailed(nodepool *hwmgmtv1alpha1.NodePool) bool {
	provisionedCondition := GetNodePoolProvisionedCondition(nodepool)
	if provisionedCondition != nil && provisionedCondition.Reason == string(hwmgmtv1alpha1.Failed) {
		return true
	}

	return false
}

func UpdateNodePoolStatusCondition(
	ctx context.Context,
	c client.Client,
	nodepool *hwmgmtv1alpha1.NodePool,
	conditionType hwmgmtv1alpha1.ConditionType,
	conditionReason hwmgmtv1alpha1.ConditionReason,
	conditionStatus metav1.ConditionStatus,
	message string) error {

	SetStatusCondition(&nodepool.Status.Conditions,
		string(conditionType),
		string(conditionReason),
		conditionStatus,
		message)

	// nolint: wrapcheck
	err := RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodepool := &hwmgmtv1alpha1.NodePool{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodepool), newNodepool); err != nil {
			return err
		}
		SetStatusCondition(&newNodepool.Status.Conditions,
			string(conditionType),
			string(conditionReason),
			conditionStatus,
			message)
		if err := c.Status().Update(ctx, newNodepool); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update nodepool condition: %s, %w", nodepool.Name, err)
	}

	return nil
}

func UpdateNodePoolProperties(
	ctx context.Context,
	c client.Client,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	// nolint: wrapcheck
	err := RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodepool := &hwmgmtv1alpha1.NodePool{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodepool), newNodepool); err != nil {
			return err
		}
		newNodepool.Status.Properties = nodepool.Status.Properties
		if err := c.Status().Update(ctx, newNodepool); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update nodepool properties: %w", err)
	}

	return nil
}

func UpdateNodePoolSelectedPools(
	ctx context.Context,
	c client.Client,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	// nolint: wrapcheck
	err := RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodepool := &hwmgmtv1alpha1.NodePool{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodepool), newNodepool); err != nil {
			return err
		}
		newNodepool.Status.SelectedPools = nodepool.Status.SelectedPools
		if err := c.Status().Update(ctx, newNodepool); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update nodepool selectedPools: %w", err)
	}

	return nil
}

func UpdateNodePoolPluginStatus(
	ctx context.Context,
	c client.Client,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	// nolint: wrapcheck
	err := RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodepool := &hwmgmtv1alpha1.NodePool{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodepool), newNodepool); err != nil {
			return err
		}
		newNodepool.Status.HwMgrPlugin.ObservedGeneration = newNodepool.ObjectMeta.Generation
		if err := c.Status().Update(ctx, newNodepool); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to update nodepool condition: %w", err)
	}

	return nil
}

// DeriveNodePoolStatusFromNodes evaluates all child nodes and returns an appropriate
// NodePool Configured condition status and reason.
func DeriveNodePoolStatusFromNodes(
	ctx context.Context,
	reader client.Reader,
	logger *slog.Logger,
	nodelist *hwmgmtv1alpha1.NodeList,
) (metav1.ConditionStatus, string, string) {

	for _, node := range nodelist.Items {
		// Fetch the latest version of the node from the API server
		updatedNode, err := GetNode(ctx, logger, reader, node.Namespace, node.Name)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to fetch updated node", slog.String("name", node.Name), slog.String("error", err.Error()))
			// Fail conservatively if we can't confirm the node's status
			return metav1.ConditionFalse, string(hwmgmtv1alpha1.InProgress),
				fmt.Sprintf("Node %s could not be read: %v", node.Name, err)
		}

		cond := meta.FindStatusCondition(updatedNode.Status.Conditions, string(hwmgmtv1alpha1.Configured))
		if cond == nil {
			return metav1.ConditionFalse, string(hwmgmtv1alpha1.InProgress),
				fmt.Sprintf("Node %s missing Configured condition", node.Name)
		}

		// If not successfully applied, return this node’s current condition
		if cond.Reason != string(hwmgmtv1alpha1.ConfigApplied) {
			return cond.Status, cond.Reason, fmt.Sprintf("Node %s: %s", node.Name, cond.Message)
		}
	}

	// All nodes are successfully configured
	return metav1.ConditionTrue, string(hwmgmtv1alpha1.ConfigApplied), string(hwmgmtv1alpha1.ConfigSuccess)
}

func NodepoolAddFinalizer(
	ctx context.Context,
	c client.Client,
	nodepool *hwmgmtv1alpha1.NodePool,
) error {
	// nolint: wrapcheck
	err := RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodepool := &hwmgmtv1alpha1.NodePool{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodepool), newNodepool); err != nil {
			return err
		}
		controllerutil.AddFinalizer(newNodepool, NodepoolFinalizer)
		if err := c.Update(ctx, newNodepool); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to add finalizer to nodepool: %w", err)
	}
	return nil
}

func NodepoolRemoveFinalizer(
	ctx context.Context,
	c client.Client,
	nodepool *hwmgmtv1alpha1.NodePool,
) error {
	// nolint: wrapcheck
	err := RetryOnConflictOrRetriable(retry.DefaultRetry, func() error {
		newNodepool := &hwmgmtv1alpha1.NodePool{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(nodepool), newNodepool); err != nil {
			return err
		}
		controllerutil.RemoveFinalizer(newNodepool, NodepoolFinalizer)
		if err := c.Update(ctx, newNodepool); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to remove finalizer from nodepool: %w", err)
	}
	return nil
}
