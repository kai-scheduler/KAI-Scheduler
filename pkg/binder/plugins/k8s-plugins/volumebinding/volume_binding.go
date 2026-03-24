// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package volumebinding

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ksf "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	k8splfeature "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	plugins "github.com/NVIDIA/KAI-scheduler/pkg/binder/plugins/k8s-plugins/common"
)

type volumeBindingPlugin struct {
	binding *volumebinding.VolumeBinding
}

func NewVolumeBindingPlugin(
	k8sFramework k8sframework.Handle,
	features *k8splfeature.Features,
	bindTimeoutSeconds int64,
) (plugins.K8sPlugin, error) {
	vb, err := volumebinding.New(context.Background(), &config.VolumeBindingArgs{BindTimeoutSeconds: bindTimeoutSeconds}, k8sFramework, *features)
	if err != nil {
		return nil, err
	}
	return &volumeBindingPlugin{
		binding: vb.(*volumebinding.VolumeBinding),
	}, nil
}

func (vb *volumeBindingPlugin) Name() string {
	return "VolumeBinding"
}

// IsRelevant checks if the pod is relevant to the K8sPlugin
func (vb *volumeBindingPlugin) IsRelevant(pod *corev1.Pod) bool {
	return len(pod.Spec.Volumes) > 0
}

// PreFilter fetches pod PVCs and writes them to state
func (vb *volumeBindingPlugin) PreFilter(ctx context.Context, pod *corev1.Pod, state ksf.CycleState) (error, bool) {
	logger := log.FromContext(ctx)
	result, status := vb.binding.PreFilter(ctx, state, pod, nil)
	logger.V(2).Info(fmt.Sprintf("PreFilter for pod %s/%s. result: %v status code: %s",
		pod.Namespace, pod.Name, result, status.Code()))
	return status.AsError(), status.IsSkip()
}

// Filter checks if all of a Pod's PVCs can be satisfied by the node
func (vb *volumeBindingPlugin) Filter(
	ctx context.Context, pod *corev1.Pod, node *corev1.Node, state ksf.CycleState,
) error {
	logger := log.FromContext(ctx)
	nodeInfo := k8sframework.NewNodeInfo()
	nodeInfo.SetNode(node)

	status := vb.binding.Filter(ctx, state, pod, nodeInfo)
	logger.V(2).Info(fmt.Sprintf("Filter for pod %s/%s and node %s. status code: %v",
		pod.Namespace, pod.Name, node.Name, status.Code().String()))

	return status.AsError()
}

// Allocate allocates volume on the host to the task
func (vb *volumeBindingPlugin) Allocate(
	ctx context.Context, pod *corev1.Pod, hostname string, state ksf.CycleState,
) error {
	logger := log.FromContext(ctx)
	status := vb.binding.Reserve(ctx, state, pod, hostname)
	logger.V(2).Info(fmt.Sprintf("Reserve for pod %s/%s and node %s. status code: %v",
		pod.Namespace, pod.Name, hostname, status.Code().String()))
	return status.AsError()
}

// UnAllocate cleans up volume allocation
func (vb *volumeBindingPlugin) UnAllocate(
	ctx context.Context, pod *corev1.Pod, hostname string, state ksf.CycleState,
) {
	logger := log.FromContext(ctx)
	vb.binding.Unreserve(ctx, state, pod, hostname)
	logger.V(2).Info(fmt.Sprintf(
		"VolumeBinding: Unreserve for pod %s/%s and node %s.", pod.Namespace, pod.Name, hostname))
}

// Bind binds volumes to the task
func (vb *volumeBindingPlugin) Bind(
	ctx context.Context, pod *corev1.Pod, request *v1alpha2.BindRequest, state ksf.CycleState,
) error {
	logger := log.FromContext(ctx)
	status := vb.binding.PreBind(ctx, state, pod, request.Spec.SelectedNode)
	logger.V(2).Info(fmt.Sprintf("PreBind for pod %s/%s and node %s. status code: %v",
		pod.Namespace, pod.Name, pod.Spec.NodeName, status.Code().String()))
	return status.AsError()
}

// PostBind is called after binding is done to clean up
func (vb *volumeBindingPlugin) PostBind(_ context.Context, _ *corev1.Pod, _ string, _ ksf.CycleState) {
	// VolumeBinding plugin doesn't implement PostBind
	return
}
