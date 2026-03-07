// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scaler

import (
	"context"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/consts"
)

type Scaler struct {
	client                   client.Client
	image                    string
	namespace                string
	scalingPodAppLabel       string
	scalingPodServiceAccount string
}

func NewScaler(client client.Client, image, namespace, scalingPodAppLabel, scalingPodServiceAccount string) *Scaler {
	scaler := Scaler{
		client:                   client,
		image:                    image,
		namespace:                namespace,
		scalingPodAppLabel:       scalingPodAppLabel,
		scalingPodServiceAccount: scalingPodServiceAccount,
	}

	return &scaler
}

func (s *Scaler) CreateScalingPod(unschedulablePod *corev1.Pod) (*corev1.Pod, error) {
	numDevices, err := resources.GetNumGPUFractionDevices(unschedulablePod)
	if err != nil {
		return nil, fmt.Errorf("could not get num GPU devices for pod %v/%v. err: %v",
			unschedulablePod.Namespace, unschedulablePod.Name, err)
	}

	pod := createScalingPodSpec(s.scalingPodAppLabel, s.scalingPodServiceAccount,
		unschedulablePod, s.image, s.namespace, numDevices)
	err = s.client.Create(context.TODO(), pod)
	if err != nil {
		err = fmt.Errorf("Failed to create scaling pod: %v. err=%v\n", pod.Name, err)
		return nil, err
	}

	origPod := pod.DeepCopy()
	pod.Status.Conditions = []corev1.PodCondition{
		{
			Type:   corev1.PodScheduled,
			Status: corev1.ConditionFalse,
			Reason: corev1.PodReasonUnschedulable,
		},
	}
	err = s.client.Status().Patch(context.TODO(), pod, client.MergeFrom(origPod))
	if err != nil {
		err = fmt.Errorf("Failed to update scaling pod status: %v. err=%v\n", pod.Name, err)
		return nil, err
	}

	log.Printf("Created scaling pod: %v", pod.Name)
	return pod, nil
}

func (s *Scaler) DeleteScalingPod(scalingPod *corev1.Pod) error {
	err := s.client.Delete(context.TODO(), scalingPod)
	if err != nil {
		err = fmt.Errorf("Could not delete scaling pod: %v. err=%v\n", scalingPod.Name, err)
		return err
	}
	log.Printf("Deleted scaling pod: %v", scalingPod.Name)
	return nil
}

func (s *Scaler) IsScalingPodExistsForUnschedulablePod(unschedulablePod *corev1.Pod) bool {
	podName := scalingPodName(unschedulablePod.Namespace, unschedulablePod.Name)
	key := types.NamespacedName{Namespace: s.namespace, Name: podName}
	pod := &corev1.Pod{}
	err := s.client.Get(context.TODO(), key, pod)
	return err == nil
}

func (s *Scaler) IsScalingPodStillNeeded(scalingPod *corev1.Pod) bool {
	if scalingPod.Status.Phase == corev1.PodSucceeded {
		log.Printf("Pod %v status is completed and is no longer needed", scalingPod.Name)
		return false
	}

	unschedulablePodName := scalingPod.Labels[consts.SharedGpuPodName]
	unschedulablePodNamespace := scalingPod.Labels[consts.SharedGpuPodNamespace]
	unschedulablePod := &corev1.Pod{}
	key := types.NamespacedName{Namespace: unschedulablePodNamespace, Name: unschedulablePodName}
	err := s.client.Get(context.TODO(), key, unschedulablePod)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Printf("Pod %v/%v does not exist. Scaling pod is not needed: %v",
				unschedulablePodNamespace, unschedulablePodName, scalingPod.Name)
			return false
		}
		log.Printf("Could not get pod %s/%s. err: %v", unschedulablePodNamespace, unschedulablePodName, err)
		return true
	}

	condition, err := getPodCondition(&unschedulablePod.Status, corev1.PodScheduled)
	if err != nil {
		log.Printf("Could not get PodScheduled condition for pod %s/%s. err: %v",
			unschedulablePodNamespace, unschedulablePodName, err)
		return true
	}

	return condition.Status == corev1.ConditionFalse && condition.Reason == corev1.PodReasonUnschedulable
}

func getPodCondition(podStatus *corev1.PodStatus, conditionType corev1.PodConditionType) (*corev1.PodCondition, error) {
	for _, condition := range podStatus.Conditions {
		if condition.Type == conditionType {
			return &condition, nil
		}
	}
	return nil, fmt.Errorf("could not find condition with type: %v", conditionType)
}
