// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAdmission(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Admission type suite")
}

var _ = Describe("Admission", func() {
	It("Set Defaults", func(ctx context.Context) {
		Admission := &Admission{}
		var replicaCount int32
		replicaCount = 1
		Admission.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(*Admission.Service.Enabled).To(Equal(true))
		Expect(*Admission.Service.Image.Name).To(Equal("admission"))
		Expect(*Admission.Replicas).To(Equal(int32(1)))
		Expect(*Admission.GPUPodRuntimeClassName).To(Equal(constants.DefaultRuntimeClassName))
	})
	It("Set Defaults with replica count", func(ctx context.Context) {
		Admission := &Admission{}
		var replicaCount int32
		replicaCount = 3
		Admission.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(*Admission.Replicas).To(Equal(int32(3)))
	})
})
