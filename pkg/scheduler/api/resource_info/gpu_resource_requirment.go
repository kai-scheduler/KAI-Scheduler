// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info/resources"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/log"
)

const (
	minGPUs                             = 0.01
	gpuPortionsAsDecimalsRoundingFactor = 100
	wholeGpuPortion                     = 1
	fractionDefaultCount                = 1
)

type GpuResourceRequirement struct {
	count        int64
	portion      float64
	gpuMemory    int64
	draGpuCounts map[string]int64
	migResources map[v1.ResourceName]int64
}

func NewGpuResourceRequirement() *GpuResourceRequirement {
	return &GpuResourceRequirement{
		count:        0,
		portion:      0,
		gpuMemory:    0,
		draGpuCounts: make(map[string]int64),
		migResources: make(map[v1.ResourceName]int64),
	}
}

func NewGpuResourceRequirementWithGpus(gpus float64, gpuMemory int64) *GpuResourceRequirement {
	gResource := &GpuResourceRequirement{
		count:        0,
		portion:      gpus,
		gpuMemory:    gpuMemory,
		draGpuCounts: make(map[string]int64),
		migResources: make(map[v1.ResourceName]int64),
	}
	if gpus >= wholeGpuPortion {
		gResource.count = int64(gpus)
		gResource.portion = wholeGpuPortion
	} else if gpus > 0 || gpuMemory > 0 { // Fraction
		gResource.count = fractionDefaultCount
	}
	return gResource
}

func NewGpuResourceRequirementWithMultiFraction(count int64, portion float64, gpuMemory int64) *GpuResourceRequirement {
	gResource := &GpuResourceRequirement{
		count:        count,
		portion:      portion,
		gpuMemory:    gpuMemory,
		draGpuCounts: make(map[string]int64),
		migResources: make(map[v1.ResourceName]int64),
	}
	return gResource
}

func NewGpuResourceRequirementWithMig(migResources map[v1.ResourceName]int64) *GpuResourceRequirement {
	return &GpuResourceRequirement{
		count:        0,
		portion:      0,
		gpuMemory:    0,
		draGpuCounts: make(map[string]int64),
		migResources: migResources,
	}
}

func (g *GpuResourceRequirement) SetDraGpus(draGpus map[string]int64) {
	g.draGpuCounts = make(map[string]int64, len(draGpus))
	for deviceClassName, count := range draGpus {
		g.draGpuCounts[deviceClassName] += count
	}
}

func (g *GpuResourceRequirement) IsEmpty() bool {
	if getExtendedResourceGpus(g.portion, g.count) > minGPUs {
		return false
	}
	for _, rQuant := range g.draGpuCounts {
		if rQuant > 0 {
			return false
		}
	}
	for _, rQuant := range g.migResources {
		if rQuant > 0 {
			return false
		}
	}
	return true
}

func (g *GpuResourceRequirement) Clone() *GpuResourceRequirement {
	return &GpuResourceRequirement{
		count:        g.count,
		portion:      g.portion,
		gpuMemory:    g.gpuMemory,
		draGpuCounts: maps.Clone(g.draGpuCounts),
		migResources: maps.Clone(g.migResources),
	}
}

func (g *GpuResourceRequirement) SetMaxResource(gg *GpuResourceRequirement) error {
	if g.portion != 0 && gg.portion != 0 && g.portion != gg.portion {
		return fmt.Errorf("cannot calculate max resource for GpuResourceRequirements with different fractional portions. %v vs %v", g.portion, gg.portion)
	}
	if getExtendedResourceGpus(gg.portion, gg.count) > getExtendedResourceGpus(g.portion, g.count) {
		g.count = gg.count
		g.portion = gg.portion
	}
	for name, ggQuant := range gg.draGpuCounts {
		if gQuant, found := g.draGpuCounts[name]; !found || ggQuant > gQuant {
			g.draGpuCounts[name] = ggQuant
		}
	}
	for name, ggQuant := range gg.migResources {
		if gQuant, found := g.migResources[name]; !found || ggQuant > gQuant {
			g.migResources[name] = ggQuant
		}
	}
	return nil
}

