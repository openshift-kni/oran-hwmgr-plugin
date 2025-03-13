/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hwmgrpluginoranopenshiftiov1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
)

var _ = Describe("HardwareManager Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		hardwaremanager := &hwmgrpluginoranopenshiftiov1alpha1.HardwareManager{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind HardwareManager")
			err := k8sClient.Get(ctx, typeNamespacedName, hardwaremanager)
			if err != nil && errors.IsNotFound(err) {
				resource := &hwmgrpluginoranopenshiftiov1alpha1.HardwareManager{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: hwmgrpluginoranopenshiftiov1alpha1.HardwareManagerSpec{
						AdaptorID: "loopback",
						LoopbackData: &hwmgrpluginoranopenshiftiov1alpha1.LoopbackData{
							AddtionalInfo: "test string",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &hwmgrpluginoranopenshiftiov1alpha1.HardwareManager{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance HardwareManager")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &HardwareManagerReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Logger:    logger,
				Namespace: "default", // TODO(user):Modify as needed
				AdaptorID: "loopback",
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
