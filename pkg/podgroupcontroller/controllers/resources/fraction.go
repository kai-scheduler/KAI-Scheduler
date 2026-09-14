// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

const (
	amdNodeGpuMemoryLabelPrefix       = "beta.amd.com/gpu.vram"
	amdNodeGpuMemoryLabelValuePattern = "(\\d+.*)"
	amdNodeGpuMemoryValueFactor       = 1000000
)

var (
	ErrNotFoundLabel = errors.New("failed to find label")
)

func calculateAllocatedFraction(
	ctx context.Context, pod *v1.Pod, kubeClient client.Client,
) (resource.Quantity, error) {
	req, err := resources.ParsePodGPUFractionRequest(pod)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("failed to parse GPU fraction for pod %s/%s: %s", pod.Namespace, pod.Name, err)
	}
	if req == nil {
		return resource.Quantity{}, fmt.Errorf("cannot calculate fraction because the pod doesn't have a fraction or memory annotation")
	}

	if req.Portion > 0 {
		return resource.MustParse(fmt.Sprintf("%g", req.Portion)), nil
	}
	return getFractionFromMemoryRequest(ctx, req.Memory.Value()/resources.BytesInMiB, pod.Spec.NodeName, kubeClient)
}

func getFractionFromMemoryRequest(
	ctx context.Context, gpuMemory int64, nodeName string, kubeClient client.Client,
) (resource.Quantity, error) {
	nodeGpuMemory, err := getNodeSingleGpuMemory(ctx, nodeName, kubeClient)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("failed extract node gpu memory for node %s : %w",
			nodeName, err)
	}

	gpuReceivedFraction := float64(gpuMemory) / nodeGpuMemory

	gpuFractionStr := strconv.FormatFloat(gpuReceivedFraction, 'f', -1, 64)
	return resource.MustParse(gpuFractionStr), nil
}

func getNodeSingleGpuMemory(ctx context.Context, nodeName string, kubeClient client.Client) (float64, error) {
	node := v1.Node{}
	err := kubeClient.Get(ctx, client.ObjectKey{Name: nodeName}, &node)
	if err != nil {
		return 0, err
	}

	singleNvidiaGpuMemory, nvidiaMemoryError := getNodeSingleNvidiaGpuMemory(&node)
	if nvidiaMemoryError != nil && !errors.Is(nvidiaMemoryError, ErrNotFoundLabel) {
		return 0, fmt.Errorf("failed to extract memory from nvidia node gpu memory label. error: %w",
			nvidiaMemoryError)
	}

	singleAmdGpuMemory, amdMemoryError := getNodeSingleAmdGpuMemory(&node)
	if amdMemoryError != nil && !errors.Is(amdMemoryError, ErrNotFoundLabel) {
		return 0, fmt.Errorf("failed to extract memory from amd node gpu memory label. error: %w",
			amdMemoryError)
	}

	if errors.Is(nvidiaMemoryError, ErrNotFoundLabel) && errors.Is(amdMemoryError, ErrNotFoundLabel) {
		return 0, fmt.Errorf(
			"failed to extract memory from gpu node nither nvidia not amd memory labels has been found for: %s",
			nodeName)
	}
	if nvidiaMemoryError == nil && amdMemoryError == nil {
		return 0, fmt.Errorf(
			"the node %s has both nvidia and amd gpu labels. "+
				"Such nodes aren't supported for fraction resource requests", nodeName)
	}

	return singleNvidiaGpuMemory + singleAmdGpuMemory, nil
}

func getNodeSingleNvidiaGpuMemory(node *v1.Node) (float64, error) {
	nvidiaGpuMemoryStr, foundNvidiaLabel := node.Labels[constants.NvidiaGpuMemory]
	if !foundNvidiaLabel {
		return 0, ErrNotFoundLabel
	}
	singleGpuMemory, err := strconv.Atoi(nvidiaGpuMemoryStr)
	if err != nil {
		return 0, err
	}
	return float64(singleGpuMemory), nil
}

func getNodeSingleAmdGpuMemory(node *v1.Node) (float64, error) {
	for label := range node.Labels {
		if !strings.HasPrefix(label, amdNodeGpuMemoryLabelPrefix) {
			continue
		}
		regex := regexp.MustCompile(amdNodeGpuMemoryLabelValuePattern)
		match := regex.FindString(label)
		if match != "" {
			res, err := resource.ParseQuantity(match)
			if err != nil {
				return 0, fmt.Errorf(
					"failed to parse amd memory resource size. Label: %s, regex match: %s, error: %w",
					label, match, err)
			}
			value, success := res.AsInt64()
			if !success {
				return 0, fmt.Errorf("could not extract memory amount from amd gpu memory label %s", label)
			}
			return float64(value) / amdNodeGpuMemoryValueFactor, nil
		}
	}
	return 0, ErrNotFoundLabel
}
