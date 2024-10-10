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

package loopback

import (
	"context"
	"fmt"
	"slices"

	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// Struct definitions for the nodelist configmap
type cmBmcInfo struct {
	Address        string `json:"address,omitempty"`
	UsernameBase64 string `json:"username-base64,omitempty"`
	PasswordBase64 string `json:"password-base64,omitempty"`
}

type cmNodeInfo struct {
	HwProfile  string                      `json:"hwprofile" yaml:"hwprofile"`
	BMC        *cmBmcInfo                  `json:"bmc,omitempty"`
	Interfaces []*hwmgmtv1alpha1.Interface `json:"interfaces,omitempty"`
	Hostname   string                      `json:"hostname,omitempty"`
}

type cmResources struct {
	HwProfiles []string              `json:"hwprofiles" yaml:"hwprofiles"`
	Nodes      map[string]cmNodeInfo `json:"nodes" yaml:"nodes"`
}

type cmAllocatedCloud struct {
	CloudID    string              `json:"cloudID" yaml:"cloudID"`
	Nodegroups map[string][]string `json:"nodegroups" yaml:"nodegroups"`
}

type cmAllocations struct {
	Clouds []cmAllocatedCloud `json:"clouds" yaml:"clouds"`
}

const (
	resourcesKey   = "resources"
	allocationsKey = "allocations"
	cmName         = "loopback-adaptor-nodelist"
)

// getFreeNodesInProfile compares the parsed configmap data to get the list of free nodes for a given hardware profile
func getFreeNodesInProfile(resources cmResources, allocations cmAllocations, profname string) (freenodes []string) {
	inuse := make(map[string]bool)
	for _, cloud := range allocations.Clouds {
		for groupname := range cloud.Nodegroups {
			for _, nodename := range cloud.Nodegroups[groupname] {
				inuse[nodename] = true
			}
		}
	}

	for nodename, node := range resources.Nodes {
		if node.HwProfile != profname {
			continue
		}

		if _, used := inuse[nodename]; !used {
			freenodes = append(freenodes, nodename)
		}
	}

	return
}

// GetCurrentResources parses the nodelist configmap to get the current available and allocated resource lists
func (a *LoopbackAdaptor) GetCurrentResources(ctx context.Context) (
	cm *corev1.ConfigMap, resources cmResources, allocations cmAllocations, err error) {
	cm, err = utils.GetConfigmap(ctx, a.Client, cmName, a.Namespace)
	if err != nil {
		err = fmt.Errorf("unable to get configmap: %w", err)
		return
	}

	resources, err = utils.ExtractDataFromConfigMap[cmResources](cm, resourcesKey)
	if err != nil {
		err = fmt.Errorf("unable to parse resources from configmap: %w", err)
		return
	}

	allocations, err = utils.ExtractDataFromConfigMap[cmAllocations](cm, allocationsKey)
	if err != nil {
		// Allocated node field may not be present
		a.Logger.InfoContext(ctx, "unable to parse allocations from configmap")
		err = nil
	}

	return
}

// GetAllocatedNodes gets a list of nodes allocated for the specified NodePool CR
func (a *LoopbackAdaptor) GetAllocatedNodes(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) (allocatedNodes []string, err error) {
	cloudID := nodepool.Spec.CloudID

	_, _, allocations, err := a.GetCurrentResources(ctx)
	if err != nil {
		err = fmt.Errorf("unable to get current resources: %w", err)
		return
	}

	var cloud *cmAllocatedCloud
	for i, iter := range allocations.Clouds {
		if iter.CloudID == cloudID {
			cloud = &allocations.Clouds[i]
			break
		}
	}
	if cloud == nil {
		// Cloud has not been allocated yet
		return
	}

	// Get allocated resources
	for _, nodegroup := range nodepool.Spec.NodeGroup {
		allocatedNodes = append(allocatedNodes, cloud.Nodegroups[nodegroup.Name]...)
	}

	slices.Sort(allocatedNodes)
	return
}
