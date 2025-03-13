/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"log/slog"

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

	// Make sure this is an instance for this adaptor and that this generation hasn't already been handled
	if hwmgr.Spec.AdaptorID != r.AdaptorID ||
		hwmgr.Status.ObservedGeneration == hwmgr.Generation {
		// Nothing to do
		return
	}

	ctx = logging.AppendCtx(ctx, slog.String("hwmgr", hwmgr.Name))

	hwmgr.Status.ObservedGeneration = hwmgr.Generation

	// Configuration data is not currently mandatory for the loopback adaptor
	if updateErr := utils.UpdateHardwareManagerStatusCondition(ctx, r.Client, hwmgr,
		pluginv1alpha1.ConditionTypes.Validation,
		pluginv1alpha1.ConditionReasons.Completed,
		metav1.ConditionTrue,
		"Validated"); updateErr != nil {
		err = fmt.Errorf("failed to update status for hardware manager (%s) with validation success: %w", hwmgr.Name, updateErr)
		return
	}

	r.Logger.InfoContext(ctx, "[Loopback HardwareManager]", slog.Any("loopbackData", hwmgr.Spec.LoopbackData))

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
	r.AdaptorID = pluginv1alpha1.SupportedAdaptors.Loopback
	r.Logger.Info("Setting up Loopback controller", slog.String("adaptorId", string(r.AdaptorID)))
	if err := ctrl.NewControllerManagedBy(mgr).
		Named(string(r.AdaptorID)).
		For(&pluginv1alpha1.HardwareManager{}).
		WithEventFilter(filterEvents(r.AdaptorID)).
		Complete(r); err != nil {
		return fmt.Errorf("failed to setup controller for %s: %w", r.AdaptorID, err)
	}

	return nil

}
