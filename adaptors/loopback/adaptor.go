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
	"log/slog"

	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/loopback/controller"
	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Setup the Loopback Adaptor
type LoopbackAdaptor struct {
	client.Client
	Scheme    *runtime.Scheme
	Logger    *slog.Logger
	Namespace string
	AdaptorID pluginv1alpha1.HardwareManagerAdaptorID
}

func NewLoopbackAdaptor(client client.Client, scheme *runtime.Scheme, logger *slog.Logger, namespace string) *LoopbackAdaptor {
	return &LoopbackAdaptor{
		Client:    client,
		Scheme:    scheme,
		Logger:    logger.With("adaptor", "loopback"),
		Namespace: namespace,
	}
}

func (a *LoopbackAdaptor) SetupAdaptor(mgr ctrl.Manager) error {
	a.Logger.Info("SetupAdaptor called for Loopback")

	if err := (&controller.HardwareManagerReconciler{
		Client:    a.Client,
		Scheme:    a.Scheme,
		Logger:    a.Logger,
		Namespace: a.Namespace,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to setup loopback adaptor: %w", err)
	}

	return nil
}

// Loopback Adaptor FSM
type NodePoolFSMAction int

const (
	NodePoolFSMCreate = iota
	NodePoolFSMProcessing
	NodePoolFSMNoop
)

func (a *LoopbackAdaptor) determineAction(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) NodePoolFSMAction {
	if len(nodepool.Status.Conditions) == 0 {
		a.Logger.InfoContext(ctx, "Handling Create NodePool request, name="+nodepool.Name)
		return NodePoolFSMCreate
	}

	provisionedCondition := meta.FindStatusCondition(
		nodepool.Status.Conditions,
		string(hwmgmtv1alpha1.Provisioned))
	if provisionedCondition != nil {
		if provisionedCondition.Status == metav1.ConditionTrue {
			a.Logger.InfoContext(ctx, "NodePool request in Provisioned state, name="+nodepool.Name)
			return NodePoolFSMNoop
		}

		return NodePoolFSMProcessing
	}

	return NodePoolFSMNoop
}

func (a *LoopbackAdaptor) HandleNodePool(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {
	result := utils.DoNotRequeue()

	switch a.determineAction(ctx, nodepool) {
	case NodePoolFSMCreate:
		return a.HandleNodePoolCreate(ctx, hwmgr, nodepool)
	case NodePoolFSMProcessing:
		return a.HandleNodePoolProcessing(ctx, hwmgr, nodepool)
	case NodePoolFSMNoop:
		// Nothing to do
		return result, nil
	}

	return result, nil
}

func (a *LoopbackAdaptor) HandleNodePoolDeletion(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) error {
	a.Logger.InfoContext(ctx, "Finalizing nodepool", "name", nodepool.Name)

	if err := a.ReleaseNodePool(ctx, hwmgr, nodepool); err != nil {
		return fmt.Errorf("failed to release nodepool %s: %w", nodepool.Name, err)
	}

	return nil
}
