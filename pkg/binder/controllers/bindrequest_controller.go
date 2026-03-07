// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kai-scheduler/kai-scheduler/pkg/binder/binding"
	"github.com/kai-scheduler/kai-scheduler/pkg/binder/binding/resourcereservation"
	"github.com/kai-scheduler/kai-scheduler/pkg/binder/common"

	schedulingv1alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v1alpha2"
)

const (
	podBoundCondition = "PodBound"
)

// BindRequestReconciler reconciles a BindRequest object
type BindRequestReconciler struct {
	Client              client.Client
	Scheme              *runtime.Scheme
	binder              binding.Interface
	resourceReservation resourcereservation.Interface
	eventRecorder       record.EventRecorder
	params              *ReconcilerParams
}

func NewBindRequestReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	eventRecorder record.EventRecorder,
	params *ReconcilerParams,
	binder binding.Interface,
	resourceReservation resourcereservation.Interface,
) *BindRequestReconciler {
	return &BindRequestReconciler{
		Client:              client,
		Scheme:              scheme,
		binder:              binder,
		resourceReservation: resourceReservation,
		eventRecorder:       eventRecorder,
		params:              params,
	}
}

// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=core,resources=pods/binding,verbs=create;patch;update
// +kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=create;patch;update
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;list;watch;create;patch;update
// +kubebuilder:rbac:groups=resource.k8s.io,resources=deviceclasses;resourceslices,verbs=get;list;watch
// +kubebuilder:rbac:groups=resource.k8s.io,resources=resourceclaims,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=resource.k8s.io,resources=resourceclaims/status,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=scheduling.run.ai,resources=bindrequests,verbs=get;list;watch;patch;update;delete
// +kubebuilder:rbac:groups=scheduling.run.ai,resources=bindrequests/finalizers,verbs=patch;update
// +kubebuilder:rbac:groups=scheduling.run.ai,resources=bindrequests/status,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses;csinodes;csidrivers;csistoragecapacities,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
func (r *BindRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling BindRequest", "name", req.NamespacedName)

	var bindRequest = &schedulingv1alpha2.BindRequest{}
	var pod *v1.Pod

	// Fetch the BindRequest instance
	if err = r.Client.Get(ctx, req.NamespacedName, bindRequest); err != nil {
		if kerrors.IsNotFound(err) {
			logger.Info("BindRequest not found, probably deleted", "name", req.NamespacedName)
		}
		return result, client.IgnoreNotFound(err)
	}

	if bindRequest.DeletionTimestamp != nil {
		return result, nil
	}

	if bindRequest.Status.Phase == schedulingv1alpha2.BindRequestPhaseSucceeded {
		return result, nil
	}

	defer func() {
		var finalError error
		if r := recover(); r != nil {
			finalError = fmt.Errorf("Internal Error: %v\n%s", r, string(debug.Stack()))
			err = fmt.Errorf("Internal Error: %v", r)
		}

		result, err = r.UpdateStatus(ctx, bindRequest, result, err)
		if pod != nil {
			r.updatePodCondition(ctx, bindRequest, pod, result, err)
		}

		if finalError != nil {
			err = finalError
		}
	}()

	pod = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bindRequest.Spec.PodName,
			Namespace: bindRequest.Namespace,
		},
	}
	if err = r.Client.Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil {
		return result, err
	}
	if pod.Spec.NodeName != "" {
		logger.Info("Pod is already bound to node", "name", pod.Name, "namespace", pod.Namespace,
			"node", pod.Spec.NodeName)
		return result, nil
	}

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindRequest.Spec.SelectedNode,
		},
	}
	if err = r.Client.Get(ctx, client.ObjectKeyFromObject(node), node); err != nil {
		return result, err
	}

	logger.Info("Binding pod to node", "pod", pod.Name, "namespace", pod.Namespace,
		"node", node.Name)
	err = r.binder.Bind(ctx, pod, node, bindRequest)

	if errors.Is(err, binding.InvalidCrdWarning) {
		logger.Info("This binding request seems to be created by failed conversion from the previous version."+
			" Delete it and a new, valid one will be created.", "pod", pod.Name,
			"namespace", pod.Namespace)
		err = r.Client.Delete(ctx, bindRequest)
	}
	if err != nil {
		logger.Error(err, "Failed to bind pod to node", "pod", pod.Name, "namespace", pod.Namespace,
			"node", node.Name)

		if rollbackErr := r.binder.Rollback(ctx, pod, node, bindRequest); rollbackErr != nil {
			logger.Error(rollbackErr, "Failed to rollback after bind failure", "pod", pod.Name,
				"namespace", pod.Namespace)
		}
	}
	return result, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *BindRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&schedulingv1alpha2.BindRequest{}).
		Watches(&schedulingv1alpha2.BindRequest{}, r.eventHandlers()).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.params.MaxConcurrentReconciles,
			RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
				time.Duration(r.params.RateLimiterBaseDelaySeconds)*time.Second,
				time.Duration(r.params.RateLimiterMaxDelaySeconds)*time.Second,
			),
			SkipNameValidation: &[]bool{true}[0],
		}).
		Complete(r)
}

