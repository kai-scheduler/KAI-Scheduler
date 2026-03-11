/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package capacity

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"golang.org/x/exp/maps"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func SkipIfInsufficientClusterResources(clientset kubernetes.Interface, resourceRequest *ResourceList) {
	hasResources, clusterMetrics, err := hasSufficientClusterResources(clientset, resourceRequest)
	if err != nil {
		gomega.Expect(err).NotTo(gomega.HaveOccurred(),
			"Failed to validate sufficient resources with error")
	}
	if !hasResources {
		ginkgo.Skip(
			fmt.Sprintf(
				"The current cluster doesn't have enough resources to run the test. "+
					"Requested resources: %s. Cluster resources: %s",
				resourceRequest, clusterMetrics,
			),
		)
	}
}

func SkipIfInsufficientClusterTopologyResources(client kubernetes.Interface, nodeResourceRequests []ResourceList) {
	hasResources, resources, err := hasSufficientClusterTopologyResources(client, nodeResourceRequests)
	if err != nil {
		gomega.Expect(err).NotTo(gomega.HaveOccurred(),
			"Failed to validate sufficient resources with error")
	}
	if !hasResources {
		ginkgo.Skip(
			fmt.Sprintf(
				"The current cluster doesn't have enough resources to run the test, taking into account the required topology. "+
					"Requested resources: %v, Cluster resources: %v", nodeResourceRequests, resources,
			),
		)
	}
}

// SkipIfNonHomogeneousGpuCounts skips the test if the cluster has nodes with different GPU counts, not considering cpu-only nodes.
func SkipIfNonHomogeneousGpuCounts(clientset kubernetes.Interface) int {
	resources, err := GetNodesAllocatableResources(clientset)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to validate homogeneous gpu resources")

	gpuCounts := map[int]bool{}
	for _, resource := range resources {
		gpuCounts[int(resource.Gpu.Value())] = true
	}

	delete(gpuCounts, 0)
	if len(gpuCounts) > 1 {
		ginkgo.Skip(
			fmt.Sprintf(
				"The current cluster has nodes with different GPU counts: %v", gpuCounts,
			),
		)
	}

	return maps.Keys(gpuCounts)[0]
}

func SkipIfCSIDriverIsMissing(ctx context.Context, client kubernetes.Interface, name string) {
	_, err := client.StorageV1().CSIDrivers().Get(ctx, name, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		ginkgo.Skip(
			fmt.Sprintf(
				"The current cluster doesn't have %v CSIDriver", name,
			),
		)
	}
}

func SkipIfCSICapacitiesAreMissing(ctx context.Context, client kubernetes.Interface) {
	capacities, err := client.StorageV1().CSIStorageCapacities("openebs").List(ctx, metav1.ListOptions{})
	if err != nil || len(capacities.Items) == 0 {
		ginkgo.Skip(
			fmt.Sprintf("Found %d CSIStorageCapacities, err: %s", len(capacities.Items), err),
		)
	}
}

func hasSufficientClusterResources(client kubernetes.Interface, resourceRequest *ResourceList) (
	bool, *ResourceList, error) {
	clusterMetrics, err := getClusterResources(client)
	if err != nil {
		return false, nil, err
	}

	if resourceRequest.LessOrEqual(clusterMetrics) {
		return true, clusterMetrics, nil
	}
	return false, clusterMetrics, nil
}

func hasSufficientClusterTopologyResources(client kubernetes.Interface, nodeResourceRequests []ResourceList,
) (bool, map[string]*ResourceList, error) {
	nodesResourcesMap, err := GetNodesIdleResources(client)
	if err != nil {
		return false, nil, err
	}
	var nodeResources []ResourceList
	for _, nodeResource := range nodesResourcesMap {
		nodeResources = append(nodeResources, *nodeResource)
	}
	return theseNodesSufficientForTheseRequirementsRec(nodeResourceRequests, nodeResources), nodesResourcesMap, nil
}

func theseNodesSufficientForTheseRequirementsRec(reqs, nodes []ResourceList) bool {
	if len(reqs) == 0 {
		return true
	}
	if len(nodes) == 0 {
		return false
	}

	nodeReq := reqs[0]
	newReqs := removeItemFromList(reqs, 0)
	for index, node := range nodes {
		if nodeReq.LessOrEqual(&node) {
			newNodes := removeItemFromList(nodes, index)
			if theseNodesSufficientForTheseRequirementsRec(newReqs, newNodes) {
				return true
			}
		}
	}
	return false
}

func removeItemFromList(list []ResourceList, index int) []ResourceList {
	return append(list[:index], list[index+1:]...)
}
