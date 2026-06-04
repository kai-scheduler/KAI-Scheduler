// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"fmt"
	"strconv"
	"sync"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/gpusharing"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/hamicore"
	k8splugins "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/k8s-plugins"
)

type PluginBuildContext struct {
	KubeClient      client.Client
	K8sInterface    kubernetes.Interface
	InformerFactory informers.SharedInformerFactory
}

type PluginBuilder func(PluginBuildContext, map[string]string) (Plugin, error)

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
	RegisterPluginBuilder(HamiCorePluginName, newHamiCorePlugin)
}

func BuildConfiguredPlugins(buildContext PluginBuildContext, config Config) (*BinderPlugins, error) {
	if err := validateDependentPlugins(config); err != nil {
		return nil, err
	}

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

func newVolumeBindingPlugin(buildContext PluginBuildContext, arguments map[string]string) (Plugin, error) {
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

func newDynamicResourcesPlugin(buildContext PluginBuildContext, arguments map[string]string) (Plugin, error) {
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

func newGPUSharingPlugin(buildContext PluginBuildContext, arguments map[string]string) (Plugin, error) {
	cdiEnabled, err := boolArgument(arguments, CDIEnabledArgument)
	if err != nil {
		return nil, err
	}
	return gpusharing.New(buildContext.KubeClient, cdiEnabled), nil
}

func newHamiCorePlugin(buildContext PluginBuildContext, _ map[string]string) (Plugin, error) {
	return hamicore.New(buildContext.KubeClient), nil
}

func validateDependentPlugins(config Config) error {
	hamiCoreCfg, hamiCoreFound := config[HamiCorePluginName]
	if !hamiCoreFound || (hamiCoreCfg.Enabled != nil && !*hamiCoreCfg.Enabled) {
		return nil
	}

	gpuSharingCfg, gpuSharingFound := config[GPUSharingPluginName]
	if !gpuSharingFound || (gpuSharingCfg.Enabled != nil && !*gpuSharingCfg.Enabled) {
		return fmt.Errorf("%q plugin requires %q plugin to be enabled", HamiCorePluginName, GPUSharingPluginName)
	}

	// PreBind is invoked in EnabledOptions() order (higher priority first).
	// hamicore requires gpusharing to have already created the configmap.
	hamiCorePri := ptr.Deref(config[HamiCorePluginName].Priority, 0)
	gpuSharingPri := ptr.Deref(config[GPUSharingPluginName].Priority, 0)
	if gpuSharingPri <= hamiCorePri {
		return fmt.Errorf("%q plugin requires %q to run before it (expected %q.priority > %q.priority, got %d <= %d)",
			HamiCorePluginName, GPUSharingPluginName, GPUSharingPluginName, HamiCorePluginName,
			gpuSharingPri, hamiCorePri)
	}

	return nil
}

func int64Argument(arguments map[string]string, name string) (int64, error) {
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

func boolArgument(arguments map[string]string, name string) (bool, error) {
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
