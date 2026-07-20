// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/status"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	ksf "k8s.io/kube-scheduler/framework"
	k8splfeature "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	plugins "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/k8s-plugins/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

type dynamicResourcesPlugin struct {
	client      clientset.Interface
	bindTimeout int64
}

func NewDynamicResourcesPlugin(
	k8sFramework ksf.Handle,
	_ *k8splfeature.Features,
	bindTimeoutSeconds int64,
) (plugins.K8sPlugin, error) {
	return &dynamicResourcesPlugin{
		client:      k8sFramework.ClientSet(),
		bindTimeout: bindTimeoutSeconds,
	}, nil
}

func (drp *dynamicResourcesPlugin) Name() string {
	return "DynamicResources"
}

// IsRelevant checks if the pod is relevant to the K8sPlugin
func (drp *dynamicResourcesPlugin) IsRelevant(pod *corev1.Pod, request *v1alpha2.BindRequest) bool {
	return len(pod.Spec.ResourceClaims) > 0 || (request != nil && request.Spec.ExtendedResourceClaimAllocation != nil)
}

// PreFilter fetches pod Resource Claims and writes them to state, checking if the pod can be scheduled
func (drp *dynamicResourcesPlugin) PreFilter(_ context.Context, _ *corev1.Pod, _ ksf.CycleState) (error, bool) {
	return nil, false
}

// Filter checks if all of a Pod's Resource Claims can be satisfied by the node
func (drp *dynamicResourcesPlugin) Filter(
	_ context.Context, _ *corev1.Pod, _ *corev1.Node, _ ksf.CycleState) error {
	return nil
}

// Allocate allocates Resource Claims for the task when needed
func (drp *dynamicResourcesPlugin) Allocate(
	_ context.Context, _ *corev1.Pod, _ string, _ ksf.CycleState,
) error {
	return nil
}

// UnAllocate deletes the extended-resource claim created during Bind, if any.
func (drp *dynamicResourcesPlugin) UnAllocate(ctx context.Context, pod *corev1.Pod, _ string, _ ksf.CycleState) {
	claim, err := drp.findExtendedResourceClaim(ctx, pod)
	if err != nil || claim == nil {
		return
	}
	_ = drp.client.ResourceV1().ResourceClaims(pod.Namespace).Delete(ctx, claim.Name, metav1.DeleteOptions{})
}

// Bind binds Resource Claims to the task according to the allocation status from the bind request
func (drp *dynamicResourcesPlugin) Bind(
	parentCtx context.Context, pod *corev1.Pod, request *v1alpha2.BindRequest, _ ksf.CycleState,
) error {
	ctx, cancelFn := context.WithTimeout(parentCtx, time.Duration(drp.bindTimeout)*time.Second)
	defer cancelFn()

	for _, claimStatus := range request.Spec.ResourceClaimAllocations {
		err := drp.bindResourceClaim(ctx, &claimStatus, pod)
		if err != nil {
			return err
		}
	}

	if alloc := request.Spec.ExtendedResourceClaimAllocation; alloc != nil {
		if err := drp.bindExtendedResourceClaim(ctx, alloc, pod); err != nil {
			return err
		}
	}

	return nil
}

func (drp *dynamicResourcesPlugin) bindResourceClaim(ctx context.Context, desiredStatus *v1alpha2.ResourceClaimAllocation, pod *corev1.Pod) error {
	if desiredStatus.Allocation == nil {
		return status.Errorf(2, "empty status for claim %s in bind request for pod %s/%s",
			desiredStatus.Name, pod.Namespace, pod.Name)
	}

	claimName, err := getClaimName(pod, desiredStatus.Name)
	if err != nil {
		return status.Error(2, fmt.Sprintf("failed to get claim %s name for pod %s/%s: %v",
			desiredStatus.Name, pod.Namespace, pod.Name, err))
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		originalClaim, err := drp.client.ResourceV1().ResourceClaims(pod.Namespace).Get(ctx, claimName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		claim := originalClaim.DeepCopy()

		resources.UpsertReservedFor(claim, pod)
		if claim.Status.Allocation == nil {
			claim.Status.Allocation = desiredStatus.Allocation
		}

		_, err = drp.client.ResourceV1().ResourceClaims(pod.Namespace).UpdateStatus(ctx, claim, metav1.UpdateOptions{})

		return err
	})

	if err != nil {
		return status.Error(2, fmt.Sprintf("failed to update claim %s for pod %s/%s: %v",
			claimName, pod.Namespace, pod.Name, err))
	}
	return nil
}

