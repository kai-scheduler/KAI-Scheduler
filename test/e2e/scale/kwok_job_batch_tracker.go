// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

const batchProgressLogInterval = 30 * time.Second

const (
	watchRestartInitialBackoff = time.Second
	watchRestartMaxBackoff     = 30 * time.Second
)

// BatchStatus is the aggregate state of one scale-test Job batch.
type BatchStatus struct {
	ExpectedPods      int
	ExpectedPodGroups int
	ObservedPods      int
	ObservedPodGroups int
	ScheduledPods     int
	RunningPods       int
	SucceededPods     int
	PendingPods       int
	LastScheduledAt   time.Time
}

// PodGroupConditionTiming identifies when a PodGroup and one of its conditions appeared.
type PodGroupConditionTiming struct {
	CreatedAt    time.Time
	TransitionAt time.Time
}

type podState struct {
	scheduled   bool
	running     bool
	succeeded   bool
	pending     bool
	scheduledAt time.Time
}

type podGroupState struct {
	createdAt  time.Time
	conditions map[v2alpha2.SchedulingConditionType]time.Time
}

type jobBatchTracker struct {
	client            runtimeClient.WithWatch
	namespace         string
	batchID           string
	batchLabels       map[string]string
	expectedPods      int
	expectedPodGroups int

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu            sync.Mutex
	pods          map[types.UID]podState
	podGroups     map[types.UID]podGroupState
	jobs          map[runtimeClient.ObjectKey]*batchv1.Job
	scheduledPods int
	runningPods   int
	succeededPods int
	pendingPods   int
	lastScheduled time.Time
	changed       chan struct{}
	terminalError error
}

func newJobBatchTracker(
	ctx context.Context,
	client runtimeClient.WithWatch,
	namespace string,
	batchID string,
	batchLabels map[string]string,
	expectedPods, expectedPodGroups int,
) (*jobBatchTracker, error) {
	trackerCtx, cancel := context.WithCancel(ctx)
	t := &jobBatchTracker{
		client:            client,
		namespace:         namespace,
		batchID:           batchID,
		batchLabels:       batchLabels,
		expectedPods:      expectedPods,
		expectedPodGroups: expectedPodGroups,
		ctx:               trackerCtx,
		cancel:            cancel,
		pods:              make(map[types.UID]podState),
		podGroups:         make(map[types.UID]podGroupState),
		jobs:              make(map[runtimeClient.ObjectKey]*batchv1.Job),
		changed:           make(chan struct{}),
	}

	podWatch, err := t.watchAndListPods()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("watch batch Pods: %w", err)
	}
	podGroupWatch, err := t.watchAndListPodGroups()
	if err != nil {
		podWatch.Stop()
		cancel()
		return nil, fmt.Errorf("watch batch PodGroups: %w", err)
	}

	t.wg.Add(2)
	go t.runWatch("Pods", podWatch, t.watchAndListPods, t.updatePod)
	go t.runWatch("PodGroups", podGroupWatch, t.watchAndListPodGroups, t.updatePodGroup)
	return t, nil
}

func (t *jobBatchTracker) AddJob(job *batchv1.Job) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.jobs[runtimeClient.ObjectKeyFromObject(job)] = job.DeepCopy()
	t.notifyLocked()
}

func (t *jobBatchTracker) Jobs() []*batchv1.Job {
	t.mu.Lock()
	defer t.mu.Unlock()
	jobs := make([]*batchv1.Job, 0, len(t.jobs))
	for _, job := range t.jobs {
		jobs = append(jobs, job.DeepCopy())
	}
	return jobs
}

func (t *jobBatchTracker) WaitForReady(ctx context.Context) error {
	_, err := t.waitForLocked(ctx, "batch resources to be created", func(status BatchStatus) bool {
		return status.ObservedPods >= status.ExpectedPods &&
			status.ObservedPodGroups >= status.ExpectedPodGroups
	})
	return err
}

func (t *jobBatchTracker) WaitForScheduled(ctx context.Context) (BatchStatus, error) {
	return t.waitForLocked(ctx, "batch Pods to be scheduled", func(status BatchStatus) bool {
		return status.ObservedPods >= status.ExpectedPods && status.ScheduledPods >= status.ExpectedPods
	})
}

func (t *jobBatchTracker) WaitForRunning(ctx context.Context) (BatchStatus, error) {
	return t.waitForLocked(ctx, "batch Pods to be running", func(status BatchStatus) bool {
		return status.ObservedPods >= status.ExpectedPods && status.RunningPods >= status.ExpectedPods
	})
}

func (t *jobBatchTracker) WaitForStatus(
	ctx context.Context, description string, condition func(BatchStatus) bool,
) (BatchStatus, error) {
	return t.waitForLocked(ctx, description, condition)
}

