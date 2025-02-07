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

package hwmgrclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	typederrors "github.com/openshift-kni/oran-hwmgr-plugin/internal/typed-errors"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/securityprovider"
	hwmgrapi "github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/generated"
	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	RoleKey       = "role"
	DefaultTenant = "default_tenant"
)

type JobStatus int

const (
	JobStatusInProgress = iota
	JobStatusCompleted
	JobStatusFailed
	JobStatusUnknown
)

// HardwareManagerClient provides functions for calling the hardware manager APIs
type HardwareManagerClient struct {
	rtclient    client.Client
	HwmgrClient *hwmgrapi.ClientWithResponses
	Logger      *slog.Logger
	Namespace   string
	hwmgr       *pluginv1alpha1.HardwareManager
}

// GetTenant gets the tenant parameter from the hwmgr configuration
func (c *HardwareManagerClient) GetTenant() string {
	if c.hwmgr.Spec.DellData.Tenant != nil && *c.hwmgr.Spec.DellData.Tenant != "" {
		return *c.hwmgr.Spec.DellData.Tenant
	}

	return DefaultTenant
}

// GetToken sends a request to the hardware manager to request an authentication token
func (c *HardwareManagerClient) GetToken(ctx context.Context) (string, error) {
	clientSecrets, err := utils.GetSecret(ctx, c.rtclient, c.hwmgr.Spec.DellData.AuthSecret, c.Namespace)
	if err != nil {
		return "", fmt.Errorf("failed to get client secret: %w", err)
	}

	clientId, err := utils.GetSecretField(clientSecrets, "client-id")
	if err != nil {
		return "", fmt.Errorf("failed to get client-id from secret: %s, %w", c.hwmgr.Spec.DellData.AuthSecret, err)
	}

	username, err := utils.GetSecretField(clientSecrets, corev1.BasicAuthUsernameKey)
	if err != nil {
		return "", fmt.Errorf("failed to get %s from secret: %s, %w", corev1.BasicAuthUsernameKey, c.hwmgr.Spec.DellData.AuthSecret, err)
	}

	password, err := utils.GetSecretField(clientSecrets, corev1.BasicAuthPasswordKey)
	if err != nil {
		return "", fmt.Errorf("failed to get %s from secret: %s, %w", corev1.BasicAuthPasswordKey, c.hwmgr.Spec.DellData.AuthSecret, err)
	}

	grant_type := string(pluginv1alpha1.OAuthGrantTypes.Password)

	req := hwmgrapi.GetTokenJSONRequestBody{
		ClientId:  &clientId,
		Username:  &username,
		Password:  &password,
		GrantType: &grant_type,
	}

	tokenrsp, err := c.HwmgrClient.GetTokenWithResponse(ctx, req)
	if err != nil {
		return "", typederrors.NewTokenError(err, "failed to get token: response: %v", tokenrsp)
	}

	if tokenrsp.StatusCode() != http.StatusOK {
		return "", typederrors.NewTokenError(nil, "token request failed with status %s (%d), message=%s",
			tokenrsp.Status(), tokenrsp.StatusCode(), string(tokenrsp.Body))
	}

	var tokenData hwmgrapi.RhprotoGetTokenResponseBody
	if err := json.Unmarshal(tokenrsp.Body, &tokenData); err != nil {
		return "", typederrors.NewTokenError(err, "failed to parse token: response: %v", tokenrsp)
	}

	if tokenData.AccessToken == nil {
		return "", typederrors.NewTokenError(nil, "failed to get token: access_token field empty: %v", tokenrsp)
	}
	return *tokenData.AccessToken, nil
}

