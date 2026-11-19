// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package binding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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

// ErrBindingOutcomeUnknown means retrying must preserve allocations and the
// BindRequest outcome until an earlier binding can no longer commit.
var ErrBindingOutcomeUnknown = errors.New("binding outcome is not yet known")

// Keep confirmation from extending a failed binding request indefinitely.
const bindingConfirmationTimeout = 5 * time.Second

const bindingAttemptAnnotation = "kai.scheduler/binding-attempt"

// AttemptPreparer excludes prior Binding requests before a controller reports
// an authoritative failure that occurs before Bind can run.
type AttemptPreparer interface {
	PrepareBindingAttempt(context.Context, *v1.Pod) error
}

type Binder struct {
	kubeClient                 client.Client
	apiReader                  client.Reader
	resourceReservationService resourcereservation.Interface
	plugins                    *plugins.BinderPlugins
}

func NewBinder(kubeClient client.Client, apiReader client.Reader, rrs resourcereservation.Interface, plugins *plugins.BinderPlugins) *Binder {
	return &Binder{
		kubeClient:                 kubeClient,
		apiReader:                  apiReader,
		resourceReservationService: rrs,
		plugins:                    plugins,
	}
}

// PrepareBindingAttempt makes prior resourceVersion-constrained Binding requests
// stale. A nil apiReader retains the previous pre-Bind failure behavior.
func (b *Binder) PrepareBindingAttempt(ctx context.Context, pod *v1.Pod) error {
	if b.apiReader == nil {
		return nil
	}
	return b.prepareBindingAttempt(ctx, pod, pod.UID)
}

// ForgetBindingState releases bookkeeping after a confirmed lifecycle outcome.
func (b *Binder) ForgetBindingState(requestUID, podUID types.UID) {
	b.plugins.ForgetBindingState(requestUID, podUID)
}

func (b *Binder) Bind(ctx context.Context, pod *v1.Pod, node *v1.Node, bindRequest *v1alpha2.BindRequest) (err error) {
	logger := log.FromContext(ctx)
	intendedUID := pod.UID
	if b.apiReader != nil {
		if err = b.prepareBindingAttempt(ctx, pod, intendedUID); err != nil {
			return err
		}
		// PreBind can fail after an earlier process issued a Binding request. The
		// entry write excludes that request, and cleanup still requires this Pod.
		defer func() {
			if err != nil && !errors.Is(err, ErrBindingOutcomeUnknown) {
				err = b.protectFailedAttempt(ctx, pod, intendedUID, err)
			}
		}()
	}
	err = b.resourceReservationService.SyncForNode(ctx, bindRequest.Spec.SelectedNode)
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
	if b.apiReader != nil && pod.UID != intendedUID {
		return unknownBindingOutcome(fmt.Errorf("pod %s/%s changed identity while reserving GPUs", pod.Namespace, pod.Name))
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
	if b.apiReader != nil && pod.UID != intendedUID {
		return unknownBindingOutcome(fmt.Errorf("pod %s/%s changed identity during PreBind", pod.Namespace, pod.Name))
	}
	if b.apiReader != nil {
		// Plugins can update Pod status through another object, advancing its
		// resourceVersion without updating the Pod passed to PreBind.
		freshPod, readErr := b.readPod(ctx, pod)
		if readErr != nil {
			return unknownBindingOutcome(readErr)
		}
		if freshPod.UID != intendedUID || freshPod.Spec.NodeName != "" || freshPod.ResourceVersion == "" {
			return unknownBindingOutcome(fmt.Errorf("pod %s/%s is no longer the expected unbound Pod after PreBind", pod.Namespace, pod.Name))
		}
		freshPod.DeepCopyInto(pod)
	}

	err = b.patchPodBindingAnnotations(ctx, pod, bindingState)
	if err != nil {
		return fmt.Errorf("failed to patch pod <%s/%s> with bind annotations: %w", pod.Namespace, pod.Name, err)
	}
	if b.apiReader != nil && (pod.UID != intendedUID || pod.Spec.NodeName != "" || pod.ResourceVersion == "") {
		return unknownBindingOutcome(fmt.Errorf("pod %s/%s changed identity or became bound during preparation", pod.Namespace, pod.Name))
	}

	logger.Info("Binding pod", "namespace", pod.Namespace, "name", pod.Name, "hostname", node.Name)
	binding := &v1.Binding{
		ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.Name, UID: intendedUID},
		Target: v1.ObjectReference{
			Kind: "Node",
			Name: node.Name,
		},
	}
	if b.apiReader != nil {
		binding.ResourceVersion = pod.ResourceVersion
	}
	if err = b.kubeClient.SubResource("binding").Create(ctx, pod, binding); err != nil {
		bindErr := fmt.Errorf("failed to bind pod <%s/%s> to node <%s>: %w", pod.Namespace, pod.Name, node.Name, err)
		if b.apiReader == nil {
			return bindErr
		}
		if err = b.resolveBindingError(ctx, pod, intendedUID, node.Name, binding.ResourceVersion, bindErr); err != nil {
			return err
		}
		logger.Info("Pod binding confirmed after API request returned an error",
			"namespace", pod.Namespace, "name", pod.Name, "hostname", node.Name, "error", bindErr)
	}

	b.plugins.PostBind(ctx, pod, node, bindRequest, bindingState)
	return nil
}

