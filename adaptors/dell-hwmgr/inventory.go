/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package dellhwmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	hwmgrapi "github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/generated"
	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/hwmgrclient"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	invserver "github.com/openshift-kni/oran-hwmgr-plugin/internal/server/api/generated"
	typederrors "github.com/openshift-kni/oran-hwmgr-plugin/internal/typed-errors"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"github.com/samber/lo"
)

func getResourceInfoAdminState(resource hwmgrapi.ApiprotoResource) invserver.ResourceInfoAdminState {
	if resource.AState == nil {
		return invserver.ResourceInfoAdminStateUNKNOWN
	}

	switch *resource.AState {
	case hwmgrapi.LOCKED:
		return invserver.ResourceInfoAdminStateLOCKED
	case hwmgrapi.SHUTTINGDOWN:
		return invserver.ResourceInfoAdminStateSHUTTINGDOWN
	case hwmgrapi.UNKNOWNADMINSTATE:
		return invserver.ResourceInfoAdminStateUNKNOWN
	case hwmgrapi.UNLOCKED:
		return invserver.ResourceInfoAdminStateUNLOCKED
	default:
		return invserver.ResourceInfoAdminStateUNKNOWN
	}
}

func getResourceInfoDescription(resource hwmgrapi.ApiprotoResource) string {
	if resource.Description == nil {
		return ""
	}
	return *resource.Description
}

func getResourceInfoGlobalAsserId(resource hwmgrapi.ApiprotoResource) *string {
	return resource.GlobalAssetId
}

func getResourceInfoGroups(resource hwmgrapi.ApiprotoResource) *[]string {
	if resource.Groups == nil {
		return nil
	}
	return resource.Groups.Group
}

func getResourceInfoLabels(resource hwmgrapi.ApiprotoResource) *map[string]string { // nolint: gocritic
	if resource.Labels != nil {
		labels := make(map[string]string)
		for _, label := range *resource.Labels {
			if label.Key == nil || label.Value == nil {
				continue
			}
			labels[*label.Key] = *label.Value
		}
		return &labels
	}

	return nil
}

func getResourceInfoMemory(server *hwmgrapi.ApiprotoServer) int {
	capacity := 0

	if server != nil && server.Status != nil && server.Status.Memory != nil {
		for _, mem := range *server.Status.Memory {
			capacity += int(*mem.CapacityMiB)
		}
	}
	return capacity
}

func getResourceInfoModel(server *hwmgrapi.ApiprotoServer) string {
	if server == nil || server.Status == nil || server.Status.Model == nil {
		return ""
	}
	return *server.Status.Model
}

func getResourceInfoName(resource hwmgrapi.ApiprotoResource) string {
	if resource.Name == nil {
		return ""
	}
	return *resource.Name
}

func getResourceInfoOperationalState(resource hwmgrapi.ApiprotoResource) invserver.ResourceInfoOperationalState {
	if resource.OpState == nil {
		return invserver.ResourceInfoOperationalStateUNKNOWN
	}

	switch *resource.OpState {
	case hwmgrapi.DISABLED:
		return invserver.ResourceInfoOperationalStateDISABLED
	case hwmgrapi.ENABLED:
		return invserver.ResourceInfoOperationalStateENABLED
	case hwmgrapi.UNKNOWNOPSTATE:
		return invserver.ResourceInfoOperationalStateUNKNOWN
	default:
		return invserver.ResourceInfoOperationalStateUNKNOWN
	}
}

func getResourceInfoPartNumber(server *hwmgrapi.ApiprotoServer) string {
	if server == nil || server.Status == nil || server.Status.PartNumber == nil {
		return ""
	}
	return *server.Status.PartNumber
}

func getResourceInfoPowerState(server *hwmgrapi.ApiprotoServer) *invserver.ResourceInfoPowerState {
	state := invserver.OFF
	if server != nil && server.Status != nil && server.Status.PowerState == nil && *server.Status.PowerState == "On" {
		state = invserver.ON
	}

	return &state
}

