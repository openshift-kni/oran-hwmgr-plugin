/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metal3

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	typederrors "github.com/openshift-kni/oran-hwmgr-plugin/internal/typed-errors"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

// AllocateBMH assigns a BareMetalHost to a NodePool.
func (a *Adaptor) allocateBMHToNodePool(ctx context.Context, bmh *metal3v1alpha1.BareMetalHost, nodepool *hwmgmtv1alpha1.NodePool, group hwmgmtv1alpha1.NodeGroup) error {
	nodeName := utils.GenerateNodeName()
	nodeId := bmh.Name
	nodeNs := bmh.Namespace
	cloudID := nodepool.Spec.CloudID // cluster name

	if err := a.CreateNode(ctx, nodepool, cloudID, nodeName, nodeId, nodeNs, group.NodePoolData.Name, group.NodePoolData.HwProfile); err != nil {
		return fmt.Errorf("failed to create allocated node (%s): %w", nodeName, err)
	}

	// Label the BMH
	if err := a.markBMHAllocated(ctx, bmh); err != nil {
		return fmt.Errorf("failed to add allocated label to node (%s): %w", nodeName, err)
	}

	nodepool.Status.Properties.NodeNames = append(nodepool.Status.Properties.NodeNames, nodeName)

	bmhInterface := a.buildInterfacesFromBMH(nodepool, *bmh)
	nodeInfo := bmhNodeInfo{
		ResourcePoolID: group.NodePoolData.ResourcePoolId,
		BMC: &bmhBmcInfo{
			Address:         bmh.Spec.BMC.Address,
			CredentialsName: bmh.Spec.BMC.CredentialsName,
		},
		Interfaces: bmhInterface,
	}
	updating, err := a.processHwProfile(ctx, bmh, nodeName, false)
	if err != nil {
		return err
	}
	a.Logger.InfoContext(ctx, "processed hw profile", slog.Bool("updating", updating))
	if err := a.UpdateNodeStatus(ctx, nodeInfo, nodeName, group.NodePoolData.HwProfile, updating); err != nil {
		return fmt.Errorf("failed to update node status (%s): %w", nodeName, err)
	}
	if !updating {
		if err := a.clearBMHNetworkData(ctx, types.NamespacedName{Name: bmh.Name, Namespace: bmh.Namespace}); err != nil {
			return fmt.Errorf("failed to clearBMHNetworkData bmh (%s/%s): %w", bmh.Name, bmh.Namespace, err)
		}
	}

	return nil
}

// ProcessNodePoolAllocation allocates BareMetalHosts to a NodePool while ensuring all BMHs are in the same namespace.
func (a *Adaptor) ProcessNodePoolAllocation(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allocationErr error

	// Get the BMH namespace from an already allocated node in this pool
	bmhNamespace, err := a.getNodePoolBMHNamespace(ctx, nodepool)
	if err != nil {
		return fmt.Errorf("unable to determine BMH namespace for pool %s: %w", nodepool.Name, err)
	}

	// Process allocation for each NodeGroup
	for _, nodeGroup := range nodepool.Spec.NodeGroup {
		if nodeGroup.Size == 0 {
			continue // Skip groups with size 0
		}

		// Retrieve only unallocated BMHs for the current site, resourcePoolId, and namespace
		unallocatedBMHs, err := a.FetchBMHList(ctx, nodepool.Spec.Site, nodeGroup.NodePoolData, UnallocatedBMHs, bmhNamespace)
		if err != nil {
			return fmt.Errorf("unable to fetch unallocated BMHs for site=%s, nodegroup=%s: %w",
				nodepool.Spec.Site, nodeGroup.NodePoolData.Name, err)
		}

		if len(unallocatedBMHs.Items) == 0 {
			return fmt.Errorf("no available nodes for site=%s, nodegroup=%s",
				nodepool.Spec.Site, nodeGroup.NodePoolData.Name)
		}

		// Calculate pending nodes for the group
		pendingNodes := nodeGroup.Size - a.countNodesInGroup(ctx, nodepool.Status.Properties.NodeNames, nodeGroup.NodePoolData.Name)
		if pendingNodes <= 0 {
			continue
		}

		// Shared counter to track remaining nodes needed
		nodeCounter := pendingNodes

		// Allocate multiple nodes concurrently within the group
		for _, bmh := range unallocatedBMHs.Items {
			mu.Lock()
			if nodeCounter <= 0 {
				mu.Unlock()
				break // Stop allocation if we've reached the required count
			}

			nodeCounter--
			mu.Unlock()

			wg.Add(1)
			go func(bmh *metal3v1alpha1.BareMetalHost) {
				defer wg.Done()

				// Allocate BMH to NodePool
				err := a.allocateBMHToNodePool(ctx, bmh, nodepool, nodeGroup)
				if err != nil {
					mu.Lock()
					if typederrors.IsInputError(err) {
						allocationErr = err
					} else {
						allocationErr = fmt.Errorf("failed to allocate BMH %s: %w", bmh.Name, err)
					}
					mu.Unlock()
				}
			}(&bmh)
		}
	}

	wg.Wait()

	// Check if any error occurred in goroutines
	if allocationErr != nil {
		return allocationErr
	}

	// Update node pool properties after all allocations are complete
	if err := utils.UpdateNodePoolProperties(ctx, a.Client, nodepool); err != nil {
		return fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
	}

	return nil
}

// getNodePoolBMHNamespace retrieves the namespace of an already allocated BMH in the given NodePool.
func (a *Adaptor) getNodePoolBMHNamespace(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) (string, error) {
	for _, nodeGroup := range nodepool.Spec.NodeGroup {
		if nodeGroup.Size == 0 {
			continue // Skip groups with size 0
		}

		// Fetch only allocated BMHs that match site and resourcePoolId
		bmhList, err := a.FetchBMHList(ctx, nodepool.Spec.Site, nodeGroup.NodePoolData, AllocatedBMHs, "")
		if err != nil {
			return "", fmt.Errorf("unable to fetch allocated BMHs for nodegroup=%s: %w", nodeGroup.NodePoolData.Name, err)
		}

		// Return the namespace of the first allocated BMH and stop searching
		if len(bmhList.Items) > 0 {
			return bmhList.Items[0].Namespace, nil
		}
	}

	return "", nil // No allocated BMH found, return empty namespace
}
