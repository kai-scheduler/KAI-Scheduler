// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package ray

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	rayClusterKind = "RayCluster"
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

	return rg.getPodGroupMetadataInternal(topOwner, rayClusterObj, pod)
}

func (rg *RayGrouper) getPodGroupMetadataInternal(
	topOwner *unstructured.Unstructured, rayClusterObj *unstructured.Unstructured, pod *v1.Pod) (
	*podgroup.Metadata, error,
) {
	podGroupMetadata, err := rg.DefaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}

	minReplicas, err := calcJobNumOfPods(rayClusterObj)
	if err != nil {
		return nil, err
	}
	podGroupMetadata.MinAvailable = minReplicas

	return podGroupMetadata, nil
}

func (rg *RayGrouper) extractRayClusterObject(
	topOwner *unstructured.Unstructured, rayClusterNamePaths [][]string) (
	rayClusterObj *unstructured.Unstructured, err error,
) {
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
func calcJobNumOfPods(topOwner *unstructured.Unstructured) (int32, error) {
	minAvailableReplicas := int32(0)

	launcherMinReplicas, minReplicasFound, err := unstructured.NestedInt64(topOwner.Object,
		"spec", "headGroupSpec", "minReplicas")
	launcherReplicas, replicasFound, replicasErr := unstructured.NestedInt64(topOwner.Object,
		"spec", "headGroupSpec", "replicas")
	if err != nil || !minReplicasFound || launcherMinReplicas == 0 {
		if replicasErr == nil && replicasFound && launcherReplicas > 0 {
			launcherMinReplicas = launcherReplicas
		} else {
			launcherMinReplicas = 1 // Set default value 1
		}
	}
	minAvailableReplicas += int32(launcherMinReplicas)

	workerGroupSpecs, minReplicasFound, err := unstructured.NestedSlice(topOwner.Object,
		"spec", "workerGroupSpecs")
	if err != nil {
		return 0, err
	}
	if !minReplicasFound || len(workerGroupSpecs) == 0 {
		return minAvailableReplicas, nil
	}

	for groupIndex, groupSpec := range workerGroupSpecs {
		groupMinReplicas, err := getReplicaCountersForWorkerGroup(groupSpec, groupIndex)
		minAvailableReplicas += int32(groupMinReplicas)

		if err != nil {
			return 0, err
		}
	}

	return minAvailableReplicas, nil
}

func getReplicaCountersForWorkerGroup(groupSpec interface{}, groupIndex int) (int64, error) {
	workerGroupSpec := groupSpec.(map[string]interface{})

	workerGroupReplicas, found, err := unstructured.NestedInt64(workerGroupSpec, "replicas")
	if err != nil {
		return 0, err
	}
	if !found || workerGroupReplicas == 0 {
		return 0, nil
	}

	workerGroupMinReplicas, found, err := unstructured.NestedInt64(workerGroupSpec, "minReplicas")
	if err != nil {
		return 0, err
	}
	if !found || workerGroupMinReplicas > workerGroupReplicas {
		return 0, fmt.Errorf(
			"ray-cluster replicas for a workerGroupsSpec can't be less than minReplicas. "+
				"Given %v replicas and %v minReplicas. Please fix the replicas field for group number %v",
			workerGroupReplicas, workerGroupMinReplicas, groupIndex)
	}
	return workerGroupMinReplicas, nil
}
