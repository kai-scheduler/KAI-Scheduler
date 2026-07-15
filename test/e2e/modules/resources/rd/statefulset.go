/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/

package rd

import (
	v1 "k8s.io/api/apps/v1"
	v12 "k8s.io/api/core/v1"
	v13 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
)

const StatefulSetAppLabel = "stateful-set-app-name"

func CreateStatefulSetObject(namespace, queueName string) *v1.StatefulSet {
	cfg := testconfig.GetConfig()
	matchLabelValue := utils.GenerateRandomK8sName(10)

	return &v1.StatefulSet{
		ObjectMeta: v13.ObjectMeta{
			Name:      utils.GenerateRandomK8sName(10),
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName: "engine-e2e",
				StatefulSetAppLabel:    matchLabelValue,
			},
		},
		Spec: v1.StatefulSetSpec{
			Replicas: pointer.Int32(1),
			Selector: &v13.LabelSelector{
				MatchLabels: map[string]string{
					constants.AppLabelName: "engine-e2e",
					StatefulSetAppLabel:    matchLabelValue,
				},
			},
			Template: v12.PodTemplateSpec{
				ObjectMeta: v13.ObjectMeta{
					Labels: map[string]string{
						constants.AppLabelName: "engine-e2e",
						StatefulSetAppLabel:    matchLabelValue,
						cfg.QueueLabelKey:      queueName,
					},
				},
				Spec: v12.PodSpec{
					Containers: []v12.Container{
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
					TerminationGracePeriodSeconds: pointer.Int64(0),
					SchedulerName:                 cfg.SchedulerName,
					Tolerations: []v12.Toleration{
						{
							Key:      "nvidia.com/gpu",
							Operator: v12.TolerationOpExists,
							Effect:   v12.TaintEffectNoSchedule,
						},
					},
				},
			},
		},
	}
}
