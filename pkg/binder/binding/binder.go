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

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
	"github.com/kai-scheduler/api/constants"
	"github.com/kai-scheduler/api/scheduling/v1alpha2"
)

var InvalidCrdWarning = errors.New("invalid binding request")

type Binder struct {
	kubeClient                 client.Client
	podReader                  client.Reader
	resourceReservationService resourcereservation.Interface
	plugins                    *plugins.BinderPlugins
}

func NewBinder(kubeClient client.Client, rrs resourcereservation.Interface, plugins *plugins.BinderPlugins) *Binder {
	return &Binder{
		kubeClient:                 kubeClient,
		podReader:                  kubeClient,
		resourceReservationService: rrs,
		plugins:                    plugins,
	}
}

// SetPodReader selects the reader used to confirm placement after a binding error.
// The binder client reads pods from the informer cache, which can still show a pod
// as unassigned after the apiserver has stored the binding. Pass an API reader.
func (b *Binder) SetPodReader(reader client.Reader) {
	if reader != nil {
		b.podReader = reader
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
		BindingPodAnnotations: map[string]string{
			constants.ReceivedResourceType: bindRequest.Spec.ReceivedResourceType,
		},
		ReservedGPUIds: reservedGPUIds,
	}
	if len(bindRequest.Spec.PredictedNUMAZones) > 0 {
		placement, err := json.Marshal(bindRequest.Spec.PredictedNUMAZones)
		if err != nil {
			return err
		}
		bindingState.BindingPodAnnotations[constants.NumaPlacementPredicted] = string(placement)
	}
	if bindRequest.Spec.PredictedNUMAMemoryGroups != nil {
		groups, err := json.Marshal(bindRequest.Spec.PredictedNUMAMemoryGroups)
		if err != nil {
			return err
		}
		bindingState.BindingPodAnnotations[constants.NumaMemoryGroupsPredicted] = string(groups)
	}

	err = b.plugins.PreBind(ctx, pod, node, bindRequest, bindingState)
	if err != nil {
		return err
	}

	err = b.patchPodBindingAnnotations(ctx, pod, bindingState)
	if err != nil {
		return fmt.Errorf("failed to patch pod <%s/%s> with bind annotations: %w", pod.Namespace, pod.Name, err)
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
		// The apiserver can commit the assignment and still drop the response.
		// Cleaning up after that removes settings the assigned Pod still needs.
		if b.podHasPlacement(ctx, pod, node.Name) {
			logger.Info("Binding response lost after the pod was assigned; keeping the placement",
				"namespace", pod.Namespace, "name", pod.Name, "node", node.Name, "error", err)
			b.plugins.PostBind(ctx, pod, node, bindRequest, bindingState)
			return nil
		}
		return fmt.Errorf("failed to bind pod <%s/%s> to node <%s>: %w", pod.Namespace, pod.Name, node.Name, err)
	}

	b.plugins.PostBind(ctx, pod, node, bindRequest, bindingState)
	return nil
}

// podHasPlacement reports whether the apiserver has assigned this pod to nodeName.
// The in-memory pod and the informer cache can both lag that write.
func (b *Binder) podHasPlacement(ctx context.Context, pod *v1.Pod, nodeName string) bool {
	logger := log.FromContext(ctx)
	latest := &v1.Pod{}
	if err := b.podReader.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, latest); err != nil {
		logger.Error(err, "Failed to read pod after binding error",
			"namespace", pod.Namespace, "name", pod.Name)
		return false
	}
	if latest.UID != pod.UID {
		logger.Info("Pod identity changed after binding error",
			"namespace", pod.Namespace, "name", pod.Name,
			"expectedUID", pod.UID, "actualUID", latest.UID)
		return false
	}
	if latest.Spec.NodeName == nodeName {
		return true
	}
	if latest.Spec.NodeName != "" {
		logger.Info("Pod is assigned to an unexpected node after binding error",
			"namespace", pod.Namespace, "name", pod.Name,
			"expectedNode", nodeName, "actualNode", latest.Spec.NodeName)
	}
	return false
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
	fractionalGpuGroups := bindRequest.Spec.SelectedFractionalGpuGroupsOrDefault()
	if len(fractionalGpuGroups) == 0 {
		// Old bindingRequest bad conversion. delete the binding request.
		return nil, fmt.Errorf("no selected GPU groups for fractional pod: %w", InvalidCrdWarning)
	}

	var gpuIndexes []string
	for _, fractionalGpuGroup := range fractionalGpuGroups {
		gpuIndex, err := b.resourceReservationService.ReserveGpuDevice(
			ctx, pod, bindRequest.Spec.SelectedNode, fractionalGpuGroup)
		if err != nil {
			// Cleanup will be handled by the rollback function

			return nil, fmt.Errorf(
				"failed to reserve GPUs for pod <%s/%s> in gpu group <%s>: %w",
				pod.Namespace, pod.Name, fractionalGpuGroup.ID, err)
		}
		gpuIndexes = append(gpuIndexes, gpuIndex)
	}
	return gpuIndexes, nil
}

func (b *Binder) patchPodBindingAnnotations(ctx context.Context, pod *v1.Pod, bindingState *state.BindingState) error {
	patchBytes, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": bindingState.BindingPodAnnotations,
		},
	})
	if err != nil {
		return err
	}

	return b.kubeClient.Patch(ctx, pod, client.RawPatch(types.MergePatchType, patchBytes))
}
