// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mock_binder "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/common"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
)

func newPlacementTestReconciler(t *testing.T, pod *v1.Pod, bindRequest *schedulingv1alpha2.BindRequest) (
	*BindRequestReconciler, client.WithWatch, *record.FakeRecorder,
) {
	t.Helper()
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(v1.AddToScheme(scheme)).To(Succeed())
	g.Expect(schedulingv1alpha2.AddToScheme(scheme)).To(Succeed())
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&schedulingv1alpha2.BindRequest{}).
		WithObjects(pod, bindRequest, &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: bindRequest.Spec.SelectedNode}}).
		Build()
	recorder := record.NewFakeRecorder(10)
	binder := mock_binder.NewMockInterface(gomock.NewController(t))
	return NewBindRequestReconciler(c, scheme, recorder, &ReconcilerParams{}, binder, nil), c, recorder
}

func TestReconcileRefreshesCachedPodBeforeValidatingIntent(t *testing.T) {
	tests := []struct {
		name          string
		cachedUID     types.UID
		cachedNode    string
		ownerUID      types.UID
		actualNode    string
		cachedGroup   string
		actualGroup   string
		expectedBind  bool
		expectedError string
	}{
		{name: "new request binds the replacement after observing a deleted bound Pod", cachedNode: "deleted-node", ownerUID: "current-pod", expectedBind: true},
		{name: "new request refreshes a conflicting cached unbound UID", ownerUID: "current-pod", expectedBind: true},
		{name: "old request cannot bind a replacement Pod", cachedNode: "deleted-node", ownerUID: "deleted-pod", expectedError: "with UID deleted-pod, but the current pod has UID current-pod"},
		{name: "fresh node mismatch overrides cached matching node", cachedNode: "selected-node", ownerUID: "current-pod", actualNode: "actual-node", expectedError: "already bound to node actual-node"},
		{name: "fresh matching groups override stale mismatching groups", cachedNode: "selected-node", ownerUID: "current-pod", actualNode: "selected-node", cachedGroup: "old-group", actualGroup: "selected-group"},
		{name: "fresh group mismatch overrides cached matching groups", cachedNode: "selected-node", ownerUID: "current-pod", actualNode: "selected-node", cachedGroup: "selected-group", actualGroup: "actual-group", expectedError: "already bound with GPU groups [actual-group]"},
		{name: "stale unbound cache cannot hide a fresh node mismatch", cachedUID: "current-pod", ownerUID: "current-pod", actualNode: "actual-node", expectedError: "already bound to node actual-node"},
		{name: "stale unbound cache cannot hide a fresh group mismatch", cachedUID: "current-pod", ownerUID: "current-pod", actualNode: "selected-node", cachedGroup: "selected-group", actualGroup: "actual-group", expectedError: "already bound with GPU groups [actual-group]"},
		{name: "optional absent owner remains compatible", cachedNode: "deleted-node", expectedBind: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "replacement-pod", Namespace: "default", UID: "current-pod"},
				Spec:       v1.PodSpec{NodeName: tt.actualNode},
			}
			bindRequest := baseRequest.DeepCopy()
			bindRequest.Spec.PodName = pod.Name
			bindRequest.Spec.SelectedNode = "selected-node"
			if tt.ownerUID != "" {
				bindRequest.OwnerReferences = []metav1.OwnerReference{{
					APIVersion: "v1", Kind: "Pod", Name: pod.Name, UID: tt.ownerUID,
				}}
			}
			if tt.actualGroup != "" {
				pod.Labels = map[string]string{"runai-gpu-group": tt.actualGroup}
				bindRequest.Spec.ReceivedResourceType = common.ReceivedTypeFraction
				bindRequest.Spec.SelectedGPUGroups = []string{"selected-group"}
			}
			r, c, recorder := newPlacementTestReconciler(t, pod, bindRequest)
			originalPod := &v1.Pod{}
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pod), originalPod)).To(Succeed())
			cachedPod := originalPod.DeepCopy()
			cachedPod.UID = "deleted-pod"
			if tt.cachedUID != "" {
				cachedPod.UID = tt.cachedUID
			}
			cachedPod.Spec.NodeName = tt.cachedNode
			if tt.cachedGroup != "" {
				cachedPod.Labels = map[string]string{"runai-gpu-group": tt.cachedGroup}
			}
			r.Client = interceptor.NewClient(c, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if pod, ok := obj.(*v1.Pod); ok {
						cachedPod.DeepCopyInto(pod)
						return nil
					}
					return c.Get(ctx, key, obj, opts...)
				},
			})
			apiReads := 0
			r.apiReader = interceptor.NewClient(c, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					apiReads++
					freshPod, ok := obj.(*v1.Pod)
					g.Expect(ok).To(BeTrue())
					g.Expect(freshPod.UID).To(BeEmpty())
					g.Expect(freshPod.Spec.NodeName).To(BeEmpty())
					g.Expect(freshPod.Labels).To(BeEmpty())
					return c.Get(ctx, key, obj, opts...)
				},
			})
			if tt.expectedBind {
				binder := r.binder.(*mock_binder.MockInterface)
				binder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, pod *v1.Pod, node *v1.Node, _ *schedulingv1alpha2.BindRequest) error {
						g.Expect(pod.UID).To(Equal(types.UID("current-pod")))
						g.Expect(pod.Spec.NodeName).To(BeEmpty())
						pod.Spec.NodeName = node.Name
						return c.Update(ctx, pod)
					})
			}

			result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(bindRequest)})
			g.Expect(result).To(Equal(ctrl.Result{}))
			g.Expect(apiReads).To(Equal(1))
			stored := &schedulingv1alpha2.BindRequest{}
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(bindRequest), stored)).To(Succeed())
			storedPod := &v1.Pod{}
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pod), storedPod)).To(Succeed())
			if tt.expectedError != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tt.expectedError)))
				g.Expect(stored.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseFailed))
				g.Expect(storedPod).To(Equal(originalPod))
				g.Expect(recorder.Events).To(BeEmpty())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(stored.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseSucceeded))
				g.Expect(storedPod.Spec.NodeName).To(Equal("selected-node"))
				g.Expect(recorder.Events).To(Receive(ContainSubstring("Normal Bound")))
			}
		})
	}
}

