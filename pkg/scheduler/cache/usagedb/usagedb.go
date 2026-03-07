// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package usagedb

import (
	"fmt"
	"sync"
	"time"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/metrics"
)

var defaultFetchInterval = 1 * time.Minute
var defaultWaitTimeout = 1 * time.Minute

type UsageLister struct {
	client             api.Interface
	lastUsageData      *queue_info.ClusterUsage
	lastUsageDataMutex sync.RWMutex
	lastUsageDataTime  *time.Time
	fetchInterval      time.Duration
	stalenessPeriod    time.Duration
	waitTimeout        time.Duration
}

func NewUsageLister(client api.Interface, fetchInterval, stalenessPeriod, waitTimeout *time.Duration) *UsageLister {
	if fetchInterval == nil {
		log.InfraLogger.V(3).Infof("fetchInterval is not set, using default: %s", defaultFetchInterval)
		fetchInterval = &defaultFetchInterval
	}

	if stalenessPeriod == nil {
		period := 5 * defaultFetchInterval
		stalenessPeriod = &period
		log.InfraLogger.V(3).Infof("stalenessPeriod is not set, using default: %s", period)
	}

	if waitTimeout == nil {
		waitTimeout = &defaultWaitTimeout
	}

	if stalenessPeriod.Seconds() < fetchInterval.Seconds() {
		log.InfraLogger.V(2).Warnf("stalenessPeriod is less than fetchInterval, using stalenessPeriod: %s", stalenessPeriod)
		stalenessPeriod = fetchInterval
	}

	return &UsageLister{
		client:          client,
		lastUsageData:   queue_info.NewClusterUsage(),
		fetchInterval:   *fetchInterval,
		stalenessPeriod: *stalenessPeriod,
		waitTimeout:     *waitTimeout,
	}
}

// GetResourceUsage returns the last known resource usage data.
// If the data is stale, an error is returned, but the most recent data is still returned.
func (l *UsageLister) GetResourceUsage() (*queue_info.ClusterUsage, error) {
	l.lastUsageDataMutex.RLock()
	defer l.lastUsageDataMutex.RUnlock()

	if l.client == nil {
		return nil, fmt.Errorf("client is not set")
	}

	if l.lastUsageDataTime == nil {
		return nil, fmt.Errorf("usage data is not available")
	}

	var err error
	if time.Since(*l.lastUsageDataTime) > l.stalenessPeriod {
		err = fmt.Errorf("usage data is stale, last update: %s, staleness period: %s, time since last update: %s", l.lastUsageDataTime, l.stalenessPeriod, time.Since(*l.lastUsageDataTime))
	}

	return l.lastUsageData, err
}

// Start begins periodic fetching of resource usage data in a background goroutine.
// The data is fetched every minute by default.
func (l *UsageLister) Start(stopCh <-chan struct{}) {
	if l.client == nil {
		log.InfraLogger.V(1).Errorf("failed to fetch usage data: client is not set")
		return
	}

	go func() {
		ticker := time.NewTicker(l.fetchInterval)
		defer ticker.Stop()

		// Fetch immediately on start
		l.fetchAndUpdateUsage()

		for {
			select {
			case <-ticker.C:
				l.fetchAndUpdateUsage()
			case <-stopCh:
				return
			}
		}
	}()
}

func (l *UsageLister) WaitForCacheSync(stopCh <-chan struct{}) bool {
	if l.client == nil {
		return true
	}

	// Check every 10ms for data or stop signal
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(l.waitTimeout)

	for {
		select {
		case <-stopCh:
			return false
		case <-timeout:
			log.InfraLogger.V(2).Warnf("usage data fetch timed out after %s", l.waitTimeout)
			return false
		case <-ticker.C:
			l.lastUsageDataMutex.RLock()
			updated := l.lastUsageDataTime != nil
			l.lastUsageDataMutex.RUnlock()

			if updated {
				return true
			}
		}
	}
}

func (l *UsageLister) fetchAndUpdateUsage() {
	now := time.Now()
	usage, err := l.client.GetResourceUsage()
	if err != nil {
		log.InfraLogger.V(1).Errorf("failed to fetch usage data: %v", err)
		return
	}
	metrics.UpdateUsageQueryLatency(time.Since(now))

	l.lastUsageDataMutex.Lock()
	defer l.lastUsageDataMutex.Unlock()

	l.lastUsageData = usage
	l.lastUsageDataTime = &now
}
