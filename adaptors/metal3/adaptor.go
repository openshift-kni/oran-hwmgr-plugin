/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package metal3

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	metal3v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/metal3/controller"
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
		Logger:          logger.With(slog.String("adaptor", "metal3")),
		Namespace:       namespace,
	}
}

// SetupAdaptor sets up the metal3 adaptor
func (a *Adaptor) SetupAdaptor(mgr ctrl.Manager) error {
	a.Logger.Info("SetupAdaptor called for metal3")

	if err := (&controller.HardwareManagerReconciler{
		Client:    a.Client,
		Scheme:    a.Scheme,
		Logger:    a.Logger,
		Namespace: a.Namespace,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to setup metal3 adaptor: %w", err)
	}

	return nil
}

// Metal3 Adaptor FSM
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

func (a *Adaptor) HandleNodePoolDeletion(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) (bool, error) {
	a.Logger.InfoContext(ctx, "Finalizing nodepool")

	if err := a.ReleaseNodePool(ctx, hwmgr, nodepool); err != nil {
		return false, fmt.Errorf("failed to release nodepool %s: %w", nodepool.Name, err)
	}

	return true, nil
}

func (a *Adaptor) GetResourcePools(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager) ([]invserver.ResourcePoolInfo, int, error) {
	var resp []invserver.ResourcePoolInfo

	var bmhList metal3v1alpha1.BareMetalHostList
	var opts []client.ListOption

	if err := a.Client.List(ctx, &bmhList, opts...); err != nil {
		return resp, http.StatusInternalServerError, fmt.Errorf("failed to get bmh list: %w", err)
	}

	for _, bmh := range bmhList.Items {
		if includeInInventory(bmh) {
			siteID := bmh.Labels[LabelSiteID]
			poolID := bmh.Labels[LabelResourcePoolID]
			resp = append(resp, invserver.ResourcePoolInfo{
				ResourcePoolId: poolID,
				Description:    poolID,
				Name:           poolID,
				SiteId:         &siteID,
			})

		}
	}

	return resp, http.StatusOK, nil
}

func (a *Adaptor) GetResources(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager) ([]invserver.ResourceInfo, int, error) {
	var resp []invserver.ResourceInfo

	var bmhList metal3v1alpha1.BareMetalHostList
	var opts []client.ListOption

	if err := a.Client.List(ctx, &bmhList, opts...); err != nil {
		return resp, http.StatusInternalServerError, fmt.Errorf("failed to get bmh list: %w", err)
	}

	for _, bmh := range bmhList.Items {
		if includeInInventory(bmh) {
			resp = append(resp, getResourceInfo(bmh))
		}
	}

	return resp, http.StatusOK, nil
}
