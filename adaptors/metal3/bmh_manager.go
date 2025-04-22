/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metal3

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// BMHAllocationStatus defines filtering options for FetchBMHList
type BMHAllocationStatus string

const (
	AllBMHs         BMHAllocationStatus = "all"
	UnallocatedBMHs BMHAllocationStatus = "unallocated"
	AllocatedBMHs   BMHAllocationStatus = "allocated"
)

const (
	BiosUpdateNeededAnnotation     = "hwmgr-plugin.oran.openshift.io/bios-update-needed"
	FirmwareUpdateNeededAnnotation = "hwmgr-plugin.oran.openshift.io/firmware-update-needed"
	BmhNamespaceLabel              = "baremetalhost.metal3.io/namespace"
	BmhAllocatedLabel              = "hwmgr-plugin.oran.openshift.io/allocated"
	Metal3Finalizer                = "preprovisioningimage.metal3.io"
	UpdateReasonBIOSSettings       = "bios-settings-update"
	UpdateReasonFirmware           = "firmware-update"
	LabelValueTrue                 = "true"
	LabelOpAdd                     = "add"
	LabelOpRemove                  = "remove"
)

// Struct definitions for the nodelist configmap
type bmhBmcInfo struct {
	Address         string `json:"address,omitempty"`
	CredentialsName string `json:"credentialsName,omitempty"`
}

type bmhNodeInfo struct {
	ResourcePoolID string                      `json:"poolID,omitempty"`
	BMC            *bmhBmcInfo                 `json:"bmc,omitempty"`
	Interfaces     []*hwmgmtv1alpha1.Interface `json:"interfaces,omitempty"`
}

func (a *Adaptor) updateBMHLabelWithRetry(ctx context.Context, name types.NamespacedName, labelKey, labelValue, operation string) error {
	// nolint: wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		// Fetch the latest version of the BMH
		var latestBMH metal3v1alpha1.BareMetalHost
		if err := a.Client.Get(ctx, name, &latestBMH); err != nil {
			a.Logger.ErrorContext(ctx, "Failed to fetch BMH for label update",
				slog.Any("bmh", name),
				slog.String("error", err.Error()))
			return err
		}

		// Early return for no-op remove
		if operation == LabelOpRemove {
			if latestBMH.Labels == nil {
				a.Logger.InfoContext(ctx, "BMH has no labels, skipping remove operation",
					slog.Any("bmh", name))
				return nil
			}
			if _, exists := latestBMH.Labels[labelKey]; !exists {
				a.Logger.InfoContext(ctx, "Label not present, skipping remove operation",
					slog.Any("bmh", name),
					slog.String("label", labelKey))
				return nil
			}
		}

		// Create a patch base
		patch := client.MergeFrom(latestBMH.DeepCopy())

		if operation == LabelOpAdd && latestBMH.Labels == nil {
			latestBMH.Labels = make(map[string]string)
		}

		switch operation {
		case LabelOpAdd:
			latestBMH.Labels[labelKey] = labelValue
		case LabelOpRemove:
			delete(latestBMH.Labels, labelKey)
		default:
			return fmt.Errorf("unsupported operation: %s", operation)
		}

		// Apply the patch
		if err := a.Client.Patch(ctx, &latestBMH, patch); err != nil {
			a.Logger.ErrorContext(ctx, "Failed to update BMH label",
				slog.String("bmh", name.Name),
				slog.String("operation", operation),
				slog.String("error", err.Error()))
			return fmt.Errorf("failed to %s label on BMH %s: %w", operation, name.Name, err)
		}

		a.Logger.InfoContext(ctx, "Successfully updated BMH label",
			slog.String("bmh", name.Name),
			slog.String("operation", operation))
		return nil
	})
}

