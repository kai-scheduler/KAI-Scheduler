// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_plugins

import (
	"context"
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	ksf "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/features"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	k8splfeature "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/k8s_utils"

	"github.com/NVIDIA/KAI-scheduler/pkg/binder/plugins/k8s-plugins/common"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/plugins/k8s-plugins/dynamicresources"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/plugins/k8s-plugins/volumebinding"
	"github.com/NVIDIA/KAI-scheduler/pkg/binder/plugins/state"
)

type K8sPlugins struct {
	plugins []common.K8sPlugin
	states  sync.Map // underlying type is map[types.UID]*PodState
}

type PodState struct {
	states map[string]ksf.CycleState
	skip   map[string]bool
}

func New(
	client kubernetes.Interface,
	informerFactory informers.SharedInformerFactory,
	timeoutSeconds int64,
) (*K8sPlugins, error) {
	var k8sPlugins []common.K8sPlugin
	k8sFramework := k8s_utils.NewFrameworkHandle(client, informerFactory, nil)
	k8sFeatures := k8splfeature.Features{
		// EnableVolumeCapacityPriority:    feature.DefaultFeatureGate.Enabled(features.VolumeCapacityPriority),
		EnableDynamicResourceAllocation: feature.DefaultFeatureGate.Enabled(features.DynamicResourceAllocation),
	}

	logger := log.Log.WithName("binder-plugins")
	logger.Info("Feature flags", "features", k8sFeatures)

	for _, initFunc := range []func(k8sframework.Handle, *k8splfeature.Features, int64) (common.K8sPlugin, error){
		volumebinding.NewVolumeBindingPlugin,
		dynamicresources.NewDynamicResourcesPlugin,
	} {
		plugin, err := initFunc(k8sFramework, &k8sFeatures, timeoutSeconds)
		if err != nil {
			return nil, err
		}
		k8sPlugins = append(k8sPlugins, plugin)
	}

	return &K8sPlugins{
		plugins: k8sPlugins,
	}, nil
}

func (p *K8sPlugins) Name() string {
	return "k8s-plugins"
}

func (p *K8sPlugins) Validate(*v1.Pod) error {
	return nil
}

func (p *K8sPlugins) Mutate(*v1.Pod) error {
	return nil
}

func (p *K8sPlugins) PreBind(ctx context.Context, pod *v1.Pod, node *v1.Node, request *v1alpha2.BindRequest,
	_ *state.BindingState) error {
	podState := &PodState{
		states: map[string]ksf.CycleState{},
		skip:   map[string]bool{},
	}

	pod.Spec.NodeName = node.Name
	for index, plugin := range p.plugins {
		err, state := p.bindPluginWrapper(ctx, plugin, pod, node, request, podState)
		podState.states[plugin.Name()] = state
		if err != nil {
			logger := log.FromContext(ctx)
			logger.Error(
				err, "Binder plugin failed",
				"plugin", plugin.Name(),
				"pod", pod.Name,
				"namespace", pod.Namespace,
				"node", node.Name,
			)

			for i := 0; i < index; i++ {
				p.plugins[i].UnAllocate(ctx, pod, node.Name, podState.states[p.plugins[i].Name()])
			}

			pod.Spec.NodeName = ""
			return err
		}
	}

	p.states.Store(pod.UID, podState)
	return nil
}

func (p *K8sPlugins) PostBind(ctx context.Context, pod *v1.Pod, node *v1.Node, _ *v1alpha2.BindRequest,
	_ *state.BindingState) {
	podStateAny, found := p.states.LoadAndDelete(pod.UID)
	if !found {
		logger := log.FromContext(ctx)
		logger.Error(fmt.Errorf("state not found"), "PostBind: could not find state for pod",
			pod.Namespace, pod.Name)
		return
	}
	podState := podStateAny.(*PodState)

	for _, plugin := range p.plugins {
		if !plugin.IsRelevant(pod) || podState.skip[plugin.Name()] {
			continue
		}
		plugin.PostBind(ctx, pod, node.Name, podState.states[plugin.Name()])
	}
}

func (p *K8sPlugins) bindPluginWrapper(
	ctx context.Context, plugin common.K8sPlugin, pod *v1.Pod, node *v1.Node, request *v1alpha2.BindRequest, podState *PodState,
) (error, ksf.CycleState) {
	state := common.NewState()
	if !plugin.IsRelevant(pod) {
		return nil, state
	}
	err, skip := plugin.PreFilter(ctx, pod, state)
	if err != nil {
		return fmt.Errorf("K8sPlugin %s failed PreFilter for pod: %s/%s. error: %s",
			plugin.Name(), pod.Namespace, pod.Name, err), state
	}
	if skip {
		podState.skip[plugin.Name()] = true
		return nil, state
	}

	err = plugin.Filter(ctx, pod, node, state)
	if err != nil {
		return fmt.Errorf("K8sPlugin %s failed Filter for pod: %s/%s and node %s. error: %s",
			plugin.Name(), pod.Namespace, pod.Name, node.Name, err), state
	}

	err = plugin.Allocate(ctx, pod, node.Name, state)
	if err != nil {
		return fmt.Errorf("K8sPlugin %s failed Allocate for pod: %s/%s and node %s. error: %s",
			plugin.Name(), pod.Namespace, pod.Name, node.Name, err), state
	}

	err = plugin.Bind(ctx, pod, request, state)
	if err != nil {
		plugin.UnAllocate(ctx, pod, node.Name, state)
		return fmt.Errorf("K8sPlugin %s failed Bind for pod: %s/%s and node %s. error: %s",
			plugin.Name(), pod.Namespace, pod.Name, node.Name, err), state
	}
	return nil, state
}
