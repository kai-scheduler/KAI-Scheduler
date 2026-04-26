// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"
	"errors"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/state"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig(90, true)

	if got := config[VolumeBindingPluginName].Arguments[BindTimeoutSecondsArgument]; got != "90" {
		t.Fatalf("expected volume binding timeout 90, got %q", got)
	}
	if got := config[DynamicResourcesPluginName].Arguments[BindTimeoutSecondsArgument]; got != "90" {
		t.Fatalf("expected dynamic resources timeout 90, got %q", got)
	}
	if got := config[GPUSharingPluginName].Arguments[CDIEnabledArgument]; got != "true" {
		t.Fatalf("expected gpusharing CDI true, got %q", got)
	}

	options := config.EnabledOptions()
	if got := []string{options[0].Name, options[1].Name, options[2].Name}; !equalStrings(got,
		[]string{VolumeBindingPluginName, DynamicResourcesPluginName, GPUSharingPluginName}) {
		t.Fatalf("expected default order to match current binder order, got %v", got)
	}
}

func TestResolveConfig(t *testing.T) {
	defaults := DefaultConfig(120, false)
	overrides := Config{
		GPUSharingPluginName: {
			Enabled: ptr.To(false),
		},
		VolumeBindingPluginName: {
			Priority: ptr.To(50),
		},
		DynamicResourcesPluginName: {
			Arguments: map[string]string{
				BindTimeoutSecondsArgument: "30",
			},
		},
		"custom": {
			Priority: ptr.To(250),
		},
	}

	resolved := ResolveConfig(defaults, overrides)

	if *resolved[GPUSharingPluginName].Enabled {
		t.Fatalf("expected gpusharing to be disabled")
	}
	if *resolved[VolumeBindingPluginName].Priority != 50 {
		t.Fatalf("expected volume binding priority override, got %d", *resolved[VolumeBindingPluginName].Priority)
	}
	if got := resolved[DynamicResourcesPluginName].Arguments[BindTimeoutSecondsArgument]; got != "30" {
		t.Fatalf("expected dynamic resources timeout override, got %q", got)
	}
	if !*resolved["custom"].Enabled {
		t.Fatalf("expected custom plugin to default to enabled")
	}

	options := resolved.EnabledOptions()
	if got := []string{options[0].Name, options[1].Name, options[2].Name}; !equalStrings(got,
		[]string{"custom", DynamicResourcesPluginName, VolumeBindingPluginName}) {
		t.Fatalf("expected resolved order with disabled plugin removed, got %v", got)
	}
}

func TestBuildConfiguredPlugins(t *testing.T) {
	const fakePluginName = "fake-test-plugin"
	RegisterPluginBuilder(fakePluginName, func(PluginBuildContext, PluginArguments) (Plugin, error) {
		return &fakePlugin{name: fakePluginName}, nil
	})

	binderPlugins, err := BuildConfiguredPlugins(PluginBuildContext{}, Config{
		fakePluginName: {
			Enabled:  ptr.To(true),
			Priority: ptr.To(1),
		},
	})
	if err != nil {
		t.Fatalf("expected build to succeed: %v", err)
	}
	if binderPlugins == nil || len(binderPlugins.plugins) != 1 {
		t.Fatalf("expected one configured plugin, got %#v", binderPlugins)
	}
}

func TestBuildConfiguredPluginsReturnsUnknownPluginError(t *testing.T) {
	_, err := BuildConfiguredPlugins(PluginBuildContext{}, Config{
		"missing": {
			Enabled: ptr.To(true),
		},
	})
	if err == nil {
		t.Fatalf("expected error for unknown plugin")
	}
}

func TestArgumentParsers(t *testing.T) {
	if _, err := int64Argument(PluginArguments{}, BindTimeoutSecondsArgument); err == nil {
		t.Fatalf("expected missing int argument error")
	}
	if _, err := int64Argument(PluginArguments{BindTimeoutSecondsArgument: "bad"}, BindTimeoutSecondsArgument); err == nil {
		t.Fatalf("expected invalid int argument error")
	}
	if _, err := boolArgument(PluginArguments{CDIEnabledArgument: "bad"}, CDIEnabledArgument); err == nil {
		t.Fatalf("expected invalid bool argument error")
	}
}

type fakePlugin struct {
	name string
}

func (p *fakePlugin) Name() string {
	return p.name
}

func (p *fakePlugin) PreBind(
	context.Context, *v1.Pod, *v1.Node, *v1alpha2.BindRequest, *state.BindingState,
) error {
	return nil
}

func (p *fakePlugin) PostBind(
	context.Context, *v1.Pod, *v1.Node, *v1alpha2.BindRequest, *state.BindingState,
) {
}

func (p *fakePlugin) Rollback(
	context.Context, *v1.Pod, *v1.Node, *v1alpha2.BindRequest, *state.BindingState,
) error {
	return errors.New("not implemented")
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