// FetchBMHList retrieves BareMetalHosts filtered by site ID, allocation status, and optional namespace.
func (a *Adaptor) FetchBMHList(
	ctx context.Context,
	site string,
	nodePoolData hwmgmtv1alpha1.NodePoolData,
	allocationStatus BMHAllocationStatus,
	namespace string) (metal3v1alpha1.BareMetalHostList, error) {

	var bmhList metal3v1alpha1.BareMetalHostList
	opts := []client.ListOption{}
	matchingLabels := make(client.MatchingLabels)

	// Add site ID filter if provided
	if site != "" {
		matchingLabels[LabelSiteID] = site
	}

	// Add pool ID filter if provided
	if nodePoolData.ResourcePoolId != "" {
		matchingLabels[LabelResourcePoolID] = nodePoolData.ResourcePoolId
	}

	if nodePoolData.ResourceSelector != "" {
		resourceSelectors := make(map[string]string)

		if err := json.Unmarshal([]byte(nodePoolData.ResourceSelector), &resourceSelectors); err != nil {
			return bmhList, fmt.Errorf("unable to parse resourceSelector: %s: %w", nodePoolData.ResourceSelector, err)
		}

		for key, value := range resourceSelectors {
			fullLabelName := key
			if !REPatternResourceSelectorLabel.MatchString(fullLabelName) {
				fullLabelName = LabelPrefixResourceSelector + key
			}

			matchingLabels[fullLabelName] = value
		}
	}

	// Add namespace filter if provided
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}

	// Apply allocation filtering based on enum value
	switch allocationStatus {
	case AllocatedBMHs:
		// Fetch only allocated BMHs
		matchingLabels[BmhAllocatedLabel] = LabelValueTrue

	case UnallocatedBMHs:
		// Fetch only unallocated BMHs
		selector := metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      BmhAllocatedLabel,
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{LabelValueTrue}, // Exclude allocated=true
				},
			},
		}
		labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return bmhList, fmt.Errorf("failed to create label selector: %w", err)
		}
		opts = append(opts, client.MatchingLabelsSelector{Selector: labelSelector})

	case AllBMHs:
		// fetch all BMHs
	}

	opts = append(opts, matchingLabels)

	// Fetch BMHs based on filters
	if err := a.Client.List(ctx, &bmhList, opts...); err != nil {
		return bmhList, fmt.Errorf("failed to get BMH list: %w", err)
	}

	if len(bmhList.Items) == 0 {
		a.Logger.WarnContext(ctx, "No BareMetalHosts found",
			slog.String(LabelSiteID, site),
			slog.String("Allocation Status", string(allocationStatus)))
		return bmhList, nil
	}

	// we only care about the ones in "available" state
	return filterAvailableBMHs(bmhList), nil
}

// filterAvailableBMHs filters out BareMetalHosts that are not in the "Available" provisioning state.
func filterAvailableBMHs(bmhList metal3v1alpha1.BareMetalHostList) metal3v1alpha1.BareMetalHostList {
	var filteredBMHs metal3v1alpha1.BareMetalHostList
	for _, bmh := range bmhList.Items {
		if bmh.Status.Provisioning.State == metal3v1alpha1.StateAvailable {
			filteredBMHs.Items = append(filteredBMHs.Items, bmh)
		}
	}
	return filteredBMHs
}

// GroupBMHsByResourcePool groups unallocated BMHs by resource pool ID.
func (a *Adaptor) GroupBMHsByResourcePool(unallocatedBMHs metal3v1alpha1.BareMetalHostList) map[string][]metal3v1alpha1.BareMetalHost {
	grouped := make(map[string][]metal3v1alpha1.BareMetalHost)
	for _, bmh := range unallocatedBMHs.Items {
		if resourcePoolID, exists := bmh.Labels[LabelResourcePoolID]; exists {
			grouped[resourcePoolID] = append(grouped[resourcePoolID], bmh)
		}
	}
	return grouped
}

func (a *Adaptor) buildInterfacesFromBMH(nodepool *hwmgmtv1alpha1.NodePool, bmh metal3v1alpha1.BareMetalHost) []*hwmgmtv1alpha1.Interface {
	var interfaces []*hwmgmtv1alpha1.Interface

	for _, nic := range bmh.Status.HardwareDetails.NIC {
		label := ""

		if strings.EqualFold(nic.MAC, bmh.Spec.BootMACAddress) {
			// For the boot interface, use the label from the bootInterfaceLabel annotation on the nodepool CR
			label = nodepool.Annotations[hwmgmtv1alpha1.BootInterfaceLabelAnnotation]
		} else {
			// Interface labels with MACs use - instead of :
			hyphenatedMac := strings.ReplaceAll(nic.MAC, ":", "-")

			// Process interface labels
			for fullLabel, value := range bmh.Labels {
				match := REPatternInterfaceLabel.FindStringSubmatch(fullLabel)
				if len(match) != 2 {
					continue
				}

				if value == nic.Name || strings.EqualFold(hyphenatedMac, value) {
					// We found a matching label
					label = match[1]
					break
				}
			}
		}

		interfaces = append(interfaces, &hwmgmtv1alpha1.Interface{
			Name:       nic.Name,
			MACAddress: nic.MAC,
			Label:      label,
		})
	}

	return interfaces
}