func (r *BindRequestReconciler) eventHandlers() handler.Funcs {
	return handler.Funcs{
		CreateFunc: func(ctx context.Context, event event.CreateEvent, wq workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			h := handler.EnqueueRequestForObject{}
			h.Create(ctx, event, wq)
		},
		UpdateFunc: func(ctx context.Context, event event.UpdateEvent, wq workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			h := handler.EnqueueRequestForObject{}
			h.Update(ctx, event, wq)
		},
		DeleteFunc: r.deleteHandler,
		GenericFunc: func(ctx context.Context, event event.GenericEvent, wq workqueue.TypedRateLimitingInterface[reconcile.Request]) {
		},
	}
}

func (r *BindRequestReconciler) deleteHandler(ctx context.Context, event event.TypedDeleteEvent[client.Object],
	_ workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	logger := log.FromContext(ctx)
	bindRequest, ok := event.Object.(*schedulingv1alpha2.BindRequest)
	if !ok {
		return
	}

	if common.IsSharedGPUAllocation(bindRequest) {
		for _, gpuGroup := range bindRequest.Spec.SelectedGPUGroups {
			err := r.resourceReservation.SyncForGpuGroup(ctx, gpuGroup)
			if err != nil {
				logger.Error(err, "Failed to sync reservation for GPU Group",
					"gpuGroup", gpuGroup)
			}
		}
	}
}

func (r *BindRequestReconciler) UpdateStatus(
	ctx context.Context, bindRequest *schedulingv1alpha2.BindRequest, result ctrl.Result, err error,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	originalBindRequest := &schedulingv1alpha2.BindRequest{}
	bindRequest.DeepCopyInto(originalBindRequest)

	if err != nil {
		if bindRequest.Spec.BackoffLimit != nil && *bindRequest.Spec.BackoffLimit > bindRequest.Status.FailedAttempts {
			result.RequeueAfter = (1 << bindRequest.Status.FailedAttempts) * time.Second
			bindRequest.Status.FailedAttempts++
		}
		bindRequest.Status.Phase = schedulingv1alpha2.BindRequestPhaseFailed
		bindRequest.Status.Reason = err.Error()
	} else {
		bindRequest.Status.Phase = schedulingv1alpha2.BindRequestPhaseSucceeded
	}

	if originalBindRequest.Status.Phase == bindRequest.Status.Phase {
		return result, nil
	}

	patchErr := r.Client.Status().Patch(ctx, bindRequest, client.MergeFrom(originalBindRequest))
	if patchErr != nil {
		logger.Error(patchErr, "Failed to patch status for BindRequest",
			"Namespace", bindRequest.Namespace, "Name", bindRequest.Name)
	}

	return result, err
}

func (r *BindRequestReconciler) updatePodCondition(
	ctx context.Context, bindRequest *schedulingv1alpha2.BindRequest, pod *v1.Pod, result ctrl.Result, err error,
) {
	logger := log.FromContext(ctx)

	var message string
	var condition *v1.PodCondition
	var eventType string
	var reason string

	if err == nil || result.RequeueAfter != 0 {
		message = fmt.Sprintf("Pod bound successfully to node %s", bindRequest.Spec.SelectedNode)
		condition = &v1.PodCondition{
			Type:    podBoundCondition,
			Status:  v1.ConditionTrue,
			Reason:  "Bound",
			Message: message,
		}
		eventType = v1.EventTypeNormal
		reason = "Bound"
	} else {
		message = fmt.Sprintf(
			"Failed to bind pod %s/%s to node %s: %s", pod.Namespace, pod.Name,
			bindRequest.Spec.SelectedNode, err.Error(),
		)
		condition = &v1.PodCondition{
			Type:    podBoundCondition,
			Status:  v1.ConditionFalse,
			Reason:  "BindingError",
			Message: message,
		}
		eventType = v1.EventTypeWarning
		reason = "BindingError"
	}

	r.eventRecorder.Eventf(pod, eventType, reason, message)

	if podutil.UpdatePodCondition(&pod.Status, condition) {
		statusPatchBaseObject := v1.PodStatus{}
		statusPatchBaseObject.Conditions = []v1.PodCondition{*condition}
		podStatusPatchBytes, err := json.Marshal(statusPatchBaseObject)
		if err != nil {
			logger.Error(err, "Failed to marshal pod status patch", "pod", pod.Name,
				"namespace", pod.Namespace)
			return
		}

		patchData := []byte(fmt.Sprintf(`{"status":%s}`, string(podStatusPatchBytes)))
		err = r.Client.Status().Patch(ctx, pod, client.RawPatch(types.StrategicMergePatchType, patchData))
		if err != nil {
			logger.Error(err, "Failed to patch pod status", "pod", pod.Name,
				"namespace", pod.Namespace)
		}
	}
}