func TestReconcileDoesNotConsumeRetryBudgetOnAuthoritativeReadError(t *testing.T) {
	for _, phase := range []string{schedulingv1alpha2.BindRequestPhasePending, schedulingv1alpha2.BindRequestPhaseFailed} {
		for _, readErr := range []error{
			errors.New("API is temporarily unavailable"),
		} {
			t.Run(phase+"/"+readErr.Error(), func(t *testing.T) {
				g := NewWithT(t)
				ctx := context.Background()
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "bound-pod", Namespace: "default", UID: "current-pod"},
					Spec:       v1.PodSpec{NodeName: "selected-node"},
					Status: v1.PodStatus{Conditions: []v1.PodCondition{
						{Type: podBoundCondition, Status: v1.ConditionTrue, Reason: "Bound"},
					}},
				}
				bindRequest := baseRequest.DeepCopy()
				bindRequest.Spec.PodName = pod.Name
				bindRequest.Spec.SelectedNode = pod.Spec.NodeName
				bindRequest.Spec.BackoffLimit = ptr.To(int32(3))
				bindRequest.Status.Phase = phase
				if phase == schedulingv1alpha2.BindRequestPhaseFailed {
					bindRequest.Status.FailedAttempts = 1
					bindRequest.Status.Reason = "previous binding failure"
				}
				r, c, recorder := newPlacementTestReconciler(t, pod, bindRequest)
				originalRequest := &schedulingv1alpha2.BindRequest{}
				g.Expect(c.Get(ctx, client.ObjectKeyFromObject(bindRequest), originalRequest)).To(Succeed())
				originalPod := &v1.Pod{}
				g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pod), originalPod)).To(Succeed())
				statusPatches := 0
				r.Client = interceptor.NewClient(c, interceptor.Funcs{
					SubResourcePatch: func(context.Context, client.Client, string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
						statusPatches++
						return errors.New("unexpected status mutation")
					},
				})
				r.apiReader = interceptor.NewClient(c, interceptor.Funcs{
					Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
						return readErr
					},
				})

				result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(bindRequest)})
				g.Expect(err).To(MatchError(readErr))
				g.Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeFalse())
				g.Expect(result).To(Equal(ctrl.Result{}))
				stored := &schedulingv1alpha2.BindRequest{}
				g.Expect(c.Get(ctx, client.ObjectKeyFromObject(bindRequest), stored)).To(Succeed())
				g.Expect(stored).To(Equal(originalRequest))
				storedPod := &v1.Pod{}
				g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pod), storedPod)).To(Succeed())
				g.Expect(storedPod).To(Equal(originalPod))
				g.Expect(statusPatches).To(BeZero())
				g.Expect(recorder.Events).To(BeEmpty())
			})
		}
	}
}

