/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metal3

import (
	"context"
	"fmt"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/client"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
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

	a.Logger.InfoContext(ctx, "Processing ProcessNewNodePool request")

	// Check if enough resources are available for each NodeGroup
	for _, nodeGroup := range nodepool.Spec.NodeGroup {
		if nodeGroup.Size == 0 {
			continue // Skip groups with size 0
		}

		// Fetch unallocated BMHs for the specific site and poolID
		bmhListForGroup, err := a.FetchBMHList(ctx, nodepool.Spec.Site, nodeGroup.NodePoolData, UnallocatedBMHs, "")
		if err != nil {
			return fmt.Errorf("unable to fetch BMHs for nodegroup=%s: %w", nodeGroup.NodePoolData.Name, err)
		}

		// Ensure enough resources exist in the requested pool
		if len(bmhListForGroup.Items) < nodeGroup.Size {
			return fmt.Errorf("not enough free resources matching nodegroup=%s criteria: freenodes=%d, required=%d",
				nodeGroup.NodePoolData.Name, len(bmhListForGroup.Items), nodeGroup.Size)
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

// handleInProgressUpdate checks for any node marked as having a configuration update in progress.
// If a node is found and its associated BMH status indicates that the update has completed,
// it updates the node status, clears the annotation, applies the post-change annotation, and
// requeues immediately.
func (a *Adaptor) handleInProgressUpdate(ctx context.Context, nodelist *hwmgmtv1alpha1.NodeList) (ctrl.Result, bool, error) {
	node := utils.FindNodeConfigInProgress(nodelist)
	if node == nil {
		a.Logger.InfoContext(ctx, "No node found that is in progress")
		return ctrl.Result{}, false, nil
	}
	a.Logger.InfoContext(ctx, "Node found that is in progress", slog.String("node", node.Name))
	bmh, err := a.getBMHForNode(ctx, node)
	if err != nil {
		return ctrl.Result{}, true, fmt.Errorf("failed to get BMH for node %s: %w", node.Name, err)
	}

	// Check if the update is complete by examining the BMH operational status.
	if bmh.Status.OperationalStatus == metal3v1alpha1.OperationalStatusOK {
		a.Logger.InfoContext(ctx, "BMH update complete", slog.String("BMH", bmh.Name))

		// Update the node's status to reflect the new hardware profile.
		node.Status.HwProfile = node.Spec.HwProfile
		if err := utils.UpdateK8sCRStatus(ctx, a.Client, node); err != nil {
			return ctrl.Result{}, true, fmt.Errorf("failed to update status for node %s: %w", node.Name, err)
		}
		utils.RemoveConfigAnnotation(node)
		if err := utils.CreateOrUpdateK8sCR(ctx, a.Client, node, nil, utils.PATCH); err != nil {
			return ctrl.Result{}, true, fmt.Errorf("failed to clear annotation from node %s: %w", node.Name, err)
		}

		// Apply the post-change annotation to indicate completion.
		if err := a.applyPostChangeAnnotation(ctx, bmh); err != nil {
			return ctrl.Result{}, true, fmt.Errorf("failed to apply post-change annotation for BMH %s/%s: %w", bmh.Namespace, bmh.Name, err)
		}

		return utils.RequeueImmediately(), true, nil
	}
	a.Logger.InfoContext(ctx, "BMH config in progress", slog.String("bmh", bmh.Name))
	return utils.RequeueWithMediumInterval(), true, nil
}

// initiateNodeUpdate starts the update process for the given node by processing the new hardware profile,
func (a *Adaptor) initiateNodeUpdate(ctx context.Context, node *hwmgmtv1alpha1.Node, newHwProfile string) (ctrl.Result, error) {

	bmh, err := a.getBMHForNode(ctx, node)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get BMH for node %s: %w", node.Name, err)
	}
	a.Logger.InfoContext(ctx, "Issuing profile update to node",
		slog.String("hwMgrNodeId", node.Spec.HwMgrNodeId),
		slog.String("curHwProfile", node.Spec.HwProfile),
		slog.String("newHwProfile", newHwProfile))

	// Copy the current node object for patching
	patch := client.MergeFrom(node.DeepCopy())

	// Set the new profile in the spec
	node.Spec.HwProfile = newHwProfile

	if err = a.Client.Patch(ctx, node, patch); err != nil {
		return utils.RequeueWithShortInterval(), fmt.Errorf("failed to patch Node %s in namespace %s: %w", node.Name, node.Namespace, err)
	}

	updateRequired, err := a.processHwProfile(ctx, bmh, node.Name, true)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to process hardware profile for node %s: %w", node.Name, err)
	}
	a.Logger.InfoContext(ctx, "Processed hardware profile", slog.Bool("updatedRequired", updateRequired))

	if updateRequired {
		// Apply a pre-change annotation to the BMH.
		if err := a.applyPreChangeAnnotation(ctx, bmh); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to apply pre-change annotation for BMH %s/%s: %w", bmh.Namespace, bmh.Name, err)
		}
		// Return a medium interval requeue to allow time for the update to progress.
		return utils.RequeueWithMediumInterval(), nil
	}
	return ctrl.Result{}, nil
}