// NewClientWithResponses creates an authenticated client connected to the hardware manager
func NewClientWithResponses(
	ctx context.Context,
	logger *slog.Logger,
	rtclient client.Client,
	hwmgr *pluginv1alpha1.HardwareManager) (*HardwareManagerClient, error) {

	hwmgrClient := HardwareManagerClient{
		rtclient:  rtclient,
		Logger:    logger,
		Namespace: hwmgr.Namespace,
		hwmgr:     hwmgr,
	}

	// If the HardwareManager CR includes certificates, get the bundle to add to the client
	var caBundle string
	if hwmgr.Spec.DellData.CaBundleName != nil {
		cm, err := utils.GetConfigmap(ctx, rtclient, *hwmgr.Spec.DellData.CaBundleName, hwmgr.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get configmap: %w", err)
		}

		caBundle, err = utils.GetConfigMapField(cm, "ca-bundle.pem")
		if err != nil {
			return nil, fmt.Errorf("failed to get certificate bundle from configmap: %w", err)
		}
	}

	config := utils.OAuthClientConfig{
		CaBundle: []byte(caBundle),
	}

	tr, err := utils.GetTransportWithCaBundle(config, hwmgr.Spec.DellData.InsecureSkipTLSVerify, utils.IsHardwareManagerLogMessagesEnabled(hwmgr))
	if err != nil {
		return nil, fmt.Errorf("failed to get http transport: %w", err)
	}

	httpClient := &http.Client{Transport: tr}

	// Create the hwmgrapi client, along with a bearer token
	hwmgrClient.HwmgrClient, err = hwmgrapi.NewClientWithResponses(
		hwmgr.Spec.DellData.ApiUrl,
		hwmgrapi.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to setup client to %s: %w", hwmgr.Spec.DellData.ApiUrl, err)
	}

	token, err := hwmgrClient.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token for %s: %w", hwmgr.Name, err)
	}

	bearerAuth, err := securityprovider.NewSecurityProviderBearerToken(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create security provider for %s: %w", hwmgr.Name, err)
	}

	// Create a new client with an intercept to add the bearer token
	hwmgrClient.HwmgrClient, err = hwmgrapi.NewClientWithResponses(
		hwmgr.Spec.DellData.ApiUrl,
		hwmgrapi.WithHTTPClient(httpClient),
		hwmgrapi.WithRequestEditorFn(bearerAuth.Intercept))
	if err != nil {
		return nil, fmt.Errorf("failed to setup auth client for %s: %w", hwmgr.Name, err)
	}

	return &hwmgrClient, nil
}

// GetResourceGroupFromNodePool queries the hardware manager to get the resource group data
func (c *HardwareManagerClient) GetResourceGroupFromNodePool(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) (*hwmgrapi.RhprotoResourceGroupObjectGetResponseBody, error) {
	rg := c.ResourceGroupFromNodePool(ctx, nodepool)
	rgId := *rg.ResourceGroup.Id
	tenant := c.GetTenant()

	response, err := c.HwmgrClient.GetResourceGroupWithResponse(ctx, tenant, rgId)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource group %s: response: %v, err: %w", rgId, response, err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("resource group get failed with status %s (%d), message=%s",
			response.Status(), response.StatusCode(), string(response.Body))
	}

	return response.JSON200, nil
}

// GetResourceGroup queries the hardware manager to get the resource group data
func (c *HardwareManagerClient) GetResourceGroupFromId(ctx context.Context, rgId string) (*hwmgrapi.RhprotoResourceGroupObjectGetResponseBody, error) {
	tenant := c.GetTenant()

	response, err := c.HwmgrClient.GetResourceGroupWithResponse(ctx, tenant, rgId)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource group %s: response: %v, err: %w", rgId, response, err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("resource group get failed with status %s (%d), message=%s",
			response.Status(), response.StatusCode(), string(response.Body))
	}

	return response.JSON200, nil
}

// GetResourceGroup queries the hardware manager to get the resource group data
func (c *HardwareManagerClient) GetResourceGroups(ctx context.Context) (*hwmgrapi.RhprotoResourceGroupsResp, error) {
	tenant := c.GetTenant()

	params := hwmgrapi.GetResourceGroupsParams{}
	response, err := c.HwmgrClient.GetResourceGroupsWithResponse(ctx, tenant, &params)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource groups: response: %v, err: %w", response, err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("resource groups get failed with status %s (%d), message=%s",
			response.Status(), response.StatusCode(), string(response.Body))
	}

	return response.JSON200, nil
}

// ResourceGroupIdFromNodePool returns the resource group identifier corresponding to the specified nodepool
func ResourceGroupIdFromNodePool(nodepool *hwmgmtv1alpha1.NodePool) string {
	return fmt.Sprintf("rhplugin-rg-%s", nodepool.Spec.CloudID)
}

