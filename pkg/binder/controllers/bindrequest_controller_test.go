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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kubeaischedulerscheme "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned/scheme"
	schedulingv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"

	"github.com/NVIDIA/KAI-scheduler/pkg/binder/binding"
	mock_binder "github.com/NVIDIA/KAI-scheduler/pkg/binder/binding/mock"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/plugins"
	mockplugins "github.com/NVIDIA/KAI-scheduler/pkg/binder/plugins/mock"
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
			resourceReservationNameSpace, resourceReservationServiceAccount, resourceReservationAppLabelValue, scalingPodsNamespace, constants.DefaultRuntimeClassName,
			nil, nil, nil)
		binder := binding.NewBinder(fakeClient, rrs, binderPlugins)
		reconciler = NewBindRequestReconciler(fakeClient, testScheme, fakeEventRecorder, params,
			binder, rrs)
	})

	Describe("Reconcile", func() {
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
	})

	Describe("UpdateStatus", func() {
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
			It("updates pod condition", func() {
				Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
				Expect(fakeClient.Create(context.TODO(), pod)).Should(Succeed())

				reconciler.updatePodCondition(context.TODO(), bindRequest, pod, ctrl.Result{}, nil)

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

				reconciler.updatePodCondition(context.TODO(), bindRequest, pod, ctrl.Result{}, nil)

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
				expectedFailMsg = fmt.Sprintf("Failed to bind pod %s/%s to node %s: error", pod.Namespace, pod.Name, bindRequest.Spec.SelectedNode)
			})

			It("updates pod condition", func() {
				Expect(fakeClient.Create(context.TODO(), bindRequest)).Should(Succeed())
				Expect(fakeClient.Create(context.TODO(), pod)).Should(Succeed())

				reconciler.updatePodCondition(context.TODO(), bindRequest, pod, ctrl.Result{}, errors.New("error"))

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

				reconciler.updatePodCondition(context.TODO(), bindRequest, pod, ctrl.Result{}, errors.New("error"))

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