func (drp *dynamicResourcesPlugin) bindExtendedResourceClaim(
	ctx context.Context,
	alloc *v1alpha2.ExtendedResourceClaimAllocation,
	pod *corev1.Pod,
) error {
	// Idempotency: if claim already exists (e.g., from a prior partial bind), skip creation.
	existing, err := drp.findExtendedResourceClaim(ctx, pod)
	if err != nil {
		return fmt.Errorf("failed to check for existing extended resource claim for pod %s/%s: %w",
			pod.Namespace, pod.Name, err)
	}

	var claimName string
	if existing != nil {
		claimName = existing.Name
	} else {
		claim := &resourceapi.ResourceClaim{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: pod.Name + "-extended-resources-",
				Namespace:    pod.Namespace,
				Finalizers:   []string{resourceapi.Finalizer},
				Annotations: map[string]string{
					resourceapi.ExtendedResourceClaimAnnotation: "true",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "v1",
						Kind:               "Pod",
						Name:               pod.Name,
						UID:                pod.UID,
						Controller:         ptr.To(true),
						BlockOwnerDeletion: ptr.To(true),
					},
				},
			},
			Spec: resourceapi.ResourceClaimSpec{
				Devices: resourceapi.DeviceClaim{
					Requests: alloc.DeviceRequests,
				},
			},
		}
		created, err := drp.client.ResourceV1().ResourceClaims(pod.Namespace).Create(ctx, claim, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create extended resource claim for pod %s/%s: %w",
				pod.Namespace, pod.Name, err)
		}
		claimName = created.Name
	}

	// Set allocation and ReservedFor on the claim status.
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := drp.client.ResourceV1().ResourceClaims(pod.Namespace).Get(ctx, claimName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		updated := current.DeepCopy()
		if updated.Status.Allocation == nil {
			updated.Status.Allocation = alloc.Allocation
		}
		resources.UpsertReservedFor(updated, pod)
		_, err = drp.client.ResourceV1().ResourceClaims(pod.Namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to update status of extended resource claim %s for pod %s/%s: %w",
			claimName, pod.Namespace, pod.Name, err)
	}

	// Record the claim name in pod status so the kubelet can inject devices.
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := drp.client.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		updated := current.DeepCopy()
		updated.Status.ExtendedResourceClaimStatus = &corev1.PodExtendedResourceClaimStatus{
			ResourceClaimName: claimName,
			RequestMappings:   alloc.ContainerMappings,
		}
		_, err = drp.client.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to update pod status for extended resource claim for pod %s/%s: %w",
			pod.Namespace, pod.Name, err)
	}

	return nil
}

func (drp *dynamicResourcesPlugin) findExtendedResourceClaim(ctx context.Context, pod *corev1.Pod) (*resourceapi.ResourceClaim, error) {
	claims, err := drp.client.ResourceV1().ResourceClaims(pod.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.Annotations[resourceapi.ExtendedResourceClaimAnnotation] != "true" {
			continue
		}
		for _, or_ := range claim.OwnerReferences {
			if or_.Name == pod.Name && or_.UID == pod.UID && or_.Controller != nil && *or_.Controller {
				return claim, nil
			}
		}
	}
	return nil, nil
}

func getClaimName(pod *corev1.Pod, podClaimName string) (string, error) {
	var claimName string
	for _, c := range pod.Spec.ResourceClaims {
		if c.Name == podClaimName {
			var err error
			claimName, err = resources.GetResourceClaimName(pod, &c)
			if err != nil {
				return "", err
			}
			break
		}
	}

	if claimName == "" {
		return "", status.Error(2, fmt.Sprintf("claim %s from bind request not found in pod %s/%s",
			podClaimName, pod.Namespace, pod.Name))
	}

	return claimName, nil
}

// PostBind is called after binding is done to clean up
func (drp *dynamicResourcesPlugin) PostBind(
	ctx context.Context, _ *corev1.Pod, _ string, _ ksf.CycleState,
) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("dynamicResourcesPlugin.PostBind called - noop")
}
