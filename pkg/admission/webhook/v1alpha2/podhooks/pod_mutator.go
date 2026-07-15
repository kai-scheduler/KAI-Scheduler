// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podhooks

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/plugins"
)

// log is for logging in this package.
var mutatorlog = logf.Log.WithName("pod-mutator")

type PodMutator interface {
	Default(ctx context.Context, obj *corev1.Pod) error
}

type podMutator struct {
	kubeClient    client.Client
	plugins       *plugins.KaiAdmissionPlugins
	schedulerName string
}

func NewPodMutator(kubeClient client.Client, plugins *plugins.KaiAdmissionPlugins, schedulerName string) PodMutator {
	return &podMutator{
		kubeClient:    kubeClient,
		plugins:       plugins,
		schedulerName: schedulerName,
	}
}

func (cpm *podMutator) Default(ctx context.Context, pod *corev1.Pod) error {
	mutatorlog.Info("customDefaulter", "kind", pod.GetObjectKind().GroupVersionKind().Kind)

	if pod.Spec.SchedulerName != cpm.schedulerName {
		return nil
	}

	namespace, err := extractMutatingPodTargetNamespace(ctx, pod)
	if err != nil {
		return err
	}
	pod.Namespace = namespace

	return cpm.plugins.Mutate(pod)
}

func extractMutatingPodTargetNamespace(ctx context.Context, pod *corev1.Pod) (string, error) {
	admissionRequest, err := admission.RequestFromContext(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to extract admissionRequest for pod %s ",
			pod.Name)
	}

	if pod.Namespace != "" && admissionRequest.Namespace != "" && pod.Namespace != admissionRequest.Namespace {
		return "", fmt.Errorf(
			"error: the namespace from the provided object %s does not match the namespace %s",
			pod.Namespace, admissionRequest.Namespace)
	}

	if pod.Namespace != "" {
		return pod.Namespace, nil
	}

	return admissionRequest.Namespace, nil
}
