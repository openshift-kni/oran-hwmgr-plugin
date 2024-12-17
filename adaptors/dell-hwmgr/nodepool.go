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

const (
	JobIdAnnotation = "hwmgr-plugin.oran.openshift.io/jobId"
)

// ValidateNodePool performs basic validation of the nodepool data
func (a *Adaptor) ValidateNodePool(nodepool *hwmgmtv1alpha1.NodePool) error {
	resourceTypeId := utils.GetResourceTypeId(nodepool)
	if resourceTypeId == "" {
		return fmt.Errorf("nodepool %s is missing resourceTypeId in spec", nodepool.Name)
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
	annotations := nodepool.GetAnnotations()
	annotations[JobIdAnnotation] = jobId
	nodepool.SetAnnotations(annotations)
	if err := utils.CreateK8sCR(ctx, a.Client, nodepool, nil, utils.PATCH); err != nil {
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
	annotation := nodepool.GetAnnotations()
	if annotation == nil {
		return result, fmt.Errorf("annotations are missing from nodePool %s in namespace %s", nodepool.Name, nodepool.Namespace)
	}

	// Ensure the jobId annotation exists and is not empty
	jobId, exists := annotation[JobIdAnnotation]
	if !exists || jobId == "" {
		return result, fmt.Errorf("%s annotation is missing or empty from nodepool %s", JobIdAnnotation, nodepool.Name)
	}

	ctx = logging.AppendCtx(ctx, slog.String("jobId", jobId))

	// Query the hardware manager for the job status
	status, err := hwmgrClient.CheckResourceGroupRequest(ctx, jobId)
	if err != nil {
		a.Logger.InfoContext(ctx, "Job progress check failed", slog.String("error", err.Error()))
		return result, fmt.Errorf("failed to check job progress, jobId=%s: %w", jobId, err)
	}

	if status == nil || status.Brief == nil || status.Brief.Status == nil {
		a.Logger.InfoContext(ctx, "Job progress check missing data", slog.Any("status", status))
		return result, fmt.Errorf("job progress check missing data, jobId=%s: %w", jobId, err)
	}

	// Process the status response
	switch *status.Brief.Status {
	case "started":
		a.Logger.InfoContext(ctx, "Job has started")
		return utils.RequeueWithShortInterval(), nil
	case "pending":
		a.Logger.InfoContext(ctx, "Job is pending")
		return utils.RequeueWithShortInterval(), nil
	case "completed":
		a.Logger.InfoContext(ctx, "Job has completed")
	default:
		a.Logger.InfoContext(ctx, "Job has failed", slog.Any("status", status))
		msg := fmt.Sprintf("resource group creation has failed: status=%s", *status.Brief.Status)
		if status.Brief.FailReason != nil {
			msg += fmt.Sprintf(", message=%s", *status.Brief.FailReason)
		}
		return result, fmt.Errorf("%s", msg)
	}

	// The job has completed. Get the resource group data from the hardware manager
	rg, err := hwmgrClient.GetResourceGroup(ctx, nodepool)
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

	// TODO: Need to add validation to ensure the rg satisfies the nodepool

	// WIP: Perform Validation
	a.Logger.InfoContext(ctx, "Validating created resource group with nodepool")

	if err := utils.ValidateNodepoolWithResourceSelector(ctx, nodepool, *rg.ResourceSelectors); err != nil {
		a.Logger.InfoContext(ctx, "Validation failed for resource group", slog.String("error", err.Error()))
		return utils.RequeueWithMediumInterval(), fmt.Errorf("validation failed for resource group: %w", err)
	}

	a.Logger.InfoContext(ctx, "Validation complete for resource group with nodepool")

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
					a.Logger.InfoContext(ctx, "Node previously allocted, but not in nodepool properties",
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

		status, err := hwmgrClient.CheckResourceGroupRequest(ctx, jobId)
		if err != nil {
			a.Logger.InfoContext(ctx, "Job progress check failed", slog.String("error", err.Error()))
			return fmt.Errorf("job progress check failed: %w", err)
		}

		if status == nil || status.Brief == nil || status.Brief.Status == nil {
			a.Logger.InfoContext(ctx, "Job progress check missing data", slog.Any("status", status))
			return fmt.Errorf("job progress check returned with no data: %v", status)
		}

		switch *status.Brief.Status {
		case "started":
			a.Logger.InfoContext(ctx, "Job has started")
			continue
		case "completed":
			a.Logger.InfoContext(ctx, "Job has completed")
			finished = true
		default:
			a.Logger.InfoContext(ctx, "Job has failed", slog.Any("status", status))
			msg := fmt.Sprintf("resource group deletion has failed: status=%s", *status.Brief.Status)
			if status.Brief.FailReason != nil {
				msg += fmt.Sprintf(", message=%s", *status.Brief.FailReason)
			}
			return fmt.Errorf("%s", msg)
		}
	}

	return nil
}

func (a *Adaptor) checkNodeUpgradeProcess(
	ctx context.Context,
	allocatedNodes []string) ([]*hwmgmtv1alpha1.Node, []*hwmgmtv1alpha1.Node, error) {

	var upgradedNodes []*hwmgmtv1alpha1.Node
	var nodesStillUpgrading []*hwmgmtv1alpha1.Node

	for _, name := range allocatedNodes {
		// Fetch the latest version of each node to ensure up-to-date status
		updatedNode, err := utils.GetNode(ctx, a.Client, a.Namespace, name)
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

			// TODO: notify Dell hwmgr on successful node update
			nodesStillUpgrading = append(nodesStillUpgrading, updatedNode)
		}
	}

	return upgradedNodes, nodesStillUpgrading, nil
}

// GetAllocatedNodes gets a list of nodes allocated for the specified NodePool CR
func (a *Adaptor) GetAllocatedNodes(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) (allocatedNodes []string, err error) {

	allocatedNodes = []string{}

	for _, ng := range nodepool.Spec.NodeGroup {
		allocatedNodes = append(allocatedNodes, ng.NodePoolData.Name)
	}
	if len(allocatedNodes) == 0 {
		return allocatedNodes, fmt.Errorf("failed to allocate nodes from nodepool:%s", nodepool.GetName())
	}
	return allocatedNodes, nil
}

func (a *Adaptor) handleNodePoolConfiguring(
	ctx context.Context,
	nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {

	var nodesToCheck []*hwmgmtv1alpha1.Node // To track nodes that we actually attempted to upgrade
	var result ctrl.Result

	a.Logger.InfoContext(ctx, "Handling Node Pool Configuring, name="+nodepool.Name)

	allocatedNodes := nodepool.Status.Properties.NodeNames

	if len(allocatedNodes) == 0 {
		return ctrl.Result{}, fmt.Errorf("failed to get allocated nodes for Node Pool:%s", nodepool.Name)
	}

	// Stage 1: Initiate upgrades by updating node.Spec.HwProfile as necessary
	for _, name := range allocatedNodes {
		node, err := utils.GetNode(ctx, a.Client, a.Namespace, name)
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

	return a.handleNodePoolConfiguring(ctx, nodepool)
}
