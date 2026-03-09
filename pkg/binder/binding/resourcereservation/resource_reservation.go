// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resourcereservation

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/NVIDIA/KAI-scheduler/pkg/binder/binding/resourcereservation/group_mutex"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/resources"
)

type Interface interface {
	Sync(ctx context.Context) error
	SyncForNode(ctx context.Context, nodeName string) error
	SyncForGpuGroup(ctx context.Context, gpuGroup string) error
	ReserveGpuDevice(ctx context.Context, pod *v1.Pod, nodeName string, gpuGroup string) (string, error)
	RemovePodGpuGroupsConnection(ctx context.Context, pod *v1.Pod) error
}

const (
	resourceReservation            = "resource-reservation"
	gpuReservationPodPrefix        = "gpu-reservation"
	gpuIndexAnnotationName         = "run.ai/reserve_for_gpu_index"
	numberOfGPUsToReserve          = 1
	reservationPodRandomCharacters = 5
	unknownGpuIndicator            = "-1"
)

type service struct {
	fakeGPuNodes             bool
	kubeClient               client.WithWatch
	reservationPodImage      string
	allocationTimeout        time.Duration
	gpuGroupMutex            *group_mutex.GroupMutex
	namespace                string
	serviceAccountName       string
	appLabelValue            string
	scalingPodNamespace      string
	runtimeClassName         string
	podResources             *v1.ResourceRequirements
	podSecurityContext       *v1.PodSecurityContext
	containerSecurityContext *v1.SecurityContext
}

func NewService(
	fakeGPuNodes bool,
	kubeClient client.WithWatch,
	reservationPodImage string,
	allocationTimeout time.Duration,
	namespace string,
	serviceAccountName string,
	appLabelValue string,
	scalingPodNamespace string,
	runtimeClassName string,
	podResources *v1.ResourceRequirements,
	podSecurityContext *v1.PodSecurityContext,
	containerSecurityContext *v1.SecurityContext,
) *service {
	return &service{
		fakeGPuNodes:             fakeGPuNodes,
		kubeClient:               kubeClient,
		reservationPodImage:      reservationPodImage,
		allocationTimeout:        allocationTimeout,
		gpuGroupMutex:            group_mutex.NewGroupMutex(),
		namespace:                namespace,
		serviceAccountName:       serviceAccountName,
		appLabelValue:            appLabelValue,
		scalingPodNamespace:      scalingPodNamespace,
		runtimeClassName:         runtimeClassName,
		podResources:             podResources,
		podSecurityContext:       podSecurityContext,
		containerSecurityContext: containerSecurityContext,
	}
}

func (rsc *service) Sync(ctx context.Context) error {
	podsList := &v1.PodList{}
	err := rsc.kubeClient.List(ctx, podsList,
		client.HasLabels{constants.GPUGroup},
	)
	if err != nil {
		return err
	}

	return rsc.SyncForPodsList(ctx, podsList)
}

func (rsc *service) SyncForNode(ctx context.Context, nodeName string) error {
	podsList := &v1.PodList{}
	err := rsc.kubeClient.List(ctx, podsList,
		client.HasLabels{constants.GPUGroup},
		client.MatchingFields{"spec.nodeName": nodeName},
	)
	if err != nil {
		return err
	}

	return rsc.SyncForPodsList(ctx, podsList)
}

func (rsc *service) SyncForPodsList(ctx context.Context, podsList *v1.PodList) error {
	gpuGroupsMap := map[string]bool{}
	for _, pod := range podsList.Items {
		gpuGroups := resources.GetGpuGroups(&pod)
		for _, gpuGroup := range gpuGroups {
			gpuGroupsMap[gpuGroup] = true
		}
	}

	for gpuGroup := range gpuGroupsMap {
		err := rsc.SyncForGpuGroup(ctx, gpuGroup)
		if err != nil {
			return err
		}
	}

	return nil
}

