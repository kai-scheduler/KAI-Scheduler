// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package minruntime

import (
	"time"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/queue_info"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("MinRuntime Resolver", func() {
	var (
		queues                 map[common_info.QueueID]*queue_info.QueueInfo
		resolver               *resolver
		defaultPreemptDuration metav1.Duration
		defaultReclaimDuration metav1.Duration
	)

	BeforeEach(func() {
		queues = createTestQueues()
		defaultPreemptDuration = metav1.Duration{Duration: 2 * time.Second}
		defaultReclaimDuration = metav1.Duration{Duration: 1 * time.Second}

		resolver = NewResolver(queues, defaultPreemptDuration, defaultReclaimDuration)
	})

	AfterEach(func() {
		// Reset the caches after each test
		resolver.preemptMinRuntimeCache = make(map[common_info.QueueID]metav1.Duration)
		resolver.reclaimMinRuntimeCache = make(map[common_info.QueueID]map[common_info.QueueID]metav1.Duration)
	})

	Describe("getPreemptMinRuntime", func() {
		Context("when queue has a set value (prod-team2)", func() {
			It("should return the queue's value", func() {
				result, err := resolver.getPreemptMinRuntime(queues["prod-team2"])
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(metav1.Duration{Duration: 15 * time.Second}))
			})
		})

		Context("when queue inherits from parent (prod-team1 from prod)", func() {
			It("should return the parent's value", func() {
				result, err := resolver.getPreemptMinRuntime(queues["prod-team1"])
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(metav1.Duration{Duration: 20 * time.Second}))
			})
		})

		Context("when queue inherits from parent (dev-team1 from dev)", func() {
			It("should return the parent's value", func() {
				result, err := resolver.getPreemptMinRuntime(queues["dev-team1"])
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(metav1.Duration{Duration: 5 * time.Second}))
			})
		})

		Context("when top-level queue has a set value (prod)", func() {
			It("should return the queue's value", func() {
				result, err := resolver.getPreemptMinRuntime(queues["prod"])
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(metav1.Duration{Duration: 20 * time.Second}))
			})
		})

		Context("when research queue has a set value", func() {
			It("should return the queue's value", func() {
				result, err := resolver.getPreemptMinRuntime(queues["research"])
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(metav1.Duration{Duration: 4 * time.Second}))
			})
		})

		Context("when no queue is found", func() {
			It("should return the default value", func() {
				nonexistentQueue := &queue_info.QueueInfo{UID: "nonexistent", ParentQueue: "also-nonexistent"}
				result, err := resolver.getPreemptMinRuntime(nonexistentQueue)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(defaultPreemptDuration))
			})
		})

		Context("when queue is nil", func() {
			It("should return an error and the default value", func() {
				result, err := resolver.getPreemptMinRuntime(nil)
				Expect(err).To(HaveOccurred())
				Expect(result).To(Equal(defaultPreemptDuration))
			})
		})
	})

	Describe("getReclaimMinRuntime with queue method", func() {
		Context("when victim queue has a set value (pendingJob:dev-team1, victim:prod-team2)", func() {
			It("should return the victim queue's value", func() {
				result, err := resolver.getReclaimMinRuntime(
					resolveMethodQueue,
					queues["dev-team1"],
					queues["prod-team2"],
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(metav1.Duration{Duration: 35 * time.Second}))
			})
		})

		Context("when victim queue inherits from parent (pendingJob:dev-team1, victim:dev-team2)", func() {
			It("should return the victim's parent value", func() {
				result, err := resolver.getReclaimMinRuntime(
					resolveMethodQueue,
					queues["dev-team1"],
					queues["dev-team2"],
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(metav1.Duration{Duration: 10 * time.Second}))
			})
		})

		Context("when victim queue is not found (pendingJob:dev-team1, victim:nonexistent)", func() {
			It("should return the default value", func() {
				nonexistentQueue := &queue_info.QueueInfo{UID: "nonexistent", ParentQueue: "also-nonexistent"}
				result, err := resolver.getReclaimMinRuntime(
					resolveMethodQueue,
					queues["dev-team1"],
					nonexistentQueue,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(defaultReclaimDuration))
			})
		})

		Context("when queue is nil", func() {
			It("should return an error and the default value", func() {
				result, err := resolver.getReclaimMinRuntime(
					resolveMethodQueue,
					nil,
					queues["dev-team2"],
				)
				Expect(err).To(HaveOccurred())
				Expect(result).To(Equal(defaultReclaimDuration))
			})
		})
	})

	Describe("getReclaimMinRuntimeLCA", func() {
		Context("when pendingJob and victim are in different top-level queues (pendingJob:dev-team1, victim:prod-team2)", func() {
			It("should use the top-level victim queue's value", func() {
				result, err := resolver.resolveReclaimMinRuntimeLCA(
					queues["dev-team1"],
					queues["prod-team2"],
				)
				Expect(err).NotTo(HaveOccurred())
				// In this case, the top-level ancestor's value will be used (prod's value is 30s)
				Expect(result).To(Equal(metav1.Duration{Duration: 30 * time.Second}))
			})
		})

		Context("when LCA is dev and victim is dev-team1 with ReclaimMinRuntime set (pendingJob:dev-team2, victim:dev-team1)", func() {
			It("should return the victim's value", func() {
				result, err := resolver.resolveReclaimMinRuntimeLCA(
					queues["dev-team2"],
					queues["dev-team1"],
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Duration).To(BeNumerically("==", 8*time.Second))
			})
		})

		Context("when LCA is prod and victim is prod-team2 with ReclaimMinRuntime set (pendingJob:prod-team1, victim:prod-team2)", func() {
			It("should return the victim's value", func() {
				result, err := resolver.resolveReclaimMinRuntimeLCA(
					queues["prod-team1"],
					queues["prod-team2"],
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(metav1.Duration{Duration: 35 * time.Second}))
			})
		})

		Context("when pendingJob and victim are the same queue (pendingJob:prod-team2, victim:prod-team2)", func() {
			It("should return the queue's own value", func() {
				result, err := resolver.resolveReclaimMinRuntimeLCA(
					queues["prod-team2"],
					queues["prod-team2"],
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(metav1.Duration{Duration: 35 * time.Second}))
			})
		})

		Context("when research queue is victim (pendingJob:dev-team1, victim:research-project)", func() {
			It("should use research queue's value from the top-level ancestor", func() {
				result, err := resolver.resolveReclaimMinRuntimeLCA(
					queues["dev-team1"],
					queues["research-project"],
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(metav1.Duration{Duration: 6 * time.Second}))
			})
		})

		Context("when queue is nil", func() {
			It("should return an error and the default value", func() {
				result, err := resolver.resolveReclaimMinRuntimeLCA(nil, nil)
				Expect(err).To(HaveOccurred())
				Expect(result).To(Equal(defaultReclaimDuration))
			})
		})
	})

	Describe("getQueueHierarchyPath", func() {
		It("should return correct path for top-level queue (dev)", func() {
			result := resolver.getQueueHierarchyPath(queues["dev"])
			resultUIDs := extractUIDs(result)
			Expect(resultUIDs).To(Equal([]common_info.QueueID{"dev"}))
		})

		It("should return correct path for leaf queue (dev-team1)", func() {
			result := resolver.getQueueHierarchyPath(queues["dev-team1"])
			resultUIDs := extractUIDs(result)
			Expect(resultUIDs).To(Equal([]common_info.QueueID{"dev", "dev-team1"}))
		})

		It("should return correct path for different branch leaf queue (prod-team2)", func() {
			result := resolver.getQueueHierarchyPath(queues["prod-team2"])
			resultUIDs := extractUIDs(result)
			Expect(resultUIDs).To(Equal([]common_info.QueueID{"prod", "prod-team2"}))
		})

		It("should return correct path for research project queue", func() {
			result := resolver.getQueueHierarchyPath(queues["research-project"])
			resultUIDs := extractUIDs(result)
			Expect(resultUIDs).To(Equal([]common_info.QueueID{"research", "research-project"}))
		})

		It("should handle queues with non-existent parent", func() {
			orphanQueue := &queue_info.QueueInfo{UID: "orphan", ParentQueue: "nonexistent"}
			result := resolver.getQueueHierarchyPath(orphanQueue)
			resultUIDs := extractUIDs(result)
			Expect(resultUIDs).To(Equal([]common_info.QueueID{"orphan"}))
		})
	})

	Describe("getReclaimMinRuntime with both methods", func() {
		It("should return different values for different methods (pendingJob:dev-team1, victim:prod-team2)", func() {
			preemptorQueue := queues["dev-team1"]
			preempteeQueue := queues["prod-team2"]

			queueResult, queueErr := resolver.resolveReclaimMinRuntimeQueue(preemptorQueue, preempteeQueue)
			Expect(queueErr).NotTo(HaveOccurred())
			Expect(queueResult).To(Equal(metav1.Duration{Duration: 35 * time.Second}))

			lcaResult, lcaErr := resolver.resolveReclaimMinRuntimeLCA(preemptorQueue, preempteeQueue)
			Expect(lcaErr).NotTo(HaveOccurred())
			// Should use prod's value (30s) since it's the top-level ancestor in the shadow parent tree
			Expect(lcaResult).To(Equal(metav1.Duration{Duration: 30 * time.Second}))
		})

		It("should handle different hierarchies with the research queue (pendingJob:dev-team1, victim:research-project)", func() {
			preemptorQueue := queues["dev-team1"]
			preempteeQueue := queues["research-project"]

			queueResult, queueErr := resolver.resolveReclaimMinRuntimeQueue(preemptorQueue, preempteeQueue)
			Expect(queueErr).NotTo(HaveOccurred())
			Expect(queueResult).To(Equal(metav1.Duration{Duration: 9 * time.Second}))

			lcaResult, lcaErr := resolver.resolveReclaimMinRuntimeLCA(preemptorQueue, preempteeQueue)
			Expect(lcaErr).NotTo(HaveOccurred())
			Expect(lcaResult).To(Equal(metav1.Duration{Duration: 6 * time.Second}))
		})
	})

	Describe("Edge cases", func() {
		Context("with nil queue", func() {
			It("should handle nil queue gracefully", func() {
				preemptResult, preemptErr := resolver.getPreemptMinRuntime(nil)
				Expect(preemptErr).To(HaveOccurred())
				Expect(preemptResult).To(Equal(defaultPreemptDuration))

				reclaimResult, reclaimErr := resolver.resolveReclaimMinRuntimeLCA(nil, nil)
				Expect(reclaimErr).To(HaveOccurred())
				Expect(reclaimResult).To(Equal(defaultReclaimDuration))
			})
		})

		Context("with orphaned victim queue (pendingJob:dev-team1, victim:orphan)", func() {
			It("should use resolver default value when ancestor is missing", func() {
				orphanQueue := &queue_info.QueueInfo{
					UID:               "orphan",
					ParentQueue:       "nonexistent",
					ReclaimMinRuntime: &metav1.Duration{Duration: 7 * time.Second},
				}

				result, err := resolver.resolveReclaimMinRuntimeLCA(queues["dev-team1"], orphanQueue)
				Expect(err).NotTo(HaveOccurred())
				// Current implementation should use the orphan queue's value since it has one set
				Expect(result.Duration).To(BeNumerically("==", 7*time.Second))
			})
		})

		Context("with top-level victim queue having no reclaim min runtime (pendingJob:dev-team1, victim:leaf)", func() {
			It("should fall back to default value", func() {
				// Create a top-level queue with no reclaim min runtime
				noReclaimQueue := &queue_info.QueueInfo{
					UID:               "no-reclaim",
					ParentQueue:       "",
					ReclaimMinRuntime: nil,
				}

				// Create a leaf queue under this top-level queue
				leafQueue := &queue_info.QueueInfo{
					UID:               "leaf",
					ParentQueue:       "no-reclaim",
					ReclaimMinRuntime: nil,
				}

				// Add these queues to a test resolver
				testQueues := map[common_info.QueueID]*queue_info.QueueInfo{
					"no-reclaim": noReclaimQueue,
					"leaf":       leafQueue,
				}

				resolver := NewResolver(testQueues, defaultPreemptDuration, defaultReclaimDuration)

				// Test LCA between queue from different hierarchies
				result, err := resolver.resolveReclaimMinRuntimeLCA(queues["dev-team1"], leafQueue)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(defaultReclaimDuration))
			})
		})
	})
})

