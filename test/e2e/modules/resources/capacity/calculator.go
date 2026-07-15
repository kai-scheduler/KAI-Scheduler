/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package capacity

import (
	"context"
	"fmt"
	"strconv"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"
)

func getClusterResources(clientset kubernetes.Interface) (*ResourceList, error) {
	perNodeResources, err := GetNodesIdleResources(clientset)
	if err != nil {
		return nil, err
	}

	rl := initEmptyResourcesList()
	for _, nodeResources := range perNodeResources {
		rl.Add(nodeResources)
	}
	return rl, nil
}

func GetNodesIdleResources(clientset kubernetes.Interface) (map[string]*ResourceList, error) {
	podList, nodeList, err := getPodsAndNodes(clientset)
	if err != nil {
		return nil, err
	}

	return calcNodesResources(podList, nodeList), nil
}

func GetNodesAllocatableResources(clientset kubernetes.Interface) (map[string]*ResourceList, error) {
	nodeList, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing Nodes: %w", err)
	}

	perNodeResources := map[string]*ResourceList{}

	for _, node := range nodeList.Items {
		perNodeResources[node.Name] = &ResourceList{
			Cpu:            node.Status.Allocatable.Cpu().DeepCopy(),
			Memory:         node.Status.Allocatable.Memory().DeepCopy(),
			Gpu:            node.Status.Allocatable[constants.NvidiaGpuResource].DeepCopy(),
			GpuMemory:      calcNodeGpuMemory(node),
			PodCount:       int(node.Status.Allocatable.Pods().Value()),
			OtherResources: getOtherResourcesFromResourceList(node.Status.Allocatable),
		}
	}

	return perNodeResources, nil
}

func GetClusterAllocatableResources(clientset kubernetes.Interface) (*ResourceList, error) {
	perNodeResources, err := GetNodesAllocatableResources(clientset)
	if err != nil {
		return nil, err
	}

	rl := initEmptyResourcesList()
	for _, nodeResources := range perNodeResources {
		rl.Add(nodeResources)
	}
	return rl, nil
}

func getPodsAndNodes(clientset kubernetes.Interface) (
	*corev1.PodList, *corev1.NodeList, error) {
	nodeList, err := clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("error listing Nodes: %w", err)
	}

	podList, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("error listing Pods: %w", err)
	}

	nodes := map[string]bool{}
	for _, node := range nodeList.Items {
		nodes[node.GetName()] = true
	}

	var newPodItems []corev1.Pod
	for _, pod := range podList.Items {
		if !nodes[pod.Spec.NodeName] {
			continue
		}

		newPodItems = append(newPodItems, pod)
	}
	podList.Items = newPodItems

	return podList, nodeList, nil
}

func calcNodesResources(podList *corev1.PodList, nodeList *corev1.NodeList) map[string]*ResourceList {
	perNodeResources := map[string]*ResourceList{}

	for _, node := range nodeList.Items {
		perNodeResources[node.Name] = &ResourceList{
			Cpu:            node.Status.Allocatable.Cpu().DeepCopy(),
			Memory:         node.Status.Allocatable.Memory().DeepCopy(),
			Gpu:            node.Status.Allocatable[constants.NvidiaGpuResource].DeepCopy(),
			GpuMemory:      calcNodeGpuMemory(node),
			PodCount:       int(node.Status.Allocatable.Pods().Value()),
			OtherResources: getOtherResourcesFromResourceList(node.Status.Allocatable),
		}
	}

	for _, pod := range podList.Items {
		if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed &&
			pod.Spec.NodeName != "" {
			subtractPodResources(perNodeResources[pod.Spec.NodeName], &pod)
		}
	}

	return perNodeResources
}

func calcNodeGpuMemory(node corev1.Node) resource.Quantity {
	var nodeGpusMemory resource.Quantity
	nodeGpus := node.Status.Allocatable[constants.NvidiaGpuResource]
	if nodeGpusMemoryStr := node.Labels[constant.NvidiaGPUMemoryLabelName]; nodeGpusMemoryStr != "" {
		singleGpuMemory, _ := strconv.Atoi(nodeGpusMemoryStr)
		totalNodeGpuMemory := strconv.Itoa(singleGpuMemory * int(nodeGpus.Value()))
		nodeGpusMemory = resource.MustParse(totalNodeGpuMemory + "Mi")
	} else {
		nodeGpusMemory = resource.MustParse("0")
	}
	return nodeGpusMemory
}

func subtractPodResources(nodeResourceList *ResourceList, pod *corev1.Pod) {
	req, _ := resourcehelper.PodRequestsAndLimits(pod)

	podResourceList := ResourcesRequestToList(req, pod.Annotations)
	nodeResourceList.Sub(podResourceList)
}

func ResourcesRequestToList(req corev1.ResourceList, podAnnotations map[string]string) *ResourceList {
	podResourceList := &ResourceList{
		Cpu:            *req.Cpu(),
		Memory:         *req.Memory(),
		Gpu:            calcPodGpus(podAnnotations, req),
		GpuMemory:      calcPodGpuMemory(podAnnotations),
		PodCount:       1,
		OtherResources: getOtherResourcesFromResourceList(req),
	}
	return podResourceList
}

func calcPodGpus(podAnnotations map[string]string, req corev1.ResourceList) resource.Quantity {
	gpuReq := req[constants.NvidiaGpuResource].DeepCopy()

	if fractionalGpuStr := podAnnotations[constants.GpuFraction]; fractionalGpuStr != "" {
		fractionalGpu := resource.MustParse(fractionalGpuStr)

		gpuReq.Add(fractionalGpu)
	}
	return gpuReq
}

func calcPodGpuMemory(podAnnotations map[string]string) resource.Quantity {
	if gpuMemoryStr := podAnnotations["gpu-memory"]; gpuMemoryStr != "" {
		return resource.MustParse(gpuMemoryStr)
	} else {
		return resource.MustParse("0")
	}
}

func getOtherResourcesFromResourceList(list corev1.ResourceList) map[corev1.ResourceName]resource.Quantity {
	otherResources := map[corev1.ResourceName]resource.Quantity{}
	for key, value := range list {
		if key == corev1.ResourceCPU ||
			key == corev1.ResourceMemory ||
			key == corev1.ResourcePods ||
			key == constants.NvidiaGpuResource {
			continue
		}
		otherResources[key] = value
	}
	return otherResources
}
