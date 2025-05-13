/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metal3

import (
	"fmt"
	"regexp"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	invserver "github.com/openshift-kni/oran-hwmgr-plugin/internal/server/api/generated"
)

const (
	LabelPrefixResources = "resources.oran.openshift.io/"
	LabelResourcePoolID  = LabelPrefixResources + "resourcePoolId"
	LabelSiteID          = LabelPrefixResources + "siteId"

	LabelPrefixResourceSelector = "resourceselector.oran.openshift.io/"

	LabelPrefixInterfaces = "interfacelabel.oran.openshift.io/"
)

// The following regex pattern is used to find interface labels
var REPatternInterfaceLabel = regexp.MustCompile(`^` + LabelPrefixInterfaces + `(.*)`)

// The following regex pattern is used to check resourceselector label pattern
var REPatternResourceSelectorLabel = regexp.MustCompile(`^` + LabelPrefixResourceSelector)

var REPatternResourceSelectorLabelMatch = regexp.MustCompile(`^` + LabelPrefixResourceSelector + `(.*)`)

var emptyString = ""

func getResourceInfoAdminState(bmh metal3v1alpha1.BareMetalHost) invserver.ResourceInfoAdminState {
	return invserver.ResourceInfoAdminStateUNKNOWN
}

func getResourceInfoDescription(bmh metal3v1alpha1.BareMetalHost) string {
	return emptyString
}

func getResourceInfoGlobalAssetId(bmh metal3v1alpha1.BareMetalHost) *string {
	return &emptyString
}

func getResourceInfoGroups(bmh metal3v1alpha1.BareMetalHost) *[]string {
	return nil
}

func getResourceInfoLabels(bmh metal3v1alpha1.BareMetalHost) *map[string]string { // nolint: gocritic
	if bmh.Labels != nil {
		labels := make(map[string]string)
		for label, value := range bmh.Labels {
			labels[label] = value
		}
		return &labels
	}

	return nil
}

func getResourceInfoMemory(bmh metal3v1alpha1.BareMetalHost) int {
	if bmh.Status.HardwareDetails != nil {
		return bmh.Status.HardwareDetails.RAMMebibytes
	}
	return 0
}

func getResourceInfoModel(bmh metal3v1alpha1.BareMetalHost) string {
	if bmh.Status.HardwareDetails != nil {
		return bmh.Status.HardwareDetails.SystemVendor.ProductName
	}
	return emptyString
}

func getResourceInfoName(bmh metal3v1alpha1.BareMetalHost) string {
	return bmh.Name
}

func getResourceInfoOperationalState(bmh metal3v1alpha1.BareMetalHost) invserver.ResourceInfoOperationalState {
	return invserver.ResourceInfoOperationalStateUNKNOWN
}

func getResourceInfoPartNumber(bmh metal3v1alpha1.BareMetalHost) string {
	return emptyString
}

func getResourceInfoPowerState(bmh metal3v1alpha1.BareMetalHost) *invserver.ResourceInfoPowerState {
	state := invserver.OFF
	if bmh.Status.PoweredOn {
		state = invserver.ON
	}

	return &state
}

func getProcessorInfoArchitecture(bmh metal3v1alpha1.BareMetalHost) *string {
	if bmh.Status.HardwareDetails != nil {
		return &bmh.Status.HardwareDetails.CPU.Arch
	}
	return &emptyString
}

func getProcessorInfoCores(bmh metal3v1alpha1.BareMetalHost) *int {
	if bmh.Status.HardwareDetails != nil {
		return &bmh.Status.HardwareDetails.CPU.Count
	}

	return nil
}

func getProcessorInfoManufacturer(bmh metal3v1alpha1.BareMetalHost) *string {
	return &emptyString
}

func getProcessorInfoModel(bmh metal3v1alpha1.BareMetalHost) *string {
	if bmh.Status.HardwareDetails != nil {
		return &bmh.Status.HardwareDetails.CPU.Model
	}
	return &emptyString
}