func unknownBindingOutcome(err error) error {
	return fmt.Errorf("%w: %w", ErrBindingOutcomeUnknown, err)
}

func (b *Binder) readPod(ctx context.Context, pod *v1.Pod) (*v1.Pod, error) {
	ctx, cancel := context.WithTimeout(ctx, bindingConfirmationTimeout)
	defer cancel()
	freshPod := &v1.Pod{}
	if err := b.apiReader.Get(ctx, client.ObjectKeyFromObject(pod), freshPod); err != nil {
		return nil, err
	}
	return freshPod, nil
}

func (b *Binder) prepareBindingAttempt(ctx context.Context, pod *v1.Pod, intendedUID types.UID) error {
	freshPod, err := b.readPod(ctx, pod)
	if err != nil {
		return unknownBindingOutcome(err)
	}
	if intendedUID == "" || freshPod.UID != intendedUID || freshPod.Spec.NodeName != "" || freshPod.ResourceVersion == "" {
		// A newly observed bound Pod must pass the controller's placement checks,
		// including GPU groups, before it can be reported as successful.
		return unknownBindingOutcome(fmt.Errorf("pod %s/%s is no longer the expected unbound Pod", pod.Namespace, pod.Name))
	}
	if err := b.advanceBindingAttempt(ctx, freshPod); err != nil {
		return unknownBindingOutcome(err)
	}
	freshPod.DeepCopyInto(pod)
	return nil
}

func (b *Binder) advanceBindingAttempt(ctx context.Context, pod *v1.Pod) error {
	uid, previousVersion := pod.UID, pod.ResourceVersion
	attempt := uuid.NewString()
	patchBytes, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"uid": uid, "resourceVersion": previousVersion,
			"annotations": map[string]string{bindingAttemptAnnotation: attempt},
		},
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, bindingConfirmationTimeout)
	defer cancel()
	if err := b.kubeClient.Patch(ctx, pod, client.RawPatch(types.MergePatchType, patchBytes)); err != nil {
		return err
	}
	if pod.UID != uid || pod.Spec.NodeName != "" || pod.ResourceVersion == "" ||
		pod.ResourceVersion == previousVersion || pod.Annotations[bindingAttemptAnnotation] != attempt {
		return fmt.Errorf("pod %s/%s did not acknowledge a new unbound binding attempt", pod.Namespace, pod.Name)
	}
	return nil
}

func (b *Binder) resolveBindingError(ctx context.Context, pod *v1.Pod, intendedUID types.UID,
	nodeName, bindingVersion string, bindErr error) error {
	freshPod, err := b.readPod(ctx, pod)
	if err != nil {
		return unknownBindingOutcome(errors.Join(bindErr, err))
	}
	if freshPod.UID != intendedUID || (freshPod.Spec.NodeName != "" && freshPod.Spec.NodeName != nodeName) {
		return unknownBindingOutcome(bindErr)
	}
	if nodeName != "" && freshPod.Spec.NodeName == nodeName {
		freshPod.DeepCopyInto(pod)
		return nil
	}
	if freshPod.ResourceVersion == "" {
		return unknownBindingOutcome(bindErr)
	}
	if freshPod.ResourceVersion == bindingVersion {
		// Reading an unbound Pod alone does not exclude a delayed Binding. A
		// changing conditional write makes its resourceVersion precondition stale.
		if err := b.advanceBindingAttempt(ctx, freshPod); err != nil {
			// The write response may itself be lost. A subsequent authoritative read
			// can still prove either completion or exclusion of the Binding.
			observed, readErr := b.readPod(ctx, pod)
			if readErr != nil || observed.UID != intendedUID {
				return unknownBindingOutcome(errors.Join(bindErr, err, readErr))
			}
			if nodeName != "" && observed.Spec.NodeName == nodeName {
				observed.DeepCopyInto(pod)
				return nil
			}
			if observed.Spec.NodeName != "" || observed.ResourceVersion == "" || observed.ResourceVersion == bindingVersion {
				return unknownBindingOutcome(errors.Join(bindErr, err))
			}
			freshPod = observed
		}
	}
	freshPod.DeepCopyInto(pod)
	return bindErr
}

func (b *Binder) protectFailedAttempt(ctx context.Context, pod *v1.Pod, intendedUID types.UID, bindErr error) error {
	freshPod, err := b.readPod(ctx, pod)
	if err != nil {
		return unknownBindingOutcome(errors.Join(bindErr, err))
	}
	if freshPod.UID != intendedUID || freshPod.Spec.NodeName != "" {
		return unknownBindingOutcome(bindErr)
	}
	freshPod.DeepCopyInto(pod)
	return bindErr
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
	metadata := map[string]interface{}{"annotations": bindingState.BindingPodAnnotations}
	if b.apiReader != nil {
		metadata["uid"] = pod.UID
		metadata["resourceVersion"] = pod.ResourceVersion
	}
	patchBytes, err := json.Marshal(map[string]interface{}{"metadata": metadata})
	if err != nil {
		return err
	}

	return b.kubeClient.Patch(ctx, pod, client.RawPatch(types.MergePatchType, patchBytes))
}