func (a *Adaptor) handleNodePoolConfiguring(
	ctx context.Context,
	nodepool *hwmgmtv1alpha1.NodePool,
) (ctrl.Result, error) {

	a.Logger.InfoContext(ctx, "Handling Node Pool Configuring")

	nodelist, err := utils.GetChildNodes(ctx, a.Logger, a.Client, nodepool)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get child nodes for Node Pool %s: %w", nodepool.Name, err)
	}

	// STEP 1: Handle nodes in transition (from update-needed to update in-progress).
	updating, err := a.handleTransitionNodes(ctx, nodelist, true)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error handling transitioning nodes: %w", err)
	}
	if updating {
		// Return a short interval requeue to allow time for the transition
		return utils.RequeueWithShortInterval(), nil
	}

	// STEP 2: Process any node that is already in the update-in-progress state.
	if res, handled, err := a.handleInProgressUpdate(ctx, nodelist); err != nil || handled {
		return res, err
	}

	// STEP 3: Look for the next node that requires an update.
	for _, nodegroup := range nodepool.Spec.NodeGroup {
		newHwProfile := nodegroup.NodePoolData.HwProfile
		node := utils.FindNextNodeToUpdate(nodelist, nodegroup.NodePoolData.Name, newHwProfile)
		if node == nil {
			// No node pending update in this nodegroup; continue to the next one.
			continue
		}

		// Initiate the update process for the selected node.
		res, err := a.initiateNodeUpdate(ctx, node, newHwProfile)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to initiate node update for node %s: %w", node.Name, err)
		}
		// Requeue after starting the update on one node.
		return res, nil
	}

	// STEP 4: If no nodes are pending updates, mark the NodePool as fully configured.
	a.Logger.InfoContext(ctx, "All nodes have been updated to new profile")
	if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
		hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.ConfigApplied, metav1.ConditionTrue,
		string(hwmgmtv1alpha1.ConfigSuccess)); err != nil {
		return utils.RequeueWithShortInterval(), fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
	}

	if err := utils.UpdateNodePoolPluginStatus(ctx, a.Client, nodepool); err != nil {
		return utils.RequeueWithShortInterval(), fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", err)
	}

	return ctrl.Result{}, nil
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

// ReleaseNodePool frees resources allocated to a NodePool
func (a *Adaptor) ReleaseNodePool(ctx context.Context,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	cloudID := nodepool.Spec.CloudID

	a.Logger.InfoContext(ctx, "Processing ReleaseNodePool request:",
		slog.String("cloudID", cloudID),
	)

	// remove the allocated label from BMHs and finalizer from the corresponding PreprovisioningImage resources
	nodelist, err := utils.GetChildNodes(ctx, a.Logger, a.Client, nodepool)
	if err != nil {
		return fmt.Errorf("failed to get child nodes for Node Pool %s: %w", nodepool.Name, err)
	}
	for _, node := range nodelist.Items {
		bmh, err := a.getBMHForNode(ctx, &node)
		if err != nil {
			return fmt.Errorf("failed to get BMH for node %s: %w", node.Name, err)
		}
		if err = a.unmarkBMHAllocated(ctx, bmh); err != nil {
			return fmt.Errorf("failed to unmarkBMHAllocated: %w", err)
		}
		if err = a.removeMetal3Finalizer(ctx, bmh.Name, bmh.Namespace); err != nil {
			return fmt.Errorf("failed to remove finalizer: %w", err)
		}
	}

	return nil
}
