// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package usagedb

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache/usagedb/fake"
)

func TestNewUsageLister(t *testing.T) {
	tests := []struct {
		name            string
		fetchInterval   *time.Duration
		stalenessPeriod *time.Duration
		wantInterval    time.Duration
		wantStaleness   time.Duration
	}{
		{
			name:          "default values",
			wantInterval:  defaultFetchInterval,
			wantStaleness: 5 * defaultFetchInterval,
		},
		{
			name: "custom fetch interval",
			fetchInterval: func() *time.Duration {
				d := 30 * time.Second
				return &d
			}(),
			wantInterval:  30 * time.Second,
			wantStaleness: 5 * defaultFetchInterval,
		},
		{
			name: "custom staleness period",
			stalenessPeriod: func() *time.Duration {
				d := 10 * time.Minute
				return &d
			}(),
			wantInterval:  defaultFetchInterval,
			wantStaleness: 10 * time.Minute,
		},
		{
			name: "staleness less than fetch interval",
			fetchInterval: func() *time.Duration {
				d := 2 * time.Minute
				return &d
			}(),
			stalenessPeriod: func() *time.Duration {
				d := 1 * time.Minute
				return &d
			}(),
			wantInterval:  2 * time.Minute,
			wantStaleness: 2 * time.Minute, // Should be adjusted to match fetch interval
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lister := NewUsageLister(&fake.FakeClient{}, tt.fetchInterval, tt.stalenessPeriod, nil)
			assert.Equal(t, tt.wantInterval, lister.fetchInterval)
			assert.Equal(t, tt.wantStaleness, lister.stalenessPeriod)
			assert.NotNil(t, lister.lastUsageData)
			assert.Nil(t, lister.lastUsageDataTime)
		})
	}
}

func TestGetResourceUsage(t *testing.T) {
	tests := []struct {
		name        string
		setupLister func(*UsageLister)
		wantUsage   *queue_info.ClusterUsage
		wantErr     bool
	}{
		{
			name: "no data available",
			setupLister: func(l *UsageLister) {
				// Do nothing - simulate fresh lister
			},
			wantErr: true,
		},
		{
			name: "fresh data available",
			setupLister: func(l *UsageLister) {
				usage := queue_info.NewClusterUsage()
				usage.Queues["queue1"] = queue_info.QueueUsage{constants.NvidiaGpuResource: 5}
				now := time.Now()
				l.lastUsageData = usage
				l.lastUsageDataTime = &now
			},
			wantUsage: func() *queue_info.ClusterUsage {
				usage := queue_info.NewClusterUsage()
				usage.Queues["queue1"] = queue_info.QueueUsage{constants.NvidiaGpuResource: 5}
				return usage
			}(),
		},
		{
			name: "stale data",
			setupLister: func(l *UsageLister) {
				usage := queue_info.NewClusterUsage()
				staleTime := time.Now().Add(-10 * time.Minute)
				l.lastUsageData = usage
				l.lastUsageDataTime = &staleTime
			},
			wantUsage: func() *queue_info.ClusterUsage {
				usage := queue_info.NewClusterUsage()
				return usage
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lister := NewUsageLister(&fake.FakeClient{}, nil, nil, nil)
			if tt.setupLister != nil {
				tt.setupLister(lister)
			}

			got, err := lister.GetResourceUsage()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.wantUsage != nil {
				assert.Equal(t, tt.wantUsage, got)
			}
		})
	}
}
