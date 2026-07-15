// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	"fmt"

	"github.com/pkg/errors"
	storage "k8s.io/api/storage/v1"
	"k8s.io/component-helpers/storage/ephemeral"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/csidriver_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storagecapacity_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storageclaim_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/storageclass_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

func (c *ClusterInfo) snapshotCSIStorageDrivers() (map[common_info.CSIDriverID]*csidriver_info.CSIDriverInfo, error) {
	drivers, err := c.dataLister.ListCSIDrivers()
	if err != nil {
		err = errors.WithStack(fmt.Errorf("error listing csidrivers: %w", err))
		return nil, err
	}

	result := map[common_info.CSIDriverID]*csidriver_info.CSIDriverInfo{}
	for _, driver := range drivers {
		capacityEnabled := false
		if driver.Spec.StorageCapacity != nil {
			capacityEnabled = *driver.Spec.StorageCapacity
		}

		result[common_info.CSIDriverID(driver.Name)] = &csidriver_info.CSIDriverInfo{
			ID:              common_info.CSIDriverID(driver.Name),
			CapacityEnabled: capacityEnabled,
		}
	}

	return result, nil
}

func (c *ClusterInfo) snapshotStorageClasses() (map[common_info.StorageClassID]*storageclass_info.StorageClassInfo, error) {
	storageClasses, err := c.dataLister.ListStorageClasses()
	if err != nil {
		err = errors.WithStack(fmt.Errorf("error listing storageclasses: %w", err))
		return nil, err
	}

	result := map[common_info.StorageClassID]*storageclass_info.StorageClassInfo{}
	for _, sc := range storageClasses {
		if sc.VolumeBindingMode == nil {
			log.InfraLogger.V(2).Warnf("VolumeBindingMode not set for storageclass %s, ignoring", sc.Name)
			continue
		}

		if *sc.VolumeBindingMode != storage.VolumeBindingWaitForFirstConsumer {
			log.InfraLogger.V(7).Info("VolumeBindingMode is %s for storage class %s, ignoring",
				*sc.VolumeBindingMode, sc.Name)
			continue
		}

		result[common_info.StorageClassID(sc.Name)] = &storageclass_info.StorageClassInfo{
			ID:          common_info.StorageClassID(sc.Name),
			Provisioner: sc.Provisioner,
		}
	}

	return result, nil
}

func (c *ClusterInfo) snapshotStorageClaims() (map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo, error) {
	storageClaims, err := c.dataLister.ListPersistentVolumeClaims()
	if err != nil {
		err = errors.WithStack(fmt.Errorf("error listing pvcs: %w", err))
		return nil, err
	}

	result := map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{}
	for _, sc := range storageClaims {
		podOwner, err := storageclaim_info.GetPodOwner(sc)
		if err != nil {
			log.InfraLogger.V(6).Infof(
				"Error finding pod owner for PVC, treating as un-owned: PVC %s/%s, error: %s",
				sc.Namespace, sc.Name, err)
		}
		scInfo := storageclaim_info.NewStorageClaimInfo(sc, podOwner)
		result[scInfo.Key] = scInfo
	}

	return result, nil
}

func (c *ClusterInfo) snapshotStorageCapacities() (map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo, error) {
	capacities, err := c.dataLister.ListCSIStorageCapacities()
	if err != nil {
		err = errors.WithStack(fmt.Errorf("error listing storage capacities: %w", err))
		return nil, err
	}

	result := map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo{}
	for _, capacity := range capacities {
		capacityInfo, err := storagecapacity_info.NewStorageCapacityInfo(capacity)
		if err != nil {
			// Skip invalid capacity and continue processing others
			log.InfraLogger.V(2).Warnf(
				"Failed to process CSIStorageCapacity %s/%s, skipping: %v",
				capacity.Namespace, capacity.Name, err)
			continue
		}
		result[capacityInfo.UID] = capacityInfo
	}
	return result, nil
}

// linkStorageObjects sets the relationships between the storage objects: adds the pvcs to the pods that use them, and to
// the storageCapacities they are provisioned on, if applicable.
func linkStorageObjects(
	storageClaims map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo,
	storageCapacities map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo,
	existingPods map[common_info.PodID]*pod_info.PodInfo,
	nodes map[string]*node_info.NodeInfo) {

	setNodesAccessibleCapacities(storageCapacities, nodes)
	setPodsPvcs(storageClaims, existingPods)

	linkStorageClaimsToStorageCapacities(existingPods, nodes)
}

