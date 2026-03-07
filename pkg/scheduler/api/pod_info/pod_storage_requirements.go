// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/storageclaim_info"
)

type PodStorageRequirements struct {
	PVCs         []PVCInfo   `json:"pvcs"`
	OtherVolumes []v1.Volume `json:"otherVolumes"`
}

type PVCInfo struct {
	StorageClassName string                        `json:"storageClassName"`
	Size             *resource.Quantity            `json:"size"`
	Phase            v1.PersistentVolumeClaimPhase `json:"phase"`
}

func getStorageRequirements(pod *v1.Pod, storageClaims map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo) PodStorageRequirements {
	requirements := PodStorageRequirements{}
	for _, volume := range pod.Spec.Volumes {
		if volume.VolumeSource.PersistentVolumeClaim != nil {
			pvc, err := getPVC(volume.VolumeSource, pod.Namespace, storageClaims)
			if err != nil {
				requirements.OtherVolumes = append(requirements.OtherVolumes, volume)
				continue
			}
			requirements.PVCs = append(requirements.PVCs, pvc)
			continue
		}

		if volume.VolumeSource.ConfigMap != nil {
			continue
		}

		if volume.VolumeSource.Secret != nil {
			continue
		}

		namelessVolume := volume.DeepCopy()
		namelessVolume.Name = ""
		requirements.OtherVolumes = append(requirements.OtherVolumes, *namelessVolume)
	}
	return requirements
}

func getPVC(volumeSource v1.VolumeSource, namespace string, storageClaims map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo) (PVCInfo, error) {
	pvc, found := storageClaims[storageclaim_info.NewKey(namespace, volumeSource.PersistentVolumeClaim.ClaimName)]
	if !found {
		return PVCInfo{}, fmt.Errorf("PVC %s not found", volumeSource.PersistentVolumeClaim.ClaimName)
	}
	return PVCInfo{
		StorageClassName: string(pvc.StorageClass),
		Size:             pvc.Size,
		Phase:            pvc.Phase,
	}, nil
}
