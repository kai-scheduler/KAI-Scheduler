// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pytorch

import (
	"fmt"
	"strconv"
	"strings"

	pytorchv1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow"
)

const (
	replicaSpecName  = "pytorchReplicaSpecs"
	replicaTypeLabel = pytorchv1.ReplicaTypeLabel

	replicaTypeMaster = string(pytorchv1.PyTorchJobReplicaTypeMaster)
	replicaTypeWorker = string(pytorchv1.PyTorchJobReplicaTypeWorker)
)

type PyTorchGrouper struct {
	*kubeflow.KubeflowDistributedGrouper
}

func NewPyTorchGrouper(kubeflowGrouper *kubeflow.KubeflowDistributedGrouper) *PyTorchGrouper {
	return &PyTorchGrouper{
		KubeflowDistributedGrouper: kubeflowGrouper,
	}
}

func (ptg *PyTorchGrouper) Name() string {
	return "PyTorchJob Grouper"
}

func (ptg *PyTorchGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	podGroupMetadata, err := ptg.KubeflowDistributedGrouper.GetPodGroupMetadata(topOwner, pod, replicaSpecName, []string{})
	if err != nil {
		return nil, err
	}

	minReplicas, err := getMinReplicas(topOwner)
	if err == nil {
		podGroupMetadata.MinAvailable = int32(minReplicas)
	}

	minAvailable, err := getMinAvailable(topOwner)
	if err == nil {
		podGroupMetadata.MinAvailable = int32(minAvailable)
	}

	subGroups, err := ptg.buildSubGroups(topOwner, pod, podGroupMetadata.MinAvailable)
	if err != nil {
		return nil, err
	}
	podGroupMetadata.SubGroups = subGroups

	return podGroupMetadata, nil
}

func (ptg *PyTorchGrouper) buildSubGroups(
	topOwner *unstructured.Unstructured, pod *v1.Pod, totalMinAvailable int32,
) ([]*podgroup.SubGroupMetadata, error) {
	replicaSpecs, found, err := unstructured.NestedMap(topOwner.Object, "spec", "pytorchReplicaSpecs")
	if err != nil {
		return nil, fmt.Errorf("failed to get pytorchReplicaSpecs from PyTorchJob %s/%s. Err: %w", topOwner.GetNamespace(), topOwner.GetName(), err)
	}
	if !found {
		return nil, fmt.Errorf("pytorchReplicaSpecs not found in PyTorchJob %s/%s", topOwner.GetNamespace(), topOwner.GetName())
	}

	masterReplicas, found, err := unstructured.NestedInt64(replicaSpecs, replicaTypeMaster, "replicas")
	if err != nil {
		return nil, fmt.Errorf("failed to get replicas from pytorchReplicaSpecs[%s] in PyTorchJob %s/%s. Err: %w", replicaTypeMaster, topOwner.GetNamespace(), topOwner.GetName(), err)
	}
	if !found {
		masterReplicas = 0
	}

	var subGroups []*podgroup.SubGroupMetadata

	masterSubGroup := buildMasterSubGroup(replicaSpecs, pod, int32(masterReplicas))
	if masterSubGroup != nil {
		subGroups = append(subGroups, masterSubGroup)
	}

	workerMinAvailable := max(0, totalMinAvailable-int32(masterReplicas))
	workerSubGroups, err := buildWorkerSubGroups(replicaSpecs, pod, workerMinAvailable, topOwner)
	if err != nil {
		return nil, err
	}
	subGroups = append(subGroups, workerSubGroups...)

	return subGroups, nil
}

func buildMasterSubGroup(replicaSpecs map[string]interface{}, pod *v1.Pod, masterReplicas int32) *podgroup.SubGroupMetadata {
	if _, exists := replicaSpecs[replicaTypeMaster]; !exists {
		return nil
	}
	if masterReplicas == 0 {
		return nil
	}

	var podReferences []string
	if pod.Labels[replicaTypeLabel] == strings.ToLower(string(replicaTypeMaster)) {
		podReferences = append(podReferences, pod.Name)
	}

	return &podgroup.SubGroupMetadata{
		Name:           strings.ToLower(replicaTypeMaster),
		MinAvailable:   masterReplicas,
		PodsReferences: podReferences,
	}
}

func buildWorkerSubGroups(
	replicaSpecs map[string]interface{}, pod *v1.Pod, workerMinAvailable int32, topOwner *unstructured.Unstructured,
) ([]*podgroup.SubGroupMetadata, error) {
	if _, exists := replicaSpecs[replicaTypeWorker]; !exists {
		return nil, nil
	}

	workerReplicas, found, err := unstructured.NestedInt64(replicaSpecs, replicaTypeWorker, "replicas")
	if err != nil {
		return nil, fmt.Errorf("failed to get replicas for worker groups. Err: %w", err)
	}
	if !found {
		workerReplicas = 1
	}

	var podReferences []string
	if pod.Labels[replicaTypeLabel] == strings.ToLower(string(replicaTypeWorker)) {
		podReferences = append(podReferences, pod.Name)
	}

	segmentSize, found, err := getSegmentSize(pod, replicaSpecs)
	if err != nil {
		return nil, err
	}
	if !found {
		return []*podgroup.SubGroupMetadata{{
			Name:           strings.ToLower(replicaTypeWorker),
			MinAvailable:   workerMinAvailable,
			PodsReferences: podReferences,
		}}, nil
	}

	workers := int(workerReplicas)
	numSegments := workers / segmentSize
	partialSegmentSize := workers % segmentSize
	if partialSegmentSize != 0 {
		numSegments++
	}
	segmentIndex, err := getPodSegmentIndex(pod, segmentSize)
	if err != nil {
		return nil, err
	}

	topologyConstraints := getSegmentTopologyConstraints(pod, replicaSpecs, topOwner)

	subGroups := []*podgroup.SubGroupMetadata{{
		Name:         strings.ToLower(replicaTypeWorker),
		MinAvailable: workerMinAvailable,
	}}
	for i := range numSegments {
		subGroup := &podgroup.SubGroupMetadata{
			Name:                fmt.Sprintf("worker-%d", i),
			TopologyConstraints: topologyConstraints,
			Parent:              ptr.To(strings.ToLower(replicaTypeWorker)),
			MinAvailable:        int32(segmentSize),
		}
		if i == segmentIndex {
			subGroup.PodsReferences = podReferences
		}
		if partialSegmentSize != 0 && i == numSegments-1 {
			subGroup.MinAvailable = int32(partialSegmentSize)
		}
		subGroups = append(subGroups, subGroup)
	}

	return subGroups, nil
}