func setNodesAccessibleCapacities(storageCapacities map[common_info.StorageCapacityID]*storagecapacity_info.StorageCapacityInfo,
	nodes map[string]*node_info.NodeInfo) {
	for _, capacity := range storageCapacities {
		for _, node := range nodes {
			if capacity.IsNodeValid(node.Node.Labels) {
				node.AccessibleStorageCapacities[capacity.StorageClass] = append(node.AccessibleStorageCapacities[capacity.StorageClass], capacity)
			}
		}
	}

	handleMultiCapacityNodes(nodes)
}

// we currently don't know how to handle cases where there are multiple accessible storage capacities per node per storageclass.
func handleMultiCapacityNodes(nodes map[string]*node_info.NodeInfo) {
	for _, node := range nodes {
		for sc, capacities := range node.AccessibleStorageCapacities {
			if len(capacities) > 1 {
				log.InfraLogger.V(2).Warnf("Node %s has access to %d capacities of storageclass %s - ignoring"+
					"for advanced storage scheduling", node.Name, len(capacities), sc)
				node.AccessibleStorageCapacities = map[common_info.StorageClassID][]*storagecapacity_info.StorageCapacityInfo{}
			}
		}
	}
}

func setPodsPvcs(
	storageClaims map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo,
	existingPods map[common_info.PodID]*pod_info.PodInfo,
) {
	for _, pod := range existingPods {
		for _, volume := range pod.Pod.Spec.Volumes {
			// A pod can only reference a PVC through one of two types of volumes - a PVC or an Ephemeral volume. For more
			// details see the implementation of GetPodVolumeClaims in pkg/scheduler/framework/plugins/volumebinding/binder.go
			// in versions >= v1.27.0
			pvcName := ""
			switch {
			case volume.PersistentVolumeClaim != nil:
				pvcName = volume.PersistentVolumeClaim.ClaimName
			case volume.Ephemeral != nil:
				// Generic ephemeral inline volumes also use a PVC, just with a computed name
				pvcName = ephemeral.VolumeClaimName(pod.Pod, &volume)
			default:
				log.InfraLogger.V(6).Infof("Volume %s for pod %s/%s is not a PVC, ignoring for advanced scheduling",
					volume.Name, pod.Namespace, pod.Name)
				continue
			}
			key := storageclaim_info.NewKey(pod.Namespace, pvcName)
			storageClaim, found := storageClaims[key]
			if !found {
				log.InfraLogger.V(2).Warnf("Pod %s/%s is referencing a pvc that was not found: %v",
					pod.Namespace, pod.Name, key)
				continue
			}
			pod.UpsertStorageClaim(storageClaim)
		}
	}
}

func linkStorageClaimsToStorageCapacities(existingPods map[common_info.PodID]*pod_info.PodInfo, nodes map[string]*node_info.NodeInfo) {
	for _, pod := range existingPods {
		if !pod_status.IsPodBound(pod.Status) {
			continue
		}

		nodeInfo, found := nodes[pod.NodeName]
		if !found {
			continue
		}

		for _, claim := range pod.GetAllStorageClaims() {
			capacities, found := nodeInfo.AccessibleStorageCapacities[claim.StorageClass]
			if !found {
				continue
			}
			for _, capacity := range capacities {
				capacity.ProvisionedPVCs[claim.Key] = claim
			}
		}
	}
}

func filterStorageClasses(storageClasses map[common_info.StorageClassID]*storageclass_info.StorageClassInfo, csiDrivers map[common_info.CSIDriverID]*csidriver_info.CSIDriverInfo) map[common_info.StorageClassID]*storageclass_info.StorageClassInfo {
	csiStorageClasses := map[common_info.StorageClassID]*storageclass_info.StorageClassInfo{}
	for id, sc := range storageClasses {
		if _, found := csiDrivers[common_info.CSIDriverID(sc.Provisioner)]; found {
			log.InfraLogger.V(6).Infof("Storage Class %s uses provisioner %s which is a CSI driver", id, sc.Provisioner)

			csiStorageClasses[id] = sc
		} else {
			log.InfraLogger.V(6).Infof("Storage Class %s uses provisioner %s which is not a CSI driver - ignoring for advanced CSI scheduling", id, sc.Provisioner)
		}
	}
	return csiStorageClasses
}

func filterStorageClaims(storageClaims map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo, storageClasses map[common_info.StorageClassID]*storageclass_info.StorageClassInfo) map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo {
	csiStorageClaims := map[storageclaim_info.Key]*storageclaim_info.StorageClaimInfo{}
	for key, claim := range storageClaims {
		if _, found := storageClasses[claim.StorageClass]; found {
			csiStorageClaims[key] = claim
		} else {
			log.InfraLogger.V(6).Infof("Storage Claim %s/%s uses storage class %s which is not managed by a CSI driver - ignoring for advanced CSI scheduling", claim.Namespace, claim.Name, claim.StorageClass)
		}
	}

	return csiStorageClaims
}
