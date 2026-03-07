/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package rd

import (
	"context"

	schedulingv1 "k8s.io/api/scheduling/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	e2econstant "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/constant"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"
)

func CreatePreemptibleAndNonPriorityClass(ctx context.Context, client *kubernetes.Clientset) (string, string, error) {
	preemptible, err := CreatePreemptiblePriorityClass(ctx, client)
	if err != nil {
		return "", "", err
	}
	nonPreemptible, err := CreateNonPreemptiblePriorityClass(ctx, client)
	if err != nil {
		return "", "", err
	}
	return preemptible, nonPreemptible, nil
}

func CreatePreemptiblePriorityClass(ctx context.Context, client *kubernetes.Clientset) (string, error) {
	preemptible := utils.GenerateRandomK8sName(10)
	preemptibleValue := utils.RandomIntBetween(0, e2econstant.NonPreemptiblePriorityThreshold)
	_, err := client.SchedulingV1().PriorityClasses().
		Create(ctx, CreatePriorityClass(preemptible, preemptibleValue), v12.CreateOptions{})
	if err != nil {
		return "", err
	}
	return preemptible, nil
}

func CreateNonPreemptiblePriorityClass(ctx context.Context, client *kubernetes.Clientset) (string, error) {
	nonPreemptible := utils.GenerateRandomK8sName(10)
	nonPreemptibleValue := utils.RandomIntBetween(e2econstant.NonPreemptiblePriorityThreshold, 2*e2econstant.NonPreemptiblePriorityThreshold)
	_, err := client.SchedulingV1().PriorityClasses().
		Create(ctx, CreatePriorityClass(nonPreemptible, nonPreemptibleValue), v12.CreateOptions{})
	if err != nil {
		return "", err
	}
	return nonPreemptible, nil
}

func DeleteAllE2EPriorityClasses(ctx context.Context, client runtimeClient.WithWatch) error {
	err := client.DeleteAllOf(
		ctx, &schedulingv1.PriorityClass{},
		runtimeClient.MatchingLabels{constants.AppLabelName: "engine-e2e"},
	)
	err = runtimeClient.IgnoreNotFound(err)
	return err
}

func CreatePriorityClass(name string, value int) *schedulingv1.PriorityClass {
	return &schedulingv1.PriorityClass{
		ObjectMeta: v12.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				constants.AppLabelName: "engine-e2e",
			},
		},
		Value: int32(value),
	}
}
