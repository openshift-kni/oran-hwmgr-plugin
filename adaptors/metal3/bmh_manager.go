/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metal3

import (
	"context"
	"fmt"
	"strings"

	"log/slog"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	BIOS_UPDATE_NEEDED_ANNOTATION = "bios-update-needed"
	BMH_NAMESPACE_LABEL           = "baremetalhost.metal3.io/namespace"
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

// FetchBMHList retrieves a list of BareMetalHosts filtered by site ID.
func (a *Adaptor) FetchBMHList(ctx context.Context, site string) (metal3v1alpha1.BareMetalHostList, error) {
	var bmhList metal3v1alpha1.BareMetalHostList
	var opts []client.ListOption

	// Add label selector only if site is not empty
	if site != "" {
		opts = append(opts, client.MatchingLabels{"siteId": site})
	}

	if err := a.Client.List(ctx, &bmhList, opts...); err != nil {
		return bmhList, fmt.Errorf("failed to get bmh list: %w", err)
	}

	if len(bmhList.Items) == 0 {
		a.Logger.WarnContext(ctx, "No BareMetalHosts found for siteId", slog.String("siteId", site))
	}

	return bmhList, nil
}

// FilterBMHList filters BareMetalHosts by resource pool ID and availability.
func (a *Adaptor) FilterBMHList(ctx context.Context, bmhList *metal3v1alpha1.BareMetalHostList, resourcePoolID string) metal3v1alpha1.BareMetalHostList {
	var filtered metal3v1alpha1.BareMetalHostList
	for _, bmh := range bmhList.Items {
		if bmh.Status.Provisioning.State == metal3v1alpha1.StateAvailable {
			if poolID, exists := bmh.Labels["resourcePoolId"]; exists && poolID == resourcePoolID {
				filtered.Items = append(filtered.Items, bmh)
			}
		}
	}
	return filtered
}

// getUnallocatedBMHs returns a list of unallocated BMH
func (a *Adaptor) getUnallocatedBMHs(ctx context.Context, bmhList metal3v1alpha1.BareMetalHostList) ([]metal3v1alpha1.BareMetalHost, error) {
	var unallocatedBMHs []metal3v1alpha1.BareMetalHost

	for _, bmh := range bmhList.Items {
		allocated, err := a.isBMHAllocated(ctx, bmh)
		if err != nil {
			return nil, fmt.Errorf("error checking allocation status for BMH %s: %w", bmh.Name, err)
		}

		if !allocated {
			unallocatedBMHs = append(unallocatedBMHs, bmh)
		}
	}
	return unallocatedBMHs, nil
}

// GroupBMHsByResourcePool groups unallocated BMHs by resource pool ID.
func (a *Adaptor) GroupBMHsByResourcePool(unallocatedBMHs []metal3v1alpha1.BareMetalHost) map[string][]metal3v1alpha1.BareMetalHost {
	grouped := make(map[string][]metal3v1alpha1.BareMetalHost)
	for _, bmh := range unallocatedBMHs {
		if resourcePoolID, exists := bmh.Labels["resourcePoolId"]; exists {
			grouped[resourcePoolID] = append(grouped[resourcePoolID], bmh)
		}
	}
	return grouped
}

func (a *Adaptor) buildInterfacesFromBMH(bmh metal3v1alpha1.BareMetalHost) []*hwmgmtv1alpha1.Interface {
	var interfaces []*hwmgmtv1alpha1.Interface

	for _, nic := range bmh.Status.HardwareDetails.NIC {
		label := ""
		mac := nic.MAC
		if strings.EqualFold(nic.MAC, bmh.Spec.BootMACAddress) {
			label = "bootable-interface"
			// use the current BMH mac address format
			mac = bmh.Spec.BootMACAddress
		}

		interfaces = append(interfaces, &hwmgmtv1alpha1.Interface{
			Name:       nic.Name,
			MACAddress: mac,
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

func (a *Adaptor) isBMHAllocated(ctx context.Context, bmh metal3v1alpha1.BareMetalHost) (bool, error) {
	nodeList, err := a.GetNodeList(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to list nodes: %w", err)
	}

	for _, node := range nodeList.Items {
		// Could check the nodeId or BMC address
		// if node.Status.BMC.Address == bmh.Spec.BMC.Address {
		if node.Spec.HwMgrNodeId == bmh.Name {
			return true, nil
		}
	}
	return false, nil
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

func (a *Adaptor) processHwProfile(ctx context.Context, bmh metal3v1alpha1.BareMetalHost, nodeName string) (bool, error) {

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

	// If update is required, annotate BMH
	if updateRequired {
		if err := a.annotateBMHNeedsBiosUpdate(ctx, bmh); err != nil {
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

// annotateNodeBiosInProgress sets an annotation on the corresponding Node object to indicate BIOS config is in progress.
func (a *Adaptor) annotateNodeBiosInProgress(ctx context.Context, nodeName string, bmh metal3v1alpha1.BareMetalHost) error {

	// Fetch the Node object
	node := &hwmgmtv1alpha1.Node{}
	if err := a.Client.Get(ctx, types.NamespacedName{Name: nodeName, Namespace: a.Namespace}, node); err != nil {
		return fmt.Errorf("unable to get Node object (%s): %w", nodeName, err)
	}

	// Set annotation to indicate BIOS configuration is in progress
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	utils.SetBiosConfig(node, bmh.Name)

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

		// If the BMH needs BIOS update and is "Preparing", mark the node
		if bmh.Annotations["bios-update-needed"] == "true" {

			if bmh.Status.Provisioning.State != metal3v1alpha1.StatePreparing {
				a.Logger.InfoContext(ctx, "BMH is not in 'Preparing' state yet, requeuing", slog.String("BMH", bmh.Name))
				return true, nil // Requeue
			}

			a.Logger.InfoContext(ctx, "BMH transitioned to 'Preparing' state", slog.String("BMH", bmh.Name))

			// Remove "bios-update-needed" annotation
			if err := a.removeBMHAnnotation(ctx, bmh, BIOS_UPDATE_NEEDED_ANNOTATION); err != nil {
				return true, err
			}

			// Mark the node as BIOS update in progress
			if err := a.annotateNodeBiosInProgress(ctx, node.Name, *bmh); err != nil {
				a.Logger.ErrorContext(ctx, "Failed to annotate BIOS update in progress", slog.String("error", err.Error()))
				return true, err
			}

			a.Logger.InfoContext(ctx, "BMH BIOS update initiated", slog.String("BMH", bmh.Name))
			return true, nil // Indicates BIOS update is ongoing
		}
	}
	return false, nil
}

func (a *Adaptor) handleBMHCompletion(ctx context.Context, nodelist *hwmgmtv1alpha1.NodeList) (bool, error) {

	a.Logger.InfoContext(ctx, "Checking for node with BIOS config in progress")
	node := utils.FindNodeInProgress(nodelist)
	if node == nil {
		return false, nil // No node is in BIOS config progress
	}

	// Get BMH associated with the node
	bmh, err := a.getBMHForNode(ctx, node)
	if err != nil {
		return false, fmt.Errorf("failed to get BMH for node %s: %w", node.Name, err)
	}

	// Check if BMH has transitioned to "Available"
	bmhAvailable := a.checkBMHStatus(ctx, bmh, metal3v1alpha1.StateAvailable)

	// If BMH is not available yet, BIOS update is still ongoing
	if !bmhAvailable {
		return true, nil
	}

	// Apply post-config updates and finalize the process
	if err := a.ApplyPostConfigUpdates(ctx, types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}, node); err != nil {
		return false, fmt.Errorf("failed to apply post config update on node %s: %w", node.Name, err)
	}

	return false, nil // BIOS update is now complete
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
		a.Logger.InfoContext(ctx, "Skipping handleBMHCompletion as BIOS update is in progress")
		return true, nil
	}

	// Check if BIOS configuration is completed
	updating, err = a.handleBMHCompletion(ctx, nodelist)
	if err != nil {
		return updating, err
	}

	return updating, nil
}

func (a *Adaptor) getBMHForNode(ctx context.Context, node *hwmgmtv1alpha1.Node) (*metal3v1alpha1.BareMetalHost, error) {
	bmhName := node.Spec.HwMgrNodeId
	bmhNamespace := node.Labels[BMH_NAMESPACE_LABEL]
	name := types.NamespacedName{Name: bmhName, Namespace: bmhNamespace}

	var bmh metal3v1alpha1.BareMetalHost
	if err := a.Client.Get(ctx, name, &bmh); err != nil {
		return nil, fmt.Errorf("unable to find BMH (%v): %w", name, err)
	}

	return &bmh, nil
}

func (a *Adaptor) annotateBMHNeedsBiosUpdate(ctx context.Context, bmh metal3v1alpha1.BareMetalHost) error {
	bmhPatch := client.MergeFrom(bmh.DeepCopy()) // Create a patch
	if bmh.Annotations == nil {
		bmh.Annotations = make(map[string]string)
	}
	bmh.Annotations[BIOS_UPDATE_NEEDED_ANNOTATION] = "true"

	if err := a.Client.Patch(ctx, &bmh, bmhPatch); err != nil {
		return fmt.Errorf("failed to annotate BMH %s for BIOS update: %w", bmh.Name, err)
	}

	a.Logger.InfoContext(ctx, "Annotated BMH for BIOS update", slog.String("BMH", bmh.Name))
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
