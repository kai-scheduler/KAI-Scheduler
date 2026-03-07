// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package prometheus

import (
	"testing"
	"time"

	"github.com/aptible/supercronic/cronexpr"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewPrometheusClient(t *testing.T) {
	tests := []struct {
		name          string
		address       string
		params        *api.UsageParams
		expectError   bool
		errorContains string
	}{
		{
			name:          "valid address",
			address:       "http://localhost:9090",
			params:        nil,
			expectError:   false,
			errorContains: "",
		},
		{
			name:          "invalid address",
			address:       "://invalid:9090",
			params:        nil,
			expectError:   true,
			errorContains: "error creating prometheus client",
		},
		{
			name:    "invalid cron string - for cron window",
			address: "http://localhost:9090",
			params: &api.UsageParams{
				WindowType: &[]api.WindowType{api.CronWindow}[0],
				CronString: "invalid",
			},
			expectError:   true,
			errorContains: "error parsing cron string 'invalid' for usage tumbling window",
		},
		{
			name:    "invalid cron string - for sliding window",
			address: "http://localhost:9090",
			params: &api.UsageParams{
				WindowType: &[]api.WindowType{api.SlidingWindow}[0],
				CronString: "invalid",
			},
			expectError:   false,
			errorContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.params == nil {
				tt.params = &api.UsageParams{}
				tt.params.SetDefaults()
			}
			client, err := NewPrometheusClient(tt.address, tt.params)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				assert.Nil(t, client)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, client)
				promClient, ok := client.(*PrometheusClient)
				require.True(t, ok)
				assert.NotNil(t, promClient.client)
				assert.NotNil(t, promClient.promClient)
			}
		})
	}
}

func TestGetLatestUsageResetTime_CronWindow(t *testing.T) {
	tests := []struct {
		name              string
		cronExpression    string
		now               time.Time
		expectedResetTime time.Time
	}{
		{
			name:           "hourly cron - middle of hour",
			cronExpression: "0 * * * *", // Every hour at minute 0
			now:            time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
			// Should return 14:00:00 (last occurrence before 14:30:00)
			expectedResetTime: time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC),
		},
		{
			name:           "hourly cron - exact hour",
			cronExpression: "0 * * * *", // Every hour at minute 0
			now:            time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC),
			// Should return 13:00:00 (when exactly at boundary, it's considered the next window started)
			expectedResetTime: time.Date(2025, 1, 15, 13, 0, 0, 0, time.UTC),
		},
		{
			name:           "daily cron at midnight - morning time",
			cronExpression: "0 0 * * *", // Every day at midnight
			now:            time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			// Should return midnight of current day
			expectedResetTime: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:           "daily cron at midnight - before midnight",
			cronExpression: "0 0 * * *", // Every day at midnight
			now:            time.Date(2025, 1, 15, 23, 59, 59, 0, time.UTC),
			// Should return midnight of current day (not next day)
			expectedResetTime: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:           "every 6 hours - between intervals",
			cronExpression: "0 */6 * * *", // Every 6 hours
			now:            time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			// Should return 06:00:00 (last 6-hour mark before 10:00)
			expectedResetTime: time.Date(2025, 1, 15, 6, 0, 0, 0, time.UTC),
		},
		{
			name:           "every 6 hours - exact interval",
			cronExpression: "0 */6 * * *", // Every 6 hours
			now:            time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
			// Should return 06:00:00 (when exactly at boundary, previous boundary is returned)
			expectedResetTime: time.Date(2025, 1, 15, 6, 0, 0, 0, time.UTC),
		},
		{
			name:           "weekly cron - monday at noon - middle of week",
			cronExpression: "0 12 * * 1",                                  // Every Monday at 12:00
			now:            time.Date(2025, 1, 15, 15, 0, 0, 0, time.UTC), // Wednesday
			// Should return previous Monday at 12:00
			expectedResetTime: time.Date(2025, 1, 13, 12, 0, 0, 0, time.UTC), // Monday
		},
		{
			name:           "monthly cron - first of month - one hour before next month (31 to 30 days)",
			cronExpression: "0 0 1 * *",                                   // First of every month at midnight
			now:            time.Date(2025, 3, 31, 23, 0, 0, 0, time.UTC), // March 31 at 11pm (March has 31 days, April has 30)
			// Should return March 1st (current month start)
			expectedResetTime: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:           "every 15 minutes - between intervals",
			cronExpression: "*/15 * * * *", // Every 15 minutes
			now:            time.Date(2025, 1, 15, 14, 37, 0, 0, time.UTC),
			// Should return 14:30:00
			expectedResetTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
		},
		{
			name:           "every 15 minutes - exact interval",
			cronExpression: "*/15 * * * *", // Every 15 minutes
			now:            time.Date(2025, 1, 15, 14, 45, 0, 0, time.UTC),
			// Should return 14:30:00 (when exactly at boundary, previous boundary is returned)
			expectedResetTime: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the cron expression
			cronExpr, err := cronexpr.Parse(tt.cronExpression)
			require.NoError(t, err, "Failed to parse cron expression")

			// Create a PrometheusClient with the cron expression
			client := &PrometheusClient{
				cronWindowExpression: cronExpr,
			}

			// Call the function with the test's "now" time
			result := client.getLatestUsageResetTime_CronWindow(tt.now)

			// Verify the result matches the expected reset time
			assert.Equal(t, tt.expectedResetTime, result,
				"Expected reset time: %v, Got: %v, Now: %v",
				tt.expectedResetTime, result, tt.now)

			// Additional validation: verify the result is before or equal to now
			assert.True(t, result.Before(tt.now) || result.Equal(tt.now),
				"Result should be before or equal to now. Result: %v, Now: %v", result, tt.now)

			// Verify the next occurrence is after or equal to now (equal when exactly at boundary)
			nextAfterResult := cronExpr.Next(result)
			assert.True(t, nextAfterResult.After(tt.now) || nextAfterResult.Equal(tt.now),
				"Next occurrence should be after or equal to now. Next: %v, Now: %v", nextAfterResult, tt.now)
		})
	}
}