func (a *Adaptor) countNodesInGroup(ctx context.Context, nodeNames []string, groupName string) int {
	count := 0
	for _, nodeName := range nodeNames {
		node, err := utils.GetNode(ctx, a.Logger, a.Client, a.Namespace, nodeName)
		if err == nil && node != nil {
			if node.Spec.GroupName == groupName {
				count++
			}
		}
	}
	return count
}

func (a *Adaptor) isBMHAllocated(bmh *metal3v1alpha1.BareMetalHost) bool {
	if currentValue, exists := bmh.Labels[BmhAllocatedLabel]; exists && currentValue == LabelValueTrue {
		return true
	}
	return false
}

func (a *Adaptor) clearBMHNetworkData(ctx context.Context, name types.NamespacedName) error {
	// nolint:wrapcheck
	return retry.OnError(retry.DefaultRetry, errors.IsConflict, func() error {
		updatedBmh := &metal3v1alpha1.BareMetalHost{}

		if err := a.Get(ctx, name, updatedBmh); err != nil {
			return fmt.Errorf("failed to fetch BMH %s/%s: %w", name.Namespace, name.Name, err)
		}
		if updatedBmh.Spec.PreprovisioningNetworkDataName != "" {
			updatedBmh.Spec.PreprovisioningNetworkDataName = ""
			return a.Client.Update(ctx, updatedBmh)
		}
		return nil
	})
}

func (a *Adaptor) processHwProfile(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost, nodeName string) (bool, error) {

	node, err := utils.GetNode(ctx, a.Logger, a.Client, a.Namespace, nodeName)
	if err != nil {
		return false, fmt.Errorf("failed to get node %s/%s: %w", a.Namespace, nodeName, err)
	}

	name := types.NamespacedName{
		Name:      node.Spec.HwProfile,
		Namespace: a.Namespace,
	}

	hwProfile := &pluginv1alpha1.HardwareProfile{}
	if err := a.Client.Get(ctx, name, hwProfile); err != nil {
		return false, fmt.Errorf("unable to find HardwareProfile CR (%s): %w", node.Spec.HwProfile, err)
	}

	// Check if BIOS update is required
	updateRequired := false
	if hwProfile.Spec.Bios.Attributes != nil {
		updateRequired, err = a.IsBiosUpdateRequired(ctx, bmh, hwProfile.Spec.Bios)
		if err != nil {
			return false, err
		}
	}

	// Check if firmware update is required
	firmwareUpdateRequired, err := a.IsFirmwareUpdateRequired(ctx, bmh, hwProfile.Spec)
	if err != nil {
		return false, err
	}

	// If bios update is required, annotate BMH
	if updateRequired {
		if err := a.addBMHAnnotation(ctx, bmh, BiosUpdateNeededAnnotation); err != nil {
			return true, fmt.Errorf("failed to annotate BMH %s/%s: %w", bmh.Namespace, bmh.Name, err)
		}
		return true, nil
	}

	// if firmware update is required, annotate BMH
	if firmwareUpdateRequired {
		if err := a.addBMHAnnotation(ctx, bmh, FirmwareUpdateNeededAnnotation); err != nil {
			return true, fmt.Errorf("failed to annotate BMH %s/%s: %w", bmh.Namespace, bmh.Name, err)
		}
		return true, nil
	}

	// No update required
	return false, nil

}

func (a *Adaptor) checkBMHStatus(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost, state metal3v1alpha1.ProvisioningState) bool {
	// Check if the BMH is in  desired state
	if bmh.Status.Provisioning.State == state {
		a.Logger.InfoContext(ctx, "BMH is now in desired state", slog.String("BMH", bmh.Name), slog.Any("State", state))
		return true
	}
	return false
}

