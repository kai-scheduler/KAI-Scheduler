// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale_adjuster

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/consts"
	"github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/scaler"
	testutils "github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/test-utils"
)

type remainingPod struct {
	namespace  string
	name       string
	numDevices *int64
}

type testData struct {
	unschedulablePods []*corev1.Pod
	scalingPods       []*corev1.Pod
	remainingPods     []remainingPod
	coolDownSeconds   *int
	interceptFuncs    *interceptor.Funcs
	wantErr           bool
	isInCoolDown      bool
}

func TestScaleAdjuster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Scale Adjuster Test Suite")
}

var _ = Describe("Scale Adjuster Test Suite", func() {
	var numInterceptedCalls int
	BeforeEach(func() {
		numInterceptedCalls = 0
	})
	DescribeTable(
		"Node Scale Adjuster tests",
		func(data testData) {
			var existingPods []runtimeClient.Object
			for _, pod := range data.unschedulablePods {
				existingPods = append(existingPods, pod)
			}
			for _, pod := range data.scalingPods {
				existingPods = append(existingPods, pod)
			}
			client := testutils.NewFakeClient(data.interceptFuncs, existingPods...)

			coolDown := consts.DefaultCoolDownSeconds
			if data.coolDownSeconds != nil {
				coolDown = *data.coolDownSeconds
			}

			scaler := scaler.NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
				testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)
			sa := NewScaleAdjuster(client, scaler, testutils.ScalingPodNamespace, int64(coolDown),
				consts.DefaultGPUMemoryToFractionRatio, testutils.SchedulerName)
			isInCoolDown, err := sa.Adjust()
			Expect(isInCoolDown).To(Equal(data.isInCoolDown))
			Expect(err != nil).To(Equal(data.wantErr))

			allPods := &corev1.PodList{}
			err = client.List(context.TODO(), allPods)
			Expect(err).To(BeNil())
			Expect(allPods.Items).To(HaveLen(len(data.remainingPods)))

			for _, remainingPod := range data.remainingPods {
				pod := &corev1.Pod{}
				key := types.NamespacedName{Namespace: remainingPod.namespace, Name: remainingPod.name}
				err := client.Get(context.TODO(), key, pod)
				Expect(err).To(BeNil())

				if remainingPod.numDevices != nil {
					actualNumDevices := pod.Spec.Containers[0].Resources.Limits[constants.NvidiaGpuResource]
					Expect(actualNumDevices.Value()).To(Equal(*remainingPod.numDevices))
				}
			}
		},
		Entry(
			"no pods",
			testData{
				unschedulablePods: []*corev1.Pod{},
				scalingPods:       []*corev1.Pod{},
				remainingPods:     []remainingPod{},
				wantErr:           false,
				isInCoolDown:      false,
			},
		),
		Entry(
			"simple scenario with multiple pods - nothing to adjust",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.7", 1),
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.7", 1),
					testutils.CreateUnschedulableFractionPod("pod3", "ns3", "0.1", 1),
				},
				scalingPods: []*corev1.Pod{
					testutils.CreateScalingPod("ns1", "pod1", 1),
					testutils.CreateScalingPod("ns2", "pod2", 1),
				},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace: "ns3",
						name:      "pod3",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(1)),
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns2-pod2",
						numDevices: ptr.To(int64(1)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"two scaling pods - one is no longer needed",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.7", 1),
				},
				scalingPods: []*corev1.Pod{
					testutils.CreateScalingPod("ns1", "pod1", 1),
					testutils.CreateScalingPod("ns2", "pod2", 1),
				},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(1)),
					},
				},
				wantErr:      false,
				isInCoolDown: true,
			},
		),
		Entry(
			"single unschedulable GPU fraction pod - create scaling pod",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.7", 1),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(1)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"single unschedulable GPU memory pod - create scaling pod",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulablePodWithGpuMemory("pod1", "ns1", "1024", 1),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(1)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"two unschedulable pods - create single scaling pod",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.7", 1),
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.1", 1),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(1)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"two unschedulable pods - create two scaling pods",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.7", 1),
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.7", 1),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(1)),
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns2-pod2",
						numDevices: ptr.To(int64(1)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"two unschedulable pods and one scaling pod - create another one",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.7", 1),
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.7", 1),
				},
				scalingPods: []*corev1.Pod{
					testutils.CreateScalingPod("ns1", "pod1", 1),
				},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(1)),
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns2-pod2",
						numDevices: ptr.To(int64(1)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"unschedulable pod with 2 devices - fit into single GPU - create a scaling pod",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.2", 2),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(2)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"unschedulable pod with 2 devices - requires two GPUs - create one scaling pod with 2 GPUs",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.9", 2),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(2)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"unschedulable pods with 2 devices - fit into single GPU - create one scaling pod with 2 GPUs",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.2", 2),
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.1", 2),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(2)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"unschedulable pods with 2 devices - fit into 2 GPUs - create single scaling pod with 2 GPUs",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.5", 2),
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.1", 2),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(2)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"unschedulable pods with 2 devices - fit into 3 GPUs - create two scaling pods with 2 GPUs",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.5", 2),
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.8", 2),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(2)),
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns2-pod2",
						numDevices: ptr.To(int64(2)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"unschedulable pods with multiple devices - fit into a single GPU - create two scaling pods",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.2", 2),
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.1", 3),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(2)),
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns2-pod2",
						numDevices: ptr.To(int64(3)),
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"unschedulable pods and one unneeded scaling pod - default cooldown",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.7", 1),
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.7", 1),
				},
				scalingPods: []*corev1.Pod{
					testutils.CreateScalingPod("non-existing", "pod", 1),
				},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
				},
				wantErr:      false,
				isInCoolDown: true,
			},
		),
		Entry(
			"two unschedulable pods and one unneeded scaling pod - no cooldown",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.7", 1),
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.7", 1),
				},
				scalingPods: []*corev1.Pod{
					testutils.CreateScalingPod("non-existing", "pod", 1),
				},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(1)),
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns2-pod2",
						numDevices: ptr.To(int64(1)),
					},
				},
				coolDownSeconds: ptr.To(0),
				wantErr:         false,
				isInCoolDown:    false,
			},
		),
		Entry(
			"three unschedulable pods and two scaling pods - delete one and create one",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.7", 1),
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.7", 1),
					testutils.CreateUnschedulableFractionPod("pod3", "ns3", "0.2", 1),
				},
				scalingPods: []*corev1.Pod{
					testutils.CreateScalingPod("ns1", "pod1", 1),
					testutils.CreateScalingPod("non-existing", "pod", 1),
				},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace: "ns3",
						name:      "pod3",
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(1)),
					},
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns2-pod2",
						numDevices: ptr.To(int64(1)),
					},
				},
				coolDownSeconds: ptr.To(0),
				wantErr:         false,
				isInCoolDown:    false,
			},
		),
		Entry(
			"one scaling pod - fail to list pods",
			testData{
				unschedulablePods: []*corev1.Pod{},
				scalingPods: []*corev1.Pod{
					testutils.CreateScalingPod("ns1", "pod1", 1),
				},
				remainingPods: []remainingPod{
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(1)),
					},
				},
				interceptFuncs: &interceptor.Funcs{
					List: func(ctx context.Context, client runtimeClient.WithWatch, list runtimeClient.ObjectList,
						opts ...runtimeClient.ListOption) error {
						numInterceptedCalls = numInterceptedCalls + 1
						if numInterceptedCalls == 1 {
							return fmt.Errorf("list pods failed")
						}
						return client.List(ctx, list, opts...)
					},
				},
				wantErr:      true,
				isInCoolDown: false,
			},
		),
		Entry(
			"one scaling pod - fail to delete pod",
			testData{
				unschedulablePods: []*corev1.Pod{},
				scalingPods: []*corev1.Pod{
					testutils.CreateScalingPod("ns1", "pod1", 1),
				},
				remainingPods: []remainingPod{
					{
						namespace:  testutils.ScalingPodNamespace,
						name:       "ns1-pod1",
						numDevices: ptr.To(int64(1)),
					},
				},
				interceptFuncs: &interceptor.Funcs{
					Delete: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object,
						opts ...runtimeClient.DeleteOption) error {
						return fmt.Errorf("delete pod failed")
					},
				},
				wantErr:      true,
				isInCoolDown: false,
			},
		),
		Entry(
			"one scaling pod - fail to list unschedulable pods",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.7", 1),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
				},
				interceptFuncs: &interceptor.Funcs{
					List: func(ctx context.Context, client runtimeClient.WithWatch, list runtimeClient.ObjectList,
						opts ...runtimeClient.ListOption) error {
						numInterceptedCalls = numInterceptedCalls + 1
						if numInterceptedCalls == 2 {
							return fmt.Errorf("list pods failed")
						}
						return client.List(ctx, list, opts...)
					},
				},
				wantErr:      true,
				isInCoolDown: false,
			},
		),
		Entry(
			"one unschedulable pod - fail to get num needed devices",
			testData{
				unschedulablePods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod1",
							Namespace: "ns1",
							Annotations: map[string]string{
								constants.GpuFraction:            "0.7",
								constants.GpuFractionsNumDevices: "a3",
							},
						},
						Spec: corev1.PodSpec{
							SchedulerName: testutils.SchedulerName,
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodPending,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodScheduled,
									Status: corev1.ConditionFalse,
									Reason: corev1.PodReasonUnschedulable,
								},
							},
						},
					},
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"two unschedulable pods - fail to get num needed devices, skip one",
			testData{
				unschedulablePods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod1",
							Namespace: "ns1",
							Annotations: map[string]string{
								constants.GpuFraction:            "0.7",
								constants.GpuFractionsNumDevices: "a3",
							},
						},
						Spec: corev1.PodSpec{
							SchedulerName: testutils.SchedulerName,
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodPending,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodScheduled,
									Status: corev1.ConditionFalse,
									Reason: corev1.PodReasonUnschedulable,
								},
							},
						},
					},
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.7", 1),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace: testutils.ScalingPodNamespace,
						name:      "ns2-pod2",
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"one unschedulable fraction pod - fail to parse GPU fraction",
			testData{
				unschedulablePods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod1",
							Namespace: "ns1",
							Annotations: map[string]string{
								constants.GpuFraction: "a4f",
							},
						},
						Spec: corev1.PodSpec{
							SchedulerName: testutils.SchedulerName,
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodPending,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodScheduled,
									Status: corev1.ConditionFalse,
									Reason: corev1.PodReasonUnschedulable,
								},
							},
						},
					},
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"one unschedulable GPU memory pod - fail to parse GPU memory annotation",
			testData{
				unschedulablePods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod1",
							Namespace: "ns1",
							Annotations: map[string]string{
								constants.GpuMemory: "b4f",
							},
						},
						Spec: corev1.PodSpec{
							SchedulerName: testutils.SchedulerName,
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodPending,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodScheduled,
									Status: corev1.ConditionFalse,
									Reason: corev1.PodReasonUnschedulable,
								},
							},
						},
					},
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"two unschedulable pods - fail to pase GPU fraction, skip one",
			testData{
				unschedulablePods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod1",
							Namespace: "ns1",
							Annotations: map[string]string{
								constants.GpuFraction: "a4f",
							},
						},
						Spec: corev1.PodSpec{
							SchedulerName: testutils.SchedulerName,
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodPending,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodScheduled,
									Status: corev1.ConditionFalse,
									Reason: corev1.PodReasonUnschedulable,
								},
							},
						},
					},
					testutils.CreateUnschedulableFractionPod("pod2", "ns2", "0.7", 1),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
					{
						namespace: "ns2",
						name:      "pod2",
					},
					{
						namespace: testutils.ScalingPodNamespace,
						name:      "ns2-pod2",
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"one unschedulable pod - fail to create scaling pod",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.7", 1),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
				},
				interceptFuncs: &interceptor.Funcs{
					Create: func(ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object,
						opts ...runtimeClient.CreateOption) error {
						return fmt.Errorf("create pod failed")
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"non-fraction unschedulable pod - nothing to adjust",
			testData{
				unschedulablePods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "pod1",
							Namespace:   "ns1",
							Annotations: map[string]string{},
						},
						Spec: corev1.PodSpec{
							SchedulerName: testutils.SchedulerName,
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodPending,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodScheduled,
									Status: corev1.ConditionFalse,
									Reason: corev1.PodReasonUnschedulable,
								},
							},
						},
					},
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"unschedulable pod with succeeded phase - nothing to adjust",
			testData{
				unschedulablePods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod1",
							Namespace: "ns1",
							Annotations: map[string]string{
								constants.GpuFraction: "0.5",
							},
						},
						Spec: corev1.PodSpec{
							SchedulerName: testutils.SchedulerName,
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodSucceeded,
						},
					},
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"unschedulable pod with failed phase - nothing to adjust",
			testData{
				unschedulablePods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod1",
							Namespace: "ns1",
							Annotations: map[string]string{
								constants.GpuFraction: "0.5",
							},
						},
						Spec: corev1.PodSpec{
							SchedulerName: testutils.SchedulerName,
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodFailed,
						},
					},
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"non-related unschedulable pod - nothing to adjust",
			testData{
				unschedulablePods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "pod1",
							Namespace:   "ns1",
							Annotations: map[string]string{},
						},
						Spec: corev1.PodSpec{},
						Status: corev1.PodStatus{
							Phase: corev1.PodPending,
							Conditions: []corev1.PodCondition{
								{
									Type:   corev1.PodScheduled,
									Status: corev1.ConditionFalse,
									Reason: corev1.PodReasonUnschedulable,
								},
							},
						},
					},
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"bound pod - nothing to adjust",
			testData{
				unschedulablePods: []*corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod1",
							Namespace: "ns1",
							Annotations: map[string]string{
								constants.GpuFraction: "0.5",
							},
						},
						Spec: corev1.PodSpec{
							SchedulerName: testutils.SchedulerName,
							NodeName:      "some-node",
						},
						Status: corev1.PodStatus{
							Phase: corev1.PodRunning,
						},
					},
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
		Entry(
			"pending pod without unschedulable condition - nothing to adjust",
			testData{
				unschedulablePods: []*corev1.Pod{
					testutils.CreatePendingFractionPod("pod1", "ns1", "0.7", 1),
				},
				scalingPods: []*corev1.Pod{},
				remainingPods: []remainingPod{
					{
						namespace: "ns1",
						name:      "pod1",
					},
				},
				wantErr:      false,
				isInCoolDown: false,
			},
		),
	)
})
