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

package dellhwmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/hwmgrclient"
	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/logging"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ValidateNodePool performs basic validation of the nodepool data
func (a *Adaptor) ValidateNodePool(nodepool *hwmgmtv1alpha1.NodePool) error {
	for _, nodegroup := range nodepool.Spec.NodeGroup {
		if nodegroup.NodePoolData.ResourceSelector != "" {
			// Validate that the resourceSelector is parsable
			selectors := make(map[string]string)
			if err := json.Unmarshal([]byte(nodegroup.NodePoolData.ResourceSelector), &selectors); err != nil {
				return fmt.Errorf("unable to parse resourceSelector: %s", nodegroup.NodePoolData.ResourceSelector)
			}
		}
	}

	return nil
}

// HandleNodePoolCreate processes a new NodePool CR, creating a resource group on the hardware manager
func (a *Adaptor) HandleNodePoolCreate(
	ctx context.Context,
	hwmgrClient *hwmgrclient.HardwareManagerClient,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {

	conditionType := hwmgmtv1alpha1.Provisioned
	var conditionReason hwmgmtv1alpha1.ConditionReason
	var conditionStatus metav1.ConditionStatus
	var message string

	// Validate the nodepool data
	if validationErr := a.ValidateNodePool(nodepool); validationErr != nil {
		if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Failed, metav1.ConditionFalse,
			"NodePool configuration invalid: "+validationErr.Error()); err != nil {
			return utils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
		}

		return utils.DoNotRequeue(), nil
	}

	if err := a.FindResourcePoolIds(ctx, hwmgrClient, nodepool); err != nil {
		if updateErr := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Failed, metav1.ConditionFalse,
			"Failed to select resource pools: "+err.Error()); updateErr != nil {
			return utils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, updateErr)
		}

		return utils.DoNotRequeue(), nil
	}

	if err := a.ProcessNewNodePool(ctx, hwmgrClient, hwmgr, nodepool); err != nil {
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

	// Update the Node Pool hwMgrPlugin status
	if err := utils.UpdateNodePoolPluginStatus(ctx, a.Client, nodepool); err != nil {
		return utils.RequeueWithShortInterval(),
			fmt.Errorf("failed to update hwMgrPlugin observedGeneration for NodePool %s: Status: %w",
				nodepool.Name, err)
	}
	return utils.DoNotRequeue(), nil
}

// ProcessNewNodePool sends a request to the hardware manager to create a resource group
func (a *Adaptor) ProcessNewNodePool(ctx context.Context,
	hwmgrClient *hwmgrclient.HardwareManagerClient,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	a.Logger.InfoContext(ctx, "Processing ProcessNewNodePool request")

	jobId, err := hwmgrClient.CreateResourceGroup(ctx, nodepool)
	if err != nil {
		return fmt.Errorf("failed CreateResourceGroup: %w", err)
	}

	// Add the jobId in an annotation
	utils.SetJobId(nodepool, jobId)

	if err := utils.CreateOrUpdateK8sCR(ctx, a.Client, nodepool, nil, utils.PATCH); err != nil {
		return fmt.Errorf("failed to annotate nodepool %s: %w", nodepool.Name, err)
	}

	return nil
}

