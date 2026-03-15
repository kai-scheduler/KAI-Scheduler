// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package mpi

import (
	"github.com/kubeflow/mpi-operator/pkg/apis/kubeflow/v2beta1"
	trainingoperatorv1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
)

const (
	launcherReplicaType = "Launcher"
	workerReplicaType   = "Worker"
)

func CreateV1Object(namespace, queueName string) *trainingoperatorv1.MPIJob {
	matchLabelValue := constant.EngineTestPodsApp

	return &trainingoperatorv1.MPIJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GenerateRandomK8sName(10),
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName: matchLabelValue,
			},
		},
		Spec: trainingoperatorv1.MPIJobSpec{
			SlotsPerWorker: pointer.Int32(1),
			MPIReplicaSpecs: map[trainingoperatorv1.ReplicaType]*trainingoperatorv1.ReplicaSpec{
				launcherReplicaType: {
					Replicas: pointer.Int32(1),
					Template: getPodTemplate(queueName, matchLabelValue),
				},
				workerReplicaType: {
					Replicas: pointer.Int32(1),
					Template: getPodTemplate(queueName, matchLabelValue),
				},
			},
		},
	}
}

func CreateV2beta1Object(namespace, queueName string) *v2beta1.MPIJob {
	matchLabelValue := constant.EngineTestPodsApp

	return &v2beta1.MPIJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GenerateRandomK8sName(10),
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName: matchLabelValue,
			},
		},
		Spec: v2beta1.MPIJobSpec{
			SlotsPerWorker: pointer.Int32(1),
			MPIReplicaSpecs: map[v2beta1.MPIReplicaType]*v2beta1.ReplicaSpec{
				v2beta1.MPIReplicaTypeLauncher: {
					Replicas: pointer.Int32(1),
					Template: getPodTemplate(queueName, matchLabelValue),
				},
				v2beta1.MPIReplicaTypeWorker: {
					Replicas: pointer.Int32(1),
					Template: getPodTemplate(queueName, matchLabelValue),
				},
			},
		},
	}
}

func getPodTemplate(queueName, matchLabelValue string) corev1.PodTemplateSpec {
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
					Name:  "ubuntu-container",
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
