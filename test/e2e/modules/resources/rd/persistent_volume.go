/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package rd

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
)

func CreatePersistentVolumeObject(name string, path string) *v1.PersistentVolume {
	return &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				constants.AppLabelName: "engine-e2e",
			},
		},
		Spec: v1.PersistentVolumeSpec{
			StorageClassName: "manual",
			AccessModes: []v1.PersistentVolumeAccessMode{
				v1.ReadWriteOnce,
			},
			Capacity: v1.ResourceList{
				v1.ResourceStorage: resource.MustParse("10k"),
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: path,
				},
			},
		},
	}
}

func CreatePersistentVolumeClaimObject(name string, storageClassName string,
	quantity resource.Quantity) *v1.PersistentVolumeClaim {
	return &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				constants.AppLabelName: "engine-e2e",
			},
		},
		Spec: v1.PersistentVolumeClaimSpec{
			StorageClassName: ptr.To(storageClassName),
			AccessModes: []v1.PersistentVolumeAccessMode{
				v1.ReadWriteOnce,
			},
			Resources: v1.VolumeResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: quantity,
				},
			},
		},
	}
}

// CreateStorageClass creates StorageClass matching CSI driver for provisioning local PVs backed by LVM
// documentation: https://github.com/openebs/lvm-localpv
func CreateStorageClass(name string) *storagev1.StorageClass {
	volumeBindingMode := storagev1.VolumeBindingWaitForFirstConsumer
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				constants.AppLabelName: "engine-e2e",
			},
		},
		Provisioner: "local.csi.openebs.io",
		Parameters: map[string]string{
			"storage":  "lvm",
			"volgroup": "lvmvg",
		},
		VolumeBindingMode: &volumeBindingMode,
	}
}

func DeleteAllStorageObjects(ctx context.Context, k8sClient client.Client) error {
	err := DeleteAllPersistentVolumes(ctx, k8sClient)
	if err != nil {
		return err
	}

	return DeleteAllStorageClasses(ctx, k8sClient)
}

func DeleteAllPersistentVolumes(ctx context.Context, k8sClient client.Client) error {
	pv := &v1.PersistentVolume{}
	return k8sClient.DeleteAllOf(ctx, pv, client.MatchingLabels{constants.AppLabelName: "engine-e2e"})
}

func DeleteAllStorageClasses(ctx context.Context, k8sClient client.Client) error {
	storageClass := &storagev1.StorageClass{}
	return k8sClient.DeleteAllOf(ctx, storageClass, client.MatchingLabels{constants.AppLabelName: "engine-e2e"})
}

func GetCSICapacity(ctx context.Context, k8sClient client.Client, storageclass *storagev1.StorageClass) (storageCapacity *storagev1.CSIStorageCapacity, err error) {
	var capacities storagev1.CSIStorageCapacityList
	err = k8sClient.List(ctx, &capacities)
	if err != nil {
		return nil, err
	}

	if len(capacities.Items) == 0 {
		return nil, fmt.Errorf("no CSIStorageCapacities found")
	}

	for i, capacity := range capacities.Items {
		if capacity.StorageClassName == storageclass.Name {
			return &capacities.Items[i], nil

		}
	}

	return nil, fmt.Errorf("no CSIStorageCapacity found for storage class %s", storageclass.Name)
}
