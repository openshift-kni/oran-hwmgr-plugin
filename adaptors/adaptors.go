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

package adaptors

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	adaptorinterface "github.com/openshift-kni/oran-hwmgr-plugin/adaptors/adaptor-interface"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/logging"
	invserver "github.com/openshift-kni/oran-hwmgr-plugin/internal/server/api/generated"

	// Import the adaptors
	dellhwmgr "github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr"
	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/loopback"
)

// Supported adaptor IDs
const (
	LoopbackAdaptorID  = "loopback"
	DellHwMgrAdaptorID = "dell-hwmgr"
)

// HwMgrAdaptorController
type HwMgrAdaptorController struct {
	client.Client
	Scheme    *runtime.Scheme
	Logger    *slog.Logger
	Namespace string
	adaptors  map[string]adaptorinterface.HwMgrAdaptorIntf
}

func (c *HwMgrAdaptorController) SetupWithManager(mgr ctrl.Manager) error {
	// Setup the supported adaptors
	c.adaptors = make(map[string]adaptorinterface.HwMgrAdaptorIntf)
	c.adaptors[LoopbackAdaptorID] = loopback.NewAdaptor(c.Client, c.Scheme, c.Logger, c.Namespace)
	c.adaptors[DellHwMgrAdaptorID] = dellhwmgr.NewAdaptor(c.Client, c.Scheme, c.Logger, c.Namespace)

	for id, adaptor := range c.adaptors {
		if err := adaptor.SetupAdaptor(mgr); err != nil {
			c.Logger.Error("failed to setup adaptor", slog.String("id", id), slog.String("error", err.Error()))
		}
	}

	return nil
}

func (c *HwMgrAdaptorController) getHwMgr(ctx context.Context, hwMgrId string) (*pluginv1alpha1.HardwareManager, int, error) {
	name := types.NamespacedName{
		Name:      hwMgrId,
		Namespace: c.Namespace,
	}

	hwmgr := &pluginv1alpha1.HardwareManager{}
	if err := c.Client.Get(ctx, name, hwmgr); err != nil {
		return nil, http.StatusNotFound, fmt.Errorf("unable to find HardwareManager CR (%s): %w", hwMgrId, err)
	}

	// Validate that the required config data is present
	switch hwmgr.Spec.AdaptorID {
	case pluginv1alpha1.SupportedAdaptors.Loopback:
		if hwmgr.Spec.LoopbackData == nil {
			// Configuration data is not currently mandatory for the loopback adaptor, so just log it
			c.Logger.DebugContext(ctx, "config data missing from HardwareManager", slog.String("name", hwmgr.Name))
		}
	case pluginv1alpha1.SupportedAdaptors.Dell:
		if hwmgr.Spec.DellData == nil {
			return nil, http.StatusServiceUnavailable, fmt.Errorf("required config data missing from HardwareManager: name=%s", hwmgr.Name)
		}
	default:
		return nil, http.StatusServiceUnavailable, fmt.Errorf("unsupported adaptorId (%s) HardwareManager: name=%s", hwmgr.Spec.AdaptorID, hwmgr.Name)
	}

	return hwmgr, http.StatusOK, nil
}

