// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"sync"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
)

// failedSearches remembers, per action and job, the scenario searches that found no solution, so
// that the action skips the job until its backoff ends or the job changes.
type failedSearches struct {
	mutex   sync.Mutex
	records map[failedSearchKey]*failedSearch
	now     func() time.Time
}

type failedSearchKey struct {
	action string
	job    common_info.PodGroupID
}

type failedSearch struct {
	shape    string
	failures int
	until    time.Time
}

func newFailedSearches(now func() time.Time) *failedSearches {
	return &failedSearches{records: map[failedSearchKey]*failedSearch{}, now: now}
}

// record starts or extends the job's backoff for action: minBackoff after its first failed search,
// doubled after each further one, up to maxBackoff. A job whose search shape changed starts over.
func (fs *failedSearches) record(action string, job *podgroup_info.PodGroupInfo, minBackoff, maxBackoff time.Duration) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	key := failedSearchKey{action: action, job: job.UID}
	shape := job.GetSearchShapeKey()
	search, found := fs.records[key]
	if !found || search.shape != shape {
		search = &failedSearch{shape: shape}
		fs.records[key] = search
	}
	backoff := minBackoff
	for range search.failures {
		// 2*backoff > maxBackoff, without overflowing.
		if backoff > maxBackoff-backoff {
			backoff = maxBackoff
			break
		}
		backoff *= 2
	}
	search.failures++
	search.until = fs.now().Add(backoff)
}

func (fs *failedSearches) clear(action string, job common_info.PodGroupID) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	delete(fs.records, failedSearchKey{action: action, job: job})
}

// mark sets SearchBackoffUntil on the jobs still in a backoff, and forgets the jobs that left the
// cluster or changed shape.
func (fs *failedSearches) mark(podGroups map[common_info.PodGroupID]*podgroup_info.PodGroupInfo) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	now := fs.now()
	for key, search := range fs.records {
		job, found := podGroups[key.job]
		if !found || job.GetSearchShapeKey() != search.shape {
			delete(fs.records, key)
			continue
		}
		if now.Before(search.until) {
			if job.SearchBackoffUntil == nil {
				job.SearchBackoffUntil = map[string]time.Time{}
			}
			job.SearchBackoffUntil[key.action] = search.until
		}
	}
}