// ResourceGroupFromNodePool transforms data from a nodepool object to a CreateResourceGroupJSONRequestBody instance
func (c *HardwareManagerClient) ResourceGroupFromNodePool(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) *hwmgrapi.CreateResourceGroupJSONRequestBody {
	rgId := ResourceGroupIdFromNodePool(nodepool)
	tenant := c.GetTenant()
	resourceTypeId := utils.GetResourceTypeId(nodepool)
	description := "Resource Group managed by O-Cloud Hardware Manager Plugin"
	excludes := make(map[string]interface{})
	roleKey := RoleKey

	resourceSelectors := make(map[string]hwmgrapi.RhprotoResourceSelectorRequest)
	for _, nodegroup := range nodepool.Spec.NodeGroup {
		inclusions := []hwmgrapi.RhprotoResourceSelectorFilterIncludeLabel{
			{
				Key:   &roleKey,
				Value: &nodegroup.NodePoolData.Name, // TODO: This should be nodegroup.NodePoolData.Role, but has to be nodegroup.NodePoolData.Name for now
			},
		}
		if nodegroup.NodePoolData.ResourceSelector != "" {
			selectors := make(map[string]string)
			if err := json.Unmarshal([]byte(nodegroup.NodePoolData.ResourceSelector), &selectors); err != nil {
				c.Logger.InfoContext(ctx, "Unable to parse resourceSelector", slog.String("resourceSelector", nodegroup.NodePoolData.ResourceSelector))
			} else {
				for key, value := range selectors {
					inclusions = append(inclusions, hwmgrapi.RhprotoResourceSelectorFilterIncludeLabel{Key: &key, Value: &value})
				}
			}
		}

		rpId := nodepool.Status.SelectedPools[nodegroup.NodePoolData.Name]
		resourceSelectors[nodegroup.NodePoolData.Name] = hwmgrapi.RhprotoResourceSelectorRequest{
			RpId:              &rpId,
			ResourceProfileId: &nodegroup.NodePoolData.HwProfile,
			NumResources:      &nodegroup.Size,
			Filters: &hwmgrapi.RhprotoResourceSelectorFilter{
				Include: &hwmgrapi.RhprotoResourceSelectorFilterInclude{
					Labels: &inclusions,
				},
				Exclude: &excludes,
			},
		}
	}

	// Currently, the hardware manager requires having a "worker" resource selector, even if the number of servers requested is zero.
	// To avoid needing to configure it in the NodePool CR, automatically add it if not already present.
	controller := "controller"
	worker := "worker"
	if _, exists := resourceSelectors[worker]; !exists {
		// Copy the data from the "controller" selector
		if controllerSelector, exists := resourceSelectors[controller]; exists {
			inclusions := []hwmgrapi.RhprotoResourceSelectorFilterIncludeLabel{
				{
					Key:   &roleKey,
					Value: &worker,
				},
			}

			numResources := 0
			resourceSelectors[worker] = hwmgrapi.RhprotoResourceSelectorRequest{
				RpId:              controllerSelector.RpId,
				ResourceProfileId: controllerSelector.ResourceProfileId,
				NumResources:      &numResources,
				Filters: &hwmgrapi.RhprotoResourceSelectorFilter{
					Include: &hwmgrapi.RhprotoResourceSelectorFilterInclude{
						Labels: &inclusions,
					},
					Exclude: &excludes,
				},
			}

		}
	}

	rg := hwmgrapi.CreateResourceGroupJSONRequestBody{
		Tenant: &tenant,
		ResourceGroup: &hwmgrapi.RhprotoResourceGroupObjectRequest{
			Description:       &description,
			Id:                &rgId,
			Name:              &rgId,
			ResourceTypeId:    &resourceTypeId,
			ResourceSelectors: &resourceSelectors,
		},
	}

	return &rg
}

// CreateResourceGroup sends a request to the hardware manager, returns a jobId
// TODO: Improve error handling for different status codes
func (c *HardwareManagerClient) CreateResourceGroup(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) (string, error) {
	rg := c.ResourceGroupFromNodePool(ctx, nodepool)
	rgId := *rg.ResourceGroup.Id
	tenant := c.GetTenant()

	// First check whether the resource group already exists
	response, err := c.HwmgrClient.GetResourceGroupWithResponse(ctx, tenant, rgId)
	if err != nil {
		return "", fmt.Errorf("failed to query for resource group %s: response: %v, err: %w", rgId, response, err)
	}

	if response.StatusCode() == http.StatusOK {
		return "", fmt.Errorf("resource group %s already exists", rgId)
	}

	// Send a request to the hardware manager to create the resource group
	rgResponse, err := c.HwmgrClient.CreateResourceGroupWithResponse(ctx, tenant, *rg)
	if err != nil {
		return "", fmt.Errorf("failed to create resource group %s, api failure: response: %v, err: %w", rgId, response, err)
	}

	if rgResponse.StatusCode() != http.StatusOK {
		// TODO: Remove this log
		c.Logger.InfoContext(ctx, "Failure from CreateResourceGroupWithResponse", slog.String("message", *rgResponse.JSONDefault.Message), slog.Any("response", rgResponse.JSONDefault))
		return "", fmt.Errorf("failed to create resource group %s, bad status: %s, code: %d, response: %v", rgId, rgResponse.Status(), rgResponse.StatusCode(), rgResponse)
	}

	// Return the job ID for the request
	return *rgResponse.JSON200.Jobid, nil
}