func getProcessorInfoArchitecture(processor hwmgrapi.ApiprotoProcessorSpec) *string {
	return processor.ProcessorArchitecture
}

func getProcessorInfoCores(processor hwmgrapi.ApiprotoProcessorSpec) *int {
	if processor.TotalCores == nil {
		return nil
	}

	cores := int(*processor.TotalCores)
	return &cores
}

func getProcessorInfoManufacturer(processor hwmgrapi.ApiprotoProcessorSpec) *string {
	return processor.Manufacturer
}

func getProcessorInfoModel(processor hwmgrapi.ApiprotoProcessorSpec) *string {
	return processor.Model
}

func getResourceInfoProcessors(server *hwmgrapi.ApiprotoServer) []invserver.ProcessorInfo {
	processors := []invserver.ProcessorInfo{}

	if server.Status != nil && server.Status.Processors != nil {
		for _, processor := range *server.Status.Processors {
			processors = append(processors, invserver.ProcessorInfo{
				Architecture: getProcessorInfoArchitecture(processor),
				Cores:        getProcessorInfoCores(processor),
				Manufacturer: getProcessorInfoManufacturer(processor),
				Model:        getProcessorInfoModel(processor),
			})
		}
	}
	return processors
}

func getResourceInfoResourceId(resource hwmgrapi.ApiprotoResource) string {
	if resource.Res == nil || resource.Res.Id == nil {
		return ""
	}
	return *resource.Res.Id
}

func getResourceInfoResourcePoolId(resource hwmgrapi.ApiprotoResource) string {
	if resource.ResourcePoolId == nil {
		return ""
	}
	return *resource.ResourcePoolId
}

func getResourceInfoResourceProfileId(resource hwmgrapi.ApiprotoResource) string {
	if resource.ResourceProfileID == nil {
		return ""
	}
	return *resource.ResourceProfileID
}

func getResourceInfoSerialNumber(server *hwmgrapi.ApiprotoServer) string {
	if server == nil || server.Status == nil || server.Status.SerialNumber == nil {
		return ""
	}
	return *server.Status.SerialNumber
}

func getResourceInfoTags(resource hwmgrapi.ApiprotoResource) *[]string {
	return resource.Tags
}

func getResourceInfoUsageState(resource hwmgrapi.ApiprotoResource) invserver.ResourceInfoUsageState {
	if resource.UState == nil {
		return invserver.UNKNOWN
	}

	switch *resource.UState {
	case hwmgrapi.ResourceUsageStateACTIVE:
		return invserver.ACTIVE
	case hwmgrapi.ResourceUsageStateBUSY:
		return invserver.BUSY
	case hwmgrapi.ResourceUsageStateIDLE:
		return invserver.IDLE
	default:
		return invserver.UNKNOWN
	}
}

func getResourceInfoVendor(server *hwmgrapi.ApiprotoServer) string {
	if server == nil || server.Status == nil || server.Status.Manufacturer == nil {
		return ""
	}
	return *server.Status.Manufacturer
}

func getResourceInfo(resource hwmgrapi.ApiprotoResource, server *hwmgrapi.ApiprotoServer) invserver.ResourceInfo {
	return invserver.ResourceInfo{
		AdminState:       getResourceInfoAdminState(resource),
		Description:      getResourceInfoDescription(resource),
		GlobalAssetId:    getResourceInfoGlobalAsserId(resource),
		Groups:           getResourceInfoGroups(resource),
		HwProfile:        getResourceInfoResourceProfileId(resource),
		Labels:           getResourceInfoLabels(resource),
		Memory:           getResourceInfoMemory(server),
		Model:            getResourceInfoModel(server),
		Name:             getResourceInfoName(resource),
		OperationalState: getResourceInfoOperationalState(resource),
		PartNumber:       getResourceInfoPartNumber(server),
		PowerState:       getResourceInfoPowerState(server),
		Processors:       getResourceInfoProcessors(server),
		ResourceId:       getResourceInfoResourceId(resource),
		ResourcePoolId:   getResourceInfoResourcePoolId(resource),
		SerialNumber:     getResourceInfoSerialNumber(server),
		Tags:             getResourceInfoTags(resource),
		UsageState:       getResourceInfoUsageState(resource),
		Vendor:           getResourceInfoVendor(server),
	}
}

