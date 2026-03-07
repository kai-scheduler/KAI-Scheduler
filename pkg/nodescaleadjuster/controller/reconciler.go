// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/consts"
	"github.com/kai-scheduler/kai-scheduler/pkg/nodescaleadjuster/scale_adjuster"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	ScaleAdjuster      *scale_adjuster.ScaleAdjuster
	NodeScaleNamespace string
	SchedulerName      string
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=create;patch;update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.V(1).Info("Reconciling pod", req.Namespace, req.Name)
	isInCoolDown, err := r.ScaleAdjuster.Adjust()
	if err != nil {
		logger.V(1).Error(err, "failed to adjust unschedulable pods")
		return ctrl.Result{}, err
	}
	if isInCoolDown {
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Second * consts.DefaultCoolDownSeconds,
		}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Pod{}).
		WithEventFilter(&eventPredicate{
			nodeScaleNamespace: r.NodeScaleNamespace,
			schedulerName:      r.SchedulerName,
		}).
		Complete(r)
}

type eventPredicate struct {
	nodeScaleNamespace string
	schedulerName      string
}

func (ep *eventPredicate) filter(object client.Object) bool {
	pod, ok := object.(*v1.Pod)
	if !ok {
		return false
	}

	if pod.Namespace == ep.nodeScaleNamespace {
		return true
	}

	return pod.Spec.SchedulerName == ep.schedulerName
}

func (ep *eventPredicate) Create(e event.CreateEvent) bool {
	return ep.filter(e.Object)
}

func (ep *eventPredicate) Update(e event.UpdateEvent) bool {
	return ep.filter(e.ObjectNew)
}

func (ep *eventPredicate) Delete(e event.DeleteEvent) bool {
	return ep.filter(e.Object)
}

func (ep *eventPredicate) Generic(e event.GenericEvent) bool {
	return ep.filter(e.Object)
}

// TypedPredicate filters events before enqueuing the keys.
type TypedPredicate[object any] interface {
	// Create returns true if the Create event should be processed
	Create(event.TypedCreateEvent[object]) bool

	// Delete returns true if the Delete event should be processed
	Delete(event.TypedDeleteEvent[object]) bool

	// Update returns true if the Update event should be processed
	Update(event.TypedUpdateEvent[object]) bool

	// Generic returns true if the Generic event should be processed
	Generic(event.TypedGenericEvent[object]) bool
}
