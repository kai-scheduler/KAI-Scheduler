// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package redactor

import (
	corev1 "k8s.io/api/core/v1"
)

// redactVolumes redacts volume names and all provider-specific source details.
func (r *Redactor) redactVolumes(volumes []corev1.Volume) {
	for i := range volumes {
		volumes[i].Name = r.Obfuscate(volumes[i].Name, "volume")
		r.redactVolumeSource(&volumes[i].VolumeSource)

		if volumes[i].Secret != nil {
			volumes[i].Secret.SecretName = r.Obfuscate(volumes[i].Secret.SecretName, "secret")
			r.mu.Lock()
			r.stats.SecretsRedacted++
			r.mu.Unlock()
		}

		if volumes[i].ConfigMap != nil {
			volumes[i].ConfigMap.Name = r.Obfuscate(volumes[i].ConfigMap.Name, "configmap")
			r.mu.Lock()
			r.stats.ConfigMapsRedacted++
			r.mu.Unlock()
		}

		if volumes[i].PersistentVolumeClaim != nil {
			volumes[i].PersistentVolumeClaim.ClaimName = r.Obfuscate(
				volumes[i].PersistentVolumeClaim.ClaimName, "pvc",
			)
			r.mu.Lock()
			r.stats.VolumesRedacted++
			r.mu.Unlock()
		}
	}
}

// redactVolumeSource redacts provider-specific fields such as server
// addresses, disk names, and cloud resource identifiers.
func (r *Redactor) redactVolumeSource(vs *corev1.VolumeSource) {
	if vs == nil {
		return
	}

	if vs.HostPath != nil {
		vs.HostPath.Path = r.Obfuscate(vs.HostPath.Path, "hostpath")
	}

	if vs.NFS != nil {
		vs.NFS.Server = r.Obfuscate(vs.NFS.Server, "nfsserver")
		vs.NFS.Path = r.Obfuscate(vs.NFS.Path, "nfspath")
	}

	if vs.ISCSI != nil {
		vs.ISCSI.TargetPortal = r.Obfuscate(vs.ISCSI.TargetPortal, "iscsiportal")
		if vs.ISCSI.InitiatorName != nil {
			obfuscated := r.Obfuscate(*vs.ISCSI.InitiatorName, "iscsi-init")
			vs.ISCSI.InitiatorName = &obfuscated
		}
	}

	if vs.Glusterfs != nil {
		vs.Glusterfs.EndpointsName = r.Obfuscate(vs.Glusterfs.EndpointsName, "glusterfs-endpoint")
		vs.Glusterfs.Path = r.Obfuscate(vs.Glusterfs.Path, "glusterfs-path")
	}

	if vs.Flocker != nil && vs.Flocker.DatasetName != "" {
		vs.Flocker.DatasetName = r.Obfuscate(vs.Flocker.DatasetName, "flocker-dataset")
	}

	if vs.AzureFile != nil {
		vs.AzureFile.ShareName = r.Obfuscate(vs.AzureFile.ShareName, "azure-share")
	}

	if vs.AzureDisk != nil {
		vs.AzureDisk.DiskName = r.Obfuscate(vs.AzureDisk.DiskName, "azure-disk")
	}

	if vs.AWSElasticBlockStore != nil {
		vs.AWSElasticBlockStore.VolumeID = r.Obfuscate(
			vs.AWSElasticBlockStore.VolumeID, "ebs-volume",
		)
	}

	if vs.GCEPersistentDisk != nil {
		vs.GCEPersistentDisk.PDName = r.Obfuscate(vs.GCEPersistentDisk.PDName, "gce-pd")
	}

	if vs.Cinder != nil {
		vs.Cinder.VolumeID = r.Obfuscate(vs.Cinder.VolumeID, "cinder-volume")
	}

	if vs.VsphereVolume != nil {
		vs.VsphereVolume.VolumePath = r.Obfuscate(vs.VsphereVolume.VolumePath, "vsphere-path")
	}

	if vs.Projected != nil {
		for i := range vs.Projected.Sources {
			if vs.Projected.Sources[i].ConfigMap != nil {
				vs.Projected.Sources[i].ConfigMap.Name = r.Obfuscate(
					vs.Projected.Sources[i].ConfigMap.Name, "configmap",
				)
			}
			if vs.Projected.Sources[i].Secret != nil {
				vs.Projected.Sources[i].Secret.Name = r.Obfuscate(
					vs.Projected.Sources[i].Secret.Name, "secret",
				)
			}
		}
	}
}