// CheckJobStatus queries the hardware manager for the status of a job
func (c *HardwareManagerClient) CheckJobStatus(ctx context.Context, jobId string) (JobStatus, string, error) {
	failReason := ""
	tenant := c.GetTenant()
	response, err := c.HwmgrClient.VerifyRequestStatusWithResponse(ctx, tenant, jobId)
	if err != nil {
		return JobStatusUnknown, failReason, fmt.Errorf("failed to query for job status: id: %s, response: %v, err: %w", jobId, response, err)
	}

	if response.StatusCode() != http.StatusOK {
		return JobStatusUnknown, failReason, fmt.Errorf("job query failed for %s: %s", jobId, *response.JSONDefault.Message)
	}

	status := response.JSON200
	if status == nil || status.Brief == nil || status.Brief.Status == nil {
		c.Logger.InfoContext(ctx, "Job progress check missing data", slog.Any("status", status))
		return JobStatusUnknown, failReason, fmt.Errorf("job progress check missing data, jobId=%s: %w", jobId, err)
	}

	// Process the status response
	switch *status.Brief.Status {
	case "started":
		c.Logger.InfoContext(ctx, "Job has started")
		return JobStatusInProgress, failReason, nil
	case "pending":
		c.Logger.InfoContext(ctx, "Job is pending")
		return JobStatusInProgress, failReason, nil
	case "completed":
		c.Logger.InfoContext(ctx, "Job has completed")
	case "failed":
		if status.Brief.FailReason != nil {
			failReason = *status.Brief.FailReason
		} else {
			failReason = "unknown"
		}
		c.Logger.InfoContext(ctx, "Job has failed", slog.Any("status", status), slog.String("failReason", failReason))
		return JobStatusFailed, failReason, nil
	default:
		if status.Brief.FailReason != nil {
			failReason = *status.Brief.FailReason
		} else {
			failReason = "unknown"
		}
		c.Logger.InfoContext(ctx, "Job status is unknown", slog.Any("status", status), slog.String("failReason", failReason))
		return JobStatusUnknown, failReason, nil
	}

	return JobStatusCompleted, failReason, nil
}

// DeleteResourceGroup asks the hardware manager to delete the resource group associated with the specified nodepool
func (c *HardwareManagerClient) DeleteResourceGroup(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) (string, error) {
	rgId := ResourceGroupIdFromNodePool(nodepool)
	tenant := c.GetTenant()

	response, err := c.HwmgrClient.DeleteResourceGroupWithResponse(ctx, tenant, rgId)
	if err != nil || response.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("failed to delete resource group %s: response: %v, err: %w", rgId, response, err)
	}

	return *response.JSON200.Jobid, nil
}

// GetResourcePools queries the hardware manager to get the resource pool list
func (c *HardwareManagerClient) GetResourcePools(ctx context.Context) (*hwmgrapi.ApiprotoResourcePoolsResp, error) {
	tenant := c.GetTenant()
	body := hwmgrapi.GetResourcePoolsJSONRequestBody{}
	response, err := c.HwmgrClient.GetResourcePoolsWithResponse(ctx, tenant, body)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource pools: response: %v, err: %w", response, err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("resource pool get failed with status %s (%d), message=%s",
			response.Status(), response.StatusCode(), string(response.Body))
	}

	return response.JSON200, nil
}