// Helper function to extract UIDs from queue hierarchy path
func extractUIDs(queuePath []*queue_info.QueueInfo) []common_info.QueueID {
	resultUIDs := make([]common_info.QueueID, len(queuePath))
	for i, q := range queuePath {
		resultUIDs[i] = q.UID
	}
	return resultUIDs
}

func createTestQueues() map[common_info.QueueID]*queue_info.QueueInfo {
	dev := &queue_info.QueueInfo{
		UID:               "dev",
		Name:              "Development Queue",
		ParentQueue:       "",
		ChildQueues:       []common_info.QueueID{"dev-team1", "dev-team2"},
		PreemptMinRuntime: &metav1.Duration{Duration: 5 * time.Second},
		ReclaimMinRuntime: &metav1.Duration{Duration: 10 * time.Second}, // Adding reclaim min runtime
	}

	prod := &queue_info.QueueInfo{
		UID:               "prod",
		Name:              "Production Queue",
		ParentQueue:       "",
		ChildQueues:       []common_info.QueueID{"prod-team1", "prod-team2"},
		PreemptMinRuntime: &metav1.Duration{Duration: 20 * time.Second},
		ReclaimMinRuntime: &metav1.Duration{Duration: 30 * time.Second},
	}

	// Add a third queue for research
	research := &queue_info.QueueInfo{
		UID:               "research",
		Name:              "Research Queue",
		ParentQueue:       "",
		ChildQueues:       []common_info.QueueID{"research-project"},
		PreemptMinRuntime: &metav1.Duration{Duration: 4 * time.Second},
		ReclaimMinRuntime: &metav1.Duration{Duration: 6 * time.Second},
	}

	devTeam1 := &queue_info.QueueInfo{
		UID:               "dev-team1",
		Name:              "Dev Team 1",
		ParentQueue:       "dev",
		ChildQueues:       []common_info.QueueID{},
		PreemptMinRuntime: nil, // Not set, should inherit from parent
		ReclaimMinRuntime: &metav1.Duration{Duration: 8 * time.Second},
	}

	devTeam2 := &queue_info.QueueInfo{
		UID:               "dev-team2",
		Name:              "Dev Team 2",
		ParentQueue:       "dev",
		ChildQueues:       []common_info.QueueID{},
		PreemptMinRuntime: &metav1.Duration{Duration: 3 * time.Second},
		ReclaimMinRuntime: nil, // Not set, should inherit from parent/resolver rules
	}

	prodTeam1 := &queue_info.QueueInfo{
		UID:               "prod-team1",
		Name:              "Prod Team 1",
		ParentQueue:       "prod",
		ChildQueues:       []common_info.QueueID{},
		PreemptMinRuntime: nil, // Not set, should inherit from parent
		ReclaimMinRuntime: &metav1.Duration{Duration: 25 * time.Second},
	}

	prodTeam2 := &queue_info.QueueInfo{
		UID:               "prod-team2",
		Name:              "Prod Team 2",
		ParentQueue:       "prod",
		ChildQueues:       []common_info.QueueID{},
		PreemptMinRuntime: &metav1.Duration{Duration: 15 * time.Second},
		ReclaimMinRuntime: &metav1.Duration{Duration: 35 * time.Second},
	}

	// Add a research project queue
	researchProject := &queue_info.QueueInfo{
		UID:               "research-project",
		Name:              "Research Project",
		ParentQueue:       "research",
		ChildQueues:       []common_info.QueueID{},
		PreemptMinRuntime: &metav1.Duration{Duration: 7 * time.Second},
		ReclaimMinRuntime: &metav1.Duration{Duration: 9 * time.Second},
	}

	return map[common_info.QueueID]*queue_info.QueueInfo{
		"dev":              dev,
		"prod":             prod,
		"research":         research,
		"dev-team1":        devTeam1,
		"dev-team2":        devTeam2,
		"prod-team1":       prodTeam1,
		"prod-team2":       prodTeam2,
		"research-project": researchProject,
	}
}
