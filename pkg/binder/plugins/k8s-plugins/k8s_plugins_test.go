// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_plugins

import (
	"context"
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/k8s-plugins/common"
	MockK8sPluigns "github.com/kai-scheduler/KAI-scheduler/pkg/binder/plugins/k8s-plugins/common/mock"
)

func TestPlugins(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Plugins Suite")
}

var _ = Describe("k8sPlugins", func() {
	var (
		plugin1    *MockK8sPluigns.MockK8sPlugin
		plugin2    *MockK8sPluigns.MockK8sPlugin
		k8sPlugins *K8sPlugins

		pod  *v1.Pod
		node *v1.Node
	)
	BeforeEach(func() {
		mockController := gomock.NewController(GinkgoT())
		plugin1 = MockK8sPluigns.NewMockK8sPlugin(mockController)
		plugin1.EXPECT().Name().Return("plugin1").AnyTimes()
		plugin2 = MockK8sPluigns.NewMockK8sPlugin(mockController)
		plugin2.EXPECT().Name().Return("plugin2").AnyTimes()

		k8sPlugins = &K8sPlugins{
			plugins: []common.K8sPlugin{plugin1, plugin2},
		}

		pod = &v1.Pod{}
		node = &v1.Node{}
	})

	Context("happy flow", func() {

		It("Calls all plugins functions in order", func() {
			for _, plugin := range []*MockK8sPluigns.MockK8sPlugin{plugin1, plugin2} {
				happyFlowExpect(plugin, pod, node)
			}

			err, podState := invokePlugins(context.TODO(), pod, node, k8sPlugins)
			Expect(err).To(BeNil())

			Expect(podState.states).To(HaveLen(2))
		})

		It("Skips all calls for non relevant plugins", func() {
			plugin1.EXPECT().IsRelevant(gomock.Any()).Return(false).Times(2)
			happyFlowExpect(plugin2, pod, node)

			err, _ := invokePlugins(context.TODO(), pod, node, k8sPlugins)
			Expect(err).To(BeNil())
		})

		It("Skips the rest of the calls if PreFilter returns skip", func() {
			plugin1.EXPECT().IsRelevant(pod).Return(true).Times(2)
			plugin1.EXPECT().PreFilter(context.TODO(), pod, gomock.Any()).Return(nil, true).Times(1)
			happyFlowExpect(plugin2, pod, node)

			err, _ := invokePlugins(context.TODO(), pod, node, k8sPlugins)
			Expect(err).To(BeNil())
		})

	})

	Context("error flow", func() {
		It("calls UnAllocate for all plugins if one fails", func() {
			plugin1.EXPECT().IsRelevant(pod).Return(true).Times(1)
			call := plugin1.EXPECT().PreFilter(context.TODO(), pod, gomock.Any()).Return(nil, false).Times(1)
			call = plugin1.EXPECT().Filter(context.TODO(), pod, node, gomock.Any()).Return(nil).After(call).Times(1)
			call = plugin1.EXPECT().Allocate(context.TODO(), pod, node.Name, gomock.Any()).Return(nil).After(call).Times(1)
			call = plugin1.EXPECT().Bind(context.TODO(), pod, gomock.Any(), gomock.Any()).Return(nil).Times(1).After(call)
			plugin1.EXPECT().PostBind(context.TODO(), pod, node.Name, gomock.Any()).AnyTimes().After(call)
			plugin1.EXPECT().UnAllocate(context.TODO(), pod, node.Name, gomock.Any()).Times(1)

			plugin2.EXPECT().IsRelevant(pod).Return(true).Times(1)
			call = plugin2.EXPECT().PreFilter(context.TODO(), pod, gomock.Any()).Return(nil, false).Times(1)
			call = plugin2.EXPECT().Filter(context.TODO(), pod, node, gomock.Any()).Return(nil).After(call).Times(1)
			call = plugin2.EXPECT().Allocate(context.TODO(), pod, node.Name, gomock.Any()).Return(nil).After(call).Times(1)
			call = plugin2.EXPECT().Bind(context.TODO(), pod, gomock.Any(), gomock.Any()).Return(errors.New("failed to bind")).Times(1).After(call)
			plugin2.EXPECT().UnAllocate(context.TODO(), pod, node.Name, gomock.Any()).Times(1)

			err, _ := invokePlugins(context.TODO(), pod, node, k8sPlugins)
			Expect(err).ToNot(BeNil())

		})
	})
})

func invokePlugins(ctx context.Context, pod *v1.Pod, node *v1.Node, k8sPlugins *K8sPlugins) (error, *PodState) {
	err := k8sPlugins.PreBind(ctx, pod, node, nil, nil)
	if err != nil {
		return err, nil
	}
	podStateAny, _ := k8sPlugins.states.Load(pod.UID)
	podState := podStateAny.(*PodState)
	k8sPlugins.PostBind(ctx, pod, node, nil, nil)
	return nil, podState
}

func happyFlowExpect(plugin *MockK8sPluigns.MockK8sPlugin, pod *v1.Pod, node *v1.Node) {
	plugin.EXPECT().IsRelevant(pod).Return(true).Times(2)
	call := plugin.EXPECT().PreFilter(context.TODO(), pod, gomock.Any()).Return(nil, false).Times(1)
	call = plugin.EXPECT().Filter(context.TODO(), pod, node, gomock.Any()).Return(nil).After(call).Times(1)
	call = plugin.EXPECT().Allocate(context.TODO(), pod, node.Name, gomock.Any()).Return(nil).After(call).Times(1)
	call = plugin.EXPECT().Bind(context.TODO(), pod, gomock.Any(), gomock.Any()).Return(nil).Times(1).After(call)
	plugin.EXPECT().PostBind(context.TODO(), pod, node.Name, gomock.Any()).AnyTimes().After(call)
	plugin.EXPECT().UnAllocate(context.TODO(), pod, node.Name, gomock.Any()).Times(0)
}
