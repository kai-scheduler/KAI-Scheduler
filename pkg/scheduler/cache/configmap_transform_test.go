// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/configmap_info"
)

var _ = Describe("ConfigMap informer transform", func() {
	It("retains existence checks without retaining payloads or modifying API objects", func() {
		configMap := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "config", Namespace: "workload", UID: "config-uid", ResourceVersion: "1",
				Labels:        map[string]string{"app": "unrelated"},
				Annotations:   map[string]string{"large": strings.Repeat("x", 1024)},
				ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
			},
			Data:       map[string]string{"payload": strings.Repeat("x", 1024*1024)},
			BinaryData: map[string][]byte{"payload": []byte("binary")},
			Immutable:  ptr.To(true),
		}
		schedulerCache, stopCh := setupCacheWithObjects(true, []runtime.Object{configMap})
		defer close(stopCh)
		sc := schedulerCache.(*SchedulerCache)
		lister := sc.KubeInformerFactory().Core().V1().ConfigMaps().Lister().ConfigMaps(configMap.Namespace)
		stored, err := lister.Get(configMap.Name)
		Expect(err).NotTo(HaveOccurred())
		Expect(stored).To(Equal(&v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: configMap.Name, Namespace: configMap.Namespace, UID: configMap.UID,
			ResourceVersion: configMap.ResourceVersion,
		}}))
		original, err := sc.kubeClient.CoreV1().ConfigMaps(configMap.Namespace).Get(context.Background(), configMap.Name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(original).To(Equal(configMap))

		snapshot, err := sc.Snapshot()
		Expect(err).NotTo(HaveOccurred())
		info := configmap_info.NewConfigMapInfo(configMap)
		Expect(snapshot.ConfigMaps).To(HaveLen(1))
		Expect(snapshot.ConfigMaps[info.UID]).To(Equal(info))
		_, err = sc.KubeInformerFactory().Core().V1().ConfigMaps().Lister().ConfigMaps("other").Get(configMap.Name)
		Expect(errors.IsNotFound(err)).To(BeTrue())

		updated := original.DeepCopy()
		updated.ResourceVersion = "2"
		updated.Data["payload"] = "updated"
		_, err = sc.kubeClient.CoreV1().ConfigMaps(updated.Namespace).Update(context.Background(), updated, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			stored, err := lister.Get(updated.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(stored.ResourceVersion).To(Equal("2"))
			g.Expect(stored.Data).To(BeNil())
			g.Expect(stored.BinaryData).To(BeNil())
		}).Should(Succeed())

		err = sc.kubeClient.CoreV1().ConfigMaps(configMap.Namespace).Delete(context.Background(), configMap.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() bool {
			_, err := lister.Get(configMap.Name)
			return errors.IsNotFound(err)
		}).Should(BeTrue())
		snapshot, err = sc.Snapshot()
		Expect(err).NotTo(HaveOccurred())
		Expect(snapshot.ConfigMaps).To(BeEmpty())
	})

	It("leaves other object types unchanged", func() {
		pod := &v1.Pod{}
		transformed, err := compactSchedulerConfigMap(pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(transformed).To(BeIdenticalTo(pod))
	})
})