// aannotateNodeConfigInProgress sets an annotation on the corresponding Node object to indicate configuration is in progress.
func (a *Adaptor) annotateNodeConfigInProgress(ctx context.Context, nodeName, reason string) error {
	// Fetch the Node object
	node := &hwmgmtv1alpha1.Node{}
	if err := a.Client.Get(ctx, types.NamespacedName{Name: nodeName, Namespace: a.Namespace}, node); err != nil {
		return fmt.Errorf("unable to get Node object (%s): %w", nodeName, err)
	}

	// Set annotation to indicate configuration is in progress
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	utils.SetConfigAnnotation(node, reason)

	// Update the Node object
	if err := a.Client.Update(ctx, node); err != nil {
		a.Logger.InfoContext(ctx, "Failed to annotate node for BIOS configuration", slog.String("node", nodeName))
		return fmt.Errorf("failed to update node %s: %w", nodeName, err)
	}
	a.Logger.InfoContext(ctx, "Annotated node with BIOS config", slog.String("node", nodeName))
	return nil
}

func (a *Adaptor) handleBMHsTransitioningToPreparing(ctx context.Context, nodelist *hwmgmtv1alpha1.NodeList) (bool, error) {

	for _, node := range nodelist.Items {
		bmh, err := a.getBMHForNode(ctx, &node)
		if err != nil {
			return false, fmt.Errorf("failed to get BMH for node %s: %w", node.Name, err)
		}

		updateCases := []struct {
			AnnotationKey string
			Reason        string
			LogLabel      string
		}{
			{BiosUpdateNeededAnnotation, UpdateReasonBIOSSettings, "BIOS settings"},
			{FirmwareUpdateNeededAnnotation, UpdateReasonFirmware, "firmware"},
		}

		for _, uc := range updateCases {
			_, updateNeeded := bmh.Annotations[uc.AnnotationKey]
			if !updateNeeded {
				continue
			}

			if bmh.Status.Provisioning.State != metal3v1alpha1.StatePreparing {
				a.Logger.InfoContext(ctx, "BMH is not in 'Preparing' state yet, requeuing",
					slog.String("BMH", bmh.Name))
				return true, nil
			}

			a.Logger.InfoContext(ctx, fmt.Sprintf("BMH transitioned to 'Preparing' state for %s update", uc.LogLabel),
				slog.String("BMH", bmh.Name))

			// Remove the update-needed annotation
			if err := a.removeBMHAnnotation(ctx, bmh, uc.AnnotationKey); err != nil {
				return true, err
			}

			// Only annotate in-progress if not already set
			if utils.GetConfigAnnotation(&node) == "" {
				if err := a.annotateNodeConfigInProgress(ctx, node.Name, uc.Reason); err != nil {
					a.Logger.ErrorContext(ctx, fmt.Sprintf("Failed to annotate %s update in progress", uc.LogLabel),
						slog.String("error", err.Error()))
					return true, err
				}
				a.Logger.InfoContext(ctx, fmt.Sprintf("BMH %s update initiated", uc.LogLabel),
					slog.String("BMH", bmh.Name))
			} else {
				a.Logger.InfoContext(ctx, "Skipping annotation; another config already in progress",
					slog.String("BMH", bmh.Name),
					slog.String("skipped", uc.Reason))
			}

			return true, nil
		}
	}
	return false, nil
}

func (a *Adaptor) handleBMHCompletion(ctx context.Context, nodelist *hwmgmtv1alpha1.NodeList) (bool, error) {

	a.Logger.InfoContext(ctx, "Checking for node with config in progress")
	node := utils.FindNodeInProgress(nodelist)
	if node == nil {
		return false, nil // No node is in config progress
	}

	// Get BMH associated with the node
	bmh, err := a.getBMHForNode(ctx, node)
	if err != nil {
		return false, fmt.Errorf("failed to get BMH for node %s: %w", node.Name, err)
	}

	// Check if BMH has transitioned to "Available"
	bmhAvailable := a.checkBMHStatus(ctx, bmh, metal3v1alpha1.StateAvailable)

	// If BMH is not available yet, update is still ongoing
	if !bmhAvailable {
		return true, nil
	}

	// Apply post-config updates and finalize the process
	if err := a.ApplyPostConfigUpdates(ctx, types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}, node); err != nil {
		return false, fmt.Errorf("failed to apply post config update on node %s: %w", node.Name, err)
	}

	return false, nil // update is now complete
}

