/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

//nolint:all
package dellhwmgr

import (
	"context"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/openshift-kni/oran-hwmgr-plugin/test/utils"

	"github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/crds"
	dellserver "github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/dell-hwmgr/dell-server"

	"github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/assets"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/phayes/freeport"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	apiserver "github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/dell-hwmgr/dell-server/generated"

	hwmgrpluginoranopenshiftiov1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
)

// These tests use Ginkgo: http://onsi.github.io/ginkgo/

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	logger    *slog.Logger

	// a free test server port
	fp int

	// a http test server infra
	server *http.Server

	// store external CRDs
	tmpDir string

	// cancel the test server goroutine
	ctx    context.Context
	cancel context.CancelFunc
)

func TestDellAdaptor(t *testing.T) {
	RegisterFailHandler(Fail)

	tmpDir = t.TempDir()

	RunSpecs(t, "The Dell adaptor test suite")
}

var _ = BeforeSuite(func() {

	// create a logger
	options := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}
	handler := slog.NewJSONHandler(GinkgoWriter, options)
	logger = slog.New(handler)

	// fetch 'hardwaremanagement' module info
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

	ds := dellserver.DellServer{}
	h := apiserver.HandlerWithOptions(ds, apiserver.GorillaServerOptions{})

	fp, err = freeport.GetFreePort()
	Expect(err).NotTo(HaveOccurred(), "failed to find a free port to listen on")

	server = &http.Server{Addr: ":" + strconv.Itoa(fp), Handler: h}

	// start the test server
	go func() {
		defer GinkgoRecover()
		err = server.ListenAndServe()
		Expect(err).To(Equal(http.ErrServerClosed)) // when tearing down the test environment
	}()

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	// stop the test server
	if server != nil {
		ctx, cancel = context.WithCancel(
			context.Background())
		err := server.Shutdown(ctx)
		Expect(err).NotTo(HaveOccurred(), "failed to stop test server")
		cancel()
	}

	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})
