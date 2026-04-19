// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

const (
	controllerName = "pod-grouper"

	rateLimiterBaseDelay = time.Second
	rateLimiterMaxDelay  = time.Minute
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	podGrouper      podgrouper.Interface
	PodGroupHandler *podgroup.Handler
	configs         Configs
	eventRecorder   record.EventRecorder
}

type Configs struct {
	NodePoolLabelKey         string
	MaxConcurrentReconciles  int
	SearchForLegacyPodGroups bool
	KnativeGangSchedule      bool
	SchedulerName            string
	SchedulingQueueLabelKey  string

	DefaultPrioritiesConfigMapName      string
	DefaultPrioritiesConfigMapNamespace string
}

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update;get;list;watch
// +kubebuilder:rbac:groups="scheduling.k8s.io",resources=priorityclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups="scheduling.run.ai",resources=podgroups,verbs=create;update;patch;get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.V(1).Info("Reconciling pod", req.Namespace, req.Name)
	pod := v1.Pod{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &pod)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info(fmt.Sprintf("Pod %s/%s not found, was probably deleted", pod.Namespace, pod.Name))
			return ctrl.Result{}, nil
		}

		logger.V(1).Error(err, "Failed to get pod", req.Namespace, req.Name)
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: 1,
		}, err
	}

	if pod.Spec.SchedulerName != r.configs.SchedulerName {
		return ctrl.Result{}, nil
	}

	defer func() {
		if err != nil {
			r.eventRecorder.Event(&pod, v1.EventTypeWarning, constants.PodGrouperWarning, err.Error())
		}
	}()

	if isOrphanPodWithPodGroup(ctx, &pod) {
		return ctrl.Result{}, nil
	}

	topOwner, allOwners, err := r.podGrouper.GetPodOwners(ctx, &pod)
	if err != nil {
		logger.V(1).Error(err, "Failed to find pod top owner", req.Namespace, req.Name)
		return ctrl.Result{}, err
	}

	metadata, err := r.podGrouper.GetPGMetadata(ctx, &pod, topOwner, allOwners)
	if err != nil {
		logger.V(1).Error(err, "Failed to create pod group metadata for pod", req.Namespace, req.Name)
		return ctrl.Result{}, err
	}

	if len(r.configs.NodePoolLabelKey) > 0 {
		addNodePoolLabel(metadata, &pod, r.configs.NodePoolLabelKey)
	}

	err = r.PodGroupHandler.ApplyToCluster(ctx, *metadata)
	if err != nil {
		logger.V(1).Error(err, "Failed to apply metadata for pod group", metadata.Namespace, metadata.Name)
		return ctrl.Result{}, err
	}

	err = r.addPodGroupAnnotationToPod(ctx, &pod, metadata.Name, string(metadata.Owner.UID))
	if err != nil {
		logger.V(1).Error(err, "Failed to update pod with podgroup annotation", "pod", pod)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager, configs Configs) error {
	clientWithoutCache, err := client.New(mgr.GetConfig(), client.Options{Cache: nil})
	if err != nil {
		return err
	}

	r.podGrouper = podgrouper.NewPodgrouper(mgr.GetClient(), clientWithoutCache, configs.SearchForLegacyPodGroups,
		configs.KnativeGangSchedule, configs.SchedulingQueueLabelKey, configs.NodePoolLabelKey,
		configs.DefaultPrioritiesConfigMapName, configs.DefaultPrioritiesConfigMapNamespace)
	r.PodGroupHandler = podgroup.NewHandler(mgr.GetClient(), configs.NodePoolLabelKey, configs.SchedulingQueueLabelKey)
	r.configs = configs
	r.eventRecorder = mgr.GetEventRecorderFor(controllerName)

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Pod{}).
		WithOptions(
			controller.Options{
				MaxConcurrentReconciles: r.configs.MaxConcurrentReconciles,
				RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
					rateLimiterBaseDelay, rateLimiterMaxDelay),
			}).
		Complete(r)
}

func (r *PodReconciler) addPodGroupAnnotationToPod(ctx context.Context, pod *v1.Pod, podGroup, jobID string) error {
	logger := log.FromContext(ctx)
	if len(pod.Annotations) == 0 {
		pod.Annotations = map[string]string{}
	}

	value, found := pod.Annotations[constants.PodGroupAnnotationForPod]
	if found && value == podGroup {
		return nil
	}
	logger.V(1).Info("Reconciling podgroup annotation for pod", "pod",
		fmt.Sprintf("%s/%s", pod.Namespace, pod.Name), "old", value, "new", podGroup)

	newPod := pod.DeepCopy()
	newPod.Annotations[constants.PodGroupAnnotationForPod] = podGroup

	return r.Client.Patch(ctx, newPod, client.MergeFrom(pod))
}

func addNodePoolLabel(metadata *podgroup.Metadata, pod *v1.Pod, nodePoolKey string) {
	if metadata.Labels == nil {
		metadata.Labels = map[string]string{}
	}

	if _, found := metadata.Labels[nodePoolKey]; found {
		return
	}

	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}

	if labelValue, found := pod.Labels[nodePoolKey]; found {
		metadata.Labels[nodePoolKey] = labelValue
	}
}

func isOrphanPodWithPodGroup(ctx context.Context, pod *v1.Pod) bool {
	logger := log.FromContext(ctx)
	podGroupName, foundPGAnnotation := pod.Annotations[constants.PodGroupAnnotationForPod]
	if foundPGAnnotation && pod.OwnerReferences == nil {
		logger.V(1).Error(fmt.Errorf("orphan pod detected"), "Detected pod with no owner but with podgroup annotation",
			"pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name), "podgroup", podGroupName)
		return true
	}

	return false
}
