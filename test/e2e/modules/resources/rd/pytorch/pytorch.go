// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pytorch

import (
	trainingoperatorv1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/constant"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
)

const (
	MasterReplicaType = "Master"
	WorkerReplicaType = "Worker"
)

func CreateObject(namespace, queueName string) *trainingoperatorv1.PyTorchJob {
	matchLabelValue := constant.EngineTestPodsApp

	return &trainingoperatorv1.PyTorchJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GenerateRandomK8sName(10),
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName: matchLabelValue,
			},
		},
		Spec: trainingoperatorv1.PyTorchJobSpec{
			PyTorchReplicaSpecs: map[trainingoperatorv1.ReplicaType]*trainingoperatorv1.ReplicaSpec{
				MasterReplicaType: {
					Replicas: pointer.Int32(1),
					Template: GetPodTemplate(queueName, matchLabelValue),
				},
				WorkerReplicaType: {
					Replicas: pointer.Int32(1),
					Template: GetPodTemplate(queueName, matchLabelValue),
				},
			},
		},
	}
}

func CreateWorkerOnlyObject(namespace, queueName string, replicas int32) *trainingoperatorv1.PyTorchJob {
	matchLabelValue := constant.EngineTestPodsApp

	return &trainingoperatorv1.PyTorchJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GenerateRandomK8sName(10),
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName: matchLabelValue,
			},
		},
		Spec: trainingoperatorv1.PyTorchJobSpec{
			PyTorchReplicaSpecs: map[trainingoperatorv1.ReplicaType]*trainingoperatorv1.ReplicaSpec{
				"Worker": {
					Replicas: pointer.Int32(replicas),
					Template: GetPodTemplate(queueName, matchLabelValue),
				},
			},
		},
	}
}

func GetPodTemplate(queueName, matchLabelValue string) corev1.PodTemplateSpec {
	cfg := testconfig.GetConfig()
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				constants.AppLabelName: matchLabelValue,
				cfg.QueueLabelKey:      queueName,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			SchedulerName:                 cfg.SchedulerName,
			TerminationGracePeriodSeconds: pointer.Int64(0),
			Containers: []corev1.Container{
				{
					Image: cfg.ContainerImage,
					Name:  "pytorch",
					Args: []string{
						"sleep",
						"infinity",
					},
					SecurityContext: rd.DefaultSecurityContext(),
				},
			},
		},
	}
}
