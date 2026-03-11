// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"context"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	enginev2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
)

func (su *defaultStatusUpdater) Run(stopCh <-chan struct{}) {
	for i := 0; i < su.numberOfWorkers; i++ {
		go su.updateWorker(stopCh)
	}
	go su.queueBufferWorker(stopCh)
}

func (su *defaultStatusUpdater) keyForPodGroupPayload(name, namespace string, uid types.UID) updatePayloadKey {
	return updatePayloadKey(types.NamespacedName{Name: name, Namespace: namespace}.String() + "_" + string(uid))
}

func (su *defaultStatusUpdater) keyForPodStatusPayload(name, namespace string, uid types.UID) updatePayloadKey {
	return updatePayloadKey(types.NamespacedName{Name: name, Namespace: namespace}.String() + "_" + string(uid) + "-Status")
}

func (su *defaultStatusUpdater) keyForPodLabelsPayload(name, namespace string, uid types.UID) updatePayloadKey {
	return updatePayloadKey(types.NamespacedName{Name: name, Namespace: namespace}.String() + "_" + string(uid) + "-Labels")
}

func (su *defaultStatusUpdater) processPayload(ctx context.Context, payload *updatePayload) {
	updateData, found := su.loadInflightUpdate(payload)
	if !found {
		return
	}

	switch payload.objectType {
	case podType:
		su.updatePod(ctx, payload.key, updateData.patchData, updateData.subResources, updateData.object)
	case podGroupType:
		su.updatePodGroup(ctx, payload.key, updateData)
	}
}

func (su *defaultStatusUpdater) loadInflightUpdate(payload *updatePayload) (*inflightUpdate, bool) {
	var data any
	var found bool
	switch {
	case payload.objectType == podType:
		data, found = su.inFlightPods.Load(payload.key)
	case payload.objectType == podGroupType:
		data, found = su.inFlightPodGroups.Load(payload.key)
	}

	if !found {
		return nil, false
	}
	return data.(*inflightUpdate), true
}

func (su *defaultStatusUpdater) updatePod(
	ctx context.Context, key updatePayloadKey, patchData []byte, subResources []string, object runtime.Object,
) {
	pod := object.(*v1.Pod)
	_, err := su.kubeClient.CoreV1().Pods(pod.Namespace).Patch(
		ctx, pod.Name, types.StrategicMergePatchType, patchData, metav1.PatchOptions{}, subResources...,
	)

	if err != nil {
		log.StatusUpdaterLogger.V(1).Errorf("Failed to patch pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return
	}

	su.inFlightPods.Delete(key)
}

// +kubebuilder:rbac:groups="scheduling.run.ai",resources=podgroups,verbs=update;patch
// +kubebuilder:rbac:groups="scheduling.run.ai",resources=podgroups/status,verbs=create;delete;update;patch;get;list;watch

func (su *defaultStatusUpdater) updatePodGroup(
	ctx context.Context, key updatePayloadKey, updateData *inflightUpdate,
) {
	podGroup := updateData.object.(*enginev2alpha2.PodGroup)

	var statusErr, patchErr error
	if updateData.updateStatus {
		_, statusErr = su.kaiClient.SchedulingV2alpha2().PodGroups(podGroup.Namespace).UpdateStatus(
			ctx, podGroup, metav1.UpdateOptions{},
		)
	}

	if len(updateData.patchData) > 0 {
		_, patchErr = su.kaiClient.SchedulingV2alpha2().PodGroups(podGroup.Namespace).Patch(
			ctx, podGroup.Name, types.JSONPatchType, updateData.patchData, metav1.PatchOptions{}, updateData.subResources...,
		)
	}

	if statusErr != nil || patchErr != nil {

		if statusErr != nil {
			log.StatusUpdaterLogger.V(1).Errorf("Failed to update pod group status %s/%s: %v",
				podGroup.Namespace, podGroup.Name, statusErr)
			if apierrors.IsConflict(statusErr) {
				// Don't retry this update if the resource version is outdated - The status update cannot be updated with the given object.
				// If a pod group status update is required (e.g. a scheduling condition) a new status update with an updated object
				//  will be enqueued in the next scheduling cycle.
				su.inFlightPodGroups.Delete(key)
				return
			}
		}
		if patchErr != nil {
			log.StatusUpdaterLogger.V(1).Errorf("Failed to patch pod group %s/%s: %v",
				podGroup.Namespace, podGroup.Name, patchErr)
		}

		su.pushToUpdateQueue(
			&updatePayload{
				key:        key,
				objectType: podGroupType,
			},
			updateData,
		)

		return
	}

	// Move the update to the applied cache
	su.appliedPodGroupUpdates.Store(key, updateData)
	_, loaded := su.inFlightPodGroups.LoadAndDelete(key)
	if !loaded {
		su.appliedPodGroupUpdates.Delete(key)
	}
}

func (su *defaultStatusUpdater) updateInFlightObject(key updatePayloadKey, objectType string, object *inflightUpdate) {
	switch objectType {
	case podType:
		su.inFlightPods.Store(key, object)
	case podGroupType:
		su.inFlightPodGroups.Store(key, object)
	}
}

func (su *defaultStatusUpdater) pushToUpdateQueue(payload *updatePayload, update *inflightUpdate) {
	su.updateInFlightObject(payload.key, payload.objectType, update)
	su.updateQueueIn <- payload
}

func (su *defaultStatusUpdater) queueBufferWorker(stopCh <-chan struct{}) {
	for {
		if len(su.updateQueueBuffer) == 0 {
			select {
			case <-stopCh:
				return
			case payload := <-su.updateQueueIn:
				su.updateQueueBuffer = append(su.updateQueueBuffer, payload)
			}
		} else {
			select {
			case <-stopCh:
				return
			case payload := <-su.updateQueueIn:
				su.updateQueueBuffer = append(su.updateQueueBuffer, payload)
			case su.updateQueueOut <- su.updateQueueBuffer[0]:
				su.updateQueueBuffer = su.updateQueueBuffer[1:]
			}
		}
	}
}

func (su *defaultStatusUpdater) updateWorker(stopCh <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	for {
		select {
		case <-stopCh:
			cancel()
			return
		case payload := <-su.updateQueueOut:
			su.processPayload(ctx, payload)
		}
	}
}
