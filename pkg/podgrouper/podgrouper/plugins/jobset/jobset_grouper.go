// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jobset

import (
	"context"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	// JobSet pod labels (duplicated to avoid importing JobSet project).
	jobSetLabelReplicatedJobName = "jobset.sigs.k8s.io/replicatedjob-name"
	jobSetLabelJobIndex          = "jobset.sigs.k8s.io/job-index"
	jobSetPodGroupNamePrefix     = "pg"
	// startupPolicyOrderInOrder is the InOrder startup policy order value.
	startupPolicyOrderAnyOrder = "AnyOrder"
	startupPolicyOrderInOrder  = "InOrder"

	jobSetPodGroupNameFormat   = "%s-%s-%s"
	replicasSubgroupNameFormat = "%s-replica-%s"
)

// JobSetGrouper creates a single PodGroup per JobSet with a two-level SubGroup
// hierarchy: one parent SubGroup per replicatedJob and one leaf SubGroup per
// replica. The root PodGroup uses MinSubGroup to allow incremental scheduling
// (JobSet's controller enforces start ordering when configured).
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

func (g *JobSetGrouper) GetPodGroupMetadata(jobsetObj *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	pgMeta, err := g.DefaultGrouper.GetPodGroupMetadata(jobsetObj, pod)
	if err != nil {
		return nil, err
	}

	jobSetName := jobsetObj.GetName()
	jobSetUID := jobsetObj.GetUID()
	if jobSetName == "" || len(jobSetUID) == 0 {
		return nil, fmt.Errorf("jobset top owner %s/%s missing name or UID", jobsetObj.GetNamespace(), jobsetObj.GetName())
	}

	replicatedJobs, err := getReplicatedJobs(jobsetObj)
	if err != nil {
		return nil, err
	}
	if len(replicatedJobs) == 0 {
		return nil, fmt.Errorf("jobset %s/%s has no replicatedJobs", jobsetObj.GetNamespace(), jobSetName)
	}

	podGroupMinSubGroup, err := computePodGroupMinSubGroup(jobsetObj, replicatedJobs)
	if err != nil {
		return nil, err
	}

	pgMeta.Name = fmt.Sprintf(jobSetPodGroupNameFormat, jobSetPodGroupNamePrefix, jobSetName, string(jobSetUID))
	pgMeta.MinSubGroup = ptr.To(podGroupMinSubGroup)
	pgMeta.MinAvailable = 0

	subGroups, err := buildSubGroups(replicatedJobs)
	if err != nil {
		return nil, err
	}
	pgMeta.SubGroups = subGroups

	if err := assignPodToSubGroup(pod, pgMeta.SubGroups); err != nil {
		return nil, err
	}

	return pgMeta, nil
}

func computePodGroupMinSubGroup(jobSet *unstructured.Unstructured, replicatedJobs []map[string]any) (int32, error) {
	numReplicatedJobs := int32(len(replicatedJobs))

	if override, ok := jobSet.GetAnnotations()[constants.MinMemberOverrideKey]; ok {
		userSetMinSubGroup, err := strconv.ParseInt(override, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid %s annotation on JobSet: %w", constants.MinMemberOverrideKey, err)
		}
		if userSetMinSubGroup < 1 {
			return 0, fmt.Errorf("invalid %s annotation on JobSet: value %d must be >= 1", constants.MinMemberOverrideKey, userSetMinSubGroup)
		}
		if int32(userSetMinSubGroup) < numReplicatedJobs {
			return int32(userSetMinSubGroup), nil
		}
		return numReplicatedJobs, nil
	}

	orderPolicy, err := getStartupPolicyOrder(jobSet)
	if err != nil {
		return 0, err
	}
	// If the order policy is InOrder, set the MinSubGroup to 1. The jobset controller will create one replicatedJob at a time.
	if orderPolicy == startupPolicyOrderInOrder {
		return 1, nil
	}
	// If the order policy is AnyOrder, set the MinSubGroup to the number of replicatedJobs.
	return numReplicatedJobs, nil
}

// buildSubGroups produces the parent-per-replicatedJob / leaf-per-replica tree.
func buildSubGroups(replicatedJobs []map[string]any) ([]*podgroup.SubGroupMetadata, error) {
	var subGroups []*podgroup.SubGroupMetadata
	for i, rj := range replicatedJobs {
		replicatedJobName, found, err := unstructured.NestedString(rj, "name")
		if err != nil || !found || replicatedJobName == "" {
			return nil, fmt.Errorf("replicatedJobs[%d] missing name", i)
		}

		replicasCount, err := getReplicas(rj)
		if err != nil {
			return nil, err
		}

		leafMinMember, err := singleReplicaSubGroupMinMember(rj)
		if err != nil {
			return nil, err
		}

		parentName := replicatedJobName
		subGroups = append(subGroups, &podgroup.SubGroupMetadata{
			Name:        parentName,
			MinSubGroup: ptr.To(replicasCount),
		})
		for idx := int32(0); idx < replicasCount; idx++ {
			leafName := fmt.Sprintf(replicasSubgroupNameFormat, parentName, strconv.Itoa(int(idx)))
			subGroups = append(subGroups, &podgroup.SubGroupMetadata{
				Name:         leafName,
				MinAvailable: leafMinMember,
				Parent:       ptr.To(parentName),
			})
		}
	}
	return subGroups, nil
}