func TestReconcileAuthoritativePodAbsenceKeepsFailureContract(t *testing.T) {
	tests := []struct {
		name           string
		phase          string
		failedAttempts int32
		backoffLimit   *int32
	}{
		{name: "Pending without retry budget", phase: schedulingv1alpha2.BindRequestPhasePending},
		{name: "Pending with retry budget", phase: schedulingv1alpha2.BindRequestPhasePending, backoffLimit: ptr.To(int32(3))},
		{name: "retryable Failed", phase: schedulingv1alpha2.BindRequestPhaseFailed, failedAttempts: 1, backoffLimit: ptr.To(int32(3))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "deleted-pod", Namespace: "default", UID: "deleted-pod-uid"}}
			bindRequest := baseRequest.DeepCopy()
			bindRequest.Spec.PodName = pod.Name
			bindRequest.Spec.SelectedNode = "selected-node"
			bindRequest.Spec.BackoffLimit = tt.backoffLimit
			bindRequest.Status.Phase = tt.phase
			bindRequest.Status.FailedAttempts = tt.failedAttempts
			bindRequest.Status.Reason = "previous binding failure"
			r, c, recorder := newPlacementTestReconciler(t, pod, bindRequest)
			g.Expect(c.Delete(ctx, pod)).To(Succeed())
			r.Client = interceptor.NewClient(c, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if cachedPod, ok := obj.(*v1.Pod); ok {
						pod.DeepCopyInto(cachedPod)
						return nil
					}
					return c.Get(ctx, key, obj, opts...)
				},
			})

			result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(bindRequest)})
			g.Expect(kerrors.IsNotFound(err)).To(BeTrue())
			g.Expect(errors.Is(err, reconcile.TerminalError(nil))).To(Equal(tt.backoffLimit == nil))
			expectedAttempts := tt.failedAttempts
			expectedResult := ctrl.Result{}
			if tt.backoffLimit != nil {
				expectedAttempts++
				expectedResult.RequeueAfter = (1 << tt.failedAttempts) * time.Second
			}
			g.Expect(result).To(Equal(expectedResult))
			stored := &schedulingv1alpha2.BindRequest{}
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(bindRequest), stored)).To(Succeed())
			g.Expect(stored.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseFailed))
			g.Expect(stored.Status.FailedAttempts).To(Equal(expectedAttempts))
			g.Expect(stored.Status.Reason).To(ContainSubstring("not found"))
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pod), &v1.Pod{})).To(MatchError(ContainSubstring("not found")))
			g.Expect(recorder.Events).To(BeEmpty())
		})
	}
}

