// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package env_tests

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/env-tests/timeaware"
)

var _ = Describe("Time Aware Fairness", Ordered, func() {
	BeforeAll(func() {
		runtests, err := strconv.ParseBool(os.Getenv("RUN_TIME_AWARE_TESTS"))
		if err != nil {
			Skip(fmt.Sprintf("Failed to parse RUN_TIME_AWARE_TESTS environment variable: %v", err))
		}
		if !runtests {
			Skip("Skipping time aware fairness tests (RUN_TIME_AWARE_TESTS is not set to true)")
		}
	})

	It("Should run simulation", func(ctx context.Context) {
		allocationHistory, err, cleanupError := timeaware.RunSimulation(ctx, ctrlClient, cfg, timeaware.TimeAwareSimulation{
			Queues: []timeaware.TestQueue{
				{Name: "test-department", Parent: ""},
				{Name: "test-queue1", Parent: "test-department"},
				{Name: "test-queue2", Parent: "test-department"},
			},
			Jobs: map[string]timeaware.TestJobs{
				"test-queue1": {NumPods: 1, NumJobs: 100, GPUs: 4},
				"test-queue2": {NumPods: 1, NumJobs: 100, GPUs: 4},
			},
			Nodes: []timeaware.TestNodes{
				{GPUs: 4, Count: 1},
			},
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to run simulation")
		Expect(cleanupError).NotTo(HaveOccurred(), "Failed to cleanup simulation")

		filename := fmt.Sprintf("allocation_history_%s.csv", time.Now().Format("20060102150405"))
		err = os.WriteFile(filename, []byte(allocationHistory.ToCSV()), 0644)
		Expect(err).NotTo(HaveOccurred(), "Failed to write allocation history to file")
	})

	It("Should run a longer simulation", func(ctx context.Context) {
		allocationHistory, err, cleanupError := timeaware.RunSimulation(ctx, ctrlClient, cfg, timeaware.TimeAwareSimulation{
			Queues: []timeaware.TestQueue{
				{Name: "test-department", Parent: ""},
				{Name: "test-queue1", Parent: "test-department"},
				{Name: "test-queue2", Parent: "test-department"},
			},
			Jobs: map[string]timeaware.TestJobs{
				"test-queue1": {NumPods: 1, NumJobs: 100, GPUs: 16},
				"test-queue2": {NumPods: 1, NumJobs: 100, GPUs: 16},
			},
			Nodes: []timeaware.TestNodes{
				{GPUs: 16, Count: 1},
			},
			WindowSize:     ptr.To(256),
			HalfLifePeriod: ptr.To(128),
			Cycles:         ptr.To(1024),
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to run simulation")
		Expect(cleanupError).NotTo(HaveOccurred(), "Failed to cleanup simulation")

		filename := "allocation_history_oscillating.csv"
		err = os.WriteFile(filename, []byte(allocationHistory.ToCSV()), 0644)
		Expect(err).NotTo(HaveOccurred(), "Failed to write allocation history to file")
	})

	It("Should allow burst use cases", func(ctx context.Context) {
		allocationHistory, err, cleanupError := timeaware.RunSimulation(ctx, ctrlClient, cfg, timeaware.TimeAwareSimulation{
			Queues: []timeaware.TestQueue{
				{Name: "test-department", Parent: ""},
				{Name: "test-queue1", Parent: "test-department"},
				{Name: "test-queue2", Parent: "test-department"},
				{Name: "test-queue-burst", Parent: "test-department"},
			},
			Jobs: map[string]timeaware.TestJobs{
				"test-queue1":      {NumPods: 1, NumJobs: 1000, GPUs: 1},
				"test-queue2":      {NumPods: 1, NumJobs: 1000, GPUs: 1},
				"test-queue-burst": {NumPods: 1, NumJobs: 1000, GPUs: 12},
			},
			Nodes: []timeaware.TestNodes{
				{GPUs: 16, Count: 1},
			},
			Cycles:     ptr.To(1024),
			WindowSize: ptr.To(256),
			KValue:     ptr.To(1.0),
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to run simulation")
		Expect(cleanupError).NotTo(HaveOccurred(), "Failed to cleanup simulation")

		filename := "allocation_history_burst.csv"
		err = os.WriteFile(filename, []byte(allocationHistory.ToCSV()), 0644)
		Expect(err).NotTo(HaveOccurred(), "Failed to write allocation history to file")
	})

	It("3-Way oscillation between queues", func(ctx context.Context) {
		allocationHistory, err, cleanupError := timeaware.RunSimulation(ctx, ctrlClient, cfg, timeaware.TimeAwareSimulation{
			Queues: []timeaware.TestQueue{
				{Name: "test-department", Parent: ""},
				{Name: "test-queue1", Parent: "test-department"},
				{Name: "test-queue2", Parent: "test-department"},
				{Name: "test-queue3", Parent: "test-department"},
			},
			Jobs: map[string]timeaware.TestJobs{
				"test-queue1": {NumPods: 1, NumJobs: 1000, GPUs: 16},
				"test-queue2": {NumPods: 1, NumJobs: 1000, GPUs: 16},
				"test-queue3": {NumPods: 1, NumJobs: 1000, GPUs: 16},
			},
			Nodes: []timeaware.TestNodes{
				{GPUs: 16, Count: 1},
			},
			Cycles:     ptr.To(1024),
			WindowSize: ptr.To(256),
			KValue:     ptr.To(10.0),
		})
		Expect(err).NotTo(HaveOccurred(), "Failed to run simulation")
		Expect(cleanupError).NotTo(HaveOccurred(), "Failed to cleanup simulation")

		filename := "allocation_history_3-way-oscillation.csv"
		err = os.WriteFile(filename, []byte(allocationHistory.ToCSV()), 0644)
		Expect(err).NotTo(HaveOccurred(), "Failed to write allocation history to file")
	})
})