func (g *GpuResourceRequirement) LessEqual(gg *GpuResourceRequirement) bool {
	if g.count > gg.count {
		return false
	}
	if !lessEqualWithMinDiff(g.portion, gg.portion, minGPUs) {
		return false
	}

	for name, gQuant := range g.migResources {
		ggQuant, found := gg.migResources[name]
		if !found || gQuant > ggQuant {
			return false
		}
	}
	return true
}

// GetGpusQuota returns the total number of gpus requested by the pod. It includes whole gpus, fractional gpus, gpu dra claims and the MIG gpus.
func (g *GpuResourceRequirement) GetGpusQuota() float64 {
	var totalGpusQuota float64
	for migResource, quant := range g.migResources {
		gpuPortion, _, err := resources.ExtractGpuAndMemoryFromMigResourceName(migResource.String())
		if err != nil {
			log.InfraLogger.Errorf("Failed to get device portion from %v", migResource)
			continue
		}

		totalGpusQuota += float64(gpuPortion) * float64(quant)
	}
	for _, count := range g.draGpuCounts {
		totalGpusQuota += float64(count)
	}
	totalGpusQuota += getExtendedResourceGpus(g.portion, g.count)

	return totalGpusQuota
}

func (g *GpuResourceRequirement) DraGpuCounts() map[string]int64 {
	return g.draGpuCounts
}

func (g *GpuResourceRequirement) GetDraGpusCount() int64 {
	count := int64(0)
	for _, singleClaimCount := range g.draGpuCounts {
		count += singleClaimCount
	}
	return count
}

func (g *GpuResourceRequirement) MigResources() map[v1.ResourceName]int64 {
	return g.migResources
}

func (g *GpuResourceRequirement) ClearMigResources() {
	g.migResources = map[v1.ResourceName]int64{}
}

func (g *GpuResourceRequirement) GpuMemory() int64 {
	return g.gpuMemory
}

func (g *GpuResourceRequirement) GPUs() float64 {
	return getExtendedResourceGpus(g.portion, g.count)
}

func (g *GpuResourceRequirement) GetNumOfGpuDevices() int64 {
	return g.count
}

func (g *GpuResourceRequirement) GpuFractionalPortion() float64 {
	return g.portion
}

func (g *GpuResourceRequirement) IsFractionalRequest() bool {
	if g.gpuMemory > 0 {
		return true
	}
	return g.count > 0 && g.portion < wholeGpuPortion
}

func (g *GpuResourceRequirement) GpusAsString() string {
	var requestedGpuString string
	if g.IsFractionalRequest() && g.GetNumOfGpuDevices() > 1 {
		requestedGpuString = fmt.Sprintf("%d X %s", g.GetNumOfGpuDevices(),
			strconv.FormatFloat(g.GpuFractionalPortion(), 'g', 3, 64))
	} else if len(g.draGpuCounts) > 0 {
		requestedGpuString = strconv.FormatFloat(float64(g.GetDraGpusCount()), 'g', 3, 64) + " as DRA gpu claims "
	} else {
		requestedGpuString = strconv.FormatFloat(g.GPUs(), 'g', 3, 64)
	}
	return requestedGpuString
}

func getExtendedResourceGpus(portion float64, count int64) float64 {
	// use fixed-point arithmetic to avoid floating point errors
	portionAsDecimals := int64(math.Round(portion * gpuPortionsAsDecimalsRoundingFactor))
	return float64(portionAsDecimals*count) / gpuPortionsAsDecimalsRoundingFactor
}

func IsMigResource(rName v1.ResourceName) bool {
	return strings.HasPrefix(string(rName), "nvidia.com/mig-")
}

func lessEqualWithMinDiff(l, r, minDiff float64) bool {
	return l < r || math.Abs(l-r) < minDiff
}
