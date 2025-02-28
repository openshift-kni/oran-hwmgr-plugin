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

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/hwmgrclient"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/logging"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
)

// HardwareManagerReconciler reconciles a HardwareManager object
type HardwareManagerReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Logger    *slog.Logger
	Namespace string
	AdaptorID pluginv1alpha1.HardwareManagerAdaptorID
}

//+kubebuilder:rbac:groups=hwmgr-plugin.oran.openshift.io,resources=hardwaremanagers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=hwmgr-plugin.oran.openshift.io,resources=hardwaremanagers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=hwmgr-plugin.oran.openshift.io,resources=hardwaremanagers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *HardwareManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	result = utils.DoNotRequeue()

	// Fetch the CR:
	hwmgr := &pluginv1alpha1.HardwareManager{}
	if err = r.Client.Get(ctx, req.NamespacedName, hwmgr); err != nil {
		if errors.IsNotFound(err) {
			// The NodePool has likely been deleted
			err = nil
			return
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch NodePool",
			slog.String("error", err.Error()),
		)
		return
	}

	// Make sure this is an instance for this adaptor
	if hwmgr.Spec.AdaptorID != r.AdaptorID {
		// Skip this CR
		return
	}

	ctx = logging.AppendCtx(ctx, slog.String("hwmgr", hwmgr.Name))

	hwmgr.Status.ObservedGeneration = hwmgr.Generation

	if hwmgr.Spec.DellData == nil {
		// Invalid data
		if updateErr := utils.UpdateHardwareManagerStatusCondition(ctx, r.Client, hwmgr,
			pluginv1alpha1.ConditionTypes.Validation,
			pluginv1alpha1.ConditionReasons.Failed,
			metav1.ConditionFalse,
			"Missing dellData configuration field"); updateErr != nil {
			err = fmt.Errorf("failed to update status for hardware manager (%s) with validation failure: %w", hwmgr.Name, updateErr)
			return
		}
		r.Logger.ErrorContext(ctx, "HardwareManager CR missing dellData configuration field", slog.String("name", hwmgr.Name))
		return
	}

	result = utils.RequeueWithLongInterval()

	r.Logger.InfoContext(ctx, "Validating client connection", slog.String("apiUrl", hwmgr.Spec.DellData.ApiUrl))

	client, clientErr := hwmgrclient.NewClientWithResponses(ctx, r.Logger, r.Client, hwmgr)
	if clientErr != nil {
		r.Logger.InfoContext(ctx, "NewClientWithResponses error", slog.String("error", clientErr.Error()))
		if updateErr := utils.UpdateHardwareManagerStatusCondition(ctx, r.Client, hwmgr,
			pluginv1alpha1.ConditionTypes.Validation,
			pluginv1alpha1.ConditionReasons.Failed,
			metav1.ConditionFalse,
			"Authentication failure - "+clientErr.Error()); updateErr != nil {
			err = fmt.Errorf("failed to update status for hardware manager (%s) with authentication failure: %w", hwmgr.Name, updateErr)
			return
		}
		r.Logger.ErrorContext(ctx, "Failed to establish connection to hardware manager", slog.String("name", hwmgr.Name), slog.String("error", clientErr.Error()))
		return
	}

	pools, clientErr := client.GetResourcePools(ctx)
	if clientErr != nil {
		r.Logger.InfoContext(ctx, "GetResourcePools error", slog.String("error", clientErr.Error()))
		if updateErr := utils.UpdateHardwareManagerStatusCondition(ctx, r.Client, hwmgr,
			pluginv1alpha1.ConditionTypes.Validation,
			pluginv1alpha1.ConditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to query resource pools - "+clientErr.Error()); updateErr != nil {
			err = fmt.Errorf("failed to update status for hardware manager (%s) with authentication failure: %w", hwmgr.Name, updateErr)
			return
		}
		r.Logger.ErrorContext(ctx, "Failed to query resource pools", slog.String("name", hwmgr.Name), slog.String("error", clientErr.Error()))
		return
	}

	hwmgr.Status.ResourcePools = make(pluginv1alpha1.PerSiteResourcePoolList)
	if pools.ResourcePools != nil {
		tenant := client.GetTenant()
		for _, pool := range *pools.ResourcePools {
			if pool.SiteId == nil || pool.Id == nil ||
				pool.Res == nil || pool.Res.Tenant == nil {
				// Skip pools that are missing data
				r.Logger.InfoContext(ctx, "entry in resourcepools list missing data", slog.Any("pool", pool))
				continue
			}

			if *pool.Res.Tenant != tenant {
				// Skip pools for other tenants
				r.Logger.InfoContext(ctx, "resourcepools list contains entry for other tenant", slog.String("tenant", *pool.Res.Tenant), slog.String("id", *pool.Id))
				continue
			}

			hwmgr.Status.ResourcePools[*pool.SiteId] = append(hwmgr.Status.ResourcePools[*pool.SiteId], *pool.Id)
			slices.Sort(hwmgr.Status.ResourcePools[*pool.SiteId])
		}
	}

	if updateErr := utils.UpdateHardwareManagerStatusCondition(ctx, r.Client, hwmgr,
		pluginv1alpha1.ConditionTypes.Validation,
		pluginv1alpha1.ConditionReasons.Completed,
		metav1.ConditionTrue,
		"Authentication passed"); updateErr != nil {
		err = fmt.Errorf("failed to update status for hardware manager (%s) with validation success: %w", hwmgr.Name, updateErr)
		return
	}

	return
}

func filterEvents(adaptorID pluginv1alpha1.HardwareManagerAdaptorID) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(object client.Object) bool {
		hwmgr := object.(*pluginv1alpha1.HardwareManager)
		return hwmgr.Spec.AdaptorID == adaptorID
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *HardwareManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.AdaptorID = pluginv1alpha1.SupportedAdaptors.Dell
	r.Logger.Info("Setting up Dell controller", slog.String("adaptorId", string(r.AdaptorID)))
	if err := ctrl.NewControllerManagedBy(mgr).
		Named(string(r.AdaptorID)).
		For(&pluginv1alpha1.HardwareManager{}).
		WithEventFilter(filterEvents(r.AdaptorID)).
		WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})).
		Complete(r); err != nil {
		return fmt.Errorf("failed to setup controller for %s: %w", r.AdaptorID, err)
	}

	return nil

}
