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

package o2imshardwaremanagement

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openshift-kni/oran-hwmgr-plugin/internal/logging"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	adaptors "github.com/openshift-kni/oran-hwmgr-plugin/adaptors"
	"github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

// NodePoolReconciler reconciles a NodePool object
type NodePoolReconciler struct {
	ctrl.Manager
	client.Client
	NoncachedClient client.Reader
	Scheme          *runtime.Scheme
	Logger          *slog.Logger
	Namespace       string
	HwMgrAdaptor    *adaptors.HwMgrAdaptorController
	indexerEnabled  bool
}

func (r *NodePoolReconciler) SetupIndexer(ctx context.Context) error {
	// Setup Node CRD indexer. This field indexer allows us to query a list of Node CRs, filtered by the spec.nodePool field.
	nodeIndexFunc := func(obj client.Object) []string {
		return []string{obj.(*hwmgmtv1alpha1.Node).Spec.NodePool}
	}

	if err := r.Manager.GetFieldIndexer().IndexField(ctx, &hwmgmtv1alpha1.Node{}, utils.NodeSpecNodePoolKey, nodeIndexFunc); err != nil {
		return fmt.Errorf("failed to setup node indexer: %w", err)
	}

	return nil
}

//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodepools,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodepools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodepools/finalizers,verbs=update
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodes,verbs=get;create;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodes/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;update;patch;watch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *NodePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	result = utils.DoNotRequeue()

	ctx = logging.AppendCtx(ctx, slog.String("nodepool", req.Name))

	if !r.indexerEnabled {
		if err = r.SetupIndexer(ctx); err != nil {
			err = fmt.Errorf("failed to setup indexer: %w", err)
			return
		}
		r.Logger.InfoContext(ctx, "NodePool field indexer initialized")
		r.indexerEnabled = true
	}

	// Fetch the nodepool:
	nodepool := &hwmgmtv1alpha1.NodePool{}
	if err = utils.GetNodePool(ctx, r.NoncachedClient, req.NamespacedName, nodepool); err != nil {
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

	ctx = logging.AppendCtx(ctx, slog.String("CloudID", nodepool.Spec.CloudID))

	r.Logger.InfoContext(ctx, "Reconciling NodePool")

	if nodepool.GetDeletionTimestamp() != nil {
		// Handle deletion
		r.Logger.InfoContext(ctx, "Nodepool is being deleted")
		if controllerutil.ContainsFinalizer(nodepool, utils.NodepoolFinalizer) {
			if err := r.HwMgrAdaptor.HandleNodePoolDeletion(ctx, nodepool); err != nil {
				// Log the failure and continue, to remove the finalizer and allow the deletion
				r.Logger.InfoContext(ctx, "Failed HandleNodePoolDeletion", slog.String("error", err.Error()))
			}

			if err := utils.NodepoolRemoveFinalizer(ctx, r.Client, nodepool); err != nil {
				return utils.RequeueImmediately(), fmt.Errorf("failed to remove finalizer from nodepool: %w", err)
			}

			return utils.DoNotRequeue(), nil
		}
		return utils.DoNotRequeue(), nil
	}

	// Hand off the CR to the adaptor
	result, err = r.HwMgrAdaptor.HandleNodePool(ctx, nodepool)
	if err != nil {
		err = fmt.Errorf("failed HandleNodePool: %w", err)
		return
	}

	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodePoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&hwmgmtv1alpha1.NodePool{}).
		Complete(r); err != nil {
		return fmt.Errorf("failed to create controller: %w", err)
	}

	return nil
}
