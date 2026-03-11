// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package grove

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	labelKeyPodGangName = "grove.io/podgang"
)

type GroveGrouper struct {
	client client.Client
	*defaultgrouper.DefaultGrouper
}

func NewGroveGrouper(client client.Client, defaultGrouper *defaultgrouper.DefaultGrouper) *GroveGrouper {
	return &GroveGrouper{
		client:         client,
		DefaultGrouper: defaultGrouper,
	}
}

func (gg *GroveGrouper) Name() string {
	return "Grove Grouper"
}

// PodCliqueSet is the top-level CR in Grove. PodGangSet is the older name and got renamed to PodCLiqueSet.
// PodGangSet support and rbac will be eventually deprecated.

// +kubebuilder:rbac:groups=grove.io,resources=podgangsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=grove.io,resources=podgangsets/finalizers,verbs=patch;update;create
// +kubebuilder:rbac:groups=grove.io,resources=podcliquesets,verbs=get;list;watch
// +kubebuilder:rbac:groups=grove.io,resources=podcliquesets/finalizers,verbs=patch;update;create
// +kubebuilder:rbac:groups=grove.io,resources=podcliques,verbs=get;list;watch
// +kubebuilder:rbac:groups=grove.io,resources=podcliques/finalizers,verbs=patch;update;create
// +kubebuilder:rbac:groups=grove.io,resources=podcliquescalinggroups,verbs=get;list;watch
// +kubebuilder:rbac:groups=grove.io,resources=podcliquescalinggroups/finalizers,verbs=patch;update;create
// +kubebuilder:rbac:groups=scheduler.grove.io,resources=podgangs,verbs=get;list;watch
// +kubebuilder:rbac:groups=scheduler.grove.io,resources=podgangs/finalizers,verbs=patch;update;create

func (gg *GroveGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	podGangName, ok := pod.Labels[labelKeyPodGangName]
	if !ok {
		return nil, fmt.Errorf("label for podgang name (key: %s) not found in pod %s/%s",
			labelKeyPodGangName, pod.Namespace, pod.Name)
	}

	podGang := &unstructured.Unstructured{}
	podGang.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "scheduler.grove.io",
		Kind:    "PodGang",
		Version: "v1alpha1",
	})

	err := gg.client.Get(context.Background(), client.ObjectKey{
		Namespace: pod.Namespace,
		Name:      podGangName,
	}, podGang)
	if err != nil {
		return nil, fmt.Errorf("failed to get PodGang %s/%s. Err: %w",
			pod.Namespace, podGangName, err)
	}

	metadata, err := gg.DefaultGrouper.GetPodGroupMetadata(podGang, pod)
	if err != nil {
		return nil, fmt.Errorf("failed to get DefaultGrouper metadata for PodGang %s/%s. Err: %w",
			pod.Namespace, podGangName, err)
	}

	priorityClassName, found, err := unstructured.NestedString(podGang.Object, "spec", "priorityClassName")
	if err != nil {
		return nil, fmt.Errorf("failed to get spec.priorityClassName from PodGang %s/%s. Err: %w",
			pod.Namespace, podGangName, err)
	}
	if found {
		metadata.PriorityClassName = priorityClassName
	}

	// Grove can be invoked through Dynamo. However, metadata does not propagate from Dynamo to Grove. We use metadata propagation from PodCLiqueSet to PodGang for
	// Podgroup creation.
	// Dynamo Grove Ownership tree: DynamoGraphDeployment(DGD) -> PodCLiqueSet -> PodClique && PodGang. PodClique -> Pod
	if topOwner != nil {
		topOwnerLabels := topOwner.GetLabels()
		for k, v := range topOwnerLabels {
			if _, exists := metadata.Labels[k]; !exists {
				metadata.Labels[k] = v
			}
		}
		topOwnerAnnotations := topOwner.GetAnnotations()
		for k, v := range topOwnerAnnotations {
			if _, exists := metadata.Annotations[k]; !exists {
				metadata.Annotations[k] = v
			}
		}
	}

	metadata, err = gg.parseMetadataFromTopOwner(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata from top owner %s/%s. Err: %w",
			pod.Namespace, podGangName, err)
	}
	var minAvailable int32
	pgSlice, found, err := unstructured.NestedSlice(podGang.Object, "spec", "podgroups")
	if err != nil {
		return nil, fmt.Errorf("failed to get spec.podgroups from PodGang %s/%s. Err: %w",
			pod.Namespace, podGangName, err)
	}
	for pgIndex, v := range pgSlice {
		pgr, ok := v.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid structure of spec.podgroup[%v] in PodGang %s/%s",
				pgIndex, pod.Namespace, podGangName)
		}
		subGroup, err := parseGroveSubGroup(pgr, pgIndex, pod.Namespace, podGangName)
		if err != nil {
			return nil, fmt.Errorf("failed to parse spec.podgroups[%d] from PodGang %s/%s. Err: %w",
				pgIndex, pod.Namespace, podGangName, err)
		}
		metadata.SubGroups = append(metadata.SubGroups, subGroup)

		minAvailable += subGroup.MinAvailable
	}
	metadata.MinAvailable = minAvailable

	return metadata, nil
}

