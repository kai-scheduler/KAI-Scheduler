// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"

	kaicommon "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
)

func TestScheduler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scheduler type suite")
}

var _ = Describe("Scheduler", func() {
	It("Set Defaults when Service is nil", func(ctx context.Context) {
		scheduler := &Scheduler{}
		var replicaCount int32 = 1
		scheduler.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(scheduler.Service).NotTo(BeNil())
		Expect(*scheduler.Service.Enabled).To(Equal(true))
		Expect(*scheduler.Service.Image.Name).To(Equal("scheduler"))
		Expect(scheduler.Service.Resources.Requests[v1.ResourceCPU]).To(Equal(resource.MustParse("250m")))
		Expect(scheduler.Service.Resources.Requests[v1.ResourceMemory]).To(Equal(resource.MustParse("512Mi")))
		Expect(scheduler.Service.Resources.Limits[v1.ResourceCPU]).To(Equal(resource.MustParse("700m")))
		Expect(scheduler.Service.Resources.Limits[v1.ResourceMemory]).To(Equal(resource.MustParse("512Mi")))
	})

	It("Set Defaults with GOGC unset", func(ctx context.Context) {
		scheduler := &Scheduler{}
		var replicaCount int32 = 2
		scheduler.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(*scheduler.GOGC).To(Equal(400))
	})

	It("Set Defaults with SchedulerService unset", func(ctx context.Context) {
		scheduler := &Scheduler{}
		var replicaCount int32 = 3
		scheduler.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(scheduler.SchedulerService).NotTo(BeNil())
		Expect(*scheduler.SchedulerService.Type).To(Equal(v1.ServiceTypeClusterIP))
		Expect(*scheduler.SchedulerService.Port).To(Equal(8080))
		Expect(*scheduler.SchedulerService.TargetPort).To(Equal(8080))
	})

	It("Replicas set to replicaCount value", func(ctx context.Context) {
		scheduler := &Scheduler{}
		var replicaCount int32 = 4
		scheduler.SetDefaultsWhereNeeded(&replicaCount, nil)
		Expect(*scheduler.Replicas).To(Equal(int32(4)))
	})

	It("Replicas default to 1 when replicaCount is nil", func(ctx context.Context) {
		scheduler := &Scheduler{}
		var replicaCount *int32
		scheduler.SetDefaultsWhereNeeded(replicaCount, nil)
		Expect(*scheduler.Replicas).To(Equal(int32(1)))
	})

	It("inherits globalVPA when VPA is nil", func(ctx context.Context) {
		scheduler := &Scheduler{}
		mode := vpav1.UpdateModeOff
		globalVPA := &kaicommon.VPASpec{
			Enabled:      ptr.To(true),
			UpdatePolicy: &vpav1.PodUpdatePolicy{UpdateMode: &mode},
		}
		scheduler.SetDefaultsWhereNeeded(ptr.To(int32(1)), globalVPA)

		Expect(scheduler.VPA).To(Equal(globalVPA))
		Expect(*scheduler.VPA.UpdatePolicy.UpdateMode).To(Equal(vpav1.UpdateModeOff))
	})

	It("applies defaults to local VPA when UpdateMode is nil", func(ctx context.Context) {
		scheduler := &Scheduler{
			VPA: &kaicommon.VPASpec{
				Enabled:      ptr.To(true),
				UpdatePolicy: &vpav1.PodUpdatePolicy{},
			},
		}
		scheduler.SetDefaultsWhereNeeded(ptr.To(int32(1)), nil)

		Expect(scheduler.VPA.UpdatePolicy.UpdateMode).NotTo(BeNil())
		Expect(*scheduler.VPA.UpdatePolicy.UpdateMode).To(Equal(vpav1.UpdateModeInPlaceOrRecreate))
	})

	It("does not call SetDefaultsWhereNeeded when VPA remains nil", func(ctx context.Context) {
		scheduler := &Scheduler{}
		scheduler.SetDefaultsWhereNeeded(ptr.To(int32(1)), nil)

		Expect(scheduler.VPA).To(BeNil())
	})
})
