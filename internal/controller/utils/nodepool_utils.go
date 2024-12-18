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

package utils

import (
	"context"
	"fmt"

	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	NodepoolFinalizer = "oran-hwmgr-plugin/nodepool-finalizer"
	ResourceTypeIdKey = "resourceTypeId"
)

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
		return fmt.Errorf("failed to update nodepool condition: %w", err)
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
		return fmt.Errorf("failed to remove finalizer from nodepool: %w", err)
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
