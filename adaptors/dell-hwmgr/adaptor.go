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
	"net/http"

	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/controller"
	hwmgrapi "github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/generated"
	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/hwmgrclient"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	invserver "github.com/openshift-kni/oran-hwmgr-plugin/internal/server/api/generated"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
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
		Logger:    logger.With("adaptor", "dell-hwmgr"),
		Namespace: namespace,
	}
}

// SetupAdaptor sets up the Dell Hardware Manager Adaptor
func (a *Adaptor) SetupAdaptor(mgr ctrl.Manager) error {
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

		if provisionedCondition.Reason == string(hwmgmtv1alpha1.Failed) {
			a.Logger.InfoContext(ctx, "NodePool request in Failed state")
			return NodePoolFSMNoop
		}

		return NodePoolFSMProcessing
	}

	return NodePoolFSMNoop
}

func (a *Adaptor) HandleNodePool(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error) {
	result := utils.DoNotRequeue()

	hwmgrClient, clientErr := hwmgrclient.NewClientWithResponses(ctx, a.Logger, a.Client, hwmgr)
	if clientErr != nil {
		// TODO: Improve client error handling to distinguish between connectivity errors, auth, etc
		a.Logger.InfoContext(ctx, "NewClientWithResponses error", slog.String("error", clientErr.Error()))
		return result, fmt.Errorf("failed to setup hwmgr client: %w", clientErr)
	}

	switch a.determineAction(ctx, nodepool) {
	case NodePoolFSMCreate:
		return a.HandleNodePoolCreate(ctx, hwmgrClient, hwmgr, nodepool)
	case NodePoolFSMProcessing:
		return a.HandleNodePoolProcessing(ctx, hwmgrClient, hwmgr, nodepool)
	case NodePoolFSMSpecChanged:
		return a.HandleNodePoolSpecChanged(ctx, hwmgrClient, hwmgr, nodepool)
	case NodePoolFSMNoop:
		// Nothing to do
		return result, nil
	}

	return result, nil
}

func (a *Adaptor) HandleNodePoolDeletion(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) error {
	a.Logger.InfoContext(ctx, "Finalizing nodepool")

	hwmgrClient, clientErr := hwmgrclient.NewClientWithResponses(ctx, a.Logger, a.Client, hwmgr)
	if clientErr != nil {
		// TODO: Improve client error handling to distinguish between connectivity errors, auth, etc
		a.Logger.InfoContext(ctx, "NewClientWithResponses error", slog.String("error", clientErr.Error()))
		return fmt.Errorf("failed to setup hwmgr client: %w", clientErr)
	}

	if err := a.ReleaseNodePool(ctx, hwmgrClient, hwmgr, nodepool); err != nil {
		return fmt.Errorf("failed to release nodepool %s: %w", nodepool.Name, err)
	}

	return nil
}

func (a *Adaptor) GetResourcePools(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager) ([]invserver.ResourcePoolInfo, int, error) {
	var resp []invserver.ResourcePoolInfo

	client, err := hwmgrclient.NewClientWithResponses(ctx, a.Logger, a.Client, hwmgr)
	if err != nil {
		// TODO: Expose status errors from client
		a.Logger.InfoContext(ctx, "NewClientWithResponses error", slog.String("error", err.Error()))
		return resp, http.StatusInternalServerError, fmt.Errorf("unable to create hwmgr client: %w", err)
	}

	pools, err := client.GetResourcePools(ctx)
	if err != nil {
		a.Logger.InfoContext(ctx, "GetResourcePools error", slog.String("error", err.Error()))
		return resp, http.StatusInternalServerError, fmt.Errorf("unable to query pools: %w", err)
	}

	for _, pool := range *pools.ResourcePools {
		resp = append(resp, invserver.ResourcePoolInfo{
			ResourcePoolId: *pool.Id,
			Description:    *pool.Description,
			Name:           *pool.Name,
			SiteId:         pool.SiteId,
		})
	}
	return resp, http.StatusOK, nil
}

func (a *Adaptor) GetResources(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager) ([]invserver.ResourceInfo, int, error) {
	var resp []invserver.ResourceInfo

	client, err := hwmgrclient.NewClientWithResponses(ctx, a.Logger, a.Client, hwmgr)
	if err != nil {
		// TODO: Expose status errors from client
		a.Logger.InfoContext(ctx, "NewClientWithResponses error", slog.String("error", err.Error()))
		return resp, http.StatusInternalServerError, fmt.Errorf("unable to create hwmgr client: %w", err)
	}

	resources, err := client.GetResources(ctx)
	if err != nil {
		a.Logger.InfoContext(ctx, "GetResources error", slog.String("error", err.Error()))
		return resp, http.StatusInternalServerError, fmt.Errorf("unable to query resources: %w", err)
	}

	servers, err := client.GetServersInventory(ctx)
	if err != nil {
		a.Logger.InfoContext(ctx, "GetServersInventory error", slog.String("error", err.Error()))
		return resp, http.StatusInternalServerError, fmt.Errorf("unable to query server inventory: %w", err)
	}

	for _, resource := range *resources.Resources {
		var server *hwmgrapi.ApiprotoServer
		for _, iter := range *servers.Servers {
			if resource.Name == nil || iter.Metadata.Name == nil || *resource.Name != *iter.Metadata.Name {
				continue
			}
			server = &iter
		}
		resp = append(resp, invserver.ResourceInfo{
			Name:           *resource.Name,
			ResourceId:     *resource.Res.Id,
			GlobalAssetId:  resource.GlobalAssetId,
			ResourcePoolId: *resource.ResourcePoolId,
			Description:    *resource.Description,
		})
		a.Logger.InfoContext(ctx, "Placeholder comment for server data reference", slog.Any("server", server))
	}

	return resp, http.StatusOK, nil
}
