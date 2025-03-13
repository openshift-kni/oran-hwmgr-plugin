/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package dellserver

import (
	"net/http"

	apiserver "github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/dell-hwmgr/dell-server/generated"
)

// These functions will be mocked on a test basis
var GetTokenFn http.HandlerFunc

// This struct implements the http interface provided by the server infra
type DellServer struct{}

func (s DellServer) GetToken(w http.ResponseWriter, r *http.Request) {
	GetTokenFn(w, r)
}

func (s DellServer) VerifyRequestStatus(w http.ResponseWriter, r *http.Request, tenant, jobid string) {
	// To be implemented
}

func (s DellServer) CreateResourceGroup(w http.ResponseWriter, r *http.Request, tenant string) {
	// To be implemented
}

func (s DellServer) DeleteResourceGroup(w http.ResponseWriter, r *http.Request, tenant, resourceGroupId string) {
	// To be implemented
}

func (s DellServer) GetResourceGroup(w http.ResponseWriter, r *http.Request, tenant, resourceGroupId string) {
	// To be implemented
}

func (s DellServer) GetResourceGroups(w http.ResponseWriter, r *http.Request, tenant string, params apiserver.GetResourceGroupsParams) {
	// To be implemented
}

func (s DellServer) CreateResourcePool(w http.ResponseWriter, r *http.Request, tenant string) {
	// To be implemented
}

func (s DellServer) DeleteResourcePool(w http.ResponseWriter, r *http.Request, tenant, resourcePoolId string, params apiserver.DeleteResourcePoolParams) {
	// To be implemented
}

func (s DellServer) UpdateResource(w http.ResponseWriter, r *http.Request, tenant string) {
	// To be implemented
}

func (s DellServer) CreateResource(w http.ResponseWriter, r *http.Request, tenant string) {
	// To be implemented
}

func (s DellServer) GetResourceDeployments(w http.ResponseWriter, r *http.Request, tenant, id string) {
	// To be implemented
}

func (s DellServer) DeleteResource(w http.ResponseWriter, r *http.Request, tenant, resourceId string, params apiserver.DeleteResourceParams) {
	// To be implemented
}

func (s DellServer) SubscribeResources(w http.ResponseWriter, r *http.Request, tenant string) {
	// To be implemented
}

func (s DellServer) UnsubscribeResources(w http.ResponseWriter, r *http.Request, tenant string) {
	// To be implemented
}

func (s DellServer) GetResourcePools(w http.ResponseWriter, r *http.Request, tenant string) {
	// To be implemented
}

func (s DellServer) GetResourcePool(w http.ResponseWriter, r *http.Request, tenant, id string) {
	// To be implemented
}

func (s DellServer) GetResources(w http.ResponseWriter, r *http.Request, tenant string) {
	// To be implemented
}

func (s DellServer) GetResource(w http.ResponseWriter, r *http.Request, tenant, id string) {
	// To be implemented
}

func (s DellServer) GetResourceSubscriptions(w http.ResponseWriter, r *http.Request, tenant string) {
	// To be implemented
}

func (s DellServer) GetResourceSubscription(w http.ResponseWriter, r *http.Request, tenant, id string) {
	// To be implemented
}

func (s DellServer) GetSecrets(w http.ResponseWriter, r *http.Request, tenant, secretKey string) {
	// To be implemented
}

func (s DellServer) GetLocationsInventory(w http.ResponseWriter, r *http.Request, tenant string, params apiserver.GetLocationsInventoryParams) {
	// To be implemented
}

func (s DellServer) GetLocationInventory(w http.ResponseWriter, r *http.Request, tenant, id string, params apiserver.GetLocationInventoryParams) {
	// To be implemented
}

func (s DellServer) GetResourcePoolsInventory(w http.ResponseWriter, r *http.Request, tenant string, params apiserver.GetResourcePoolsInventoryParams) {
	// To be implemented
}

func (s DellServer) GetResourcePoolInventory(w http.ResponseWriter, r *http.Request, tenant, id string, params apiserver.GetResourcePoolInventoryParams) {
	// To be implemented
}

func (s DellServer) GetResourcesInventory(w http.ResponseWriter, r *http.Request, tenant string, params apiserver.GetResourcesInventoryParams) {
	// To be implemented
}

func (s DellServer) GetResourceInventory(w http.ResponseWriter, r *http.Request, tenant, id string) {
	// To be implemented
}

func (s DellServer) GetInvRetentionPolicy(w http.ResponseWriter, r *http.Request, tenant string, params apiserver.GetInvRetentionPolicyParams) {
	// To be implemented
}

func (s DellServer) UpdateInvRetentionPolicy(w http.ResponseWriter, r *http.Request, tenant string) {
	// To be implemented
}

func (s DellServer) GetServersInventory(w http.ResponseWriter, r *http.Request, tenant string, params apiserver.GetServersInventoryParams) {
	// To be implemented
}

func (s DellServer) GetServerInventory(w http.ResponseWriter, r *http.Request, tenant, id string, params apiserver.GetServerInventoryParams) {
	// To be implemented
}

func (s DellServer) GetSitesInventory(w http.ResponseWriter, r *http.Request, tenant string, params apiserver.GetSitesInventoryParams) {
	// To be implemented
}

func (s DellServer) GetSiteInventory(w http.ResponseWriter, r *http.Request, tenant, id string, params apiserver.GetSiteInventoryParams) {
	// To be implemented
}
