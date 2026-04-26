// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"fmt"
	"strconv"
	"sync"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/gpusharing"
	k8splugins "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/k8s-plugins"
)

type PluginBuildContext struct {
	KubeClient      client.Client
	K8sInterface    kubernetes.Interface
	InformerFactory informers.SharedInformerFactory
}

type PluginBuilder func(PluginBuildContext, PluginArguments) (Plugin, error)

var (
	pluginBuildersMutex sync.Mutex
	pluginBuilders      = map[string]PluginBuilder{}
)

func RegisterPluginBuilder(name string, builder PluginBuilder) {
	pluginBuildersMutex.Lock()
	defer pluginBuildersMutex.Unlock()

	pluginBuilders[name] = builder
}

func GetPluginBuilder(name string) (PluginBuilder, bool) {
	pluginBuildersMutex.Lock()
	defer pluginBuildersMutex.Unlock()

	builder, found := pluginBuilders[name]
	return builder, found
}

func InitDefaultPlugins() {
	RegisterPluginBuilder(VolumeBindingPluginName, newVolumeBindingPlugin)
	RegisterPluginBuilder(DynamicResourcesPluginName, newDynamicResourcesPlugin)
	RegisterPluginBuilder(GPUSharingPluginName, newGPUSharingPlugin)
}

func BuildConfiguredPlugins(buildContext PluginBuildContext, config Config) (*BinderPlugins, error) {
	binderPlugins := New()
	for _, option := range config.EnabledOptions() {
		builder, found := GetPluginBuilder(option.Name)
		if !found {
			return nil, fmt.Errorf("failed to get binder plugin %s", option.Name)
		}
		plugin, err := builder(buildContext, option.Arguments)
		if err != nil {
			return nil, fmt.Errorf("failed to build binder plugin %s: %w", option.Name, err)
		}
		binderPlugins.RegisterPlugin(plugin)
	}

	return binderPlugins, nil
}

func newVolumeBindingPlugin(buildContext PluginBuildContext, arguments PluginArguments) (Plugin, error) {
	timeoutSeconds, err := int64Argument(arguments, BindTimeoutSecondsArgument)
	if err != nil {
		return nil, err
	}
	plugin, err := k8splugins.NewVolumeBinding(
		buildContext.K8sInterface, buildContext.InformerFactory, timeoutSeconds)
	if err != nil {
		return nil, err
	}
	return k8splugins.NewWithPlugins(VolumeBindingPluginName, plugin), nil
}

func newDynamicResourcesPlugin(buildContext PluginBuildContext, arguments PluginArguments) (Plugin, error) {
	timeoutSeconds, err := int64Argument(arguments, BindTimeoutSecondsArgument)
	if err != nil {
		return nil, err
	}
	plugin, err := k8splugins.NewDynamicResources(
		buildContext.K8sInterface, buildContext.InformerFactory, timeoutSeconds)
	if err != nil {
		return nil, err
	}
	return k8splugins.NewWithPlugins(DynamicResourcesPluginName, plugin), nil
}

func newGPUSharingPlugin(buildContext PluginBuildContext, arguments PluginArguments) (Plugin, error) {
	cdiEnabled, err := boolArgument(arguments, CDIEnabledArgument)
	if err != nil {
		return nil, err
	}
	return gpusharing.New(buildContext.KubeClient, cdiEnabled), nil
}

func int64Argument(arguments PluginArguments, name string) (int64, error) {
	value, found := arguments[name]
	if !found {
		return 0, fmt.Errorf("missing argument %q", name)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid argument %q=%q: %w", name, value, err)
	}
	return parsed, nil
}

func boolArgument(arguments PluginArguments, name string) (bool, error) {
	value, found := arguments[name]
	if !found {
		return false, fmt.Errorf("missing argument %q", name)
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid argument %q=%q: %w", name, value, err)
	}
	return parsed, nil
}
