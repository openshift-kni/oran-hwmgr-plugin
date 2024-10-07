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

	adaptorinterface "github.com/openshift-kni/oran-hwmgr-plugin/adaptors/adaptor-interface"
	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// Import the adaptors
	dellhwmgr "github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr"
	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/loopback"
)

// TODO: Define supported adaptor IDs in oran-o2ims
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
	c.adaptors[LoopbackAdaptorID] = loopback.NewLoopbackAdaptor(c.Client, c.Scheme, c.Logger, c.Namespace)
	c.adaptors[DellHwMgrAdaptorID] = dellhwmgr.NewDellHwMgrAdaptor(c.Client, c.Scheme, c.Logger, c.Namespace)

	for id, adaptor := range c.adaptors {
		if err := adaptor.SetupAdaptor(mgr); err != nil {
			c.Logger.Error("failed to setup adaptor", "id", id, "error", err)
		}
	}

	return nil
}

func (c *HwMgrAdaptorController) getHwMgr(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) (*pluginv1alpha1.HardwareManager, error) {
	name := types.NamespacedName{
		Name:      nodepool.Spec.HwMgrId,
		Namespace: c.Namespace,
	}

	hwmgr := &pluginv1alpha1.HardwareManager{}
	if err := c.Client.Get(ctx, name, hwmgr); err != nil {
		return nil, fmt.Errorf("unable to find HardwareManager CR (%s): %w", nodepool.Spec.HwMgrId, err)
	}

	// Validate that the required config data is present
	switch hwmgr.Spec.AdaptorID {
	case pluginv1alpha1.SupportedAdaptors.Loopback:
		if hwmgr.Spec.LoopbackData == nil {
			return nil, fmt.Errorf("required config data missing from HardwareManager: name=%s", hwmgr.Name)
		}
	case pluginv1alpha1.SupportedAdaptors.Dell:
		if hwmgr.Spec.DellData == nil {
			return nil, fmt.Errorf("required config data missing from HardwareManager: name=%s", hwmgr.Name)
		}
	default:
		return nil, fmt.Errorf("unsupported adaptor-id (%s) HardwareManager: name=%s", hwmgr.Spec.AdaptorID, hwmgr.Name)
	}

	return hwmgr, nil
}

// HandleNodePool calls the applicable adaptor handler to process the NodePool CR
func (c *HwMgrAdaptorController) HandleNodePool(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {
	hwmgr, err := c.getHwMgr(ctx, nodepool)
	if err != nil {
		c.Logger.Error("failed to get adaptor instance",
			slog.String("hwMgrId", nodepool.Spec.HwMgrId),
			slog.String("error", err.Error()))

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
		c.Logger.Error("unsupported adaptor ID", "adaptorID", adaptorID)

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

	return result, nil
}

// HandleNodePool calls the applicable adaptor handler to process the NodePool CR deletion
func (c *HwMgrAdaptorController) HandleNodePoolDeletion(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) error {
	hwmgr, err := c.getHwMgr(ctx, nodepool)
	if err != nil {
		return fmt.Errorf("failed to get HardwareManager CR (%s): %w", nodepool.Spec.HwMgrId, err)
	}

	adaptorID := string(hwmgr.Spec.AdaptorID)

	// Validate the specified adaptor ID
	adaptor, exists := c.adaptors[adaptorID]
	if !exists {
		c.Logger.Error("unsupported adaptor ID", "adaptorID", adaptorID)
		return nil
	}

	if err := adaptor.HandleNodePoolDeletion(ctx, hwmgr, nodepool); err != nil {
		return fmt.Errorf("failed HandleNodePoolDeletion for adaptorID %s: %w", adaptorID, err)
	}

	return nil
}
