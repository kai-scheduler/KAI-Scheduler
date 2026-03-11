// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package ray

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	schedulingv2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

var logger = log.FromContext(context.Background())

const (
	rayClusterKind = "RayCluster"

	rayPriorityClassName = "ray.io/priority-class-name"
)

type RayGrouper struct {
	client client.Client
	*defaultgrouper.DefaultGrouper
}

func NewRayGrouper(client client.Client, defaultGrouper *defaultgrouper.DefaultGrouper) *RayGrouper {
	return &RayGrouper{
		client:         client,
		DefaultGrouper: defaultGrouper,
	}
}

// shouldUseSubGroups checks if the new subgrouping logic should be used.
// Returns false only when an existing PodGroup has no SubGroups (legacy workload).
// This ensures backwards compatibility for workloads created before subgrouping was introduced.
// This logic will be removed in v0.14
func (rg *RayGrouper) shouldUseSubGroups(namespace, name string) bool {
	existingPG := &schedulingv2alpha2.PodGroup{}
	err := rg.client.Get(context.Background(), types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, existingPG)

	if err != nil {
		if !errors.IsNotFound(err) {
			logger.V(1).Info("Failed to get existing PodGroup, using new subgrouping logic",
				"namespace", namespace, "name", name, "error", err)
		}
		return true
	}

	return len(existingPG.Spec.SubGroups) > 0
}

// +kubebuilder:rbac:groups=ray.io,resources=rayclusters;rayjobs;rayservices,verbs=get;list;watch
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/finalizers;rayjobs/finalizers;rayservices/finalizers,verbs=patch;update;create

func (rg *RayGrouper) getPodGroupMetadataWithClusterNamePath(
	topOwner *unstructured.Unstructured, pod *v1.Pod, clusterNamePaths [][]string,
) (
	*podgroup.Metadata, error,
) {
	var rayClusterObj *unstructured.Unstructured
	var err error
	rayClusterObj, err = rg.extractRayClusterObject(topOwner, clusterNamePaths)
	if err != nil {
		return nil, err
	}

	podGroupMetadata, err := rg.getPodGroupMetadataInternal(topOwner, rayClusterObj, pod)
	if err != nil {
		return nil, err
	}

	// Ray has it's own way to specify the workload priority class
	if rayPriorityClassName, labelFound := topOwner.GetLabels()[rayPriorityClassName]; labelFound {
		podGroupMetadata.PriorityClassName = rayPriorityClassName
	}

	// Only assign pod to subgroup if we're using subgroups
	if len(podGroupMetadata.SubGroups) > 0 {
		if err = assignRayPodToSubGroup(pod, podGroupMetadata); err != nil {
			logger.V(1).Info("Failed to assign ray pod to subgroup", "pod", pod.Name, "namespace", pod.Namespace, "error", err)
		}
	}

	return podGroupMetadata, nil
}

func assignRayPodToSubGroup(pod *v1.Pod, pgMetadata *podgroup.Metadata) error {
	group, found := pod.Labels[utils.RayNodeGroupLabelKey]
	if !found {
		return fmt.Errorf("ray node group label (%s) not found on pod %s/%s", utils.RayNodeGroupLabelKey, pod.Namespace, pod.Name)
	}

	for _, subGroup := range pgMetadata.SubGroups {
		if subGroup.Name == group {
			subGroup.PodsReferences = append(subGroup.PodsReferences, pod.Name)
			return nil
		}
	}

	return fmt.Errorf("subgroup %s not found in pod group metadata", group)
}

func (rg *RayGrouper) getPodGroupMetadataInternal(
	topOwner *unstructured.Unstructured, rayClusterObj *unstructured.Unstructured, pod *v1.Pod) (
	*podgroup.Metadata, error,
) {
	podGroupMetadata, err := rg.DefaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}

	minReplicas, subGroups, err := calcJobNumOfPodsAndSubGroups(rayClusterObj)
	if err != nil {
		return nil, err
	}

	podGroupMetadata.MinAvailable = minReplicas
	if rg.shouldUseSubGroups(podGroupMetadata.Namespace, podGroupMetadata.Name) {
		podGroupMetadata.SubGroups = subGroups
	}

	return podGroupMetadata, nil
}

func (rg *RayGrouper) extractRayClusterObject(
	topOwner *unstructured.Unstructured, rayClusterNamePaths [][]string) (
	rayClusterObj *unstructured.Unstructured, err error,
) {
	if len(rayClusterNamePaths) == 0 {
		// If no rayClusterNamePaths are provided, use the topOwner as the rayClusterObj
		return topOwner, nil
	}

	var found bool = false
	var rayClusterName string
	for pathIndex := 0; (pathIndex < len(rayClusterNamePaths)) && !found; pathIndex++ {
		rayClusterName, found, err = unstructured.NestedString(topOwner.Object, rayClusterNamePaths[pathIndex]...)
		if err != nil {
			return nil, err
		}
	}
	if !found || rayClusterName == "" {
		return nil, fmt.Errorf("failed to extract rayClusterName for %s/%s of kind %s."+
			" Please make sure that the ray-operator is up and a rayCluster child-object has been created",
			topOwner.GetNamespace(), topOwner.GetName(), topOwner.GetKind())
	}

	rayClusterObj = &unstructured.Unstructured{}
	rayClusterObj.SetAPIVersion(topOwner.GetAPIVersion())
	rayClusterObj.SetKind(rayClusterKind)
	key := types.NamespacedName{
		Namespace: topOwner.GetNamespace(),
		Name:      rayClusterName,
	}
	err = rg.client.Get(context.Background(), key, rayClusterObj)
	if err != nil {
		return nil, err
	}

	return rayClusterObj, nil
}

