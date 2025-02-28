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
	"time"

	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// CheckNodePoolProgress checks to see if a NodePool is fully allocated, allocating additional resources as needed
func (a *Adaptor) CheckNodePoolProgress(
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
			slog.String("cloudID", cloudID),
			slog.String("nodegroup name", nodegroup.NodePoolData.Name),
		)

		if err = a.AllocateNode(ctx, nodepool); err != nil {
			err = fmt.Errorf("failed to allocate node: %w", err)
			return
		}
	}

	return
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
		a.Logger.InfoContext(ctx, "failed ProcessNewNodePool", slog.String("err", err.Error()))
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

func (a *Adaptor) checkNodeUpgradeProcess(
	ctx context.Context,
	allocatedNodes []string) ([]*hwmgmtv1alpha1.Node, []*hwmgmtv1alpha1.Node, error) {

	var upgradedNodes []*hwmgmtv1alpha1.Node
	var nodesStillUpgrading []*hwmgmtv1alpha1.Node

	for _, name := range allocatedNodes {
		// Fetch the latest version of each node to ensure up-to-date status
		updatedNode, err := utils.GetNode(ctx, a.Logger, a.Client, a.Namespace, name)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get node %s: %w", name, err)
		}

		if updatedNode.Status.HwProfile == updatedNode.Spec.HwProfile {
			// Node has completed the upgrade
			upgradedNodes = append(upgradedNodes, updatedNode)
		} else {
			updatedNode.Status.HwProfile = updatedNode.Spec.HwProfile
			if err := utils.UpdateK8sCRStatus(ctx, a.Client, updatedNode); err != nil {
				return nil, nil, fmt.Errorf("failed to update status for node %s: %w", updatedNode.Name, err)
			}
			nodesStillUpgrading = append(nodesStillUpgrading, updatedNode)
		}
	}

	return upgradedNodes, nodesStillUpgrading, nil
}

func (a *Adaptor) handleNodePoolConfiguring(
	ctx context.Context,
	nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {

	var nodesToCheck []*hwmgmtv1alpha1.Node // To track nodes that we actually attempted to upgrade
	var result ctrl.Result

	a.Logger.InfoContext(ctx, "Handling Node Pool Configuring")

	allocatedNodes, err := a.GetAllocatedNodes(ctx, nodepool)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get allocated nodes for %s: %w", nodepool.Name, err)
	}

	// Stage 1: Initiate upgrades by updating node.Spec.HwProfile as necessary
	for _, name := range allocatedNodes {
		node, err := utils.GetNode(ctx, a.Logger, a.Client, a.Namespace, name)
		if err != nil {
			return utils.RequeueWithShortInterval(), err
		}
		// Check each node against each nodegroup in the node pool spec
		for _, nodegroup := range nodepool.Spec.NodeGroup {
			if node.Spec.GroupName != nodegroup.NodePoolData.Name || node.Spec.HwProfile == nodegroup.NodePoolData.HwProfile {
				continue
			}
			// Node needs an upgrade, so update Spec.HwProfile
			patch := client.MergeFrom(node.DeepCopy())
			node.Spec.HwProfile = nodegroup.NodePoolData.HwProfile
			if err = a.Client.Patch(ctx, node, patch); err != nil {
				return utils.RequeueWithShortInterval(), fmt.Errorf("failed to patch Node %s in namespace %s: %w", node.Name, node.Namespace, err)
			}
			nodesToCheck = append(nodesToCheck, node) // Track nodes we attempted to upgrade
			break
		}
	}

	// Requeue if there are nodes to check
	if len(nodesToCheck) > 0 {
		return utils.RequeueWithCustomInterval(30 * time.Second), nil
	}

	// Stage 2: Verify and track completion of upgrades
	_, nodesStillUpgrading, err := a.checkNodeUpgradeProcess(ctx, allocatedNodes)
	if err != nil {
		return utils.RequeueWithShortInterval(), fmt.Errorf("failed to check upgrade status for nodes: %w", err)
	}

	// Update NodePool status if all nodes are upgraded
	if len(nodesStillUpgrading) == 0 {
		if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
			hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.ConfigApplied, metav1.ConditionTrue, string(hwmgmtv1alpha1.ConfigSuccess)); err != nil {
			return utils.RequeueWithShortInterval(), fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
		}
		// Update the Node Pool hwMgrPlugin status
		if err = utils.UpdateNodePoolPluginStatus(ctx, a.Client, nodepool); err != nil {
			return utils.RequeueWithShortInterval(), fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", err)
		}
	} else {
		// Requeue if there are still nodes upgrading
		return utils.RequeueWithMediumInterval(), nil
	}

	return result, nil
}

func (a *Adaptor) HandleNodePoolSpecChanged(
	ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {

	if err := utils.UpdateNodePoolStatusCondition(
		ctx,
		a.Client,
		nodepool,
		hwmgmtv1alpha1.Configured,
		hwmgmtv1alpha1.ConfigUpdate,
		metav1.ConditionFalse,
		string(hwmgmtv1alpha1.AwaitConfig)); err != nil {
		return utils.RequeueWithMediumInterval(),
			fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
	}

	return a.handleNodePoolConfiguring(ctx, nodepool)
}

// ProcessNewNodePool processes a new NodePool CR, verifying that there are enough free resources to satisfy the request
func (a *Adaptor) ProcessNewNodePool(ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	cloudID := nodepool.Spec.CloudID
	a.Logger.InfoContext(ctx, "Processing ProcessNewNodePool request:",
		slog.Any("loopback additionalInfo", hwmgr.Spec.LoopbackData),
		slog.String("cloudID", cloudID),
	)

	_, resources, allocations, err := a.GetCurrentResources(ctx)
	if err != nil {
		return fmt.Errorf("unable to get current resources: %w", err)
	}

	for _, nodegroup := range nodepool.Spec.NodeGroup {
		freenodes := getFreeNodesInPool(resources, allocations, nodegroup.NodePoolData.ResourcePoolId)
		if nodegroup.Size > len(freenodes) {
			return fmt.Errorf("not enough free resources in resource pool %s: freenodes=%d", nodegroup.NodePoolData.ResourcePoolId, len(freenodes))
		}
	}

	return nil
}

// IsNodePoolFullyAllocated checks to see if a NodePool CR has been fully allocated
func (a *Adaptor) IsNodePoolFullyAllocated(ctx context.Context,
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
		used := cloud.Nodegroups[nodegroup.NodePoolData.Name]
		remaining := nodegroup.Size - len(used)
		if remaining <= 0 {
			// This group is allocated
			a.Logger.InfoContext(ctx, "nodegroup is fully allocated", slog.String("nodegroup", nodegroup.NodePoolData.Name))
			continue
		}

		freenodes := getFreeNodesInPool(resources, allocations, nodegroup.NodePoolData.ResourcePoolId)
		if remaining > len(freenodes) {
			return false, fmt.Errorf("not enough free resources remaining in resource pool %s", nodegroup.NodePoolData.ResourcePoolId)
		}

		// Cloud is not fully allocated, and there are resources available
		return false, nil
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
		a.Logger.InfoContext(ctx, "no allocated nodes found", slog.String("cloudID", cloudID))
		return nil
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
