// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package tasks_fake

import (
	"fmt"
	"maps"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	commonconstants "github.com/kai-scheduler/api/constants"
	"github.com/kai-scheduler/api/utilities/resources"
)

const (
	ownerUUID       = "1234"
	NodeAffinityKey = "kai.scheduler/type"
)

type TestTaskBasic struct {
	Name              string
	GPUGroups         []string
	SubGroupName      string
	RequiredGPUs      *int64
	State             pod_status.PodStatus
	NodeName          string // Relevant if job is running
	NodeAffinityNames []string
	Annotations       map[string]string
	PodAffinityLabels map[string]string
	// PodAffinityLabels are put on the pod and, by default, also select the pods it attracts and
	// repels (a pod attracting/repelling its own kind). PodAntiAffinitySelector overrides only the
	// required anti-affinity term's matchLabels and is never added to the pod's labels, so a task
	// can repel another set of pods without repelling itself. nil keeps the legacy behaviour.
	PodAntiAffinitySelector    map[string]string
	PodAffinityTopologyKey     string
	PodAntiAffinityTopologyKey string
	RequiredMigInstances       map[v1.ResourceName]int
	Priority                   *int
	IsLegacyMigTask            bool
	ResourceClaimTemplates     map[string]string
	ResourceClaimNames         []string
	PersistentVolumeClaimNames []string
	// ResizeStatus holds the allocated/enacted size; the job's request is the desired size.
	ResizeStatus *TestResizeStatus
	HostPorts    []int32
	// ConfigMapNames are required by the main container through envFrom.
	ConfigMapNames []string
}

// TestResizeStatus uses the same units as the job's RequiredCPUsPerTask/RequiredMemoryPerTask.
type TestResizeStatus struct {
	ActualCPUs   float64
	ActualMemory float64
	Deferred     bool
	Infeasible   bool
}

func BuildPod(
	name, namespace string,
	task *TestTaskBasic,
	phase v1.PodPhase, req v1.ResourceList,
	gpuFraction string, gpuMemoryMiB uint64, gpuGroups []string, jobName string,
) *v1.Pod {
	controllerBool := true
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{
				Controller: &controllerBool,
				UID:        ownerUUID,
				Name:       name,
			}},
			UID:       types.UID(name),
			Name:      name,
			Namespace: namespace,
			Labels: func() map[string]string {
				baseLabels := map[string]string{
					"job-name": name,
				}
				maps.Copy(baseLabels, task.PodAffinityLabels)
				return baseLabels
			}(),
			Annotations: map[string]string{
				commonconstants.GpuFraction:              gpuFraction,
				commonconstants.PodGroupAnnotationForPod: jobName,
			},
		},
		Status: v1.PodStatus{
			Phase: phase,
		},
		Spec: v1.PodSpec{
			NodeName: task.NodeName,
			Containers: []v1.Container{
				{
					Name: "main",
					Resources: v1.ResourceRequirements{
						Requests: req,
					},
				},
			},
			SchedulerName: "kai-scheduler",
		},
	}
	maps.Copy(pod.Annotations, task.Annotations)
	if gpuMemoryMiB > 0 {
		pod.Annotations[resources.CalcGpuFractionAnnotationForContainer("main")] = resources.GpuMemoryAnnotationToNvFractionsMemoryRequest(gpuMemoryMiB).String()
	}
	if len(gpuGroups) > 1 {
		for _, gpuGroup := range gpuGroups {
			multiGroupKey, multiGroupValue := resources.GetMultiFractionGpuGroupLabel(gpuGroup)
			pod.Labels[multiGroupKey] = multiGroupValue
		}
	} else if len(gpuGroups) > 0 {
		pod.Labels[pod_info.GPUGroup] = gpuGroups[0]
	}

	if len(task.NodeAffinityNames) > 0 {
		affinity := &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      NodeAffinityKey,
								Operator: v1.NodeSelectorOpIn,
								Values:   task.NodeAffinityNames,
							},
						},
					},
				},
			},
		}

		pod.Spec.Affinity = &v1.Affinity{NodeAffinity: affinity}
	}
	pod.Spec.Affinity = applyPodAffinityLabels(
		pod.Spec.Affinity, task.PodAffinityLabels, task.PodAntiAffinitySelector,
		task.PodAffinityTopologyKey, task.PodAntiAffinityTopologyKey)

	for migInstance, count := range task.RequiredMigInstances {
		pod.Annotations[migInstance.String()] = fmt.Sprintf("%d", count)
	}

	maps.Copy(pod.Annotations, task.Annotations)

	applyResizeStatus(pod, task.ResizeStatus)
	for _, hostPort := range task.HostPorts {
		pod.Spec.Containers[0].Ports = append(pod.Spec.Containers[0].Ports, v1.ContainerPort{
			ContainerPort: hostPort,
			HostPort:      hostPort,
			Protocol:      v1.ProtocolTCP,
		})
	}

	for _, configMapName := range task.ConfigMapNames {
		pod.Spec.Containers[0].EnvFrom = append(pod.Spec.Containers[0].EnvFrom, v1.EnvFromSource{
			ConfigMapRef: &v1.ConfigMapEnvSource{LocalObjectReference: v1.LocalObjectReference{Name: configMapName}},
		})
	}

	for _, claimName := range task.ResourceClaimNames {
		pod.Spec.ResourceClaims = append(pod.Spec.ResourceClaims, v1.PodResourceClaim{
			Name:              claimName,
			ResourceClaimName: &claimName,
		})
	}

	for templateName, claimName := range task.ResourceClaimTemplates {
		pod.Spec.ResourceClaims = append(pod.Spec.ResourceClaims, v1.PodResourceClaim{
			Name:                      templateName,
			ResourceClaimTemplateName: &templateName,
		})
		pod.Status.ResourceClaimStatuses = append(pod.Status.ResourceClaimStatuses, v1.PodResourceClaimStatus{
			Name:              templateName,
			ResourceClaimName: &claimName,
		})
	}

	return pod
}