// HandleNodePool calls the applicable adaptor handler to process the NodePool CR
func (c *HwMgrAdaptorController) HandleNodePool(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {
	ctx = logging.AppendCtx(ctx, slog.String("hwmgr", nodepool.Spec.HwMgrId))
	hwmgr, _, err := c.getHwMgr(ctx, nodepool.Spec.HwMgrId)
	if err != nil {
		c.Logger.ErrorContext(ctx, "failed to get adaptor instance", slog.String("error", err.Error()))

		if err := utils.UpdateNodePoolStatusCondition(ctx, c.Client, nodepool,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Failed, metav1.ConditionFalse,
			"Unable to find HardwareManager instance: "+nodepool.Spec.HwMgrId); err != nil {
			return utils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
		}

		return utils.DoNotRequeue(), nil
	}

	adaptorID := string(hwmgr.Spec.AdaptorID)

	// Validate the specified adaptor ID
	adaptor, exists := c.adaptors[adaptorID]
	if !exists {
		c.Logger.ErrorContext(ctx, "unsupported adaptor ID", slog.String("adaptorID", adaptorID))

		if err := utils.UpdateNodePoolStatusCondition(ctx, c.Client, nodepool,
			hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Failed, metav1.ConditionFalse,
			"Unsupported adaptor ID specified: "+adaptorID); err != nil {
			return utils.RequeueWithMediumInterval(),
				fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
		}

		return utils.DoNotRequeue(), nil
	}

	result, err := adaptor.HandleNodePool(ctx, hwmgr, nodepool)
	if err != nil {
		return result, fmt.Errorf("failed HandleNodePool for adaptorID %s: %w", adaptorID, err)
	}

	if !controllerutil.ContainsFinalizer(nodepool, utils.NodepoolFinalizer) {
		c.Logger.InfoContext(ctx, "Adding finalizer to NodePool")
		if err := utils.NodepoolAddFinalizer(ctx, c.Client, nodepool); err != nil {
			return utils.RequeueImmediately(), fmt.Errorf("failed to add finalizer to nodepool: %w", err)
		}
	}

	return result, nil
}

// HandleNodePool calls the applicable adaptor handler to process the NodePool CR deletion
func (c *HwMgrAdaptorController) HandleNodePoolDeletion(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) (bool, error) {
	hwmgr, _, err := c.getHwMgr(ctx, nodepool.Spec.HwMgrId)
	if err != nil {
		return false, fmt.Errorf("failed to get HardwareManager CR (%s): %w", nodepool.Spec.HwMgrId, err)
	}

	adaptorID := string(hwmgr.Spec.AdaptorID)

	// Validate the specified adaptor ID
	adaptor, exists := c.adaptors[adaptorID]
	if !exists {
		c.Logger.ErrorContext(ctx, "unsupported adaptor ID", slog.String("adaptorID", adaptorID))
		return true, nil
	}

	completed, err := adaptor.HandleNodePoolDeletion(ctx, hwmgr, nodepool)
	if err != nil {
		return false, fmt.Errorf("failed HandleNodePoolDeletion for adaptorID %s: %w", adaptorID, err)
	}

	return completed, nil
}

// HandleNodePool calls the applicable adaptor handler to process the NodePool CR deletion
func (c *HwMgrAdaptorController) GetResourcePools(ctx context.Context, request invserver.GetResourcePoolsRequestObject) (invserver.GetResourcePoolsResponseObject, error) {

	hwmgr, statusCode, err := c.getHwMgr(ctx, request.HwMgrId)
	if err != nil {
		if statusCode == http.StatusNotFound {
			return invserver.GetResourcePools404ApplicationProblemPlusJSONResponse(invserver.ProblemDetails{
				Status: statusCode,
				Detail: fmt.Sprintf("Hardware Manager %s not found", request.HwMgrId),
			}), fmt.Errorf("hardware manager %s not found: %w", request.HwMgrId, err)
		} else {
			return invserver.GetResourcePools503ApplicationProblemPlusJSONResponse(invserver.ProblemDetails{
				Status: statusCode,
				Detail: fmt.Sprintf("Hardware Manager %s unavailable: %s", request.HwMgrId, err.Error()),
			}), fmt.Errorf("unable to get hardware manager %s: %w", request.HwMgrId, err)
		}
	}

	adaptorID := string(hwmgr.Spec.AdaptorID)

	// Validate the specified adaptor ID
	adaptor, exists := c.adaptors[adaptorID]
	if !exists {
		// We should never get here, as the adaptor ID is validated in getHwMgr
		c.Logger.ErrorContext(ctx, "unsupported adaptor ID", slog.String("adaptorID", adaptorID))
		return invserver.GetResourcePools500ApplicationProblemPlusJSONResponse(invserver.ProblemDetails{
			Status: statusCode,
			Detail: fmt.Sprintf("Hardware Manager %s specifies invalid adaptorId: %s", request.HwMgrId, adaptorID),
		}), fmt.Errorf("hardware manager %s species invalid adaptorId: %s", request.HwMgrId, adaptorID)
	}

	resp, statusCode, err := adaptor.GetResourcePools(ctx, hwmgr)
	if err != nil {
		c.Logger.ErrorContext(ctx, "unable to get resource pools from hardware manager", slog.String("hwMgrId", request.HwMgrId), slog.String("error", err.Error()))
		return invserver.GetResourcePools500ApplicationProblemPlusJSONResponse(invserver.ProblemDetails{
			Status: statusCode,
			Detail: fmt.Sprintf("Resource Pool query failed for %s: %s", request.HwMgrId, err.Error()),
		}), fmt.Errorf("unable to query pools from hardware manager %s: %w", request.HwMgrId, err)
	}

	return invserver.GetResourcePools200JSONResponse(resp), nil
}

// HandleNodePool calls the applicable adaptor handler to process the NodePool CR deletion
func (c *HwMgrAdaptorController) GetResources(ctx context.Context, request invserver.GetResourcesRequestObject) (invserver.GetResourcesResponseObject, error) {

	hwmgr, statusCode, err := c.getHwMgr(ctx, request.HwMgrId)
	if err != nil {
		if statusCode == http.StatusNotFound {
			return invserver.GetResources404ApplicationProblemPlusJSONResponse(invserver.ProblemDetails{
				Status: statusCode,
				Detail: fmt.Sprintf("Hardware Manager %s not found", request.HwMgrId),
			}), fmt.Errorf("hardware manager %s not found: %w", request.HwMgrId, err)
		} else {
			return invserver.GetResources503ApplicationProblemPlusJSONResponse(invserver.ProblemDetails{
				Status: statusCode,
				Detail: fmt.Sprintf("Hardware Manager %s unavailable: %s", request.HwMgrId, err.Error()),
			}), fmt.Errorf("unable to get hardware manager %s: %w", request.HwMgrId, err)
		}
	}

	adaptorID := string(hwmgr.Spec.AdaptorID)

	// Validate the specified adaptor ID
	adaptor, exists := c.adaptors[adaptorID]
	if !exists {
		// We should never get here, as the adaptor ID is validated in getHwMgr
		c.Logger.ErrorContext(ctx, "unsupported adaptor ID", slog.String("adaptorID", adaptorID))
		return invserver.GetResources500ApplicationProblemPlusJSONResponse(invserver.ProblemDetails{
			Status: statusCode,
			Detail: fmt.Sprintf("Hardware Manager %s specifies invalid adaptorId: %s", request.HwMgrId, adaptorID),
		}), fmt.Errorf("hardware manager %s species invalid adaptorId: %s", request.HwMgrId, adaptorID)
	}

	resp, statusCode, err := adaptor.GetResources(ctx, hwmgr)
	if err != nil {
		c.Logger.ErrorContext(ctx, "unable to get resources from hardware manager", slog.String("hwMgrId", request.HwMgrId), slog.String("error", err.Error()))
		return invserver.GetResources500ApplicationProblemPlusJSONResponse(invserver.ProblemDetails{
			Status: statusCode,
			Detail: fmt.Sprintf("Resource query failed for %s: %s", request.HwMgrId, err.Error()),
		}), fmt.Errorf("unable to query resources from hardware manager %s: %w", request.HwMgrId, err)
	}

	return invserver.GetResources200JSONResponse(resp), nil
}
