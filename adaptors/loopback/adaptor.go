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
	"net/http"

	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/loopback/controller"
	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	invserver "github.com/openshift-kni/oran-hwmgr-plugin/internal/server/api/generated"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Adaptor struct {
	client.Client
	Scheme    *runtime.Scheme
	Logger    *slog.Logger
	Namespace string
	AdaptorID pluginv1alpha1.HardwareManagerAdaptorID
}

func NewAdaptor(client client.Client, scheme *runtime.Scheme, logger *slog.Logger, namespace string) *Adaptor {
	return &Adaptor{
		Client:    client,
		Scheme:    scheme,
		Logger:    logger.With("adaptor", "loopback"),
		Namespace: namespace,
	}
}

// SetupAdaptor sets up the Loopback adaptor
func (a *Adaptor) SetupAdaptor(mgr ctrl.Manager) error {
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
type fsmAction int

const (
	NodePoolFSMCreate = iota
	NodePoolFSMProcessing
	NodePoolFSMSpecChanged
	NodePoolFSMNoop
)

func (a *Adaptor) determineAction(ctx context.Context, nodepool *hwmgmtv1alpha1.NodePool) fsmAction {
	if len(nodepool.Status.Conditions) == 0 {
		a.Logger.InfoContext(ctx, "Handling Create NodePool request")
		return NodePoolFSMCreate
	}

	provisionedCondition := meta.FindStatusCondition(
		nodepool.Status.Conditions,
		string(hwmgmtv1alpha1.Provisioned))
	if provisionedCondition != nil {
		if provisionedCondition.Status == metav1.ConditionTrue {
			// Check if the generation has changed
			if nodepool.ObjectMeta.Generation != nodepool.Status.HwMgrPlugin.ObservedGeneration {
				a.Logger.InfoContext(ctx, "Handling NodePool Spec change")
				return NodePoolFSMSpecChanged
			}
			a.Logger.InfoContext(ctx, "NodePool request in Provisioned state")
			return NodePoolFSMNoop
		}

		return NodePoolFSMProcessing
	}

	return NodePoolFSMNoop
}

func (a *Adaptor) HandleNodePool(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {
	result := utils.DoNotRequeue()

	switch a.determineAction(ctx, nodepool) {
	case NodePoolFSMCreate:
		return a.HandleNodePoolCreate(ctx, hwmgr, nodepool)
	case NodePoolFSMProcessing:
		return a.HandleNodePoolProcessing(ctx, hwmgr, nodepool)
	case NodePoolFSMSpecChanged:
		return a.HandleNodePoolSpecChanged(ctx, hwmgr, nodepool)
	case NodePoolFSMNoop:
		// Nothing to do
		return result, nil
	}

	return result, nil
}

func (a *Adaptor) HandleNodePoolDeletion(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) error {
	a.Logger.InfoContext(ctx, "Finalizing nodepool")

	if err := a.ReleaseNodePool(ctx, hwmgr, nodepool); err != nil {
		return fmt.Errorf("failed to release nodepool %s: %w", nodepool.Name, err)
	}

	return nil
}

func (a *Adaptor) GetResourcePools(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager) ([]invserver.ResourcePoolInfo, int, error) {
	var resp []invserver.ResourcePoolInfo
	_, resources, _, err := a.GetCurrentResources(ctx)
	if err != nil {
		return resp, http.StatusServiceUnavailable, fmt.Errorf("unable to get current resources: %w", err)
	}

	siteId := "n/a"
	for _, pool := range resources.ResourcePools {
		resp = append(resp, invserver.ResourcePoolInfo{
			ResourcePoolId: pool,
			Description:    pool,
			Name:           pool,
			SiteId:         &siteId,
		})
	}

	return resp, http.StatusOK, nil
}

func (a *Adaptor) GetResources(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager) ([]invserver.ResourceInfo, int, error) {
	var resp []invserver.ResourceInfo

	_, resources, _, err := a.GetCurrentResources(ctx)
	if err != nil {
		return resp, http.StatusServiceUnavailable, fmt.Errorf("unable to get current resources: %w", err)
	}

	notavailable := "n/a" // Some data isn't available from dtias
	for name, server := range resources.Nodes {
		resp = append(resp, invserver.ResourceInfo{
			ResourcePoolId: server.ResourcePoolID,
			Description:    notavailable,
			Name:           name,
		})
	}
	return resp, http.StatusOK, nil
}
