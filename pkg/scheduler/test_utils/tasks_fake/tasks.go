// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package tasks_fake

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	commonconstants "github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/resources"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/pod_status"
)

const (
	ownerUUID       = "1234"
	NodeAffinityKey = "run.ai/type"
)

type TestTaskBasic struct {
	Name                       string
	GPUGroups                  []string
	RequiredGPUs               *int64
	State                      pod_status.PodStatus
	NodeName                   string // Relevant if job is running
	NodeAffinityNames          []string
	RequiredMigInstances       map[v1.ResourceName]int
	Priority                   *int
	IsLegacyMigTask            bool
	ResourceClaimTemplates     map[string]string
	ResourceClaimNames         []string
	PersistentVolumeClaimNames []string
}

func BuildPod(
	name, namespace, nodeName string,
	nodeAffinityNames []string,
	phase v1.PodPhase, req v1.ResourceList,
	gpuFraction, gpuMemory string, gpuGroups []string, jobName string,
	requiredMigInstances map[v1.ResourceName]int, claimNames []string, claimTemplates map[string]string,
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
			Labels: map[string]string{
				"job-name": name,
			},
			Annotations: map[string]string{
				common_info.GPUFraction:                  gpuFraction,
				pod_info.GpuMemoryAnnotationName:         gpuMemory,
				commonconstants.PodGroupAnnotationForPod: jobName,
			},
		},
		Status: v1.PodStatus{
			Phase: phase,
		},
		Spec: v1.PodSpec{
			NodeName: nodeName,
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: req,
					},
				},
			},
			SchedulerName: "kai-scheduler",
		},
	}
	if len(gpuGroups) > 1 {
		for _, gpuGroup := range gpuGroups {
			multiGroupKey, multiGroupValue := resources.GetMultiFractionGpuGroupLabel(gpuGroup)
			pod.Labels[multiGroupKey] = multiGroupValue
		}
	} else if len(gpuGroups) > 0 {
		pod.Labels[pod_info.GPUGroup] = gpuGroups[0]
	}

	if len(nodeAffinityNames) > 0 {
		affinity := &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      NodeAffinityKey,
								Operator: v1.NodeSelectorOpIn,
								Values:   nodeAffinityNames,
							},
						},
					},
				},
			},
		}
		pod.Spec.Affinity = &v1.Affinity{NodeAffinity: affinity}
	}

	for migInstance, count := range requiredMigInstances {
		pod.Annotations[migInstance.String()] = fmt.Sprintf("%d", count)
	}

	for _, claimName := range claimNames {
		pod.Spec.ResourceClaims = append(pod.Spec.ResourceClaims, v1.PodResourceClaim{
			Name:              claimName,
			ResourceClaimName: &claimName,
		})
	}

	for templateName, claimName := range claimTemplates {
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
