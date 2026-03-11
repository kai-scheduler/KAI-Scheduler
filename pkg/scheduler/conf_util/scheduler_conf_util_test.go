// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package conf_util

import (
	"os"
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"

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
								Name: "n1",
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
								Name: "n1",
							},
						},
					},
				},
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
				t.Errorf("ResolveConfigurationFromFile() got: \n%+v, want: \n%+v", *got, *tt.want)
				t.Errorf("Actions equal: %v", got.Actions == tt.want.Actions)
				t.Errorf("Tiers equal: %v", reflect.DeepEqual(got.Tiers, tt.want.Tiers))
				t.Errorf("QueueDepthPerAction equal: %v", reflect.DeepEqual(got.QueueDepthPerAction, tt.want.QueueDepthPerAction))
				t.Errorf("UsageDBConfig equal: %v", reflect.DeepEqual(got.UsageDBConfig, tt.want.UsageDBConfig))
				if len(got.Tiers) > 0 && len(tt.want.Tiers) > 0 {
					t.Errorf("First tier plugins equal: %v", reflect.DeepEqual(got.Tiers[0].Plugins, tt.want.Tiers[0].Plugins))
					if len(got.Tiers[0].Plugins) > 0 && len(tt.want.Tiers[0].Plugins) > 0 {
						t.Errorf("First plugin Arguments: got=%v (nil=%v), want=%v (nil=%v)",
							got.Tiers[0].Plugins[0].Arguments,
							got.Tiers[0].Plugins[0].Arguments == nil,
							tt.want.Tiers[0].Plugins[0].Arguments,
							tt.want.Tiers[0].Plugins[0].Arguments == nil)
					}
				}
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
