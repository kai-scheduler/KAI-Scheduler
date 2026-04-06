/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package testconfig

import (
	"context"

	"k8s.io/client-go/kubernetes"
)

type TestConfig struct {
	SchedulerName           string
	SystemPodsNamespace     string
	ReservationNamespace    string
	SchedulerDeploymentName string
	QueueLabelKey           string
	QueueNamespacePrefix    string
	ContainerImage          string

	OnNamespaceCreated func(ctx context.Context, kubeClientset kubernetes.Interface, namespaceName, queueName string) error
}

var activeConfig = TestConfig{
	SchedulerName:           "kai-scheduler",
	SystemPodsNamespace:     "kai-scheduler",
	ReservationNamespace:    "kai-resource-reservation",
	SchedulerDeploymentName: "kai-scheduler-default",
	QueueLabelKey:           "kai.scheduler/queue",
	QueueNamespacePrefix:    "kai-",
	ContainerImage:          "ubuntu",
}

func SetConfig(cfg TestConfig) { activeConfig = cfg }
func GetConfig() TestConfig    { return activeConfig }
