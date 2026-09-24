// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale_adjuster

import (
	"fmt"
	"log"
	"math"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

const gpuFractionDecimalRoundingFactor = 100

type calculator struct {
	gpuMemoryToFractionRatio float64
}

func newCalculator(gpuMemoryToFractionRatio float64) *calculator {
	return &calculator{
		gpuMemoryToFractionRatio: gpuMemoryToFractionRatio,
	}
}

func (c *calculator) calculateNumScalingDevices(scalingPods []*v1.Pod) int64 {
	numScalingDevices := int64(0)
	for _, pod := range scalingPods {
		resReq := pod.Spec.Containers[0].Resources.Requests
		numDevices := resReq[constants.NvidiaGpuResource]
		numScalingDevices += numDevices.Value()
	}
	return numScalingDevices
}

func (c *calculator) calculateMaxScalingDevices(scalingPods []*v1.Pod) int64 {
	maxScalingDevices := int64(0)
	for _, pod := range scalingPods {
		resReq := pod.Spec.Containers[0].Resources.Requests
		numDevices := resReq[constants.NvidiaGpuResource]
		maxScalingDevices = max(numDevices.Value(), maxScalingDevices)
	}
	return maxScalingDevices
}

func (c *calculator) calculateNumNeededDevices(unschedulablePods []*v1.Pod) (int64, []*v1.Pod) {
	numNeededDevices := float64(0)
	podsToScale := make([]*v1.Pod, 0)
	for _, pod := range unschedulablePods {
		gpuFraction, numDevices, err := c.getGPUFractionRequest(pod)
		if err != nil {
			log.Printf("could not get GPU fraction for pod %v/%v. err: %v",
				pod.Namespace, pod.Name, err)
			continue
		}
		fractionAsDecimal := int64(math.Round(gpuFraction * gpuFractionDecimalRoundingFactor))
		numNeededDevices += float64(fractionAsDecimal*numDevices) / gpuFractionDecimalRoundingFactor
		podsToScale = append(podsToScale, pod)
	}
	return int64(math.Ceil(numNeededDevices)), podsToScale
}

func (c *calculator) getGPUFractionRequest(pod *v1.Pod) (float64, int64, error) {
	req, err := resources.ParsePodGPUFractionRequest(pod)
	if err != nil {
		return 0, 0, err
	}
	if req == nil {
		return 0, 0, fmt.Errorf("pod %v/%v does not have GPU fraction or memory annotation",
			pod.Namespace, pod.Name)
	}

	switch req.FractionType {
	case resources.FractionTypePortion:
		return req.Portion, req.NumDevices, nil
	case resources.FractionTypeMemory, resources.FractionTypeNvFractions:
		// Memory-based forms (legacy gpu-memory and NvFractions) use the configured
		// ratio, matching the previous gpu-memory path.
		return c.gpuMemoryToFractionRatio, req.NumDevices, nil
	default:
		return 0, 0, fmt.Errorf("pod %v/%v has unsupported GPU fraction type %q",
			pod.Namespace, pod.Name, req.FractionType)
	}
}
