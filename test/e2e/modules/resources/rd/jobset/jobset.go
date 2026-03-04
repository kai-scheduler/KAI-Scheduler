// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package jobset

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	jobsetv1alpha2 "sigs.k8s.io/jobset/api/jobset/v1alpha2"

	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/testconfig"
)

// CreateUnequalCompletionJobSetObject creates a JobSet with a single replicatedJob
func CreateUnequalCompletionJobSetObject(name, namespace, queueName string, parallelism, completions int32) *jobsetv1alpha2.JobSet {
	return &jobsetv1alpha2.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName:               "engine-e2e",
				testconfig.GetConfig().QueueLabelKey: queueName,
			},
		},
		Spec: jobsetv1alpha2.JobSetSpec{
			ReplicatedJobs: []jobsetv1alpha2.ReplicatedJob{
				{
					Name:     "worker",
					Replicas: 1,
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Parallelism:  pointer.Int32(parallelism),
							Completions:  pointer.Int32(completions),
							BackoffLimit: pointer.Int32(0),
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									RestartPolicy: corev1.RestartPolicyNever,
									SchedulerName: testconfig.GetConfig().SchedulerName,
									Containers: []corev1.Container{
										{
											Name:  "worker",
											Image: "busybox:1.36",
											Command: []string{
												"/bin/sh",
												"-c",
												// First pod sleeps 5s, others sleep 10s
												"if [ \"$JOB_COMPLETION_INDEX\" = \"0\" ]; then sleep 5; else sleep 10; fi && echo 'Job completed'",
											},
											Env: []corev1.EnvVar{
												{
													Name: "JOB_COMPLETION_INDEX",
													ValueFrom: &corev1.EnvVarSource{
														FieldRef: &corev1.ObjectFieldSelector{
															FieldPath: "metadata.annotations['batch.kubernetes.io/job-completion-index']",
														},
													},
												},
											},
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU:    resource.MustParse("100m"),
													corev1.ResourceMemory: resource.MustParse("128Mi"),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// CreateObjectWithStartupPolicy creates a JobSet with 2 replicatedJobs and specified startup policy
func CreateObjectWithStartupPolicy(name, namespace, queueName, startupPolicyOrder string) *jobsetv1alpha2.JobSet {
	jobSet := &jobsetv1alpha2.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName:               "engine-e2e",
				testconfig.GetConfig().QueueLabelKey: queueName,
			},
		},
		Spec: jobsetv1alpha2.JobSetSpec{
			StartupPolicy: &jobsetv1alpha2.StartupPolicy{
				StartupPolicyOrder: jobsetv1alpha2.StartupPolicyOptions(startupPolicyOrder),
			},
			ReplicatedJobs: []jobsetv1alpha2.ReplicatedJob{
				{
					Name:     "job1",
					Replicas: 1,
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Parallelism:  pointer.Int32(1),
							Completions:  pointer.Int32(1),
							BackoffLimit: pointer.Int32(0),
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									RestartPolicy: corev1.RestartPolicyNever,
									SchedulerName: testconfig.GetConfig().SchedulerName,
									Containers: []corev1.Container{
										{
											Name:  "job1",
											Image: "busybox:1.36",
											Command: []string{
												"/bin/sh",
												"-c",
												"sleep 5 && echo 'Job1 completed'",
											},
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU:    resource.MustParse("100m"),
													corev1.ResourceMemory: resource.MustParse("128Mi"),
												},
											},
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
							Parallelism:  pointer.Int32(1),
							Completions:  pointer.Int32(1),
							BackoffLimit: pointer.Int32(0),
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									RestartPolicy: corev1.RestartPolicyNever,
									SchedulerName: testconfig.GetConfig().SchedulerName,
									Containers: []corev1.Container{
										{
											Name:  "job2",
											Image: "busybox:1.36",
											Command: []string{
												"/bin/sh",
												"-c",
												"sleep 5 && echo 'Job2 completed'",
											},
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU:    resource.MustParse("100m"),
													corev1.ResourceMemory: resource.MustParse("128Mi"),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return jobSet
}

// CreateObjectWithHighParallelism creates a JobSet with a single ReplicatedJob (parallelism=8)
func CreateObjectWithHighParallelism(name, namespace, queueName string) *jobsetv1alpha2.JobSet {
	return &jobsetv1alpha2.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName:               "engine-e2e",
				testconfig.GetConfig().QueueLabelKey: queueName,
			},
		},
		Spec: jobsetv1alpha2.JobSetSpec{
			SuccessPolicy: &jobsetv1alpha2.SuccessPolicy{
				Operator: jobsetv1alpha2.OperatorAll,
			},
			FailurePolicy: &jobsetv1alpha2.FailurePolicy{
				MaxRestarts: 3,
			},
			ReplicatedJobs: []jobsetv1alpha2.ReplicatedJob{
				{
					Name:     "worker",
					Replicas: 1,
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Parallelism:  pointer.Int32(8),
							Completions:  pointer.Int32(8),
							BackoffLimit: pointer.Int32(0),
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									RestartPolicy: corev1.RestartPolicyNever,
									SchedulerName: testconfig.GetConfig().SchedulerName,
									Containers: []corev1.Container{
										{
											Name:  "worker",
											Image: testconfig.GetConfig().ContainerImage,
											Command: []string{
												"sleep",
												"3600",
											},
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													"nvidia.com/gpu": resource.MustParse("1"),
												},
												Limits: corev1.ResourceList{
													"nvidia.com/gpu": resource.MustParse("1"),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// CreateObjectWithMultipleReplicatedJobs creates a JobSet with multiple ReplicatedJobs
func CreateObjectWithMultipleReplicatedJobs(name, namespace, queueName string) *jobsetv1alpha2.JobSet {
	return &jobsetv1alpha2.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName:               "engine-e2e",
				testconfig.GetConfig().QueueLabelKey: queueName,
			},
		},
		Spec: jobsetv1alpha2.JobSetSpec{
			StartupPolicy: &jobsetv1alpha2.StartupPolicy{
				StartupPolicyOrder: jobsetv1alpha2.StartupPolicyOptions("AnyOrder"),
			},
			SuccessPolicy: &jobsetv1alpha2.SuccessPolicy{
				Operator: jobsetv1alpha2.OperatorAll,
			},
			FailurePolicy: &jobsetv1alpha2.FailurePolicy{
				MaxRestarts: 3,
			},
			ReplicatedJobs: []jobsetv1alpha2.ReplicatedJob{
				{
					Name:     "coordinator",
					Replicas: 1,
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Parallelism:  pointer.Int32(2),
							Completions:  pointer.Int32(2),
							BackoffLimit: pointer.Int32(0),
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									RestartPolicy: corev1.RestartPolicyNever,
									SchedulerName: testconfig.GetConfig().SchedulerName,
									Containers: []corev1.Container{
										{
											Name:  "coordinator",
											Image: testconfig.GetConfig().ContainerImage,
											Command: []string{
												"sleep",
												"3600",
											},
										},
									},
								},
							},
						},
					},
				},
				{
					Name:     "worker",
					Replicas: 1, // Reduced from 2 to fit 4 GPU env
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Parallelism:  pointer.Int32(2), // Reduced from 4 to 2 (2 GPUs total)
							Completions:  pointer.Int32(2), // Reduced from 4 to 2
							BackoffLimit: pointer.Int32(0),
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									RestartPolicy: corev1.RestartPolicyNever,
									SchedulerName: testconfig.GetConfig().SchedulerName,
									Containers: []corev1.Container{
										{
											Name:  "worker",
											Image: testconfig.GetConfig().ContainerImage,
											Command: []string{
												"sleep",
												"3600",
											},
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													"nvidia.com/gpu": resource.MustParse("1"),
												},
												Limits: corev1.ResourceList{
													"nvidia.com/gpu": resource.MustParse("1"),
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// CreateObjectWithDefaultParallelism creates a JobSet with single replica and default parallelism
func CreateObjectWithDefaultParallelism(name, namespace, queueName string) *jobsetv1alpha2.JobSet {
	return &jobsetv1alpha2.JobSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName:               "engine-e2e",
				testconfig.GetConfig().QueueLabelKey: queueName,
			},
		},
		Spec: jobsetv1alpha2.JobSetSpec{
			SuccessPolicy: &jobsetv1alpha2.SuccessPolicy{
				Operator: jobsetv1alpha2.OperatorAll,
			},
			FailurePolicy: &jobsetv1alpha2.FailurePolicy{
				MaxRestarts: 3,
			},
			ReplicatedJobs: []jobsetv1alpha2.ReplicatedJob{
				{
					Name:     "single",
					Replicas: 1,
					Template: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							// No parallelism specified, should default to 1
							Completions:  pointer.Int32(1),
							BackoffLimit: pointer.Int32(0),
							Template: corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									RestartPolicy: corev1.RestartPolicyNever,
									SchedulerName: testconfig.GetConfig().SchedulerName,
									Containers: []corev1.Container{
										{
											Name:  "single",
											Image: testconfig.GetConfig().ContainerImage,
											Command: []string{
												"sleep",
												"3600",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
