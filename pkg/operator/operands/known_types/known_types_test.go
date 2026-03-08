// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package known_types

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	kaiv1 "github.com/kai-scheduler/KAI-Scheduler/pkg/apis/kai/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKnownTypes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "KnownTypes Suite")
}

var _ = Describe("KnownTypes", func() {
	Context("Owner Keys For Indexing", func() {
		It("should return the correct owner key", func() {
			obj := &kaiv1.Config{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Config",
					APIVersion: kaiv1.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: SingletonInstanceName,
					UID:  SingletonInstanceName,
				},
			}

			owner := &metav1.OwnerReference{
				APIVersion: kaiv1.GroupVersion.String(),
				Kind:       "Config",
				Name:       SingletonInstanceName,
				UID:        SingletonInstanceName,
			}

			Expect(getOwnerKey(owner)).To(Equal(getReconcilerKey(obj)))
		})
	})
})