func (rsc *service) SyncForGpuGroup(ctx context.Context, gpuGroup string) error {
	rsc.gpuGroupMutex.LockMutexForGroup(gpuGroup)
	defer rsc.gpuGroupMutex.ReleaseMutex(gpuGroup)

	return rsc.syncForGpuGroupWithLock(ctx, gpuGroup)
}

func (rsc *service) syncForGpuGroupWithLock(ctx context.Context, gpuGroup string) error {
	podsList := &v1.PodList{}
	err := rsc.kubeClient.List(ctx, podsList,
		client.MatchingLabels{constants.GPUGroup: gpuGroup},
	)
	if err != nil {
		return err
	}

	multiFractionsPodsList := &v1.PodList{}
	multiGroupKey, multiGroupValue := resources.GetMultiFractionGpuGroupLabel(gpuGroup)
	err = rsc.kubeClient.List(ctx, multiFractionsPodsList,
		client.MatchingLabels{multiGroupKey: multiGroupValue},
	)
	if err != nil {
		return err
	}

	pods := []*v1.Pod{}
	for index := range len(podsList.Items) {
		pods = append(pods, &podsList.Items[index])
	}
	for index := range len(multiFractionsPodsList.Items) {
		pods = append(pods, &multiFractionsPodsList.Items[index])
	}

	return rsc.syncForPods(ctx, pods, gpuGroup)
}