func (a *Adaptor) FindAllocatedServers(ctx context.Context, hwmgrClient *hwmgrclient.HardwareManagerClient) ([]string, error) {
	allocatedServers := []string{}

	resourceGroups, err := hwmgrClient.GetResourceGroups(ctx)
	if err != nil {
		a.Logger.InfoContext(ctx, "GetResourceGroups error", slog.String("error", err.Error()))
		return allocatedServers, fmt.Errorf("unable to query resource groups: %w", err)
	}

	if resourceGroups.ResourceGroups == nil {
		a.Logger.InfoContext(ctx, "ResourceGroups returned from query is nil")
		return allocatedServers, nil
	}

	for _, iter := range *resourceGroups.ResourceGroups {
		if iter.Id == nil {
			continue
		}

		rg, err := hwmgrClient.GetResourceGroupFromId(ctx, *iter.Id)
		if err != nil {
			a.Logger.InfoContext(ctx, "Failed GetResourceGroup", slog.String("error", err.Error()))
			return allocatedServers, fmt.Errorf("unable to query resource group %s: %w", *iter.Id, err)
		}

		if rg.ResourceSelectors == nil {
			continue
		}

		for _, resourceSelector := range *rg.ResourceSelectors {
			for _, node := range *resourceSelector.Resources {
				if node.Id != nil {
					allocatedServers = append(allocatedServers, *node.Id)
				}
			}
		}
	}

	return allocatedServers, nil
}

// labelsMatch checks the set of labels for a given match
func labelsMatch(labels *[]hwmgrapi.ApiprotoLabel, key, value string) bool {
	if labels == nil {
		return false
	}

	for _, label := range *labels {
		if label.Key != nil && *label.Key == key && label.Value != nil && *label.Value == value {
			return true
		}
	}

	return false
}

func checkResourceSelectors(labels *[]hwmgrapi.ApiprotoLabel, resourceSelectors map[string]string) bool {
	for key, value := range resourceSelectors {
		if !labelsMatch(labels, key, value) {
			return false
		}
	}

	return true
}

func findFreeServersInPool(
	allocatedServers []string,
	resources *hwmgrapi.ApiprotoGetResourcesResp,
	resourceSelectors map[string]string,
	pool string) []string {
	freeServers := []string{}

	for _, resource := range *resources.Resources {
		if resource.ResourcePoolId == nil || *resource.ResourcePoolId != pool {
			// This is not the pool we're looking for
			continue
		}
		if lo.Contains(allocatedServers, *resource.Id) {
			// Server is already allocated
			continue
		}

		if !checkResourceSelectors(resource.Labels, resourceSelectors) {
			// Server doesn't match criteria
			continue
		}

		freeServers = append(freeServers, *resource.Id)
	}
	return freeServers
}

func findMatchingPool(
	pools *hwmgrapi.ApiprotoResourcePoolsResp,
	allocatedServers []string,
	resources *hwmgrapi.ApiprotoGetResourcesResp,
	resourceSelectors map[string]string,
	numServers int) string {

	for _, pool := range *pools.ResourcePools {
		freeServers := findFreeServersInPool(allocatedServers, resources, resourceSelectors, *pool.Id)
		if len(freeServers) >= numServers {
			return *pool.Id
		}
	}

	return ""
}

func poolExists(
	pools *hwmgrapi.ApiprotoResourcePoolsResp,
	pool string) bool {

	for _, iter := range *pools.ResourcePools {
		if iter.Id != nil && *iter.Id == pool {
			return true
		}
	}

	return false
}

