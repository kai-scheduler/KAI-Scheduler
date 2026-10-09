// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/configmap_info"
)

var _ = Describe("compactConfigMap", func() {
	configMap := func() *v1.ConfigMap {
		return &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "settings",
				Namespace:       "team-a",
				UID:             "settings-uid",
				ResourceVersion: "42",
				Labels:          map[string]string{"app": "trainer"},
				Annotations:     map[string]string{"kubectl.kubernetes.io/last-applied-configuration": "{...}"},
				ManagedFields:   []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
			},
			Data:       map[string]string{"payload": "large"},
			BinaryData: map[string][]byte{"blob": []byte("large")},
		}
	}

	It("keeps only the identity of the configmap", func() {
		transformed, err := compactConfigMap(configMap())
		Expect(err).NotTo(HaveOccurred())

		Expect(transformed).To(Equal(&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "settings",
				Namespace:       "team-a",
				UID:             "settings-uid",
				ResourceVersion: "42",
			},
		}))
	})

	It("keeps the fields the scheduler snapshot reads", func() {
		original := configMap()
		transformed, err := compactConfigMap(original)
		Expect(err).NotTo(HaveOccurred())

		Expect(configmap_info.NewConfigMapInfo(transformed.(*v1.ConfigMap))).To(
			Equal(configmap_info.NewConfigMapInfo(original)))
	})

	It("passes through non-configmap objects", func() {
		pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod"}}

		transformed, err := compactConfigMap(pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(transformed).To(BeIdenticalTo(pod))
	})
})
