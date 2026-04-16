// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package conf_util

import (
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v2"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins"
)

func TestResolveConfigurationFromFile(t *testing.T) {
	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()

	type args struct {
		config *conf.SchedulerConfiguration
	}
	tests := []struct {
		name    string
		args    args
		want    *conf.SchedulerConfiguration
		wantErr bool
	}{
		{
			name: "valid config",
			args: args{
				config: &conf.SchedulerConfiguration{
					Actions: "consolidation",
					Tiers: []conf.Tier{
						{
							Plugins: []conf.PluginOption{
								{
									Name: "n1",
								},
							},
						},
					},
					QueueDepthPerAction: map[string]int{
						"consolidation": 10,
					},
				},
			},
			want: &conf.SchedulerConfiguration{
				Actions: "consolidation",
				Tiers: []conf.Tier{
					{
						Plugins: []conf.PluginOption{
							{
								Name:      "n1",
								Arguments: map[string]string{},
							},
						},
					},
				},
				QueueDepthPerAction: map[string]int{
					"consolidation": 10,
				},
			},
			wantErr: false,
		},
		{
			name: "valid config - no QueueDepthPerAction map",
			args: args{
				config: &conf.SchedulerConfiguration{
					Actions: "consolidation",
					Tiers: []conf.Tier{
						{
							Plugins: []conf.PluginOption{
								{
									Name: "n1",
								},
							},
						},
					},
				},
			},
			want: &conf.SchedulerConfiguration{
				Actions: "consolidation",
				Tiers: []conf.Tier{
					{
						Plugins: []conf.PluginOption{
							{
								Name:      "n1",
								Arguments: map[string]string{},
							},
						},
					},
				},
				QueueDepthPerAction: nil,
			},
			wantErr: false,
		},
		{
			name: "invalid config - wrong action",
			args: args{
				config: &conf.SchedulerConfiguration{
					Actions: "action1",
					Tiers: []conf.Tier{
						{
							Plugins: []conf.PluginOption{
								{
									Name: "n1",
								},
							},
						},
					},
					QueueDepthPerAction: map[string]int{
						"consolidation": 10,
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			confFile, err := generateConfigFile(tt.args.config)
			defer os.Remove(confFile.Name())
			if err != nil {
				t.Errorf("generateConfigFile() error = %v", err)
			}

			got, err := ResolveConfigurationFromFile(confFile.Name())
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveConfigurationFromFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ResolveConfigurationFromFile() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func generateConfigFile(config *conf.SchedulerConfiguration) (*os.File, error) {
	confFile, err := os.CreateTemp("", "scheduler_test_conf_")
	if err != nil {
		panic(err)
	}

	// Marshal the scheduler config to yaml format.
	data, err := yaml.Marshal(config)
	if err != nil {
		panic(err)
	}
	if _, err = confFile.Write(data); err != nil {
		panic(err)
	}
	confFile.Close()
	return confFile, err
}
