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

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/fips"
	"github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/consts"
	testutils "github.com/kai-scheduler/KAI-scheduler/pkg/nodescaleadjuster/test-utils"
	"github.com/kai-scheduler/api/constants"
)

const (
	PodName      = "test-pod-1"
	PodNamespace = "test-ns-1"
)

func TestGetScalingPodName(t *testing.T) {
	name := scalingPodName("namespace", "name")
	if name != "scaling.namespace.name" {
		t.Errorf("Expected scaling pod name to be 'scaling.namespace.name', actual is %v", name)
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
		unschedulablePod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3, false)
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

func TestCreateScalingPodSpecFIPSOnly(t *testing.T) {
	unschedulablePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PodName,
			Namespace: PodNamespace,
		},
	}

	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		unschedulablePod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3, true)
	expectedEnv := []corev1.EnvVar{{Name: fips.GODEBUGEnvName, Value: fips.OnlyGODEBUGValue}}
	if !reflect.DeepEqual(scalingPod.Spec.Containers[0].Env, expectedEnv) {
		t.Errorf("Expected env %v, actual %v", expectedEnv, scalingPod.Spec.Containers[0].Env)
	}

	scalingPod = createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		unschedulablePod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3, false)
	if len(scalingPod.Spec.Containers[0].Env) != 0 {
		t.Errorf("Expected no env, actual %v", scalingPod.Spec.Containers[0].Env)
	}
}

func TestCreateScalingPodWithPodAffinity(t *testing.T) {
	pod := loadTestPodFromYaml("test_pods/pod_with_pod_affinity.yaml")
	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		pod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3, false)
	if !reflect.DeepEqual(scalingPod.Spec.Affinity, pod.Spec.Affinity) {
		t.Errorf("Scaling pod did not inherrit pod affinity")
	}
}

func TestCreateScalingPodWithNodeAffinity(t *testing.T) {
	pod := loadTestPodFromYaml("test_pods/pod_with_node_affinity.yaml")
	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		pod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3, false)
	if !reflect.DeepEqual(scalingPod.Spec.Affinity, pod.Spec.Affinity) {
		t.Errorf("Scaling pod did not inherrit node affinity")
	}
}

func TestCreateScalingPodWithNodeSelector(t *testing.T) {
	pod := loadTestPodFromYaml("test_pods/pod_with_node_selector.yaml")
	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		pod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3, false)
	if !reflect.DeepEqual(scalingPod.Spec.NodeSelector, pod.Spec.NodeSelector) {
		t.Errorf("Scaling pod did not inherrit node selector")
	}
}

func TestCreateScalingPodWithToleration(t *testing.T) {
	pod := loadTestPodFromYaml("test_pods/pod_with_toleration.yaml")
	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		pod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3, false)
	if !reflect.DeepEqual(scalingPod.Spec.Tolerations, pod.Spec.Tolerations) {
		t.Errorf("Scaling pod did not inherrit tolerations")
	}
}

func TestRequestedResources(t *testing.T) {
	pod := loadTestPodFromYaml("test_pods/pod_with_multiple_containers.yaml")
	scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
		pod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 3, false)
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

func TestRequestedResourcesFollowPodEffectiveRequests(t *testing.T) {
	sidecarRestartPolicy := corev1.ContainerRestartPolicyAlways
	requests := func(cpu, memory string) corev1.ResourceRequirements {
		return corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(memory),
		}}
	}

	tests := []struct {
		name           string
		spec           corev1.PodSpec
		expectedCPU    string
		expectedMemory string
	}{
		{
			name: "app containers only",
			spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "a", Resources: requests("1", "1Gi")},
					{Name: "b", Resources: requests("2", "2Gi")},
				},
			},
			expectedCPU:    "3",
			expectedMemory: "3Gi",
		},
		{
			name: "init container larger than app containers",
			spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{Name: "model-download", Resources: requests("4", "16Gi")},
				},
				Containers: []corev1.Container{
					{Name: "app", Resources: requests("1", "2Gi")},
				},
			},
			expectedCPU:    "4",
			expectedMemory: "16Gi",
		},
		{
			name: "native sidecar adds to app containers",
			spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{Name: "proxy", RestartPolicy: &sidecarRestartPolicy, Resources: requests("500m", "512Mi")},
				},
				Containers: []corev1.Container{
					{Name: "app", Resources: requests("1", "2Gi")},
				},
			},
			expectedCPU:    "1500m",
			expectedMemory: "2560Mi",
		},
		{
			name: "pod-level requests",
			spec: corev1.PodSpec{
				Resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("64Gi"),
				}},
				Containers: []corev1.Container{{Name: "app"}},
			},
			expectedCPU:    "8",
			expectedMemory: "64Gi",
		},
		{
			name: "pod overhead",
			spec: corev1.PodSpec{
				Overhead: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("250m"),
					corev1.ResourceMemory: resource.MustParse("120Mi"),
				},
				Containers: []corev1.Container{
					{Name: "app", Resources: requests("1", "2Gi")},
				},
			},
			expectedCPU:    "1250m",
			expectedMemory: "2168Mi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: PodName, Namespace: PodNamespace},
				Spec:       tt.spec,
			}
			scalingPod := createScalingPodSpec(testutils.ScalingPodAppLabel, testutils.ScalingPodServiceAccount,
				pod, consts.DefaultScalingPodImage, testutils.ScalingPodNamespace, 1, false)
			requestedResources := scalingPod.Spec.Containers[0].Resources.Requests

			if expected := resource.MustParse(tt.expectedCPU); !requestedResources.Cpu().Equal(expected) {
				t.Errorf("expected CPU request %s, got %s", expected.String(), requestedResources.Cpu().String())
			}
			if expected := resource.MustParse(tt.expectedMemory); !requestedResources.Memory().Equal(expected) {
				t.Errorf("expected memory request %s, got %s", expected.String(), requestedResources.Memory().String())
			}
			gpu := requestedResources.Name(constants.NvidiaGpuResource, resource.DecimalSI)
			if expected := resource.MustParse("1"); !gpu.Equal(expected) {
				t.Errorf("expected GPU request %s, got %s", expected.String(), gpu.String())
			}
		})
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
