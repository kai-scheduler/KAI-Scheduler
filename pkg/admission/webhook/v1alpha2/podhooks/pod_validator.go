// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podhooks

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kai-scheduler/kai-scheduler/pkg/admission/plugins"
)

// log is for logging in this package.
var validatorlog = logf.Log.WithName("pod-validator")

type PodValidator interface {
	// ValidateCreate validates the object on creation.
	// The optional warnings will be added to the response as warning messages.
	// Return an error if the object is invalid.
	ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error)

	// ValidateUpdate validates the object on update.
	// The optional warnings will be added to the response as warning messages.
	// Return an error if the object is invalid.
	ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (warnings admission.Warnings, err error)

	// ValidateDelete validates the object on deletion.
	// The optional warnings will be added to the response as warning messages.
	// Return an error if the object is invalid.
	ValidateDelete(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error)
}

type podValidator struct {
	kubeClient    client.Client
	plugins       *plugins.KaiAdmissionPlugins
	schedulerName string
}

func NewPodValidator(kubeClient client.Client, plugins *plugins.KaiAdmissionPlugins, schedulerName string) PodValidator {
	return &podValidator{
		kubeClient:    kubeClient,
		plugins:       plugins,
		schedulerName: schedulerName,
	}
}

func (v *podValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	validatorlog.Info("pod validator", "kind", obj.GetObjectKind().GroupVersionKind().Kind)
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("bad object type")
	}
	if pod.Spec.SchedulerName != v.schedulerName {
		return nil, nil
	}

	return nil, v.plugins.Validate(pod)
}

func (v *podValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (
	warnings admission.Warnings, err error) {
	pod, ok := newObj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("bad object type")
	}
	if pod.Spec.SchedulerName != v.schedulerName {
		return nil, nil
	}

	return nil, v.plugins.Validate(pod)
}

func (v *podValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
