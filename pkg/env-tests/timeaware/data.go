// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package timeaware

import (
	"fmt"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
)

type SimulationDataPoint struct {
	Allocation float64
	FairShare  float64
}

type SimulationHistory []map[common_info.QueueID]SimulationDataPoint

func (a SimulationHistory) ToCSV() string {
	var builder strings.Builder

	// Pre-allocate based on estimated size
	// Header + estimated 50 bytes per entry
	estimatedSize := 32
	for _, dataPoints := range a {
		estimatedSize += len(dataPoints) * 50
	}
	builder.Grow(estimatedSize)

	builder.WriteString("Time,QueueID,Allocation,FairShare\n")
	for timeIndex, allocationDataPoint := range a {
		for queueID, allocationDataPoint := range allocationDataPoint {
			builder.WriteString(fmt.Sprintf("%d,%s,%f,%f\n", timeIndex, queueID, allocationDataPoint.Allocation, allocationDataPoint.FairShare))
		}
	}
	return builder.String()
}
