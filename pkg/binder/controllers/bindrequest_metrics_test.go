// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kubeaischedulerscheme "github.com/kai-scheduler/api/client/clientset/versioned/scheme"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding"
	mock_binder "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/metrics"
)

func TestReconcileRecordsBindRequestMetrics(t *testing.T) {
	const (
		podName  = "metrics-pod"
		nodeName = "metrics-node"
	)
	newPod := func(boundNode string) *v1.Pod {
		return &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: "default"},
			Spec:       v1.PodSpec{NodeName: boundNode},
		}
	}
	newNode := func() *v1.Node {
		return &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
	}

	tests := []struct {
		name            string
		objects         []client.Object
		backoffLimit    *int32
		bindErr         error
		expectBind      bool
		expectedResult  string
		expectedReason  string
		expectedRetries float64
		expectedToBound uint64
	}{
		{
			name:            "bound",
			objects:         []client.Object{newPod(""), newNode()},
			expectBind:      true,
			expectedResult:  metrics.ResultSucceeded,
			expectedReason:  metrics.ReasonBound,
			expectedToBound: 1,
		},
		{
			name:           "pod already bound",
			objects:        []client.Object{newPod(nodeName), newNode()},
			expectedResult: metrics.ResultSucceeded,
			expectedReason: metrics.ReasonAlreadyBound,
		},
		{
			name:           "pod not found",
			objects:        []client.Object{newNode()},
			expectedResult: metrics.ResultFailed,
			expectedReason: metrics.ReasonPodLookupFailed,
		},
		{
			name:           "node not found",
			objects:        []client.Object{newPod("")},
			expectedResult: metrics.ResultFailed,
			expectedReason: metrics.ReasonNodeLookupFailed,
		},
		{
			name:            "bind fails and is retried",
			objects:         []client.Object{newPod(""), newNode()},
			backoffLimit:    ptr.To(int32(3)),
			bindErr:         fmt.Errorf("plugin failed"),
			expectBind:      true,
			expectedResult:  metrics.ResultFailed,
			expectedReason:  metrics.ReasonBindFailed,
			expectedRetries: 1,
		},
		{
			name:           "bind fails without backoff limit",
			objects:        []client.Object{newPod(""), newNode()},
			bindErr:        fmt.Errorf("plugin failed"),
			expectBind:     true,
			expectedResult: metrics.ResultFailed,
			expectedReason: metrics.ReasonBindFailed,
		},
		{
			name:           "invalid bind request",
			objects:        []client.Object{newPod(""), newNode()},
			bindErr:        fmt.Errorf("conversion: %w", binding.InvalidCrdWarning),
			expectBind:     true,
			expectedResult: metrics.ResultFailed,
			expectedReason: metrics.ReasonInvalidCrd,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testScheme := runtime.NewScheme()
			require.NoError(t, v1.AddToScheme(testScheme))
			require.NoError(t, kubeaischedulerscheme.AddToScheme(testScheme))
			fakeClient := fake.NewClientBuilder().WithScheme(testScheme).
				WithStatusSubresource(&schedulingv1alpha2.BindRequest{}).
				Build()
			for _, object := range tc.objects {
				require.NoError(t, fakeClient.Create(context.TODO(), object))
			}

			bindRequest := &schedulingv1alpha2.BindRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "metrics-bind-request",
					Namespace:         "default",
					CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Second)),
				},
				Spec: schedulingv1alpha2.BindRequestSpec{
					PodName:      podName,
					SelectedNode: nodeName,
					BackoffLimit: tc.backoffLimit,
				},
			}
			require.NoError(t, fakeClient.Create(context.TODO(), bindRequest))

			mockBinder := mock_binder.NewMockInterface(gomock.NewController(t))
			if tc.expectBind {
				mockBinder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(tc.bindErr).Times(1)
			}
			if tc.bindErr != nil && !errors.Is(tc.bindErr, binding.InvalidCrdWarning) {
				mockBinder.EXPECT().Rollback(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(1)
			}
			reconciler := NewBindRequestReconciler(fakeClient, testScheme, record.NewFakeRecorder(10),
				&ReconcilerParams{MaxConcurrentReconciles: 1}, mockBinder, nil)

			requestsBefore := metrics.BindRequestsCount(tc.expectedResult, tc.expectedReason)
			retriesBefore := metrics.BindRetriesCount()
			toBoundBefore := metrics.BindRequestToBoundSampleCount()

			_, _ = reconciler.Reconcile(context.TODO(), ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(bindRequest),
			})

			assert.Equal(t, requestsBefore+1, metrics.BindRequestsCount(tc.expectedResult, tc.expectedReason))
			assert.Equal(t, retriesBefore+tc.expectedRetries, metrics.BindRetriesCount())
			assert.Equal(t, toBoundBefore+tc.expectedToBound, metrics.BindRequestToBoundSampleCount())
		})
	}
}

func TestReconcileSkipsMetricsForNonActionableBindRequest(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(testScheme))
	require.NoError(t, kubeaischedulerscheme.AddToScheme(testScheme))
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme).
		WithStatusSubresource(&schedulingv1alpha2.BindRequest{}).
		Build()

	bindRequest := &schedulingv1alpha2.BindRequest{
		ObjectMeta: metav1.ObjectMeta{Name: "done", Namespace: "default"},
		Spec:       schedulingv1alpha2.BindRequestSpec{PodName: "pod", SelectedNode: "node"},
	}
	require.NoError(t, fakeClient.Create(context.TODO(), bindRequest))
	bindRequest.Status.Phase = schedulingv1alpha2.BindRequestPhaseSucceeded
	require.NoError(t, fakeClient.Status().Update(context.TODO(), bindRequest))

	reconciler := NewBindRequestReconciler(fakeClient, testScheme, record.NewFakeRecorder(10),
		&ReconcilerParams{MaxConcurrentReconciles: 1}, mock_binder.NewMockInterface(gomock.NewController(t)), nil)

	succeededBefore := metrics.BindRequestsCount(metrics.ResultSucceeded, metrics.ReasonBound)
	alreadyBoundBefore := metrics.BindRequestsCount(metrics.ResultSucceeded, metrics.ReasonAlreadyBound)

	_, err := reconciler.Reconcile(context.TODO(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(bindRequest)})
	require.NoError(t, err)

	assert.Equal(t, succeededBefore, metrics.BindRequestsCount(metrics.ResultSucceeded, metrics.ReasonBound))
	assert.Equal(t, alreadyBoundBefore, metrics.BindRequestsCount(metrics.ResultSucceeded, metrics.ReasonAlreadyBound))
}
