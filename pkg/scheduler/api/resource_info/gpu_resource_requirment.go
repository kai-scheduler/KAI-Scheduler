// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	"fmt"
	"math"
	"strings"

	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
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
	migResources map[v1.ResourceName]int64
}

func NewGpuResourceRequirement() *GpuResourceRequirement {
	return &GpuResourceRequirement{
		count:        0,
		portion:      0,
		gpuMemory:    0,
		migResources: make(map[v1.ResourceName]int64),
	}
}

func NewGpuResourceRequirementWithGpus(gpus float64, gpuMemory int64) *GpuResourceRequirement {
	gResource := &GpuResourceRequirement{
		count:        0,
		portion:      gpus,
		gpuMemory:    gpuMemory,
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
		migResources: make(map[v1.ResourceName]int64),
	}
	return gResource
}

func NewGpuResourceRequirementWithMig(migResources map[v1.ResourceName]int64) *GpuResourceRequirement {
	return &GpuResourceRequirement{
		count:        0,
		portion:      0,
		gpuMemory:    0,
		migResources: migResources,
	}
}

func (g *GpuResourceRequirement) IsEmpty() bool {
	if getNonMigGpus(g.portion, g.count) > minGPUs {
		return false
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
		migResources: maps.Clone(g.migResources),
	}
}

func (g *GpuResourceRequirement) SetMaxResource(gg *GpuResourceRequirement) error {
	if g.portion != 0 && gg.portion != 0 && g.portion != gg.portion {
		return fmt.Errorf("cannot calculate max resource for GpuResourceRequirements with different fractional portions. %v vs %v", g.portion, gg.portion)
	}
	if getNonMigGpus(gg.portion, gg.count) > getNonMigGpus(g.portion, g.count) {
		g.count = gg.count
		g.portion = gg.portion
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

func (g *GpuResourceRequirement) GetSumGPUs() float64 {
	var totalMigGPUs float64
	for migResource, quant := range g.migResources {
		gpuPortion, _, err := resources.ExtractGpuAndMemoryFromMigResourceName(migResource.String())
		if err != nil {
			log.InfraLogger.Errorf("Failed to get device portion from %v", migResource)
			continue
		}

		totalMigGPUs += float64(gpuPortion) * float64(quant)
	}

	return totalMigGPUs + getNonMigGpus(g.portion, g.count)
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
	return getNonMigGpus(g.portion, g.count)
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

func getNonMigGpus(portion float64, count int64) float64 {
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