func getResourceInfoProcessors(bmh metal3v1alpha1.BareMetalHost) []invserver.ProcessorInfo {
	processors := []invserver.ProcessorInfo{}

	if bmh.Status.HardwareDetails != nil {
		processors = append(processors, invserver.ProcessorInfo{
			Architecture: getProcessorInfoArchitecture(bmh),
			Cores:        getProcessorInfoCores(bmh),
			Manufacturer: getProcessorInfoManufacturer(bmh),
			Model:        getProcessorInfoModel(bmh),
		})
	}
	return processors
}

func getResourceInfoResourceId(bmh metal3v1alpha1.BareMetalHost) string {
	return fmt.Sprintf("%s/%s", bmh.Namespace, bmh.Name)
}

func getResourceInfoResourcePoolId(bmh metal3v1alpha1.BareMetalHost) string {
	return bmh.Labels[LabelResourcePoolID]
}

func getResourceInfoResourceProfileId(bmh metal3v1alpha1.BareMetalHost) string {
	return bmh.Status.HardwareProfile
}

func getResourceInfoSerialNumber(bmh metal3v1alpha1.BareMetalHost) string {
	if bmh.Status.HardwareDetails != nil {
		return bmh.Status.HardwareDetails.SystemVendor.SerialNumber
	}
	return emptyString
}

func getResourceInfoTags(bmh metal3v1alpha1.BareMetalHost) *[]string {
	var tags []string

	for fullLabel, value := range bmh.Labels {
		match := REPatternResourceSelectorLabelMatch.FindStringSubmatch(fullLabel)
		if len(match) != 2 {
			continue
		}

		tags = append(tags, fmt.Sprintf("%s: %s", match[1], value))
	}

	return &tags
}

func getResourceInfoUsageState(bmh metal3v1alpha1.BareMetalHost) invserver.ResourceInfoUsageState {
	return invserver.UNKNOWN
}

func getResourceInfoVendor(bmh metal3v1alpha1.BareMetalHost) string {
	if bmh.Status.HardwareDetails != nil {
		return bmh.Status.HardwareDetails.SystemVendor.Manufacturer
	}
	return emptyString
}

func getResourceInfo(bmh metal3v1alpha1.BareMetalHost) invserver.ResourceInfo {
	return invserver.ResourceInfo{
		AdminState:       getResourceInfoAdminState(bmh),
		Description:      getResourceInfoDescription(bmh),
		GlobalAssetId:    getResourceInfoGlobalAssetId(bmh),
		Groups:           getResourceInfoGroups(bmh),
		HwProfile:        getResourceInfoResourceProfileId(bmh),
		Labels:           getResourceInfoLabels(bmh),
		Memory:           getResourceInfoMemory(bmh),
		Model:            getResourceInfoModel(bmh),
		Name:             getResourceInfoName(bmh),
		OperationalState: getResourceInfoOperationalState(bmh),
		PartNumber:       getResourceInfoPartNumber(bmh),
		PowerState:       getResourceInfoPowerState(bmh),
		Processors:       getResourceInfoProcessors(bmh),
		ResourceId:       getResourceInfoResourceId(bmh),
		ResourcePoolId:   getResourceInfoResourcePoolId(bmh),
		SerialNumber:     getResourceInfoSerialNumber(bmh),
		Tags:             getResourceInfoTags(bmh),
		UsageState:       getResourceInfoUsageState(bmh),
		Vendor:           getResourceInfoVendor(bmh),
	}
}

func includeInInventory(bmh metal3v1alpha1.BareMetalHost) bool {
	if bmh.Labels == nil || bmh.Labels[LabelResourcePoolID] == "" || bmh.Labels[LabelSiteID] == "" {
		// Ignore BMH CRs without the required labels
		return false
	}

	switch bmh.Status.Provisioning.State {
	case metal3v1alpha1.StateAvailable,
		metal3v1alpha1.StateProvisioning,
		metal3v1alpha1.StateProvisioned,
		metal3v1alpha1.StatePreparing:
		return true
	}
	return false
}
