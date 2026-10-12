// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package npe is the NUMA Placement Exporter: it ties the kubelet podresources observations to
// pod annotations. On each tick it lists local pod allocations, computes their NUMA placement,
// and patches the result onto pods whose placement changed.
package npe

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/consts"
	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/cputopology"
	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/placement"
)

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;patch

// resourceLister lists per-pod resource allocations from the local kubelet. *podresources.Client
// satisfies it; the interface keeps the exporter testable without a real podresources socket.
type resourceLister interface {
	List(ctx context.Context) ([]*podresourcesv1.PodResources, error)
}

// Exporter reconciles observed NUMA placement onto pods on a single node.
type Exporter struct {
	nodeName     string
	pollInterval time.Duration

	resources resourceLister
	cpuToNUMA cputopology.CPUToNUMA
	clientset kubernetes.Interface

	// Retain observations until informer updates arrive so incomplete allocations cannot restore
	// prediction fallback. Pod UIDs prevent carrying observations across pod name reuse.
	writtenGroups map[types.UID]struct{}
	pods          corelisters.PodLister
	events        chan struct{}
}

// New constructs an Exporter. The deprecated drift interval is ignored.
func New(nodeName string, pollInterval, _ time.Duration, resources resourceLister,
	cpuToNUMA cputopology.CPUToNUMA, clientset kubernetes.Interface) *Exporter {
	return &Exporter{
		nodeName:      nodeName,
		pollInterval:  pollInterval,
		resources:     resources,
		cpuToNUMA:     cpuToNUMA,
		clientset:     clientset,
		writtenGroups: map[types.UID]struct{}{},
		events:        make(chan struct{}, 1),
	}
}

// Run reconciles podresources observations on pod updates and periodic ticks until cancellation.
// Reconciliation runs on a single goroutine, so observation history needs no locking.
func (a *Exporter) Run(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("Starting NUMA placement exporter", "node", a.nodeName,
		"pollInterval", a.pollInterval)
	factory := informers.NewSharedInformerFactoryWithOptions(a.clientset, 0, informers.WithTweakListOptions(func(options *metav1.ListOptions) {
		options.FieldSelector = fields.OneTermEqualSelector("spec.nodeName", a.nodeName).String()
	}))
	podInformer := factory.Core().V1().Pods()
	notify := func() {
		select {
		case a.events <- struct{}{}:
		default:
		}
	}
	if _, err := podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(any) { notify() }, UpdateFunc: func(any, any) { notify() }, DeleteFunc: func(any) { notify() },
	}); err != nil {
		return err
	}
	factory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), podInformer.Informer().HasSynced) {
		return ctx.Err()
	}
	a.pods = podInformer.Lister()

	if err := a.reconcile(ctx); err != nil {
		logger.Error(err, "Reconcile failed")
	}

	pollTicker := time.NewTicker(a.pollInterval)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pollTicker.C:
		case <-a.events:
		}
		if err := a.reconcile(ctx); err != nil {
			logger.Error(err, "Reconcile failed")
		}
	}
}

func (a *Exporter) reconcile(ctx context.Context) error {
	logger := log.FromContext(ctx)

	pods, err := a.resources.List(ctx)
	if err != nil {
		return err
	}

	observed := make(map[string]*podresourcesv1.PodResources, len(pods))
	for _, pod := range pods {
		observed[pod.GetNamespace()+"/"+pod.GetName()] = pod
	}
	apiPods, err := a.pods.List(labels.Everything())
	if err != nil {
		return err
	}
	seen := map[types.UID]struct{}{}
	for _, pod := range apiPods {
		key := pod.Namespace + "/" + pod.Name
		seen[pod.UID] = struct{}{}
		annotations := map[string]string{}
		value, ok := a.placementValue(ctx, observed[key])
		if ok && pod.Annotations[consts.NumaPlacementAnnotation] != value {
			annotations[consts.NumaPlacementAnnotation] = value
		}
		_, previouslyWritten := a.writtenGroups[pod.UID]
		groupValue := placement.MemoryGroupsValue(ctx, pod, observed[key], pod.Annotations[constants.NumaMemoryGroupsObserved] != "" || previouslyWritten)
		if groupValue != "" && pod.Annotations[constants.NumaMemoryGroupsObserved] != groupValue {
			annotations[constants.NumaMemoryGroupsObserved] = groupValue
		}
		if len(annotations) != 0 {
			if err := a.patchAnnotations(ctx, pod, annotations); err != nil {
				if !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
					logger.Error(err, "Failed to patch NUMA observations", "pod", key)
				}
				continue
			}
		}
		if groupValue != "" {
			a.writtenGroups[pod.UID] = struct{}{}
		}
	}
	for key := range a.writtenGroups {
		if _, exists := seen[key]; exists {
			continue
		}
		delete(a.writtenGroups, key)
	}
	return nil
}

// placementValue computes a pod's observed placement and marshals it to the annotation value.
// Returns ok=false when the pod holds no topology-aligned resources or marshaling fails.
func (a *Exporter) placementValue(ctx context.Context, pod *podresourcesv1.PodResources) (string, bool) {
	observed := placement.Compute(pod, a.cpuToNUMA)
	if observed == nil {
		return "", false
	}
	value, err := observed.Marshal()
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to marshal placement",
			"pod", pod.GetNamespace()+"/"+pod.GetName())
		return "", false
	}
	return value, true
}

func (a *Exporter) patchAnnotations(ctx context.Context, pod *corev1.Pod, annotations map[string]string) error {
	patch := map[string]any{
		"metadata": map[string]any{
			"annotations":     annotations,
			"uid":             pod.UID,
			"resourceVersion": pod.ResourceVersion,
		},
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}

	_, err = a.clientset.CoreV1().Pods(pod.Namespace).Patch(
		ctx, pod.Name, types.MergePatchType, raw, metav1.PatchOptions{})
	return err
}