// HandleNodePoolProcessing checks the status of an in-progress NodePool, querying the hardware manager
// for the job status. If the job is completed, it queries for the resource group in order to create
// Node CRs corresponding to the allocated nodes.
func (a *Adaptor) HandleNodePoolProcessing(
	ctx context.Context,
	hwmgrClient *hwmgrclient.HardwareManagerClient,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {

	result := ctrl.Result{}

	jobId := utils.GetJobId(nodepool)
	if jobId == "" {
		return result, fmt.Errorf("jobId annotation is missing or empty from nodepool %s", nodepool.Name)
	}

	ctx = logging.AppendCtx(ctx, slog.String("jobId", jobId))

	// Query the hardware manager for the job status
	status, failReason, err := hwmgrClient.CheckJobStatus(ctx, jobId)
	if err != nil {
		a.Logger.InfoContext(ctx, "Resource group check failed", slog.String("error", err.Error()))
		return result, fmt.Errorf("failed to check job progress, jobId=%s: %w", jobId, err)
	}

	// Process the status response
	switch status {
	case hwmgrclient.JobStatusInProgress:
		return utils.RequeueWithShortInterval(), nil
	case hwmgrclient.JobStatusFailed:
		a.Logger.InfoContext(ctx, "Resource group creation failed", slog.String("failReason", failReason))
		if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Failed, metav1.ConditionFalse,
			fmt.Sprintf("Resource group creation failed: %s", failReason)); err != nil {
			return utils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
		}
		return result, fmt.Errorf("resource group creation failed, jobId=%s: %s", jobId, failReason)
	case hwmgrclient.JobStatusCompleted:
		a.Logger.InfoContext(ctx, "Job has completed")
	default:
		a.Logger.InfoContext(ctx, "Resource group check returned unknown status", slog.String("failReason", failReason))
		return result, fmt.Errorf("failed to check job progress, jobId=%s: %s", jobId, failReason)
	}

	// The job has completed. Get the resource group data from the hardware manager
	rg, err := hwmgrClient.GetResourceGroupFromNodePool(ctx, nodepool)
	if err != nil {
		a.Logger.InfoContext(ctx, "Failed GetResourceGroup", slog.String("error", err.Error()))

		if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Failed, metav1.ConditionFalse,
			"Failed to get resource group: "+err.Error()); err != nil {
			return utils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
		}

		return utils.DoNotRequeue(), nil
	}

	a.Logger.InfoContext(ctx, fmt.Sprintf("Validating ResourceGroup %s with nodepool %s", *rg.Id, nodepool.Name))
	if err := hwmgrClient.ValidateResourceGroup(ctx, nodepool, *rg); err != nil {
		a.Logger.InfoContext(ctx, fmt.Sprintf("Validation failed for ResourceGroup %s with nodepool %s", *rg.Id, nodepool.Name), slog.String("error", err.Error()))
		if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Failed, metav1.ConditionFalse,
			"Failed to validate resource group: "+err.Error()); err != nil {
			return utils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
		}
	}

	a.Logger.InfoContext(ctx, fmt.Sprintf("Validation complete for ResourceGroup %s with nodepool %s", *rg.Id, nodepool.Name))

	var nodelist = hwmgmtv1alpha1.NodeList{}
	if err := a.Client.List(ctx, &nodelist); err != nil {
		a.Logger.InfoContext(ctx, "Unable to query node list", slog.String("error", err.Error()))
		return utils.RequeueWithMediumInterval(), fmt.Errorf("failed to query node list: %w", err)
	}

	// Create the Node CRs corresponding to the allocated resources
	for nodegroupName, resourceSelector := range *rg.ResourceSelectors {
		for _, node := range *resourceSelector.Resources {
			nodename := utils.FindNodeInList(nodelist, nodepool.Spec.HwMgrId, *node.Id)
			if nodename != "" {
				// Node CR exists
				if slices.Contains(nodepool.Status.Properties.NodeNames, nodename) {
					a.Logger.InfoContext(ctx, "Node is already added",
						slog.String("nodename", nodename),
						slog.String("nodeId", *node.Id))
					continue
				} else {
					// TODO: Validate that the CR is current. For now, fail, as something went wrong
					a.Logger.InfoContext(ctx, "Node previously allocated, but not in nodepool properties",
						slog.String("nodename", nodename),
						slog.String("nodeId", *node.Id))
					if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
						hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Failed, metav1.ConditionFalse,
						fmt.Sprintf("Failed with partially allocated node: %s, %s", nodename, *node.Id)); err != nil {
						return utils.RequeueWithMediumInterval(),
							fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
					}

					return utils.DoNotRequeue(), nil
				}
			}
			if nodename, err := a.AllocateNode(ctx, hwmgrClient, nodepool, node, nodegroupName); err != nil {
				a.Logger.InfoContext(ctx, "Failed allocating node", slog.String("err", err.Error()))
				if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
					hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Failed, metav1.ConditionFalse,
					fmt.Sprintf("Failed to allocate node (%s): %s", *node.Name, err.Error())); err != nil {
					return utils.RequeueWithMediumInterval(),
						fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
				}

				return utils.DoNotRequeue(), nil
			} else {
				nodepool.Status.Properties.NodeNames = append(nodepool.Status.Properties.NodeNames, nodename)
			}
		}
	}

	// Update the NodePool CR
	if err := utils.UpdateNodePoolProperties(ctx, a.Client, nodepool); err != nil {
		return utils.RequeueWithMediumInterval(),
			fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
	}

	a.Logger.InfoContext(ctx, "NodePool request is fully allocated")

	if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
		hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Completed, metav1.ConditionTrue, "Created"); err != nil {
		return utils.RequeueWithMediumInterval(),
			fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
	}

	utils.ClearJobId(nodepool)
	if err := utils.CreateOrUpdateK8sCR(ctx, a.Client, nodepool, nil, utils.PATCH); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to clear annotation from nodepool %s: %w", nodepool.Name, err)
	}

	result = utils.DoNotRequeue()

	return result, nil
}

