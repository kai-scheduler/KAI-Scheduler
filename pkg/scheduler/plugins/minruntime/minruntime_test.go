// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package minruntime

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
)

type TestScenario struct {
	Preemptor *podgroup_info.PodGroupInfo
	Victims   map[common_info.PodGroupID]*api.VictimInfo
}

func (s *TestScenario) GetPreemptor() *podgroup_info.PodGroupInfo {
	return s.Preemptor
}

func (s *TestScenario) GetVictims() map[common_info.PodGroupID]*api.VictimInfo {
	return s.Victims
}

var _ = Describe("MinRuntime Plugin", func() {
	var (
		plugin                 *minruntimePlugin
		queues                 map[common_info.QueueID]*queue_info.QueueInfo
		defaultPreemptDuration metav1.Duration
		defaultReclaimDuration metav1.Duration
	)

	// Helper function to create a PodGroupInfo with a specific last start timestamp
	createPodGroup := func(uid common_info.PodGroupID, queue common_info.QueueID, lastStartTime *time.Time, minAvailable int32, podsCount int) *podgroup_info.PodGroupInfo {
		pg := &podgroup_info.PodGroupInfo{
			UID:   uid,
			Queue: queue,
			PodSets: map[string]*subgroup_info.PodSet{
				podgroup_info.DefaultSubGroup: subgroup_info.NewPodSet(podgroup_info.DefaultSubGroup, minAvailable, nil),
			},
			PodStatusIndex: make(map[pod_status.PodStatus]pod_info.PodsMap),
			TasksFitErrors: make(map[common_info.PodID]*common_info.TasksFitErrors),
		}

		if lastStartTime != nil {
			pg.LastStartTimestamp = lastStartTime
		}

		// Add pods to the pod group
		for i := 0; i < podsCount; i++ {
			podID := common_info.PodID(fmt.Sprintf("%s-pod-%d", uid, i))
			podInfo := &pod_info.PodInfo{
				UID:    podID,
				Job:    uid,
				Status: pod_status.Running,
			}
			pg.PodSets[podgroup_info.DefaultSubGroup].AssignTask(podInfo)

			// Initialize the PodStatusIndex map for this status if it doesn't exist
			if _, found := pg.PodStatusIndex[pod_status.Running]; !found {
				pg.PodStatusIndex[pod_status.Running] = make(pod_info.PodsMap)
			}
			pg.PodStatusIndex[pod_status.Running][podID] = podInfo
		}
		return pg
	}

	BeforeEach(func() {
		// Set up test data
		queues = createTestQueues()
		defaultPreemptDuration = metav1.Duration{Duration: 5 * time.Second}
		defaultReclaimDuration = metav1.Duration{Duration: 3 * time.Second}

		// Initialize the plugin
		plugin = &minruntimePlugin{
			queues:                   queues,
			defaultPreemptMinRuntime: defaultPreemptDuration,
			defaultReclaimMinRuntime: defaultReclaimDuration,
			reclaimResolveMethod:     resolveMethodLCA,
			preemptProtectionCache:   make(map[common_info.PodGroupID]bool),
			reclaimProtectionCache:   make(map[common_info.PodGroupID]map[common_info.PodGroupID]bool),
			resolver:                 NewResolver(queues, defaultPreemptDuration, defaultReclaimDuration),
		}
	})

	Describe("preemptFilterFn", func() {
		Context("when victim is protected by min-runtime", func() {
			It("should return false for non-elastic job", func() {
				// Create a pending job from dev-team1 queue
				pendingJob := createPodGroup("pending-job", "dev-team1", nil, 1, 1)

				// Create a victim job from prod-team2 queue that started recently
				// Using prod-team2 with preempt min runtime of 15s
				now := time.Now()
				recentStart := now.Add(-10 * time.Second) // Started 10 seconds ago
				victim := createPodGroup("victim-job", "prod-team2", &recentStart, 1, 1)

				// The preempt min runtime for prod-team2 is 15s, and the job started 10s ago
				// so it should be protected
				result := plugin.preemptFilterFn(pendingJob, victim)
				Expect(result).To(BeFalse(), "Job 'victim-job' should be protected from preemption")
			})
		})

		Context("when victim is not protected by min-runtime", func() {
			It("should return true if victim started long ago", func() {
				// Create a pending job from dev-team1 queue
				pendingJob := createPodGroup("pending-job", "dev-team1", nil, 1, 1)

				// Create a victim job that started a long time ago
				longAgo := time.Now().Add(-30 * time.Second) // Started 30 seconds ago
				victim := createPodGroup("old-victim", "prod-team2", &longAgo, 1, 1)

				// The preempt min runtime for prod-team2 is 15s, so 30s is past protection period
				result := plugin.preemptFilterFn(pendingJob, victim)
				Expect(result).To(BeTrue(), "Job 'old-victim' should not be protected from preemption")
			})

			It("should return true if victim has no start time", func() {
				// Create a pending job
				pendingJob := createPodGroup("pending-job", "dev-team1", nil, 1, 1)

				// Create a victim job with no start time
				victim := createPodGroup("no-start-victim", "prod-team2", nil, 1, 1)

				result := plugin.preemptFilterFn(pendingJob, victim)
				Expect(result).To(BeTrue(), "Job with no start time should not be protected")
			})
		})
	})

	Describe("reclaimFilterFn", func() {
		Context("when victim is protected by min-runtime", func() {
			It("should return false for non-elastic job", func() {
				// Create a pending job from dev-team1 queue
				pendingJob := createPodGroup("pending-job", "dev-team1", nil, 1, 1)

				// Create a victim job from prod-team2 queue that started recently
				// Using prod-team2 with reclaim min runtime of 35s
				now := time.Now()
				recentStart := now.Add(-20 * time.Second) // Started 20 seconds ago
				victim := createPodGroup("victim-job", "prod-team2", &recentStart, 1, 1)

				// The reclaim min runtime for prod-team2 is 35s, and the job started 20s ago
				// so it should be protected
				result := plugin.reclaimFilterFn(pendingJob, victim)
				Expect(result).To(BeFalse(), "Job 'victim-job' should be protected from reclaim")
			})
		})

		Context("when victim is not protected by min-runtime", func() {
			It("should return true if victim started long ago", func() {
				// Create a pending job from dev-team1 queue
				pendingJob := createPodGroup("pending-job", "dev-team1", nil, 1, 1)

				// Create a victim job that started a long time ago
				longAgo := time.Now().Add(-40 * time.Second) // Started 40 seconds ago
				victim := createPodGroup("old-victim", "prod-team2", &longAgo, 1, 1)

				// The reclaim min runtime for prod-team2 is 35s, so 40s is past protection period
				result := plugin.reclaimFilterFn(pendingJob, victim)
				Expect(result).To(BeTrue(), "Job 'old-victim' should not be protected from reclaim")
			})
		})

		Context("when using different resolve methods", func() {
			It("should use LCA method when configured", func() {
				plugin.reclaimResolveMethod = resolveMethodLCA

				// Create a pending job from dev-team1 queue
				pendingJob := createPodGroup("pending-job", "dev-team1", nil, 1, 1)

				// Create a victim job from prod-team2 queue that started recently
				now := time.Now()
				recentStart := now.Add(-20 * time.Second) // Started 20 seconds ago
				victim := createPodGroup("victim-job", "prod-team2", &recentStart, 1, 1)

				// With LCA method, the reclaim min runtime for different top-level queues
				// is determined by prod's value (30s)
				result := plugin.reclaimFilterFn(pendingJob, victim)
				Expect(result).To(BeFalse(), "Job should be protected with LCA method")
			})

			It("should use queue method when configured", func() {
				plugin.reclaimResolveMethod = resolveMethodQueue

				// Create a pending job from dev-team1 queue
				pendingJob := createPodGroup("pending-job", "dev-team1", nil, 1, 1)

				// Create a victim job from prod-team2 queue that started recently
				now := time.Now()
				recentStart := now.Add(-20 * time.Second) // Started 20 seconds ago
				victim := createPodGroup("victim-job", "prod-team2", &recentStart, 1, 1)

				// With queue method, the reclaim min runtime is prod-team2's value (35s)
				result := plugin.reclaimFilterFn(pendingJob, victim)
				Expect(result).To(BeFalse(), "Job should be protected with queue method")
			})
		})
	})

	Describe("preemptScenarioValidatorFn", func() {
		Context("when validating preemption scenario for elastic jobs", func() {
			It("should return true if not enough tasks are being preempted", func() {
				// Create a pending job from dev-team1 queue
				preemptor := createPodGroup("preemptor-job", "dev-team1", nil, 1, 1)

				// Create a victim job that is elastic (minAvailable = 1, pods = 3)
				now := time.Now()
				recentStart := now.Add(-10 * time.Second) // Started 10 seconds ago
				victim := createPodGroup("victim-job", "prod-team2", &recentStart, 1, 3)

				// Create a list of pods to be preempted - only 1 pod from the victim
				victimPod := &pod_info.PodInfo{
					UID:    "victim-job-pod-0",
					Job:    victim.UID,
					Status: pod_status.Running,
				}
				tasks := []*pod_info.PodInfo{victimPod}

				// Since minAvailable=1 and we have 3 pods total, preempting 1 pod should be fine
				scenario := &TestScenario{
					Preemptor: preemptor,
					Victims:   map[common_info.PodGroupID]*api.VictimInfo{victim.UID: {Job: victim, Tasks: tasks}},
				}
				result := plugin.preemptScenarioValidatorFn(scenario)
				Expect(result).To(BeTrue(), "Should allow preemption of one pod from elastic job")
			})

			It("should return false if too many tasks would be preempted", func() {
				// Create a pending job from dev-team1 queue
				preemptor := createPodGroup("preemptor-job", "dev-team1", nil, 1, 1)

				// Create a victim job that is elastic (minAvailable = 2, pods = 3)
				now := time.Now()
				recentStart := now.Add(-10 * time.Second) // Started 10 seconds ago
				victim := createPodGroup("victim-job", "prod-team2", &recentStart, 2, 3)

				// Create a list of pods to be preempted - 2 pods from the victim
				victimPods := []*pod_info.PodInfo{
					{
						UID:    "victim-job-pod-0",
						Job:    victim.UID,
						Status: pod_status.Running,
					},
					{
						UID:    "victim-job-pod-1",
						Job:    victim.UID,
						Status: pod_status.Running,
					},
				}

				// Since minAvailable=2 and we have 3 pods total, preempting 2 pods shouldn't be allowed
				scenario := &TestScenario{
					Preemptor: preemptor,
					Victims:   map[common_info.PodGroupID]*api.VictimInfo{victim.UID: {Job: victim, Tasks: victimPods}},
				}
				result := plugin.preemptScenarioValidatorFn(scenario)
				Expect(result).To(BeFalse(), "Should not allow preemption of too many pods from elastic job")
			})
		})
	})

	Describe("reclaimScenarioValidatorFn", func() {
		Context("when validating reclaim scenario for elastic jobs", func() {
			It("should return true if not enough tasks are being reclaimed", func() {
				// Create a pending job from dev-team1 queue
				reclaimer := createPodGroup("reclaimer-job", "dev-team1", nil, 1, 1)

				// Create a victim job that is elastic (minAvailable = 1, pods = 3)
				now := time.Now()
				recentStart := now.Add(-20 * time.Second) // Started 20 seconds ago
				victim := createPodGroup("victim-job", "prod-team2", &recentStart, 1, 3)

				// Create a list of pods to be reclaimed - only 1 pod from the victim
				victimPod := &pod_info.PodInfo{
					UID:    "victim-job-pod-0",
					Job:    victim.UID,
					Status: pod_status.Running,
				}
				tasks := []*pod_info.PodInfo{victimPod}

				// Since minAvailable=1 and we have 3 pods total, reclaiming 1 pod should be fine
				scenario := &TestScenario{
					Preemptor: reclaimer,
					Victims:   map[common_info.PodGroupID]*api.VictimInfo{victim.UID: {Job: victim, Tasks: tasks}},
				}
				result := plugin.reclaimScenarioValidatorFn(scenario)
				Expect(result).To(BeTrue(), "Should allow reclaiming of one pod from elastic job")
			})

			It("should return false if too many tasks would be reclaimed", func() {
				// Create a pending job from dev-team1 queue
				reclaimer := createPodGroup("reclaimer-job", "dev-team1", nil, 1, 1)

				// Create a victim job that is elastic (minAvailable = 2, pods = 3)
				now := time.Now()
				recentStart := now.Add(-20 * time.Second) // Started 20 seconds ago
				victim := createPodGroup("victim-job", "prod-team2", &recentStart, 2, 3)

				// First cache this victim as protected by reclaim min runtime
				plugin.reclaimFilterFn(reclaimer, victim)

				// Create a list of pods to be reclaimed - 2 pods from the victim
				victimPods := []*pod_info.PodInfo{
					{
						UID:    "victim-job-pod-0",
						Job:    victim.UID,
						Status: pod_status.Running,
					},
					{
						UID:    "victim-job-pod-1",
						Job:    victim.UID,
						Status: pod_status.Running,
					},
				}

				// Since minAvailable=2 and we have 3 pods total, reclaiming 2 pods shouldn't be allowed
				scenario := &TestScenario{
					Preemptor: reclaimer,
					Victims:   map[common_info.PodGroupID]*api.VictimInfo{victim.UID: {Job: victim, Tasks: victimPods}},
				}
				result := plugin.reclaimScenarioValidatorFn(scenario)
				Expect(result).To(BeFalse(), "Should not allow reclaim of too many pods from elastic job")
			})
		})
	})

	Describe("parseMinRuntime", func() {
		It("Sanity - parse passes", func() {
			// Test various valid min runtime strings
			validDurations := []string{
				"5s", "10s", "10m5s", "1m", "1.5s", "2m30s", "1h", "1h30m", "2h45m15s", "2d4h30m", "5w4d12h",
			}

			args := framework.PluginArguments{}

			for _, durationStr := range validDurations {
				args[defaultReclaimMinRuntimeConfig] = durationStr
				duration := parseMinRuntime(args, defaultReclaimMinRuntimeConfig)
				Expect(duration).ToNot(BeZero(), fmt.Sprintf("Expected non-zero duration for %s", durationStr))
			}
		})

		It("Invalid durations should return zero", func() {
			// Test various invalid min runtime strings
			invalidDurations := []string{
				"5", "1h2", "2h45m15", "abc", "1h-30m", "dfdsfdfdf",
			}

			args := framework.PluginArguments{}

			for _, durationStr := range invalidDurations {
				args[defaultPreemptMinRuntimeConfig] = durationStr
				duration := parseMinRuntime(args, defaultPreemptMinRuntimeConfig)
				Expect(duration).To(BeZero(), fmt.Sprintf("Expected zero duration for invalid %s", durationStr))
			}
		})

		It("Invalid negative durations", func() {
			// Test negative durations
			negativeDurations := []string{
				"-5s", "-10m", "-1h", "-2d", "-3w",
			}

			args := framework.PluginArguments{}

			for _, durationStr := range negativeDurations {
				args[defaultReclaimMinRuntimeConfig] = durationStr
				duration := parseMinRuntime(args, defaultReclaimMinRuntimeConfig)
				Expect(duration).To(BeZero(), fmt.Sprintf("Expected zero duration for negative %s", durationStr))
			}
		})
	})
})
