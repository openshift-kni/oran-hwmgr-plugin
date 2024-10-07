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

	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	"k8s.io/apimachinery/pkg/api/errors"
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
// TODO(user): Modify the Reconcile function to compare the state specified by
// the HardwareManager object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *HardwareManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	result = utils.DoNotRequeue()

	// Fetch the CR:
	instance := &pluginv1alpha1.HardwareManager{}
	if err = r.Client.Get(ctx, req.NamespacedName, instance); err != nil {
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
	if instance.Spec.AdaptorID != r.AdaptorID {
		// Nothing to do
		r.Logger.InfoContext(ctx, "[Dell HardwareManager] Not for me",
			slog.String("name", instance.Name))
		return
	}

	if instance.Spec.DellData == nil {
		// Invalid data
		return
	}

	r.Logger.InfoContext(ctx, "[Dell HardwareManager] User: "+instance.Spec.DellData.User)

	return
}

func filterEvents(adaptorID pluginv1alpha1.HardwareManagerAdaptorID) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(object client.Object) bool {
		instance := object.(*pluginv1alpha1.HardwareManager)
		return (instance.Spec.AdaptorID == adaptorID)
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *HardwareManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.AdaptorID = pluginv1alpha1.SupportedAdaptors.Dell
	r.Logger.Info("Setting up Dell controller", slog.String("adaptor-id", string(r.AdaptorID)))
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&pluginv1alpha1.HardwareManager{}).
		WithEventFilter(filterEvents(r.AdaptorID)).
		Complete(r); err != nil {
		return fmt.Errorf("failed to setup controller for %s: %w", r.AdaptorID, err)
	}

	return nil

}
