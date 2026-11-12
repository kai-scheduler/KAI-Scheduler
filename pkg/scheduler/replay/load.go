// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"

	nrtfake "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/generated/clientset/versioned/fake"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	clientfeatures "k8s.io/client-go/features"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	kaischedulerfake "github.com/kai-scheduler/api/client/clientset/versioned/fake"
	draversionawareclient "github.com/kai-scheduler/api/utilities/resources/dra_version_aware_client"
)

// DisableWatchListClientForFakeReplay turns off the watch-list client. The NRT
// fake client hangs under that feature. See https://github.com/kubernetes/kubernetes/issues/129408.
func DisableWatchListClientForFakeReplay() {
	featureGates, ok := clientfeatures.FeatureGates().(interface {
		Set(clientfeatures.Feature, bool) error
	})
	if !ok {
		return
	}
	if err := featureGates.Set(clientfeatures.WatchListClient, false); err != nil {
		utilruntime.HandleError(err)
	}
}

// LoadClientsWithSnapshot loads raw snapshot objects into fake clients for replay.
func LoadClientsWithSnapshot(rawObjects *snapshot.RawKubernetesObjects, discoverySnapshot *snapshot.DiscoverySnapshot) (kubernetes.Interface, *kaischedulerfake.Clientset, *nrtfake.Clientset) {
	kubeClient := fake.NewSimpleClientset()
	kaiClient := kaischedulerfake.NewSimpleClientset()
	nrtClient := nrtfake.NewSimpleClientset()
	if rawObjects == nil {
		return draversionawareclient.NewDRAAwareClient(kubeClient), kaiClient, nrtClient
	}

	if discoverySnapshot == nil {
		discoverySnapshot = synthesizeDiscoveryFromSnapshot(rawObjects)
	}
	applyDiscoverySnapshot(kubeClient, discoverySnapshot)

	for _, pod := range rawObjects.Pods {
		if _, err := kubeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create pod: %v", err)
		}
	}
	for _, node := range rawObjects.Nodes {
		if _, err := kubeClient.CoreV1().Nodes().Create(context.TODO(), node, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create node: %v", err)
		}
	}
	for _, bindRequest := range rawObjects.BindRequests {
		if _, err := kaiClient.SchedulingV1alpha2().BindRequests(bindRequest.Namespace).Create(context.TODO(), bindRequest, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create bind request: %v", err)
		}
	}
	for _, podGroup := range rawObjects.PodGroups {
		if _, err := kaiClient.SchedulingV2alpha2().PodGroups(podGroup.Namespace).Create(context.TODO(), podGroup, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create pod group: %v", err)
		}
	}
	for _, queue := range rawObjects.Queues {
		if _, err := kaiClient.SchedulingV2().Queues(queue.Namespace).Create(context.TODO(), queue, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create queue: %v", err)
		}
	}
	for _, priorityClass := range rawObjects.PriorityClasses {
		if _, err := kubeClient.SchedulingV1().PriorityClasses().Create(context.TODO(), priorityClass, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create priority class: %v", err)
		}
	}
	for _, configMap := range rawObjects.ConfigMaps {
		if _, err := kubeClient.CoreV1().ConfigMaps(configMap.Namespace).Create(context.TODO(), configMap, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create config map: %v", err)
		}
	}
	for _, persistentVolume := range rawObjects.PersistentVolumes {
		if _, err := kubeClient.CoreV1().PersistentVolumes().Create(context.TODO(), persistentVolume, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create persistent volume: %v", err)
		}
	}
	for _, persistentVolumeClaim := range rawObjects.PersistentVolumeClaims {
		if _, err := kubeClient.CoreV1().PersistentVolumeClaims(persistentVolumeClaim.Namespace).Create(context.TODO(), persistentVolumeClaim, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create persistent volume claim: %v", err)
		}
	}
	for _, csiStorageCapacity := range rawObjects.CSIStorageCapacities {
		if _, err := kubeClient.StorageV1().CSIStorageCapacities(csiStorageCapacity.Namespace).Create(context.TODO(), csiStorageCapacity, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create CSI storage capacity: %v", err)
		}
	}
	for _, storageClass := range rawObjects.StorageClasses {
		if _, err := kubeClient.StorageV1().StorageClasses().Create(context.TODO(), storageClass, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create storage class: %v", err)
		}
	}
	for _, csiDriver := range rawObjects.CSIDrivers {
		if _, err := kubeClient.StorageV1().CSIDrivers().Create(context.TODO(), csiDriver, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create CSI driver: %v", err)
		}
	}
	for _, topology := range rawObjects.Topologies {
		if _, err := kaiClient.KaiV1alpha1().Topologies().Create(context.TODO(), topology, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create topology: %v", err)
		}
	}
	for _, nrt := range rawObjects.NodeResourceTopologies {
		if _, err := nrtClient.TopologyV1alpha2().NodeResourceTopologies().Create(context.TODO(), nrt, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create node resource topology: %v", err)
		}
	}

	draClient := draversionawareclient.NewDRAAwareClient(kubeClient)
	for _, resourceClaim := range rawObjects.ResourceClaims {
		if _, err := draClient.ResourceV1().ResourceClaims(resourceClaim.Namespace).Create(context.TODO(), resourceClaim, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create resource claim: %v", err)
		}
	}
	for _, resourceSlice := range rawObjects.ResourceSlices {
		if _, err := draClient.ResourceV1().ResourceSlices().Create(context.TODO(), resourceSlice, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create resource slice: %v", err)
		}
	}
	for _, deviceClass := range rawObjects.DeviceClasses {
		if _, err := draClient.ResourceV1().DeviceClasses().Create(context.TODO(), deviceClass, v1.CreateOptions{}); err != nil {
			log.InfraLogger.Errorf("Failed to create device class: %v", err)
		}
	}
	return draClient, kaiClient, nrtClient
}

