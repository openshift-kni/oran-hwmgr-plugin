/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package adaptorinterface

import (
	"context"
	"errors"
	"log/slog"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginv1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	invserver "github.com/openshift-kni/oran-hwmgr-plugin/internal/server/api/generated"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

type HwMgrAdaptorIntf interface {
	SetupAdaptor(mgr ctrl.Manager) error
	HandleNodePool(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) (ctrl.Result, error)
	HandleNodePoolDeletion(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager, nodepool *hwmgmtv1alpha1.NodePool) (bool, error)
	GetResourcePools(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager) ([]invserver.ResourcePoolInfo, int, error)
	GetResources(ctx context.Context, hwmgr *pluginv1alpha1.HardwareManager) ([]invserver.ResourceInfo, int, error)
}

// Define the HwMgrAdaptor structures
type HwMgrAdaptorConfig struct {
	client.Client
	logger    *slog.Logger
	namespace string
}

type HwMgrAdaptor struct {
	config HwMgrAdaptorConfig
}

func NewHwMgrAdaptor(config *HwMgrAdaptorConfig) (hwmgr *HwMgrAdaptor, err error) {
	if config.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	hwmgr = &HwMgrAdaptor{
		config: *config,
	}

	if hwmgr.config.namespace == "" {
		hwmgr.config.namespace = os.Getenv("MY_POD_NAMESPACE")
	}

	return
}
