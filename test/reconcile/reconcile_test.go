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

package reconcile

import (
	"context"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hwmgrpluginoranopenshiftiov1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/test/reconcile/assets"
	imsv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("reconcile via the loopback adaptor", func() {
	When("reconciling a node pool", func() {

		var (
			cm    *corev1.ConfigMap
			hwmgr *hwmgrpluginoranopenshiftiov1alpha1.HardwareManager
			np    *imsv1alpha1.NodePool
		)

		ctx := context.Background()

		BeforeEach(func() {
			// Create the loopback cm instance
			var err error
			cm, err = assets.GetConfigmapFromFile("manifests/loopback-nodelist-cm.yaml")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			// Create the HardwareManager cr instance
			hwmgr, err = assets.GetHardwareManageFromFile("manifests/loopback-hm.yaml")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, hwmgr)).To(Succeed())

			// Create the Nodepool cr instance
			np, err = assets.GetNodePoolFromFile("manifests/np1.yaml")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, np)).To(Succeed())
		})

		AfterEach(func() {
			// Delete the loopback cm instance
			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())

			// Delete the HardwareManager cr instance
			Expect(k8sClient.Delete(ctx, hwmgr)).To(Succeed())

			// Delete the Nodepool cr instance
			Expect(k8sClient.Delete(ctx, np)).To(Succeed())

		})
		It("must create ims nodes", func() {
			By("reconciling according to the provided manifests")

			// check node has been created
			node := &imsv1alpha1.Node{}
			timeout, interval := 30, 1
			Eventually(nodeExists("dummy-sp-64g-0", "default", node), timeout, interval).Should(BeTrue())

			// check node must use the hardware profile specified by the nodepool cr instance
			got := node.Spec.HwProfile
			wanted := np.Spec.NodeGroup[0].HwProfile
			Expect(got).To(Equal(wanted))
		})

	})
})

func nodeExists(name string, ns string, node *imsv1alpha1.Node) func() bool {
	return func() bool {
		typeNamespacedNode := types.NamespacedName{
			Name:      name,
			Namespace: ns,
		}
		err := k8sClient.Get(ctx, typeNamespacedNode, node)
		return err == nil
	}
}