// waitForLocked calls condition while t.mu is held.
func (t *jobBatchTracker) waitForLocked(
	ctx context.Context, description string, condition func(BatchStatus) bool,
) (BatchStatus, error) {
	ticker := time.NewTicker(batchProgressLogInterval)
	defer ticker.Stop()

	for {
		t.mu.Lock()
		status := t.statusLocked()
		if t.terminalError != nil {
			err := t.terminalError
			t.mu.Unlock()
			return status, fmt.Errorf("wait for %s in batch %s: %w", description, t.batchID, err)
		}
		if condition(status) {
			t.mu.Unlock()
			return status, nil
		}
		changed := t.changed
		t.mu.Unlock()

		select {
		case <-ctx.Done():
			t.mu.Lock()
			status = t.statusLocked()
			t.mu.Unlock()
			return status, fmt.Errorf("wait for %s in batch %s: %w", description, t.batchID, ctx.Err())
		case <-changed:
		case <-ticker.C:
			GinkgoLogr.Info("Waiting for scale Job batch", "batchID", t.batchID, "condition", description,
				"expectedPods", status.ExpectedPods, "observedPods", status.ObservedPods,
				"scheduledPods", status.ScheduledPods, "runningPods", status.RunningPods,
				"succeededPods", status.SucceededPods, "pendingPods", status.PendingPods,
				"podGroups", status.ObservedPodGroups)
		}
	}
}

func (t *jobBatchTracker) WaitForPodGroupCondition(
	ctx context.Context, conditionType v2alpha2.SchedulingConditionType,
) (PodGroupConditionTiming, error) {
	var timing PodGroupConditionTiming
	_, err := t.waitForLocked(ctx, fmt.Sprintf("PodGroup condition %s", conditionType), func(BatchStatus) bool {
		for _, podGroup := range t.podGroups {
			if transitionAt, found := podGroup.conditions[conditionType]; found {
				timing = PodGroupConditionTiming{CreatedAt: podGroup.createdAt, TransitionAt: transitionAt}
				return true
			}
		}
		return false
	})
	return timing, err
}

func (t *jobBatchTracker) WaitForSinglePodGroupCreation(ctx context.Context) (time.Time, error) {
	var createdAt time.Time
	_, err := t.waitForLocked(ctx, "single PodGroup to be created", func(status BatchStatus) bool {
		if status.ExpectedPodGroups != 1 || status.ObservedPodGroups < 1 {
			return false
		}
		for _, podGroup := range t.podGroups {
			createdAt = podGroup.createdAt
			return true
		}
		return false
	})
	return createdAt, err
}

func (t *jobBatchTracker) Close() {
	t.cancel()
	t.wg.Wait()
}

func (t *jobBatchTracker) watchAndListPods() (watch.Interface, error) {
	pods := &v1.PodList{}
	if err := t.client.List(t.ctx, pods, runtimeClient.InNamespace(t.namespace), runtimeClient.MatchingLabels(t.batchLabels)); err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.pods = make(map[types.UID]podState, len(pods.Items))
	t.scheduledPods, t.runningPods, t.succeededPods, t.pendingPods = 0, 0, 0, 0
	t.lastScheduled = time.Time{}
	for i := range pods.Items {
		t.upsertPodLocked(&pods.Items[i])
	}
	t.notifyLocked()
	t.mu.Unlock()

	return t.client.Watch(t.ctx, &v1.PodList{}, t.watchOptions(pods.ResourceVersion)...)
}

func (t *jobBatchTracker) watchAndListPodGroups() (watch.Interface, error) {
	podGroups := &v2alpha2.PodGroupList{}
	if err := t.client.List(t.ctx, podGroups, runtimeClient.InNamespace(t.namespace), runtimeClient.MatchingLabels(t.batchLabels)); err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.podGroups = make(map[types.UID]podGroupState, len(podGroups.Items))
	for i := range podGroups.Items {
		t.podGroups[podGroups.Items[i].UID] = podGroupStateFromPodGroup(&podGroups.Items[i])
	}
	t.notifyLocked()
	t.mu.Unlock()

	return t.client.Watch(t.ctx, &v2alpha2.PodGroupList{}, t.watchOptions(podGroups.ResourceVersion)...)
}

func (t *jobBatchTracker) watchOptions(resourceVersion string) []runtimeClient.ListOption {
	return []runtimeClient.ListOption{
		runtimeClient.InNamespace(t.namespace),
		runtimeClient.MatchingLabels(t.batchLabels),
		&runtimeClient.ListOptions{Raw: &metav1.ListOptions{ResourceVersion: resourceVersion}},
	}
}

func (t *jobBatchTracker) runWatch(
	resource string,
	resourceWatch watch.Interface,
	restart func() (watch.Interface, error),
	update func(runtime.Object, watch.EventType) error,
) {
	defer t.wg.Done()
	backoff := watchRestartInitialBackoff
	for {
		err := t.consumeWatch(resourceWatch, update)
		resourceWatch.Stop()
		if t.ctx.Err() != nil {
			return
		}
		if err != nil && !isRecoverableWatchError(err) {
			t.setTerminalError(fmt.Errorf("watch %s: %w", resource, err))
			return
		}
		if err != nil {
			GinkgoLogr.Error(err, "Restarting scale Job batch watch", "batchID", t.batchID, "resource", resource)
			if !t.waitForWatchRestart(backoff) {
				return
			}
			backoff = min(backoff*2, watchRestartMaxBackoff)
		}

		resourceWatch, err = restart()
		if err == nil {
			backoff = watchRestartInitialBackoff
			continue
		}
		if !isRecoverableWatchError(err) {
			t.setTerminalError(fmt.Errorf("restart %s watch: %w", resource, err))
			return
		}
		GinkgoLogr.Error(err, "Retrying scale Job batch watch restart", "batchID", t.batchID, "resource", resource)
		if !t.waitForWatchRestart(backoff) {
			return
		}
		backoff = min(backoff*2, watchRestartMaxBackoff)
	}
}

