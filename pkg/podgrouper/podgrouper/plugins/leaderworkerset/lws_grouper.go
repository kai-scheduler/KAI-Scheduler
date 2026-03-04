// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package leader_worker_set

import (
	"fmt"
	"slices"
	"strconv"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	lws "sigs.k8s.io/lws/api/leaderworkerset/v1"

	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	startupPolicyLeaderReady   = "LeaderReady"
	startupPolicyLeaderCreated = "LeaderCreated"

	leaderSubGroupName  = "leader"
	workersSubGroupName = "workers"

	leaderSubGroupSize = 1
	leaderIndex        = "0"

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

func (lwsg *LwsGrouper) Name() string {
	return "LWS Grouper"
}

// +kubebuilder:rbac:groups=leaderworkerset.x-k8s.io,resources=leaderworkersets,verbs=get;list;watch
// +kubebuilder:rbac:groups=leaderworkerset.x-k8s.io,resources=leaderworkersets/finalizers,verbs=patch;update;create

func (lwsg *LwsGrouper) GetPodGroupMetadata(
	lwsJob *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	podGroupMetadata, err := lwsg.DefaultGrouper.GetPodGroupMetadata(lwsJob, pod)
	if err != nil {
		return nil, err
	}

	groupSize, err := getGroupSize(lwsJob)
	if err != nil {
		return nil, err
	}

	startupPolicy, err := getStartupPolicy(lwsJob)
	if err != nil {
		return nil, err
	}

	switch startupPolicy {
	case startupPolicyLeaderReady:
		podGroupMetadata.MinAvailable = calcLeaderReadyMinAvailable(pod, groupSize)
	case startupPolicyLeaderCreated:
		podGroupMetadata.MinAvailable = groupSize
	default:
		return nil, fmt.Errorf("unknown startupPolicy: %s", startupPolicy)
	}

	subGroups, err := lwsg.buildSubGroups(lwsJob, pod, int(podGroupMetadata.MinAvailable))
	if err != nil {
		return nil, err
	}
	podGroupMetadata.SubGroups = subGroups

	if groupIndexStr, ok := pod.Labels[lwsGroupIndexLabel]; ok {
		if groupIndex, err := strconv.Atoi(groupIndexStr); err == nil {
			podGroupMetadata.Name = fmt.Sprintf("%s-group-%d", podGroupMetadata.Name, groupIndex)
		}
	}

	return podGroupMetadata, nil
}

func getGroupSize(lwsJob *unstructured.Unstructured) (int32, error) {
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

func getStartupPolicy(lwsJob *unstructured.Unstructured) (string, error) {
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

func calcLeaderReadyMinAvailable(pod *v1.Pod, fallbackSize int32) int32 {
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
		return 1
	}
	// Either worker pod or leader is already scheduled
	return groupSize
}

func (lwsg *LwsGrouper) buildSubGroups(lwsJob *unstructured.Unstructured, pod *v1.Pod, replicasSize int) ([]*podgroup.SubGroupMetadata, error) {
	segmentationPolicy, err := getSegmentationPolicy(lwsJob, replicasSize)
	if err != nil {
		return nil, fmt.Errorf("failed to get sub group policy for LWS %s/%s: %w", lwsJob.GetNamespace(), lwsJob.GetName(), err)
	}
	if segmentationPolicy == nil {
		return buildSubGroupsWithoutSegmentation(replicasSize, pod), nil
	}
	topologyConstraints, err := getSegmentTopologyConstraints(pod, lwsJob)
	if err != nil {
		return nil, fmt.Errorf("failed to get segment topology constraints for LWS %s/%s: %w", lwsJob.GetNamespace(), lwsJob.GetName(), err)
	}
	return buildSubGroupsWithSegmentation(segmentationPolicy, topologyConstraints, replicasSize, pod)
}

func getSegmentTopologyConstraints(pod *v1.Pod, lwsJob *unstructured.Unstructured) (*podgroup.TopologyConstraintMetadata, error) {
	workerTemplate, found, err := unstructured.NestedMap(lwsJob.Object, "spec", "leaderWorkerTemplate", "workerTemplate")
	if err != nil {
		return nil, fmt.Errorf("failed to get workerTemplate from LWS %s/%s: %w", lwsJob.GetNamespace(), lwsJob.GetName(), err)
	}
	if !found {
		return nil, nil
	}
	topology := getTopology(pod, workerTemplate, lwsJob)
	if topology == "" {
		return nil, nil
	}

	required := getWorkerAnnotationValue(pod, workerTemplate, constants.SegmentTopologyRequiredPlacementKey)
	preferred := getWorkerAnnotationValue(pod, workerTemplate, constants.SegmentTopologyPreferredPlacementKey)
	if required == "" && preferred == "" {
		return nil, nil
	}

	return &podgroup.TopologyConstraintMetadata{
		Topology:               topology,
		RequiredTopologyLevel:  required,
		PreferredTopologyLevel: preferred,
	}, nil
}

func getTopology(pod *v1.Pod, workerTemplate map[string]interface{}, lwsJob *unstructured.Unstructured) string {
	if topology, found := pod.Annotations[constants.TopologyKey]; found && topology != "" {
		return topology
	}
	topology := getWorkerAnnotationValue(pod, workerTemplate, constants.TopologyKey)
	if topology != "" {
		return topology
	}
	if topology, found := lwsJob.GetAnnotations()[constants.TopologyKey]; found {
		return topology
	}
	return ""
}

func getWorkerAnnotationValue(pod *v1.Pod, workerTemplate map[string]interface{}, key string) string {
	if value, found := pod.Annotations[key]; found && value != "" {
		return value
	}
	value, found, _ := unstructured.NestedString(workerTemplate, "metadata", "annotations", key)
	if found {
		return value
	}
	return ""
}

// getSegmentationPolicy resolves the sub-group segmentation policy from the LWS spec.
// The segment size is read first from spec.leaderWorkerTemplate.subGroupPolicy.subGroupSize,
// then falls back to the kai.scheduler/segment-size annotation on the LWS object,
// and finally to the kai.scheduler/segment-size annotation on the workerTemplate.

// The policy type, which defines if the leader pod will be included in a subgroup under the segments or sits in a separate subgroup,
// is read from subGroupPolicyType and defaults to LeaderWorker when not specified.
//
// Returns nil when no segment size is defined (segmentation disabled).
func getSegmentationPolicy(lwsJob *unstructured.Unstructured, replicasSize int) (*lws.SubGroupPolicy, error) {
	segmentSize, err := getSegmentSize(lwsJob, replicasSize)
	if err != nil || segmentSize == nil {
		return nil, err
	}
	policyStr, found, err := unstructured.NestedString(lwsJob.Object, "spec", "leaderWorkerTemplate", "subGroupPolicy", "subGroupPolicyType")
	if err != nil {
		return nil, err
	}
	policy := lws.SubGroupPolicyTypeLeaderWorker
	if found {
		policy = lws.SubGroupPolicyType(policyStr)
	}
	return &lws.SubGroupPolicy{
		SubGroupSize: segmentSize,
		Type:         ptr.To(lws.SubGroupPolicyType(policy)),
	}, nil
}

func getSegmentSize(lwsJob *unstructured.Unstructured, replicasSize int) (*int32, error) {
	segmentSizeInt64, foundSegmentDefinition, err := unstructured.NestedInt64(lwsJob.Object, "spec", "leaderWorkerTemplate", "subGroupPolicy",
		"subGroupSize")
	if err != nil {
		return nil, err
	}
	segmentSize := int(segmentSizeInt64)
	if !foundSegmentDefinition {
		if segmentSizeStr, found := lwsJob.GetAnnotations()[constants.SegmentSizeKey]; found {
			if segmentSize, err = strconv.Atoi(segmentSizeStr); err == nil {
				foundSegmentDefinition = true
			} else {
				return nil, fmt.Errorf("invalid segment size annotation %s in LWS %s/%s: %w",
					segmentSizeStr, lwsJob.GetNamespace(), lwsJob.GetName(), err)
			}
		}
	}
	if !foundSegmentDefinition {
		if segmentSizeStr, found, _ := unstructured.NestedString(lwsJob.Object, "spec", "leaderWorkerTemplate", "workerTemplate",
			"metadata", "annotations", constants.SegmentSizeKey); found {
			if segmentSize, err = strconv.Atoi(segmentSizeStr); err == nil {
				foundSegmentDefinition = true
			} else {
				return nil, fmt.Errorf("invalid segment size annotation %s in workerTemplate of LWS %s/%s: %w",
					segmentSizeStr, lwsJob.GetNamespace(), lwsJob.GetName(), err)
			}
		}
	}
	if !foundSegmentDefinition {
		return nil, nil
	}
	if segmentSize <= 1 {
		return nil, fmt.Errorf("segmentSize %d is not valid. It must be greater than 1", segmentSize)
	}
	if segmentSize > replicasSize {
		return nil, fmt.Errorf("segmentSize %d is greater than replicasSize %d", segmentSize, replicasSize)
	}
	return ptr.To(int32(segmentSize)), nil
}

func buildSubGroupsWithoutSegmentation(replicasSize int, pod *v1.Pod) []*podgroup.SubGroupMetadata {
	leaderSubGroup := &podgroup.SubGroupMetadata{
		Name:           leaderSubGroupName,
		MinAvailable:   leaderSubGroupSize,
		PodsReferences: []string{},
	}
	if isLeaderPod(pod) {
		leaderSubGroup.PodsReferences = append(leaderSubGroup.PodsReferences, pod.Name)
	}
	subGroups := []*podgroup.SubGroupMetadata{leaderSubGroup}

	if replicasSize-leaderSubGroupSize > 0 {
		workerSubGroup := &podgroup.SubGroupMetadata{
			Name:           workersSubGroupName,
			MinAvailable:   int32(replicasSize - leaderSubGroupSize),
			PodsReferences: []string{},
		}
		if !isLeaderPod(pod) {
			workerSubGroup.PodsReferences = append(workerSubGroup.PodsReferences, pod.Name)
		}
		subGroups = append(subGroups, workerSubGroup)
	}
	return subGroups
}

func buildSubGroupsWithSegmentation(
	segmentationPolicy *lws.SubGroupPolicy, topologyConstraints *podgroup.TopologyConstraintMetadata,
	replicasSize int, pod *v1.Pod) ([]*podgroup.SubGroupMetadata, error) {
	segmentSize := int(*segmentationPolicy.SubGroupSize)
	policy := *segmentationPolicy.Type

	subGroups := createSegmentSubgroups(topologyConstraints, replicasSize, segmentSize)
	podSegment, err := getPodSegment(pod, replicasSize, segmentSize)
	if err != nil {
		return nil, err
	}

	setPodSubgroupReference := false
	if policy == lws.SubGroupPolicyTypeLeaderWorker {
		subGroups, setPodSubgroupReference = handleLeaderInFirstSegment(subGroups, replicasSize, segmentSize, pod, podSegment)
	}
	if policy == lws.SubGroupPolicyTypeLeaderExcluded {
		subGroups, err = addExcludedLeaderSegments(subGroups, replicasSize, segmentSize)
		if err != nil {
			return nil, err
		}

		if !isLeaderPod(pod) {
			// We added an extra subgroup, that will contain only the leader.
			// We need to adjust the pod segment index for workers to point to the correct subgroup.
			podSegment += 1
		}
	}
	if !setPodSubgroupReference {
		subGroups[podSegment].PodsReferences = append(subGroups[podSegment].PodsReferences, pod.Name)
	}

	return subGroups, nil
}

func createSegmentSubgroups(topologyConstraints *podgroup.TopologyConstraintMetadata, replicasSize int, segmentSize int) []*podgroup.SubGroupMetadata {
	subGroups := []*podgroup.SubGroupMetadata{}
	maxWorkerIndex := replicasSize - leaderSubGroupSize
	numOfSegmentSubgroups := getSegmentIndex(maxWorkerIndex, replicasSize, segmentSize) + 1
	for segmentIndex := 0; segmentIndex < numOfSegmentSubgroups; segmentIndex++ {
		subGroups = append(subGroups, &podgroup.SubGroupMetadata{
			Name:                fmt.Sprintf("segment-%d", segmentIndex),
			TopologyConstraints: topologyConstraints,
			MinAvailable:        int32(segmentSize),
			PodsReferences:      []string{},
		})
	}

	fixLastSegmentSize(replicasSize, segmentSize, subGroups)
	return subGroups
}

// The last segment might contain less pods then the other segments.
// This function calculates how many workers will actually go into the last segment.
func fixLastSegmentSize(replicasSize int, segmentSize int, subGroups []*podgroup.SubGroupMetadata) {
	lastSegment := subGroups[len(subGroups)-1]
	lastSegmentWorkersCount := 0
	maxWorkerIndex := replicasSize - leaderSubGroupSize
	for workerIndex := maxWorkerIndex; workerIndex >= 0; workerIndex-- {
		segmentIndex := getSegmentIndex(workerIndex, replicasSize, segmentSize)
		if segmentIndex == len(subGroups)-1 {
			lastSegmentWorkersCount++
		} else {
			break
		}
	}
	lastSegment.MinAvailable = int32(lastSegmentWorkersCount)
}

func getPodSegment(pod *v1.Pod, replicasSize int, segmentSize int) (int, error) {
	podWorkerIndex, err := strconv.Atoi(pod.Labels[lwsWorkerIndexLabel])
	if err != nil {
		return 0, fmt.Errorf("failed to get worker index from pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	podSegmentIndex := getSegmentIndex(podWorkerIndex, replicasSize, segmentSize)
	return podSegmentIndex, nil
}

func handleLeaderInFirstSegment(
	subGroups []*podgroup.SubGroupMetadata, replicasSize int, segmentSize int, pod *v1.Pod, podSegment int) ([]*podgroup.SubGroupMetadata, bool) {
	setPodSubgroupReference := false
	firstSegmentSubGroup := subGroups[0]
	if isReplicasSizeDivisibleBySubGroupSize(replicasSize, segmentSize) {
		// the first segment will contain pods (0, 1, ... subGroupSize) if replicasSize is divisible by segmentSize
		firstSegmentSubGroup.MinAvailable += leaderSubGroupSize
	}

	var segmentSubGroups []*podgroup.SubGroupMetadata
	segmentSubGroups, setPodSubgroupReference = addLeaderAndWorkersSubgroupsForSegment(firstSegmentSubGroup.Name,
		firstSegmentSubGroup.MinAvailable, pod, podSegment)
	subGroups = append(subGroups, segmentSubGroups...)
	return subGroups, setPodSubgroupReference
}

func addLeaderAndWorkersSubgroupsForSegment(
	segmentSubGroupName string, segmentSubGroupMinAvailable int32, pod *v1.Pod, podSubGroupIndex int) ([]*podgroup.SubGroupMetadata, bool) {
	setPodSubgroupReference := false

	subGroups := []*podgroup.SubGroupMetadata{}
	leaderSubGroup := &podgroup.SubGroupMetadata{
		Name:           leaderSubGroupName,
		MinAvailable:   leaderSubGroupSize,
		Parent:         ptr.To(segmentSubGroupName),
		PodsReferences: []string{},
	}
	if isLeaderPod(pod) {
		leaderSubGroup.PodsReferences = append(leaderSubGroup.PodsReferences, pod.Name)
		setPodSubgroupReference = true
	}
	subGroups = append(subGroups, leaderSubGroup)

	firstSubGroupWorkers := &podgroup.SubGroupMetadata{
		Name:           workersSubGroupName,
		MinAvailable:   segmentSubGroupMinAvailable - leaderSubGroupSize,
		Parent:         ptr.To(segmentSubGroupName),
		PodsReferences: []string{},
	}
	if podSubGroupIndex == 0 && !isLeaderPod(pod) {
		firstSubGroupWorkers.PodsReferences = append(firstSubGroupWorkers.PodsReferences, pod.Name)
		setPodSubgroupReference = true
	}
	subGroups = append(subGroups, firstSubGroupWorkers)
	return subGroups, setPodSubgroupReference
}

func addExcludedLeaderSegments(
	subGroups []*podgroup.SubGroupMetadata, replicasSize int, segmentSize int) ([]*podgroup.SubGroupMetadata, error) {
	if !isReplicasSizeDivisibleBySubGroupSize(replicasSize, segmentSize) {
		return nil, fmt.Errorf("replicasSize %d is not divisible by segmentSize %d. "+
			"LeaderExcluded policy is only supported when replicasSize is divisible by segmentSize.", replicasSize, segmentSize)
	}
	subGroups = slices.Insert(subGroups, 0, &podgroup.SubGroupMetadata{
		Name:           leaderSubGroupName,
		MinAvailable:   leaderSubGroupSize,
		PodsReferences: []string{},
	})
	return subGroups, nil
}

func isReplicasSizeDivisibleBySubGroupSize(replicasSize int, subGroupSize int) bool {
	return (replicasSize-leaderSubGroupSize)%subGroupSize == 0
}

func getSegmentIndex(workerIndex int, replicasSize int, subGroupSize int) int {
	if workerIndex == 0 {
		return 0 // leader is always in the first subgroup
	}
	if isReplicasSizeDivisibleBySubGroupSize(replicasSize, subGroupSize) {
		// Leader is considered as extra pod, it is part of the first group
		return (workerIndex - 1) / subGroupSize
	}
	return workerIndex / subGroupSize
}

func isLeaderPod(pod *v1.Pod) bool {
	workerIndex, hasWorkerIndex := pod.Labels[lwsWorkerIndexLabel]
	return hasWorkerIndex && workerIndex == leaderIndex
}
