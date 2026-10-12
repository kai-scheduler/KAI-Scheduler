// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale_adjuster

import (
	"fmt"
	"log"
	"math"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/api/constants"
	"github.com/kai-scheduler/api/utilities/resources"
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

func (c *calculator) calculateNumNeededDevices(unschedulableFractionalPods []*v1.Pod) (int64, []*v1.Pod) {
	numNeededDevices := float64(0)
	podsToScale := make([]*v1.Pod, 0)
	for _, pod := range unschedulableFractionalPods {
		gpuFraction, err := c.getGPUFraction(pod)
		if err != nil {
			log.Printf("could not get GPU fraction for pod %v/%v. err: %v",
				pod.Namespace, pod.Name, err)
			continue
		}
		numDevices, err := resources.GetNumGPUFractionDevices(pod)
		if err != nil {
			log.Printf("could not get num GPU devices for pod %v/%v. err: %v",
				pod.Namespace, pod.Name, err)
			continue
		}
		fractionAsDecimal := int64(math.Round(gpuFraction * gpuFractionDecimalRoundingFactor))
		numNeededDevices += float64(fractionAsDecimal*numDevices) / gpuFractionDecimalRoundingFactor
		podsToScale = append(podsToScale, pod)
	}
	return int64(math.Ceil(numNeededDevices)), podsToScale
}

func (c *calculator) getGPUFraction(pod *v1.Pod) (float64, error) {
	req, err := resources.ParsePodGPUFractionRequest(pod)
	if err != nil {
		return 0, fmt.Errorf("failed to parse GPU fraction request for pod %v/%v: %w", pod.Namespace, pod.Name, err)
	}
	if req == nil {
		return 0, nil // not a fractional pod
	}
	if req.Portion != 0 {
		return req.Portion, nil
	}
	if req.Memory != nil {
		return c.gpuMemoryToFractionRatio, nil
	}
	return 0, fmt.Errorf("failed to parse either portion or memory for fractional pod %v/%v", pod.Namespace, pod.Name)
}
