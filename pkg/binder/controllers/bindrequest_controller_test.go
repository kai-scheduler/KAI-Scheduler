// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kubeaischedulerscheme "github.com/kai-scheduler/api/client/clientset/versioned/scheme"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding"
	mock_binder "github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/mock"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins"
	mockplugins "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/mock"
)

const (
	resourceReservationNameSpace      = "kai-resource-reservation"
	resourceReservationServiceAccount = resourceReservationNameSpace
	resourceReservationAppLabelValue  = resourceReservationNameSpace
	scalingPodsNamespace              = "kai-scale-adjust"
)

func TestBindRequest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Binder Controller Suite")
}

var baseRequest *schedulingv1alpha2.BindRequest = &schedulingv1alpha2.BindRequest{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test",
		Namespace: "default",
	},
	Status: schedulingv1alpha2.BindRequestStatus{
		Phase: schedulingv1alpha2.BindRequestPhasePending,
	},
}

func nodeNameIndexer(rawObj client.Object) []string {
	pod := rawObj.(*v1.Pod)
	return []string{pod.Spec.NodeName}
}

var _ = Describe("BindRequest Controller", func() {
	var (
		fakeClient        client.WithWatch
		reconciler        *BindRequestReconciler
		fakeEventRecorder *record.FakeRecorder
		fakePlugin        *mockplugins.MockPlugin
	)
	BeforeEach(func() {
		testScheme := runtime.NewScheme()
		Expect(v1.AddToScheme(testScheme)).Should(Succeed())
		Expect(kubeaischedulerscheme.AddToScheme(testScheme)).Should(Succeed())
		fakeClient = fake.NewClientBuilder().WithScheme(testScheme).
			WithIndex(&v1.Pod{}, "spec.nodeName", nodeNameIndexer).
			WithStatusSubresource(&schedulingv1alpha2.BindRequest{}).
			Build()
		fakeEventRecorder = record.NewFakeRecorder(100)

		params := &ReconcilerParams{
			MaxConcurrentReconciles:     1,
			RateLimiterBaseDelaySeconds: 1,
			RateLimiterMaxDelaySeconds:  1,
		}

		binderPlugins := plugins.New()
		fakePlugin = mockplugins.NewMockPlugin(gomock.NewController(GinkgoT()))
		binderPlugins.RegisterPlugin(fakePlugin)

		rrs := resourcereservation.NewService(false, fakeClient, "", 40*time.Second,
			resourceReservationNameSpace, resourceReservationServiceAccount, resourceReservationAppLabelValue, scalingPodsNamespace, "",
			nil, nil, nil, false)
		binder := binding.NewBinder(fakeClient, rrs, binderPlugins)
		reconciler = NewBindRequestReconciler(fakeClient, testScheme, fakeEventRecorder, params,
			binder, rrs)
	})

	Describe("Reconcile", func() {
		DescribeTable("recovers a status retry while the cached Pod remains unbound",
			func(initiallyFailed bool) {
				ctx := context.Background()
				pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{
					Name: "stale-status-pod", Namespace: "default", UID: "stale-status-pod-uid",
				}}
				node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "stale-status-node"}}
				Expect(fakeClient.Create(ctx, pod)).To(Succeed())
				Expect(fakeClient.Create(ctx, node)).To(Succeed())
				stalePod := pod.DeepCopy()
				bindRequest := baseRequest.DeepCopy()
				bindRequest.Spec.PodName = pod.Name
				bindRequest.Spec.SelectedNode = node.Name
				bindRequest.Spec.BackoffLimit = ptr.To(int32(3))
				if initiallyFailed {
					bindRequest.Status.Phase = schedulingv1alpha2.BindRequestPhaseFailed
					bindRequest.Status.FailedAttempts = 1
					bindRequest.Status.Reason = "previous binding failure"
				}
				Expect(fakeClient.Create(ctx, bindRequest)).To(Succeed())
				initialStatus := bindRequest.Status

				statusErr := kerrors.NewServerTimeout(schema.GroupResource{
					Group: schedulingv1alpha2.GroupVersion.Group, Resource: "bindrequests",
				}, "patch", 1)
				readErr := errors.New("temporary Pod API read failure")
				statusPatches, podStatusPatches, cachedPodReads, freshPodReads := 0, 0, 0, 0
				readFails := false
				reconciler.Client = interceptor.NewClient(fakeClient, interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey,
						obj client.Object, opts ...client.GetOption) error {
						if cached, ok := obj.(*v1.Pod); ok {
							cachedPodReads++
							stalePod.DeepCopyInto(cached)
							return nil
						}
						return c.Get(ctx, key, obj, opts...)
					},
					SubResourcePatch: func(ctx context.Context, c client.Client, subResource string,
						obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
						switch obj.(type) {
						case *schedulingv1alpha2.BindRequest:
							statusPatches++
							if statusPatches == 1 {
								return statusErr
							}
						case *v1.Pod:
							podStatusPatches++
						}
						return c.SubResource(subResource).Patch(ctx, obj, patch, opts...)
					},
				})
				reconciler.apiReader = interceptor.NewClient(fakeClient, interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey,
						obj client.Object, opts ...client.GetOption) error {
						freshPodReads++
						if readFails {
							return readErr
						}
						return c.Get(ctx, key, obj, opts...)
					},
				})
				mockBinder := mock_binder.NewMockInterface(gomock.NewController(GinkgoT()))
				bindCalls, rollbackCalls := 0, 0
				mockBinder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, pod *v1.Pod, node *v1.Node, _ *schedulingv1alpha2.BindRequest) error {
						bindCalls++
						if bindCalls > 1 {
							return kerrors.NewBadRequest("pod is already assigned to a node")
						}
						pod.Spec.NodeName = node.Name
						return fakeClient.Update(ctx, pod)
					}).AnyTimes()
				mockBinder.EXPECT().Rollback(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(context.Context, *v1.Pod, *v1.Node, *schedulingv1alpha2.BindRequest) error {
						rollbackCalls++
						return nil
					}).AnyTimes()
				reconciler.binder = mockBinder
				req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(bindRequest)}

				result, err := reconciler.Reconcile(ctx, req)
				Expect(err).To(MatchError(statusErr))
				Expect(result).To(Equal(ctrl.Result{}))
				Expect(statusPatches).To(Equal(1))
				Expect(fakeEventRecorder.Events).To(Receive(ContainSubstring("Normal Bound")))
				Expect(fakeEventRecorder.Events).To(BeEmpty())
				stored := &schedulingv1alpha2.BindRequest{}
				Expect(fakeClient.Get(ctx, req.NamespacedName, stored)).To(Succeed())
				Expect(stored.Status).To(Equal(initialStatus))
				boundPod := &v1.Pod{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(pod), boundPod)).To(Succeed())
				Expect(boundPod.Spec.NodeName).To(Equal(node.Name))
				Expect(boundPod.Status.Conditions).To(ContainElement(And(
					HaveField("Type", v1.PodConditionType(podBoundCondition)),
					HaveField("Status", v1.ConditionTrue),
				)))

				cachedPod := &v1.Pod{}
				Expect(reconciler.Client.Get(ctx, client.ObjectKeyFromObject(pod), cachedPod)).To(Succeed())
				Expect(cachedPod.Spec.NodeName).To(BeEmpty())
				readFails = true
				previousPodStatusPatches := podStatusPatches
				result, err = reconciler.Reconcile(ctx, req)
				Expect([]int{bindCalls, rollbackCalls}).To(Equal([]int{1, 0}))
				Expect(err).To(MatchError(readErr))
				Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeFalse())
				Expect(result).To(Equal(ctrl.Result{}))
				Expect(cachedPodReads).To(Equal(1))
				Expect(freshPodReads).To(Equal(2))
				Expect(statusPatches).To(Equal(1))
				Expect(podStatusPatches).To(Equal(previousPodStatusPatches))
				Expect(fakeClient.Get(ctx, req.NamespacedName, stored)).To(Succeed())
				Expect(stored.Status).To(Equal(initialStatus))
				unchangedPod := &v1.Pod{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(pod), unchangedPod)).To(Succeed())
				Expect(unchangedPod.Status).To(Equal(boundPod.Status))
				Expect(fakeEventRecorder.Events).To(BeEmpty())

				readFails = false
				result, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
				Expect([]int{bindCalls, rollbackCalls}).To(Equal([]int{1, 0}))
				Expect(cachedPodReads).To(Equal(1))
				Expect(freshPodReads).To(Equal(3))
				Expect(statusPatches).To(Equal(2))
				Expect(fakeClient.Get(ctx, req.NamespacedName, stored)).To(Succeed())
				Expect(stored.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseSucceeded))
				Expect(stored.Status.FailedAttempts).To(Equal(initialStatus.FailedAttempts))
				Expect(fakeEventRecorder.Events).To(Receive(ContainSubstring("Normal Bound")))
				Expect(fakeEventRecorder.Events).To(BeEmpty())
				Expect(reconciler.Client.Get(ctx, client.ObjectKeyFromObject(pod), cachedPod)).To(Succeed())
				Expect(cachedPod.Spec.NodeName).To(BeEmpty())
			},
			Entry("initially Pending", false),
			Entry("initially retryable Failed", true),
		)

		DescribeTable("retries status write errors without losing Pod reports",
			func(writeFailure string, bindingFails, withBackoff bool) {
				ctx := context.Background()
				pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "status-pod", Namespace: "default"}}
				node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "status-node"}}
				Expect(fakeClient.Create(ctx, pod)).To(Succeed())
				Expect(fakeClient.Create(ctx, node)).To(Succeed())
				bindRequest := baseRequest.DeepCopy()
				bindRequest.Spec.PodName = pod.Name
				bindRequest.Spec.SelectedNode = node.Name
				if withBackoff {
					bindRequest.Spec.BackoffLimit = ptr.To(int32(3))
				}
				Expect(fakeClient.Create(ctx, bindRequest)).To(Succeed())

				statusErr := kerrors.NewServerTimeout(schema.GroupResource{
					Group: schedulingv1alpha2.GroupVersion.Group, Resource: "bindrequests",
				}, "patch", 1)
				statusPatches := 0
				reconciler.Client = interceptor.NewClient(fakeClient, interceptor.Funcs{
					SubResourcePatch: func(ctx context.Context, c client.Client, subResource string,
						obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
						if _, ok := obj.(*schedulingv1alpha2.BindRequest); ok {
							statusPatches++
							if statusPatches == 1 && writeFailure != "none" {
								if writeFailure == "after commit" {
									Expect(c.SubResource(subResource).Patch(ctx, obj, patch, opts...)).To(Succeed())
								}
								return statusErr
							}
						}
						return c.SubResource(subResource).Patch(ctx, obj, patch, opts...)
					},
				})

				var bindErr error
				phase, conditionStatus, eventReason := schedulingv1alpha2.BindRequestPhaseSucceeded, v1.ConditionTrue, "Normal Bound"
				bindCalls := 1
				if bindingFails {
					bindErr = errors.New("temporary binding failure")
					phase, conditionStatus, eventReason = schedulingv1alpha2.BindRequestPhaseFailed, v1.ConditionFalse, "Warning BindingError"
					if writeFailure == "before commit" || withBackoff {
						bindCalls++
					}
				}
				mockBinder := mock_binder.NewMockInterface(gomock.NewController(GinkgoT()))
				mockBinder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, pod *v1.Pod, node *v1.Node, _ *schedulingv1alpha2.BindRequest) error {
						if bindErr == nil {
							pod.Spec.NodeName = node.Name
							return fakeClient.Update(ctx, pod)
						}
						return bindErr
					}).Times(bindCalls)
				if bindingFails {
					mockBinder.EXPECT().Rollback(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(bindCalls)
				}
				reconciler.binder = mockBinder
				req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(bindRequest)}

				result, err := reconciler.Reconcile(ctx, req)
				if writeFailure != "none" {
					Expect(err).To(MatchError(statusErr))
					Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeFalse())
					Expect(result).To(Equal(ctrl.Result{}))
				} else if bindingFails {
					Expect(err).To(MatchError(bindErr))
					Expect(errors.Is(err, reconcile.TerminalError(nil))).To(Equal(!withBackoff))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
				stored := &schedulingv1alpha2.BindRequest{}
				Expect(fakeClient.Get(ctx, req.NamespacedName, stored)).To(Succeed())
				if writeFailure == "before commit" {
					Expect(stored.Status).To(Equal(bindRequest.Status))
				} else {
					Expect(stored.Status.Phase).To(Equal(phase))
				}
				Expect(shouldEnqueueBindRequestUpdate(bindRequest, stored)).To(BeFalse())
				storedPod := &v1.Pod{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(pod), storedPod)).To(Succeed())
				Expect(storedPod.Status.Conditions).To(ContainElement(And(
					HaveField("Type", v1.PodConditionType(podBoundCondition)),
					HaveField("Status", conditionStatus),
				)))
				Expect(fakeEventRecorder.Events).To(Receive(ContainSubstring(eventReason)))

				// Once the bound Pod is observed, a status retry must not bind or roll back again.
				_, err = reconciler.Reconcile(ctx, req)
				if bindCalls == 2 {
					Expect(err).To(MatchError(bindErr))
					Expect(errors.Is(err, reconcile.TerminalError(nil))).To(Equal(!withBackoff))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
				Expect(fakeClient.Get(ctx, req.NamespacedName, stored)).To(Succeed())
				Expect(stored.Status.Phase).To(Equal(phase))
				if bindingFails && withBackoff {
					expectedAttempts := int32(2)
					if writeFailure == "before commit" {
						expectedAttempts = 1
					}
					Expect(stored.Status.FailedAttempts).To(Equal(expectedAttempts))
				}
				if writeFailure == "before commit" || bindCalls == 2 {
					Expect(statusPatches).To(Equal(2))
					Expect(fakeEventRecorder.Events).To(Receive(ContainSubstring(eventReason)))
				} else {
					Expect(statusPatches).To(Equal(1))
				}
				Expect(fakeEventRecorder.Events).To(BeEmpty())
			},
			Entry("successful binding, write rejected", "before commit", false, false),
			Entry("failed binding without retry budget, write rejected", "before commit", true, false),
			Entry("failed binding with retry budget, write rejected", "before commit", true, true),
			Entry("successful binding, response lost after write", "after commit", false, false),
			Entry("failed binding without retry budget, response lost after write", "after commit", true, false),
			Entry("failed binding with retry budget, response lost after write", "after commit", true, true),
			Entry("successful binding, ordinary write", "none", false, false),
			Entry("failed binding without retry budget, ordinary write", "none", true, false),
			Entry("failed binding with retry budget, ordinary write", "none", true, true),
		)

		var (
			pod = &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod",
					Namespace: "default",
				},
			}
			node = &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node",
				},
			}
		)

		DescribeTable(
			"reconcile single pod",
			func(objects []client.Object, bindRequest *schedulingv1alpha2.BindRequest, expectedError, expectedRequeue, expectBinding bool) {
				for _, object := range objects {
					Expect(fakeClient.Create(context.TODO(), object)).Should(Succeed())
				}

				Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())

				mockBinder := mock_binder.NewMockInterface(gomock.NewController(GinkgoT()))
				if expectBinding {
					mockBinder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				}
				reconciler.binder = mockBinder

				result, err := reconciler.Reconcile(context.TODO(), ctrl.Request{
					NamespacedName: client.ObjectKey{
						Namespace: bindRequest.Namespace,
						Name:      bindRequest.Name,
					},
				})
				if expectedError {
					Expect(err).Should(HaveOccurred())
				} else {
					Expect(err).Should(BeNil())
				}
				Expect(result.Requeue).To(Equal(false))
				Expect(result.RequeueAfter != 0).To(Equal(expectedRequeue))
			},
			Entry(
				"sanity test",
				[]client.Object{pod.DeepCopy(), node.DeepCopy()},
				func() *schedulingv1alpha2.BindRequest {
					bindRequest := baseRequest.DeepCopy()
					bindRequest.Spec.PodName = pod.Name
					bindRequest.Spec.SelectedNode = node.Name
					return bindRequest
				}(),
				false, false, true,
			),
			Entry(
				"no node",
				[]client.Object{pod.DeepCopy()},
				func() *schedulingv1alpha2.BindRequest {
					bindRequest := baseRequest.DeepCopy()
					bindRequest.Spec.PodName = pod.Name
					bindRequest.Spec.SelectedNode = node.Name
					return bindRequest
				}(),
				true, false, false,
			),
			Entry(
				"no pod",
				[]client.Object{node.DeepCopy()},
				func() *schedulingv1alpha2.BindRequest {
					bindRequest := baseRequest.DeepCopy()
					bindRequest.Spec.PodName = pod.Name
					bindRequest.Spec.SelectedNode = node.Name
					return bindRequest
				}(),
				true, false, false,
			),
			Entry(
				"pod already bound to node",
				[]client.Object{
					node.DeepCopy(),
					func() *v1.Pod {
						pod := pod.DeepCopy()
						pod.Spec.NodeName = node.Name
						return pod
					}(),
				},
				func() *schedulingv1alpha2.BindRequest {
					bindRequest := baseRequest.DeepCopy()
					bindRequest.Spec.PodName = pod.Name
					bindRequest.Spec.SelectedNode = node.Name
					bindRequest.Spec.BackoffLimit = ptr.To(int32(1))
					return bindRequest
				}(),
				false, false, false,
			),
			Entry(
				"pod already bound to another node",
				[]client.Object{
					node.DeepCopy(),
					func() *v1.Pod {
						pod := pod.DeepCopy()
						pod.Spec.NodeName = node.Name + "-other"
						return pod
					}(),
				},
				func() *schedulingv1alpha2.BindRequest {
					bindRequest := baseRequest.DeepCopy()
					bindRequest.Spec.PodName = pod.Name
					bindRequest.Spec.SelectedNode = node.Name
					return bindRequest
				}(),
				false, false, false,
			),
			Entry(
				"missing node but backoff limit not reached",
				[]client.Object{
					pod.DeepCopy(),
				},
				func() *schedulingv1alpha2.BindRequest {
					bindRequest := baseRequest.DeepCopy()
					bindRequest.Spec.PodName = pod.Name
					bindRequest.Spec.SelectedNode = node.Name
					bindRequest.Spec.BackoffLimit = ptr.To(int32(1))
					return bindRequest
				}(),
				true, true, false,
			),
			Entry(
				"missing node but backoff limit already reached",
				[]client.Object{
					pod.DeepCopy(),
				},
				func() *schedulingv1alpha2.BindRequest {
					bindRequest := baseRequest.DeepCopy()
					bindRequest.Spec.PodName = pod.Name
					bindRequest.Spec.SelectedNode = node.Name
					bindRequest.Spec.BackoffLimit = ptr.To(int32(2))
					bindRequest.Status.FailedAttempts = int32(2)
					return bindRequest
				}(),
				true, false, false,
			),
		)

		Context("multiple pods", func() {
			It("handles multiple pods concurrently", func() {
				Expect(fakeClient.Create(context.TODO(), node)).Should(Succeed())

				pods := []*v1.Pod{}
				bindRequests := []*schedulingv1alpha2.BindRequest{}

				numberOfPods := 10
				for i := 0; i < numberOfPods; i++ {
					pod := pod.DeepCopy()
					pod.Name = fmt.Sprintf("pod-%d", i)
					Expect(fakeClient.Create(context.TODO(), pod)).Should(Succeed())
					pods = append(pods, pod)

					bindRequest := baseRequest.DeepCopy()
					bindRequest.Name = fmt.Sprintf("bind-request-%d", i)
					bindRequest.Spec.PodName = pod.Name
					bindRequest.Spec.SelectedNode = node.Name
					Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
					bindRequests = append(bindRequests, bindRequest)
				}

				mockBinder := mock_binder.NewMockInterface(gomock.NewController(GinkgoT()))
				mockBinder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(numberOfPods)
				reconciler.binder = mockBinder

				wg := sync.WaitGroup{}
				for i := 0; i < numberOfPods; i++ {
					wg.Add(1)
					go func(i int) {
						defer GinkgoRecover()
						defer wg.Done()
						_, err := reconciler.Reconcile(context.TODO(), ctrl.Request{
							NamespacedName: client.ObjectKey{
								Namespace: "default",
								Name:      bindRequests[i].Name,
							},
						})
						Expect(err).Should(BeNil())
					}(i)
				}

				wg.Wait()

				for i := 0; i < numberOfPods; i++ {
					updatedBindRequest := &schedulingv1alpha2.BindRequest{}
					Expect(fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(bindRequests[i]), updatedBindRequest)).Should(Succeed())
					Expect(updatedBindRequest.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseSucceeded))

					updatedPod := &v1.Pod{}
					Expect(fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(pods[i]), updatedPod)).Should(Succeed())

					foundCondition := false
					for _, condition := range updatedPod.Status.Conditions {
						if condition.Type == podBoundCondition {
							Expect(condition.Status).To(Equal(v1.ConditionTrue))
							Expect(condition.Reason).To(Equal("Bound"))
							foundCondition = true
						}
					}
					Expect(foundCondition).To(BeTrue())
				}
			})
		})

		Context("repeated bind failure", func() {
			It("persists consecutive failures and stops binding at the backoff limit", func() {
				ctx := context.Background()
				retryPod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "retry-pod", Namespace: "default"}}
				retryNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "retry-node"}}
				Expect(fakeClient.Create(ctx, retryPod)).To(Succeed())
				Expect(fakeClient.Create(ctx, retryNode)).To(Succeed())

				bindRequest := baseRequest.DeepCopy()
				bindRequest.Spec.PodName = retryPod.Name
				bindRequest.Spec.SelectedNode = retryNode.Name
				bindRequest.Spec.BackoffLimit = ptr.To(int32(2))
				Expect(fakeClient.Create(ctx, bindRequest)).To(Succeed())

				bindErr := errors.New("temporary binding failure")
				mockBinder := mock_binder.NewMockInterface(gomock.NewController(GinkgoT()))
				mockBinder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(bindErr).Times(2)
				mockBinder.EXPECT().Rollback(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).Times(2)
				reconciler.binder = mockBinder

				req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(bindRequest)}
				for attempt := int32(1); attempt <= 2; attempt++ {
					result, err := reconciler.Reconcile(ctx, req)
					stored := &schedulingv1alpha2.BindRequest{}
					Expect(fakeClient.Get(ctx, req.NamespacedName, stored)).To(Succeed())
					Expect(stored.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseFailed))
					Expect(stored.Status.FailedAttempts).To(Equal(attempt))
					Expect(stored.Status.Reason).To(Equal(bindErr.Error()))
					Expect(err).To(MatchError(bindErr))
					Expect(result.RequeueAfter).To(Equal((1 << (attempt - 1)) * time.Second))
				}

				result, err := reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
				Expect(fakeEventRecorder.Events).To(HaveLen(2))
				for range 2 {
					event := <-fakeEventRecorder.Events
					Expect(event).To(ContainSubstring("BindingError"))
					Expect(event).NotTo(ContainSubstring("bound successfully"))
				}
			})

			It("stops retrying after a successful bind without repeating the Bound event", func() {
				ctx := context.Background()
				retryPod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "retry-pod", Namespace: "default"}}
				retryNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "retry-node"}}
				Expect(fakeClient.Create(ctx, retryPod)).To(Succeed())
				Expect(fakeClient.Create(ctx, retryNode)).To(Succeed())

				bindRequest := baseRequest.DeepCopy()
				bindRequest.Spec.PodName = retryPod.Name
				bindRequest.Spec.SelectedNode = retryNode.Name
				bindRequest.Spec.BackoffLimit = ptr.To(int32(2))
				Expect(fakeClient.Create(ctx, bindRequest)).To(Succeed())

				bindErr := errors.New("temporary binding failure")
				mockBinder := mock_binder.NewMockInterface(gomock.NewController(GinkgoT()))
				gomock.InOrder(
					mockBinder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(bindErr),
					mockBinder.EXPECT().Rollback(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
					mockBinder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil),
				)
				reconciler.binder = mockBinder

				req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(bindRequest)}
				_, err := reconciler.Reconcile(ctx, req)
				Expect(err).To(MatchError(bindErr))

				result, err := reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
				stored := &schedulingv1alpha2.BindRequest{}
				Expect(fakeClient.Get(ctx, req.NamespacedName, stored)).To(Succeed())
				Expect(stored.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseSucceeded))
				Expect(stored.Status.FailedAttempts).To(Equal(int32(1)))

				result, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))
				Expect(fakeEventRecorder.Events).To(HaveLen(2))
				Expect(<-fakeEventRecorder.Events).To(ContainSubstring("BindingError"))
				Expect(<-fakeEventRecorder.Events).To(ContainSubstring("Normal Bound Pod bound successfully"))
			})

			It("report BindingError num of FailedAttempts 1 in this case) and stop reconciling the bindingrequest", func() {
				repeatFailurePod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "repeat-failure-pod",
						Namespace: "default",
					},
				}
				repeatFailureNode := &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "repeat-failure-node",
					},
				}
				Expect(fakeClient.Create(context.TODO(), repeatFailureNode)).Should(Succeed())
				Expect(fakeClient.Create(context.TODO(), repeatFailurePod)).Should(Succeed())

				bindRequest := baseRequest.DeepCopy()
				bindRequest.Spec.PodName = repeatFailurePod.Name
				bindRequest.Spec.SelectedNode = repeatFailureNode.Name
				Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())

				mockBinder := mock_binder.NewMockInterface(gomock.NewController(GinkgoT()))
				mockBinder.EXPECT().Bind(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(errors.New("admission webhook denied the request")).Times(1)
				mockBinder.EXPECT().Rollback(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				reconciler.binder = mockBinder

				req := ctrl.Request{
					NamespacedName: client.ObjectKey{
						Namespace: bindRequest.Namespace,
						Name:      bindRequest.Name,
					},
				}

				// first reconcile: Pending -> Failed, a real phase change
				_, err := reconciler.Reconcile(context.TODO(), req)
				Expect(err).Should(HaveOccurred())

				// second reconcile on the same, due to the phase being failed and no binding backoffs left, the reconciler returns early without updating the status
				_, err = reconciler.Reconcile(context.TODO(), req)
				Expect(err).Should(BeNil())

				close(fakeEventRecorder.Events)
				var events []string
				for event := range fakeEventRecorder.Events {
					events = append(events, event)
				}
				Expect(events).To(HaveLen(1))
				for _, event := range events {
					Expect(event).NotTo(ContainSubstring("bound successfully"))
					Expect(event).To(ContainSubstring("BindingError"))
				}
			})
		})
	})

	Describe("event handlers", func() {
		var queue workqueue.TypedRateLimitingInterface[reconcile.Request]

		BeforeEach(func() {
			queue = workqueue.NewTypedRateLimitingQueue(
				workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			DeferCleanup(queue.ShutDown)
		})

		assertCreateQueueLength := func(bindRequest *schedulingv1alpha2.BindRequest, expected int) {
			createQueue := workqueue.NewTypedRateLimitingQueue(
				workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer createQueue.ShutDown()

			reconciler.eventHandlers().CreateFunc(context.TODO(),
				event.CreateEvent{Object: bindRequest}, createQueue)
			Expect(createQueue.Len()).To(Equal(expected))
		}

		It("enqueues only actionable BindRequest creates", func() {
			assertCreateQueueLength(baseRequest.DeepCopy(), 1)

			succeeded := baseRequest.DeepCopy()
			succeeded.Status.Phase = schedulingv1alpha2.BindRequestPhaseSucceeded
			assertCreateQueueLength(succeeded, 0)

			failedWithoutBackoff := baseRequest.DeepCopy()
			failedWithoutBackoff.Status.Phase = schedulingv1alpha2.BindRequestPhaseFailed
			assertCreateQueueLength(failedWithoutBackoff, 0)

			failedAfterBackoff := baseRequest.DeepCopy()
			failedAfterBackoff.Status.Phase = schedulingv1alpha2.BindRequestPhaseFailed
			failedAfterBackoff.Spec.BackoffLimit = ptr.To(int32(1))
			failedAfterBackoff.Status.FailedAttempts = 1
			assertCreateQueueLength(failedAfterBackoff, 0)

			deleted := baseRequest.DeepCopy()
			deleted.DeletionTimestamp = ptr.To(metav1.Now())
			assertCreateQueueLength(deleted, 0)

			failedWithRetry := baseRequest.DeepCopy()
			failedWithRetry.Status.Phase = schedulingv1alpha2.BindRequestPhaseFailed
			failedWithRetry.Spec.BackoffLimit = ptr.To(int32(2))
			failedWithRetry.Status.FailedAttempts = 1
			assertCreateQueueLength(failedWithRetry, 1)
		})

		It("enqueues only actionable BindRequest spec updates", func() {
			oldBindRequest := baseRequest.DeepCopy()
			updatedBindRequest := oldBindRequest.DeepCopy()
			updatedBindRequest.Generation = oldBindRequest.Generation + 1
			reconciler.eventHandlers().UpdateFunc(context.TODO(), event.UpdateEvent{
				ObjectOld: oldBindRequest,
				ObjectNew: updatedBindRequest,
			}, queue)
			Expect(queue.Len()).To(Equal(1))

			request, shutdown := queue.Get()
			Expect(shutdown).To(BeFalse())
			queue.Done(request)

			terminal := updatedBindRequest.DeepCopy()
			terminal.Status.Phase = schedulingv1alpha2.BindRequestPhaseSucceeded
			terminal.Generation++
			reconciler.eventHandlers().UpdateFunc(context.TODO(), event.UpdateEvent{
				ObjectOld: updatedBindRequest,
				ObjectNew: terminal,
			}, queue)
			Expect(queue.Len()).To(Equal(0))
		})

		It("enqueues updates of a BindRequest replaced under the same name", func() {
			oldBindRequest := baseRequest.DeepCopy()
			oldBindRequest.UID = "old-uid"
			replacement := oldBindRequest.DeepCopy()
			replacement.UID = "new-uid"

			reconciler.eventHandlers().UpdateFunc(context.TODO(), event.UpdateEvent{
				ObjectOld: oldBindRequest,
				ObjectNew: replacement,
			}, queue)
			Expect(queue.Len()).To(Equal(1))
		})

		It("does not enqueue status-only retry updates", func() {
			oldBindRequest := baseRequest.DeepCopy()
			oldBindRequest.Spec.BackoffLimit = ptr.To(int32(3))
			oldBindRequest.Status.Phase = schedulingv1alpha2.BindRequestPhaseFailed
			oldBindRequest.Status.FailedAttempts = 1
			updatedBindRequest := oldBindRequest.DeepCopy()
			updatedBindRequest.Status.FailedAttempts = 2
			updatedBindRequest.Status.Reason = "another temporary binding failure"

			reconciler.eventHandlers().UpdateFunc(context.Background(), event.UpdateEvent{
				ObjectOld: oldBindRequest,
				ObjectNew: updatedBindRequest,
			}, queue)
			Expect(queue.Len()).To(Equal(0))
		})
	})

	Describe("UpdateStatus", func() {
		DescribeTable("does not patch unchanged status", func(phase string, outcome error) {
			ctx := context.Background()
			bindRequest := baseRequest.DeepCopy()
			bindRequest.Status.Phase = phase
			if outcome != nil {
				bindRequest.Status.Reason = outcome.Error()
			}
			Expect(fakeClient.Create(ctx, bindRequest)).To(Succeed())

			patchCalls := 0
			reconciler.Client = interceptor.NewClient(fakeClient, interceptor.Funcs{
				SubResourcePatch: func(context.Context, client.Client, string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
					patchCalls++
					return nil
				},
			})

			result, err := reconciler.UpdateStatus(ctx, bindRequest, ctrl.Result{}, outcome)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
			Expect(patchCalls).To(Equal(0))
		},
			Entry("succeeded", schedulingv1alpha2.BindRequestPhaseSucceeded, nil),
			Entry("failed without retries", schedulingv1alpha2.BindRequestPhaseFailed, errors.New("binding failure")),
		)

		Context("no error", func() {
			It("Leavs FailedAttempts unchanged", func() {
				bindRequest := baseRequest.DeepCopy()
				bindRequest.Status.FailedAttempts = int32(1)

				Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
				reconciler.UpdateStatus(context.TODO(), bindRequest, ctrl.Result{}, nil)
				Expect(bindRequest.Status.FailedAttempts).To(Equal(int32(1)))
			})

			It("Set Phase to Succeeded", func() {
				bindRequest := baseRequest.DeepCopy()

				Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
				reconciler.UpdateStatus(context.TODO(), bindRequest, ctrl.Result{}, nil)
				Expect(bindRequest.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseSucceeded))
			})
		})

		Context("error", func() {
			It("Sets Phase to Failed", func() {
				bindRequest := baseRequest.DeepCopy()

				Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
				reconciler.UpdateStatus(context.TODO(), bindRequest, ctrl.Result{Requeue: true}, errors.New("error"))
				Expect(bindRequest.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseFailed))
			})

			Context("Backoff Limit set", func() {
				It("persists the latest retry failure and returns its error when the phase is unchanged", func() {
					ctx := context.Background()
					bindRequest := baseRequest.DeepCopy()
					bindRequest.Spec.BackoffLimit = ptr.To(int32(3))
					bindRequest.Status.Phase = schedulingv1alpha2.BindRequestPhaseFailed
					bindRequest.Status.FailedAttempts = 1
					bindRequest.Status.Reason = "first binding failure"
					Expect(fakeClient.Create(ctx, bindRequest)).To(Succeed())

					bindErr := errors.New("another temporary binding failure")
					result, err := reconciler.UpdateStatus(ctx, bindRequest, ctrl.Result{}, bindErr)
					Expect(err).To(MatchError(bindErr))
					Expect(errors.Is(err, reconcile.TerminalError(nil))).To(BeFalse())
					Expect(result.RequeueAfter).To(Equal(2 * time.Second))

					stored := &schedulingv1alpha2.BindRequest{}
					Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(bindRequest), stored)).To(Succeed())
					Expect(stored.Status.Phase).To(Equal(schedulingv1alpha2.BindRequestPhaseFailed))
					Expect(stored.Status.FailedAttempts).To(Equal(int32(2)))
					Expect(stored.Status.Reason).To(Equal(bindErr.Error()))
				})

				It("Increments FailedAttempts if less than BackoffLimit", func() {
					bindRequest := baseRequest.DeepCopy()
					bindRequest.Spec.BackoffLimit = ptr.To(int32(5))
					bindRequest.Status.FailedAttempts = int32(0)

					Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
					reconciler.UpdateStatus(context.TODO(), bindRequest, ctrl.Result{Requeue: true}, errors.New("error"))
					Expect(bindRequest.Status.FailedAttempts).To(Equal(int32(1)))
				})

				It("Sets delayed requeue if less than BackoffLimit", func() {
					bindRequest := baseRequest.DeepCopy()
					bindRequest.Spec.BackoffLimit = ptr.To(int32(5))
					bindRequest.Status.FailedAttempts = int32(3)

					Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
					res, _ := reconciler.UpdateStatus(context.TODO(), bindRequest, ctrl.Result{Requeue: true}, errors.New("error"))

					Expect(bindRequest.Status.FailedAttempts).To(Equal(int32(4)))
					Expect(res.RequeueAfter).To(Equal((8) * time.Second))
				})
			})
		})

		Context("BindRequest changed during the reconcile", func() {
			It("discards retry progress when a newer resource version is present", func() {
				ctx := context.Background()
				original := baseRequest.DeepCopy()
				original.Spec.SelectedNode = "stale-node"
				original.Spec.BackoffLimit = ptr.To(int32(3))
				original.Status.Phase = schedulingv1alpha2.BindRequestPhaseFailed
				original.Status.FailedAttempts = 1
				original.Status.Reason = "first binding failure"
				Expect(fakeClient.Create(ctx, original)).To(Succeed())
				stale := original.DeepCopy()

				newer := original.DeepCopy()
				newer.Spec.SelectedNode = "replacement-node"
				Expect(fakeClient.Update(ctx, newer)).To(Succeed())

				_, err := reconciler.UpdateStatus(ctx, stale, ctrl.Result{}, errors.New("stale retry failure"))
				Expect(err).NotTo(HaveOccurred())

				stored := &schedulingv1alpha2.BindRequest{}
				Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(original), stored)).To(Succeed())
				Expect(stored.Spec.SelectedNode).To(Equal(newer.Spec.SelectedNode))
				Expect(stored.Status).To(Equal(newer.Status))
			})

			// A reconcile holds its own copy for the length of the bind, during which the
			// scheduler may delete the BindRequest and create a replacement under the same
			// name. The outcome must not land on an object it does not describe. The fake
			// client reuses resourceVersion "1" after a delete and create, so the
			// replacement is staged here as an in-place change, which is the same
			// precondition failure a real API server reports for a recreated object.
			It("discards the outcome instead of stamping the newer object", func() {
				original := baseRequest.DeepCopy()
				original.Spec.SelectedNode = "stale-node"
				Expect(fakeClient.Create(context.TODO(), original)).Should(Succeed())

				stale := &schedulingv1alpha2.BindRequest{}
				Expect(fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(original), stale)).Should(Succeed())

				newer := &schedulingv1alpha2.BindRequest{}
				Expect(fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(original), newer)).Should(Succeed())
				newer.Spec.SelectedNode = "replacement-node"
				Expect(fakeClient.Update(context.TODO(), newer)).Should(Succeed())

				_, err := reconciler.UpdateStatus(context.TODO(), stale, ctrl.Result{}, nil)
				Expect(err).Should(BeNil())

				live := &schedulingv1alpha2.BindRequest{}
				Expect(fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(original), live)).Should(Succeed())
				Expect(live.Spec.SelectedNode).To(Equal("replacement-node"))
				Expect(live.Status.Phase).NotTo(Equal(schedulingv1alpha2.BindRequestPhaseSucceeded))
			})
		})
	})

	Describe("updatePodCondition", func() {
		var (
			pod         *v1.Pod
			bindRequest *schedulingv1alpha2.BindRequest
		)
		BeforeEach(func() {
			bindRequest = baseRequest.DeepCopy()
			bindRequest.Spec = schedulingv1alpha2.BindRequestSpec{
				PodName:      "pod-1",
				SelectedNode: "node-1",
			}
			pod = &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: bindRequest.Namespace,
					Name:      bindRequest.Spec.PodName,
				},
			}
		})
		Context("success", func() {
			BeforeEach(func() {
				bindRequest.Status.Phase = schedulingv1alpha2.BindRequestPhaseSucceeded
			})

			It("updates pod condition", func() {
				Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
				Expect(fakeClient.Create(context.TODO(), pod)).Should(Succeed())

				reconciler.updatePodCondition(context.TODO(), bindRequest, pod)

				updatedPod := &v1.Pod{}
				Expect(fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(pod), updatedPod)).Should(Succeed())

				Expect(updatedPod.Status.Conditions).To(HaveLen(1))
				Expect(string(updatedPod.Status.Conditions[0].Type)).To(Equal(podBoundCondition))
				Expect(updatedPod.Status.Conditions[0].Status).To(Equal(v1.ConditionTrue))
				Expect(updatedPod.Status.Conditions[0].Reason).To(Equal("Bound"))
				Expect(updatedPod.Status.Conditions[0].Message).To(Equal("Pod bound successfully to node node-1"))
			})

			It("publishes an event on the pod", func() {
				Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
				Expect(fakeClient.Create(context.TODO(), pod)).Should(Succeed())

				reconciler.updatePodCondition(context.TODO(), bindRequest, pod)

				close(fakeEventRecorder.Events)

				Expect(fakeEventRecorder.Events).To(HaveLen(1))
				for event := range fakeEventRecorder.Events {
					Expect(event).To(ContainSubstring("Bound"))
					Expect(event).To(ContainSubstring("Pod bound successfully to node node-1"))
				}
			})
		})

		Context("error", func() {
			var (
				expectedFailMsg string
			)
			BeforeEach(func() {
				bindRequest.Status.Phase = schedulingv1alpha2.BindRequestPhaseFailed
				bindRequest.Status.Reason = "error"
				expectedFailMsg = fmt.Sprintf("Failed to bind pod %s/%s to node %s: error", pod.Namespace, pod.Name, bindRequest.Spec.SelectedNode)
			})

			It("updates pod condition", func() {
				Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
				Expect(fakeClient.Create(context.TODO(), pod)).Should(Succeed())

				reconciler.updatePodCondition(context.TODO(), bindRequest, pod)

				updatedPod := &v1.Pod{}
				Expect(fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(pod), updatedPod)).Should(Succeed())

				Expect(updatedPod.Status.Conditions).To(HaveLen(1))
				Expect(string(updatedPod.Status.Conditions[0].Type)).To(Equal(podBoundCondition))
				Expect(updatedPod.Status.Conditions[0].Status).To(Equal(v1.ConditionFalse))
				Expect(updatedPod.Status.Conditions[0].Reason).To(Equal("BindingError"))
				Expect(updatedPod.Status.Conditions[0].Message).To(Equal(expectedFailMsg))
			})

			It("publishes an event on the pod", func() {
				Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
				Expect(fakeClient.Create(context.TODO(), pod)).Should(Succeed())

				reconciler.updatePodCondition(context.TODO(), bindRequest, pod)

				close(fakeEventRecorder.Events)

				Expect(fakeEventRecorder.Events).To(HaveLen(1))
				for event := range fakeEventRecorder.Events {
					Expect(event).To(ContainSubstring("BindingError"))
					Expect(event).To(ContainSubstring(expectedFailMsg))
				}
			})
		})
	})
})
