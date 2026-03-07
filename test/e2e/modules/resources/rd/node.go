/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package rd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/resources/capacity"
)

const draDriverName = "gpu.nvidia.com"

func LabelNode(ctx context.Context, client kubernetes.Interface, node *corev1.Node, labelName, labelValue string,
) error {
	patchData, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]string{
				labelName: labelValue,
			},
		},
	})
	if err != nil {
		return err
	}
	_, err = client.CoreV1().Nodes().Patch(ctx, node.Name, types.MergePatchType, patchData, metav1.PatchOptions{})
	return err
}

func UnLabelNode(ctx context.Context, client kubernetes.Interface, node *corev1.Node, labelName string) error {
	node, err := client.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	labels := node.Labels
	if _, found := labels[labelName]; !found {
		return nil
	}

	labelName = escapeLabelName(labelName)
	patchData, err := json.Marshal([]map[string]string{
		{"op": "remove", "path": fmt.Sprintf("/metadata/labels/%s", labelName)},
	})
	if err != nil {
		return err
	}

	_, err = client.CoreV1().Nodes().Patch(ctx, node.Name, types.JSONPatchType, patchData, metav1.PatchOptions{})
	return err
}

// escapeLabelName escapes special characters in labels, see https://jsonpatch.com/#json-pointer
func escapeLabelName(labelName string) string {
	labelName = strings.ReplaceAll(labelName, "~", "~0")
	labelName = strings.ReplaceAll(labelName, "/", "~1")

	return labelName
}

func FindNodeWithNoGPU(ctx context.Context, client runtimeClient.Client, kubeClient kubernetes.Interface) *corev1.Node {
	allNodes := corev1.NodeList{}
	Expect(client.List(ctx, &allNodes)).To(Succeed())

	draDevicesByNode := capacity.ListDevicesByNode(kubeClient, draDriverName)

	for i, node := range allNodes.Items {
		if len(node.Spec.Taints) > 0 {
			continue
		}
		gpuResource := node.Status.Capacity[constants.NvidiaGpuResource]
		hasDevicePluginGPUs := gpuResource.CmpInt64(0) > 0
		hasDRAGPUs := draDevicesByNode[node.Name] > 0
		if !hasDevicePluginGPUs && !hasDRAGPUs {
			return &allNodes.Items[i]
		}
	}
	return nil
}

func FindNodeWithEnoughGPUs(ctx context.Context, client runtimeClient.Client, numFreeGPUs int64) *corev1.Node {
	allNodes := corev1.NodeList{}
	Expect(client.List(ctx, &allNodes)).To(Succeed())
	for _, node := range allNodes.Items {
		gpusQty, found := node.Status.Allocatable[constants.NvidiaGpuResource]
		if !found {
			continue
		}
		desiredQty := resource.NewQuantity(numFreeGPUs, resource.DecimalSI)
		if gpusQty.Cmp(*desiredQty) >= 0 {
			return &node
		}
	}
	return nil
}

func FindNodesWithEnoughGPUs(ctx context.Context, client runtimeClient.Client, numFreeGPUs int64) []*corev1.Node {
	allNodes := corev1.NodeList{}
	var gpuNodes []*corev1.Node
	Expect(client.List(ctx, &allNodes)).To(Succeed())
	for index, node := range allNodes.Items {
		gpusQty, found := node.Status.Allocatable[constants.NvidiaGpuResource]
		if !found || gpusQty.IsZero() {
			continue
		}
		desiredQty := resource.NewQuantity(numFreeGPUs, resource.DecimalSI)
		if gpusQty.Cmp(*desiredQty) >= 0 {
			gpuNodes = append(gpuNodes, &allNodes.Items[index])
		}
	}
	return gpuNodes
}

func FindNodesWithExactGPUs(ctx context.Context, client runtimeClient.Client, numFreeGPUs int64) []*corev1.Node {
	allNodes := corev1.NodeList{}
	var gpuNodes []*corev1.Node
	Expect(client.List(ctx, &allNodes)).To(Succeed())
	for index, node := range allNodes.Items {
		gpusQty, found := node.Status.Allocatable[constants.NvidiaGpuResource]
		if !found || gpusQty.IsZero() {
			continue
		}
		desiredQty := resource.NewQuantity(numFreeGPUs, resource.DecimalSI)
		if gpusQty.Cmp(*desiredQty) == 0 {
			gpuNodes = append(gpuNodes, &allNodes.Items[index])
		}
	}
	return gpuNodes
}

func GetNodeGpuDeviceMemory(ctx context.Context, client runtimeClient.Client, nodeName string) int64 {
	node := &v1.Node{}
	err := client.Get(ctx, runtimeClient.ObjectKey{Name: nodeName}, node)
	Expect(err).NotTo(HaveOccurred())

	parsed, err := strconv.ParseInt(node.Labels["nvidia.com/gpu.memory"], 10, 64)
	Expect(err).NotTo(HaveOccurred())

	// value is return in MiB
	return parsed * 1024 * 1024
}

func GetNodeGpuDeviceCount(ctx context.Context, client runtimeClient.Client, nodeName string) int64 {
	node := &v1.Node{}
	err := client.Get(ctx, runtimeClient.ObjectKey{Name: nodeName}, node)
	Expect(err).NotTo(HaveOccurred())

	parsed, err := strconv.ParseInt(node.Labels["nvidia.com/gpu.count"], 10, 64)
	Expect(err).NotTo(HaveOccurred())

	return parsed
}
