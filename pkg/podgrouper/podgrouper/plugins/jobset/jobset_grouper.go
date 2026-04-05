// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jobset

import (
	"fmt"
	"math"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
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
//   - MinAvailable: replicas * min(parallelism, completions if set) (defaults to 1)
//
// When startupPolicy.startupPolicyOrder is not "InOrder":
//   - Creates a single PodGroup for all replicatedJobs
//   - PodGroup name: pg-<jobset-name>-<jobset-uid>
//   - MinAvailable: sum of all replicatedJobs' minAvailable
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
		minAvailable, err := getReplicatedJobMinAvailable(topOwner, replicatedJobName)
		if err != nil {
			return nil, err
		}
		pgMeta.MinAvailable = minAvailable
	} else {
		pgMeta.Name = fmt.Sprintf(
			"%s-%s-%s",
			jobSetPodGroupNamePrefix,
			jobSetName,
			string(jobSetUID),
		)
		minAvailable, err := getJobSetMinAvailable(topOwner)
		if err != nil {
			return nil, err
		}
		pgMeta.MinAvailable = minAvailable
	}

	return pgMeta, nil
}

// calculateReplicatedJobMinAvailable calculates minAvailable for a single replicatedJob.
func calculateReplicatedJobMinAvailable(rjMap map[string]interface{}, jobSetNamespace, jobSetName, replicatedJobName string) (int32, error) {
	replicas64, foundReplicas, err := unstructured.NestedInt64(rjMap, "replicas")
	if err != nil {
		return 0, fmt.Errorf("failed to read replicas from JobSet %s/%s replicatedJob %s: %w",
			jobSetNamespace, jobSetName, replicatedJobName, err)
	}
	replicas := int64(1)
	if foundReplicas && replicas64 > 0 {
		replicas = replicas64
	}

	parallelism64, foundParallelism, err := unstructured.NestedInt64(rjMap, "template", "spec", "parallelism")
	if err != nil {
		return 0, fmt.Errorf("failed to read template.spec.parallelism from JobSet %s/%s replicatedJob %s: %w",
			jobSetNamespace, jobSetName, replicatedJobName, err)
	}
	parallelism := int64(1)
	if foundParallelism && parallelism64 > 0 {
		parallelism = parallelism64
	}

	completions64, foundCompletions, err := unstructured.NestedInt64(rjMap, "template", "spec", "completions")
	if err != nil {
		return 0, fmt.Errorf("failed to read template.spec.completions from JobSet %s/%s replicatedJob %s: %w",
			jobSetNamespace, jobSetName, replicatedJobName, err)
	}
	if foundCompletions && completions64 > 0 && completions64 < parallelism {
		parallelism = completions64
	}

	minAvailable64 := replicas * parallelism
	if minAvailable64 <= 0 {
		return 1, nil
	}
	if minAvailable64 > math.MaxInt32 {
		return 0, fmt.Errorf("minAvailable too large (%d) for JobSet %s/%s replicatedJob %s: exceeds int32 max value",
			minAvailable64, jobSetNamespace, jobSetName, replicatedJobName)
	}
	return int32(minAvailable64), nil
}

// getReplicatedJobMinAvailable returns minAvailable for a specific replicatedJob.
func getReplicatedJobMinAvailable(jobSet *unstructured.Unstructured, replicatedJobName string) (int32, error) {
	replicatedJobs, found, err := unstructured.NestedSlice(jobSet.Object, "spec", "replicatedJobs")
	if err != nil {
		return 0, fmt.Errorf("failed to read spec.replicatedJobs from JobSet %s/%s: %w",
			jobSet.GetNamespace(), jobSet.GetName(), err)
	}
	if !found || len(replicatedJobs) == 0 {
		return 1, nil
	}

	for _, rjRaw := range replicatedJobs {
		rjMap, ok := rjRaw.(map[string]interface{})
		if !ok {
			continue
		}

		name, _, _ := unstructured.NestedString(rjMap, "name")
		if name != replicatedJobName {
			continue
		}

		return calculateReplicatedJobMinAvailable(rjMap, jobSet.GetNamespace(), jobSet.GetName(), replicatedJobName)
	}

	return 1, nil
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

// getJobSetMinAvailable calculates total minAvailable for all replicatedJobs in the JobSet.
func getJobSetMinAvailable(jobSet *unstructured.Unstructured) (int32, error) {
	replicatedJobs, found, err := unstructured.NestedSlice(jobSet.Object, "spec", "replicatedJobs")
	if err != nil {
		return 0, fmt.Errorf("failed to read spec.replicatedJobs from JobSet %s/%s: %w",
			jobSet.GetNamespace(), jobSet.GetName(), err)
	}
	if !found || len(replicatedJobs) == 0 {
		return 1, nil
	}

	var totalMinAvailable int64 = 0
	for _, rjRaw := range replicatedJobs {
		rjMap, ok := rjRaw.(map[string]interface{})
		if !ok {
			continue
		}

		name, _, _ := unstructured.NestedString(rjMap, "name")
		if name == "" {
			continue
		}

		minAvailable, err := calculateReplicatedJobMinAvailable(rjMap, jobSet.GetNamespace(), jobSet.GetName(), name)
		if err != nil {
			return 0, err
		}
		totalMinAvailable += int64(minAvailable)
	}

	if totalMinAvailable <= 0 {
		return 1, nil
	}
	if totalMinAvailable > math.MaxInt32 {
		return 0, fmt.Errorf("total minAvailable too large (%d) for JobSet %s/%s: exceeds int32 max value",
			totalMinAvailable, jobSet.GetNamespace(), jobSet.GetName())
	}
	return int32(totalMinAvailable), nil
}
