// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"math"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

const (
	reclaim = "reclaim"
	preempt = "preempt"
)

func TestFailedSearchesBackoff(t *testing.T) {
	start := time.Unix(1000, 0)
	tests := []struct {
		name                   string
		minBackoff, maxBackoff time.Duration
		want                   []time.Duration
	}{
		{"doubles up to the maximum", 10 * time.Second, time.Minute,
			[]time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, time.Minute, time.Minute}},
		{"stays fixed when the maximum is the minimum", 10 * time.Second, 10 * time.Second,
			[]time.Duration{10 * time.Second, 10 * time.Second, 10 * time.Second}},
		{"stops at a maximum between two doublings", 10 * time.Second, 25 * time.Second,
			[]time.Duration{10 * time.Second, 20 * time.Second, 25 * time.Second, 25 * time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFailedSearches(func() time.Time { return start })
			for i, want := range tt.want {
				fs.record(reclaim, testJob("job", "queue", "1"), tt.minBackoff, tt.maxBackoff)
				if got := snapshotJob(fs, testJob("job", "queue", "1")).SearchBackoffUntil[reclaim]; !got.Equal(start.Add(want)) {
					t.Errorf("failed search %d: backoff until %v, want %v", i+1, got, start.Add(want))
				}
			}
		})
	}

	t.Run("reaches a maximum too large to double into without overflowing", func(t *testing.T) {
		fs := newFailedSearches(func() time.Time { return start })
		for range 64 {
			fs.record(reclaim, testJob("job", "queue", "1"), time.Hour, math.MaxInt64)
		}
		if got := snapshotJob(fs, testJob("job", "queue", "1")).SearchBackoffUntil[reclaim]; !got.Equal(start.Add(math.MaxInt64)) {
			t.Errorf("backoff until %v, want %v", got, start.Add(math.MaxInt64))
		}
	})
}