// GetServersInventory queries the hardware manager to get the server inventory
func (c *HardwareManagerClient) GetServersInventory(ctx context.Context) (*hwmgrapi.ApiprotoGetServersInventoryResp, error) {
	tenant := c.GetTenant()
	params := hwmgrapi.GetServersInventoryParams{}
	response, err := c.HwmgrClient.GetServersInventoryWithResponse(ctx, tenant, &params)
	if err != nil {
		return nil, fmt.Errorf("failed to get servers inventory: response: %v, err: %w", response, err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("server inventory get failed with status %s (%d), message=%s",
			response.Status(), response.StatusCode(), string(response.Body))
	}

	return response.JSON200, nil
}

// GetResources queries the hardware manager to get the resources list
func (c *HardwareManagerClient) GetResources(ctx context.Context) (*hwmgrapi.ApiprotoGetResourcesResp, error) {
	tenant := c.GetTenant()
	body := hwmgrapi.GetResourcesJSONRequestBody{}
	response, err := c.HwmgrClient.GetResourcesWithResponse(ctx, tenant, body)
	if err != nil {
		return nil, fmt.Errorf("failed to get resources: response: %v, err: %w", response, err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("resources get failed with status %s (%d), message=%s",
			response.Status(), response.StatusCode(), string(response.Body))
	}

	return response.JSON200, nil
}

// GetSecret queries the hardware manager to get the Secret data
func (c *HardwareManagerClient) GetSecret(ctx context.Context, secretKey string) (*hwmgrapi.RhprotoGetSecretsResponseBody, error) {
	tenant := c.GetTenant()
	response, err := c.HwmgrClient.GetSecretsWithResponse(ctx, tenant, secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s: response: %v, err: %w", secretKey, response, err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("get secret failed with status %s (%d), message=%s",
			response.Status(), response.StatusCode(), string(response.Body))
	}

	return response.JSON200, nil
}

// ValidateResourceGroup validates the hardware manager resource group data with nodepool
func (c *HardwareManagerClient) ValidateResourceGroup(
	ctx context.Context,
	nodepool *hwmgmtv1alpha1.NodePool,
	resourceGroup hwmgrapi.RhprotoResourceGroupObjectGetResponseBody,
) error {
	if resourceGroup.ResourceSelectors != nil && *resourceGroup.ResourceSelectors != nil {
		resourceSelector := *resourceGroup.ResourceSelectors
		for _, nodegroup := range nodepool.Spec.NodeGroup {
			nodegroupName := nodegroup.NodePoolData.Name
			if resource, exists := resourceSelector[nodegroupName]; exists {
				if resource.NumResources != nil {
					// Ensure expected number of nodes are present
					if float32(nodegroup.Size) != *resource.NumResources {
						return fmt.Errorf("invalid num of resources for node %s\n expected: %f found: %f",
							nodegroupName, float32(nodegroup.Size), *resource.NumResources)
					}
				} else {
					return fmt.Errorf("missing num of resources for node %s\n expected: %f",
						nodegroupName, float32(nodegroup.Size))
				}
				rpId := nodepool.Status.SelectedPools[nodegroup.NodePoolData.Name]
				if resource.RpId != nil {
					// Ensure resource pool id match
					if rpId != *resource.RpId {
						return fmt.Errorf("invalid resource pool id for node %s\n expected: %s found: %s",
							nodegroupName, rpId, *resource.RpId)
					}
				} else {
					return fmt.Errorf("missing resource pool id for node %s\n expected: %s",
						nodegroupName, rpId)
				}
			} else {
				return fmt.Errorf("validation failed, %s node does not exist in resource group", nodegroupName)
			}
		}
		return nil
	} else {
		return fmt.Errorf("validation failed, resourceSelector missing in Resource Group")
	}
}

// GetResource queries the hardware manager to get the resource data
func (c *HardwareManagerClient) GetResource(ctx context.Context, node *hwmgmtv1alpha1.Node) (*hwmgrapi.ApiprotoGetResourceResp, error) {
	tenant := c.GetTenant()
	response, err := c.HwmgrClient.GetResourceWithResponse(ctx, tenant, node.Spec.HwMgrNodeId)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: response: %v, err: %w", response, err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("resource get failed with status %s (%d), message=%s",
			response.Status(), response.StatusCode(), string(response.Body))
	}

	return response.JSON200, nil
}

// UpdateResourceProfile sends a request to update the resource profile for a node
func (c *HardwareManagerClient) UpdateResourceProfile(ctx context.Context, node *hwmgmtv1alpha1.Node, newHwProfile string) (string, error) {
	tenant := c.GetTenant()

	op := "replace"
	path := "/Resource/ResourceProfileID"
	value := []map[string]interface{}{{"resourceProfileID": newHwProfile}}
	body := hwmgrapi.UpdateResourceJSONRequestBody{
		ResourceName: &node.Spec.HwMgrNodeId,
		Resource: &[]hwmgrapi.ApiprotoUpdateResource{
			{
				Op:    &op,
				Path:  &path,
				Value: &value,
			},
		},
	}
	response, err := c.HwmgrClient.UpdateResourceWithResponse(ctx, tenant, body)
	if err != nil {
		return "", fmt.Errorf("failed to get resource: response: %v, err: %w", response, err)
	}

	if response.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("resource get failed with status %s (%d), message=%s",
			response.Status(), response.StatusCode(), string(response.Body))
	}

	return *response.JSON200.Response.Jobid, nil
}