func singleReplicaSubGroupMinMember(rj map[string]any) (int32, error) {
	parallelism, err := getParallelism(rj)
	if err != nil {
		return 0, err
	}

	singleReplicaUserSetMinMember, found, err := unstructured.NestedString(rj, "template", "metadata", "annotations", constants.MinMemberOverrideKey)
	if err != nil {
		return 0, fmt.Errorf("failed to read template annotation %s: %w", constants.MinMemberOverrideKey, err)
	}
	if !found {
		return parallelism, nil
	}

	userSetMinMember, err := strconv.ParseInt(singleReplicaUserSetMinMember, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s annotation on replicatedJob template: %w", constants.MinMemberOverrideKey, err)
	}
	if userSetMinMember < 1 {
		return 0, fmt.Errorf("invalid %s annotation on replicatedJob template: value %d must be >= 1", constants.MinMemberOverrideKey, userSetMinMember)
	}
	if int32(userSetMinMember) > parallelism {
		log.FromContext(context.Background()).Info(
			"min-member annotation exceeds parallelism; applying user value",
			"annotation", constants.MinMemberOverrideKey,
			"value", userSetMinMember,
			"parallelism", parallelism,
		)
	}
	return int32(userSetMinMember), nil
}

// assignPodToSubGroup routes the pod into its leaf SubGroup based on JobSet labels.
func assignPodToSubGroup(pod *v1.Pod, subGroups []*podgroup.SubGroupMetadata) error {
	rj, ok := pod.Labels[jobSetLabelReplicatedJobName]
	if !ok || rj == "" {
		return fmt.Errorf("pod %s/%s missing required label %q", pod.Namespace, pod.Name, jobSetLabelReplicatedJobName)
	}
	idx, ok := pod.Labels[jobSetLabelJobIndex]
	if !ok || idx == "" {
		return fmt.Errorf("pod %s/%s missing required label %q", pod.Namespace, pod.Name, jobSetLabelJobIndex)
	}

	leafName := fmt.Sprintf(replicasSubgroupNameFormat, rj, idx)
	for _, sg := range subGroups {
		if sg.Name == leafName {
			sg.PodsReferences = append(sg.PodsReferences, pod.Name)
			return nil
		}
	}
	return fmt.Errorf("leaf subgroup %q not found for pod %s/%s", leafName, pod.Namespace, pod.Name)
}

// Parse jobset crd functions

// getReplicatedJobs returns spec.replicatedJobs as a slice of unstructured maps.
func getReplicatedJobs(jobSet *unstructured.Unstructured) ([]map[string]any, error) {
	raw, found, err := unstructured.NestedSlice(jobSet.Object, "spec", "replicatedJobs")
	if err != nil {
		return nil, fmt.Errorf("failed to read spec.replicatedJobs from JobSet %s/%s: %w",
			jobSet.GetNamespace(), jobSet.GetName(), err)
	}
	if !found {
		return nil, nil
	}
	out := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		rj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("replicatedJobs[%d] is not an object", i)
		}
		out = append(out, rj)
	}
	return out, nil
}

// getStartupPolicyOrder returns spec.startupPolicy.startupPolicyOrder, defaulting to AnyOrder.
func getStartupPolicyOrder(jobSet *unstructured.Unstructured) (string, error) {
	order, found, err := unstructured.NestedString(jobSet.Object, "spec", "startupPolicy", "startupPolicyOrder")
	if err != nil {
		return "", fmt.Errorf("failed to read spec.startupPolicy.startupPolicyOrder from JobSet %s/%s: %w",
			jobSet.GetNamespace(), jobSet.GetName(), err)
	}
	if !found {
		return startupPolicyOrderAnyOrder, nil
	}
	return order, nil
}

// getParallelism reads replicatedJob.template.spec.parallelism, defaulting to 1.
func getParallelism(rj map[string]any) (int32, error) {
	v, found, err := unstructured.NestedInt64(rj, "template", "spec", "parallelism")
	if err != nil {
		return 0, fmt.Errorf("failed to read template.spec.parallelism: %w", err)
	}
	if !found {
		return 1, nil
	}
	return int32(v), nil
}

// getReplicas reads replicatedJob.replicas, defaulting to 1.
func getReplicas(rj map[string]any) (int32, error) {
	v, found, err := unstructured.NestedInt64(rj, "replicas")
	if err != nil {
		return 0, fmt.Errorf("failed to read replicatedJob.replicas: %w", err)
	}
	if !found {
		return 1, nil
	}
	return int32(v), nil
}
