// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jobset

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	jobsetv1alpha2 "sigs.k8s.io/jobset/api/jobset/v1alpha2"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/api/constants"
)

func NewJobSet(name, namespace, queueName string, spec jobsetv1alpha2.JobSetSpec) *jobsetv1alpha2.JobSet {
	return &jobsetv1alpha2.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName:               "engine-e2e",
				testconfig.GetConfig().QueueLabelKey: queueName,
			},
		},
		Spec: spec,
	}
}

func CreateSetWith2ReplicatedJobs(name, namespace, queueName, startupPolicyOrder,
	firstJobCommand string, firstJobParallelism int32, secondJobCommand string, secondJobParallelism int32) *jobsetv1alpha2.JobSet {
	return NewJobSet(name, namespace, queueName, jobsetv1alpha2.JobSetSpec{
		StartupPolicy: &jobsetv1alpha2.StartupPolicy{
			StartupPolicyOrder: jobsetv1alpha2.StartupPolicyOptions(startupPolicyOrder),
		},
		SuccessPolicy: &jobsetv1alpha2.SuccessPolicy{
			Operator: jobsetv1alpha2.OperatorAll,
		},
		FailurePolicy: &jobsetv1alpha2.FailurePolicy{
			MaxRestarts: 3,
		},
		ReplicatedJobs: []jobsetv1alpha2.ReplicatedJob{
			{
				Name:     "job1",
				Replicas: 1,
				Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Parallelism:  pointer.Int32(firstJobParallelism),
						Completions:  pointer.Int32(firstJobParallelism),
						BackoffLimit: pointer.Int32(0),
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyNever,
								SchedulerName: testconfig.GetConfig().SchedulerName,
								Containers: []corev1.Container{
									{
										Name:    "job1",
										Image:   testconfig.GetConfig().ContainerImage,
										Command: []string{"sh", "-c", firstJobCommand},
									},
								},
							},
						},
					},
				},
			},
			{
				Name:     "job2",
				Replicas: 1,
				Template: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Parallelism:  pointer.Int32(secondJobParallelism),
						Completions:  pointer.Int32(secondJobParallelism),
						BackoffLimit: pointer.Int32(0),
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyNever,
								SchedulerName: testconfig.GetConfig().SchedulerName,
								Containers: []corev1.Container{
									{
										Name:    "job2",
										Image:   testconfig.GetConfig().ContainerImage,
										Command: []string{"sh", "-c", secondJobCommand},
									},
								},
							},
						},
					},
				},
			},
		},
	})
}
