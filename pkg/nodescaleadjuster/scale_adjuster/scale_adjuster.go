// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale_adjuster

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/scaler"
)

type ScaleAdjuster struct {
	client          client.Client
	scaler          *scaler.Scaler
	eventMutex      sync.Mutex
	calculator      *calculator
	namespace       string
	lastScaleUpTime int64
	coolDownTime    int64
	schedulerName   string
}

func NewScaleAdjuster(client client.Client, scaler *scaler.Scaler, namespace string,
	coolDownTime int64, gpuMemoryToFractionRatio float64, schedulerName string) *ScaleAdjuster {
	return &ScaleAdjuster{
		client,
		scaler,
		sync.Mutex{},
		newCalculator(gpuMemoryToFractionRatio),
		namespace,
		-1,
		coolDownTime,
		schedulerName,
	}
}

func (sa *ScaleAdjuster) Adjust() (bool, error) {
	sa.eventMutex.Lock()
	defer sa.eventMutex.Unlock()
	log.Println("Looking for pods to adjust..")

	remainingScalingPods, err := sa.removeUnneededScalingPods()
	if err != nil {
		log.Printf("Failed to remove unneeded scaling pods. err: %v", err)
		return false, err
	}
	if sa.isInCoolDown() {
		return true, nil
	}
	numCreatedPods, err := sa.createNewScalingPods(remainingScalingPods)
	if err != nil {
		return false, err
	}
	if numCreatedPods > 0 {
		log.Printf("Created %v scaling pods", numCreatedPods)
	}
	return false, nil
}

func (sa *ScaleAdjuster) removeUnneededScalingPods() (remainingPods []*corev1.Pod, err error) {
	pods := corev1.PodList{}
	err = sa.client.List(context.Background(), &pods, client.InNamespace(sa.namespace))
	if err != nil {
		err = fmt.Errorf("failed to list scaling pods. err: %v", err)
		log.Printf("%v", err)
		return nil, err
	}

	for index, pod := range pods.Items {
		if sa.scaler.IsScalingPodStillNeeded(&pod) {
			remainingPods = append(remainingPods, &pods.Items[index])
		} else {
			log.Printf("Deleting scaling pod: %v/%v", pod.Namespace, pod.Name)
			err = sa.scaler.DeleteScalingPod(&pod)
			if err != nil {
				return nil, err
			}
			sa.lastScaleUpTime = time.Now().Unix()
		}
	}

	return remainingPods, nil
}

func (sa *ScaleAdjuster) isInCoolDown() bool {
	if sa.lastScaleUpTime == -1 {
		return false
	}
	return time.Now().Unix()-sa.lastScaleUpTime < sa.coolDownTime
}

func (sa *ScaleAdjuster) createNewScalingPods(existingScalingPods []*corev1.Pod) (int, error) {
	pods, err := sa.getUnschedulablePods()
	if err != nil {
		return 0, fmt.Errorf("could not get unschedulable pods. err: %v", err)
	}

	log.Printf("Found %d unschedulable pods", len(pods))

	numNeededDevices, podsToScale := sa.calculator.calculateNumNeededDevices(pods)
	if numNeededDevices == 0 {
		log.Printf("No additional scaling pods are needed")
		return 0, nil
	}
	numCreatedPods := 0
	for _, pod := range podsToScale {
		if sa.scaler.IsScalingPodExistsForUnschedulablePod(pod) {
			continue
		}

		maxScalingDevicesPerNode := sa.calculator.calculateMaxScalingDevices(existingScalingPods)
		neededDevicesForPod, err := resources.GetNumGPUFractionDevices(pod)
		if err != nil {
			log.Printf("Could not get num needed devices for pod %v/%v. err: %v",
				pod.Namespace, pod.Name, err)
			continue
		}

		numScalingDevices := sa.calculator.calculateNumScalingDevices(existingScalingPods)
		if numNeededDevices > numScalingDevices || neededDevicesForPod > maxScalingDevicesPerNode {
			log.Printf("Creating a new scaling pod for %v/%v", pod.Namespace, pod.Name)
			scalingPod, err := sa.scaler.CreateScalingPod(pod)
			if err != nil {
				log.Printf("failed to create scaling pod for %v/%v. err: %v",
					pod.Namespace, pod.Name, err)
				continue
			}
			numCreatedPods += 1
			existingScalingPods = append(existingScalingPods, scalingPod)
		} else {
			log.Printf("Not creating a scaling pod for %v/%v (already scaling up)", pod.Namespace, pod.Name)
		}
	}
	return numCreatedPods, nil
}

func (sa *ScaleAdjuster) getUnschedulablePods() ([]*corev1.Pod, error) {
	podsList := &corev1.PodList{}
	err := sa.client.List(context.Background(), podsList)
	if err != nil {
		log.Printf("Failed to list unschedulable pods. err: %v", err)
		return nil, err
	}

	var pods []*corev1.Pod
	for index, pod := range podsList.Items {
		if pod.Spec.NodeName != "" {
			continue
		}
		if pod.Spec.SchedulerName != sa.schedulerName {
			continue
		}
		if !requestFractionalGPU(&pod) {
			continue
		}
		if !isPodAlive(&pod) {
			continue
		}
		if !isPodUnschedulable(&pod) {
			continue
		}
		pods = append(pods, &podsList.Items[index])
	}

	return pods, nil
}

func requestFractionalGPU(pod *corev1.Pod) bool {
	return pod.Annotations[constants.GpuFraction] != "" || pod.Annotations[constants.GpuMemory] != ""
}

func isPodAlive(pod *corev1.Pod) bool {
	return !slices.Contains([]corev1.PodPhase{corev1.PodSucceeded, corev1.PodFailed}, pod.Status.Phase)
}

func isPodUnschedulable(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled {
			return condition.Status == corev1.ConditionFalse &&
				condition.Reason == corev1.PodReasonUnschedulable
		}
	}
	return false
}
