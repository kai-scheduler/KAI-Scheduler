// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package evictor

import (
	"context"
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/k8s_internal"
)

type defaultEvictor struct {
	kubeClient               kubernetes.Interface
	shouldUpdatePodCondition bool
}

func New(kubeClient kubernetes.Interface, updatePodCondition bool) *defaultEvictor {
	return &defaultEvictor{
		kubeClient:               kubeClient,
		shouldUpdatePodCondition: updatePodCondition,
	}
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=delete

func (de *defaultEvictor) Evict(pod *v1.Pod, message string) error {
	if de.shouldUpdatePodCondition {
		err := de.updatePodCondition(pod, message)
		if err != nil {
			return err
		}
	}

	return de.kubeClient.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name,
		metav1.DeleteOptions{})
}

func (de *defaultEvictor) updatePodCondition(pod *v1.Pod, message string) error {
	condition := &v1.PodCondition{
		Type:    v1.DisruptionTarget,
		Status:  v1.ConditionTrue,
		Reason:  v1.PodReasonPreemptionByScheduler,
		Message: message,
	}

	if k8s_internal.UpdatePodCondition(&pod.Status, condition) {
		statusPatchBaseObject := v1.PodStatus{}
		statusPatchBaseObject.Conditions = []v1.PodCondition{*condition}
		podStatusPatchBytes, err := json.Marshal(statusPatchBaseObject)
		if err != nil {
			return err
		}

		patchData := []byte(fmt.Sprintf(`{"status": %s}`, string(podStatusPatchBytes)))
		_, err = de.kubeClient.CoreV1().Pods(pod.Namespace).Patch(
			context.Background(), pod.Name, types.StrategicMergePatchType, patchData, metav1.PatchOptions{},
			"status",
		)
		if err != nil {
			return err
		}
	}
	return nil
}