// ReleaseNodePool frees resources allocated to a NodePool
func (a *Adaptor) ReleaseNodePool(ctx context.Context,
	hwmgrClient *hwmgrclient.HardwareManagerClient,
	hwmgr *pluginv1alpha1.HardwareManager,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	a.Logger.InfoContext(ctx, "Processing ReleaseNodePool request")

	// Issue a resource group deletion request to the hardware manager
	jobId, err := hwmgrClient.DeleteResourceGroup(ctx, nodepool)
	if err != nil {
		return fmt.Errorf("failed CreateResourceGroup: %w", err)
	}

	ctx = logging.AppendCtx(ctx, slog.String("jobId", jobId))

	// TODO: Can we even requeue the finalizer? If so, we should have a separate annotation for this jobID.
	// For now, poll until the job finishes
	finished := false
	for !finished {
		time.Sleep(time.Second * 10)
		a.Logger.InfoContext(ctx, "Checking deletion job progress")

		status, failReason, err := hwmgrClient.CheckJobStatus(ctx, jobId)
		if err != nil {
			a.Logger.InfoContext(ctx, "Deletion job progress check failed", slog.String("error", err.Error()))
			return fmt.Errorf("deletion job progress check failed: %w", err)
		}

		// TODO: Currently, the hardware manager is clearing the job immediately on deletion, so the check fails
		a.Logger.InfoContext(ctx, "Deletion job progress check returned", slog.Any("status", status), slog.String("failReason", failReason))
	}

	return nil
}

