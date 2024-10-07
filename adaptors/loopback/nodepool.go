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

package loopback

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"
)

// CheckNodePoolProgress checks to see if a NodePool is fully allocated, allocating additional resources as needed
func (a *LoopbackAdaptor) CheckNodePoolProgress(
	ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) (full bool, err error) {

	cloudID := nodepool.Spec.CloudID

	if full, err = a.IsNodePoolFullyAllocated(ctx, hwmgr, nodepool); err != nil {
		err = fmt.Errorf("failed to check nodepool allocation: %w", err)
		return
	} else if full {
		// Node is fully allocated
		return
	}

	for _, nodegroup := range nodepool.Spec.NodeGroup {
		a.Logger.InfoContext(ctx, "Allocating node for CheckNodePoolProgress request:",
			"cloudID", cloudID,
			"nodegroup name", nodegroup.Name,
		)

		if err = a.AllocateNode(ctx, nodepool); err != nil {
			err = fmt.Errorf("failed to allocate node: %w", err)
			return
		}
	}

	return
}

func (a *LoopbackAdaptor) HandleNodePoolCreate(
	ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {

	conditionType := hwmgmtv1alpha1.Provisioned
	var conditionReason hwmgmtv1alpha1.ConditionReason
	var conditionStatus metav1.ConditionStatus
	var message string

	if err := a.ProcessNewNodePool(ctx, hwmgr, nodepool); err != nil {
		a.Logger.Error("failed createNodePool", "err", err)
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

	return utils.DoNotRequeue(), nil
}

func (a *LoopbackAdaptor) HandleNodePoolProcessing(
	ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {

	full, err := a.CheckNodePoolProgress(ctx, hwmgr, nodepool)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed CheckNodePoolProgress: %w", err)
	}

	allocatedNodes, err := a.GetAllocatedNodes(ctx, nodepool)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get allocated nodes for %s: %w", nodepool.Name, err)
	}
	nodepool.Status.Properties.NodeNames = allocatedNodes

	if err := utils.UpdateNodePoolProperties(ctx, a.Client, nodepool); err != nil {
		return utils.RequeueWithMediumInterval(),
			fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
	}

	var result ctrl.Result

	if full {
		a.Logger.InfoContext(ctx, "NodePool request is fully allocated, name="+nodepool.Name)

		if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Completed, metav1.ConditionTrue, "Created"); err != nil {
			return utils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
		}

		result = utils.DoNotRequeue()
	} else {
		a.Logger.InfoContext(ctx, "NodePool request in progress, name="+nodepool.Name)
		result = utils.RequeueWithShortInterval()
	}

	return result, nil
}

// ProcessNewNodePool processes a new NodePool CR, verifying that there are enough free resources to satisfy the request
func (a *LoopbackAdaptor) ProcessNewNodePool(ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	cloudID := nodepool.Spec.CloudID

	a.Logger.InfoContext(ctx, "Processing ProcessNewNodePool request:",
		slog.String("loopback additional-info", hwmgr.Spec.LoopbackData.AddtionalInfo),
		slog.String("cloudID", cloudID),
	)

	_, resources, allocations, err := a.GetCurrentResources(ctx)
	if err != nil {
		return fmt.Errorf("unable to get current resources: %w", err)
	}

	for _, nodegroup := range nodepool.Spec.NodeGroup {
		freenodes := getFreeNodesInProfile(resources, allocations, nodegroup.HwProfile)
		if nodegroup.Size > len(freenodes) {
			return fmt.Errorf("not enough free resources in group %s: freenodes=%d", nodegroup.HwProfile, len(freenodes))
		}
	}

	return nil
}

// IsNodePoolFullyAllocated checks to see if a NodePool CR has been fully allocated
func (a *LoopbackAdaptor) IsNodePoolFullyAllocated(ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) (bool, error) {

	cloudID := nodepool.Spec.CloudID

	_, resources, allocations, err := a.GetCurrentResources(ctx)
	if err != nil {
		return false, fmt.Errorf("unable to get current resources: %w", err)
	}

	var cloud *cmAllocatedCloud
	for i, iter := range allocations.Clouds {
		if iter.CloudID == cloudID {
			cloud = &allocations.Clouds[i]
			break
		}
	}
	if cloud == nil {
		// Cloud has not been allocated yet
		return false, nil
	}

	// Check allocated resources
	for _, nodegroup := range nodepool.Spec.NodeGroup {
		used := cloud.Nodegroups[nodegroup.Name]
		remaining := nodegroup.Size - len(used)
		if remaining <= 0 {
			// This group is allocated
			a.Logger.InfoContext(ctx, "nodegroup is fully allocated", "nodegroup", nodegroup.Name)
			continue
		}

		freenodes := getFreeNodesInProfile(resources, allocations, nodegroup.HwProfile)
		if remaining > len(freenodes) {
			return false, fmt.Errorf("not enough free resources remaining in group %s", nodegroup.HwProfile)
		}

		// Cloud is not fully allocated, and there are resources available
		return false, nil
	}

	return true, nil
}

// ReleaseNodePool frees resources allocated to a NodePool
func (a *LoopbackAdaptor) ReleaseNodePool(ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	cloudID := nodepool.Spec.CloudID

	a.Logger.InfoContext(ctx, "Processing ReleaseNodePool request:",
		"cloudID", cloudID,
	)

	cm, _, allocations, err := a.GetCurrentResources(ctx)
	if err != nil {
		return fmt.Errorf("unable to get current resources: %w", err)
	}

	index := -1
	for i, cloud := range allocations.Clouds {
		if cloud.CloudID == cloudID {
			index = i
			break
		}
	}

	if index == -1 {
		a.Logger.InfoContext(ctx, "no allocated nodes found", "cloudID", cloudID)
		return nil
	}

	for groupname := range allocations.Clouds[index].Nodegroups {
		for _, nodename := range allocations.Clouds[index].Nodegroups[groupname] {
			if err := a.DeleteBMCSecret(ctx, nodename); err != nil {
				return fmt.Errorf("failed to delete bmc-secret for %s: %w", nodename, err)
			}

			if err := a.DeleteNode(ctx, nodename); err != nil {
				return fmt.Errorf("failed to delete node %s: %w", nodename, err)
			}
		}
	}

	allocations.Clouds = slices.Delete[[]cmAllocatedCloud](allocations.Clouds, index, index+1)

	// Update the configmap
	yamlString, err := yaml.Marshal(&allocations)
	if err != nil {
		return fmt.Errorf("unable to marshal allocated data: %w", err)
	}
	cm.Data[allocationsKey] = string(yamlString)
	if err := a.Client.Update(ctx, cm); err != nil {
		return fmt.Errorf("failed to update configmap: %w", err)
	}

	return nil
}
