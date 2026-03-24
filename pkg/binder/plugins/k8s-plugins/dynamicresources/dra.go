// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dynamicresources

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/status"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	k8splfeature "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	plugins "github.com/NVIDIA/KAI-scheduler/pkg/binder/plugins/k8s-plugins/common"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/resources"
)

type dynamicResourcesPlugin struct {
	client      clientset.Interface
	bindTimeout int64
}

func NewDynamicResourcesPlugin(
	k8sFramework k8sframework.Handle,
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
func (drp *dynamicResourcesPlugin) IsRelevant(pod *corev1.Pod) bool {
	return len(pod.Spec.ResourceClaims) > 0
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

// UnAllocate cleans up Resource Claim allocation
func (drp *dynamicResourcesPlugin) UnAllocate(
	_ context.Context, _ *corev1.Pod, _ string, _ ksf.CycleState,
) {
	return
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
		originalClaim, err := drp.client.ResourceV1().ResourceClaims(pod.Namespace).Get(ctx, claimName, v1.GetOptions{})
		if err != nil {
			return err
		}
		claim := originalClaim.DeepCopy()

		resources.UpsertReservedFor(claim, pod)
		if claim.Status.Allocation == nil {
			claim.Status.Allocation = desiredStatus.Allocation
		}

		_, err = drp.client.ResourceV1().ResourceClaims(pod.Namespace).UpdateStatus(ctx, claim, v1.UpdateOptions{})

		return err
	})

	if err != nil {
		return status.Error(2, fmt.Sprintf("failed to update claim %s for pod %s/%s: %v",
			claimName, pod.Namespace, pod.Name, err))
	}
	return nil
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
