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

package metal3

import (
	"context"
	"fmt"
	"log/slog"

	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	typederrors "github.com/openshift-kni/oran-hwmgr-plugin/internal/typed-errors"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// CheckNodePoolProgress checks to see if a NodePool is fully allocated, allocating additional resources as needed
func (a *Adaptor) CheckNodePoolProgress(
	ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) (full bool, err error) {

	if full, err = a.IsNodePoolFullyAllocated(ctx, hwmgr, nodepool); err != nil {
		err = fmt.Errorf("failed to check nodepool allocation: %w", err)
		return false, err
	}
	if !full {
		return false, a.ProcessNodePoolAllocation(ctx, nodepool)
	}
	// Node is fully allocated
	// check if there are any pending work such as bios configuring
	if updating, err := a.checkForPendingUpdate(ctx, nodepool); err != nil {
		return false, err
	} else if updating {
		return false, nil
	}
	return true, nil
}

func (a *Adaptor) HandleNodePoolCreate(
	ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {

	conditionType := hwmgmtv1alpha1.Provisioned
	var conditionReason hwmgmtv1alpha1.ConditionReason
	var conditionStatus metav1.ConditionStatus
	var message string

	if err := a.ProcessNewNodePool(ctx, hwmgr, nodepool); err != nil {
		a.Logger.ErrorContext(ctx, "failed createNodePool", slog.String("error", err.Error()))
		conditionReason = hwmgmtv1alpha1.Failed
		conditionStatus = metav1.ConditionFalse
		message = "Creation request failed: " + err.Error()
	} else {
		conditionReason = hwmgmtv1alpha1.InProgress
		conditionStatus = metav1.ConditionFalse
		message = "Handling creation"
	}

	if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
		conditionType, conditionReason, conditionStatus, message); err != nil {
		return utils.RequeueWithMediumInterval(),
			fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
	}
	// Update the Node Pool hwMgrPlugin status
	if err := utils.UpdateNodePoolPluginStatus(ctx, a.Client, nodepool); err != nil {
		return utils.RequeueWithShortInterval(), fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", err)
	}

	return utils.DoNotRequeue(), nil
}

func (a *Adaptor) HandleNodePoolProcessing(
	ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {

	var result ctrl.Result
	full, err := a.CheckNodePoolProgress(ctx, hwmgr, nodepool)
	if err != nil {
		if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool, hwmgmtv1alpha1.Provisioned,
			hwmgmtv1alpha1.Failed, metav1.ConditionFalse, err.Error()); err != nil {
			return utils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
		}
		if !typederrors.IsInputError(err) {
			return utils.DoNotRequeue(), fmt.Errorf("failed CheckNodePoolProgress: %w", err)
		}
		return utils.RequeueWithMediumInterval(), nil
	}

	if full {
		a.Logger.InfoContext(ctx, "NodePool request is fully allocated")

		if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Completed, metav1.ConditionTrue, "Created"); err != nil {
			return utils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
		}
		result = utils.DoNotRequeue()
	} else {
		a.Logger.InfoContext(ctx, "NodePool request in progress")
		result = utils.RequeueWithShortInterval()
	}

	return result, nil
}

// ProcessNewNodePool processes a new NodePool CR, verifying that there are enough free resources to satisfy the request
func (a *Adaptor) ProcessNewNodePool(ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	cloudID := nodepool.Spec.CloudID
	a.Logger.InfoContext(ctx, "Processing ProcessNewNodePool request:",
		slog.String("cloudID", cloudID),
	)

	// Fetch the list of BMHs for the NodePool's site
	bmhList, err := a.FetchBMHList(ctx, nodepool.Spec.Site)
	if err != nil {
		return fmt.Errorf("unable to fetch BMHs for site %s: %w", nodepool.Spec.Site, err)
	}

	// Filter the unallocated BMHs
	unallocatedBMHs, err := a.getUnallocatedBMHs(ctx, bmhList)
	if err != nil {
		return fmt.Errorf("unable to fetch unallocated BMHs for site %s: %w", nodepool.Spec.Site, err)
	}

	// Group unallocated BMHs by resourcePoolId
	groupedBMHs := a.GroupBMHsByResourcePool(unallocatedBMHs)

	// Check if enough resources are available
	for _, nodeGroup := range nodepool.Spec.NodeGroup {
		if nodeGroup.Size == 0 {
			continue // Skip groups with size 0
		}

		resourcePoolId := nodeGroup.NodePoolData.ResourcePoolId
		bmhListForGroup := groupedBMHs[resourcePoolId]

		if len(bmhListForGroup) < nodeGroup.Size {
			return fmt.Errorf("not enough free resources in resource pool %s: freenodes=%d", resourcePoolId, len(bmhListForGroup))
		}
	}

	return nil
}

// IsNodePoolFullyAllocated checks to see if a NodePool CR has been fully allocated
func (a *Adaptor) IsNodePoolFullyAllocated(ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) (bool, error) {

	for _, nodeGroup := range nodepool.Spec.NodeGroup {
		allocatedNodes := a.countNodesInGroup(ctx, nodepool.Status.Properties.NodeNames, nodeGroup.NodePoolData.Name)
		if allocatedNodes < nodeGroup.Size {
			return false, nil // At least one group is not fully allocated
		}
	}
	return true, nil
}

// ReleaseNodePool frees resources allocated to a NodePool
func (a *Adaptor) ReleaseNodePool(ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	cloudID := nodepool.Spec.CloudID

	a.Logger.InfoContext(ctx, "Processing ReleaseNodePool request:",
		slog.String("cloudID", cloudID),
	)
	// TODO: what needs to be done here
	return nil
}
