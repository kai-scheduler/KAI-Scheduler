// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// redactObjectMeta redacts every sensitive field inside a Kubernetes ObjectMeta.
// It is called for every resource type in the snapshot.
//
// Fields deliberately NOT redacted and the reason why:
//   - Generation: a monotonic system counter with no identifying information.
//   - DeletionGracePeriodSeconds: a numeric duration, not user-supplied text.
//   - Kind and APIVersion: these live on TypeMeta, not ObjectMeta, and are
//     standard Kubernetes constants needed for correct JSON decoding.
func (r *Redactor) redactObjectMeta(meta *metav1.ObjectMeta, resourcePrefix string) {
	if meta == nil {
		return
	}

	// Name is the primary identifier. We use the caller-supplied resourcePrefix
	// so that cross-resource references stay consistent. For example, every
	// call site that redacts a pod name passes "pod" here, and every call site
	// that references a pod name elsewhere also uses "pod", so they always
	// resolve to the same obfuscated string.
	// Note: for most resources, Name is also set directly on the struct before
	// calling this function (e.g. pod.Name = r.Obfuscate(pod.Name, "pod")).
	// We do not redact it again here to avoid double-hashing.
	// The Namespace field however IS synced here because it is set on both
	// the struct and inside ObjectMeta, and callers set the struct-level field
	// but not always the ObjectMeta-embedded one.
	if meta.Namespace != "" {
		meta.Namespace = r.Obfuscate(meta.Namespace, "namespace")
	}

	// GenerateName is a user-supplied prefix that reveals naming conventions.
	// It must be redacted even though it is less commonly set than Name.
	if meta.GenerateName != "" {
		meta.GenerateName = r.Obfuscate(meta.GenerateName, resourcePrefix)
	}

	// SelfLink is deprecated since Kubernetes 1.20 but may still be present
	// in snapshots captured from older clusters. It contains the full API
	// path including namespace and resource name, so it leaks both.
	if meta.SelfLink != "" {
		meta.SelfLink = r.Obfuscate(meta.SelfLink, "selflink")
	}

	// UID is a system-generated unique identifier. It must be redacted because
	// it appears in OwnerReferences across resources and can be used to
	// correlate objects back to the original cluster.
	if meta.UID != "" {
		meta.UID = types.UID(r.Obfuscate(string(meta.UID), "uid"))
	}

	// ResourceVersion is an opaque concurrency token. It is not needed for
	// scheduling replay but leaks the cluster's internal revision counter
	// which can reveal how active a cluster is.
	if meta.ResourceVersion != "" {
		meta.ResourceVersion = r.Obfuscate(meta.ResourceVersion, "resver")
	}

	// Timestamps are zeroed entirely rather than hashed. They carry no
	// scheduling-relevant information and their presence leaks cluster
	// timing, deployment cadence, and object lifecycle details.
	if !meta.CreationTimestamp.IsZero() {
		meta.CreationTimestamp = metav1.Time{}
	}
	if meta.DeletionTimestamp != nil {
		t := metav1.Time{}
		meta.DeletionTimestamp = &t
	}

	// Finalizer strings are set by controllers and often contain domain names
	// that reveal which operators are running in the cluster.
	for i := range meta.Finalizers {
		meta.Finalizers[i] = r.Obfuscate(meta.Finalizers[i], "finalizer")
	}

	// ManagedFields contain field manager names, API versions, and operation
	// timestamps. They are entirely irrelevant for scheduling replay and are
	// the most verbose source of sensitive operator identity information.
	// We drop the entire slice rather than redact field by field.
	meta.ManagedFields = nil

	// Label values are redacted. Keys are preserved because they are standard
	// Kubernetes selector keys (app, tier, environment, etc.) and redacting
	// them would break label selector matching during scheduling replay.
	if meta.Labels != nil {
		r.redactMapValues(meta.Labels, false)
	}

	// Annotation values are redacted for the same reason as labels.
	// Annotation keys are also preserved because they are domain-prefixed
	// standard keys (e.g. kubectl.kubernetes.io/last-applied-configuration)
	// and do not typically contain sensitive user data.
	if meta.Annotations != nil {
		r.redactMapValues(meta.Annotations, true)
	}

	// OwnerReference names and UIDs are redacted so that owner names cannot
	// be traced back to the original cluster. Kind and APIVersion are preserved
	// because they are standard Kubernetes type identifiers needed to understand
	// the ownership structure without revealing actual names.
	for i := range meta.OwnerReferences {
		prefix := "owner"
		// PodGroup owner references must use the "podgroup" prefix to stay
		// consistent with how PodGroup objects themselves are redacted in
		// scheduling.go. Without this, the OwnerReference points to a name
		// that does not exist in the redacted snapshot.
		if meta.OwnerReferences[i].Kind == "PodGroup" {
			prefix = "podgroup"
		}
		meta.OwnerReferences[i].Name = r.Obfuscate(meta.OwnerReferences[i].Name, prefix)
		meta.OwnerReferences[i].UID = types.UID(
			r.Obfuscate(string(meta.OwnerReferences[i].UID), "uid"),
		)
	}
}

// redactMapValues redacts every non-empty value in a string map in place.
// This is used for both Labels and Annotations.
//
// Keys are intentionally preserved because:
//  1. Standard Kubernetes label keys (app, tier, zone, etc.) contain no
//     sensitive user data.
//  2. Redacting keys would break label selector matching in affinity rules,
//     node selectors, and pod affinity terms, making the snapshot useless
//     for scheduling replay.
func (r *Redactor) redactMapValues(m map[string]string, isAnnotation bool) {
	for key, value := range m {
		if value == "" {
			continue
		}
		m[key] = r.Obfuscate(value, "labelval")
		r.mu.Lock()
		if isAnnotation {
			r.stats.AnnotationsRedacted++
		} else {
			r.stats.LabelsRedacted++
		}
		r.mu.Unlock()
	}
}

// redactLabelsAndAnnotations is an alias for redactMapValues. It is kept
// for internal call sites that use the longer descriptive name for clarity.
func (r *Redactor) redactLabelsAndAnnotations(m map[string]string, isAnnotation bool) {
	r.redactMapValues(m, isAnnotation)
}
