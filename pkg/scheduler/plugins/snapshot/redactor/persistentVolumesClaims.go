// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"

// redactPersistentVolumeClaims redacts PVC names, namespaces, and PV bindings.
// It also scrubs storage classes, label selectors, data sources, attribute classes,
// and free-text condition messages to prevent infrastructure leaks.
func (r *Redactor) redactPersistentVolumeClaims(raw *snapshot.RawKubernetesObjects) {
	for _, pvc := range raw.PersistentVolumeClaims {
		if pvc == nil {
			continue
		}

		// 1. Redact top-level identifiers and ObjectMeta
		pvc.Name = r.Obfuscate(pvc.Name, "pvc")
		if pvc.Namespace != "" {
			pvc.Namespace = r.Obfuscate(pvc.Namespace, "namespace")
		}
		r.redactObjectMeta(&pvc.ObjectMeta, "pvc")

		// 2. Redact PersistentVolumeClaimSpec
		if pvc.Spec.VolumeName != "" {
			pvc.Spec.VolumeName = r.Obfuscate(pvc.Spec.VolumeName, "pv")
		}

		// Storage class names reveal backend infra (e.g., "fast-nvme-tier")
		if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
			obfuscated := r.Obfuscate(*pvc.Spec.StorageClassName, "storageclass")
			pvc.Spec.StorageClassName = &obfuscated
		}

		// VolumeAttributesClassName functions similarly to StorageClass
		if pvc.Spec.VolumeAttributesClassName != nil && *pvc.Spec.VolumeAttributesClassName != "" {
			obfuscated := r.Obfuscate(*pvc.Spec.VolumeAttributesClassName, "volumeattributesclass")
			pvc.Spec.VolumeAttributesClassName = &obfuscated
		}

		// Redact Label Selectors used for binding
		if pvc.Spec.Selector != nil {
			if pvc.Spec.Selector.MatchLabels != nil {
				newLabels := make(map[string]string, len(pvc.Spec.Selector.MatchLabels))
				for k, v := range pvc.Spec.Selector.MatchLabels {
					newLabels[r.Obfuscate(k, "labelkey")] = r.Obfuscate(v, "labelval")
				}
				pvc.Spec.Selector.MatchLabels = newLabels
			}
			for i := range pvc.Spec.Selector.MatchExpressions {
				pvc.Spec.Selector.MatchExpressions[i].Key = r.Obfuscate(pvc.Spec.Selector.MatchExpressions[i].Key, "labelkey")
				for j := range pvc.Spec.Selector.MatchExpressions[i].Values {
					pvc.Spec.Selector.MatchExpressions[i].Values[j] = r.Obfuscate(pvc.Spec.Selector.MatchExpressions[i].Values[j], "labelval")
				}
			}
		}

		// DataSource usually points to another PVC or a VolumeSnapshot for cloning.
		if pvc.Spec.DataSource != nil {
			if pvc.Spec.DataSource.Kind == "PersistentVolumeClaim" {
				pvc.Spec.DataSource.Name = r.Obfuscate(pvc.Spec.DataSource.Name, "pvc")
			} else if pvc.Spec.DataSource.Kind == "VolumeSnapshot" {
				pvc.Spec.DataSource.Name = r.Obfuscate(pvc.Spec.DataSource.Name, "snapshot")
			} else {
				pvc.Spec.DataSource.Name = r.Obfuscate(pvc.Spec.DataSource.Name, "datasource")
			}
		}

		// DataSourceRef is the newer alternative to DataSource and includes cross-namespace abilities
		if pvc.Spec.DataSourceRef != nil {
			if pvc.Spec.DataSourceRef.Kind == "PersistentVolumeClaim" {
				pvc.Spec.DataSourceRef.Name = r.Obfuscate(pvc.Spec.DataSourceRef.Name, "pvc")
			} else if pvc.Spec.DataSourceRef.Kind == "VolumeSnapshot" {
				pvc.Spec.DataSourceRef.Name = r.Obfuscate(pvc.Spec.DataSourceRef.Name, "snapshot")
			} else {
				pvc.Spec.DataSourceRef.Name = r.Obfuscate(pvc.Spec.DataSourceRef.Name, "datasource")
			}

			if pvc.Spec.DataSourceRef.Namespace != nil && *pvc.Spec.DataSourceRef.Namespace != "" {
				obfuscatedNs := r.Obfuscate(*pvc.Spec.DataSourceRef.Namespace, "namespace")
				pvc.Spec.DataSourceRef.Namespace = &obfuscatedNs
			}
		}

		// 3. Redact PersistentVolumeClaimStatus
		if pvc.Status.CurrentVolumeAttributesClassName != nil && *pvc.Status.CurrentVolumeAttributesClassName != "" {
			obfuscated := r.Obfuscate(*pvc.Status.CurrentVolumeAttributesClassName, "volumeattributesclass")
			pvc.Status.CurrentVolumeAttributesClassName = &obfuscated
		}

		// Conditions contain free-text messages (e.g., "failed to provision volume on storage array X")
		for i := range pvc.Status.Conditions {
			pvc.Status.Conditions[i].Message = ""
			pvc.Status.Conditions[i].Reason = ""
		}

		// 4. Safely increment redaction statistics
		r.mu.Lock()
		r.stats.PersistentVolumeClaimsRedacted++
		r.mu.Unlock()
	}
}