func (a *Adaptor) checkForPendingUpdate(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) (bool, error) {
	// check if there are any pending work
	nodelist, err := utils.GetChildNodes(ctx, a.Logger, a.Client, nodepool)
	if err != nil {
		return false, fmt.Errorf("failed to get child nodes for Node Pool %s: %w", nodepool.Name, err)
	}

	// Process BMHs transitioning to "Preparing"
	updating, err := a.handleBMHsTransitioningToPreparing(ctx, nodelist)
	if err != nil {
		return updating, err
	}

	if updating {
		a.Logger.InfoContext(ctx, "Skipping handleBMHCompletion as update is in progress")
		return true, nil
	}

	// Check if configuration is completed
	updating, err = a.handleBMHCompletion(ctx, nodelist)
	if err != nil {
		return updating, err
	}

	return updating, nil
}

func (a *Adaptor) getBMHForNode(ctx context.Context, node *hwmgmtv1alpha1.Node) (*metal3v1alpha1.BareMetalHost, error) {
	bmhName := node.Spec.HwMgrNodeId
	bmhNamespace := node.Labels[BmhNamespaceLabel]
	name := types.NamespacedName{Name: bmhName, Namespace: bmhNamespace}

	var bmh metal3v1alpha1.BareMetalHost
	if err := a.Client.Get(ctx, name, &bmh); err != nil {
		return nil, fmt.Errorf("unable to find BMH (%v): %w", name, err)
	}

	return &bmh, nil
}

func (a *Adaptor) addBMHAnnotation(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost, annotation string) error {
	bmhPatch := client.MergeFrom(bmh.DeepCopy()) // Create a patch
	if bmh.Annotations == nil {
		bmh.Annotations = make(map[string]string)
	}
	bmh.Annotations[annotation] = LabelValueTrue

	if err := a.Client.Patch(ctx, bmh, bmhPatch); err != nil {
		return fmt.Errorf("failed to add '%s' annotate to BMH %s: %w", annotation, bmh.Name, err)
	}

	a.Logger.InfoContext(ctx, "Added annotatation to BMH", slog.String("BMH", bmh.Name), slog.String("annotation", annotation))
	return nil
}

func (a *Adaptor) removeBMHAnnotation(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost, annotation string) error {
	bmhPatch := client.MergeFrom(bmh.DeepCopy())
	delete(bmh.Annotations, annotation)

	if err := a.Client.Patch(ctx, bmh, bmhPatch); err != nil {
		return fmt.Errorf("failed to remove '%s' annotation from BMH %s: %w", annotation, bmh.Name, err)
	}

	return nil
}

// markBMHAllocated sets the "allocated" label to "true" on a BareMetalHost.
func (a *Adaptor) markBMHAllocated(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost) error {
	// Check if the BMH is already allocated to avoid unnecessary patching
	if a.isBMHAllocated(bmh) {
		a.Logger.InfoContext(ctx, "BMH is already allocated, skipping update", slog.String("bmh", bmh.Name))
		return nil // No change needed
	}
	name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	return a.updateBMHLabelWithRetry(ctx, name, BmhAllocatedLabel, LabelValueTrue, LabelOpAdd)
}

// unmarkBMHAllocated removes the "allocated" label from a BareMetalHost if it exists.
func (a *Adaptor) unmarkBMHAllocated(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost) error {
	name := types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}
	return a.updateBMHLabelWithRetry(ctx, name, BmhAllocatedLabel, "", LabelOpRemove)
}

// removeMetal3Finalizer removes the Metal3 finalizer from the corresponding PreprovisioningImage resource.
// This is necessary because BMO will not remove the finalizer when the assisted-service is managing the resource.
func (a *Adaptor) removeMetal3Finalizer(ctx context.Context, bmhName, bmhNamespace string) error {
	name := types.NamespacedName{Name: bmhName, Namespace: bmhNamespace}

	// Retrieve the PreprovisioningImage resource
	image := &metal3v1alpha1.PreprovisioningImage{}
	if err := a.Client.Get(ctx, name, image); err != nil {
		return fmt.Errorf("unable to find PreprovisioningImage (%v): %w", name, err)
	}

	// Check if the Metal3 finalizer is present
	if !controllerutil.ContainsFinalizer(image, Metal3Finalizer) {
		return nil
	}

	controllerutil.RemoveFinalizer(image, Metal3Finalizer)
	if err := a.Client.Update(ctx, image); err != nil {
		return fmt.Errorf("failed to remove finalizer %s from PreprovisioningImage %s: %w",
			Metal3Finalizer, image.Name, err)
	}

	a.Logger.InfoContext(ctx, "Successfully removed Metal3 finalizer from PreprovisioningImage",
		slog.String("PreprovisioningImage", image.Name))
	return nil
}