// FindResourcePoolId checks the hardware manager inventory to find a pool with free resources that match the criteria
func (a *Adaptor) FindResourcePoolIds(
	ctx context.Context,
	hwmgrClient *hwmgrclient.HardwareManagerClient,
	nodepool *hwmgmtv1alpha1.NodePool) error {

	allocatedServers, err := a.FindAllocatedServers(ctx, hwmgrClient)
	if err != nil {
		a.Logger.InfoContext(ctx, "FindAllocatedServers error", slog.String("error", err.Error()))
		return typederrors.NewRetriableError(err, "unable to determine list of allocated servers")

	}

	pools, err := hwmgrClient.GetResourcePools(ctx)
	if err != nil {
		a.Logger.InfoContext(ctx, "GetResourcePools error", slog.String("error", err.Error()))
		return typederrors.NewRetriableError(err, "unable to query pools")
	}

	resources, err := hwmgrClient.GetResources(ctx)
	if err != nil {
		a.Logger.InfoContext(ctx, "GetResources error", slog.String("error", err.Error()))
		return typederrors.NewRetriableError(err, "unable to query resources")
	}

	if nodepool.Status.SelectedPools == nil {
		nodepool.Status.SelectedPools = make(map[string]string)
	}

	statusUpdated := false

	// For each nodeGroup, find the pool that has free resource to satisfy the request
	for _, nodegroup := range nodepool.Spec.NodeGroup {
		if nodepool.Status.SelectedPools[nodegroup.NodePoolData.Name] != "" {
			// The pool is already selected for this nodegroup
			continue
		}

		resourceSelectors := make(map[string]string)

		if nodegroup.NodePoolData.ResourceSelector != "" {
			if err := json.Unmarshal([]byte(nodegroup.NodePoolData.ResourceSelector), &resourceSelectors); err != nil {
				return typederrors.NewNonRetriableError(err, "unable to parse resourceSelector: %s", nodegroup.NodePoolData.ResourceSelector)
			}
		}

		if nodegroup.NodePoolData.ResourcePoolId != "" {
			// There's a pool specified in the nodegroup, so use it

			// Check whether the pool exists on hardware manager
			if !poolExists(pools, nodegroup.NodePoolData.ResourcePoolId) {
				return typederrors.NewNonRetriableError(nil, "pool specified in nodegroup does not exist on hardware manager, nodegroup: %s", nodegroup.NodePoolData.Name)
			}

			if nodegroup.Size > 0 {
				// Check whether there are free servers that match the specified criteria
				freeServers := findFreeServersInPool(allocatedServers, resources, resourceSelectors, nodegroup.NodePoolData.ResourcePoolId)
				if len(freeServers) < nodegroup.Size {
					return typederrors.NewNonRetriableError(err, "pool specified in node group does not have enough matching resources, nodegroup:%s", nodegroup.NodePoolData.Name)
				}
			}

			nodepool.Status.SelectedPools[nodegroup.NodePoolData.Name] = nodegroup.NodePoolData.ResourcePoolId
			a.Logger.InfoContext(ctx, "Setting pool from nodegroup", slog.String("pool", nodepool.Status.SelectedPools[nodegroup.NodePoolData.Name]))
		} else {
			matchingPool := findMatchingPool(pools, allocatedServers, resources, resourceSelectors, nodegroup.Size)
			if matchingPool == "" {
				return typederrors.NewNonRetriableError(nil, "unable to find pool matching criteria: resourceSelector: %s", nodegroup.NodePoolData.ResourceSelector)
			}

			nodepool.Status.SelectedPools[nodegroup.NodePoolData.Name] = matchingPool
			a.Logger.InfoContext(ctx, "Setting pool from analysis", slog.String("pool", nodepool.Status.SelectedPools[nodegroup.NodePoolData.Name]))
		}
		statusUpdated = true
	}

	if statusUpdated {
		if err := utils.UpdateNodePoolSelectedPools(ctx, a.Client, nodepool); err != nil {
			return typederrors.NewNonRetriableError(err, "failed to update status for NodePool %s", nodepool.Name)
		}
	}
	return nil
}
