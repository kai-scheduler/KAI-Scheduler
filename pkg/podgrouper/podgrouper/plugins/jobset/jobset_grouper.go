// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jobset

import (
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	// JobSet pod labels (duplicated to avoid importing JobSet project).
	jobSetLabelJobSetName        = "jobset.sigs.k8s.io/jobset-name"
	jobSetLabelReplicatedJobName = "jobset.sigs.k8s.io/replicatedjob-name"
	jobSetPodGroupNamePrefix     = "pg"
	// startupPolicyOrderInOrder is the default startup policy order for JobSet.
	startupPolicyOrderInOrder = "InOrder"
)

// JobSetGrouper creates PodGroups for JobSet workloads.
// When startupPolicy.startupPolicyOrder is "InOrder" (default):
//   - Creates one PodGroup per replicatedJob to avoid sequencing deadlocks
//   - PodGroup name: pg-<jobset-name>-<jobset-uid>-<replicatedjob-name>
//
// When startupPolicy.startupPolicyOrder is not "InOrder":
//   - Creates a single PodGroup for all replicatedJobs
//   - PodGroup name: pg-<jobset-name>-<jobset-uid>
//
// MinAvailable defaults to 1. Use the kai.scheduler/batch-min-member annotation to override.
type JobSetGrouper struct {
	*defaultgrouper.DefaultGrouper
}

func NewJobSetGrouper(defaultGrouper *defaultgrouper.DefaultGrouper) *JobSetGrouper {
	return &JobSetGrouper{
		DefaultGrouper: defaultGrouper,
	}
}

func (g *JobSetGrouper) Name() string {
	return "JobSet Grouper"
}

// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=jobset.x-k8s.io,resources=jobsets/finalizers,verbs=patch;update;create

func (g *JobSetGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured,
	pod *v1.Pod,
	_ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	pgMeta, err := g.DefaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}

	jobSetName := topOwner.GetName()
	jobSetUID := topOwner.GetUID()
	if jobSetName == "" || len(jobSetUID) == 0 {
		return nil, fmt.Errorf("jobset top owner %s/%s missing name or UID", topOwner.GetNamespace(), topOwner.GetName())
	}

	replicatedJobName, ok := pod.Labels[jobSetLabelReplicatedJobName]
	if !ok || replicatedJobName == "" {
		return nil, fmt.Errorf("pod %s/%s missing required label %q", pod.Namespace, pod.Name, jobSetLabelReplicatedJobName)
	}

	startupPolicyOrder, err := getStartupPolicyOrder(topOwner)
	if err != nil {
		return nil, err
	}

	if startupPolicyOrder == startupPolicyOrderInOrder {
		pgMeta.Name = fmt.Sprintf(
			"%s-%s-%s-%s",
			jobSetPodGroupNamePrefix,
			jobSetName,
			string(jobSetUID),
			replicatedJobName,
		)
	} else {
		pgMeta.Name = fmt.Sprintf(
			"%s-%s-%s",
			jobSetPodGroupNamePrefix,
			jobSetName,
			string(jobSetUID),
		)
	}

	pgMeta.MinAvailable, err = getMinAvailable(topOwner)
	if err != nil {
		return nil, err
	}

	return pgMeta, nil
}

// getStartupPolicyOrder returns startupPolicy.startupPolicyOrder from JobSet spec.
func getStartupPolicyOrder(jobSet *unstructured.Unstructured) (string, error) {
	order, found, err := unstructured.NestedString(jobSet.Object, "spec", "startupPolicy", "startupPolicyOrder")
	if err != nil {
		return "", fmt.Errorf("failed to read spec.startupPolicy.startupPolicyOrder from JobSet %s/%s: %w",
			jobSet.GetNamespace(), jobSet.GetName(), err)
	}
	if !found {
		return startupPolicyOrderInOrder, nil
	}
	return order, nil
}

func getMinAvailable(topOwner *unstructured.Unstructured) (int32, error) {
	override, found := topOwner.GetAnnotations()[constants.MinMemberOverrideKey]
	if !found {
		return 1, nil
	}

	minMember, err := strconv.ParseInt(override, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s annotation value: %w", constants.MinMemberOverrideKey, err)
	}

	return int32(minMember), nil
}
