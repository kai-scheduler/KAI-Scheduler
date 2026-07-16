// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topologyhooks

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1alpha1 "github.com/kai-scheduler/api/kai/v1alpha1"
)

func TestTopologyValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Topology Validator Suite")
}

func topologyWith(levels ...kaiv1alpha1.TopologyLevel) *kaiv1alpha1.Topology {
	return &kaiv1alpha1.Topology{
		ObjectMeta: metav1.ObjectMeta{Name: "network"},
		Spec:       kaiv1alpha1.TopologySpec{Levels: levels},
	}
}

var _ = Describe("Topology Validator", func() {
	var (
		ctx       context.Context
		validator TopologyValidator
	)

	BeforeEach(func() {
		ctx = context.Background()
		validator = NewTopologyValidator()
	})

	Context("ValidateCreate", func() {
		It("should allow levels without aliases", func() {
			warnings, err := validator.ValidateCreate(ctx, topologyWith(
				kaiv1alpha1.TopologyLevel{NodeLabel: "accelerator.nvidia.com/rack"},
				kaiv1alpha1.TopologyLevel{NodeLabel: "kubernetes.io/hostname"},
			))
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should allow unique aliases", func() {
			warnings, err := validator.ValidateCreate(ctx, topologyWith(
				kaiv1alpha1.TopologyLevel{NodeLabel: "accelerator.nvidia.com/rack", Alias: "rack"},
				kaiv1alpha1.TopologyLevel{NodeLabel: "kubernetes.io/hostname", Alias: "node"},
			))
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should reject duplicate aliases", func() {
			_, err := validator.ValidateCreate(ctx, topologyWith(
				kaiv1alpha1.TopologyLevel{NodeLabel: "accelerator.nvidia.com/rack", Alias: "rack"},
				kaiv1alpha1.TopologyLevel{NodeLabel: "kubernetes.io/hostname", Alias: "rack"},
			))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unique"))
			Expect(err.Error()).To(ContainSubstring("rack"))
		})

		It("should reject an alias that collides with a nodeLabel", func() {
			_, err := validator.ValidateCreate(ctx, topologyWith(
				kaiv1alpha1.TopologyLevel{NodeLabel: "accelerator.nvidia.com/rack", Alias: "kubernetes.io/hostname"},
				kaiv1alpha1.TopologyLevel{NodeLabel: "kubernetes.io/hostname", Alias: "node"},
			))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must not equal a nodeLabel"))
		})
	})

	Context("ValidateUpdate", func() {
		It("should allow re-pointing an alias to a different level", func() {
			oldT := topologyWith(
				kaiv1alpha1.TopologyLevel{NodeLabel: "accelerator.nvidia.com/rack", Alias: "rack"},
				kaiv1alpha1.TopologyLevel{NodeLabel: "kubernetes.io/hostname"},
			)
			newT := topologyWith(
				kaiv1alpha1.TopologyLevel{NodeLabel: "accelerator.nvidia.com/rack"},
				kaiv1alpha1.TopologyLevel{NodeLabel: "kubernetes.io/hostname", Alias: "rack"},
			)
			warnings, err := validator.ValidateUpdate(ctx, oldT, newT)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("should reject an update that introduces a duplicate alias", func() {
			oldT := topologyWith(kaiv1alpha1.TopologyLevel{NodeLabel: "accelerator.nvidia.com/rack", Alias: "rack"})
			newT := topologyWith(
				kaiv1alpha1.TopologyLevel{NodeLabel: "accelerator.nvidia.com/rack", Alias: "rack"},
				kaiv1alpha1.TopologyLevel{NodeLabel: "kubernetes.io/hostname", Alias: "rack"},
			)
			_, err := validator.ValidateUpdate(ctx, oldT, newT)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unique"))
		})
	})

	Context("ValidateDelete", func() {
		It("should always allow deletion", func() {
			warnings, err := validator.ValidateDelete(ctx, topologyWith(
				kaiv1alpha1.TopologyLevel{NodeLabel: "accelerator.nvidia.com/rack", Alias: "rack"},
				kaiv1alpha1.TopologyLevel{NodeLabel: "kubernetes.io/hostname", Alias: "rack"},
			))
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})
})
