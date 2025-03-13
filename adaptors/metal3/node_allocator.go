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
func (a *Adaptor) allocateBMHToNodePool(ctx context.Context, bmh metal3v1alpha1.BareMetalHost, nodepool *hwmgmtv1alpha1.NodePool, group hwmgmtv1alpha1.NodeGroup) error {
	nodeName := utils.GenerateNodeName()
	nodeId := bmh.Name
	nodeNs := bmh.Namespace
	cloudID := nodepool.Spec.CloudID // cluster name

	if err := a.CreateNode(ctx, nodepool, cloudID, nodeName, nodeId, nodeNs, group.NodePoolData.Name, group.NodePoolData.HwProfile); err != nil {
		return fmt.Errorf("failed to create allocated node (%s): %w", nodeName, err)
	}
	nodepool.Status.Properties.NodeNames = append(nodepool.Status.Properties.NodeNames, nodeName)

	bmhInteface := a.buildInterfacesFromBMH(bmh)
	nodeInfo := bmhNodeInfo{
		ResourcePoolID: group.NodePoolData.ResourcePoolId,
		BMC: &bmhBmcInfo{
			Address:         bmh.Spec.BMC.Address,
			CredentialsName: bmh.Spec.BMC.CredentialsName,
		},
		Interfaces: bmhInteface,
	}
	updating, err := a.processHwProfile(ctx, bmh, nodeName)
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

// ProcessNodePoolAllocation allocates BareMetalHosts to a NodePool using parallel processing.
func (a *Adaptor) ProcessNodePoolAllocation(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) error {
	// Fetch BMH list for the NodePool's site
	bmhList, err := a.FetchBMHList(ctx, nodepool.Spec.Site)
	if err != nil {
		return fmt.Errorf("unable to fetch BMHs for site %s: %w", nodepool.Spec.Site, err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var allocationErr error

	for _, nodeGroup := range nodepool.Spec.NodeGroup {
		if nodeGroup.Size == 0 {
			continue // Skip groups with size 0
		}

		// Filter BMHs for the current resourcePoolId
		filteredBMHList := a.FilterBMHList(ctx, &bmhList, nodeGroup.NodePoolData.ResourcePoolId)

		// Calculate pending nodes for the group
		pendingNodes := nodeGroup.Size - a.countNodesInGroup(ctx, nodepool.Status.Properties.NodeNames, nodeGroup.NodePoolData.Name)
		if pendingNodes <= 0 {
			continue
		}

		// Shared counter to track remaining nodes needed
		nodeCounter := pendingNodes

		// Allocate multiple nodes concurrently within the group
		for _, bmh := range filteredBMHList.Items {
			mu.Lock()
			if nodeCounter <= 0 {
				mu.Unlock()
				break // Stop allocation if we've reached the required count
			}
			nodeCounter--
			mu.Unlock()

			wg.Add(1)
			go func(bmh metal3v1alpha1.BareMetalHost) {
				defer wg.Done()

				allocated, err := a.isBMHAllocated(ctx, bmh)
				if err != nil {
					mu.Lock()
					allocationErr = fmt.Errorf("unable to check if BMH is allocated %s/%s: %w", bmh.Namespace, bmh.Name, err)
					mu.Unlock()
					return
				}

				if !allocated {
					err := a.allocateBMHToNodePool(ctx, bmh, nodepool, nodeGroup)
					if err != nil {
						mu.Lock()
						if typederrors.IsInputError(err) {
							allocationErr = err
						} else {
							allocationErr = fmt.Errorf("failed to allocate BMH %s: %w", bmh.Name, err)
						}
						mu.Unlock()
						return
					}
				}
			}(bmh)
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
