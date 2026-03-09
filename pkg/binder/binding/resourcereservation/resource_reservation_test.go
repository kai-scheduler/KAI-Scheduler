// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resourcereservation

import (
	"context"
	"fmt"
	"testing"
	"time"

	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
)

const (
	resourceReservationNameSpace      = "kai-resource-reservation"
	resourceReservationServiceAccount = resourceReservationNameSpace
	resourceReservationAppLabelValue  = resourceReservationNameSpace
	scalingPodsNamespace              = "kai-scale-adjust"
)

func TestResourceReservation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "resource reservation")
}

func nodeNameIndexer(rawObj runtimeClient.Object) []string {
	pod := rawObj.(*v1.Pod)
	return []string{pod.Spec.NodeName}
}

func initializeTestService(
	client runtimeClient.WithWatch,
) *service {
	service := NewService(false, client, "", 40*time.Millisecond,
		resourceReservationNameSpace, resourceReservationServiceAccount, resourceReservationAppLabelValue, scalingPodsNamespace, constants.DefaultRuntimeClassName,
		nil, nil, nil)

	return service
}

var _ = Describe("ResourceReservationService", func() {
	const (
		existingGroup     = "bla-bla"
		nodeName          = "node-1"
		gpuGroup          = "gpu-group"
		gpuGroup2         = "gpu-group-2"
		failedToCreatePod = "failed to create reservation pod"
	)
	var (
		groupLabels = map[string]string{
			constants.GPUGroup: gpuGroup,
		}
		runningStatus = v1.PodStatus{
			Phase: v1.PodRunning,
		}
		exampleReservationPod = &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example",
				Namespace: resourceReservationNameSpace,
				Labels:    groupLabels,
			},
			Spec: v1.PodSpec{
				NodeName: nodeName,
			},
			Status: runningStatus,
		}
		exampleRunningJob = &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job-1-0-0",
				Namespace: "my-ns",
				Labels:    groupLabels,
			},
			Spec: v1.PodSpec{
				NodeName: nodeName,
			},
			Status: runningStatus,
		}
	)

	Context("ReserveGpuDevice", func() {
		for testName, testData := range map[string]struct {
			reservationPod        *v1.Pod
			groupName             string
			clientInterceptFuncs  interceptor.Funcs
			numReservationPods    int
			expectedGPUIndex      string
			expectedErrorContains string
		}{
			"reservation pod exists": {
				groupName: existingGroup,
				reservationPod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: resourceReservationNameSpace,
						Annotations: map[string]string{
							gpuIndexAnnotationName: "4",
						},
						Labels: map[string]string{
							constants.GPUGroup: existingGroup,
						},
					},
				},
				numReservationPods: 1,
				expectedGPUIndex:   "4",
			},
			"reservation pod missing gpu index annotation": {
				groupName: existingGroup,
				reservationPod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:   resourceReservationNameSpace,
						Annotations: map[string]string{},
						Labels: map[string]string{
							constants.GPUGroup: existingGroup,
						},
					},
				},
				numReservationPods:    1,
				expectedGPUIndex:      unknownGpuIndicator,
				expectedErrorContains: "annotation",
			},
			"error listing pods": {
				groupName: existingGroup,
				clientInterceptFuncs: interceptor.Funcs{
					List: func(ctx context.Context, client runtimeClient.WithWatch, list runtimeClient.ObjectList, opts ...runtimeClient.ListOption) error {
						return fmt.Errorf("failed to list pods")
					},
				},
				expectedGPUIndex:      unknownGpuIndicator,
				expectedErrorContains: "failed to list pods",
			},
			"reservation pod exists only for other group - create a new one": {
				groupName: "other-group",
				reservationPod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: resourceReservationNameSpace,
						Annotations: map[string]string{
							gpuIndexAnnotationName: "4",
						},
						Labels: map[string]string{
							constants.GPUGroup: "some-other-group",
						},
					},
				},
				clientInterceptFuncs: interceptor.Funcs{
					Watch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.ObjectList, opts ...runtimeClient.ListOption) (watch.Interface, error) {
						return exampleMockWatchPod("3", 0), nil
					},
				},
				numReservationPods: 2,
				expectedGPUIndex:   "3",
			},
			"no reservation pod - create new reservation pod - happy flow": {
				clientInterceptFuncs: interceptor.Funcs{
					Watch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.ObjectList, opts ...runtimeClient.ListOption) (watch.Interface, error) {
						return exampleMockWatchPod("5", 0), nil
					},
				},
				expectedGPUIndex:   "5",
				numReservationPods: 1,
			},
			"no reservation pod - create new reservation pod - delay with pod": {
				clientInterceptFuncs: interceptor.Funcs{
					Watch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.ObjectList, opts ...runtimeClient.ListOption) (watch.Interface, error) {
						return exampleMockWatchPod("6", time.Millisecond*100), nil
					},
				},
				expectedGPUIndex:      unknownGpuIndicator,
				expectedErrorContains: "failed waiting for GPU reservation pod to allocate",
				numReservationPods:    0,
			},
			"no reservation pod - create new reservation pod - reservation pod creation fails": {
				clientInterceptFuncs: interceptor.Funcs{
					Create: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object, opts ...runtimeClient.CreateOption) error {
						return fmt.Errorf(failedToCreatePod)
					},
				},
				expectedGPUIndex:      unknownGpuIndicator,
				expectedErrorContains: failedToCreatePod,
			},
			"no reservation pod - create new reservation pod - no gpu index annotation and fail delete reservation pod": {
				clientInterceptFuncs: interceptor.Funcs{
					Delete: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object, opts ...runtimeClient.DeleteOption) error {
						return fmt.Errorf("failed to delete pod")
					},
				},
				numReservationPods:    1,
				expectedGPUIndex:      unknownGpuIndicator,
				expectedErrorContains: "failed waiting for GPU reservation pod to allocate",
			},
			"no reservation pod - create new reservation pod - cluster scale up check fails to list pods": {
				clientInterceptFuncs: interceptor.Funcs{
					List: func(ctx context.Context, client runtimeClient.WithWatch, list runtimeClient.ObjectList, opts ...runtimeClient.ListOption) error {
						listOpts := runtimeClient.ListOptions{}
						listOpts.ApplyOptions(opts)
						if listOpts.Namespace == scalingPodsNamespace {
							return fmt.Errorf("failed to list pods")
						}
						return client.List(ctx, list, opts...)
					},
					Watch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.ObjectList, opts ...runtimeClient.ListOption) (watch.Interface, error) {
						return exampleMockWatchPod("5", 0), nil
					},
				},
				numReservationPods: 1,
				expectedGPUIndex:   "5",
			},
			"no reservation pod - create new reservation pod - cluster scale up is finished": {
				clientInterceptFuncs: interceptor.Funcs{
					Watch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.ObjectList, opts ...runtimeClient.ListOption) (watch.Interface, error) {
						return exampleMockWatchPod("5", 0), nil
					},
				},
				expectedGPUIndex:   "5",
				numReservationPods: 1,
				reservationPod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: scalingPodsNamespace,
						Name:      "scaleup-1",
					},
					Status: v1.PodStatus{
						Conditions: []v1.PodCondition{
							{
								Type:   v1.PodScheduled,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
			},
			"no reservation pod - create new reservation pod - cluster is scaling up": {
				reservationPod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: scalingPodsNamespace,
						Name:      "scaleup-1",
					},
					Status: v1.PodStatus{
						Conditions: []v1.PodCondition{
							{
								Type:   v1.PodScheduled,
								Status: v1.ConditionFalse,
							},
						},
					},
				},
				expectedGPUIndex:      unknownGpuIndicator,
				expectedErrorContains: "scaling up",
			},
			"timeout waiting for gpu index": {
				numReservationPods:    0,
				expectedGPUIndex:      unknownGpuIndicator,
				expectedErrorContains: "failed waiting for GPU",
			},
			"failed to watch reservation pod": {
				clientInterceptFuncs: interceptor.Funcs{
					Watch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.ObjectList, opts ...runtimeClient.ListOption) (watch.Interface, error) {
						return nil, fmt.Errorf("failed to watch")
					},
				},
				expectedGPUIndex:      unknownGpuIndicator,
				expectedErrorContains: "failed waiting for GPU reservation pod to allocate",
			},
			"watch channel closed": {
				clientInterceptFuncs: interceptor.Funcs{
					Watch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.ObjectList, opts ...runtimeClient.ListOption) (watch.Interface, error) {
						return exampleMockWatchPodClosed(), nil
					},
				},
				expectedGPUIndex:      unknownGpuIndicator,
				expectedErrorContains: "failed waiting for GPU reservation pod to allocate",
			},
			"failed to update gpu group": {
				clientInterceptFuncs: interceptor.Funcs{
					Watch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.ObjectList, opts ...runtimeClient.ListOption) (watch.Interface, error) {
						return exampleMockWatchPod("3", 0), nil
					},
					Patch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object, patch runtimeClient.Patch, opts ...runtimeClient.PatchOption) error {
						return fmt.Errorf("failed to patch")
					},
				},
				expectedGPUIndex:      unknownGpuIndicator,
				expectedErrorContains: "failed to patch",
			},
			"failed to update gpu group and fail to delete reservation pod": {
				clientInterceptFuncs: interceptor.Funcs{
					Watch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.ObjectList, opts ...runtimeClient.ListOption) (watch.Interface, error) {
						return exampleMockWatchPod("3", 0), nil
					},
					Patch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object, patch runtimeClient.Patch, opts ...runtimeClient.PatchOption) error {
						return fmt.Errorf("failed to patch")
					},
					Delete: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object, opts ...runtimeClient.DeleteOption) error {
						return fmt.Errorf("failed to delete")
					},
				},
				expectedGPUIndex:      unknownGpuIndicator,
				expectedErrorContains: "failed to patch",
				numReservationPods:    1,
			},
		} {
			testName := testName
			testData := testData
			It(testName, func() {
				fractionPod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "team-a",
						Name:      "fraction-pod",
					},
				}
				podsInCluster := []runtime.Object{fractionPod}
				if testData.reservationPod != nil {
					podsInCluster = append(podsInCluster, testData.reservationPod)
				}
				clientWithObjs := fake.NewClientBuilder().WithRuntimeObjects(podsInCluster...).
					WithIndex(&v1.Pod{}, "spec.nodeName", nodeNameIndexer).Build()
				fakeClient := interceptor.NewClient(clientWithObjs, testData.clientInterceptFuncs)
				rsc := initializeTestService(fakeClient)

				gpuIndex, err := rsc.ReserveGpuDevice(context.TODO(), fractionPod, nodeName, existingGroup)
				Expect(gpuIndex).To(Equal(testData.expectedGPUIndex))
				if testData.expectedErrorContains == "" {
					Expect(err).To(BeNil())
					Expect(fractionPod.Labels[constants.GPUGroup], testData.groupName)
				} else {
					Expect(err).NotTo(BeNil())
					Expect(err.Error()).To(ContainSubstring(testData.expectedErrorContains))
				}
				pods := &v1.PodList{}
				err = clientWithObjs.List(context.Background(), pods,
					runtimeClient.InNamespace(resourceReservationNameSpace),
				)
				Expect(err).To(Succeed())
				Expect(len(pods.Items)).To(Equal(testData.numReservationPods))
				if testData.numReservationPods == 1 {
					Expect(pods.Items[0].Labels[constants.GPUGroup], testData.groupName)
				}
			})
		}
	})

	Context("Sync", func() {
		for testName, testData := range map[string]struct {
			podsInCluster         []runtime.Object
			clientInterceptFuncs  interceptor.Funcs
			podsLeft              int
			expectedErrorContains string
		}{
			"solitary running reservation pod": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
				},
				podsLeft: 0,
			},
			"fail to list pods": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
				},
				clientInterceptFuncs: interceptor.Funcs{
					List: func(ctx context.Context, client runtimeClient.WithWatch, list runtimeClient.ObjectList, opts ...runtimeClient.ListOption) error {
						return fmt.Errorf("failed to list pods")
					},
				},
				podsLeft:              1,
				expectedErrorContains: "failed to list pods",
			},
			"fail to delete pod": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
				},
				clientInterceptFuncs: interceptor.Funcs{
					Delete: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object, opts ...runtimeClient.DeleteOption) error {
						return fmt.Errorf("failed to delete pod")
					},
				},
				podsLeft:              1,
				expectedErrorContains: "failed to delete pod",
			},
		} {
			testName := testName
			testData := testData
			It(testName, func() {
				clientWithObjs := fake.NewClientBuilder().WithRuntimeObjects(testData.podsInCluster...).Build()
				fakeClient := interceptor.NewClient(clientWithObjs, testData.clientInterceptFuncs)
				rsc := initializeTestService(fakeClient)

				err := rsc.Sync(context.TODO())
				if testData.expectedErrorContains == "" {
					Expect(err).To(BeNil())
				} else {
					Expect(err).NotTo(BeNil())
					Expect(err.Error()).To(ContainSubstring(testData.expectedErrorContains))
				}
				pods := &v1.PodList{}
				err = clientWithObjs.List(context.Background(), pods)
				Expect(err).To(Succeed())
				Expect(len(pods.Items)).To(Equal(testData.podsLeft))
			})
		}
	})

	Context("SyncForGpuGroup", func() {
		for testName, testData := range map[string]struct {
			podsInCluster         []runtime.Object
			clientInterceptFuncs  interceptor.Funcs
			podsLeft              int
			expectedErrorContains string
		}{
			"solitary running reservation pod": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
				},
				podsLeft: 0,
			},
			"fail to list pods": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
				},
				clientInterceptFuncs: interceptor.Funcs{
					List: func(ctx context.Context, client runtimeClient.WithWatch, list runtimeClient.ObjectList, opts ...runtimeClient.ListOption) error {
						return fmt.Errorf("failed to list pods")
					},
				},
				podsLeft:              1,
				expectedErrorContains: "failed to list",
			},
			"fail to delete reservation pod": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
				},
				clientInterceptFuncs: interceptor.Funcs{
					Delete: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object, opts ...runtimeClient.DeleteOption) error {
						return fmt.Errorf("failed to delete pod")
					},
				},
				podsLeft:              1,
				expectedErrorContains: "failed to delete",
			},
			"fail to delete non-reserved pod": {
				podsInCluster: []runtime.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					},
				},
				clientInterceptFuncs: interceptor.Funcs{
					Delete: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object, opts ...runtimeClient.DeleteOption) error {
						return fmt.Errorf("failed to delete pod")
					},
				},
				podsLeft:              1,
				expectedErrorContains: "failed to delete",
			},
			"pending pod with gpu group": {
				podsInCluster: []runtime.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
					},
				},
				podsLeft: 1,
			},
			"running reservation pod with running job": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
					exampleRunningJob,
				},
				podsLeft: 2,
			},
			"running reservation pod with finished and failed jobs": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodFailed,
						},
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-2-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodSucceeded,
						},
					},
				},
				podsLeft: 2,
			},
			"running reservation pod with Pending job": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
					},
				},
				podsLeft: 2,
			},
			"single running job with no reservation pod": {
				podsInCluster: []runtime.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-2-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
					},
				},
				podsLeft: 1,
			},
			"running jobs with no reservation pod": {
				podsInCluster: []runtime.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-2-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
					},
				},
				podsLeft: 1,
			},
		} {
			testName := testName
			testData := testData
			It(testName, func() {
				clientWithObjs := fake.NewClientBuilder().WithRuntimeObjects(testData.podsInCluster...).
					WithIndex(&v1.Pod{}, "spec.nodeName", nodeNameIndexer).Build()
				fakeClient := interceptor.NewClient(clientWithObjs, testData.clientInterceptFuncs)
				rsc := initializeTestService(fakeClient)

				err := rsc.SyncForGpuGroup(context.TODO(), gpuGroup)
				if testData.expectedErrorContains == "" {
					Expect(err).To(BeNil())
				} else {
					Expect(err).NotTo(BeNil())
					Expect(err.Error()).To(ContainSubstring(testData.expectedErrorContains))
				}
				pods := &v1.PodList{}
				err = clientWithObjs.List(context.Background(), pods)
				Expect(err).To(Succeed())
				Expect(len(pods.Items)).To(Equal(testData.podsLeft))
			})
		}
	})

	Context("SyncForNode", func() {
		for testName, testData := range map[string]struct {
			podsInCluster         []runtime.Object
			clientInterceptFuncs  interceptor.Funcs
			podsLeft              int
			expectedErrorContains string
		}{
			"solitary running reservation pod": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
				},
				podsLeft: 0,
			},
			"fail to list pods on node": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
				},
				clientInterceptFuncs: interceptor.Funcs{
					List: func(ctx context.Context, client runtimeClient.WithWatch, list runtimeClient.ObjectList, opts ...runtimeClient.ListOption) error {
						return fmt.Errorf("failed to list pods")
					},
				},
				podsLeft:              1,
				expectedErrorContains: "failed to list",
			},
			"fail to delete reservation pod": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
				},
				clientInterceptFuncs: interceptor.Funcs{
					Delete: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object, opts ...runtimeClient.DeleteOption) error {
						return fmt.Errorf("failed to delete pod")
					},
				},
				podsLeft:              1,
				expectedErrorContains: "failed to delete",
			},
			"fail to delete non-reserved pod": {
				podsInCluster: []runtime.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					},
				},
				clientInterceptFuncs: interceptor.Funcs{
					Delete: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object, opts ...runtimeClient.DeleteOption) error {
						return fmt.Errorf("failed to delete pod")
					},
				},
				podsLeft:              1,
				expectedErrorContains: "failed to delete",
			},
			"pending pod with gpu group": {
				podsInCluster: []runtime.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
					},
				},
				podsLeft: 1,
			},
			"running reservation pod with running job": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
					exampleRunningJob,
				},
				podsLeft: 2,
			},
			"running reservation pod with running multi fractions job": {
				podsInCluster: []runtime.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "example",
							Namespace: resourceReservationNameSpace,
							Labels: map[string]string{
								constants.GPUGroup: gpuGroup,
							},
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: runningStatus,
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "example2",
							Namespace: resourceReservationNameSpace,
							Labels: map[string]string{
								constants.GPUGroup: gpuGroup2,
							},
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: runningStatus,
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels: map[string]string{
								constants.MultiGpuGroupLabelPrefix + gpuGroup:  gpuGroup,
								constants.MultiGpuGroupLabelPrefix + gpuGroup2: gpuGroup2,
							},
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: runningStatus,
					},
				},
				podsLeft: 3,
			},
			"running reservation pod with finished and failed jobs": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodFailed,
						},
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-2-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodSucceeded,
						},
					},
				},
				podsLeft: 2,
			},
			"running reservation pod with Pending job": {
				podsInCluster: []runtime.Object{
					exampleReservationPod,
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
					},
				},
				podsLeft: 2,
			},
			"single running job with no reservation pod": {
				podsInCluster: []runtime.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-2-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
					},
				},
				podsLeft: 1,
			},
			"running jobs with no reservation pod": {
				podsInCluster: []runtime.Object{
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-1-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					},
					&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "job-2-0-0",
							Namespace: "my-ns",
							Labels:    groupLabels,
						},
						Spec: v1.PodSpec{
							NodeName: nodeName,
						},
						Status: v1.PodStatus{
							Phase: v1.PodPending,
						},
					},
				},
				podsLeft: 1,
			},
		} {
			testName := testName
			testData := testData
			It(testName, func() {
				clientWithObjs := fake.NewClientBuilder().WithRuntimeObjects(testData.podsInCluster...).
					WithIndex(&v1.Pod{}, "spec.nodeName", nodeNameIndexer).Build()
				fakeClient := interceptor.NewClient(clientWithObjs, testData.clientInterceptFuncs)
				rsc := initializeTestService(fakeClient)

				err := rsc.SyncForNode(context.TODO(), nodeName)
				if testData.expectedErrorContains == "" {
					Expect(err).To(BeNil())
				} else {
					Expect(err).NotTo(BeNil())
					Expect(err.Error()).To(ContainSubstring(testData.expectedErrorContains))
				}
				pods := &v1.PodList{}
				err = clientWithObjs.List(context.Background(), pods)
				Expect(err).To(Succeed())
				Expect(len(pods.Items)).To(Equal(testData.podsLeft))
			})
		}
	})

	Context("createResourceReservationPod", func() {
		It("should create a pod with the correct RuntimeClassName and metadata", func() {
			customRuntime := "custom-runtime"
			rsc := &service{
				namespace:           "kai-resource-reservation",
				appLabelValue:       "kai-reservation",
				serviceAccountName:  "kai-sa",
				reservationPodImage: "nvidia/kai-reservation:latest",
				kubeClient:          fake.NewClientBuilder().Build(),
				runtimeClassName:    customRuntime,
			}

			resources := v1.ResourceRequirements{
				Limits: v1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
				},
			}

			podName := "reservation-test"
			gpuGroup := "test-group"
			nodeName := "node-test"

			pod, err := rsc.createResourceReservationPod(nodeName, gpuGroup, podName, resources)
			Expect(err).To(BeNil())
			Expect(pod).NotTo(BeNil())

			// Check metadata
			Expect(pod.Name).To(Equal(podName))
			Expect(pod.Namespace).To(Equal("kai-resource-reservation"))
			Expect(pod.Labels[constants.AppLabelName]).To(Equal("kai-reservation"))
			Expect(pod.Labels[constants.GPUGroup]).To(Equal(gpuGroup))

			// PodSpec checks
			Expect(pod.Spec.NodeName).To(Equal(nodeName))
			Expect(pod.Spec.RuntimeClassName).NotTo(BeNil())
			if pod.Spec.RuntimeClassName != nil {
				Expect(*pod.Spec.RuntimeClassName).To(Equal(customRuntime))
			}
			Expect(pod.Spec.ServiceAccountName).To(Equal("kai-sa"))

			// Check container
			Expect(len(pod.Spec.Containers)).To(Equal(1))
			container := pod.Spec.Containers[0]
			Expect(container.Name).To(Equal("resource-reservation"))
			Expect(container.Image).To(Equal("nvidia/kai-reservation:latest"))
			Expect(container.ImagePullPolicy).To(Equal(v1.PullIfNotPresent))
			Expect(container.Resources).To(Equal(resources))

			// Check env vars
			podNameEnv := v1.EnvVar{
				Name: "POD_NAME",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			}
			podNamespaceEnv := v1.EnvVar{
				Name: "POD_NAMESPACE",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			}
			Expect(container.Env).To(ContainElement(Equal(podNameEnv)))
			Expect(container.Env).To(ContainElement(Equal(podNamespaceEnv)))
		})
	})

	Context("RemovePodGpuGroupsConnection", func() {
		for testName, testData := range map[string]struct {
			pod                   *v1.Pod
			clientInterceptFuncs  interceptor.Funcs
			expectedLabels        map[string]string
			expectedErrorContains string
		}{
			"pod with no multi-gpu-group labels": {
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job-1-0-0",
						Namespace: "my-ns",
						Labels: map[string]string{
							"app":              "test",
							constants.GPUGroup: gpuGroup,
						},
					},
				},
				expectedLabels: map[string]string{
					"app": "test",
				},
			},
			"pod with one multi-gpu-group label": {
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job-1-0-0",
						Namespace: "my-ns",
						Labels: map[string]string{
							"app": "test",
							constants.MultiGpuGroupLabelPrefix + gpuGroup: gpuGroup,
						},
					},
				},
				expectedLabels: map[string]string{
					"app": "test",
				},
			},
			"pod with multiple multi-gpu-group labels": {
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job-1-0-0",
						Namespace: "my-ns",
						Labels: map[string]string{
							"app": "test",
							constants.MultiGpuGroupLabelPrefix + gpuGroup:  gpuGroup,
							constants.MultiGpuGroupLabelPrefix + gpuGroup2: gpuGroup2,
						},
					},
				},
				expectedLabels: map[string]string{
					"app": "test",
				},
			},
			"pod with mixed labels - only removes multi-gpu-group labels": {
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job-1-0-0",
						Namespace: "my-ns",
						Labels: map[string]string{
							"app":              "test",
							constants.GPUGroup: gpuGroup,
							"other-label":      "value",
							constants.MultiGpuGroupLabelPrefix + gpuGroup:  gpuGroup,
							constants.MultiGpuGroupLabelPrefix + gpuGroup2: gpuGroup2,
						},
					},
				},
				expectedLabels: map[string]string{
					"app":         "test",
					"other-label": "value",
				},
			},
			"pod with nil labels": {
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job-1-0-0",
						Namespace: "my-ns",
						Labels:    nil,
					},
				},
				expectedLabels: nil,
			},
			"pod with no gpu-group labels": {
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job-1-0-0",
						Namespace: "my-ns",
						Labels: map[string]string{
							"app": "test",
						},
					},
				},
				expectedLabels: map[string]string{
					"app": "test",
				},
			},
			"patch fails": {
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job-1-0-0",
						Namespace: "my-ns",
						Labels: map[string]string{
							constants.MultiGpuGroupLabelPrefix + gpuGroup: gpuGroup,
						},
					},
				},
				clientInterceptFuncs: interceptor.Funcs{
					Patch: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object, patch runtimeClient.Patch, opts ...runtimeClient.PatchOption) error {
						return fmt.Errorf("failed to patch pod")
					},
				},
				expectedErrorContains: "failed to patch pod",
			},
		} {
			testData := testData
			It(testName, func() {
				podsInCluster := []runtime.Object{testData.pod}
				clientWithObjs := fake.NewClientBuilder().WithRuntimeObjects(podsInCluster...).Build()
				fakeClient := interceptor.NewClient(clientWithObjs, testData.clientInterceptFuncs)
				rsc := initializeTestService(fakeClient)

				err := rsc.RemovePodGpuGroupsConnection(context.TODO(), testData.pod)
				if testData.expectedErrorContains == "" {
					Expect(err).To(BeNil())
					// Fetch the pod from the fake client to verify labels were updated
					updatedPod := &v1.Pod{}
					err = clientWithObjs.Get(context.Background(), runtimeClient.ObjectKey{
						Name:      testData.pod.Name,
						Namespace: testData.pod.Namespace,
					}, updatedPod)
					Expect(err).To(Succeed())
					Expect(updatedPod.Labels).To(Equal(testData.expectedLabels))
				} else {
					Expect(err).NotTo(BeNil())
					Expect(err.Error()).To(ContainSubstring(testData.expectedErrorContains))
				}
			})
		}
	})

	Context("createResourceReservationPod with security context", func() {
		It("should create pod with configured security contexts", func() {
			runAsUser := int64(1001)
			runAsGroup := int64(1001)
			runAsNonRoot := true
			allowPrivilegeEscalation := false
			readOnlyRootFilesystem := true

			rsc := &service{
				namespace:           "kai-resource-reservation",
				appLabelValue:       "kai-reservation",
				serviceAccountName:  "kai-sa",
				reservationPodImage: "test-image:latest",
				kubeClient:          fake.NewClientBuilder().Build(),
				runtimeClassName:    "nvidia",
				podSecurityContext: &v1.PodSecurityContext{
					RunAsUser:    &runAsUser,
					RunAsGroup:   &runAsGroup,
					RunAsNonRoot: &runAsNonRoot,
				},
				containerSecurityContext: &v1.SecurityContext{
					AllowPrivilegeEscalation: &allowPrivilegeEscalation,
					ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
					Capabilities: &v1.Capabilities{
						Drop: []v1.Capability{"ALL"},
					},
				},
			}

			resources := v1.ResourceRequirements{
				Limits: v1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
				},
			}

			pod, err := rsc.createResourceReservationPod("test-node", "test-group", "test-pod", resources)
			Expect(err).To(BeNil())
			Expect(pod).NotTo(BeNil())

			Expect(pod.Spec.SecurityContext).NotTo(BeNil())
			Expect(*pod.Spec.SecurityContext.RunAsUser).To(Equal(int64(1001)))
			Expect(*pod.Spec.SecurityContext.RunAsGroup).To(Equal(int64(1001)))
			Expect(*pod.Spec.SecurityContext.RunAsNonRoot).To(BeTrue())

			container := pod.Spec.Containers[0]
			Expect(container.SecurityContext).NotTo(BeNil())
			Expect(*container.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
			Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
			Expect(container.SecurityContext.Capabilities.Drop).To(ContainElement(v1.Capability("ALL")))
		})

		It("should create pod without security context when not configured", func() {
			rsc := &service{
				namespace:                "kai-resource-reservation",
				appLabelValue:            "kai-reservation",
				serviceAccountName:       "kai-sa",
				reservationPodImage:      "test-image:latest",
				kubeClient:               fake.NewClientBuilder().Build(),
				runtimeClassName:         "nvidia",
				podSecurityContext:       nil,
				containerSecurityContext: nil,
			}

			resources := v1.ResourceRequirements{
				Limits: v1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
				},
			}

			pod, err := rsc.createResourceReservationPod("test-node", "test-group", "test-pod", resources)
			Expect(err).To(BeNil())
			Expect(pod).NotTo(BeNil())

			Expect(pod.Spec.SecurityContext).To(BeNil())
			Expect(pod.Spec.Containers[0].SecurityContext).To(BeNil())
		})
	})

	Context("createGPUReservationPod with resource configuration", func() {
		It("should create pod with configured CPU and memory resources", func() {
			rsc := &service{
				namespace:           "kai-resource-reservation",
				appLabelValue:       "kai-reservation",
				serviceAccountName:  "kai-sa",
				reservationPodImage: "test-image:latest",
				kubeClient:          fake.NewClientBuilder().Build(),
				runtimeClassName:    "nvidia",
				podResources: &v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("2m"),
						v1.ResourceMemory: resource.MustParse("20Mi"),
					},
					Limits: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("100m"),
						v1.ResourceMemory: resource.MustParse("200Mi"),
					},
				},
				scalingPodNamespace: scalingPodsNamespace,
			}

			pod, err := rsc.createGPUReservationPod(context.TODO(), "test-node", "test-gpu-group")
			Expect(err).To(BeNil())
			Expect(pod).NotTo(BeNil())

			container := pod.Spec.Containers[0]

			// Verify CPU requests and limits
			Expect(container.Resources.Requests[v1.ResourceCPU]).To(Equal(resource.MustParse("2m")))
			Expect(container.Resources.Requests[v1.ResourceMemory]).To(Equal(resource.MustParse("20Mi")))
			Expect(container.Resources.Limits[v1.ResourceCPU]).To(Equal(resource.MustParse("100m")))
			Expect(container.Resources.Limits[v1.ResourceMemory]).To(Equal(resource.MustParse("200Mi")))

			// Verify GPU resource is still set
			gpuRequest := container.Resources.Requests[constants.NvidiaGpuResource]
			gpuLimit := container.Resources.Limits[constants.NvidiaGpuResource]
			Expect(gpuRequest.Value()).To(Equal(int64(1)))
			Expect(gpuLimit.Value()).To(Equal(int64(1)))
		})

		It("should create pod without CPU/Memory resources when not configured", func() {
			rsc := &service{
				namespace:           "kai-resource-reservation",
				appLabelValue:       "kai-reservation",
				serviceAccountName:  "kai-sa",
				reservationPodImage: "test-image:latest",
				kubeClient:          fake.NewClientBuilder().Build(),
				runtimeClassName:    "nvidia",
				podResources:        nil,
				scalingPodNamespace: scalingPodsNamespace,
			}

			pod, err := rsc.createGPUReservationPod(context.TODO(), "test-node", "test-gpu-group")
			Expect(err).To(BeNil())
			Expect(pod).NotTo(BeNil())

			container := pod.Spec.Containers[0]

			// Verify CPU and Memory are NOT set
			_, cpuRequestExists := container.Resources.Requests[v1.ResourceCPU]
			_, memRequestExists := container.Resources.Requests[v1.ResourceMemory]
			_, cpuLimitExists := container.Resources.Limits[v1.ResourceCPU]
			_, memLimitExists := container.Resources.Limits[v1.ResourceMemory]

			Expect(cpuRequestExists).To(BeFalse())
			Expect(memRequestExists).To(BeFalse())
			Expect(cpuLimitExists).To(BeFalse())
			Expect(memLimitExists).To(BeFalse())

			// Verify GPU resource is still set
			gpuRequest := container.Resources.Requests[constants.NvidiaGpuResource]
			gpuLimit := container.Resources.Limits[constants.NvidiaGpuResource]
			Expect(gpuRequest.Value()).To(Equal(int64(1)))
			Expect(gpuLimit.Value()).To(Equal(int64(1)))
		})

		It("should create pod with only CPU resources when only CPU is configured", func() {
			rsc := &service{
				namespace:           "kai-resource-reservation",
				appLabelValue:       "kai-reservation",
				serviceAccountName:  "kai-sa",
				reservationPodImage: "test-image:latest",
				kubeClient:          fake.NewClientBuilder().Build(),
				runtimeClassName:    "nvidia",
				podResources: &v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("5m"),
					},
					Limits: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("50m"),
					},
				},
				scalingPodNamespace: scalingPodsNamespace,
			}

			pod, err := rsc.createGPUReservationPod(context.TODO(), "test-node", "test-gpu-group")
			Expect(err).To(BeNil())
			Expect(pod).NotTo(BeNil())

			container := pod.Spec.Containers[0]

			// Verify CPU is set
			Expect(container.Resources.Requests[v1.ResourceCPU]).To(Equal(resource.MustParse("5m")))
			Expect(container.Resources.Limits[v1.ResourceCPU]).To(Equal(resource.MustParse("50m")))

			// Verify Memory is NOT set
			_, memRequestExists := container.Resources.Requests[v1.ResourceMemory]
			_, memLimitExists := container.Resources.Limits[v1.ResourceMemory]
			Expect(memRequestExists).To(BeFalse())
			Expect(memLimitExists).To(BeFalse())
		})

		It("should not allow GPU resources to be overridden by podResources", func() {
			// This test ensures that even if podResources contains GPU configuration,
			// it won't override the GPU resource value set by the service
			rsc := &service{
				namespace:           "kai-resource-reservation",
				appLabelValue:       "kai-reservation",
				serviceAccountName:  "kai-sa",
				reservationPodImage: "test-image:latest",
				kubeClient:          fake.NewClientBuilder().Build(),
				runtimeClassName:    "nvidia",
				podResources: &v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceCPU:              resource.MustParse("10m"),
						constants.NvidiaGpuResource: resource.MustParse("999"), // This should be ignored
					},
					Limits: v1.ResourceList{
						v1.ResourceCPU:              resource.MustParse("100m"),
						constants.NvidiaGpuResource: resource.MustParse("999"), // This should be ignored
					},
				},
				scalingPodNamespace: scalingPodsNamespace,
			}

			pod, err := rsc.createGPUReservationPod(context.TODO(), "test-node", "test-gpu-group")
			Expect(err).To(BeNil())
			Expect(pod).NotTo(BeNil())

			container := pod.Spec.Containers[0]

			// Verify CPU resources from podResources are set
			Expect(container.Resources.Requests[v1.ResourceCPU]).To(Equal(resource.MustParse("10m")))
			Expect(container.Resources.Limits[v1.ResourceCPU]).To(Equal(resource.MustParse("100m")))

			// Verify GPU resources are NOT overridden - should always be 1
			gpuRequest := container.Resources.Requests[constants.NvidiaGpuResource]
			gpuLimit := container.Resources.Limits[constants.NvidiaGpuResource]
			Expect(gpuRequest.Value()).To(Equal(int64(1)), "GPU request should be 1, not overridden by podResources")
			Expect(gpuLimit.Value()).To(Equal(int64(1)), "GPU limit should be 1, not overridden by podResources")
		})
	})
})