func (a *Adaptor) handleNodePoolConfiguring(
	ctx context.Context,
	hwmgrClient *hwmgrclient.HardwareManagerClient,
	nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {

	var result ctrl.Result

	a.Logger.InfoContext(ctx, "Handling Node Pool Configuring")

	nodelist, err := utils.GetChildNodes(ctx, a.Logger, a.Client, nodepool)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get child nodes for Node Pool %s: %w", nodepool.Name, err)
	}

	a.Logger.InfoContext(ctx, "Checking for node with profile update in-progress")

	// Search for a node that is currently being updated
	if node := utils.FindNodeUpdateInProgress(nodelist); node != nil {
		// A node has an update already in progress

		jobId := utils.GetJobId(node)
		if jobId == "" {
			return result, fmt.Errorf("jobId annotation is missing or empty from node %s", node.Name)
		}

		// Query the hardware manager for the job status
		status, failReason, err := hwmgrClient.CheckJobStatus(ctx, jobId)
		if err != nil {
			a.Logger.InfoContext(ctx, "Profile update job progress check failed", slog.String("error", err.Error()))
			return result, fmt.Errorf("failed to check profile update job progress, jobId=%s: %w", jobId, err)
		}

		// Process the status response
		switch status {
		case hwmgrclient.JobStatusInProgress:
			return utils.RequeueWithShortInterval(), nil
		case hwmgrclient.JobStatusFailed:
			a.Logger.InfoContext(ctx, "Profile update creation failed", slog.String("failReason", failReason))
			if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
				hwmgmtv1alpha1.Configured,
				hwmgmtv1alpha1.Failed,
				metav1.ConditionFalse,
				fmt.Sprintf("Profile update creation failed: %s", failReason)); err != nil {
				return utils.RequeueWithMediumInterval(),
					fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
			}
			// TODO: Mark the config change as failed
			return result, fmt.Errorf("profile update creation failed, jobId=%s: %s", jobId, failReason)
		case hwmgrclient.JobStatusCompleted:
			a.Logger.InfoContext(ctx, "Profile update job has completed")
		default:
			a.Logger.InfoContext(ctx, "Profile update check returned unknown status", slog.String("failReason", failReason))
			return result, fmt.Errorf("failed to check profile update job progress, jobId=%s: %s", jobId, failReason)
		}

		// Node update is complete
		a.Logger.InfoContext(ctx, "Node update complete", slog.String("nodename", node.Name))
		node.Status.HwProfile = node.Spec.HwProfile
		if err := utils.UpdateK8sCRStatus(ctx, a.Client, node); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update status for node %s: %w", node.Name, err)
		}

		utils.ClearJobId(node)
		if err := utils.CreateOrUpdateK8sCR(ctx, a.Client, node, nil, utils.PATCH); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to clear annotation from node %s: %w", node.Name, err)
		}

		return utils.RequeueImmediately(), nil
	}

	a.Logger.InfoContext(ctx, "Checking for nodes to update")

	// There are no nodes currently in-progress, so we can look for the next one to start updating
	for _, nodegroup := range nodepool.Spec.NodeGroup {
		newHwProfile := nodegroup.NodePoolData.HwProfile
		node := utils.FindNextNodeToUpdate(nodelist, nodegroup.NodePoolData.Name, newHwProfile)
		if node == nil {
			// No more nodes to update in this nodegroup
			continue
		}

		a.Logger.InfoContext(ctx, "Issuing profile update to node",
			slog.String("hwMgrNodeId", node.Spec.HwMgrNodeId),
			slog.String("curHwProfile", node.Spec.HwProfile),
			slog.String("newHwProfile", newHwProfile))

		jobId, err := hwmgrClient.UpdateResourceProfile(ctx, node, newHwProfile)
		if err != nil {
			return utils.RequeueWithShortInterval(), fmt.Errorf("failed to update resource for node %s: %w", node.Name, err)
		}

		a.Logger.InfoContext(ctx, "Updating Node CR with new profile",
			slog.String("nodename", node.Name),
			slog.String("newHwProfile", newHwProfile),
			slog.String("jobId", jobId),
		)

		// Copy the current node object for patching
		patch := client.MergeFrom(node.DeepCopy())

		// Set the new profile in the spec
		node.Spec.HwProfile = newHwProfile

		// Record the jobId in an annotation
		utils.SetJobId(node, jobId)

		if err = a.Client.Patch(ctx, node, patch); err != nil {
			return utils.RequeueWithShortInterval(), fmt.Errorf("failed to patch Node %s in namespace %s: %w", node.Name, node.Namespace, err)
		}

		// Requeue to check update progress
		return utils.RequeueWithMediumInterval(), nil
	}

	// All nodes have been updated
	a.Logger.InfoContext(ctx, "All nodes have been updated to new profile")
	if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
		hwmgmtv1alpha1.Configured, hwmgmtv1alpha1.ConfigApplied, metav1.ConditionTrue, string(hwmgmtv1alpha1.ConfigSuccess)); err != nil {
		return utils.RequeueWithShortInterval(), fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
	}
	// Update the Node Pool hwMgrPlugin status
	if err = utils.UpdateNodePoolPluginStatus(ctx, a.Client, nodepool); err != nil {
		return utils.RequeueWithShortInterval(), fmt.Errorf("failed to update hwMgrPlugin observedGeneration Status: %w", err)
	}

	return result, nil
}

func (a *Adaptor) HandleNodePoolSpecChanged(
	ctx context.Context,
	hwmgrClient *hwmgrclient.HardwareManagerClient,
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

	return a.handleNodePoolConfiguring(ctx, hwmgrClient, nodepool)
}
