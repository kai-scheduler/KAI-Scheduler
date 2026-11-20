// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/plugins"
)

type fakePlugin struct {
	name        string
	validateErr error
	mutateErr   error
}

func (p *fakePlugin) Name() string { return p.name }

func (p *fakePlugin) Validate(context.Context, *v1.Pod, *v1.Pod) error { return p.validateErr }

func (p *fakePlugin) Mutate(*v1.Pod) error { return p.mutateErr }

type fakeValidator struct {
	plugins *plugins.KaiAdmissionPlugins
	err     error
}

func (v *fakeValidator) ValidateCreate(ctx context.Context, pod *v1.Pod) (admission.Warnings, error) {
	if v.plugins != nil {
		return nil, v.plugins.Validate(ctx, nil, pod)
	}
	return nil, v.err
}

func (v *fakeValidator) ValidateUpdate(ctx context.Context, oldPod, pod *v1.Pod) (admission.Warnings, error) {
	return v.ValidateCreate(ctx, pod)
}

func (v *fakeValidator) ValidateDelete(ctx context.Context, pod *v1.Pod) (admission.Warnings, error) {
	return v.ValidateCreate(ctx, pod)
}

type fakeDefaulter struct {
	plugins *plugins.KaiAdmissionPlugins
}

func (d *fakeDefaulter) Default(_ context.Context, pod *v1.Pod) error {
	return d.plugins.Mutate(pod)
}

func requests(webhook, operation, result string) float64 {
	return testutil.ToFloat64(requestsTotal.WithLabelValues(webhook, operation, result))
}

func rejections(webhook, reason string) float64 {
	return testutil.ToFloat64(rejectionsTotal.WithLabelValues(webhook, reason))
}

func TestInstrumentValidatorCountsAllowedRequestsPerOperation(t *testing.T) {
	const webhook = "test-allowed-validator"
	validator := InstrumentValidator[*v1.Pod](webhook, ReasonValidationFailed, &fakeValidator{})

	_, err := validator.ValidateCreate(context.Background(), &v1.Pod{})
	assert.NoError(t, err)
	_, err = validator.ValidateUpdate(context.Background(), &v1.Pod{}, &v1.Pod{})
	assert.NoError(t, err)
	_, err = validator.ValidateUpdate(context.Background(), &v1.Pod{}, &v1.Pod{})
	assert.NoError(t, err)
	_, err = validator.ValidateDelete(context.Background(), &v1.Pod{})
	assert.NoError(t, err)

	assert.Equal(t, float64(1), requests(webhook, "create", "allowed"))
	assert.Equal(t, float64(2), requests(webhook, "update", "allowed"))
	assert.Equal(t, float64(1), requests(webhook, "delete", "allowed"))
	assert.Equal(t, float64(0), requests(webhook, "create", "rejected"))
}

func TestInstrumentValidatorUsesRejectingPluginAsReason(t *testing.T) {
	const webhook = "test-plugin-validator"
	admissionPlugins := plugins.New()
	admissionPlugins.RegisterPlugin(&fakePlugin{name: "allowing-plugin"})
	admissionPlugins.RegisterPlugin(&fakePlugin{name: "rejecting-plugin", validateErr: errors.New("pod has no label 'foo'")})
	validator := InstrumentValidator[*v1.Pod](webhook, ReasonValidationFailed, &fakeValidator{plugins: admissionPlugins})

	_, err := validator.ValidateCreate(context.Background(), &v1.Pod{})

	assert.EqualError(t, err, "pod has no label 'foo'")
	assert.Equal(t, float64(1), requests(webhook, "create", "rejected"))
	assert.Equal(t, float64(0), requests(webhook, "create", "allowed"))
	assert.Equal(t, float64(1), rejections(webhook, "rejecting-plugin"))
	assert.Equal(t, float64(0), rejections(webhook, ReasonValidationFailed))
}

func TestInstrumentValidatorUsesDefaultReasonForNonPluginErrors(t *testing.T) {
	const webhook = "test-default-reason-validator"
	validator := InstrumentValidator[*v1.Pod](webhook, ReasonResizeRejected,
		&fakeValidator{err: fmt.Errorf("queue quota exceeded")})

	_, err := validator.ValidateUpdate(context.Background(), &v1.Pod{}, &v1.Pod{})

	assert.Error(t, err)
	assert.Equal(t, float64(1), requests(webhook, "update", "rejected"))
	assert.Equal(t, float64(1), rejections(webhook, ReasonResizeRejected))
}

func TestInstrumentDefaulterCountsMutations(t *testing.T) {
	const webhook = "test-defaulter"
	admissionPlugins := plugins.New()
	failing := &fakePlugin{name: "failing-mutator"}
	admissionPlugins.RegisterPlugin(failing)
	defaulter := InstrumentDefaulter[*v1.Pod](webhook, ReasonMutationFailed, &fakeDefaulter{plugins: admissionPlugins})

	assert.NoError(t, defaulter.Default(context.Background(), &v1.Pod{}))
	assert.Equal(t, float64(1), requests(webhook, "create", "allowed"))

	failing.mutateErr = errors.New("cannot mutate")
	assert.EqualError(t, defaulter.Default(context.Background(), &v1.Pod{}), "cannot mutate")
	assert.Equal(t, float64(1), requests(webhook, "create", "rejected"))
	assert.Equal(t, float64(1), rejections(webhook, "failing-mutator"))
}
