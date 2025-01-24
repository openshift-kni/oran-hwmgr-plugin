/*
Copyright 2025.

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
	hwmgrapi "github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/generated"
	invserver "github.com/openshift-kni/oran-hwmgr-plugin/internal/server/api/generated"
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