func TestReconcileAuthoritativePodUsesFreshDecodeTarget(t *testing.T) {
	for _, alreadyBound := range []bool{false, true} {
		name := "deleted cached Pod must not leave node or labels on its unbound replacement"
		if alreadyBound {
			name = "cached fractional labels must not survive an authoritative Pod without labels"
		}
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "decoder-pod", Namespace: "default", UID: "current-pod"}}
			if alreadyBound {
				pod.Spec.NodeName = "selected-node"
			}
			bindRequest := baseRequest.DeepCopy()
			bindRequest.Spec.PodName = pod.Name
			bindRequest.Spec.SelectedNode = "selected-node"
			bindRequest.Spec.ReceivedResourceType = common.ReceivedTypeFraction
			bindRequest.Spec.SelectedGPUGroups = []string{"selected-group"}
			bindRequest.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "v1", Kind: "Pod", Name: pod.Name, UID: pod.UID,
			}}
			r, c, recorder := newPlacementTestReconciler(t, pod, bindRequest)
			originalPod := &v1.Pod{}
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pod), originalPod)).To(Succeed())
			cachedPod := originalPod.DeepCopy()
			cachedPod.Spec.NodeName = "selected-node"
			cachedPod.Labels = map[string]string{"runai-gpu-group": "selected-group"}
			if !alreadyBound {
				cachedPod.UID = "deleted-pod"
			}
			r.Client = interceptor.NewClient(c, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if pod, ok := obj.(*v1.Pod); ok {
						cachedPod.DeepCopyInto(pod)
						return nil
					}
					return c.Get(ctx, key, obj, opts...)
				},
			})
			responsePod := originalPod.DeepCopy()
			responsePod.APIVersion, responsePod.Kind = "v1", "Pod"
			payload, err := json.Marshal(responsePod)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(string(payload)).NotTo(ContainSubstring("\"labels\""))
			if !alreadyBound {
				g.Expect(string(payload)).NotTo(ContainSubstring("\"nodeName\""))
			}
			var apiReads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodGet || req.URL.Path != "/api/v1/namespaces/default/pods/decoder-pod" {
					t.Errorf("unexpected authoritative request: %s %s", req.Method, req.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				apiReads.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(payload)
			}))
			t.Cleanup(server.Close)
			mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{v1.SchemeGroupVersion})
			mapper.Add(v1.SchemeGroupVersion.WithKind("Pod"), meta.RESTScopeNamespace)
			r.apiReader, err = client.New(&rest.Config{Host: server.URL}, client.Options{Scheme: c.Scheme(), Mapper: mapper})
			g.Expect(err).NotTo(HaveOccurred())
			if !alreadyBound {
				binder := r.binder.(*mock_binder.MockInterface)
				binder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, pod *v1.Pod, node *v1.Node, _ *schedulingv1alpha2.BindRequest) error {
						g.Expect(pod.UID).To(Equal(types.UID("current-pod")))
						g.Expect(pod.Spec.NodeName).To(BeEmpty())
						g.Expect(pod.Labels).To(BeEmpty())
						pod.Spec.NodeName = node.Name
						return c.Update(ctx, pod)
					})
			}

			_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(bindRequest)})
			g.Expect(apiReads.Load()).To(Equal(int32(1)))
			stored := &schedulingv1alpha2.BindRequest{}
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(bindRequest), stored)).To(Succeed())
			storedPod := &v1.Pod{}
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pod), storedPod)).To(Succeed())
			if alreadyBound {
				g.Expect(err).To(MatchError(ContainSubstring("already bound with GPU groups []")))
				g.Expect(stored.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseFailed))
				g.Expect(storedPod).To(Equal(originalPod))
				g.Expect(recorder.Events).To(BeEmpty())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(stored.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseSucceeded))
				g.Expect(storedPod.Labels).To(BeEmpty())
				g.Expect(recorder.Events).To(Receive(ContainSubstring("Normal Bound")))
			}
		})
	}
}

func TestReconcileFreshUnboundPodKeepsBindingPath(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "unbound-pod", Namespace: "default", UID: "current-pod"}}
	bindRequest := baseRequest.DeepCopy()
	bindRequest.Spec.PodName = pod.Name
	bindRequest.Spec.SelectedNode = "selected-node"
	bindRequest.Spec.ReceivedResourceType = common.ReceivedTypeFraction
	bindRequest.Spec.SelectedGPUGroups = []string{"selected-group"}
	r, c, recorder := newPlacementTestReconciler(t, pod, bindRequest)
	apiReads := 0
	r.apiReader = interceptor.NewClient(c, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			apiReads++
			return c.Get(ctx, key, obj, opts...)
		},
	})
	binder := r.binder.(*mock_binder.MockInterface)
	binder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(bindRequest)})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))
	g.Expect(apiReads).To(Equal(1))
	stored := &schedulingv1alpha2.BindRequest{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(bindRequest), stored)).To(Succeed())
	g.Expect(stored.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseSucceeded))
	g.Expect(recorder.Events).To(Receive(ContainSubstring("Normal Bound")))
}
