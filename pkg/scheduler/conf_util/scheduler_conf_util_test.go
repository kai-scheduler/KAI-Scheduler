// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package conf_util

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/multinodegang"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/nodelocalgreedy"
	"github.com/kai-scheduler/api/constants"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
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
				ScenarioSearchBudgets: defaultScenarioSearchBudgetsForTest(),
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
				ScenarioSearchBudgets: defaultScenarioSearchBudgetsForTest(),
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
				t.Errorf("ScenarioSearchBudgets equal: %v", reflect.DeepEqual(got.ScenarioSearchBudgets, tt.want.ScenarioSearchBudgets))
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

func TestGetActionsFromConfigSkipsBlankTokens(t *testing.T) {
	actions.InitDefaultActions()

	got, err := GetActionsFromConfig(&conf.SchedulerConfiguration{
		Actions: "allocate, , consolidation,  ",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"allocate", "consolidation"}, actionNameList(got))
}

func TestGetActionsFromConfigEmptyAndBlank(t *testing.T) {
	actions.InitDefaultActions()

	for _, actionsValue := range []string{"", " , ", "   "} {
		got, err := GetActionsFromConfig(&conf.SchedulerConfiguration{Actions: actionsValue})
		require.NoError(t, err)
		require.Empty(t, got)
		require.True(t, (&conf.SchedulerConfiguration{Actions: actionsValue}).NoActions())
	}

	_, err := GetActionsFromConfig(&conf.SchedulerConfiguration{Actions: "not-an-action"})
	require.Error(t, err)
}

func TestResolveConfigurationFromFileUnsetActionsUsesDefaults(t *testing.T) {
	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()

	confFile := writeSchedulerConf(t, "tiers:\n- plugins:\n  - name: predicates\n")
	got, err := ResolveConfigurationFromFile(confFile)
	require.NoError(t, err)

	defaults, err := GetDefaultSchedulerConf()
	require.NoError(t, err)
	require.Equal(t, defaults.Actions, got.Actions)
	require.False(t, got.NoActions())
	require.Equal(t, []conf.Tier{{Plugins: []conf.PluginOption{{Name: "predicates"}}}}, got.Tiers)

	parsed, err := GetActionsFromConfig(got)
	require.NoError(t, err)
	require.Equal(t, actionNameList(mustActions(t, defaults)), actionNameList(parsed))
}

func TestResolveConfigurationFromFileEmptyActionsRunsNone(t *testing.T) {
	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()

	for _, raw := range []string{
		"actions: \"\"\ntiers:\n- plugins:\n  - name: predicates\n",
		"actions: \" , \"\ntiers:\n- plugins:\n  - name: predicates\n",
	} {
		confFile := writeSchedulerConf(t, raw)
		got, err := ResolveConfigurationFromFile(confFile)
		require.NoError(t, err)
		require.True(t, got.NoActions())

		parsed, err := GetActionsFromConfig(got)
		require.NoError(t, err)
		require.Empty(t, parsed)
		require.Equal(t, []conf.Tier{{Plugins: []conf.PluginOption{{Name: "predicates"}}}}, got.Tiers)
	}
}

func TestEmptyActionsYAMLRoundTripKeepsExplicitEmpty(t *testing.T) {
	actions.InitDefaultActions()
	plugins.InitDefaultPlugins()

	data, err := yaml.Marshal(&conf.SchedulerConfiguration{Actions: ""})
	require.NoError(t, err)
	require.Contains(t, string(data), "actions:")

	confFile := writeSchedulerConf(t, string(data)+"tiers:\n- plugins:\n  - name: predicates\n")
	got, err := ResolveConfigurationFromFile(confFile)
	require.NoError(t, err)
	require.True(t, got.NoActions())
}

func TestDefaultSchedulerConfRegistersScenarioGeneratorsForDefaultBudgets(t *testing.T) {
	actions.InitDefaultActions()

	schedulerConf, err := GetDefaultSchedulerConf()
	require.NoError(t, err)

	ssn := &framework.Session{Config: schedulerConf}
	registeredPlugins := map[string]bool{}
	for _, tier := range schedulerConf.Tiers {
		for _, plugin := range tier.Plugins {
			switch plugin.Name {
			case nodelocalgreedy.Name:
				nodelocalgreedy.New(plugin.Arguments).OnSessionOpen(ssn)
				registeredPlugins[plugin.Name] = true
			case multinodegang.Name:
				multinodegang.New(plugin.Arguments).OnSessionOpen(ssn)
				registeredPlugins[plugin.Name] = true
			}
		}
	}

	require.True(t, registeredPlugins[nodelocalgreedy.Name])
	require.True(t, registeredPlugins[multinodegang.Name])
	require.NoError(t, ssn.ValidateScenarioGeneratorBudgetKeys())
}

func defaultScenarioSearchBudgetsForTest() *kaiv1.ScenarioSearchBudgets {
	return &kaiv1.ScenarioSearchBudgets{
		MaxActionSearchDuration: map[string]metav1.Duration{
			constants.ActionDefault: scenarioSearchDurationForTest(constants.DefaultActionBudget),
		},
		MaxJobSearchDuration: scenarioSearchDurationPtrForTest(constants.DefaultJobBudget),
		MinJobSearchDuration: scenarioSearchDurationPtrForTest(constants.DefaultMinJobBudget),
		MaxGeneratorSearchDuration: map[string]metav1.Duration{
			constants.ActionDefault:            scenarioSearchDurationForTest(constants.DefaultGeneratorBudget),
			constants.GeneratorNodeLocalGreedy: scenarioSearchDurationForTest(constants.DefaultNodeLocalGreedy),
			constants.GeneratorMultiNodeGang:   scenarioSearchDurationForTest(constants.DefaultMultiNodeGang),
		},
	}
}

func scenarioSearchDurationForTest(value string) metav1.Duration {
	duration, err := time.ParseDuration(value)
	if err != nil {
		panic(err)
	}
	return metav1.Duration{Duration: duration}
}

func scenarioSearchDurationPtrForTest(value string) *metav1.Duration {
	return ptr.To(scenarioSearchDurationForTest(value))
}

func actionNameList(configured []framework.Action) []string {
	names := make([]string, 0, len(configured))
	for _, action := range configured {
		names = append(names, string(action.Name()))
	}
	return names
}

func mustActions(t *testing.T, schedulerConf *conf.SchedulerConfiguration) []framework.Action {
	t.Helper()
	configured, err := GetActionsFromConfig(schedulerConf)
	require.NoError(t, err)
	return configured
}

func writeSchedulerConf(t *testing.T, raw string) string {
	t.Helper()
	confFile, err := os.CreateTemp("", "scheduler_test_conf_")
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Remove(confFile.Name())
	})
	_, err = confFile.WriteString(raw)
	require.NoError(t, err)
	require.NoError(t, confFile.Close())
	return confFile.Name()
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
