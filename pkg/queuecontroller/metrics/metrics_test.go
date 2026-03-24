// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"testing"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestQueueMetrics(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "queuecontroller metrics tests")
}

var _ = Describe("Queue Metrics", Ordered, func() {
	var queue *v2.Queue

	BeforeAll(func() {
		InitMetrics("testns",
			map[string]string{"priority": "queue_priority", "some-other-label": "some_other_label"},
			map[string]string{"priority": "normal"},
		)
	})

	AfterEach(func() {
		ResetQueueMetrics("test-queue")
	})

	It("should create metrics with correct labels for a queue with the label", func() {
		queue = &v2.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-queue",
				Labels: map[string]string{"priority": "high", "some-other-label": "value"},
			},
			Spec: v2.QueueSpec{
				Resources: &v2.QueueResources{
					GPU:    v2.QueueResource{Quota: 2},
					CPU:    v2.QueueResource{Quota: 500},
					Memory: v2.QueueResource{Quota: 4},
				},
			},
			Status: v2.QueueStatus{
				Allocated: map[v1.ResourceName]resource.Quantity{
					"nvidia.com/gpu":  resource.MustParse("1.5"),
					v1.ResourceCPU:    resource.MustParse("250m"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
			},
		}
		SetQueueMetrics(queue)

		labels := []string{"test-queue", "high", "value"}

		// Use the helper for all metrics
		expectMetricValue(queueInfo, labels, 1)
		expectMetricValue(queueDeservedGPUs, labels, 2)
		expectMetricValue(queueQuotaCPU, labels, 0.5)
		expectMetricValue(queueQuotaMemory, labels, 4000000)
		expectMetricValue(queueAllocatedGpus, labels, 1.5)
		expectMetricValue(queueAllocatedCpus, labels, 0.25)
		expectMetricValue(queueAllocatedMemory, labels, 2147483648)
	})

	It("should use default label value if label is missing", func() {
		queue = &v2.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue",
			},
			Spec: v2.QueueSpec{
				Resources: &v2.QueueResources{
					GPU:    v2.QueueResource{Quota: 0.7},
					CPU:    v2.QueueResource{Quota: 1000},
					Memory: v2.QueueResource{Quota: 2},
				},
			},
			Status: v2.QueueStatus{
				Allocated: map[v1.ResourceName]resource.Quantity{
					"somethingelse/gpu": resource.MustParse("0.3"),
					v1.ResourceCPU:      resource.MustParse("500m"),
					v1.ResourceMemory:   resource.MustParse("1Gi"),
				},
			},
		}
		SetQueueMetrics(queue)

		labels := []string{"test-queue", "normal", ""}

		expectMetricValue(queueInfo, labels, 1)
		expectMetricValue(queueDeservedGPUs, labels, 0.7)
		expectMetricValue(queueQuotaCPU, labels, 1)
		expectMetricValue(queueQuotaMemory, labels, 2000000)
		expectMetricValue(queueAllocatedGpus, labels, 0.3)
		expectMetricValue(queueAllocatedCpus, labels, 0.5)
		expectMetricValue(queueAllocatedMemory, labels, 1073741824)
	})

	It("should use empty string if label and default are missing", func() {
		queue = &v2.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-queue",
			},
			Spec: v2.QueueSpec{
				Resources: &v2.QueueResources{
					GPU:    v2.QueueResource{Quota: 1},
					CPU:    v2.QueueResource{Quota: 1000},
					Memory: v2.QueueResource{Quota: 2},
				},
			},
			Status: v2.QueueStatus{
				Allocated: map[v1.ResourceName]resource.Quantity{},
			},
		}
		SetQueueMetrics(queue)

		labels := []string{"test-queue", "normal", ""}

		expectMetricValue(queueInfo, labels, 1)
		expectMetricValue(queueDeservedGPUs, labels, 1)
		expectMetricValue(queueQuotaCPU, labels, 1)
		expectMetricValue(queueQuotaMemory, labels, 2000000)
		expectMetricValue(queueAllocatedGpus, labels, 0)
		expectMetricValue(queueAllocatedCpus, labels, 0)
		expectMetricValue(queueAllocatedMemory, labels, 0)
	})

	It("should delete metrics when queue is deleted", func() {
		queue = &v2.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-queue",
				Labels: map[string]string{"priority": "high"},
			},
			Spec: v2.QueueSpec{
				Resources: &v2.QueueResources{
					GPU:    v2.QueueResource{Quota: 2},
					CPU:    v2.QueueResource{Quota: 2000},
					Memory: v2.QueueResource{Quota: 4},
				},
			},
			Status: v2.QueueStatus{
				Allocated: map[v1.ResourceName]resource.Quantity{
					"nvidia.com/gpu":  resource.MustParse("1"),
					v1.ResourceCPU:    resource.MustParse("1000m"),
					v1.ResourceMemory: resource.MustParse("2Gi"),
				},
			},
		}
		SetQueueMetrics(queue)
		ResetQueueMetrics("test-queue")

		// After deletion, all metrics should not exist (should return 0)
		gathered := testutil.CollectAndCount(queueInfo)
		Expect(gathered).To(Equal(0))
		gathered = testutil.CollectAndCount(queueDeservedGPUs)
		Expect(gathered).To(Equal(0))
		gathered = testutil.CollectAndCount(queueQuotaCPU)
		Expect(gathered).To(Equal(0))
		gathered = testutil.CollectAndCount(queueQuotaMemory)
		Expect(gathered).To(Equal(0))
		gathered = testutil.CollectAndCount(queueAllocatedGpus)
		Expect(gathered).To(Equal(0))
		gathered = testutil.CollectAndCount(queueAllocatedCpus)
		Expect(gathered).To(Equal(0))
		gathered = testutil.CollectAndCount(queueAllocatedMemory)
		Expect(gathered).To(Equal(0))
	})
})

func expectMetricValue(gauge *prometheus.GaugeVec, labels []string, expected float64) {
	metricGauge, err := gauge.GetMetricWithLabelValues(labels...)
	Expect(err).To(BeNil())
	Expect(metricGauge).ToNot(BeNil())
	Expect(testutil.ToFloat64(metricGauge)).To(BeEquivalentTo(expected))
}
