// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	Client              client.Client
	Scheme              *runtime.Scheme
	ResourceReservation resourcereservation.Interface
	SchedulerName       string
}

type ReconcilerParams struct {
	MaxConcurrentReconciles     int
	RateLimiterBaseDelaySeconds int
	RateLimiterMaxDelaySeconds  int
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(
	mgr ctrl.Manager, params *ReconcilerParams,
) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Watches(&corev1.Pod{}, r.eventHandlers()).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: params.MaxConcurrentReconciles,
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				time.Duration(params.RateLimiterBaseDelaySeconds)*time.Second,
				time.Duration(params.RateLimiterMaxDelaySeconds)*time.Second,
			),
			SkipNameValidation: &[]bool{true}[0],
		}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}

func (r *PodReconciler) eventHandlers() handler.Funcs {
	return handler.Funcs{
		CreateFunc: func(ctx context.Context, createEvent event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if !r.isRelevantPod(createEvent.Object) {
				return
			}
			h := handler.EnqueueRequestForObject{}
			h.Create(ctx, createEvent, q)
		},
		UpdateFunc: func(ctx context.Context, updateEvent event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if !r.isRelevantPod(updateEvent.ObjectNew) {
				return
			}
			if isCompletionEvent(updateEvent.ObjectOld, updateEvent.ObjectNew) {
				r.syncReservationIfNeeded(ctx, updateEvent.ObjectNew)
			}
			h := handler.EnqueueRequestForObject{}
			h.Update(ctx, updateEvent, q)
		},
		DeleteFunc: func(ctx context.Context, deleteEvent event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if !r.isRelevantPod(deleteEvent.Object) {
				return
			}
			r.syncReservationIfNeeded(ctx, deleteEvent.Object)

			h := handler.EnqueueRequestForObject{}
			h.Delete(ctx, deleteEvent, q)
		},
		GenericFunc: func(ctx context.Context, genericEvent event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if !r.isRelevantPod(genericEvent.Object) {
				return
			}
			h := handler.EnqueueRequestForObject{}
			h.Generic(ctx, genericEvent, q)
		},
	}
}

func (r *PodReconciler) syncReservationIfNeeded(ctx context.Context, object client.Object) {
	logger := log.FromContext(ctx)
	pod, isPod := object.(*corev1.Pod)
	if !isPod {
		return
	}
	gpuGroups := resources.GetGpuGroups(pod)
	if len(gpuGroups) == 0 {
		return
	}

	for _, gpuGroup := range gpuGroups {
		err := r.ResourceReservation.SyncForGpuGroup(ctx, gpuGroup)
		if err != nil {
			logger.Error(err, "failed to sync reservation on pod",
				"name", pod.Name, "namespace", pod.Namespace)
		}
	}
}

func (r *PodReconciler) isRelevantPod(object client.Object) bool {
	pod, isPod := object.(*corev1.Pod)
	if isPod {
		return pod.Spec.SchedulerName == r.SchedulerName
	}
	return false
}

func isCompletionEvent(oldObject client.Object, newObject client.Object) bool {
	oldPod, isPod := oldObject.(*corev1.Pod)
	if !isPod {
		return false
	}

	newPod, isPod := newObject.(*corev1.Pod)
	if !isPod {
		return false
	}

	return oldPod.Status.Phase != newPod.Status.Phase &&
		slices.Contains(
			[]corev1.PodPhase{corev1.PodFailed, corev1.PodSucceeded},
			newPod.Status.Phase)
}
