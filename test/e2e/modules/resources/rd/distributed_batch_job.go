// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package rd

import (
	"context"
	"fmt"
	"maps"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	pgconstants "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
)

const (
	// JobNameLabel is the label the k8s Job controller sets on every pod it creates.
	JobNameLabel = "batch.kubernetes.io/job-name"

	podGroupFetchTimeout = 30 * time.Second
	podGroupFetchPoll    = 250 * time.Millisecond
)

// DistributedBatchJobOptions configures CreateDistributedBatchJob. Every field is optional
// — pass DistributedBatchJobOptions{} to get a single-pod gang Job with no resource requests.
type DistributedBatchJobOptions struct {
	// Parallelism is the number of pods the Job spawns. nil means 1.
	Parallelism *int32
	// MinMember is the PodGroup MinAvailable. nil means Parallelism (gang).
	//   Gang:    MinMember == Parallelism
	//   Elastic: 1 <= MinMember < Parallelism
	MinMember *int32
	// Resources applied to each pod. Zero value means no requests/limits.
	Resources v1.ResourceRequirements
	// NamePrefix is prepended to the generated Job name.
	NamePrefix string
	// TopologyConstraint is propagated to the auto-created PodGroup via annotations.
	TopologyConstraint *v2alpha2.TopologyConstraint
	// PriorityClassName is set on the pod template; the podgrouper reads it onto the PodGroup.
	PriorityClassName string
	// Preemptibility is set as a Job label; the podgrouper reads it onto the PodGroup.
	Preemptibility v2alpha2.Preemptibility
	// ExtraLabels are merged into pod template labels (e.g. for test filtering).
	ExtraLabels map[string]string
	// PodSpecMutator is applied to the pod template spec after defaults are set. Scale
	// tests use this to inject KWOK tolerations/affinity without importing scale into rd.
	PodSpecMutator func(*v1.PodSpec)
	// BackoffLimit on the Job. nil leaves the k8s default (6 retries).
	// Set to ptr.To(int32(0)) for partial-reclaim/preemption tests where the
	// scheduler deletes pods and the test expects them to stay deleted —
	// BackoffLimit=0 marks the Job Failed on the first deletion so pod
	// replacement stops while surviving pods keep running.
	BackoffLimit *int32
}

// CreateDistributedBatchJob submits a batch Job annotated with kai.scheduler/batch-min-member
// so the podgrouper produces a single PodGroup with MinAvailable=opts.MinMember. Returns the
// Job, the PodGroup (once the podgrouper has created it), and the pods the Job spawned.
func CreateDistributedBatchJob(
	ctx context.Context,
	kubeClient runtimeClient.Client,
	jobQueue *v2.Queue,
	opts DistributedBatchJobOptions,
) (*batchv1.Job, *v2alpha2.PodGroup, []*v1.Pod, error) {
	parallelism := ptr.Deref(opts.Parallelism, 1)
	minMember := ptr.Deref(opts.MinMember, parallelism)

	job := buildDistributedBatchJob(jobQueue, opts, parallelism, minMember)
	if err := kubeClient.Create(ctx, job); err != nil {
		return nil, nil, nil, fmt.Errorf("create Job: %w", err)
	}

	podGroup, err := waitForPodGroup(ctx, kubeClient, job)
	if err != nil {
		return job, nil, nil, err
	}

	pods, err := waitForJobPods(ctx, kubeClient, job, parallelism)
	if err != nil {
		return job, podGroup, nil, err
	}

	return job, podGroup, pods, nil
}

func buildDistributedBatchJob(
	jobQueue *v2.Queue, opts DistributedBatchJobOptions, parallelism, minMember int32,
) *batchv1.Job {
	job := CreateBatchJobObject(jobQueue, opts.Resources)
	job.Name = opts.NamePrefix + job.Name
	job.Spec.Parallelism = ptr.To(parallelism)
	job.Spec.Completions = ptr.To(parallelism)
	if opts.BackoffLimit != nil {
		job.Spec.BackoffLimit = opts.BackoffLimit
	}

	if job.Annotations == nil {
		job.Annotations = map[string]string{}
	}
	job.Annotations[pgconstants.MinMemberOverrideKey] = fmt.Sprintf("%d", minMember)

	if tc := opts.TopologyConstraint; tc != nil {
		if tc.Topology != "" {
			job.Annotations[pgconstants.TopologyKey] = tc.Topology
		}
		if tc.RequiredTopologyLevel != "" {
			job.Annotations[pgconstants.TopologyRequiredPlacementKey] = tc.RequiredTopologyLevel
		}
		if tc.PreferredTopologyLevel != "" {
			job.Annotations[pgconstants.TopologyPreferredPlacementKey] = tc.PreferredTopologyLevel
		}
	}

	if opts.Preemptibility != "" {
		job.Labels[pgconstants.PreemptibilityLabelKey] = string(opts.Preemptibility)
	}

	if opts.PriorityClassName != "" {
		job.Spec.Template.Spec.PriorityClassName = opts.PriorityClassName
	}

	maps.Copy(job.Spec.Template.ObjectMeta.Labels, opts.ExtraLabels)

	if opts.PodSpecMutator != nil {
		opts.PodSpecMutator(&job.Spec.Template.Spec)
	}

	return job
}

func waitForPodGroup(
	ctx context.Context, kubeClient runtimeClient.Client, job *batchv1.Job,
) (*v2alpha2.PodGroup, error) {
	name := PodGroupNameForJob(job)
	pg := &v2alpha2.PodGroup{}
	key := types.NamespacedName{Namespace: job.Namespace, Name: name}

	err := wait.PollUntilContextTimeout(ctx, podGroupFetchPoll, podGroupFetchTimeout, true,
		func(ctx context.Context) (bool, error) {
			err := kubeClient.Get(ctx, key, pg)
			if errors.IsNotFound(err) {
				return false, nil
			}
			return err == nil, err
		})
	if err != nil {
		return nil, fmt.Errorf("wait for PodGroup %s: %w", name, err)
	}
	return pg, nil
}

func waitForJobPods(
	ctx context.Context, kubeClient runtimeClient.Client, job *batchv1.Job, expected int32,
) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	err := wait.PollUntilContextTimeout(ctx, podGroupFetchPoll, podGroupFetchTimeout, true,
		func(ctx context.Context) (bool, error) {
			list := &v1.PodList{}
			err := kubeClient.List(ctx, list,
				runtimeClient.InNamespace(job.Namespace),
				runtimeClient.MatchingLabels{JobNameLabel: job.Name},
			)
			if err != nil {
				return false, err
			}
			if int32(len(list.Items)) < expected {
				return false, nil
			}
			pods = make([]*v1.Pod, 0, len(list.Items))
			for i := range list.Items {
				pods = append(pods, &list.Items[i])
			}
			return true, nil
		})
	if err != nil {
		return nil, fmt.Errorf("wait for %d pods of Job %s: %w", expected, job.Name, err)
	}
	return pods, nil
}

// PodGroupNameForJob returns the deterministic name the podgrouper uses for a Job-owned PodGroup.
func PodGroupNameForJob(job *batchv1.Job) string {
	return fmt.Sprintf("%s-%s-%s", pgconstants.PodGroupNamePrefix, job.Name, job.UID)
}
