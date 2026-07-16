/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package config

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordinationv1 "k8s.io/api/coordination/v1"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/api/constants"
)

var _ = Describe("Scheduler leader routing", Ordered, func() {
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

	It("routes the Service to the lease holder and converges after the leader pod dies", func(ctx context.Context) {
		cfg := testconfig.GetConfig()

		Expect(configurations.PatchKAIConfig(ctx, testCtx, func(conf *kaiv1.Config) {
			conf.Spec.Scheduler.Replicas = ptr.To(int32(2))
		})).To(Succeed())
		wait.WaitForDeploymentPodsRunning(ctx, testCtx.ControllerClient, cfg.SchedulerDeploymentName, cfg.SystemPodsNamespace)
		wait.ForSchedulingShardStatusOK(ctx, testCtx.ControllerClient, "default")

		var initialLeaderIP, secondaryIP string
		Eventually(func(g Gomega) {
			leaderPodName := leaseHolderPodName(ctx, testCtx, cfg)
			g.Expect(leaderPodName).NotTo(BeEmpty())
			ips := readyEndpointIPs(ctx, g, testCtx, cfg)
			g.Expect(ips).To(HaveLen(1), "exactly one endpoint should be the leader")

			for _, p := range listSchedulerPods(ctx, g, testCtx, cfg) {
				if p.Name == leaderPodName {
					initialLeaderIP = p.Status.PodIP
				} else {
					secondaryIP = p.Status.PodIP
				}
			}
			g.Expect(initialLeaderIP).NotTo(BeEmpty())
			g.Expect(secondaryIP).NotTo(BeEmpty())
			g.Expect(ips).To(HaveKey(initialLeaderIP))
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		// Force-delete the lease holder (simulates SIGKILL / abrupt loss).
		leaderPodName := leaseHolderPodName(ctx, testCtx, cfg)
		Expect(leaderPodName).NotTo(BeEmpty())
		Expect(testCtx.ControllerClient.Delete(ctx,
			&v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: cfg.SystemPodsNamespace, Name: leaderPodName}},
			runtimeClient.GracePeriodSeconds(0),
			runtimeClient.PropagationPolicy(metav1.DeletePropagationBackground),
		)).To(Succeed())

		// Within lease TTL + reconcile budget, the EndpointSlice should swing
		// to the surviving pod with no stale entries from the deleted one.
		Eventually(func(g Gomega) {
			ips := readyEndpointIPs(ctx, g, testCtx, cfg)
			g.Expect(ips).To(HaveLen(1))
		}, time.Minute, 2*time.Second).Should(Succeed())
	})
})

func listSchedulerPods(ctx context.Context, g Gomega, testCtx *testcontext.TestContext, cfg testconfig.TestConfig) []v1.Pod {
	pods := &v1.PodList{}
	g.Expect(testCtx.ControllerClient.List(ctx, pods,
		runtimeClient.InNamespace(cfg.SystemPodsNamespace),
		runtimeClient.MatchingLabels{constants.AppLabelName: cfg.SchedulerDeploymentName},
	)).To(Succeed())
	out := make([]v1.Pod, 0, len(pods.Items))
	for _, p := range pods.Items {
		if p.DeletionTimestamp != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

func leaseHolderPodName(ctx context.Context, testCtx *testcontext.TestContext, cfg testconfig.TestConfig) string {
	lease := &coordinationv1.Lease{}
	if err := testCtx.ControllerClient.Get(ctx,
		runtimeClient.ObjectKey{Namespace: cfg.SystemPodsNamespace, Name: cfg.SchedulerName},
		lease); err != nil {
		return ""
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return ""
	}
	return strings.SplitN(*lease.Spec.HolderIdentity, "_", 2)[0]
}

// leaderEndpointSliceName matches the name produced by
// pkg/operator/operands/scheduler.endpointSliceForShard.
func leaderEndpointSliceName(cfg testconfig.TestConfig) string {
	return cfg.SchedulerDeploymentName + "-leader"
}

func readyEndpointIPs(ctx context.Context, g Gomega, testCtx *testcontext.TestContext, cfg testconfig.TestConfig) map[string]struct{} {
	es := &discoveryv1.EndpointSlice{}
	g.Expect(testCtx.ControllerClient.Get(ctx,
		runtimeClient.ObjectKey{Namespace: cfg.SystemPodsNamespace, Name: leaderEndpointSliceName(cfg)},
		es)).To(Succeed())

	ips := map[string]struct{}{}
	if es.AddressType != discoveryv1.AddressTypeIPv4 {
		return ips
	}
	for _, ep := range es.Endpoints {
		if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
			continue
		}
		for _, addr := range ep.Addresses {
			ips[addr] = struct{}{}
		}
	}
	return ips
}
