/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package gitops

import (
	"context"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
)

const (
	operatorManagedLabel = "app.kubernetes.io/managed-by"
	operatorManagedValue = "kai-operator"

	// First sync installs CRDs + all operands (image pulls included), and
	// selfHeal recovery redeploys every owner-referenced operand. Generous:
	// the concurrent startup of all operands can briefly starve a small
	// (kind) cluster's API server.
	installTimeout        = 10 * time.Minute
	selfHealDetectTimeout = 3 * time.Minute
	// Application deletion prunes all resources and runs the PostDelete
	// cleanup hook before the finalizer is removed.
	uninstallTimeout = 10 * time.Minute
	// Fail-safe deadline for AfterAll's leftover-Application cleanup,
	// deliberately shorter than uninstallTimeout: it only runs after a
	// failed spec, the cluster is about to be discarded, and a stuck
	// deletion is force-unblocked rather than waited out (a re-created
	// Application converges any half-pruned leftovers).
	cleanupForceStripTimeout = 3 * time.Minute
)

func DescribeGitOpsSpecs() bool {
	return Describe("GitOps install via ArgoCD", Label(labels.GitOps), Ordered, func() {
		var (
			rawClient runtimeClient.Client
			testCtx   *testcontext.TestContext
		)

		BeforeAll(func(ctx context.Context) {
			chartVersion := os.Getenv("GITOPS_CHART_VERSION")
			Expect(chartVersion).NotTo(BeEmpty(),
				"GITOPS_CHART_VERSION environment variable must be set to the chart version served by the in-cluster chart repo")

			// GetConnectivity works before the Application installs the KAI
			// CRDs: its preflight treats a missing Queue CRD as proof of a
			// clean test cluster.
			rawClient = testcontext.GetConnectivity(ctx, Default).ControllerClient

			By("Creating the ArgoCD Application for the KAI chart in GitOps mode")
			err := rawClient.Create(ctx, kaiApplication(chartVersion))
			if !errors.IsAlreadyExists(err) {
				// AlreadyExists: continue against the existing Application,
				// e.g. re-running after an interrupted suite.
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterAll(func(ctx context.Context) {
			if rawClient == nil {
				return
			}
			if _, err := getApplication(ctx, rawClient); errors.IsNotFound(err) {
				return
			}
			By("Cleaning up the leftover Application")
			_ = rawClient.Delete(ctx, kaiApplication(""))
			appGone := func() bool {
				_, err := getApplication(ctx, rawClient)
				return errors.IsNotFound(err)
			}
			deadline := time.Now().Add(cleanupForceStripTimeout)
			for !appGone() && time.Now().Before(deadline) {
				time.Sleep(statusPollInterval)
			}
			if !appGone() {
				// Deletion stuck on the resources finalizer; strip it so a
				// failed spec cannot wedge the cluster for subsequent steps.
				stripApplicationFinalizer(ctx, rawClient)
			}
		})

		It("should sync to Synced and Healthy on a fresh cluster", func(ctx context.Context) {
			// Covers CRD establishment on first sync: the Config CRD arrives
			// with the chart, guarded by SkipDryRunOnMissingResource on the CR
			// and syncPolicy.retry.
			waitForAppSyncedHealthy(ctx, rawClient, installTimeout)
		})

		It("should deploy a healthy Config CR and operands", func(ctx context.Context) {
			By("Connecting to the cluster")
			testCtx = testcontext.GetConnectivity(ctx, Default)

			By("Waiting for the kai-config CR to report healthy status")
			wait.ForKAIConfigStatusOKWithTimeout(ctx, testCtx.ControllerClient, installTimeout)

			By("Waiting for the default scheduling shard to report healthy status")
			wait.ForSchedulingShardStatusOKWithTimeout(ctx, testCtx.ControllerClient, "default", installTimeout)

			By("Verifying operator-managed deployments are available")
			deployments := listOperatorDeployments(ctx, testCtx.ControllerClient)
			Expect(deployments).NotTo(BeEmpty(), "Expected operator-managed deployments in %s", kaiNamespace)
			for _, deployment := range deployments {
				Expect(deployment.Status.AvailableReplicas).To(BeNumerically(">", 0),
					"Expected deployment %s to have available replicas", deployment.Name)
			}
		})

		It("should schedule a workload", func(ctx context.Context) {
			By("Creating test queues")
			parentQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), "")
			childQueue := queue.CreateQueueObject(utils.GenerateRandomK8sName(10), parentQueue.Name)
			// AddQueues rather than InitQueues: it refreshes the TestContext's
			// stored context, which belongs to the earlier spec that connected
			// and is canceled by the time this spec runs.
			testCtx.AddQueues(ctx, []*v2.Queue{childQueue, parentQueue})

			By("Creating a pod and verifying it is scheduled")
			pod := rd.CreatePodObject(testCtx.Queues[0], v1.ResourceRequirements{})
			pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
			Expect(err).NotTo(HaveOccurred())
			wait.ForPodScheduled(ctx, testCtx.ControllerClient, pod)

			// Cleanup runs here rather than in AfterAll: it waits on scheduler
			// and binder events, which are gone after the Application deletion
			// spec uninstalls KAI.
			By("Cleaning up the test workload")
			testCtx.ClusterCleanup(ctx)
		})

		It("should self-heal a deleted Config CR (regression #1751)", func(ctx context.Context) {
			kaiConfig := &kaiv1.Config{}
			Expect(testCtx.ControllerClient.Get(ctx,
				runtimeClient.ObjectKey{Name: constants.DefaultKAIConfigSingeltonInstanceName}, kaiConfig)).To(Succeed())
			deletedUID := kaiConfig.UID

			By("Deleting the kai-config CR out-of-band")
			Expect(testCtx.ControllerClient.Delete(ctx, kaiConfig)).To(Succeed())

			By("Waiting for ArgoCD selfHeal to recreate the CR")
			Eventually(func(g Gomega) types.UID {
				recreated := &kaiv1.Config{}
				err := testCtx.ControllerClient.Get(ctx,
					runtimeClient.ObjectKey{Name: constants.DefaultKAIConfigSingeltonInstanceName}, recreated)
				g.Expect(err).NotTo(HaveOccurred())
				return recreated.UID
			}, selfHealDetectTimeout, statusPollInterval).ShouldNot(Or(BeEmpty(), Equal(deletedUID)),
				"Expected ArgoCD to recreate the deleted kai-config CR")

			// Deleting the CR garbage-collects all owner-referenced operands;
			// recovery is a full redeploy.
			By("Waiting for the recreated kai-config to report healthy status")
			wait.ForKAIConfigStatusOKWithTimeout(ctx, testCtx.ControllerClient, installTimeout)

			By("Waiting for the Application to return to Synced and Healthy")
			waitForAppSyncedHealthy(ctx, rawClient, installTimeout)
		})

		It("should uninstall KAI when the Application is deleted", func(ctx context.Context) {
			By("Deleting the Application")
			Expect(rawClient.Delete(ctx, kaiApplication(""))).To(Succeed())

			By("Waiting for the Application to be removed (PostDelete cleanup hook must succeed first)")
			waitForAppGone(ctx, rawClient, uninstallTimeout)

			By("Verifying the kai-config CR is gone")
			kaiConfig := &kaiv1.Config{}
			err := testCtx.ControllerClient.Get(ctx,
				runtimeClient.ObjectKey{Name: constants.DefaultKAIConfigSingeltonInstanceName}, kaiConfig)
			Expect(errors.IsNotFound(err) || meta.IsNoMatchError(err)).To(BeTrue(),
				"Expected the kai-config CR (or its CRD) to be deleted, got: %v", err)

			By("Verifying no operator-managed deployments remain")
			Eventually(func(g Gomega) {
				g.Expect(listOperatorDeployments(ctx, testCtx.ControllerClient)).To(BeEmpty())
			}, selfHealDetectTimeout, statusPollInterval).Should(Succeed())
		})
	})
}

func listOperatorDeployments(ctx context.Context, c runtimeClient.Client) []appsv1.Deployment {
	deployments := &appsv1.DeploymentList{}
	Expect(c.List(ctx, deployments,
		runtimeClient.InNamespace(kaiNamespace),
		runtimeClient.MatchingLabels{operatorManagedLabel: operatorManagedValue})).To(Succeed())
	return deployments.Items
}
