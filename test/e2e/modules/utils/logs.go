/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package utils

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	v1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func SetLogger() {
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
}

func LogClusterState(client runtimeClient.WithWatch, logger logr.Logger) {
	e2ePods := &v1.PodList{}
	err := client.List(context.Background(), e2ePods, runtimeClient.MatchingLabels(map[string]string{
		constants.AppLabelName: "engine-e2e",
	}))
	if err != nil {
		return
	}
	logger.Info(fmt.Sprintf("Failed test cluster state - E2e pods: \n%v", podListPrinting(e2ePods)))
}

func podListPrinting(podList *v1.PodList) string {
	var podListRepresentationString strings.Builder

	for _, pod := range podList.Items {
		podListRepresentationString.WriteString(
			fmt.Sprintf("Namespace: %s, Name: %s, Phase: %s, Node:%s, Annotations: %v\n",
				pod.Namespace, pod.Name,
				pod.Status.Phase, pod.Spec.NodeName, pod.Annotations),
		)
		for _, container := range pod.Spec.Containers {
			podListRepresentationString.WriteString(
				fmt.Sprintf("\tContainer: %s, resources: %v\n", container.Name, container.Resources),
			)
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == v1.PodScheduled && condition.Status == v1.ConditionFalse {
				podListRepresentationString.WriteString(
					fmt.Sprintf("\tScheduling failed: %s\n", condition.Message),
				)
			}
		}

	}

	return podListRepresentationString.String()
}