type FakeWatchPod struct {
	Delay   time.Duration
	channel chan watch.Event
	Pod     *v1.Pod
	ticker  *time.Ticker
}

func (w *FakeWatchPod) Stop() {
	if w.ticker != nil {
		w.ticker.Stop()
	}
}

func (w *FakeWatchPod) ResultChan() <-chan watch.Event {
	if w.channel == nil {
		w.channel = make(chan watch.Event)
	}

	go func() {
		time.Sleep(w.Delay)
		w.channel <- watch.Event{Type: watch.Added, Object: w.Pod}
	}()

	return w.channel
}

func exampleMockWatchPod(gpuIndex string, delay time.Duration) watch.Interface {
	return &FakeWatchPod{
		Delay: delay,
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: resourceReservationNameSpace,
				Annotations: map[string]string{
					gpuIndexAnnotationName: gpuIndex,
				},
			},
		},
	}
}

// FakeWatchPodClosed simulates a watch that closes its channel immediately
type FakeWatchPodClosed struct {
	channel chan watch.Event
}

func (w *FakeWatchPodClosed) Stop() {
	// No-op
}

func (w *FakeWatchPodClosed) ResultChan() <-chan watch.Event {
	if w.channel == nil {
		w.channel = make(chan watch.Event)
		close(w.channel) // Close immediately to simulate channel close
	}
	return w.channel
}

func exampleMockWatchPodClosed() watch.Interface {
	return &FakeWatchPodClosed{}
}
