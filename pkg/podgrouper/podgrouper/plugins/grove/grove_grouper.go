// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package grove

import (
	"context"
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	podGang, err := gg.fetchPodGang(pod.Namespace, podGangName)
	if err != nil {
		return nil, err
	}

	metadata, err := gg.DefaultGrouper.GetPodGroupMetadata(podGang, pod)
	if err != nil {
		return nil, fmt.Errorf("failed to get DefaultGrouper metadata for PodGang %s/%s. Err: %w",
			pod.Namespace, podGangName, err)
	}
	topology := gg.getTopology(podGang)
	err = gg.applyTopologyConstraints(podGang, metadata, topology)
	if err != nil {
		return nil, fmt.Errorf("failed to apply topology constraints from PodGang %s/%s. Err: %w",
			pod.Namespace, podGangName, err)
	}
	priorityClassName, found, err := getPriorityClassName(podGang)
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

	parentSubGroups, subGroupToParentMap, err := parseParentSubGroups(podGang, pod.Namespace, podGangName, topology)
	if err != nil {
		return nil, err
	}

	childSubGroups, minAvailable, err := parseChildSubGroups(podGang, pod.Namespace, podGangName, subGroupToParentMap, topology)
	if err != nil {
		return nil, err
	}

	metadata.SubGroups = append(parentSubGroups, childSubGroups...)
	metadata.MinAvailable = minAvailable
	return metadata, nil
}

func (gg *GroveGrouper) getTopology(podGang *unstructured.Unstructured) string {
	if topology, ok := podGang.GetAnnotations()["grove.io/topology-name"]; ok {
		return topology
	}
	if topology, ok := podGang.GetAnnotations()["kai.scheduler/topology"]; ok {
		return topology
	}
	return ""
}

func (gg *GroveGrouper) applyTopologyConstraints(podGang *unstructured.Unstructured, metadata *podgroup.Metadata, topology string) error {
	topologyConstraint, err := parseTopologyConstraint(podGang.Object, topology, "spec", "topologyConstraint", "packConstraint")
	if err != nil {
		return fmt.Errorf("failed to parse topology from PodGang, Err: %w", err)
	}

	if topologyConstraint != nil {
		metadata.PreferredTopologyLevel = topologyConstraint.PreferredTopologyLevel
		metadata.RequiredTopologyLevel = topologyConstraint.RequiredTopologyLevel
		metadata.Topology = topology
	}
	return nil
}

func parseTopologyConstraint(podGang map[string]interface{}, topology string, topologyFieldPath ...string) (*podgroup.TopologyConstraintMetadata, error) {
	topologyPreferredConstraints, foundPreferred, err := unstructured.NestedString(podGang, append(topologyFieldPath, "preferred")...)
	if err != nil {
		return nil, err
	}
	topologyRequiredConstraints, foundRequired, err := unstructured.NestedString(podGang, append(topologyFieldPath, "required")...)
	if err != nil {
		return nil, err
	}

	if !foundPreferred && !foundRequired {
		return nil, nil
	}

	if topology == "" {
		return nil, fmt.Errorf("topology name cannot be empty when topology constraints are defined")
	}

	topologyConstraint := &podgroup.TopologyConstraintMetadata{}
	if foundPreferred {
		topologyConstraint.PreferredTopologyLevel = topologyPreferredConstraints
	}
	if foundRequired {
		topologyConstraint.RequiredTopologyLevel = topologyRequiredConstraints
	}
	topologyConstraint.Topology = topology
	return topologyConstraint, nil
}

func (gg *GroveGrouper) fetchPodGang(namespace string, podGangName string) (*unstructured.Unstructured, error) {
	podGang := &unstructured.Unstructured{}
	podGang.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "scheduler.grove.io",
		Kind:    "PodGang",
		Version: "v1alpha1",
	})

	err := gg.client.Get(context.Background(), client.ObjectKey{
		Namespace: namespace,
		Name:      podGangName,
	}, podGang)
	if err != nil {
		return nil, fmt.Errorf("failed to get PodGang %s/%s. Err: %w",
			namespace, podGangName, err)
	}

	return podGang, nil
}

func parseGroveSubGroup(
	pg map[string]interface{}, pgIndex int, namespace, podGangName string,
	topology string,
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
		// Validate that pod reference is in the same namespace as PodGang to prevent cross-namespace manipulation
		if namespacedName.Namespace != namespace {
			return nil, fmt.Errorf("cross-namespace pod reference not allowed: pod %s/%s cannot be referenced from PodGang in namespace %s",
				namespacedName.Namespace, namespacedName.Name, namespace)
		}
		pods = append(pods, namespacedName.Name)
	}
	topologyConstraint, err := parseTopologyConstraint(pg, topology, "topologyConstraint", "packConstraint")
	if err != nil {
		return nil, fmt.Errorf("failed to parse topology constraint from PodGang %s/%s. Err: %w", namespace, podGangName, err)
	}
	return &podgroup.SubGroupMetadata{
		Name:                name,
		MinAvailable:        int32(minAvailable),
		PodsReferences:      pods,
		TopologyConstraints: topologyConstraint,
	}, nil
}

