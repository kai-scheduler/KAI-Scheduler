// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reclaim

import (
	"bytes"
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/capacity"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

func DescribeFailedSearchBackoffSpecs() bool {
	return Describe("Reclaim with a failed-search backoff", Ordered, func() {
		const (
			minBackoff = 10 * time.Second
			maxBackoff = 20 * time.Second
		)
		var testCtx *testcontext.TestContext

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			capacity.SkipIfInsufficientClusterTopologyResources(testCtx.KubeClientset, []capacity.ResourceList{
				{
					Gpu:      resource.MustParse("4"),
					PodCount: 4,
				},
			})
			Expect(feature_flags.SetFailedSearchBackoff(ctx, testCtx, constants.ActionReclaim,
				&metav1.Duration{Duration: minBackoff}, &metav1.Duration{Duration: maxBackoff})).To(Succeed())
		})

		AfterAll(func(ctx context.Context) {
			Expect(feature_flags.SetFailedSearchBackoff(ctx, testCtx, constants.ActionReclaim, nil, nil)).To(Succeed())
			testCtx.ClusterCleanup(ctx)
		})

		It("skips the job between failed searches, and reclaims once the victims' min runtime ends", func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
			parentQueue, reclaimeeQueue, reclaimerQueue := CreateQueues(4, 1, 0)
			reclaimerQueue.Spec.Priority = ptr.To(constants.DefaultQueuePriority + 1)
			reclaimeeQueue.Spec.ReclaimMinRuntime = &metav1.Duration{Duration: time.Minute}
			testCtx.InitQueues([]*v2.Queue{parentQueue, reclaimeeQueue, reclaimerQueue})

			for _, gpus := range []float64{1, 3} {
				wait.ForPodScheduled(ctx, testCtx.ControllerClient, CreatePod(ctx, testCtx, reclaimeeQueue, gpus))
			}

			exhaustedBefore := reclaimSearchJobs(ctx, testCtx, "generators_exhausted")
			skippedBefore := reclaimSearchJobs(ctx, testCtx, "backoff")
			reclaimer := CreatePod(ctx, testCtx, reclaimerQueue, 1)

			// The victims' min runtime outlasts this window, so every search for the reclaimer fails.
			Consistently(func(g Gomega) {
				pod := &v1.Pod{}
				g.Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(reclaimer), pod)).To(Succeed())
				g.Expect(rd.IsPodScheduled(pod)).To(BeFalse())
			}, 30*time.Second, time.Second).Should(Succeed())

			// Without the backoff, reclaim searches for the job in every cycle, about 30 times. With it,
			// at about 0s, 10s and 30s, and skips the job in the cycles between.
			Expect(reclaimSearchJobs(ctx, testCtx, "generators_exhausted") - exhaustedBefore).To(BeNumerically("<=", 4))
			Expect(reclaimSearchJobs(ctx, testCtx, "backoff") - skippedBefore).To(BeNumerically(">=", 10))

			wait.ForPodScheduled(ctx, testCtx.ControllerClient, reclaimer)
		})
	})
}

// reclaimSearchJobs returns the scheduler's count of the jobs the reclaim scenario search reported with
// result.
func reclaimSearchJobs(ctx context.Context, testCtx *testcontext.TestContext, result string) float64 {
	cfg := testconfig.GetConfig()
	// The scheduler's metrics Service is named after its deployment.
	raw, err := testCtx.KubeClientset.CoreV1().Services(cfg.SystemPodsNamespace).
		ProxyGet("http", cfg.SchedulerDeploymentName, "http-metrics", "/metrics", nil).DoRaw(ctx)
	Expect(err).To(Succeed())
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(raw))
	Expect(err).To(Succeed())

	var count float64
	for _, metric := range families[constants.DefaultMetricsNamespace+"_scenario_search_jobs_total"].GetMetric() {
		labels := map[string]string{}
		for _, label := range metric.GetLabel() {
			labels[label.GetName()] = label.GetValue()
		}
		if labels["action"] == constants.ActionReclaim && labels["result"] == result {
			count += metric.GetCounter().GetValue()
		}
	}
	return count
}