func (rsc *service) syncForPods(ctx context.Context, pods []*v1.Pod, gpuGroupToSync string) error {
	logger := log.FromContext(ctx)
	reservationPods := map[string]*v1.Pod{}
	fractionPods := map[string][]*v1.Pod{}

	for _, pod := range pods {
		if pod.Namespace == rsc.namespace {
			reservationPods[gpuGroupToSync] = pod
			continue
		}

		if slices.Contains(
			[]v1.PodPhase{v1.PodRunning, v1.PodPending},
			pod.Status.Phase,
		) {
			fractionPods[gpuGroupToSync] = append(fractionPods[gpuGroupToSync], pod)
		}
	}

	for gpuGroup, pods := range fractionPods {
		if _, found := reservationPods[gpuGroup]; !found {
			err := rsc.deleteNonReservedPods(ctx, gpuGroup, pods)
			if err != nil {
				return err
			}
		}
	}

	for gpuGroup, reservationPod := range reservationPods {
		if _, found := fractionPods[gpuGroup]; !found {
			logger.Info("Did not find fraction pod for gpu group, deleting reservation pod",
				"gpuGroup", gpuGroup)
			err := rsc.deleteReservationPod(ctx, reservationPod)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (rsc *service) ReserveGpuDevice(ctx context.Context, pod *v1.Pod, nodeName string, gpuGroup string) (string, error) {
	logger := log.FromContext(ctx)

	rsc.gpuGroupMutex.LockMutexForGroup(gpuGroup)
	defer rsc.gpuGroupMutex.ReleaseMutex(gpuGroup)

	gpuIndex, err := rsc.acquireGPUIndexByGroup(ctx, nodeName, gpuGroup)
	if err != nil {
		return unknownGpuIndicator, err
	}

	err = rsc.updatePodGPUGroup(ctx, pod, nodeName, gpuGroup)
	if err != nil {
		logger.Error(err, "Failed to update GPU group annotation on pod",
			"namespace", pod.Namespace, "name", pod.Name, "node", nodeName)
		syncErr := rsc.syncForGpuGroupWithLock(ctx, gpuGroup)
		if syncErr != nil {
			logger.Error(err, "Failed to sync reservation for gpu group", "gpuGroup", gpuGroup)
		}
		return unknownGpuIndicator, err
	}

	return gpuIndex, nil
}

func (rsc *service) updatePodGPUGroup(
	ctx context.Context, pod *v1.Pod, nodeName string, gpuGroup string) error {
	logger := log.FromContext(ctx)
	logger.Info("Updating GPU group annotation for pod",
		"namespace", pod.Namespace, "name", pod.Name, "node", nodeName,
		"gpu-group", gpuGroup)
	originalPod := pod.DeepCopy()
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	isMultiFraction, err := resources.IsMultiFraction(pod)
	if err != nil {
		return fmt.Errorf(
			"failed to determine if pod <%s/%s> is a multi fractional pod while setting gpu group label. %w",
			pod.Namespace, pod.Name, err)
	}
	if isMultiFraction {
		labelKey, labelValue := resources.GetMultiFractionGpuGroupLabel(gpuGroup)
		pod.Labels[labelKey] = labelValue
	} else {
		pod.Labels[constants.GPUGroup] = gpuGroup
	}

	err = rsc.kubeClient.Patch(ctx, pod, client.MergeFrom(originalPod))
	if err != nil {
		return fmt.Errorf("failed to patch pod <%s/%s> with GPU group label: %v", pod.Namespace, pod.Name, err)
	}

	return nil
}

func (rsc *service) RemovePodGpuGroupsConnection(ctx context.Context, pod *v1.Pod) error {
	var patch []map[string]string
	for labelKey := range pod.Labels {
		if labelKey == constants.GPUGroup || strings.HasPrefix(labelKey, constants.MultiGpuGroupLabelPrefix) {
			patch = append(patch, map[string]string{
				"op":   "remove",
				"path": fmt.Sprintf("/metadata/labels/%s", escapeJSONPointer(labelKey)),
			})
		}
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to generate a patch for pod gpu-group removal. %w", err)
	}

	if err := rsc.kubeClient.Patch(ctx, pod, client.RawPatch(types.JSONPatchType, patchBytes)); err != nil {
		return err
	}
	return nil
}

// escapeJSONPointer escapes a string for use in a JSON Pointer path (RFC 6901).
// ~ must be escaped as ~0, and / must be escaped as ~1.
func escapeJSONPointer(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	s = strings.ReplaceAll(s, "/", "~1")
	return s
}

func (rsc *service) acquireGPUIndexByGroup(ctx context.Context, nodeName, gpuGroup string) (string, error) {
	gpuIndex, err := rsc.findGPUIndexByGroup(gpuGroup)
	if err != nil {
		return "", err
	}
	if gpuIndex != "" {
		return gpuIndex, err
	}
	return rsc.createGPUReservationPodAndGetIndex(ctx, nodeName, gpuGroup)
}

func (rsc *service) findGPUIndexByGroup(gpuGroup string) (
	gpuIndex string, err error,
) {
	pods := &v1.PodList{}
	err = rsc.kubeClient.List(context.Background(), pods,
		client.InNamespace(rsc.namespace),
		client.MatchingLabels{constants.GPUGroup: gpuGroup})
	if err != nil {
		return "", err
	}

	if len(pods.Items) == 0 {
		return "", nil
	}

	gpuIndex, found := pods.Items[0].Annotations[gpuIndexAnnotationName]
	if !found {
		return "", fmt.Errorf("failed to find annotation on reservation pod %s", pods.Items[0].Name)
	}

	return gpuIndex, nil
}

func (rsc *service) createGPUReservationPodAndGetIndex(ctx context.Context, nodeName, gpuGroup string) (
	gpuIndex string, err error) {
	logger := log.FromContext(ctx)
	pod, err := rsc.createGPUReservationPod(ctx, nodeName, gpuGroup)
	if err != nil {
		return unknownGpuIndicator, err
	}

	gpuIndex = rsc.waitForGPUReservationPodAllocation(ctx, nodeName, pod.Name)
	if gpuIndex == unknownGpuIndicator {
		deleteErr := rsc.deleteReservationPod(ctx, pod)
		if deleteErr != nil {
			logger.Error(deleteErr, "failed to delete reservation pod", "name", pod.Name)
		}
		return unknownGpuIndicator, fmt.Errorf(
			"failed waiting for GPU reservation pod to allocate: %v/%v", rsc.namespace, pod.Name)
	}

	return gpuIndex, err
}

func (rsc *service) deleteNonReservedPods(ctx context.Context, gpuGroup string, pods []*v1.Pod) error {
	logger := log.FromContext(ctx)
	for _, pod := range pods {
		if pod.Status.Phase != v1.PodRunning {
			continue
		}
		logger.Info("Warning: Found pod without reservation, deleting", "name", pod.Name,
			"namespace", pod.Namespace, "gpuGroup", gpuGroup)
		err := rsc.kubeClient.Delete(ctx, pod)
		if err != nil {
			logger.Error(err, "Failed to delete non-reserved pods", "name", pod.Name,
				"namespace", pod.Namespace)
			return err
		}
	}
	return nil
}

func (rsc *service) deleteReservationPod(ctx context.Context, pod *v1.Pod) error {
	logger := log.FromContext(ctx)
	logger.Info("Deleting reservation pod", "name", pod.Name)
	err := rsc.kubeClient.Delete(ctx,
		pod,
		client.GracePeriodSeconds(0),
	)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete reservation pod: %w", err)
		}
		logger.Info("Reservation pod not found, skipping deletion", "name", pod.Name)
	}
	return nil
}

func (rsc *service) createGPUReservationPod(ctx context.Context, nodeName, gpuGroup string) (*v1.Pod, error) {
	logger := log.FromContext(ctx)
	if rsc.isScalingUp(ctx) {
		return nil, fmt.Errorf("cluster is scaling up, could not create reservation pod")
	}

	podName := fmt.Sprintf("%s-%s-%s", gpuReservationPodPrefix, nodeName, rand.String(reservationPodRandomCharacters))

	// Build resource requirements starting with GPU resources
	resources := v1.ResourceRequirements{
		Limits: v1.ResourceList{
			constants.NvidiaGpuResource: *resource.NewQuantity(numberOfGPUsToReserve, resource.DecimalSI),
		},
		Requests: v1.ResourceList{
			constants.NvidiaGpuResource: *resource.NewQuantity(numberOfGPUsToReserve, resource.DecimalSI),
		},
	}

	if rsc.podResources != nil {
		if rsc.podResources.Limits != nil {
			delete(rsc.podResources.Limits, constants.NvidiaGpuResource)
			maps.Copy(resources.Limits, rsc.podResources.Limits)
		}
		if rsc.podResources.Requests != nil {
			delete(rsc.podResources.Requests, constants.NvidiaGpuResource)
			maps.Copy(resources.Requests, rsc.podResources.Requests)
		}
	}

	pod, err := rsc.createResourceReservationPod(nodeName, gpuGroup, podName, resources)
	if err != nil {
		logger.Error(err, "Failed to create GPU reservation pod on node",
			"nodeName", nodeName, "namespace", rsc.namespace, "name", podName)
		return nil, err
	}

	logger.Info(
		"Successfully created GPU resource reservation pod",
		"nodeName", nodeName, "namespace", rsc.namespace, "name", podName)
	return pod, nil
}

func (rsc *service) waitForGPUReservationPodAllocation(
	ctx context.Context, nodeName, gpuReservationPodName string,
) string {
	logger := log.FromContext(ctx)
	pods := &v1.PodList{}
	watcher, err := rsc.kubeClient.Watch(
		ctx, pods,
		client.InNamespace(rsc.namespace),
		client.MatchingFields{"metadata.name": gpuReservationPodName},
	)
	if err != nil {
		logger.Error(err, "Failed to watch for GPU reservation pod")
		return unknownGpuIndicator
	}
	defer watcher.Stop()

	timeout := time.After(rsc.allocationTimeout)
	for {
		select {
		case <-ctx.Done():
			logger.Error(ctx.Err(),
				"Context done while waiting for GPU reservation pod to be allocated",
				"nodeName", nodeName, "name", gpuReservationPodName)
			return unknownGpuIndicator
		case <-timeout:
			logger.Error(fmt.Errorf("timeout"),
				"Reached timeout while waiting for GPU reservation pod to be allocated",
				"nodeName", nodeName, "name", gpuReservationPodName)
			return unknownGpuIndicator
		case event, ok := <-watcher.ResultChan():
			if !ok {
				logger.Error(nil,
					"GPU reservation pod watch channel closed while waiting for allocation",
					"nodeName", nodeName, "name", gpuReservationPodName)
				return unknownGpuIndicator
			}

			if event.Type == watch.Error {
				logger.Error(fmt.Errorf("watch error"),
					"Error event received while waiting for GPU reservation pod allocation",
					"nodeName", nodeName, "name", gpuReservationPodName, "event", fmt.Sprintf("%v", event.Object))
				return unknownGpuIndicator
			}

			pod, ok := event.Object.(*v1.Pod)
			if pod.Annotations != nil && pod.Annotations[gpuIndexAnnotationName] != "" {
				return pod.Annotations[gpuIndexAnnotationName]
			}
		}
	}
}

func (rsc *service) createResourceReservationPod(
	nodeName, gpuGroup, podName string, resources v1.ResourceRequirements,
) (*v1.Pod, error) {
	podSpec := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: rsc.namespace,
			Labels: map[string]string{
				constants.AppLabelName: rsc.appLabelValue,
				constants.GPUGroup:     gpuGroup,
			},
			Annotations: map[string]string{
				karpenterv1.DoNotDisruptAnnotationKey: "true",
			},
		},
		Spec: v1.PodSpec{
			NodeName: nodeName,
			RuntimeClassName: func() *string {
				if len(rsc.runtimeClassName) == 0 {
					return nil
				}
				return &rsc.runtimeClassName
			}(),
			SecurityContext:    rsc.podSecurityContext,
			ServiceAccountName: rsc.serviceAccountName,
			Containers: []v1.Container{
				{
					Name:            resourceReservation,
					Image:           rsc.reservationPodImage,
					ImagePullPolicy: v1.PullIfNotPresent,
					Resources:       resources,
					SecurityContext: rsc.containerSecurityContext,
					Env: []v1.EnvVar{
						{
							Name: "POD_NAME",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "metadata.name",
								},
							},
						},
						{
							Name: "POD_NAMESPACE",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "metadata.namespace",
								},
							},
						},
					},
				},
			},
		},
		Status: v1.PodStatus{
			Conditions: []v1.PodCondition{
				{
					Type:   v1.PodScheduled,
					Status: v1.ConditionTrue,
				},
			},
		},
	}

	if rsc.fakeGPuNodes {
		podSpec.Spec.Containers[0].Image = "ubuntu"
		podSpec.Spec.Containers[0].Args = []string{"sleep", "infinity"}
	}

	return podSpec, rsc.kubeClient.Create(context.Background(), podSpec)
}

func (rsc *service) isScalingUp(ctx context.Context) bool {
	logger := log.FromContext(ctx)
	pods := &v1.PodList{}
	err := rsc.kubeClient.List(ctx, pods,
		client.InNamespace(rsc.scalingPodNamespace),
	)
	if err != nil {
		logger.Error(err, "Could not list scaling pods")
		return false
	}

	for _, pod := range pods.Items {
		podStatus := pod.Status
		for _, condition := range podStatus.Conditions {
			if condition.Type == v1.PodScheduled && condition.Status != v1.ConditionTrue {
				logger.Error(fmt.Errorf("not found"),
					"Unscheduled scaling pod was found", "name", pod.Name)
				return true
			}
		}
	}
	return false
}

func IsGPUReservationPod(pod *v1.Pod) bool {
	return strings.HasPrefix(pod.Name, gpuReservationPodPrefix)
}
