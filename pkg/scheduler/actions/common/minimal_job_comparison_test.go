// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

type podTestsInfo struct {
	resources v1.ResourceList
	status    v1.PodPhase
}

func TestMinimalJobComparison(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Minimal Job Comparison Suite")
}

var _ = Describe("MinimalJobComparison", func() {
	var (
		smallestFailedJobs *MinimalJobRepresentatives
	)

	Describe("IsEasierToSchedule", func() {
		BeforeEach(func() {
			smallestFailedJobs = NewMinimalJobRepresentatives()
		})

		Context("single pod", func() {

			for testName, testCase := range map[string]struct {
				representativeResources v1.ResourceList
				jobResources            v1.ResourceList
				result                  bool
			}{
				"empty job": {
					representativeResources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
					jobResources:            v1.ResourceList{},
					result:                  true,
				},
				"empty representative": {
					representativeResources: v1.ResourceList{},
					jobResources:            v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
					result:                  false,
				},
				"equal jobs": {
					representativeResources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
					jobResources:            v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
					result:                  false,
				},
				"cpu only over": {
					representativeResources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
					jobResources:            v1.ResourceList{v1.ResourceCPU: resource.MustParse("200m")},
					result:                  false,
				},
				"cpu only under": {
					representativeResources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
					jobResources:            v1.ResourceList{v1.ResourceCPU: resource.MustParse("50m")},
					result:                  true,
				},
				"memory only over": {
					representativeResources: v1.ResourceList{v1.ResourceMemory: resource.MustParse("100Mi")},
					jobResources:            v1.ResourceList{v1.ResourceMemory: resource.MustParse("200Mi")},
					result:                  false,
				},
				"memory only under": {
					representativeResources: v1.ResourceList{v1.ResourceMemory: resource.MustParse("100Mi")},
					jobResources:            v1.ResourceList{v1.ResourceMemory: resource.MustParse("50Mi")},
					result:                  true,
				},
				"cpu over memory under": {
					representativeResources: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("100m"),
						v1.ResourceMemory: resource.MustParse("100Mi"),
					},
					jobResources: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("200m"),
						v1.ResourceMemory: resource.MustParse("50Mi"),
					},
					result: false,
				},
				"cpu under memory over": {
					representativeResources: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("100m"),
						v1.ResourceMemory: resource.MustParse("100Mi"),
					},
					jobResources: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("50m"),
						v1.ResourceMemory: resource.MustParse("200Mi"),
					},
					result: false,
				},

				// gpu
				"gpu only over": {
					representativeResources: v1.ResourceList{
						resource_info.GPUResourceName: resource.MustParse("1"),
					},
					jobResources: v1.ResourceList{
						resource_info.GPUResourceName: resource.MustParse("2"),
					},
					result: false,
				},
				"gpu only under": {
					representativeResources: v1.ResourceList{
						resource_info.GPUResourceName: resource.MustParse("2"),
					},
					jobResources: v1.ResourceList{
						resource_info.GPUResourceName: resource.MustParse("1"),
					},
					result: true,
				},
				"gpu fraction over": {
					representativeResources: v1.ResourceList{
						resource_info.GPUResourceName: resource.MustParse("0.25"),
					},
					jobResources: v1.ResourceList{
						resource_info.GPUResourceName: resource.MustParse("0.5"),
					},
					result: false,
				},
				"gpu fraction under": {
					representativeResources: v1.ResourceList{
						resource_info.GPUResourceName: resource.MustParse("0.5"),
					},
					jobResources: v1.ResourceList{
						resource_info.GPUResourceName: resource.MustParse("0.25"),
					},
					result: true,
				},
			} {
				testName := testName
				testCase := testCase
				It(testName, func() {
					updateRepresentative(smallestFailedJobs, copyResources(testCase.representativeResources)...)
					otherJob := jobFromRequirements(copyResources(testCase.jobResources)...)
					easier, _ := smallestFailedJobs.IsEasierToSchedule(otherJob)
					Expect(easier).To(Equal(testCase.result))
				})
			}
		})

		Context("multiple pods", func() {
			for testName, testCase := range map[string]struct {
				representativeResources []v1.ResourceList
				jobResources            []v1.ResourceList
				result                  bool
			}{
				"empty representatives": {
					representativeResources: []v1.ResourceList{{}, {}},
					jobResources: []v1.ResourceList{
						{v1.ResourceCPU: resource.MustParse("100m")},
						{v1.ResourceCPU: resource.MustParse("100m")},
					},
					result: false,
				},
				"empty job": {
					representativeResources: []v1.ResourceList{
						{v1.ResourceCPU: resource.MustParse("100m")},
						{v1.ResourceCPU: resource.MustParse("100m")},
					},
					jobResources: []v1.ResourceList{{}, {}},
					result:       true,
				},
				"only one pod is small, other is equal": {
					representativeResources: []v1.ResourceList{
						{v1.ResourceCPU: resource.MustParse("100m")},
						{v1.ResourceCPU: resource.MustParse("100m")},
					},
					jobResources: []v1.ResourceList{
						{v1.ResourceCPU: resource.MustParse("100m")},
						{v1.ResourceCPU: resource.MustParse("50m")},
					},
					result: true,
				},
				"total smaller, but one pod is bigger": {
					representativeResources: []v1.ResourceList{
						{v1.ResourceCPU: resource.MustParse("1000m")},
						{v1.ResourceCPU: resource.MustParse("100m")},
						{v1.ResourceCPU: resource.MustParse("100m")},
					},
					jobResources: []v1.ResourceList{
						{v1.ResourceCPU: resource.MustParse("500m")},
						{v1.ResourceCPU: resource.MustParse("500m")},
						{v1.ResourceCPU: resource.MustParse("500m")},
					},
					result: true,
				},
			} {
				testName := testName
				testCase := testCase
				It(testName, func() {
					updateRepresentative(smallestFailedJobs, copyResources(testCase.representativeResources...)...)
					otherJob := jobFromRequirements(copyResources(testCase.jobResources...)...)
					easier, _ := smallestFailedJobs.IsEasierToSchedule(otherJob)
					Expect(easier).To(Equal(testCase.result))
				})
			}

		})

		Context("multiple pods, different pod statuses", func() {

			for testName, testCase := range map[string]struct {
				representativeResources []podTestsInfo
				jobResources            []podTestsInfo
				result                  bool
			}{
				"representative has a failing pod": {
					representativeResources: []podTestsInfo{
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
							status:    v1.PodPending,
						},
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
							status:    v1.PodFailed,
						},
					},
					jobResources: []podTestsInfo{
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
							status:    v1.PodPending,
						},
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
							status:    v1.PodPending,
						},
					},
					result: false,
				},
				"same signature, job resources have one pod running": {
					representativeResources: []podTestsInfo{
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
							status:    v1.PodPending,
						},
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
							status:    v1.PodPending,
						},
					},
					jobResources: []podTestsInfo{
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
							status:    v1.PodPending,
						},
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
							status:    v1.PodFailed,
						},
					},
					result: true,
				},
				"same signature, job resources have one pod running. The running pod is bigger": {
					representativeResources: []podTestsInfo{
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
							status:    v1.PodPending,
						},
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
							status:    v1.PodPending,
						},
					},
					jobResources: []podTestsInfo{
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("100m")},
							status:    v1.PodPending,
						},
						{
							resources: v1.ResourceList{v1.ResourceCPU: resource.MustParse("150m")},
							status:    v1.PodRunning,
						},
					},
					result: true,
				},
			} {
				testName := testName
				testCase := testCase
				It(testName, func() {
					smallestFailedJobs.UpdateRepresentative(JobFromPodSignatureData(testCase.representativeResources))
					otherJob := JobFromPodSignatureData(testCase.jobResources)
					easier, _ := smallestFailedJobs.IsEasierToSchedule(otherJob)
					Expect(easier).To(Equal(testCase.result))
				})
			}

		})
	})

	Describe("UpdateRepresentative", func() {
		BeforeEach(func() {
			smallestFailedJobs = NewMinimalJobRepresentatives()
		})
		Context("multiple pods", func() {
			for testName, testCase := range map[string]struct {
				representativeResources []v1.ResourceList
				jobResources            []v1.ResourceList
				result                  bool
			}{
				"only one pod is small, other is equal": {
					representativeResources: []v1.ResourceList{
						{v1.ResourceCPU: resource.MustParse("100m")},
						{v1.ResourceCPU: resource.MustParse("100m")},
					},
					jobResources: []v1.ResourceList{
						{v1.ResourceCPU: resource.MustParse("100m")},
						{v1.ResourceCPU: resource.MustParse("50m")},
					},
					result: true,
				},
				"total smaller, but one pod is bigger": {
					representativeResources: []v1.ResourceList{
						{v1.ResourceCPU: resource.MustParse("1000m")},
						{v1.ResourceCPU: resource.MustParse("100m")},
						{v1.ResourceCPU: resource.MustParse("100m")},
					},
					jobResources: []v1.ResourceList{
						{v1.ResourceCPU: resource.MustParse("500m")},
						{v1.ResourceCPU: resource.MustParse("500m")},
						{v1.ResourceCPU: resource.MustParse("500m")},
					},
					result: false,
				},
			} {
				testName := testName
				testCase := testCase
				It(testName, func() {
					updateRepresentative(smallestFailedJobs, testCase.representativeResources...)
					otherJob := jobFromRequirements(testCase.jobResources...)
					smallestFailedJobs.UpdateRepresentative(otherJob)
					key := otherJob.GetSchedulingConstraintsSignature()
					if testCase.result {
						Expect(smallestFailedJobs.representatives[key]).To(Equal(otherJob))
					} else {
						Expect(smallestFailedJobs.representatives[key]).NotTo(Equal(otherJob))
					}
				})
			}

		})
	})
})