func synthesizeDiscoveryFromSnapshot(rawObjects *snapshot.RawKubernetesObjects) *snapshot.DiscoverySnapshot {
	hasDRAResources := len(rawObjects.ResourceClaims) > 0 ||
		len(rawObjects.ResourceSlices) > 0 ||
		len(rawObjects.DeviceClasses) > 0
	hasNRTResources := len(rawObjects.NodeResourceTopologies) > 0
	if !hasDRAResources && !hasNRTResources {
		return nil
	}
	discoverySnapshot := &snapshot.DiscoverySnapshot{ServerVersion: &version.Info{Major: "1", Minor: "32"}}
	if hasDRAResources {
		log.InfraLogger.V(2).Infof("Synthesizing discovery data from snapshot DRA resources")
		discoverySnapshot.Resources = append(discoverySnapshot.Resources, &v1.APIResourceList{
			GroupVersion: "resource.k8s.io/v1",
			APIResources: []v1.APIResource{
				{Name: "resourceclaims", Kind: "ResourceClaim", Namespaced: true},
				{Name: "resourceslices", Kind: "ResourceSlice"},
				{Name: "deviceclasses", Kind: "DeviceClass"},
			},
		})
	}
	if hasNRTResources {
		log.InfraLogger.V(2).Infof("Synthesizing discovery data from snapshot NRT resources")
		discoverySnapshot.Resources = append(discoverySnapshot.Resources, &v1.APIResourceList{
			GroupVersion: "topology.node.k8s.io/v1alpha2",
			APIResources: []v1.APIResource{
				{Name: "noderesourcetopologies", Kind: "NodeResourceTopology"},
			},
		})
	}
	return discoverySnapshot
}

func applyDiscoverySnapshot(kubeClient *fake.Clientset, discoverySnapshot *snapshot.DiscoverySnapshot) {
	if kubeClient == nil || discoverySnapshot == nil {
		return
	}
	fakeDiscoveryClient, ok := kubeClient.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		return
	}
	if discoverySnapshot.ServerVersion != nil {
		fakeDiscoveryClient.FakedServerVersion = &version.Info{
			Major: discoverySnapshot.ServerVersion.Major,
			Minor: discoverySnapshot.ServerVersion.Minor,
		}
	}
	if discoverySnapshot.Resources != nil {
		kubeClient.Resources = discoverySnapshot.Resources
	}
}
