// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	// AnnotationEvictionStrategy controls whether KAI uses suspend-based
	// preemption ("suspend") or direct pod deletion ("delete") for a
	// workload. Set by the podgrouper based on workload type.
	AnnotationEvictionStrategy = "kai.scheduler/eviction-strategy"

	// EvictionStrategySuspend patches spec.suspend=true on the workload
	// owner instead of deleting individual pods.
	EvictionStrategySuspend = "suspend"

	// EvictionStrategyDelete is the default — delete pods directly.
	EvictionStrategyDelete = "delete"
)

// GetEvictionStrategy reads the eviction strategy from PodGroup annotations.
// Returns "delete" (default) if not set.
func GetEvictionStrategy(pg *podgroup_info.PodGroupInfo) string {
	if pg.PodGroup == nil {
		return EvictionStrategyDelete
	}
	strategy := pg.PodGroup.Annotations[AnnotationEvictionStrategy]
	if strategy == EvictionStrategySuspend {
		return EvictionStrategySuspend
	}
	return EvictionStrategyDelete
}

// SuspendWorkload patches spec.suspend=true on the PodGroup's top-level
// owner. Uses an unstructured JSON merge patch so it works with any CRD
// that has a spec.suspend field (RayJob, batch Job, JobSet, etc.).
func SuspendWorkload(dynamicClient dynamic.Interface, pg *podgroup_info.PodGroupInfo) error {
	owner, gvr, err := resolveOwner(pg)
	if err != nil {
		return err
	}

	patch, _ := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"suspend": true,
		},
	})

	log.InfraLogger.V(2).Infof("Suspending workload %s/%s (kind: %s) for PodGroup %s/%s",
		pg.Namespace, owner.Name, owner.Kind, pg.Namespace, pg.Name)

	_, err = dynamicClient.Resource(gvr).Namespace(pg.Namespace).Patch(
		context.Background(), owner.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// UnsuspendWorkload patches spec.suspend=false on the PodGroup's
// top-level owner.
func UnsuspendWorkload(dynamicClient dynamic.Interface, pg *podgroup_info.PodGroupInfo) error {
	owner, gvr, err := resolveOwner(pg)
	if err != nil {
		return err
	}

	patch, _ := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"suspend": false,
		},
	})

	log.InfraLogger.V(2).Infof("Unsuspending workload %s/%s (kind: %s) for PodGroup %s/%s",
		pg.Namespace, owner.Name, owner.Kind, pg.Namespace, pg.Name)

	_, err = dynamicClient.Resource(gvr).Namespace(pg.Namespace).Patch(
		context.Background(), owner.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// IsWorkloadSuspended checks if the PodGroup's owner has spec.suspend=true.
func IsWorkloadSuspended(dynamicClient dynamic.Interface, pg *podgroup_info.PodGroupInfo) (bool, error) {
	owner, gvr, err := resolveOwner(pg)
	if err != nil {
		return false, err
	}

	obj, err := dynamicClient.Resource(gvr).Namespace(pg.Namespace).Get(
		context.Background(), owner.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	suspend, found, err := unstructuredNestedBool(obj.Object, "spec", "suspend")
	if err != nil || !found {
		return false, nil
	}
	return suspend, nil
}

// resolveOwner finds the PodGroup's top-level owner and derives its GVR.
func resolveOwner(pg *podgroup_info.PodGroupInfo) (metav1.OwnerReference, schema.GroupVersionResource, error) {
	if pg.PodGroup == nil || len(pg.PodGroup.OwnerReferences) == 0 {
		return metav1.OwnerReference{}, schema.GroupVersionResource{},
			fmt.Errorf("PodGroup %s/%s has no owner references", pg.Namespace, pg.Name)
	}

	owner := pg.PodGroup.OwnerReferences[0]
	gvr, err := ownerRefToGVR(owner)
	if err != nil {
		return owner, schema.GroupVersionResource{}, err
	}
	return owner, gvr, nil
}

// ownerRefToGVR converts an OwnerReference's APIVersion and Kind to a
// GroupVersionResource. Uses lowercase plural of Kind as the resource name
// (standard K8s convention: RayJob → rayjobs, Job → jobs).
func ownerRefToGVR(ref metav1.OwnerReference) (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to parse apiVersion %q: %w", ref.APIVersion, err)
	}
	// Standard K8s convention: lowercase plural of Kind.
	resource := strings.ToLower(ref.Kind) + "s"
	return gv.WithResource(resource), nil
}

// unstructuredNestedBool extracts a bool from a nested map path.
func unstructuredNestedBool(obj map[string]interface{}, fields ...string) (bool, bool, error) {
	val, found, err := nestedField(obj, fields...)
	if err != nil || !found {
		return false, false, err
	}
	b, ok := val.(bool)
	if !ok {
		return false, false, fmt.Errorf("expected bool, got %T", val)
	}
	return b, true, nil
}

func nestedField(obj map[string]interface{}, fields ...string) (interface{}, bool, error) {
	var val interface{} = obj
	for _, field := range fields {
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil, false, nil
		}
		val, ok = m[field]
		if !ok {
			return nil, false, nil
		}
	}
	return val, true, nil
}
