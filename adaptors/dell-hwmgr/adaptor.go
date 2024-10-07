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

package dellhwmgr

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/controller"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
)

// Setup the Loopback Adaptor
type DellHwMgrAdaptor struct {
	client.Client
	Scheme    *runtime.Scheme
	Logger    *slog.Logger
	Namespace string
	AdaptorID pluginv1alpha1.HardwareManagerAdaptorID
}

func NewDellHwMgrAdaptor(client client.Client, scheme *runtime.Scheme, logger *slog.Logger, namespace string) *DellHwMgrAdaptor {
	return &DellHwMgrAdaptor{
		Client:    client,
		Scheme:    scheme,
		Logger:    logger.With("adaptor", "dell-hwmgr"),
		Namespace: namespace,
	}
}

func (a *DellHwMgrAdaptor) SetupAdaptor(mgr ctrl.Manager) error {
	a.Logger.Info("SetupAdaptor called for DellHwMgr")

	if err := (&controller.HardwareManagerReconciler{
		Client:    a.Client,
		Scheme:    a.Scheme,
		Logger:    a.Logger,
		Namespace: a.Namespace,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to setup dell-hwmgr adaptor: %w", err)
	}

	return nil
}

func (a *DellHwMgrAdaptor) HandleNodePool(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {
	result := utils.DoNotRequeue()

	a.Logger.Error("DellHwMgr is not yet implemented")
	if err := utils.UpdateNodePoolStatusCondition(ctx, a.Client, nodepool,
		hwmgmtv1alpha1.Provisioned, hwmgmtv1alpha1.Failed, metav1.ConditionFalse,
		"Unsupported hwmgr adaptor: dell-hwmgr is not yet implemented"); err != nil {
		return utils.RequeueWithMediumInterval(),
			fmt.Errorf("failed to update status for NodePool %s: %w", nodepool.Name, err)
	}

	return result, nil
}

func (a *DellHwMgrAdaptor) HandleNodePoolDeletion(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) error {
	a.Logger.InfoContext(ctx, "DellHwMgr HandleNodePoolDeletion", "name", nodepool.Name)

	return nil
}
