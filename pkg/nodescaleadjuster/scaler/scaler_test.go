// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scaler

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/consts"
	testutils "github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/test-utils"
)

func TestCreateScalingPod(t *testing.T) {
	unschedulablePod := testutils.CreateUnschedulableFractionPod("pod", "namespace", "0.5", 1)
	client := testutils.NewFakeClient(nil, unschedulablePod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	pod, err := scaler.CreateScalingPod(unschedulablePod)
	assert.Nil(t, err, "Failed to create scaling pod, err: %v", err)
	assert.NotNil(t, pod)

	scalingPods := &corev1.PodList{}
	err = client.List(context.Background(), scalingPods, runtimeClient.InNamespace(testutils.ScalingPodNamespace))
	assert.Nil(t, err, "Failed to list scaling pods, err: %v", err)
	assert.Equal(t, 1, len(scalingPods.Items), "Expected one scaling pod")
}

func TestCreateScalingPodWithMultipleDevices(t *testing.T) {
	unschedulablePod := testutils.CreateUnschedulableFractionPod("pod", "namespace", "0.5", 3)
	client := testutils.NewFakeClient(nil, unschedulablePod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	pod, err := scaler.CreateScalingPod(unschedulablePod)
	assert.Nil(t, err, "Failed to create scaling pod, err: %v", err)
	assert.NotNil(t, pod)

	scalingPods := &corev1.PodList{}
	err = client.List(context.Background(), scalingPods, runtimeClient.InNamespace(testutils.ScalingPodNamespace))
	assert.Nil(t, err, "Failed to list scaling pods, err: %v", err)
	assert.Equal(t, 1, len(scalingPods.Items), "Expected one scaling pod")

	container := scalingPods.Items[0].Spec.Containers[0]
	gpuResourceRequest := container.Resources.Requests[constants.NvidiaGpuResource]
	gpuResourceLimits := container.Resources.Limits[constants.NvidiaGpuResource]
	assert.Equal(t, int64(3), gpuResourceRequest.Value())
	assert.Equal(t, int64(3), gpuResourceLimits.Value())
}

func TestFailGettingNumDevicesFromPod(t *testing.T) {
	unschedulablePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pod-name",
			Namespace:   "pod-ns",
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
	}
	client := testutils.NewFakeClient(nil, unschedulablePod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	pod, err := scaler.CreateScalingPod(unschedulablePod)
	assert.NotNil(t, err, "Created scaling pod although should have failed")
	assert.Nil(t, pod)

	scalingPods := &corev1.PodList{}
	err = client.List(context.Background(), scalingPods, runtimeClient.InNamespace(testutils.ScalingPodNamespace))
	assert.Nil(t, err, "Failed to list scaling pods, err: %v", err)
	assert.Equal(t, 0, len(scalingPods.Items), "Expected no scaling pod")
}

func TestFailToCreateScalingPod(t *testing.T) {
	unschedulablePod := testutils.CreateUnschedulableFractionPod("pod", "namespace", "0.5", 1)
	interceptorFuncs := &interceptor.Funcs{
		Create: func(
			ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object,
			opts ...runtimeClient.CreateOption,
		) error {
			return fmt.Errorf("fail to create scaling pod")
		},
	}
	client := testutils.NewFakeClient(interceptorFuncs, unschedulablePod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	pod, err := scaler.CreateScalingPod(unschedulablePod)
	assert.NotNil(t, err, "Created scaling pod although should have failed")
	assert.Nil(t, pod)
}

func TestCreateScalingPodFailOnStatusUpdate(t *testing.T) {
	unschedulablePod := testutils.CreateUnschedulableFractionPod("pod", "namespace", "0.5", 1)
	interceptorFuncs := &interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, client runtimeClient.Client, subResourceName string,
			obj runtimeClient.Object, patch runtimeClient.Patch, opts ...runtimeClient.SubResourcePatchOption) error {
			return fmt.Errorf("fail to patch scaling pod status")
		},
	}
	client := testutils.NewFakeClient(interceptorFuncs, unschedulablePod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	pod, err := scaler.CreateScalingPod(unschedulablePod)
	assert.NotNil(t, err, "Created scaling pod although should have failed to update status")
	assert.Nil(t, pod)
}

func TestDeleteScalingPod(t *testing.T) {
	scalingPod := testutils.CreateScalingPod("pod-ns", "pod-name", 1)
	client := testutils.NewFakeClient(nil, scalingPod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)
	err := scaler.DeleteScalingPod(scalingPod)
	assert.Nil(t, err, "Failed to delete scaling pod, err: %v", err)
}

func TestFailToDeleteScalingPod(t *testing.T) {
	scalingPod := testutils.CreateScalingPod("pod-ns", "pod-name", 1)
	interceptorFuncs := &interceptor.Funcs{
		Delete: func(
			ctx context.Context, client runtimeClient.WithWatch, obj runtimeClient.Object,
			opts ...runtimeClient.DeleteOption,
		) error {
			return fmt.Errorf("fail to delete scaling pod")
		},
	}
	client := testutils.NewFakeClient(interceptorFuncs, scalingPod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)
	err := scaler.DeleteScalingPod(scalingPod)
	assert.NotNil(t, err, "Deleted scaling pod although should have failed")
}

func TestIsScalingPodExists(t *testing.T) {
	unschedulablePod := testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.5", 1)
	scalingPod := testutils.CreateScalingPod("ns1", "pod1", 1)
	client := testutils.NewFakeClient(nil, unschedulablePod, scalingPod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	exists := scaler.IsScalingPodExistsForUnschedulablePod(unschedulablePod)
	assert.True(t, exists, "Scaling pod does not exists")
}

func TestIsScalingPodDoesNotExist(t *testing.T) {
	unschedulablePod := testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.5", 1)
	client := testutils.NewFakeClient(nil, unschedulablePod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	exits := scaler.IsScalingPodExistsForUnschedulablePod(unschedulablePod)
	assert.False(t, exits, "Scaling pod should not exist")
}

func TestIsScalingPodStillNeeded(t *testing.T) {
	unschedulablePod := testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.5", 1)
	scalingPod := testutils.CreateScalingPod("ns1", "pod1", 1)

	client := testutils.NewFakeClient(nil, unschedulablePod, scalingPod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	needed := scaler.IsScalingPodStillNeeded(scalingPod)
	assert.True(t, needed, "Scaling pod is not needed while unschedulable pod still exists")
}

func TestIsScalingPodStillNeededPodNoCondition(t *testing.T) {
	unschedulablePod := testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.5", 1)
	scalingPod := testutils.CreateScalingPod("ns1", "pod1", 1)
	unschedulablePod.Status = corev1.PodStatus{
		Conditions: []corev1.PodCondition{},
	}

	client := testutils.NewFakeClient(nil, unschedulablePod, scalingPod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	needed := scaler.IsScalingPodStillNeeded(scalingPod)
	assert.True(t, needed, "Scaling pod is not needed while unschedulable has no conditions")
}

func TestIsScalingPodStillNeededPodSchedulable(t *testing.T) {
	unschedulablePod := testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.5", 1)
	scalingPod := testutils.CreateScalingPod("ns1", "pod1", 1)
	unschedulablePod.Status = corev1.PodStatus{
		Conditions: []corev1.PodCondition{
			{
				Type:   corev1.PodScheduled,
				Status: corev1.ConditionTrue,
			},
		},
	}

	client := testutils.NewFakeClient(nil, unschedulablePod, scalingPod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	needed := scaler.IsScalingPodStillNeeded(scalingPod)
	assert.False(t, needed, "Scaling pod is needed while unschedulable pod became schedulable")
}

func TestIsScalingPodStillNeededPodDoesNotExist(t *testing.T) {
	scalingPod := testutils.CreateScalingPod("ns1", "pod1", 1)

	client := testutils.NewFakeClient(nil, scalingPod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	needed := scaler.IsScalingPodStillNeeded(scalingPod)
	assert.False(t, needed, "Scaling pod is needed while unschedulable pod was deleted")
}

func TestIsScalingPodStillNeededFailedToGetPod(t *testing.T) {
	scalingPod := testutils.CreateScalingPod("ns1", "pod1", 1)

	interceptorFuncs := &interceptor.Funcs{
		Get: func(ctx context.Context, client runtimeClient.WithWatch, key runtimeClient.ObjectKey,
			obj runtimeClient.Object, opts ...runtimeClient.GetOption) error {
			return fmt.Errorf("fail to get unschedulable pod")
		},
	}

	client := testutils.NewFakeClient(interceptorFuncs, scalingPod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	needed := scaler.IsScalingPodStillNeeded(scalingPod)
	assert.True(t, needed, "Scaling pod is not needed while failed to get unschedulable pod")
}

func TestIsScalingPodStillNeededOnCompletion(t *testing.T) {
	unschedulablePod := testutils.CreateUnschedulableFractionPod("pod1", "ns1", "0.5", 1)
	scalingPod := testutils.CreateScalingPod("ns1", "pod1", 1)
	scalingPod.Status = corev1.PodStatus{
		Phase: corev1.PodSucceeded,
	}

	client := testutils.NewFakeClient(nil, unschedulablePod, scalingPod)
	scaler := NewScaler(client, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace,
		testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount)

	needed := scaler.IsScalingPodStillNeeded(scalingPod)
	assert.False(t, needed, "Scaling pod is needed although it completed")
}
