// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Resources", func() {
	It("Set Defaults", func(ctx context.Context) {
		resources := &Resources{}
		resources.SetDefaultsWhereNeeded()
		Expect(resources.Requests[v1.ResourceCPU]).To(Equal(resource.MustParse("50m")))
		Expect(resources.Requests[v1.ResourceMemory]).To(Equal(resource.MustParse("256Mi")))
		Expect(resources.Limits[v1.ResourceCPU]).To(Equal(resource.MustParse("100m")))
		Expect(resources.Limits[v1.ResourceMemory]).To(Equal(resource.MustParse("512Mi")))
	})
})