func TestNewPrometheusClient_ExtraParams(t *testing.T) {
	// Default metric values for reference
	defaultGpuAllocationMetric := "kai_queue_allocated_gpus"
	defaultCpuAllocationMetric := "kai_queue_allocated_cpu_cores"
	defaultMemoryAllocationMetric := "kai_queue_allocated_memory_bytes"
	defaultGpuCapacityMetric := "sum(kube_node_status_capacity{resource=\"nvidia_com_gpu\"})"
	defaultCpuCapacityMetric := "sum(kube_node_status_capacity{resource=\"cpu\"})"
	defaultMemoryCapacityMetric := "sum(kube_node_status_capacity{resource=\"memory\"})"

	tests := []struct {
		name                           string
		extraParams                    map[string]string
		expectedGpuAllocationMetric    string
		expectedCpuAllocationMetric    string
		expectedMemoryAllocationMetric string
		expectedGpuCapacityMetric      string
		expectedCpuCapacityMetric      string
		expectedMemoryCapacityMetric   string
	}{
		{
			name:                           "nil extra params - uses defaults",
			extraParams:                    nil,
			expectedGpuAllocationMetric:    defaultGpuAllocationMetric,
			expectedCpuAllocationMetric:    defaultCpuAllocationMetric,
			expectedMemoryAllocationMetric: defaultMemoryAllocationMetric,
			expectedGpuCapacityMetric:      defaultGpuCapacityMetric,
			expectedCpuCapacityMetric:      defaultCpuCapacityMetric,
			expectedMemoryCapacityMetric:   defaultMemoryCapacityMetric,
		},
		{
			name:                           "empty extra params - uses defaults",
			extraParams:                    map[string]string{},
			expectedGpuAllocationMetric:    defaultGpuAllocationMetric,
			expectedCpuAllocationMetric:    defaultCpuAllocationMetric,
			expectedMemoryAllocationMetric: defaultMemoryAllocationMetric,
			expectedGpuCapacityMetric:      defaultGpuCapacityMetric,
			expectedCpuCapacityMetric:      defaultCpuCapacityMetric,
			expectedMemoryCapacityMetric:   defaultMemoryCapacityMetric,
		},
		{
			name: "custom allocation metrics only",
			extraParams: map[string]string{
				"gpuAllocationMetric":    "custom_gpu_allocation",
				"cpuAllocationMetric":    "custom_cpu_allocation",
				"memoryAllocationMetric": "custom_memory_allocation",
			},
			expectedGpuAllocationMetric:    "custom_gpu_allocation",
			expectedCpuAllocationMetric:    "custom_cpu_allocation",
			expectedMemoryAllocationMetric: "custom_memory_allocation",
			expectedGpuCapacityMetric:      defaultGpuCapacityMetric,
			expectedCpuCapacityMetric:      defaultCpuCapacityMetric,
			expectedMemoryCapacityMetric:   defaultMemoryCapacityMetric,
		},
		{
			name: "custom capacity metrics only",
			extraParams: map[string]string{
				"gpuCapacityMetric":    "sum(custom_gpu_capacity)",
				"cpuCapacityMetric":    "sum(custom_cpu_capacity)",
				"memoryCapacityMetric": "sum(custom_memory_capacity)",
			},
			expectedGpuAllocationMetric:    defaultGpuAllocationMetric,
			expectedCpuAllocationMetric:    defaultCpuAllocationMetric,
			expectedMemoryAllocationMetric: defaultMemoryAllocationMetric,
			expectedGpuCapacityMetric:      "sum(custom_gpu_capacity)",
			expectedCpuCapacityMetric:      "sum(custom_cpu_capacity)",
			expectedMemoryCapacityMetric:   "sum(custom_memory_capacity)",
		},
		{
			name: "all custom metrics",
			extraParams: map[string]string{
				"gpuAllocationMetric":    "my_gpu_alloc",
				"cpuAllocationMetric":    "my_cpu_alloc",
				"memoryAllocationMetric": "my_mem_alloc",
				"gpuCapacityMetric":      "my_gpu_cap",
				"cpuCapacityMetric":      "my_cpu_cap",
				"memoryCapacityMetric":   "my_mem_cap",
			},
			expectedGpuAllocationMetric:    "my_gpu_alloc",
			expectedCpuAllocationMetric:    "my_cpu_alloc",
			expectedMemoryAllocationMetric: "my_mem_alloc",
			expectedGpuCapacityMetric:      "my_gpu_cap",
			expectedCpuCapacityMetric:      "my_cpu_cap",
			expectedMemoryCapacityMetric:   "my_mem_cap",
		},
		{
			name: "unrelated extra params are ignored",
			extraParams: map[string]string{
				"someOtherParam":      "someValue",
				"gpuAllocationMetric": "custom_gpu_with_other_params",
			},
			expectedGpuAllocationMetric:    "custom_gpu_with_other_params",
			expectedCpuAllocationMetric:    defaultCpuAllocationMetric,
			expectedMemoryAllocationMetric: defaultMemoryAllocationMetric,
			expectedGpuCapacityMetric:      defaultGpuCapacityMetric,
			expectedCpuCapacityMetric:      defaultCpuCapacityMetric,
			expectedMemoryCapacityMetric:   defaultMemoryCapacityMetric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := &api.UsageParams{
				ExtraParams: tt.extraParams,
			}
			params.SetDefaults()

			client, err := NewPrometheusClient("http://localhost:9090", params)
			require.NoError(t, err)
			require.NotNil(t, client)

			promClient, ok := client.(*PrometheusClient)
			require.True(t, ok)

			// Verify allocation metrics
			assert.Equal(t, tt.expectedGpuAllocationMetric, promClient.allocationMetricsMap["nvidia.com/gpu"],
				"GPU allocation metric mismatch")
			assert.Equal(t, tt.expectedCpuAllocationMetric, promClient.allocationMetricsMap["cpu"],
				"CPU allocation metric mismatch")
			assert.Equal(t, tt.expectedMemoryAllocationMetric, promClient.allocationMetricsMap["memory"],
				"Memory allocation metric mismatch")

			// Verify capacity metrics
			assert.Equal(t, tt.expectedGpuCapacityMetric, promClient.capacityMetricsMap["nvidia.com/gpu"],
				"GPU capacity metric mismatch")
			assert.Equal(t, tt.expectedCpuCapacityMetric, promClient.capacityMetricsMap["cpu"],
				"CPU capacity metric mismatch")
			assert.Equal(t, tt.expectedMemoryCapacityMetric, promClient.capacityMetricsMap["memory"],
				"Memory capacity metric mismatch")
		})
	}
}

