// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"sync/atomic"
	"testing"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestCreateWithBackoffRetriesThrottledCreates(t *testing.T) {
	var attempts atomic.Int64
	client := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c runtimeClient.WithWatch, obj runtimeClient.Object, opts ...runtimeClient.CreateOption) error {
			if attempts.Add(1) <= 2 {
				return apierrors.NewTooManyRequests("etcdserver: throttle: too many requests", 0)
			}
			return c.Create(ctx, obj, opts...)
		},
	}).Build()

	var throttled atomic.Int64
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}}
	if err := createWithBackoff(context.Background(), client, pod, &throttled); err != nil {
		t.Fatalf("expected the create to succeed after throttling, got %v", err)
	}
	if got := throttled.Load(); got != 2 {
		t.Errorf("expected 2 throttled creates, got %d", got)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 create attempts, got %d", got)
	}
}

func TestCreateWithBackoffStopsOnCancelledContext(t *testing.T) {
	client := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Create: func(context.Context, runtimeClient.WithWatch, runtimeClient.Object, ...runtimeClient.CreateOption) error {
			return apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "p", nil)
		},
	}).Build()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var throttled atomic.Int64
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}}
	if err := createWithBackoff(ctx, client, pod, &throttled); err == nil {
		t.Fatal("expected the error to be returned once the context is cancelled")
	}
}
