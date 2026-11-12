// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	corev1 "k8s.io/api/core/v1"
)

// redactPersistentVolumes redacts PV names, bounded PVC links, Node affinities,
// storage/attribute classes, and free-text statuses.
func (r *Redactor) redactPersistentVolumes(raw *snapshot.RawKubernetesObjects) {
	for _, pv := range raw.PersistentVolumes {
		if pv == nil {
			continue
		}

		// 1. Redact top-level identifiers and ObjectMeta
		pv.Name = r.Obfuscate(pv.Name, "pv")
		r.redactObjectMeta(&pv.ObjectMeta, "pv")

		r.redactPersistentVolumeSpec(&pv.Spec)
		r.redactPersistentVolumeStatus(&pv.Status)

		// Safely increment redaction statistics
		r.mu.Lock()
		r.stats.PersistentVolumesRedacted++
		r.mu.Unlock()
	}
}

// redactPersistentVolumeSpec redacts storage class names, attribute class names,
// PVC claim bindings, and node affinity rules.
func (r *Redactor) redactPersistentVolumeSpec(spec *corev1.PersistentVolumeSpec) {
	if spec == nil {
		return
	}

	// StorageClassName often reveals internal infra details (e.g., "aws-ebs-gp3-prod").
	if spec.StorageClassName != "" {
		spec.StorageClassName = r.Obfuscate(spec.StorageClassName, "storageclass")
	}

	// VolumeAttributesClassName (Feature Gate) functions similarly to StorageClass
	// and can leak internal storage tiering or CSI parameters.
	if spec.VolumeAttributesClassName != nil && *spec.VolumeAttributesClassName != "" {
		obfuscated := r.Obfuscate(*spec.VolumeAttributesClassName, "volumeattributesclass")
		spec.VolumeAttributesClassName = &obfuscated
	}

	// ClaimRef binds the PV to a specific PVC. We must use the exact prefixes
	// ("pvc" and "namespace") so the simulator maintains the mathematical binding.
	if spec.ClaimRef != nil {
		if spec.ClaimRef.Name != "" {
			spec.ClaimRef.Name = r.Obfuscate(spec.ClaimRef.Name, "pvc")
		}
		if spec.ClaimRef.Namespace != "" {
			spec.ClaimRef.Namespace = r.Obfuscate(spec.ClaimRef.Namespace, "namespace")
		}
		// Clear internal UIDs and Versions to prevent direct lookup leaks
		spec.ClaimRef.UID = ""
		spec.ClaimRef.ResourceVersion = ""
	}

	// NodeAffinity restricts where the volume can be used (e.g., local volumes).
	// We must obfuscate the node names or labels to match the Node redactor.
	if spec.NodeAffinity != nil && spec.NodeAffinity.Required != nil {
		for i := range spec.NodeAffinity.Required.NodeSelectorTerms {
			for j := range spec.NodeAffinity.Required.NodeSelectorTerms[i].MatchExpressions {
				req := &spec.NodeAffinity.Required.NodeSelectorTerms[i].MatchExpressions[j]

				// If the affinity targets specific hostnames, use the "node" prefix.
				if req.Key == corev1.LabelHostname {
					for k := range req.Values {
						req.Values[k] = r.Obfuscate(req.Values[k], "node")
					}
				} else {
					// Otherwise, obfuscate as a standard label key/value
					req.Key = r.Obfuscate(req.Key, "labelkey")
					for k := range req.Values {
						req.Values[k] = r.Obfuscate(req.Values[k], "labelval")
					}
				}
			}
		}
	}
}

// redactPersistentVolumeStatus clears free-text error messages and reasons.
func (r *Redactor) redactPersistentVolumeStatus(status *corev1.PersistentVolumeStatus) {
	if status == nil {
		return
	}

	// Reason and Message are free-text fields that leak failure details,
	// internal network paths, or hostnames. We clear them entirely.
	status.Reason = ""
	status.Message = ""
}