func TestGetLatestUsageResetTime_TumblingWindow(t *testing.T) {
	tests := []struct {
		name              string
		windowSize        time.Duration
		startTime         metav1.Time
		now               metav1.Time
		expectedResetTime metav1.Time
	}{
		{
			name:       "1 hour window - middle of window",
			windowSize: 1 * time.Hour,
			startTime:  metav1.Time{Time: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)},
			// Reference time would be "now", so we test relative to start
			now:               metav1.Time{Time: time.Date(2025, 1, 15, 12, 30, 0, 0, time.UTC)},
			expectedResetTime: metav1.Time{Time: time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)},
		},
		{
			name:              "1 hour window - exact window boundary",
			windowSize:        1 * time.Hour,
			startTime:         metav1.Time{Time: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)},
			now:               metav1.Time{Time: time.Date(2025, 1, 15, 13, 0, 0, 0, time.UTC)},
			expectedResetTime: metav1.Time{Time: time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)},
		},
		{
			name:              "1 hour window - one second after boundary",
			windowSize:        1 * time.Hour,
			startTime:         metav1.Time{Time: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)},
			now:               metav1.Time{Time: time.Date(2025, 1, 15, 13, 0, 1, 0, time.UTC)},
			expectedResetTime: metav1.Time{Time: time.Date(2025, 1, 15, 13, 0, 0, 0, time.UTC)},
		},
		{
			name:              "15 minute window - between boundaries",
			windowSize:        15 * time.Minute,
			startTime:         metav1.Time{Time: time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC)},
			now:               metav1.Time{Time: time.Date(2025, 1, 15, 14, 37, 0, 0, time.UTC)},
			expectedResetTime: metav1.Time{Time: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC)},
		},
		{
			name:              "1 week window - middle of week",
			windowSize:        7 * 24 * time.Hour,
			startTime:         metav1.Time{Time: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
			now:               metav1.Time{Time: time.Date(2025, 1, 18, 12, 0, 0, 0, time.UTC)},
			expectedResetTime: metav1.Time{Time: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)},
		},
		{
			name:              "start time equals now",
			windowSize:        1 * time.Hour,
			startTime:         metav1.Time{Time: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)},
			now:               metav1.Time{Time: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)},
			expectedResetTime: metav1.Time{Time: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)},
		},
		{
			name:       "start time with offset - middle of window",
			windowSize: 1 * time.Hour,
			startTime:  metav1.Time{Time: time.Date(2025, 1, 15, 10, 15, 30, 0, time.UTC)}, // Start at 10:15:30
			now:        metav1.Time{Time: time.Date(2025, 1, 15, 12, 45, 30, 0, time.UTC)},
			// Windows: 10:15:30-11:15:30, 11:15:30-12:15:30, 12:15:30-13:15:30
			expectedResetTime: metav1.Time{Time: time.Date(2025, 1, 15, 12, 15, 30, 0, time.UTC)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a PrometheusClient with the window parameters
			client := &PrometheusClient{
				tumblingWindowStartTime: tt.startTime,
				usageParams: &api.UsageParams{
					WindowSize: &metav1.Duration{Duration: tt.windowSize},
				},
			}

			// Call the function with the test's "now" time
			result := client.getLatestUsageResetTime_TumblingWindow(tt.now.Time)

			// Verify the result matches the expected reset time
			assert.Equal(t, tt.expectedResetTime.Time, result,
				"Expected reset time: %v, Got: %v, Now: %v, Start: %v, WindowSize: %v",
				tt.expectedResetTime.Time, result, tt.now.Time, tt.startTime.Time, tt.windowSize)

			// Additional validation: verify the result is before or equal to now
			assert.True(t, result.Before(tt.now.Time) || result.Equal(tt.now.Time),
				"Result should be before or equal to now. Result: %v, Now: %v", result, tt.now.Time)

			// Verify the next window boundary is after or equal to now (equal when exactly at boundary)
			nextBoundary := result.Add(tt.windowSize)
			assert.True(t, nextBoundary.After(tt.now.Time) || nextBoundary.Equal(tt.now.Time),
				"Next boundary should be after or equal to now. Next: %v, Now: %v", nextBoundary, tt.now.Time)

			// Verify the result is aligned with the start time
			timeSinceStart := result.Sub(tt.startTime.Time)
			assert.Equal(t, int64(0), int64(timeSinceStart)%int64(tt.windowSize),
				"Result should be aligned with window boundaries")
		})
	}
}