// Returns the segment index for the pod. Returns -1 if the pod is not a worker pod.
// Returns an error if the replica index label is not found or is invalid for a worker pod.
func getPodSegmentIndex(pod *v1.Pod, segmentSize int) (int, error) {
	if pod.Labels[replicaTypeLabel] != strings.ToLower(replicaTypeWorker) {
		return -1, nil
	}

	indexLabel, found := pod.Labels[pytorchv1.ReplicaIndexLabel]
	if !found {
		return -1, fmt.Errorf("replica index label not found on pod %s/%s", pod.Namespace, pod.Name)
	}
	index, err := strconv.Atoi(indexLabel)
	if err != nil {
		return -1, fmt.Errorf("invalid replica index %s, err: %w", indexLabel, err)
	}
	return index / segmentSize, nil
}

func getSegmentSize(pod *v1.Pod, replicaSpecs map[string]interface{}) (int, bool, error) {
	if sizeStr, found := pod.Annotations[constants.SegmentSizeKey]; found && sizeStr != "" {
		segmentSize, err := strconv.Atoi(sizeStr)
		if err != nil {
			return 0, false, fmt.Errorf("invalid segment size %s on pod %s/%s, err: %w", sizeStr, pod.Namespace, pod.Name, err)
		}
		return segmentSize, true, nil
	}

	sizeStr, found, err := unstructured.NestedString(
		replicaSpecs, replicaTypeWorker, "template", "metadata", "annotations", constants.SegmentSizeKey,
	)
	if err != nil {
		return 0, false, fmt.Errorf("failed to get segment size from worker podTemplate: %w", err)
	}
	if !found || sizeStr == "" {
		return 0, false, nil
	}

	segmentSize, err := strconv.Atoi(sizeStr)
	if err != nil {
		return 0, false, fmt.Errorf("invalid segment size %s on worker podTemplate, err: %w", sizeStr, err)
	}
	if segmentSize <= 0 {
		return 0, false, fmt.Errorf("segment size %s on worker podTemplate needs to be a positive integer", sizeStr)
	}
	return segmentSize, true, nil
}

func getTopology(pod *v1.Pod, replicaSpecs map[string]interface{}, topOwner *unstructured.Unstructured) string {
	if topology, found := pod.Annotations[constants.TopologyKey]; found && topology != "" {
		return topology
	}
	topology, found, _ := unstructured.NestedString(
		replicaSpecs, replicaTypeWorker, "template", "metadata", "annotations", constants.TopologyKey,
	)
	if found && topology != "" {
		return topology
	}
	if topOwner != nil {
		if topology, found := topOwner.GetAnnotations()[constants.TopologyKey]; found {
			return topology
		}
	}
	return ""
}

func getWorkerAnnotationValue(pod *v1.Pod, replicaSpecs map[string]interface{}, key string) string {
	if value, found := pod.Annotations[key]; found && value != "" {
		return value
	}
	value, found, _ := unstructured.NestedString(
		replicaSpecs, replicaTypeWorker, "template", "metadata", "annotations", key,
	)
	if found {
		return value
	}
	return ""
}

func getSegmentTopologyConstraints(
	pod *v1.Pod, replicaSpecs map[string]interface{}, topOwner *unstructured.Unstructured,
) *podgroup.TopologyConstraintMetadata {
	topology := getTopology(pod, replicaSpecs, topOwner)
	if topology == "" {
		return nil
	}

	required := getWorkerAnnotationValue(pod, replicaSpecs, constants.SegmentTopologyRequiredPlacementKey)
	preferred := getWorkerAnnotationValue(pod, replicaSpecs, constants.SegmentTopologyPreferredPlacementKey)

	if required == "" && preferred == "" {
		return nil
	}

	return &podgroup.TopologyConstraintMetadata{
		Topology:               topology,
		RequiredTopologyLevel:  required,
		PreferredTopologyLevel: preferred,
	}
}

func getMinReplicas(topOwner *unstructured.Unstructured) (int64, error) {
	minReplicas, found, err := unstructured.NestedInt64(topOwner.Object, "spec", "elasticPolicy", "minReplicas")
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("minReplicas not found in PyTorchJob %s/%s", topOwner.GetNamespace(), topOwner.GetName())
	}
	return minReplicas, nil
}

func getMinAvailable(topOwner *unstructured.Unstructured) (int64, error) {
	minReplicas, found, err := unstructured.NestedInt64(topOwner.Object, "spec", "runPolicy", "schedulingPolicy", "minAvailable")
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("minAvailable not found in PyTorchJob %s/%s", topOwner.GetNamespace(), topOwner.GetName())
	}
	return minReplicas, nil
}