func updateRepresentative(
	representatives *MinimalJobRepresentatives, resources ...v1.ResourceList,
) {
	currentJob := jobFromRequirements(resources...)
	representatives.UpdateRepresentative(currentJob)
}

func jobFromRequirements(requirements ...v1.ResourceList) *podgroup_info.PodGroupInfo {
	jobTestInfo := make([]podTestsInfo, len(requirements))
	for index, req := range requirements {
		jobTestInfo[index] = podTestsInfo{req, v1.PodPending}
	}
	return JobFromPodSignatureData(jobTestInfo)
}

func copyResources(resources ...v1.ResourceList) []v1.ResourceList {
	result := make([]v1.ResourceList, len(resources))
	for index, resource := range resources {
		result[index] = resource.DeepCopy()
	}
	return result
}

func JobFromPodSignatureData(jobTestInfo []podTestsInfo) *podgroup_info.PodGroupInfo {
	tasks := make([]*pod_info.PodInfo, len(jobTestInfo))
	for index, podTestInfo := range jobTestInfo {
		tasks[index] = pod_info.NewTaskInfo(
			common_info.BuildPod(
				fmt.Sprintf("test-%v", index), "test", "",
				podTestInfo.status, podTestInfo.resources.DeepCopy(),
				[]metav1.OwnerReference{},
				map[string]string{},
				map[string]string{
					commonconstants.PodGroupAnnotationForPod: "test",
				},
			), nil, resource_info.NewResourceVectorMap())
	}
	return podgroup_info.NewPodGroupInfo(
		"test", tasks...,
	)
}
