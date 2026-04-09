/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

// suspendWorkload patches spec.suspend=true on the PodGroup's top-level
// owner. Uses an unstructured JSON merge patch so it works with any CRD
// that has a spec.suspend field (RayJob, batch Job, JobSet, etc.).
func suspendWorkload(dynamicClient dynamic.Interface, pg *podgroup_info.PodGroupInfo) error {
	owner, gvr, err := resolveControllerOwnerGVR(pg)
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
		context.TODO(), owner.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// UnsuspendWorkload patches spec.suspend=false on the PodGroup's
// top-level owner.
func UnsuspendWorkload(dynamicClient dynamic.Interface, pg *podgroup_info.PodGroupInfo) error {
	owner, gvr, err := resolveControllerOwnerGVR(pg)
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
		context.TODO(), owner.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// IsWorkloadSuspended checks if the PodGroup's owner has spec.suspend=true.
func IsWorkloadSuspended(dynamicClient dynamic.Interface, pg *podgroup_info.PodGroupInfo) (bool, error) {
	owner, gvr, err := resolveControllerOwnerGVR(pg)
	if err != nil {
		return false, err
	}

	obj, err := dynamicClient.Resource(gvr).Namespace(pg.Namespace).Get(
		context.TODO(), owner.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	val, found, err := nestedField(obj.Object, "spec", "suspend")
	if err != nil {
		return false, fmt.Errorf("failed to read spec.suspend: %w", err)
	}
	if !found {
		return false, nil
	}
	b, ok := val.(bool)
	if !ok {
		return false, fmt.Errorf("spec.suspend is %T, expected bool", val)
	}
	return b, nil
}

// restMapper is set during session creation when a discovery client is
// available. Used by resolveControllerOwnerGVR to correctly map Kind to
// resource name (handles non-standard plurals like Policy → policies).
var restMapper meta.RESTMapper

// SetRESTMapper sets the package-level RESTMapper used for GVK → GVR
// resolution. Called during session creation.
func SetRESTMapper(mapper meta.RESTMapper) {
	restMapper = mapper
}

// resolveControllerOwnerGVR finds the PodGroup's controller owner and
// resolves its GVR. Uses RESTMapper when available for correct Kind →
// resource mapping, falling back to naive lowercase+s pluralization.
func resolveControllerOwnerGVR(pg *podgroup_info.PodGroupInfo) (metav1.OwnerReference, schema.GroupVersionResource, error) {
	if pg.PodGroup == nil || len(pg.PodGroup.OwnerReferences) == 0 {
		return metav1.OwnerReference{}, schema.GroupVersionResource{},
			fmt.Errorf("PodGroup %s/%s has no owner references", pg.Namespace, pg.Name)
	}

	// Prefer the controller owner.
	owner := pg.PodGroup.OwnerReferences[0]
	for _, ref := range pg.PodGroup.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			owner = ref
			break
		}
	}

	gvr, err := ownerRefToGVR(owner)
	if err != nil {
		return owner, schema.GroupVersionResource{}, err
	}
	return owner, gvr, nil
}

// ownerRefToGVR converts an OwnerReference's APIVersion and Kind to a
// GroupVersionResource. Uses the RESTMapper when available for correct
// pluralization; falls back to naive lowercase+s when the mapper is nil
// or lookup fails.
func ownerRefToGVR(ref metav1.OwnerReference) (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to parse apiVersion %q: %w", ref.APIVersion, err)
	}

	gvk := gv.WithKind(ref.Kind)

	// Use RESTMapper for correct Kind → resource mapping.
	if restMapper != nil {
		mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err == nil {
			return mapping.Resource, nil
		}
		log.InfraLogger.V(4).Infof("RESTMapper lookup failed for %v, falling back to naive pluralization: %v", gvk, err)
	}

	// Fallback: use UnsafeGuessKindToResource which handles common
	// irregular plurals (e.g. Ingress→ingresses, Policy→policies).
	guessed, _ := meta.UnsafeGuessKindToResource(gvk)
	return guessed, nil
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
