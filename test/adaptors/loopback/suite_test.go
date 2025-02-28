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

//nolint:all
package loopback

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors"
	o2imshardwaremanagement "github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/o2ims-hardwaremanagement"
	controllerutils "github.com/openshift-kni/oran-hwmgr-plugin/internal/controller/utils"
	"github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/assets"
	"github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/crds"
	"github.com/openshift-kni/oran-hwmgr-plugin/test/utils"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	hwmgrpluginoranopenshiftiov1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

// These tests use Ginkgo: http://onsi.github.io/ginkgo/

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	mgr       manager.Manager
	logger    *slog.Logger

	// store external CRDs
	tmpDir string

	// cancel the manager goroutine
	ctx    context.Context
	cancel context.CancelFunc
)

func TestLoopbackAdaptor(t *testing.T) {
	RegisterFailHandler(Fail)

	tmpDir = t.TempDir()

	RunSpecs(t, "The loopback adapator test suite")
}

var _ = BeforeSuite(func() {

	// create a logger
	options := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewJSONHandler(GinkgoWriter, options)
	logger = slog.New(handler)

	// fetch hardwaremanagement module info
	hwrMgtMod := crds.ImsRepoPath + "/" + crds.ImsRepoName + "/" + crds.ImsHwrMgtPath
	hwrMgtModNew, hwrMgtModPseudoVersionNew, err := utils.GetModuleFromGoMod(hwrMgtMod)
	Expect(err).NotTo(HaveOccurred())

	commit := utils.GetGitCommitFromPseudoVersion(hwrMgtModPseudoVersionNew)
	repo := utils.GetHardwareManagementGitRepoFromModule(hwrMgtModNew)

	// fetch required CRDs
	crdPath := filepath.Join(tmpDir, crds.ImsRepoName)
	err = crds.GetRequiredCRDsFromGit("https://"+repo, commit, crdPath)
	Expect(err).NotTo(HaveOccurred())

	reqCRDs := filepath.Join(crdPath, "bundle", "manifests")
	ownCRDs := filepath.Join("..", "..", "..", "config", "crd", "bases")

	// configure all CRDs
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{ownCRDs, reqCRDs},
		ErrorIfCRDPathMissing: true,
	}

	// add ims plugin to schema
	err = hwmgrpluginoranopenshiftiov1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// add ims to schema
	err = hwmgmtv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// create a k8s client
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// init the codecs for manifests
	err = assets.InitCodecs()
	Expect(err).NotTo(HaveOccurred())

	// build the manager
	mgr, err = manager.New(cfg, manager.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	err = controllerutils.InitNodepoolUtils(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// build the adaptor controller
	hwmgrAdaptor := &adaptors.HwMgrAdaptorController{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Logger:    logger,
		Namespace: "default",
	}

	err = hwmgrAdaptor.SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	// build the hardware manager reconciler
	nodepoolReconciler := o2imshardwaremanagement.NodePoolReconciler{
		Manager:         mgr,
		Client:          mgr.GetClient(),
		NoncachedClient: mgr.GetAPIReader(),
		Scheme:          mgr.GetScheme(),
		Logger:          logger,
		Namespace:       "default",
		HwMgrAdaptor:    hwmgrAdaptor,
	}
	err = nodepoolReconciler.SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	// start the manager
	ctx, cancel = context.WithCancel(
		context.Background())
	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	// stop the manager
	if mgr != nil {
		cancel()
	}
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})
