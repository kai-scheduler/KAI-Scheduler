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
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
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
		return fmt.Errorf("failed to sync reservation for pod <%s/%s> on node <%s>: %w", pod.Namespace, pod.Name, bindRequest.Spec.SelectedNode, err)
	}

	var reservedGPUIds []string
	if common.IsSharedGPUAllocation(bindRequest) {
		reservedGPUIds, err = b.reserveGPUs(ctx, pod, bindRequest)
		if err != nil {
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
		return fmt.Errorf("failed to patch pod <%s/%s> with resource receive type annotation: %w", pod.Namespace, pod.Name, err)
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
		return fmt.Errorf("failed to bind pod <%s/%s> to node <%s>: %w", pod.Namespace, pod.Name, node.Name, err)
	}

	b.plugins.PostBind(ctx, pod, node, bindRequest, bindingState)
	return nil
}

func (b *Binder) Rollback(ctx context.Context, pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest) error {
	logger := log.FromContext(ctx)

	logger.Info("Rolling back for failed bind attempt...",
		"pod", pod.Name, "namespace", pod.Namespace, "node", node.Name)

	var rollbackErrs []error

	if err := b.plugins.Rollback(ctx, pod, node, bindRequest, nil); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("failed to rollback plugins for pod <%s/%s>: %w", pod.Namespace, pod.Name, err))
	}

	if common.IsSharedGPUAllocation(bindRequest) {
		if err := b.resourceReservationService.RemovePodGpuGroupsConnection(ctx, pod); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("failed to remove GPU group label from pod <%s/%s> during rollback: %w", pod.Namespace, pod.Name, err))
		}

		if err := b.resourceReservationService.SyncForNode(ctx, bindRequest.Spec.SelectedNode); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("failed to sync reservation pods for node <%s> during rollback: %w", bindRequest.Spec.SelectedNode, err))
		}
	}

	return errors.Join(rollbackErrs...)
}

func (b *Binder) reserveGPUs(ctx context.Context, pod *v1.Pod, bindRequest *v1alpha2.BindRequest) ([]string, error) {
	if len(bindRequest.Spec.SelectedGPUGroups) == 0 {
		// Old bindingRequest bad conversion. delete the binding request.
		return nil, fmt.Errorf("no SelectedGPUGroups for fractional pod: %w", InvalidCrdWarning)
	}

	var gpuIndexes []string
	for _, gpuGroup := range bindRequest.Spec.SelectedGPUGroups {
		gpuIndex, err := b.resourceReservationService.ReserveGpuDevice(ctx, pod, bindRequest.Spec.SelectedNode, gpuGroup)
		if err != nil {
			// Cleanup will be handled by the rollback function

			return nil, fmt.Errorf("failed to reserve GPUs for pod <%s/%s> in gpu group <%s>: %w", pod.Namespace, pod.Name, gpuGroup, err)
		}
		gpuIndexes = append(gpuIndexes, gpuIndex)
	}
	return gpuIndexes, nil
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
