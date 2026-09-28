// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package watcher

import (
	"context"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type neverSatisfiedWatcher struct{}

func (w *neverSatisfiedWatcher) watch(context.Context) watch.Interface {
	return watch.NewRaceFreeFake()
}

func (w *neverSatisfiedWatcher) sync(context.Context) {}

func (w *neverSatisfiedWatcher) processEvent(context.Context, watch.Event) {}

func (w *neverSatisfiedWatcher) satisfied() bool {
	return false
}

func TestForEventCustomTimeoutReturnsFalseWhenContextCanceled(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core types to scheme: %v", err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if ForEventCustomTimeout(ctx, client, &neverSatisfiedWatcher{}, time.Minute) {
		t.Fatal("canceled context satisfied the watcher")
	}
}
