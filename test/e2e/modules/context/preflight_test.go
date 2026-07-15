/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package context

import (
	goctx "context"
	"strings"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	fakeClient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
)

func fakeClientWith(t *testing.T, queues ...*v2.Queue) *fakeClient.ClientBuilder {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v2.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	builder := fakeClient.NewClientBuilder().WithScheme(scheme)
	for _, q := range queues {
		builder = builder.WithObjects(q)
	}
	return builder
}

func queueObj(name string, labels map[string]string) *v2.Queue {
	return &v2.Queue{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func TestCheckForeignQueuesEmpty(t *testing.T) {
	cli := fakeClientWith(t).Build()
	if err := checkForeignQueues(goctx.Background(), cli); err != nil {
		t.Errorf("empty cluster should pass: %v", err)
	}
}

func TestCheckForeignQueuesAllTestLabeled(t *testing.T) {
	cli := fakeClientWith(t,
		queueObj("leftover", map[string]string{commonconstants.AppLabelName: constant.EngineTestPodsApp}),
	).Build()
	if err := checkForeignQueues(goctx.Background(), cli); err != nil {
		t.Errorf("test-labeled queues should pass: %v", err)
	}
}

func TestCheckForeignQueuesUnlabeled(t *testing.T) {
	cli := fakeClientWith(t, queueObj("prod", nil)).Build()
	err := checkForeignQueues(goctx.Background(), cli)
	if err == nil {
		t.Fatal("unlabeled queue should fail preflight")
	}
	if !strings.Contains(err.Error(), "prod") {
		t.Errorf("error should name offending queue, got %q", err.Error())
	}
}

func TestCheckForeignQueuesMissingCRD(t *testing.T) {
	cli := fakeClientWith(t).WithInterceptorFuncs(interceptor.Funcs{
		List: func(goctx.Context, runtimeClient.WithWatch, runtimeClient.ObjectList, ...runtimeClient.ListOption) error {
			return &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: v2.GroupVersion.Group, Kind: "Queue"}}
		},
	}).Build()
	if err := checkForeignQueues(goctx.Background(), cli); err != nil {
		t.Errorf("missing Queue CRD (KAI not installed) should pass preflight: %v", err)
	}
}

func TestCheckForeignQueuesWrongLabel(t *testing.T) {
	cli := fakeClientWith(t,
		queueObj("user", map[string]string{commonconstants.AppLabelName: "something-else"}),
	).Build()
	if err := checkForeignQueues(goctx.Background(), cli); err == nil {
		t.Error("queue with non-test label should fail preflight")
	}
}

func TestRunPreflightOnlyChecksOnce(t *testing.T) {
	preflightOnce = sync.Once{}

	cli := fakeClientWith(t).Build()
	if err := runPreflight(goctx.Background(), cli); err != nil {
		t.Fatalf("first call should pass empty cluster: %v", err)
	}

	// Even if the cluster acquires a foreign Queue after the first call,
	// subsequent calls must skip the check (sync.Once semantics).
	cli2 := fakeClientWith(t, queueObj("appeared-later", nil)).Build()
	if err := runPreflight(goctx.Background(), cli2); err != nil {
		t.Errorf("second call should be a no-op, got: %v", err)
	}
}
