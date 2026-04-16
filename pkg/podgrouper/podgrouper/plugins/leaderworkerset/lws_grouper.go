// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package leader_worker_set

import (
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	startupPolicyLeaderReady   = "LeaderReady"
	startupPolicyLeaderCreated = "LeaderCreated"

	// LWS annotation and label keys
	lwsSizeAnnotation   = "leaderworkerset.sigs.k8s.io/size"
	lwsGroupIndexLabel  = "leaderworkerset.sigs.k8s.io/group-index"
	lwsWorkerIndexLabel = "leaderworkerset.sigs.k8s.io/worker-index"
)

type LwsGrouper struct {
	*defaultgrouper.DefaultGrouper
}

func NewLwsGrouper(defaultGrouper *defaultgrouper.DefaultGrouper) *LwsGrouper {
	return &LwsGrouper{
		DefaultGrouper: defaultGrouper,
	}
}

func (lwsGrouper *LwsGrouper) Name() string {
	return "LWS Grouper"
}

// +kubebuilder:rbac:groups=leaderworkerset.x-k8s.io,resources=leaderworkersets,verbs=get;list;watch
// +kubebuilder:rbac:groups=leaderworkerset.x-k8s.io,resources=leaderworkersets/finalizers,verbs=patch;update;create

func (lwsGrouper *LwsGrouper) GetPodGroupMetadata(
	lwsJob *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	podGroupMetadata, err := lwsGrouper.DefaultGrouper.GetPodGroupMetadata(lwsJob, pod)
	if err != nil {
		return nil, err
	}

	groupSize, err := lwsGrouper.getLwsGroupSize(lwsJob)
	if err != nil {
		return nil, err
	}

	startupPolicy, err := lwsGrouper.getStartupPolicy(lwsJob)
	if err != nil {
		return nil, err
	}

	// Initialize podGroupMetadata with the group size
	switch startupPolicy {
	case startupPolicyLeaderReady:
		if err := handleLeaderReadyPolicy(pod, podGroupMetadata, groupSize); err != nil {
			return nil, fmt.Errorf("error handling leader ready policy: %w", err)
		}
	case startupPolicyLeaderCreated:
		podGroupMetadata.MinAvailable = groupSize
	default:
		return nil, fmt.Errorf("unknown startupPolicy: %s", startupPolicy)
	}

	if groupIndexStr, ok := pod.Labels[lwsGroupIndexLabel]; ok {
		if groupIndex, err := strconv.Atoi(groupIndexStr); err == nil {
			podGroupMetadata.Name = fmt.Sprintf("%s-group-%d", podGroupMetadata.Name, groupIndex)
		}
	}

	return podGroupMetadata, nil
}

func (lwsGrouper *LwsGrouper) getLwsGroupSize(lwsJob *unstructured.Unstructured) (int32, error) {
	size, found, err := unstructured.NestedInt64(lwsJob.Object, "spec", "leaderWorkerTemplate", "size")
	if err != nil {
		return 0, fmt.Errorf("failed to get leaderWorkerTemplate.size from LWS %s/%s with error: %w",
			lwsJob.GetNamespace(), lwsJob.GetName(), err)
	}
	if !found {
		return 0, fmt.Errorf("leaderWorkerTemplate.size not found in LWS %s/%s", lwsJob.GetNamespace(), lwsJob.GetName())
	}
	if size <= 0 {
		return 0, fmt.Errorf("invalid leaderWorkerTemplate.size %d in LWS %s/%s", size, lwsJob.GetNamespace(), lwsJob.GetName())
	}
	return int32(size), nil
}

// getStartupPolicy extracts the startup policy from the LWS object
func (lwsGrouper *LwsGrouper) getStartupPolicy(lwsJob *unstructured.Unstructured) (string, error) {
	policy, found, err := unstructured.NestedString(lwsJob.Object, "spec", "startupPolicy")
	if err != nil {
		return "", fmt.Errorf("failed to get startupPolicy from LWS %s/%s: %w",
			lwsJob.GetNamespace(), lwsJob.GetName(), err)
	}
	if !found {
		// Default to LeaderCreated if not specified
		return startupPolicyLeaderCreated, nil
	}
	return policy, nil
}

func handleLeaderReadyPolicy(pod *v1.Pod, podGroupMetadata *podgroup.Metadata, fallbackSize int32) error {
	groupSize := fallbackSize

	// Check for the size annotation on the pod
	if sizeStr, ok := pod.Annotations[lwsSizeAnnotation]; ok {
		if parsed, err := strconv.Atoi(sizeStr); err == nil {
			groupSize = int32(parsed)
		}
	}

	workerIndex, hasWorkerIndex := pod.Labels[lwsWorkerIndexLabel]
	isLeader := hasWorkerIndex && workerIndex == "0"
	isScheduled := pod.Spec.NodeName != ""

	if isLeader && !isScheduled {
		// Leader pod not yet scheduled, only need leader to be available
		podGroupMetadata.MinAvailable = 1
	} else {
		// Either worker pod or leader is already scheduled
		podGroupMetadata.MinAvailable = groupSize
	}

	return nil
}