func TestFailedSearchesMark(t *testing.T) {
	now := time.Unix(1000, 0)
	clock := func() time.Time { return now }

	t.Run("marks only the action that failed, while the backoff lasts", func(t *testing.T) {
		now = time.Unix(1000, 0)
		fs := newFailedSearches(clock)
		fs.record(reclaim, testJob("job", "queue", "1"), 10*time.Second, time.Minute)

		job := snapshotJob(fs, testJob("job", "queue", "1"))
		if _, found := job.SearchBackoffUntil[reclaim]; !found {
			t.Errorf("job not in backoff for %s", reclaim)
		}
		if _, found := job.SearchBackoffUntil[preempt]; found {
			t.Errorf("job in backoff for %s, which found no failed search", preempt)
		}

		now = now.Add(10 * time.Second)
		if job := snapshotJob(fs, testJob("job", "queue", "1")); len(job.SearchBackoffUntil) != 0 {
			t.Errorf("job still in backoff when it ends: %v", job.SearchBackoffUntil)
		}
	})

	t.Run("an ended backoff still doubles on the next failed search", func(t *testing.T) {
		now = time.Unix(1000, 0)
		fs := newFailedSearches(clock)
		fs.record(reclaim, testJob("job", "queue", "1"), 10*time.Second, time.Minute)
		now = now.Add(time.Hour)
		snapshotJob(fs, testJob("job", "queue", "1"))

		fs.record(reclaim, testJob("job", "queue", "1"), 10*time.Second, time.Minute)
		if got := snapshotJob(fs, testJob("job", "queue", "1")).SearchBackoffUntil[reclaim]; !got.Equal(now.Add(20 * time.Second)) {
			t.Errorf("backoff until %v, want %v", got, now.Add(20*time.Second))
		}
	})

	changes := map[string]*podgroup_info.PodGroupInfo{
		"another pending pod":                testJob("job", "queue", "1", "1"),
		"a pending pod with another request": testJob("job", "queue", "2"),
		"another queue":                      testJob("job", "other-queue", "1"),
	}
	for change, changed := range changes {
		t.Run("a job with "+change+" starts over", func(t *testing.T) {
			now = time.Unix(1000, 0)
			fs := newFailedSearches(clock)
			fs.record(reclaim, testJob("job", "queue", "1"), 10*time.Second, time.Minute)
			fs.record(reclaim, testJob("job", "queue", "1"), 10*time.Second, time.Minute)

			if job := snapshotJob(fs, changed); len(job.SearchBackoffUntil) != 0 {
				t.Errorf("changed job still in backoff: %v", job.SearchBackoffUntil)
			}
			if job := snapshotJob(fs, testJob("job", "queue", "1")); len(job.SearchBackoffUntil) != 0 {
				t.Errorf("backoff of the job's former shape kept: %v", job.SearchBackoffUntil)
			}
			fs.record(reclaim, changed, 10*time.Second, time.Minute)
			if got := snapshotJob(fs, changed).SearchBackoffUntil[reclaim]; !got.Equal(now.Add(10 * time.Second)) {
				t.Errorf("backoff until %v, want the minimum %v", got, now.Add(10*time.Second))
			}
		})
	}

	t.Run("a failed search for a changed job starts over without a snapshot in between", func(t *testing.T) {
		now = time.Unix(1000, 0)
		fs := newFailedSearches(clock)
		fs.record(reclaim, testJob("job", "queue", "1", "1"), 10*time.Second, time.Minute)
		fs.record(reclaim, testJob("job", "queue", "1", "1"), 10*time.Second, time.Minute)

		fs.record(reclaim, testJob("job", "queue", "1"), 10*time.Second, time.Minute)
		if got := snapshotJob(fs, testJob("job", "queue", "1")).SearchBackoffUntil[reclaim]; !got.Equal(now.Add(10 * time.Second)) {
			t.Errorf("backoff until %v, want the minimum %v", got, now.Add(10*time.Second))
		}
	})

	t.Run("a job that left the cluster is forgotten", func(t *testing.T) {
		now = time.Unix(1000, 0)
		fs := newFailedSearches(clock)
		fs.record(reclaim, testJob("job", "queue", "1"), 10*time.Second, time.Minute)

		fs.mark(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{})
		if job := snapshotJob(fs, testJob("job", "queue", "1")); len(job.SearchBackoffUntil) != 0 {
			t.Errorf("job back in the cluster still in backoff: %v", job.SearchBackoffUntil)
		}
	})

	t.Run("a cleared job starts over", func(t *testing.T) {
		now = time.Unix(1000, 0)
		fs := newFailedSearches(clock)
		fs.record(reclaim, testJob("job", "queue", "1"), 10*time.Second, time.Minute)
		fs.record(reclaim, testJob("job", "queue", "1"), 10*time.Second, time.Minute)
		fs.record(preempt, testJob("job", "queue", "1"), 10*time.Second, time.Minute)

		fs.clear(reclaim, testJob("job", "queue", "1").UID)
		job := snapshotJob(fs, testJob("job", "queue", "1"))
		if _, found := job.SearchBackoffUntil[reclaim]; found {
			t.Errorf("cleared job still in backoff for %s", reclaim)
		}
		if _, found := job.SearchBackoffUntil[preempt]; !found {
			t.Errorf("clearing %s ended the backoff for %s", reclaim, preempt)
		}
		fs.record(reclaim, testJob("job", "queue", "1"), 10*time.Second, time.Minute)
		if got := snapshotJob(fs, testJob("job", "queue", "1")).SearchBackoffUntil[reclaim]; !got.Equal(now.Add(10 * time.Second)) {
			t.Errorf("backoff until %v, want the minimum %v", got, now.Add(10*time.Second))
		}
	})
}

// snapshotJob marks job as a new snapshot holding only it would, and returns it.
func snapshotJob(fs *failedSearches, job *podgroup_info.PodGroupInfo) *podgroup_info.PodGroupInfo {
	fs.mark(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{job.UID: job})
	return job
}

// testJob returns a job of queue with one pending pod per GPU request.
func testJob(name, queue string, gpuRequests ...string) *podgroup_info.PodGroupInfo {
	var tasks []*pod_info.PodInfo
	for i, gpus := range gpuRequests {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("%s-%d", name, i),
				Namespace:   "ns",
				UID:         types.UID(fmt.Sprintf("%s-%d", name, i)),
				Annotations: map[string]string{commonconstants.PodGroupAnnotationForPod: name},
			},
			Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{resource_info.GPUResourceName: resource.MustParse(gpus)},
				Limits:   v1.ResourceList{resource_info.GPUResourceName: resource.MustParse(gpus)},
			}}}},
			Status: v1.PodStatus{Phase: v1.PodPending},
		}
		tasks = append(tasks, pod_info.NewTaskInfo(pod, resource_info.NewResourceVectorMap()))
	}
	job := podgroup_info.NewPodGroupInfo(common_info.PodGroupID(name), tasks...)
	job.Queue = common_info.QueueID(queue)
	job.PodGroupUID = types.UID(name)
	return job
}
