/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package rd

import (
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
)

const CronJobAppLabel = "cron-job-app-name"

func CreateCronJobObject(namespace, queueName string) *batchv1.CronJob {
	cfg := testconfig.GetConfig()
	matchLabelValue := utils.GenerateRandomK8sName(10)

	return &batchv1.CronJob{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "CronJob",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GenerateRandomK8sName(10),
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName: "engine-e2e",
				CronJobAppLabel:        matchLabelValue,
			},
		},
		Spec: batchv1.CronJobSpec{
			Schedule: "* * * * *",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								constants.AppLabelName: "engine-e2e",
								CronJobAppLabel:        matchLabelValue,
								cfg.QueueLabelKey:      queueName,
							},
						},
						Spec: v1.PodSpec{
							RestartPolicy:                 v1.RestartPolicyNever,
							SchedulerName:                 cfg.SchedulerName,
							TerminationGracePeriodSeconds: pointer.Int64(0),
							Containers: []v1.Container{
								{
									Image: cfg.ContainerImage,
									Name:  "ubuntu-container",
									Args: []string{
										"sleep",
										"infinity",
									},
									SecurityContext: DefaultSecurityContext(),
								},
							},
							Tolerations: []v1.Toleration{
								{
									Key:      "nvidia.com/gpu",
									Operator: v1.TolerationOpExists,
									Effect:   v1.TaintEffectNoSchedule,
								},
							},
						},
					},
				},
			},
		},
	}
}
