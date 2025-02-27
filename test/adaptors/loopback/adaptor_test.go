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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	hwmgrpluginoranopenshiftiov1alpha1 "github.com/openshift-kni/oran-hwmgr-plugin/api/hwmgr-plugin/v1alpha1"
	"github.com/openshift-kni/oran-hwmgr-plugin/test/adaptors/assets"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("reconcile via the loopback adaptor", func() {
	When("reconciling a node pool", func() {

		var (
			cm    *corev1.ConfigMap
			hwmgr *hwmgrpluginoranopenshiftiov1alpha1.HardwareManager
			np    *hwmgmtv1alpha1.NodePool
		)

		ctx := context.Background()

		BeforeEach(func() {
			// create the loopback cm instance
			var err error
			cm, err = assets.GetConfigmapFromFile("manifests/loopback-nodelist-cm.yaml")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			// create the HardwareManager cr instance
			hwmgr, err = assets.GetHardwareManagerFromFile("manifests/loopback-hwmgr.yaml")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, hwmgr)).To(Succeed())

			// create the Nodepool cr instance
			np, err = assets.GetNodePoolFromFile("manifests/np1-np.yaml")
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Create(ctx, np)).To(Succeed())
		})

		AfterEach(func() {

			// delete the Nodepool cr instance
			Expect(k8sClient.Delete(ctx, np)).To(Succeed())

			// delete the loopback cm instance
			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())

			// delete the HardwareManager cr instance
			Expect(k8sClient.Delete(ctx, hwmgr)).To(Succeed())

		})
		It("must create ims nodes", func() {
			By("reconciling according to the provided manifests")

			// check node has been created
			node := &hwmgmtv1alpha1.Node{}
			timeout, interval := 30, 1
			Eventually(nodeExists("dummy-sp-64g-0", node), timeout, interval).Should(BeTrue())

			// check node must use the hardware profile specified by the nodepool cr instance
			got := node.Spec.HwProfile
			wanted := np.Spec.NodeGroup[0].NodePoolData.HwProfile
			Expect(got).To(Equal(wanted))

			// get the NodePool
			np := &hwmgmtv1alpha1.NodePool{}
			ns := types.NamespacedName{
				Name:      "np1",
				Namespace: "default",
			}
			Expect(k8sClient.Get(ctx, ns, np)).To(Succeed())

			// check nodes will be automatically deleted when the Nodepool is deleted by 'ownerReference'
			Expect(DoAllnodesHaveNpOwnerRef(np.UID)).To(BeTrue())

			// check secrets will be automatically deleted when the NodePool is deleted by 'ownerReference'
			Expect(DoAllsecretsHaveNpOwnerRef(np.UID)).To(BeTrue())
		})

	})
})

func DoAllsecretsHaveNpOwnerRef(uid types.UID) bool {
	secretList := &corev1.SecretList{}
	if err := k8sClient.List(ctx, secretList); err != nil {
		return false
	}
	// for all secrets
	for _, item := range secretList.Items {
		ors := item.ObjectMeta.OwnerReferences
		found := false
		// get ownerReferences
		for _, or := range ors {
			// and check the NodePool has ownership
			if or.Kind == "NodePool" && or.UID == uid {
				found = true
				break
			}
		}
		if !found {
			return false // a secret was found without an ownerReference to the NodePool
		}
	}
	return true
}

func DoAllnodesHaveNpOwnerRef(uid types.UID) bool {
	nodelist := &hwmgmtv1alpha1.NodeList{}
	if err := k8sClient.List(ctx, nodelist); err != nil {
		return false
	}
	// for all nodes
	for _, item := range nodelist.Items {
		ors := item.ObjectMeta.OwnerReferences
		found := false
		// get ownerReferences
		for _, or := range ors {
			// and check the NodePool has ownership
			if or.Kind == "NodePool" && or.UID == uid {
				found = true
				break
			}
		}
		if !found {
			return false // a node was found without an ownerReference to the NodePool
		}
	}
	return true
}

func nodeExists(nodeId string, node *hwmgmtv1alpha1.Node) func() bool {
	return func() bool {
		nodelist := &hwmgmtv1alpha1.NodeList{}
		if err := k8sClient.List(ctx, nodelist); err != nil {
			return false
		}

		for _, nodeIter := range nodelist.Items {
			if nodeIter.Spec.HwMgrNodeId == nodeId {
				*node = nodeIter
				return true
			}
		}

		return false
	}
}
