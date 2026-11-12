// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"errors"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/plugins"
	"github.com/kai-scheduler/api/constants"
)

// Values of the webhook label.
const (
	WebhookPodMutator         = "pod-mutator"
	WebhookPodValidator       = "pod-validator"
	WebhookPodResizeValidator = "pod-resize-validator"
	WebhookTopologyValidator  = "topology-validator"
)

// Rejection reasons used when the rejection does not come from an admission plugin.
// Rejections by a plugin use the plugin's name as the reason.
const (
	ReasonMutationFailed   = "mutation_failed"
	ReasonValidationFailed = "validation_failed"
	ReasonResizeRejected   = "resize_rejected"
	ReasonInvalidTopology  = "invalid_topology"
)

const (
	operationCreate = "create"
	operationUpdate = "update"
	operationDelete = "delete"

	resultAllowed  = "allowed"
	resultRejected = "rejected"
)

var (
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: constants.DefaultMetricsNamespace,
			Name:      "admission_requests_total",
			Help:      "Total number of admission requests handled by the KAI webhooks, by webhook, operation and result",
		}, []string{"webhook", "operation", "result"})

	rejectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: constants.DefaultMetricsNamespace,
			Name:      "admission_rejections_total",
			Help:      "Total number of admission requests rejected by the KAI webhooks, by webhook and reason",
		}, []string{"webhook", "reason"})
)

func init() {
	metrics.Registry.MustRegister(requestsTotal, rejectionsTotal)
}

// observe records the outcome of a handled admission request. A non-nil err means the request was rejected.
// The rejection reason is the name of the admission plugin that rejected it, or defaultReason when the
// rejection does not come from a plugin.
func observe(webhook, operation, defaultReason string, err error) {
	if err == nil {
		requestsTotal.WithLabelValues(webhook, operation, resultAllowed).Inc()
		return
	}

	requestsTotal.WithLabelValues(webhook, operation, resultRejected).Inc()
	reason := defaultReason
	var pluginErr *plugins.PluginError
	if errors.As(err, &pluginErr) {
		reason = pluginErr.Plugin
	}
	rejectionsTotal.WithLabelValues(webhook, reason).Inc()
}

type instrumentedDefaulter[T runtime.Object] struct {
	webhook       string
	defaultReason string
	inner         admission.Defaulter[T]
}

// InstrumentDefaulter wraps a mutating webhook so that every request it handles is counted.
func InstrumentDefaulter[T runtime.Object](webhook, defaultReason string, inner admission.Defaulter[T]) admission.Defaulter[T] {
	return &instrumentedDefaulter[T]{webhook: webhook, defaultReason: defaultReason, inner: inner}
}

func (d *instrumentedDefaulter[T]) Default(ctx context.Context, obj T) error {
	err := d.inner.Default(ctx, obj)
	observe(d.webhook, operationCreate, d.defaultReason, err)
	return err
}

type instrumentedValidator[T runtime.Object] struct {
	webhook       string
	defaultReason string
	inner         admission.Validator[T]
}

// InstrumentValidator wraps a validating webhook so that every request it handles is counted.
func InstrumentValidator[T runtime.Object](webhook, defaultReason string, inner admission.Validator[T]) admission.Validator[T] {
	return &instrumentedValidator[T]{webhook: webhook, defaultReason: defaultReason, inner: inner}
}

func (v *instrumentedValidator[T]) ValidateCreate(ctx context.Context, obj T) (admission.Warnings, error) {
	warnings, err := v.inner.ValidateCreate(ctx, obj)
	observe(v.webhook, operationCreate, v.defaultReason, err)
	return warnings, err
}

func (v *instrumentedValidator[T]) ValidateUpdate(ctx context.Context, oldObj, newObj T) (admission.Warnings, error) {
	warnings, err := v.inner.ValidateUpdate(ctx, oldObj, newObj)
	observe(v.webhook, operationUpdate, v.defaultReason, err)
	return warnings, err
}

func (v *instrumentedValidator[T]) ValidateDelete(ctx context.Context, obj T) (admission.Warnings, error) {
	warnings, err := v.inner.ValidateDelete(ctx, obj)
	observe(v.webhook, operationDelete, v.defaultReason, err)
	return warnings, err
}
