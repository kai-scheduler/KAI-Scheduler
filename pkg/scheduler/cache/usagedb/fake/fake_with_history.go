// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package fake

import (
	"math"
	"sync"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache/usagedb/api"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FakeUsageDBClient struct {
	resourceUsage      *queue_info.ClusterUsage
	resourceUsageMutex sync.RWMutex

	usageParams *api.UsageParams
	// each entry is an allocation per second
	allocationHistory      AllocationHistory
	clusterCapacityHistory ClusterCapacityHistory
}

var _ api.Interface = &FakeUsageDBClient{}
var ClientWithHistory *FakeUsageDBClient

func NewFakeWithHistoryClient(_ string, usageParams *api.UsageParams) (api.Interface, error) {
	if ClientWithHistory == nil {
		ClientWithHistory = &FakeUsageDBClient{
			resourceUsage: queue_info.NewClusterUsage(),
			usageParams:   usageParams,
		}
	}

	return ClientWithHistory, nil
}

func (f *FakeUsageDBClient) ResetClient() {
	ClientWithHistory = nil
}

func (f *FakeUsageDBClient) GetResourceUsage() (*queue_info.ClusterUsage, error) {
	f.resourceUsageMutex.RLock()
	defer f.resourceUsageMutex.RUnlock()

	usage := queue_info.NewClusterUsage()

	var windowStart, windowEnd int
	size := f.usageParams.WindowSize.Duration.Seconds()
	l := len(f.allocationHistory)
	if l == 0 {
		return usage, nil
	}
	if l <= int(size) {
		windowStart = 0
		windowEnd = l
	} else {
		windowStart = l - int(size)
		windowEnd = l
	}

	decaySlice := getDecaySlice(int(size), f.usageParams.HalfLifePeriod)

	capacities := make(map[v1.ResourceName]float64)
	for i := range f.clusterCapacityHistory[windowStart:windowEnd] {
		for resource, capacity := range f.clusterCapacityHistory[windowStart:windowEnd][i] {
			if _, exists := capacities[resource]; !exists {
				capacities[resource] = 0
			}
			capacities[resource] += capacity * decaySlice[i]
		}
	}

	for i, queueAllocations := range f.allocationHistory[windowStart:windowEnd] {
		for queueID, allocation := range queueAllocations {
			if _, exists := usage.Queues[queueID]; !exists {
				usage.Queues[queueID] = queue_info.QueueUsage{}
			}
			for resource, allocation := range allocation {
				if _, exists := usage.Queues[queueID][resource]; !exists {
					usage.Queues[queueID][resource] = 0
				}
				usage.Queues[queueID][resource] += allocation * decaySlice[i]
			}
		}
	}

	for queueID, queueUsage := range usage.Queues {
		for resource, usageValue := range queueUsage {
			usage.Queues[queueID][resource] = usageValue / capacities[resource]
		}
	}

	return usage, nil
}

func (f *FakeUsageDBClient) AppendQueueAllocation(queueAllocations map[common_info.QueueID]queue_info.QueueUsage, totalInCluster map[v1.ResourceName]float64) {
	f.resourceUsageMutex.Lock()
	defer f.resourceUsageMutex.Unlock()

	f.allocationHistory = append(f.allocationHistory, queueAllocations)
	f.clusterCapacityHistory = append(f.clusterCapacityHistory, totalInCluster)
}

type AllocationHistory []map[common_info.QueueID]queue_info.QueueUsage
type ClusterCapacityHistory []map[v1.ResourceName]float64

func getDecaySlice(length int, period *metav1.Duration) []float64 {
	if period == nil || period.Duration.Seconds() == 0 {
		decaySlice := make([]float64, length)
		for i := range decaySlice {
			decaySlice[i] = 1
		}
		return decaySlice
	}

	seconds := period.Duration.Seconds()
	decaySlice := make([]float64, length)
	for i := range decaySlice {
		val := math.Pow(0.5, float64(length-i)/seconds)
		decaySlice[i] = val
	}
	return decaySlice
}
