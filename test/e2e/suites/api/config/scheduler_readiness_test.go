/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package config

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

var _ = Describe("Scheduler Readiness", Ordered, func() {
	var (
		testCtx          *testcontext.TestContext
		originalReplicas *int32
		setupComplete    bool
	)

	BeforeAll(func(ctx context.Context) {
		testCtx = testcontext.GetConnectivity(ctx, Default)

		kaiConfig := &kaiv1.Config{}
		Expect(testCtx.ControllerClient.Get(ctx,
			runtimeClient.ObjectKey{Name: constants.DefaultKAIConfigSingeltonInstanceName},
			kaiConfig)).To(Succeed())
		originalReplicas = kaiConfig.Spec.Scheduler.Replicas
		setupComplete = true
	})

	AfterAll(func(ctx context.Context) {
		if !setupComplete {
			return
		}
		Expect(configurations.PatchKAIConfig(ctx, testCtx, func(conf *kaiv1.Config) {
			conf.Spec.Scheduler.Replicas = originalReplicas
		})).To(Succeed())
		wait.ForSchedulingShardStatusOK(ctx, testCtx.ControllerClient, "default")
	})

	It("should route scheduler service traffic only to the elected leader", func(ctx context.Context) {
		cfg := testconfig.GetConfig()

		Expect(configurations.PatchKAIConfig(ctx, testCtx, func(conf *kaiv1.Config) {
			conf.Spec.Scheduler.Replicas = ptr.To(int32(2))
		})).To(Succeed())
		wait.WaitForDeploymentPodsRunning(ctx, testCtx.ControllerClient, cfg.SchedulerDeploymentName, cfg.SystemPodsNamespace)
		wait.ForSchedulingShardStatusOK(ctx, testCtx.ControllerClient, "default")

		var readyPodIP string
		Eventually(func(g Gomega) {
			pods := &v1.PodList{}
			g.Expect(testCtx.ControllerClient.List(ctx, pods,
				runtimeClient.InNamespace(cfg.SystemPodsNamespace),
				runtimeClient.MatchingLabels{constants.AppLabelName: cfg.SchedulerDeploymentName},
			)).To(Succeed())
			g.Expect(pods.Items).To(HaveLen(2))

			readyPods := []v1.Pod{}
			for _, pod := range pods.Items {
				if rd.IsPodReady(&pod) {
					readyPods = append(readyPods, pod)
				}
			}
			g.Expect(readyPods).To(HaveLen(1))
			readyPodIP = readyPods[0].Status.PodIP
			g.Expect(readyPodIP).NotTo(BeEmpty())
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		Eventually(func(g Gomega) {
			sliceList := &discoveryv1.EndpointSliceList{}
			g.Expect(testCtx.ControllerClient.List(ctx, sliceList,
				runtimeClient.InNamespace(cfg.SystemPodsNamespace),
				runtimeClient.MatchingLabels{discoveryv1.LabelServiceName: cfg.SchedulerDeploymentName},
			)).To(Succeed())

			readyIPv4 := map[string]struct{}{}
			for _, es := range sliceList.Items {
				if es.AddressType != discoveryv1.AddressTypeIPv4 {
					continue
				}
				for _, ep := range es.Endpoints {
					if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
						continue
					}
					for _, addr := range ep.Addresses {
						readyIPv4[addr] = struct{}{}
					}
				}
			}
			g.Expect(readyIPv4).To(HaveLen(1))
			g.Expect(readyIPv4).To(HaveKey(readyPodIP))
		}, time.Minute, 5*time.Second).Should(Succeed())
	})
})
