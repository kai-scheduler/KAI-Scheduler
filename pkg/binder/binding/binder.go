// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
)

var InvalidCrdWarning = errors.New("invalid binding request")

type Binder struct {
	kubeClient                 client.Client
	resourceReservationService resourcereservation.Interface
	plugins                    *plugins.BinderPlugins
}

func NewBinder(kubeClient client.Client, rrs resourcereservation.Interface, plugins *plugins.BinderPlugins) *Binder {
	return &Binder{
		kubeClient:                 kubeClient,
		resourceReservationService: rrs,
		plugins:                    plugins,
	}
}

func (b *Binder) Bind(ctx context.Context, pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest) error {
	logger := log.FromContext(ctx)
	err := b.resourceReservationService.SyncForNode(ctx, bindRequest.Spec.SelectedNode)
	if err != nil {
		logger.Error(err, "Failed to sync reservation for node",
			"pod", pod.Name, "namespace", pod.Namespace, "node", node.Name)
		return err
	}
	var reservedGPUIds []string
	if common.IsSharedGPUAllocation(bindRequest) {
		reservedGPUIds, err = b.reserveGPUs(ctx, pod, bindRequest)
		if err != nil {
			logger.Error(err, "Failed to reserve GPUs resources for pod",
				"namespace", pod.Namespace, "name", pod.Name)
			return err
		}
	}
	bindingState := &state.BindingState{
		ReservedGPUIds: reservedGPUIds,
	}

	err = b.plugins.PreBind(ctx, pod, node, bindRequest, bindingState)
	if err != nil {
		return err
	}

	err = b.patchResourceReceivedTypeAnnotation(ctx, pod, bindRequest)
	if err != nil {
		logger.Error(err, "Failed to patch pod with resource receive type",
			"namespace", pod.Namespace, "name", pod.Name)
		return err
	}

	logger.Info("Binding pod", "namespace", pod.Namespace, "name", pod.Name, "hostname", node.Name)
	binding := &v1.Binding{
		ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name, UID: pod.UID},
		Target: v1.ObjectReference{
			Kind: "Node",
			Name: node.Name,
		},
	}
	if err = b.kubeClient.SubResource("binding").Create(ctx, pod, binding); err != nil {
		logger.Error(err, "Failed to bind pod", "namespace", pod.Namespace, "name", pod.Name)
		return err
	}

	b.plugins.PostBind(ctx, pod, node, bindRequest, bindingState)
	return nil
}

func (b *Binder) reserveGPUs(ctx context.Context, pod *v1.Pod, bindRequest *v1alpha2.BindRequest) ([]string, error) {
	if len(bindRequest.Spec.SelectedGPUGroups) == 0 {
		// Old bindingRequest bad conversion. delete the binding request.
		return nil, fmt.Errorf("no SelectedGPUGroups for fractional pod: %w", InvalidCrdWarning)
	}

	var gpuIndexes []string
	for groupListIndex, gpuGroup := range bindRequest.Spec.SelectedGPUGroups {
		gpuIndex, err := b.resourceReservationService.ReserveGpuDevice(ctx, pod, bindRequest.Spec.SelectedNode, gpuGroup)
		if err != nil {
			logger := log.FromContext(ctx)
			logger.Error(err, "Failed to sync resource reservation pods",
				"nodeName", bindRequest.Spec.SelectedNode)

			// Try to clean up any previously created reservation pods created before this failure
			if groupListIndex > 0 {
				b.cleanPreviousReservationPods(ctx, pod, groupListIndex, bindRequest)
			}
			return nil, err
		}
		gpuIndexes = append(gpuIndexes, gpuIndex)
	}
	return gpuIndexes, nil
}

func (b *Binder) cleanPreviousReservationPods(
	ctx context.Context, pod *v1.Pod, failedGpuReservationIndex int, bindRequest *v1alpha2.BindRequest) {
	logger := log.FromContext(ctx)
	for gpuGroupToCleanIndex := range failedGpuReservationIndex {
		gpuGroupToClean := bindRequest.Spec.SelectedGPUGroups[gpuGroupToCleanIndex]
		cleanupErr := b.resourceReservationService.RemovePodGpuGroupConnection(ctx, pod, gpuGroupToClean)
		if cleanupErr != nil {
			logger.Error(cleanupErr, "Failed to remove reservation pod connection to pod under fraction binding.",
				"nodeName", bindRequest.Spec.SelectedNode)
		}
	}
	cleanupErr := b.resourceReservationService.SyncForNode(ctx, bindRequest.Spec.SelectedNode)
	if cleanupErr != nil {
		logger.Error(cleanupErr, "Failed to sync resource reservation pods for node.",
			"nodeName", bindRequest.Spec.SelectedNode)
	}
}

func (b *Binder) patchResourceReceivedTypeAnnotation(ctx context.Context, pod *v1.Pod, bindRequest *v1alpha2.BindRequest) error {
	patchBytes, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]string{
				constants.ReceivedResourceType: bindRequest.Spec.ReceivedResourceType,
			},
		},
	})
	if err != nil {
		return err
	}

	err = b.kubeClient.Patch(ctx, pod, client.RawPatch(types.MergePatchType, patchBytes))
	return err
}
