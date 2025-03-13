/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/
//nolint:all
package dellhwmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	api "github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/generated"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-kni/oran-hwmgr-plugin/adaptors/dell-hwmgr/hwmgrclient"
	hwmgrpluginoranopenshiftiov1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/assets"
	dellserver "github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/dell-hwmgr/dell-server"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("request an authentication token", func() {
	When("requesting an authentication token from the test server", func() {

		var (
			hwmgr  *hwmgrpluginoranopenshiftiov1alpha1.HardwareManager
			secret *corev1.Secret
		)

		ctx := context.Background()

		BeforeEach(func() {

			var err error

			// create the HardwareManager cr instance
			url := fmt.Sprintf("http://127.0.0.1:%d", fp)
			hwmgr, err = assets.GetHardwareManagerFromTmpl(url, "manifests/dell-hwmgr.tmpl")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, hwmgr)).To(Succeed())

			// create the Dell secret
			secret, err = assets.GetSecretFromFile("manifests/dell-secret.yaml")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		})

		AfterEach(func() {
			// delete the HardwareManager cr instance
			Expect(k8sClient.Delete(ctx, hwmgr)).To(Succeed())

			// delete the secret
			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())

		})
		It("must get the token on receiving a successful response from the test server", func() {
			By("parsing the response and returning the token")

			// response from server
			dellserver.GetTokenFn = GetTokenSuccessfulMock

			// request
			hmc, err := hwmgrclient.NewClientWithResponses(ctx, logger, k8sClient, hwmgr)
			Expect(err).NotTo(HaveOccurred())

			token, err := hmc.GetToken(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(token == accessTokenStr).Should(BeTrue())
		})

		It("must indicate an error on receiving a no token response from the test server", func() {
			By("parsing the response and returning an error")

			// response from server
			dellserver.GetTokenFn = GetNoTokenMock

			// request
			_, err := hwmgrclient.NewClientWithResponses(ctx, logger, k8sClient, hwmgr)
			Expect(err).To(HaveOccurred())

		})

	})

})

var accessTokenStr = "a simple token"

// mock the GetToken response (successful)
func GetTokenSuccessfulMock(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	token := api.RhprotoGetTokenResponseBody{AccessToken: &accessTokenStr}
	err := json.NewEncoder(w).Encode(token)
	Expect(err).NotTo(HaveOccurred())
}

// mock the GetToken response (no token)
func GetNoTokenMock(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	// no token included
	w.WriteHeader(http.StatusOK)
}
