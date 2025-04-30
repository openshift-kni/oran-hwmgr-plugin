/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
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
	NoncachedClient client.Reader
	Scheme          *runtime.Scheme
	Logger          *slog.Logger
	Namespace       string
	AdaptorID       pluginv1alpha1.HardwareManagerAdaptorID
}

func NewAdaptor(client client.Client, noncachedClient client.Reader, scheme *runtime.Scheme, logger *slog.Logger, namespace string) *Adaptor {
	return &Adaptor{
		Client:          client,
		NoncachedClient: noncachedClient,
		Scheme:          scheme,
		Logger:          logger.With(slog.String("adaptor", "dell-hwmgr")),
		Namespace:       namespace,
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

func (a *Adaptor) HandleNodePoolDeletion(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) (bool, error) {
	a.Logger.InfoContext(ctx, "Finalizing nodepool")

	hwmgrClient, clientErr := hwmgrclient.NewClientWithResponses(ctx, a.Logger, a.Client, hwmgr)
	if clientErr != nil {
		// TODO: Improve client error handling to distinguish between connectivity errors, auth, etc
		a.Logger.InfoContext(ctx, "NewClientWithResponses error", slog.String("error", clientErr.Error()))
		return false, fmt.Errorf("failed to setup hwmgr client: %w", clientErr)
	}

	if exists, err := hwmgrClient.ResourceGroupExists(ctx, nodepool); err != nil {
		return false, fmt.Errorf("resource group existence check failed for cloudID=%s: err: %w", nodepool.Spec.CloudID, err)
	} else if !exists {
		// The resource group doesn't exist, so there's nothing to delete
		a.Logger.InfoContext(ctx, "Resource Group no longer exists on hardware manager")
		return true, nil
	}

	completed, err := a.ReleaseNodePool(ctx, hwmgrClient, hwmgr, nodepool)
	if err != nil {
		return false, fmt.Errorf("failed to release nodepool %s: %w", nodepool.Name, err)
	}

	return completed, nil
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

		if server == nil {
			a.Logger.InfoContext(ctx, "Unable to find server info for resource. Skipping",
				slog.String("resource-name", *resource.Name))
			continue
		}

		resp = append(resp, getResourceInfo(resource, server))
	}

	return resp, http.StatusOK, nil
}
