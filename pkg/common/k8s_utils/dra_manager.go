// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"context"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	resourceslicetracker "k8s.io/dynamic-resource-allocation/resourceslice/tracker"
	"k8s.io/klog/v2"
	ksf "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	"k8s.io/kubernetes/pkg/scheduler/util/assumecache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// snapshotInformer implements assumecache.Informer by delivering a static set of objects
// to the registered handler on construction. No further events are delivered afterward,
// isolating the cache from live informer updates.
type snapshotInformer struct {
	objects []any
}

func (si *snapshotInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	for _, obj := range si.objects {
		handler.OnAdd(obj, false)
	}
	return nil, nil
}

// NewSessionDRAManager creates a session-scoped DRA manager isolated from live informer updates.
// It populates a nil-informer AssumeCache from the provided claims snapshot, ensuring no
// concurrent informer events can modify state during the scheduling session.
// The resourceSliceTracker and informerFactory are shared live singletons (read-only).
func NewSessionDRAManager(
	claims []*resourceapi.ResourceClaim,
	tracker *resourceslicetracker.Tracker,
	informerFactory informers.SharedInformerFactory,
) ksf.SharedDRAManager {
	objects := make([]any, len(claims))
	for i, claim := range claims {
		objects[i] = claim
	}

	logger := klog.LoggerWithName(klog.Background(), "SessionResourceClaimCache")
	sessionCache := assumecache.NewAssumeCache(logger, &snapshotInformer{objects: objects}, "ResourceClaim", "", nil)

	return dynamicresources.NewDRAManager(
		context.Background(), sessionCache, tracker, informerFactory,
	)
}

// StartResourceSliceTracker creates a ResourceSliceTracker from the informer factory.
// The tracker is a live singleton that watches informers for resource slices, device taint rules,
// and device classes. It should be created once and reused across sessions.
func StartResourceSliceTracker(
	informerFactory informers.SharedInformerFactory,
	kubeClient kubernetes.Interface,
) (*resourceslicetracker.Tracker, error) {
	tracker, err := resourceslicetracker.StartTracker(context.Background(), resourceslicetracker.Options{
		SliceInformer: informerFactory.Resource().V1().ResourceSlices(),
		TaintInformer: informerFactory.Resource().V1alpha3().DeviceTaintRules(),
		ClassInformer: informerFactory.Resource().V1().DeviceClasses(),
		KubeClient:    kubeClient,
	})
	if err != nil {
		log.Log.Error(err, "Failed to create resource slice tracker")
	}
	return tracker, err
}