func (t *jobBatchTracker) waitForWatchRestart(backoff time.Duration) bool {
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-t.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (t *jobBatchTracker) consumeWatch(resourceWatch watch.Interface, update func(runtime.Object, watch.EventType) error) error {
	for {
		select {
		case <-t.ctx.Done():
			return nil
		case event, open := <-resourceWatch.ResultChan():
			if !open {
				return nil
			}
			if event.Type == watch.Error {
				return watchEventError(event.Object)
			}
			if err := update(event.Object, event.Type); err != nil {
				return err
			}
		}
	}
}

func (t *jobBatchTracker) updatePod(obj runtime.Object, eventType watch.EventType) error {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return fmt.Errorf("unexpected Pod watch object %T", obj)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if eventType == watch.Deleted {
		t.deletePodLocked(pod.UID)
	} else {
		t.upsertPodLocked(pod)
	}
	t.notifyLocked()
	return nil
}

func (t *jobBatchTracker) updatePodGroup(obj runtime.Object, eventType watch.EventType) error {
	podGroup, ok := obj.(*v2alpha2.PodGroup)
	if !ok {
		return fmt.Errorf("unexpected PodGroup watch object %T", obj)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if eventType == watch.Deleted {
		delete(t.podGroups, podGroup.UID)
	} else {
		t.podGroups[podGroup.UID] = podGroupStateFromPodGroup(podGroup)
	}
	t.notifyLocked()
	return nil
}

func (t *jobBatchTracker) upsertPodLocked(pod *v1.Pod) {
	if existing, found := t.pods[pod.UID]; found {
		t.removePodCountersLocked(existing)
	}
	state := podStateFromPod(pod)
	t.pods[pod.UID] = state
	t.addPodCountersLocked(state)
}

func (t *jobBatchTracker) deletePodLocked(uid types.UID) {
	state, found := t.pods[uid]
	if !found {
		return
	}
	t.removePodCountersLocked(state)
	delete(t.pods, uid)
}

func (t *jobBatchTracker) addPodCountersLocked(state podState) {
	if state.scheduled {
		t.scheduledPods++
		if state.scheduledAt.After(t.lastScheduled) {
			t.lastScheduled = state.scheduledAt
		}
	}
	if state.running {
		t.runningPods++
	}
	if state.succeeded {
		t.succeededPods++
	}
	if state.pending {
		t.pendingPods++
	}
}

func (t *jobBatchTracker) removePodCountersLocked(state podState) {
	if state.scheduled {
		t.scheduledPods--
	}
	if state.running {
		t.runningPods--
	}
	if state.succeeded {
		t.succeededPods--
	}
	if state.pending {
		t.pendingPods--
	}
}

func (t *jobBatchTracker) statusLocked() BatchStatus {
	return BatchStatus{
		ExpectedPods:      t.expectedPods,
		ExpectedPodGroups: t.expectedPodGroups,
		ObservedPods:      len(t.pods),
		ObservedPodGroups: len(t.podGroups),
		ScheduledPods:     t.scheduledPods,
		RunningPods:       t.runningPods,
		SucceededPods:     t.succeededPods,
		PendingPods:       t.pendingPods,
		LastScheduledAt:   t.lastScheduled,
	}
}

func (t *jobBatchTracker) setTerminalError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.terminalError == nil {
		t.terminalError = err
		t.notifyLocked()
	}
}

func (t *jobBatchTracker) notifyLocked() {
	close(t.changed)
	t.changed = make(chan struct{})
}

func podStateFromPod(pod *v1.Pod) podState {
	state := podState{
		running:   pod.Status.Phase == v1.PodRunning,
		succeeded: pod.Status.Phase == v1.PodSucceeded,
		pending:   pod.Status.Phase == v1.PodPending,
	}
	if scheduledAt, err := getPodScheduledTime(pod); err == nil {
		state.scheduled = true
		state.scheduledAt = scheduledAt
	}
	return state
}

func podGroupStateFromPodGroup(podGroup *v2alpha2.PodGroup) podGroupState {
	state := podGroupState{createdAt: podGroup.CreationTimestamp.Time, conditions: make(map[v2alpha2.SchedulingConditionType]time.Time)}
	for _, condition := range podGroup.Status.SchedulingConditions {
		state.conditions[condition.Type] = condition.LastTransitionTime.Time
	}
	return state
}

func watchEventError(obj runtime.Object) error {
	if err := apierrors.FromObject(obj); err != nil {
		return err
	}
	return fmt.Errorf("Kubernetes watch returned an error event")
}

func isRecoverableWatchError(err error) bool {
	return apierrors.IsResourceExpired(err) ||
		apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err)
}
