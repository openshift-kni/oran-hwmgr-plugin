/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
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

//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
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
func (r *NodePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// Add logging context with the nodepool name
	ctx = logging.AppendCtx(ctx, slog.String("nodepool", req.Name))

	if !r.indexerEnabled {
		if err := r.SetupIndexer(ctx); err != nil {
			return utils.DoNotRequeue(), fmt.Errorf("failed to setup indexer: %w", err)
		}
		r.Logger.InfoContext(ctx, "NodePool field indexer initialized")
		r.indexerEnabled = true
	}

	// Fetch the nodepool, using non-caching client
	nodepool := &hwmgmtv1alpha1.NodePool{}
	if err := utils.GetNodePool(ctx, r.NoncachedClient, req.NamespacedName, nodepool); err != nil {
		if errors.IsNotFound(err) {
			// The NodePool has likely been deleted
			return utils.DoNotRequeue(), nil
		}
		r.Logger.InfoContext(ctx, "Unable to fetch NodePool. Requeuing", slog.String("error", err.Error()))
		return utils.RequeueWithShortInterval(), nil
	}

	// Add logging context with data from the CR
	ctx = logging.AppendCtx(ctx, slog.String("CloudID", nodepool.Spec.CloudID))
	ctx = logging.AppendCtx(ctx, slog.String("startingResourceVersion", nodepool.ResourceVersion))

	r.Logger.InfoContext(ctx, "Reconciling NodePool")

	if nodepool.GetDeletionTimestamp() != nil {
		// Handle deletion
		r.Logger.InfoContext(ctx, "Nodepool is being deleted")
		if controllerutil.ContainsFinalizer(nodepool, utils.NodepoolFinalizer) {
			completed, deleteErr := r.HwMgrAdaptor.HandleNodePoolDeletion(ctx, nodepool)
			if deleteErr != nil {
				return utils.RequeueWithShortInterval(), fmt.Errorf("failed HandleNodePoolDeletion: %w", deleteErr)
			}

			if !completed {
				r.Logger.InfoContext(ctx, "Deletion handling in progress, requeueing")
				return utils.RequeueWithShortInterval(), nil
			}

			if finalizerErr := utils.NodepoolRemoveFinalizer(ctx, r.Client, nodepool); finalizerErr != nil {
				r.Logger.InfoContext(ctx, "Failed to remove finalizer, requeueing", slog.String("error", finalizerErr.Error()))
				return utils.RequeueWithShortInterval(), nil
			}

			r.Logger.InfoContext(ctx, "Deletion handling complete, finalizer removed")
			return utils.DoNotRequeue(), nil
		}

		r.Logger.InfoContext(ctx, "No finalizer, deletion handling complete")
		return utils.DoNotRequeue(), nil
	}

	// Hand off the CR to the adaptor
	result, err := r.HwMgrAdaptor.HandleNodePool(ctx, nodepool)
	if err != nil {
		return result, fmt.Errorf("failed HandleNodePool: %w", err)
	}

	return result, nil
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
