// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package fake

import (
	"sync"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache/usagedb/api"
)

type FakeClient struct {
	resourceUsage      *queue_info.ClusterUsage
	resourceUsageMutex sync.RWMutex
	resourceUsageErr   error
}

var _ api.Interface = &FakeClient{}
var Client *FakeClient

func NewFakeClient(_ string, _ *api.UsageParams) (api.Interface, error) {
	if Client == nil {
		Client = &FakeClient{
			resourceUsage: queue_info.NewClusterUsage(),
		}
	}

	return Client, nil
}

func (f *FakeClient) GetResourceUsage() (*queue_info.ClusterUsage, error) {
	f.resourceUsageMutex.RLock()
	defer f.resourceUsageMutex.RUnlock()

	return f.resourceUsage, f.resourceUsageErr
}

func (f *FakeClient) SetResourceUsage(resourceUsage *queue_info.ClusterUsage, err error) {
	f.resourceUsageMutex.Lock()
	defer f.resourceUsageMutex.Unlock()

	f.resourceUsage = resourceUsage
	f.resourceUsageErr = err
}
