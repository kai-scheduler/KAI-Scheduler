// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	resourceslicetracker "k8s.io/dynamic-resource-allocation/resourceslice/tracker"
	ksf "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8splfeature "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeports"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"

	featuregates "github.com/kai-scheduler/KAI-scheduler/pkg/common/feature_gates"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/k8s_utils"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

type K8sPlugins struct {
	FrameworkHandle      ksf.Handle
	Features             k8splfeature.Features
	InformerFactory      informers.SharedInformerFactory
	ResourceSliceTracker *resourceslicetracker.Tracker
	SessionDRAManager    ksf.SharedDRAManager

	NodePorts       ksf.Plugin
	TaintToleration ksf.Plugin
	NodeAffinity    ksf.Plugin
	PodAffinity     ksf.Plugin
	VolumeBinding   ksf.Plugin
}

func InitializeInternalPlugins(
	client kubernetes.Interface,
	informerFactory informers.SharedInformerFactory,
	nodeInfoLister ksf.NodeInfoLister,
) *K8sPlugins {
	initiatedPlugins := &K8sPlugins{}
	k8sFrameworkHandle := k8s_utils.NewFrameworkHandle(
		client, informerFactory, nodeInfoLister,
	)
	initiatedPlugins.FrameworkHandle = k8sFrameworkHandle
	initiatedPlugins.InformerFactory = informerFactory

	if featuregates.DynamicResourcesEnabled() {
		tracker, err := k8s_utils.StartResourceSliceTracker(informerFactory, client)
		if err != nil {
			log.InfraLogger.Errorf("Failed to start resource slice tracker: %v", err)
		}
		initiatedPlugins.ResourceSliceTracker = tracker
	}

	initiatedPlugins.Features = k8s_utils.GetK8sFeatures()
	initiatedPlugins.createFrameworkPlugins()

	return initiatedPlugins
}

// WithNodeInfoLister returns a copy whose framework plugins read nodes from the given lister.
// The DRA tracker and informers are shared.
func (p *K8sPlugins) WithNodeInfoLister(nodeInfoLister ksf.NodeInfoLister) *K8sPlugins {
	copied := &K8sPlugins{
		FrameworkHandle: k8s_utils.NewFrameworkHandle(
			p.FrameworkHandle.ClientSet(), p.InformerFactory, nodeInfoLister),
		Features:             p.Features,
		InformerFactory:      p.InformerFactory,
		ResourceSliceTracker: p.ResourceSliceTracker,
	}
	copied.createFrameworkPlugins()
	return copied
}

func (p *K8sPlugins) createFrameworkPlugins() {
	if plugin, err := nodeports.New(context.Background(), nil, p.FrameworkHandle, p.Features); err != nil {
		log.InfraLogger.Errorf("Failed to create nodeports plugin: %v", err)
		p.NodePorts = nil
	} else {
		p.NodePorts = plugin
	}

	if plugin, err := tainttoleration.New(context.Background(), nil, p.FrameworkHandle, p.Features); err != nil {
		log.InfraLogger.Errorf("Failed to create tainttoleration plugin: %v", err)
		p.TaintToleration = nil
	} else {
		p.TaintToleration = plugin
	}

	if plugin, err := nodeaffinity.New(context.Background(), &config.NodeAffinityArgs{}, p.FrameworkHandle, p.Features); err != nil {
		log.InfraLogger.Errorf("Failed to create nodeaffinity plugin: %v", err)
		p.NodeAffinity = nil
	} else {
		p.NodeAffinity = plugin
	}

	if plugin, err := interpodaffinity.New(context.Background(), &config.InterPodAffinityArgs{}, p.FrameworkHandle, p.Features); err != nil {
		log.InfraLogger.Errorf("Failed to create interpodaffinity plugin: %v", err)
		p.PodAffinity = nil
	} else {
		p.PodAffinity = plugin
	}

	if plugin, err := volumebinding.New(context.Background(), &config.VolumeBindingArgs{}, p.FrameworkHandle, p.Features); err != nil {
		log.InfraLogger.Errorf("Failed to create volumebinding plugin: %v", err)
		p.VolumeBinding = nil
	} else {
		p.VolumeBinding = plugin
	}

}
