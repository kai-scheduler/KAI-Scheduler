// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	k8splfeature "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeports"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"

	"github.com/NVIDIA/KAI-scheduler/pkg/common/k8s_utils"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
)

type K8sPlugins struct {
	FrameworkHandle k8sframework.Handle
	Features        k8splfeature.Features

	NodePorts        k8sframework.Plugin
	TaintToleration  k8sframework.Plugin
	NodeAffinity     k8sframework.Plugin
	PodAffinity      k8sframework.Plugin
	VolumeBinding    k8sframework.Plugin
	DynamicResources k8sframework.Plugin
}

func InitializeInternalPlugins(
	client kubernetes.Interface,
	informerFactory informers.SharedInformerFactory,
	nodeInfoLister k8sframework.NodeInfoLister,
) *K8sPlugins {
	initiatedPlugins := &K8sPlugins{}
	k8sFrameworkHandle := k8s_utils.NewFrameworkHandle(
		client, informerFactory, nodeInfoLister,
	)
	initiatedPlugins.FrameworkHandle = k8sFrameworkHandle

	features := k8s_utils.GetK8sFeatures()
	initiatedPlugins.Features = features

	if plugin, err := nodeports.New(context.Background(), nil, k8sFrameworkHandle, features); err != nil {
		log.InfraLogger.Errorf("Failed to create nodeports plugin: %v", err)
		initiatedPlugins.NodePorts = nil
	} else {
		initiatedPlugins.NodePorts = plugin
	}

	if plugin, err := tainttoleration.New(context.Background(), nil, k8sFrameworkHandle, features); err != nil {
		log.InfraLogger.Errorf("Failed to create tainttoleration plugin: %v", err)
		initiatedPlugins.TaintToleration = nil
	} else {
		initiatedPlugins.TaintToleration = plugin
	}

	if plugin, err := nodeaffinity.New(context.Background(), &config.NodeAffinityArgs{}, k8sFrameworkHandle, features); err != nil {
		log.InfraLogger.Errorf("Failed to create nodeaffinity plugin: %v", err)
		initiatedPlugins.NodeAffinity = nil
	} else {
		initiatedPlugins.NodeAffinity = plugin
	}

	if plugin, err := interpodaffinity.New(context.Background(), &config.InterPodAffinityArgs{}, k8sFrameworkHandle, features); err != nil {
		log.InfraLogger.Errorf("Failed to create interpodaffinity plugin: %v", err)
		initiatedPlugins.PodAffinity = nil
	} else {
		initiatedPlugins.PodAffinity = plugin
	}

	if plugin, err := volumebinding.New(context.Background(), &config.VolumeBindingArgs{}, k8sFrameworkHandle, features); err != nil {
		log.InfraLogger.Errorf("Failed to create volumebinding plugin: %v", err)
		initiatedPlugins.VolumeBinding = nil
	} else {
		initiatedPlugins.VolumeBinding = plugin
	}

	draArgs := &config.DynamicResourcesArgs{}
	if plugin, err := dynamicresources.New(context.Background(), draArgs, initiatedPlugins.FrameworkHandle, initiatedPlugins.Features); err != nil {
		log.InfraLogger.Errorf("Failed to create dynamicresources plugin: %v", err)
		initiatedPlugins.DynamicResources = nil
	} else {
		initiatedPlugins.DynamicResources = plugin
	}

	return initiatedPlugins
}