// These calculations are based on the way KubeRay creates volcano pod-groups
// https://github.com/ray-project/kuberay/blob/dbcc686eabefecc3b939cd5c6e7a051f2473ad34/ray-operator/controllers/ray/batchscheduler/volcano/volcano_scheduler.go#L106
// https://github.com/ray-project/kuberay/blob/dbcc686eabefecc3b939cd5c6e7a051f2473ad34/ray-operator/controllers/ray/batchscheduler/volcano/volcano_scheduler.go#L51
func calcJobNumOfPodsAndSubGroups(topOwner *unstructured.Unstructured) (int32, []*podgroup.SubGroupMetadata, error) {
	minReplicas := int32(1)

	subGroups := []*podgroup.SubGroupMetadata{
		{
			Name:         utils.RayNodeHeadGroupLabelValue,
			MinAvailable: 1,
		},
	}

	workerGroupSpecs, workerSpecFound, err := unstructured.NestedSlice(topOwner.Object,
		"spec", "workerGroupSpecs")
	if err != nil {
		return 0, nil, err
	}
	if !workerSpecFound || len(workerGroupSpecs) == 0 {
		return minReplicas, subGroups, nil
	}

	for groupIndex, groupSpec := range workerGroupSpecs {
		groupMinReplicas, groupDesiredReplicas, err := getReplicaCountersForWorkerGroup(groupSpec, groupIndex)
		if err != nil {
			return 0, nil, err
		}
		if groupMinReplicas == 0 && groupDesiredReplicas == 0 {
			continue
		}

		numOfHosts, err := getGroupNumOfHosts(groupSpec)
		if err != nil {
			return 0, nil, err
		}

		workerGroupMinReplicas := int32(groupDesiredReplicas * numOfHosts)
		if groupMinReplicas > 0 {
			// if minReplicas is set, use it to calculate the min number for workload scheduling
			workerGroupMinReplicas = int32(groupMinReplicas * numOfHosts)
		}
		minReplicas += workerGroupMinReplicas

		// Get worker group name or generate one
		workerGroupName := getWorkerGroupName(groupSpec, groupIndex)
		subGroups = append(subGroups, &podgroup.SubGroupMetadata{
			Name:         workerGroupName,
			MinAvailable: workerGroupMinReplicas,
		})
	}

	return minReplicas, subGroups, nil
}

func getWorkerGroupName(groupSpec interface{}, groupIndex int) string {
	groupName, found, err := unstructured.NestedString(groupSpec.(map[string]interface{}), "groupName")
	if err != nil || !found || groupName == "" {
		return fmt.Sprintf("worker-group-%d", groupIndex)
	}
	return groupName
}

func getGroupNumOfHosts(groupSpec interface{}) (int64, error) {
	numOfHosts, found, err := unstructured.NestedInt64(groupSpec.(map[string]interface{}), "numOfHosts")
	if err != nil {
		return 0, err
	}
	if !found {
		numOfHosts = 1
	}
	return numOfHosts, nil
}

func getReplicaCountersForWorkerGroup(groupSpec interface{}, groupIndex int) (
	minReplicas int64, desiredReplicas int64, err error) {
	var found bool
	workerGroupSpec := groupSpec.(map[string]interface{})

	suspendedWorkerGroup, found, err := unstructured.NestedBool(workerGroupSpec, "suspended")
	if err != nil {
		return 0, 0, err
	}
	if found && suspendedWorkerGroup {
		return 0, 0, nil
	}

	desiredReplicas, found, err = unstructured.NestedInt64(workerGroupSpec, "replicas")
	if err != nil {
		return 0, 0, err
	}
	if !found {
		desiredReplicas = 0
	}

	minReplicas, found, err = unstructured.NestedInt64(workerGroupSpec, "minReplicas")
	if err != nil {
		return 0, 0, err
	}
	if !found {
		minReplicas = 0
	}
	if minReplicas > desiredReplicas {
		return 0, 0, fmt.Errorf(
			"ray-cluster replicas for a workerGroupsSpec can't be less than minReplicas. "+
				"Given %v replicas and %v minReplicas. Please fix the replicas field for group number %v",
			desiredReplicas, minReplicas, groupIndex)
	}
	return minReplicas, desiredReplicas, nil
}