func applyResizeStatus(pod *v1.Pod, resizeStatus *TestResizeStatus) {
	if resizeStatus == nil {
		return
	}
	actual := pod.Spec.Containers[0].Resources.Requests.DeepCopy()
	if resizeStatus.ActualCPUs > 0 {
		actual[v1.ResourceCPU] = resource.MustParse(strconv.FormatFloat(resizeStatus.ActualCPUs, 'f', -1, 64))
	}
	if resizeStatus.ActualMemory > 0 {
		actual[v1.ResourceMemory] = resource.MustParse(strconv.FormatFloat(resizeStatus.ActualMemory, 'f', -1, 64))
	}
	pod.Status.ContainerStatuses = []v1.ContainerStatus{{
		Name:               pod.Spec.Containers[0].Name,
		AllocatedResources: actual,
		Resources:          &v1.ResourceRequirements{Requests: actual.DeepCopy()},
	}}

	if resizeStatus.Deferred || resizeStatus.Infeasible {
		reason := v1.PodReasonDeferred
		if resizeStatus.Infeasible {
			reason = v1.PodReasonInfeasible
		}
		pod.Status.Conditions = append(pod.Status.Conditions, v1.PodCondition{
			Type:   v1.PodResizePending,
			Status: v1.ConditionTrue,
			Reason: reason,
		})
	}
}

func GetTestTaskGPUIndex(testTask *TestTaskBasic) []string {
	var gpuIndexValue []string
	if len(testTask.GPUGroups) > 0 {
		gpuIndexValue = testTask.GPUGroups
	}

	return gpuIndexValue
}

func IsTaskStartedStatus(status pod_status.PodStatus) bool {
	if status == pod_status.Running || status == pod_status.Releasing {
		return true
	}

	return false
}

func applyPodAffinityLabels(
	affinity *v1.Affinity, podAffinityLabels, podAntiAffinitySelector map[string]string,
	podAffinityTopologyKey, podAntiAffinityTopologyKey string,
) *v1.Affinity {
	if affinity == nil {
		affinity = &v1.Affinity{}
	}
	// Legacy default: a pod repels pods carrying its own PodAffinityLabels. The selector is an
	// override, not a merge: matchLabels is a conjunction, so merging would narrow it and could
	// not express two values of the same key (e.g. repel tier=train while being tier=preprocess).
	if podAntiAffinitySelector == nil {
		podAntiAffinitySelector = podAffinityLabels
	}

	if podAffinityLabels != nil && len(podAffinityTopologyKey) != 0 {
		terms := []v1.PodAffinityTerm{
			{
				TopologyKey: podAffinityTopologyKey,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: podAffinityLabels,
				},
			},
		}
		if affinity.PodAffinity == nil {
			affinity.PodAffinity = &v1.PodAffinity{}
		}
		affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution = terms
	}

	if podAntiAffinitySelector != nil && len(podAntiAffinityTopologyKey) != 0 {
		terms := []v1.PodAffinityTerm{
			{
				TopologyKey: podAntiAffinityTopologyKey,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: podAntiAffinitySelector,
				},
			},
		}
		if affinity.PodAntiAffinity == nil {
			affinity.PodAntiAffinity = &v1.PodAntiAffinity{}
		}
		affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = terms
	}

	return affinity
}