func parseGroveSubGroup(
	pg map[string]interface{}, pgIndex int, namespace, podGangName string,
) (*podgroup.SubGroupMetadata, error) {
	// Name
	name, found, err := unstructured.NestedString(pg, "name")
	if err != nil {
		return nil, fmt.Errorf("failed to parse 'name' field. Err: %v", err)
	}
	if !found {
		return nil, fmt.Errorf("missing required 'name' field")
	}

	// MinReplicas
	minAvailable, found, err := unstructured.NestedInt64(pg, "minReplicas")
	if err != nil {
		return nil, fmt.Errorf("failed to parse 'minReplicas' field. Err: %v", err)
	}
	if !found {
		return nil, fmt.Errorf("missing required 'minReplicas' field")
	}
	if minAvailable <= 0 {
		return nil, fmt.Errorf("invalid 'minReplicas' field. Must be greater than 0")
	}

	// PodReferences
	podReferences, found, err := unstructured.NestedSlice(pg, "podReferences")
	if err != nil {
		return nil, fmt.Errorf("failed to parse 'podReferences' field. Err: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("missing required 'podReferences' field")
	}
	var pods []string
	for podIndex, podRef := range podReferences {
		reference, ok := podRef.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid spec.podgroup[%d].podReferences[%d] in PodGang %s/%s",
				pgIndex, podIndex, namespace, podGangName)
		}
		namespacedName, err := parsePodReference(reference)
		if err != nil {
			return nil, fmt.Errorf("failed to parse spec.podgroups[%d].podreferences[%d] from PodGang %s/%s. Err: %w",
				pgIndex, podIndex, namespace, podGangName, err)
		}
		pods = append(pods, namespacedName.Name)
	}

	return &podgroup.SubGroupMetadata{
		Name:           name,
		MinAvailable:   int32(minAvailable),
		PodsReferences: pods,
	}, nil
}

func parsePodReference(podRef map[string]interface{}) (*types.NamespacedName, error) {
	podNamespace, found, err := unstructured.NestedString(podRef, "namespace")
	if err != nil {
		return nil, fmt.Errorf("failed to parse 'namespace' field. Err: %v", err)
	}
	if !found {
		return nil, fmt.Errorf("missing required 'namespace' field")
	}

	podName, found, err := unstructured.NestedString(podRef, "name")
	if err != nil {
		return nil, fmt.Errorf("failed to parse 'name' field. Err: %v", err)
	}
	if !found {
		return nil, fmt.Errorf("missing required 'name' field")
	}

	return &types.NamespacedName{Namespace: podNamespace, Name: podName}, nil
}

func (gg *GroveGrouper) parseMetadataFromTopOwner(metadata *podgroup.Metadata) (*podgroup.Metadata, error) {
	if priorityClassName, ok := metadata.Labels[constants.PriorityLabelKey]; ok {
		metadata.PriorityClassName = priorityClassName
	}
	if preemptibility, ok := metadata.Labels[constants.PreemptibilityLabelKey]; ok {
		preemptibility, err := v2alpha2.ParsePreemptibility(preemptibility)
		if err != nil {
			return nil, fmt.Errorf("failed to parse preemptibility from top owner %s/%s. Err: %w", metadata.Namespace, metadata.Name, err)
		}
		metadata.Preemptibility = preemptibility
	}

	// get Topology data from annotations similar to applyTopologyConstraints
	topologyConstraint := v2alpha2.TopologyConstraint{
		PreferredTopologyLevel: metadata.Annotations[constants.TopologyPreferredPlacementKey],
		RequiredTopologyLevel:  metadata.Annotations[constants.TopologyRequiredPlacementKey],
		Topology:               metadata.Annotations[constants.TopologyKey],
	}
	if metadata.PreferredTopologyLevel == "" {
		metadata.PreferredTopologyLevel = topologyConstraint.PreferredTopologyLevel
	}
	if metadata.RequiredTopologyLevel == "" {
		metadata.RequiredTopologyLevel = topologyConstraint.RequiredTopologyLevel
	}
	if metadata.Topology == "" {
		metadata.Topology = topologyConstraint.Topology
	}
	return metadata, nil
}