func getPriorityClassName(podGang *unstructured.Unstructured) (string, bool, error) {
	priorityClassName, found, err := unstructured.NestedString(podGang.Object, "spec", "priorityClassName")
	if err != nil {
		return "", false, fmt.Errorf("failed to get spec.priorityClassName: %w", err)
	}
	return priorityClassName, found, nil
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
	topologyConstraint := podgroup.TopologyConstraintMetadata{
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

func parseParentSubGroups(
	podGang *unstructured.Unstructured,
	namespace, podGangName string,
	topology string,
) ([]*podgroup.SubGroupMetadata, map[string]string, error) {
	var parentSubGroups []*podgroup.SubGroupMetadata
	subGroupToParentMap := make(map[string]string)

	groupTopologyConfigs, found, err := unstructured.NestedSlice(podGang.Object, "spec", "topologyConstraintGroupConfigs")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse 'topologyConstraintGroupConfigs' field. Err: %w", err)
	}
	if !found {
		return parentSubGroups, subGroupToParentMap, nil
	}

	for configIndex, configInterface := range groupTopologyConfigs {
		config, ok := configInterface.(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("invalid structure of spec.topologyConstraintGroupConfigs[%d] in PodGang %s/%s",
				configIndex, namespace, podGangName)
		}

		parentSubGroup, err := parseGroupTopologyConfig(config, subGroupToParentMap, topology)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse topologyConstraintGroupConfig[%d]: %w", configIndex, err)
		}

		if parentSubGroup == nil {
			continue
		}

		parentSubGroups = append(parentSubGroups, parentSubGroup)
	}

	return parentSubGroups, subGroupToParentMap, nil
}

func parseChildSubGroups(
	podGang *unstructured.Unstructured,
	namespace, podGangName string,
	subGroupToParentMap map[string]string,
	topology string,
) ([]*podgroup.SubGroupMetadata, int32, error) {
	var childSubGroups []*podgroup.SubGroupMetadata
	var minAvailable int32

	pgSlice, found, err := unstructured.NestedSlice(podGang.Object, "spec", "podgroups")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get spec.podgroups from PodGang %s/%s. Err: %w",
			namespace, podGangName, err)
	}
	if !found {
		return childSubGroups, 0, nil
	}

	for pgIndex, v := range pgSlice {
		podGroupSpec, ok := v.(map[string]interface{})
		if !ok {
			return nil, 0, fmt.Errorf("invalid structure of spec.podgroup[%v] in PodGang %s/%s",
				pgIndex, namespace, podGangName)
		}

		subGroup, err := parseGroveSubGroup(podGroupSpec, pgIndex, namespace, podGangName, topology)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to parse spec.podgroups[%d] from PodGang %s/%s. Err: %w",
				pgIndex, namespace, podGangName, err)
		}

		if parentName, hasParent := subGroupToParentMap[subGroup.Name]; hasParent {
			subGroup.Parent = ptr.To(parentName)
		}

		topologyConstraint, err := parseTopologyConstraint(podGroupSpec, topology, "topologyConstraint", "packConstraint")
		if err != nil {
			return nil, 0, fmt.Errorf("failed to parse topology from PodGroup %s: %w", subGroup.Name, err)
		}

		subGroup.TopologyConstraints = topologyConstraint

		childSubGroups = append(childSubGroups, subGroup)
		minAvailable += subGroup.MinAvailable
	}

	return childSubGroups, minAvailable, nil
}

func parseGroupTopologyConfig(config map[string]interface{}, subGroupToParentMap map[string]string, topology string) (*podgroup.SubGroupMetadata, error) {
	name, found, err := unstructured.NestedString(config, "name")
	if err != nil {
		return nil, fmt.Errorf("failed to parse 'name' field. Err: %v", err)
	}
	if !found {
		return nil, fmt.Errorf("missing required 'name' field")
	}

	podGroupNames, found, err := unstructured.NestedStringSlice(config, "podGroupNames")
	if err != nil {
		return nil, fmt.Errorf("failed to parse 'podGroupNames' field. Err: %w", err)
	}

	if !found || len(podGroupNames) == 0 {
		return nil, nil
	}

	for _, pgName := range podGroupNames {
		subGroupToParentMap[pgName] = name
	}

	topologyConstraint, err := parseTopologyConstraint(config, topology, "topologyConstraint", "packConstraint")
	if err != nil {
		return nil, fmt.Errorf("failed to parse topology from topologyConstraintGroupConfig %s. Err: %w", name, err)
	}

	return &podgroup.SubGroupMetadata{
		Name:                name,
		MinAvailable:        0,
		Parent:              nil,
		PodsReferences:      nil,
		TopologyConstraints: topologyConstraint,
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
