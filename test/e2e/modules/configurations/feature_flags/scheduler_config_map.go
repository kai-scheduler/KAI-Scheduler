/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package feature_flags

import (
	"context"

	"gopkg.in/yaml.v3"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
	testContext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	schedulerConfigMapName = "scheduler-config"
	schedulerConfigDataKey = "config.yaml"
)

type config struct {
	Actions             string         `yaml:"actions"`
	Tiers               []tier         `yaml:"tiers,omitempty"`
	QueueDepthPerAction map[string]int `yaml:"queueDepthPerAction,omitempty"`
}

type tier struct {
	Plugins []plugin `yaml:"plugins"`
}

type plugin struct {
	Name      string            `yaml:"name"`
	Arguments map[string]string `yaml:"arguments,omitempty"`
}

var defaultKaiSchedulerPlugins = []tier{
	{
		Plugins: []plugin{
			{Name: "predicates"},
			{Name: "proportion"},
			{Name: "priority"},
			{Name: "nodeavailability"},
			{Name: "resourcetype"},
			{Name: "podaffinity"},
			{Name: "elastic"},
			{Name: "kubeflow"},
			{Name: "ray"},
			{Name: "taskorder"},
			{Name: "subgrouporder"},
			{Name: "nominatednode"},
			{Name: "dynamicresources"},
			{Name: "minruntime"},
			{Name: "topology"},
		},
	},
}

type getSchedulerConfigMapData func() (*config, error)

func updateKaiSchedulerConfigMap(ctx context.Context, testCtx *testContext.TestContext, getCmData getSchedulerConfigMapData) error {
	schedulerConfig, err := testCtx.KubeClientset.CoreV1().ConfigMaps(constant.SystemPodsNamespace).
		Get(ctx, schedulerConfigMapName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	innerConfig, err := getCmData()
	if err != nil {
		return err
	}

	data, marshalErr := yaml.Marshal(&innerConfig)
	if marshalErr != nil {
		return marshalErr
	}
	schedulerConfig.Data = map[string]string{
		schedulerConfigDataKey: string(data),
	}

	_, err = testCtx.KubeClientset.CoreV1().ConfigMaps(constant.SystemPodsNamespace).
		Update(ctx, schedulerConfig, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return wait.ForRolloutRestartDeployment(ctx, testCtx.ControllerClient, constant.SystemPodsNamespace,
		constant.SchedulerDeploymentName)
}
