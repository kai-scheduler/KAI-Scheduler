// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scaler

import (
	"io/ioutil"
	_ "io/ioutil"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/consts"
	testutils "github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/test-utils"
)

const (
	PodName      = "test-pod-1"
	PodNamespace = "test-ns-1"
)

func TestGetScalingPodName(t *testing.T) {
	name := scalingPodName("namespace", "name")
	if name != "namespace-name" {
		t.Errorf("Expected scaling pod name to be 'namespace-name', actual is %v", name)
	}
}

func TestCreateScalingPodSpec(t *testing.T) {
	unschedulablePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PodName,
			Namespace: PodNamespace,
		},
	}

	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		unschedulablePod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3)
	expectedName := scalingPodName(PodNamespace, PodName)
	if scalingPod.Name != expectedName {
		t.Errorf("Pod names was expected to be %v, actual %v", expectedName, scalingPod.Name)
	}

	if val, found := scalingPod.Labels[consts.SharedGpuPodName]; !found || val != PodName {
		t.Errorf("Wrong pod name annotation, actual: %v", val)
	}

	if val, found := scalingPod.Labels[consts.SharedGpuPodNamespace]; !found || val != PodNamespace {
		t.Errorf("Wrong pod name annotation, actual: %v", val)
	}
}

func TestCreateScalingPodWithPodAffinity(t *testing.T) {
	pod := loadTestPodFromYaml("test_pods/pod_with_pod_affinity.yaml")
	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		pod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3)
	if !reflect.DeepEqual(scalingPod.Spec.Affinity, pod.Spec.Affinity) {
		t.Errorf("Scaling pod did not inherrit pod affinity")
	}
}

func TestCreateScalingPodWithNodeAffinity(t *testing.T) {
	pod := loadTestPodFromYaml("test_pods/pod_with_node_affinity.yaml")
	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		pod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3)
	if !reflect.DeepEqual(scalingPod.Spec.Affinity, pod.Spec.Affinity) {
		t.Errorf("Scaling pod did not inherrit node affinity")
	}
}

func TestCreateScalingPodWithNodeSelector(t *testing.T) {
	pod := loadTestPodFromYaml("test_pods/pod_with_node_selector.yaml")
	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		pod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3)
	if !reflect.DeepEqual(scalingPod.Spec.NodeSelector, pod.Spec.NodeSelector) {
		t.Errorf("Scaling pod did not inherrit node selector")
	}
}

func TestCreateScalingPodWithToleration(t *testing.T) {
	pod := loadTestPodFromYaml("test_pods/pod_with_toleration.yaml")
	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		pod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3)
	if !reflect.DeepEqual(scalingPod.Spec.Tolerations, pod.Spec.Tolerations) {
		t.Errorf("Scaling pod did not inherrit tolerations")
	}
}

func TestRequestedResources(t *testing.T) {
	pod := loadTestPodFromYaml("test_pods/pod_with_multiple_containers.yaml")
	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		pod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3)
	if len(scalingPod.Spec.Containers) != 1 {
		t.Errorf("Failed to aggregate container resource requests")
	}
	requestedResources := scalingPod.Spec.Containers[0].Resources.Requests
	cpuReqRes := resource.NewMilliQuantity(250+250, resource.DecimalSI)
	if !requestedResources.Cpu().Equal(*cpuReqRes) {
		t.Errorf("Failed to aggregate CPU requested resources")
	}

	memReqRes := resource.NewQuantity((64+64)*1024*1024, resource.DecimalSI)
	if !requestedResources.Memory().Equal(*memReqRes) {
		t.Errorf("Failed to aggregate memory requested resources")
	}

	gpuReqRes := resource.MustParse("3")
	if !requestedResources.Name(constants.NvidiaGpuResource, resource.DecimalSI).Equal(gpuReqRes) {
		t.Errorf("Failed to aggregate memory requested resources")
	}

}

func loadTestPodFromYaml(filename string) *corev1.Pod {
	stream, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err)
	}
	obj, _, err := scheme.Codecs.UniversalDeserializer().Decode(stream, nil, nil)
	if err != nil {
		panic(err)
	}
	return obj.(*corev1.Pod)
}
