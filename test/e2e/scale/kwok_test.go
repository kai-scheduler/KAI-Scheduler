// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kwok "github.com/run-ai/kwok-operator/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	schedulerconfig "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant/labels"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/fillers"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/crd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait/watcher"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/scale/topology"
)

var _ = ReportAfterSuite("report failed test suite if needed", func(report Report) {
	for _, specReport := range report.SpecReports {
		if specReport.Failed() {
			if err := writeTestResults("", false, nil); err != nil {
				GinkgoLogr.Error(err, "Failed to write test results")
			}
			return
		}
	}
})

var _ = Describe("Kwok scale test", Ordered, Label(labels.Scale), func() {
	var (
		testCtx                   *testcontext.TestContext
		parentQueue               *v2.Queue
		sanityTestQueue           *v2.Queue
		noGPUQuotaQueue           *v2.Queue
		reclaimSingleGPUJobsQueue *v2.Queue
		reclaimMultiNodeQueue     *v2.Queue
		fillerQueue               *v2.Queue

		priorityClass string

		originalFlowTimeout = watcher.FlowTimeout
		numberOfNodes       int
	)

	BeforeAll(func(ctx context.Context) {
		var err error

		numberOfNodes = defaultNumberOfNodes
		nodeCountEnvValue := os.Getenv("NODE_COUNT")
		if len(nodeCountEnvValue) > 0 {
			if value, err := strconv.Atoi(nodeCountEnvValue); err == nil {
				numberOfNodes = value
			} else {
				GinkgoLogr.Error(err, "failed to read NODE_COUNT environment variable")
			}
		}

		testCtx = testcontext.GetConnectivity(ctx, Default)

		crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, "stages.kwok.x-k8s.io", "v1alpha1")
		crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, "nodepools.kwok.sigs.run-ai.com", "v1beta1")

		queues := v2.QueueList{}
		Expect(testCtx.ControllerClient.List(ctx, &queues)).To(Succeed())
		for _, queueToClean := range queues.Items {
			cleanupTestQueue(ctx, testCtx, &queueToClean)
		}
		schedulerconfig.EnableScheduler(ctx, testCtx)

		parentQueue = queue.CreateQueueObject("parent-"+utils.GenerateRandomK8sName(10), "")
		fillerQueue = queue.CreateQueueObject("filler-"+utils.GenerateRandomK8sName(10), parentQueue.Name)
		sanityTestQueue = queue.CreateQueueObject("sanity-"+utils.GenerateRandomK8sName(10), parentQueue.Name)

		noGPUQuotaQueue = queue.CreateQueueObject("no-quota-"+utils.GenerateRandomK8sName(10), parentQueue.Name)
		noGPUQuotaQueue.Spec.Resources.GPU = v2.QueueResource{
			Quota:           0,
			OverQuotaWeight: 0,
			Limit:           0,
		}

		priorityClass, err = rd.CreatePreemptiblePriorityClass(ctx, testCtx.KubeClientset)
		Expect(err).NotTo(HaveOccurred())

		testCtx.InitQueues([]*v2.Queue{
			fillerQueue, sanityTestQueue, parentQueue, noGPUQuotaQueue,
		})

		watcher.FlowTimeout = maxFlowTimeoutMinutes * time.Minute
	})

	AfterAll(func(ctx context.Context) {
		if CurrentSpecReport().Failed() {
			return
		}
		for _, queueToClean := range testCtx.Queues {
			cleanupTestQueue(ctx, testCtx, queueToClean)
		}

		wait.ForNoE2EPods(ctx, testCtx.ControllerClient)

		testCtx.ClusterCleanup(ctx)
		Expect(rd.DeleteAllE2EPriorityClasses(ctx, testCtx.ControllerClient)).To(Succeed())
		watcher.FlowTimeout = originalFlowTimeout
	})

	Context("Topology", Ordered, func() {
		var (
			originalNodePools []kwok.NodePool
			topologyNodePools []kwok.NodePool

			clusterTopology kaiv1alpha1.Topology

			topologyLevels []topology.TopologyLevel
			nodesPerDomain = 2
			totalNodes     int
			topologyName   string
		)
		BeforeAll(func(ctx context.Context) {
			crd.SkipIfCrdIsNotInstalled(ctx, testCtx.KubeConfig, "topologies.kai.scheduler", "v1alpha1")

			updateFakeGPUOperatorGPUsPerNode(ctx, testCtx)

			var nodePools kwok.NodePoolList
			Expect(testCtx.ControllerClient.List(ctx, &nodePools)).To(Succeed())

			originalNodePools = nodePools.DeepCopy().Items
			for _, nodePool := range nodePools.Items {
				baseNodePool := nodePool.DeepCopy()
				nodePool.Spec.NodeCount = 0
				Expect(testCtx.ControllerClient.Patch(ctx, &nodePool, runtimeClient.MergeFrom(baseNodePool))).To(Succeed(), "Failed to scale node pool to 0", "nodePool", nodePool.Name)
			}

			topologyLevels = []topology.TopologyLevel{
				{
					Name:      "cloud.provider.com/topology-zone",
					Count:     4,
					ShortName: "zone",
				},
				{
					Name:      "cloud.provider.com/topology-block",
					Count:     8,
					ShortName: "block",
				},
				{
					Name:      "cloud.provider.com/topology-rack",
					Count:     8,
					ShortName: "rack",
				},
			}
			totalNodes = nodesPerDomain
			for _, level := range topologyLevels {
				totalNodes *= level.Count
			}

			topologyName = "e2e-topology-tree"
			clusterTopology = topology.GenerateTopology(topologyLevels, topologyName)
			Expect(testCtx.ControllerClient.Create(ctx, &clusterTopology)).To(Succeed())

			topologyNodePools = topology.GenerateNodePools(topologyLevels, nodesPerDomain, map[string]string{"test": "topology-e2e"})
			var wg sync.WaitGroup
			for _, nodePool := range topologyNodePools {
				wg.Add(1)
				go func(nodePool kwok.NodePool) {
					defer wg.Done()
					defer GinkgoRecover()
					Expect(testCtx.ControllerClient.Create(ctx, &nodePool)).To(Succeed(), "Failed to create topology node pool", "nodePool", nodePool.Name)
				}(nodePool)
			}
			wg.Wait()

			startTime := time.Now()
			wait.ForExactlyNKWOKOperatorNodePools(ctx, testCtx.ControllerClient, map[string]string{"test": "topology-e2e"}, len(topologyNodePools))
			duration := time.Since(startTime)
			GinkgoLogr.Info("Time to create and wait for topology node pools", "duration", duration)

			startTime = time.Now()
			wait.ForAtLeastNNodes(ctx, testCtx.ControllerClient, map[string]string{"test": "topology-e2e"}, len(topologyNodePools))
			duration = time.Since(startTime)
			GinkgoLogr.Info("Time to wait for topology nodes to be ready", "duration", duration)

			startTime = time.Now()
			wait.ForGPUOPeratorUpdateOnKWOKNodes(ctx, testCtx.ControllerClient, totalNodes, gpusPerNode)
			duration = time.Since(startTime)
			GinkgoLogr.Info("Time to wait for GPU operator to update topology nodes", "duration", duration)
		})

		AfterAll(func(ctx context.Context) {
			Expect(testCtx.ControllerClient.Delete(ctx, &clusterTopology)).To(Succeed(), "Failed to delete cluster topology")

			Expect(testCtx.ControllerClient.DeleteAllOf(ctx, &kwok.NodePool{},
				runtimeClient.MatchingLabels{"test": "topology-e2e"})).
				To(Succeed(), "Failed to delete topology node pools")

			wait.ForExactlyNKWOKOperatorNodePools(ctx, testCtx.ControllerClient, map[string]string{"test": "topology-e2e"}, 0)

			for _, nodePool := range originalNodePools {
				baseNodePool := nodePool.DeepCopy()
				baseNodePool.Spec.NodeCount = 0
				Expect(testCtx.ControllerClient.Patch(ctx, &nodePool, runtimeClient.MergeFrom(baseNodePool))).To(Succeed(), "Failed to restore node pool", "nodePool", nodePool.Name)
				wait.ForKWOKOperatorNodePool(ctx, testCtx.ControllerClient, nodePool.Name)
			}
			wait.ForZeroKWOKNodes(ctx, testCtx.ControllerClient) // Wait until all kwok nodes are deleted
		})

		AfterEach(func(ctx context.Context) {
			if CurrentSpecReport().Failed() {
				return
			}
			cleanupTestQueue(ctx, testCtx, sanityTestQueue)
		})

		It("Allocate single distributed job with preferred topology", func(ctx context.Context) {
			distributedJobsScaleTestInternal(ctx, testCtx, sanityTestQueue,
				1, totalNodes, 2, "Allocate with preferred topology", totalNodes,
				&v2alpha2.TopologyConstraint{
					PreferredTopologyLevel: topologyLevels[2].Name,
					Topology:               topologyName,
				})
		})

		It("Allocate single distributed job without preferred topology", func(ctx context.Context) {
			distributedJobsScaleTestInternal(ctx, testCtx, sanityTestQueue,
				1, totalNodes, 2, "Allocate without preferred topology", totalNodes,
				nil)
		})
	})

	Context("Big cluster", Ordered, func() {
		BeforeAll(func(ctx context.Context) {
			GinkgoLogr.Info("Updating fake GPU operator GPUs per node")
			updateFakeGPUOperatorGPUsPerNode(ctx, testCtx)

			GinkgoLogr.Info("Setting up managed node pool to 0 nodes")
			managedNodePool := kwok.NodePool{
				ObjectMeta: metav1.ObjectMeta{
					Name: KWOKOperatorNodePoolName,
				},
			}
			Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(&managedNodePool), &managedNodePool)).To(Succeed())

			originalManagedNodePool := managedNodePool.DeepCopy()
			managedNodePool.Spec.NodeCount = int32(0)

			Expect(testCtx.ControllerClient.Patch(
				ctx, &managedNodePool, runtimeClient.MergeFrom(originalManagedNodePool))).NotTo(HaveOccurred())

			wait.ForKWOKOperatorNodePool(ctx, testCtx.ControllerClient, managedNodePool.Name)
			wait.ForZeroKWOKNodes(ctx, testCtx.ControllerClient) // Wait until all kwok nodes are deleted

			GinkgoLogr.Info("Filling all existing nodes with single GPU jobs to avoid fair share calculation issues")
			_, _, err := fillers.FillAllNodesWithJobs(
				ctx, testCtx, fillerQueue,
				SingleGPURequirement, nil, nil, priorityClass,
			)
			Expect(err).To(Succeed())

			originalManagedNodePool = managedNodePool.DeepCopy()
			managedNodePool.Spec.NodeCount = int32(numberOfNodes)

			GinkgoLogr.Info("Setting up managed node pool. ", "numberOfNodes", numberOfNodes, "gpusPerNode", gpusPerNode)
			Expect(testCtx.ControllerClient.Patch(
				ctx, &managedNodePool, runtimeClient.MergeFrom(originalManagedNodePool))).NotTo(HaveOccurred())

			wait.ForKWOKOperatorNodePool(ctx, testCtx.ControllerClient, managedNodePool.Name)
			wait.ForGPUOPeratorUpdateOnKWOKNodes(ctx, testCtx.ControllerClient, numberOfNodes, gpusPerNode)
		})

		Context("Sanity Fill Cluster", Ordered, func() {
			AfterEach(func(ctx context.Context) {
				if CurrentSpecReport().Failed() {
					return
				}
				cleanupTestQueue(ctx, testCtx, sanityTestQueue)
			})

			Context("scheduler disabled during job creation", func() {
				It("fill cluster with single GPU Jobs", func(ctx context.Context) {
					basicScaleTest(
						ctx, testCtx, "Fill Cluster with single GPU Jobs",
						sanityTestQueue, true, numberOfNodes,
					)
				}, SpecTimeout(maxFlowTimeoutMinutes*time.Minute))
			})

			Context("all services are running during job creation", func() {
				It("fill cluster with single GPU Jobs", func(ctx context.Context) {
					basicScaleTest(
						ctx, testCtx, "Fill Cluster with single GPU Jobs - scheduler is running while submitting jobs",
						sanityTestQueue, false, numberOfNodes,
					)
				}, SpecTimeout(maxFlowTimeoutMinutes*time.Minute))
			})
		})

		Context("Whole GPU Tests", Ordered, func() {
			AfterEach(func(ctx context.Context) {
				if CurrentSpecReport().Failed() {
					return
				}
				for _, queueToClean := range testCtx.Queues {
					if queueToClean.Name == fillerQueue.Name {
						continue
					}
					cleanupTestQueue(ctx, testCtx, queueToClean)
				}
			})

			It("schedules jobs with pending tasks in background", func(ctx context.Context) {
				var wg sync.WaitGroup
				for range pendingBackgroundTasks {
					wg.Add(1)
					go func() {
						defer wg.Done()
						createJobObjectForKwok(
							ctx, testCtx, noGPUQuotaQueue,
							SingleGPURequirement, map[string]string{},
						)
					}()
				}
				wg.Wait()

				wait.ForAtLeastNPodCreation(ctx, testCtx.ControllerClient, metav1.LabelSelector{
					MatchLabels: map[string]string{
						testconfig.GetConfig().QueueLabelKey: noGPUQuotaQueue.Name,
					},
				}, pendingBackgroundTasks)

				basicScaleTest(
					ctx, testCtx, fmt.Sprintf(
						"Fill Cluster with single GPU Jobs with %v pending tasks in background", pendingBackgroundTasks,
					),
					sanityTestQueue, true, numberOfNodes,
				)
			}, SpecTimeout(maxFlowTimeoutMinutes*time.Minute))

			Context("Reclaim", func() {
				BeforeAll(func(ctx context.Context) {
					sanityTestQueue.Spec.Resources.GPU = v2.QueueResource{
						Quota:           0,
						OverQuotaWeight: 0,
						Limit:           -1,
					}
					Expect(testCtx.ControllerClient.Patch(ctx, sanityTestQueue, runtimeClient.MergeFrom(&v2.Queue{}))).To(Succeed())

					reclaimSingleGPUJobsQueue = queue.CreateQueueObject("reclaim-single-"+utils.GenerateRandomK8sName(10), parentQueue.Name)
					testCtx.AddQueues(ctx, []*v2.Queue{reclaimSingleGPUJobsQueue})
				})

				Context("Single GPU Reclaimees", func() {
					BeforeEach(func(ctx context.Context) {
						fillClusterWithJobs(ctx, testCtx, sanityTestQueue, true, numberOfNodes, SingleGPURequirement)
					})

					Context("measure reclaim failure time", func() {
						BeforeAll(func(ctx context.Context) {
							Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(sanityTestQueue), sanityTestQueue)).To(Succeed())
							sanityTestQueue.Spec.Resources.GPU.Quota = float64((numberOfNodes * gpusPerNode) - (defaultPodsPerDistributedJob * gpusPerNode) + 1)
							Expect(testCtx.ControllerClient.Update(ctx, sanityTestQueue)).To(Succeed())
						})

						AfterAll(func(ctx context.Context) {
							if CurrentSpecReport().Failed() {
								return
							}
							Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKeyFromObject(sanityTestQueue), sanityTestQueue)).To(Succeed())
							sanityTestQueue.Spec.Resources.GPU.Quota = 0
							Expect(testCtx.ControllerClient.Update(ctx, sanityTestQueue)).To(Succeed())
						})

						It("measure time for reclaim to fail on distributed job last pod", func(ctx context.Context) {
							averageTimeToUnschedulable := measureUnschedulableDelayInSeconds(
								ctx, testCtx, reclaimSingleGPUJobsQueue,
								func(ctx context.Context, testCtx *testcontext.TestContext, queue *v2.Queue) (*v2alpha2.PodGroup, []*v1.Pod, error) {
									return createDistributedJobForKwok(
										ctx, testCtx, queue,
										v1.ResourceRequirements{
											Limits: map[v1.ResourceName]resource.Quantity{
												constants.NvidiaGpuResource: *resource.NewQuantity(int64(gpusPerNode), resource.DecimalSI),
											},
										}, defaultPodsPerDistributedJob,
										map[string]string{}, nil,
									)
								},
							)
							Expect(writeTestResults(
								"Average time to unschedulable for distributed job", true,
								map[string]interface{}{
									"nodes":                numberOfNodes,
									"pods":                 defaultPodsPerDistributedJob,
									"total requested gpus": defaultPodsPerDistributedJob * gpusPerNode,
									"average time to unschedulable (seconds)": averageTimeToUnschedulable,
								},
							)).To(Succeed())
						})
					})

					Context("measure reclaim time", func() {
						BeforeEach(func(ctx context.Context) {
							GinkgoLogr.Info("Wait for pods in sanity queue to come back up")
							wait.ForPodCountInNamespace(ctx, testCtx.ControllerClient, sanityTestQueue, numberOfNodes*gpusPerNode, maxFlowTimeoutMinutes*time.Minute)
						})

						AfterEach(func(ctx context.Context) {
							if CurrentSpecReport().Failed() {
								return
							}
							cleanupTestQueue(ctx, testCtx, reclaimSingleGPUJobsQueue)
						})

						It("reclaims for one very large job", func(ctx context.Context) {
							reclaimForOneLargeJob(ctx, testCtx, reclaimSingleGPUJobsQueue, numberOfNodes/2)
						}, SpecTimeout(maxFlowTimeoutMinutes*time.Minute))

						It("reclaims single GPU Jobs at intervals to measure latency", func(ctx context.Context) {
							measureReclaimSingleGPUJob(ctx, testCtx, reclaimSingleGPUJobsQueue, numberOfNodes)
						}, SpecTimeout(maxFlowTimeoutMinutes*time.Minute))
					})

					It("reclaims many single GPU Jobs", func(ctx context.Context) {
						GinkgoLogr.Info("Wait for pods in sanity queue to come back up")
						wait.ForPodCountInNamespace(ctx, testCtx.ControllerClient, sanityTestQueue, numberOfNodes*gpusPerNode, maxFlowTimeoutMinutes*time.Minute)

						basicScaleTest(
							ctx, testCtx, fmt.Sprintf(
								"Reclaim %d single GPU Jobs", numberOfNodes*gpusPerNode,
							),
							reclaimSingleGPUJobsQueue, true, numberOfNodes,
						)
					}, SpecTimeout(maxFlowTimeoutMinutes*time.Minute))

					Context("distributed jobs", func() {
						BeforeAll(func(ctx context.Context) {
							reclaimMultiNodeQueue = queue.CreateQueueObject("reclaim-distributed-"+utils.GenerateRandomK8sName(10), parentQueue.Name)
							testCtx.AddQueues(ctx, []*v2.Queue{reclaimMultiNodeQueue})
						})

						It("reclaims with distributed jobs", func(ctx context.Context) {
							distributedJobsScaleTest(ctx, testCtx, reclaimMultiNodeQueue, "Multi Node Reclaim for distributed jobs", numberOfNodes)
						}, SpecTimeout(maxFlowTimeoutMinutes*time.Minute))

						It("consolidates for many large jobs", func(ctx context.Context) {
							GinkgoLogr.Info("delete jobs from each node.", "queue", sanityTestQueue.Name)
							deleteJobsFromAllNodes(ctx, testCtx, sanityTestQueue)

							GinkgoLogr.Info("running consolidate test")
							consolidateScaleTest(ctx, testCtx, reclaimMultiNodeQueue, numberOfNodes)
						})
					})
				})

				Context("Full Node GPU Reclaimees", func() {
					BeforeEach(func(ctx context.Context) {
						fillClusterWithJobs(ctx, testCtx, sanityTestQueue, true, numberOfNodes, FullNodeGPURequirement)
					})

					Context("measure reclaim time", func() {
						BeforeEach(func(ctx context.Context) {
							GinkgoLogr.Info("Wait for pods in sanity queue to come back up")
							wait.ForPodCountInNamespace(ctx, testCtx.ControllerClient, sanityTestQueue, numberOfNodes, maxFlowTimeoutMinutes*time.Minute)
						})

						AfterEach(func(ctx context.Context) {
							if CurrentSpecReport().Failed() {
								return
							}
							cleanupTestQueue(ctx, testCtx, reclaimSingleGPUJobsQueue)
						})

						It("reclaims for one very large job", func(ctx context.Context) {
							reclaimForOneLargeJob(ctx, testCtx, reclaimSingleGPUJobsQueue, numberOfNodes)
						}, SpecTimeout(maxFlowTimeoutMinutes*time.Minute))
					})
				})
			})

			Context("Burst Jobs", func() {
				BeforeAll(func(ctx context.Context) {
					Expect(createPodCompletionStage(
						ctx, testCtx.ControllerClient,
						time.Second*20, time.Second*40,
					)).To(Succeed())
				})
				AfterAll(func(ctx context.Context) {
					Expect(deletePodCompletionStage(
						ctx, testCtx.ControllerClient),
					).To(Succeed())
				})

				It("Runs NCCL Simulation on empty cluster", Label(labels.NCCL), func(ctx context.Context) {
					testSucceeded, totalPods, completedPods, pendingPods, startTime := runNCCLSimulation(ctx, testCtx, sanityTestQueue, numberOfNodes)
					writeTestResults(
						"NCCL Simulation on empty cluster", testSucceeded,
						map[string]interface{}{
							"nodes":          numberOfNodes,
							"total pods":     totalPods,
							"completed pods": completedPods,
							"pending pods":   pendingPods,
							"time(seconds)":  time.Since(startTime).Seconds(),
						},
					)
				})

				// TODO: This can't complete in time yet, will have to wait for scale tests to run in cluster instead of from github action
				// It("Runs NCCL Simulation when cluster is initially full", func(ctx context.Context) {
				// 	sanityTestQueue.Spec.Resources.GPU = v2.QueueResource{
				// 		Quota:           0,
				// 		OverQuotaWeight: 0,
				// 		Limit:           -1,
				// 	}
				// 	Expect(testCtx.ControllerClient.Patch(ctx, sanityTestQueue, runtimeClient.MergeFrom(&v2.Queue{}))).To(Succeed())

				// 	ncclTestJobsQueue := queue.CreateQueueObject("nccl-"+utils.GenerateRandomK8sName(10), parentQueue.Name)
				// 	testCtx.AddQueues(ctx, []*v2.Queue{ncclTestJobsQueue})
				// 	fillClusterWithJobs(ctx, testCtx, sanityTestQueue, false, numberOfNodes)
				// 	runNCCLSimulation(ctx, testCtx, ncclTestJobsQueue)
				// })
			})
		})
	})
})

func updateFakeGPUOperatorGPUsPerNode(ctx context.Context, testCtx *testcontext.TestContext) {
	topologyConfig := &v1.ConfigMap{}

	Expect(testCtx.ControllerClient.Get(ctx, runtimeClient.ObjectKey{
		Namespace: gpuOperatorNamespace,
		Name:      "topology",
	}, topologyConfig)).To(Succeed())

	var config interface{}
	Expect(yaml.Unmarshal([]byte(topologyConfig.Data["topology.yml"]), &config)).NotTo(HaveOccurred())

	config.(map[string]interface{})["nodePools"].(map[string]interface{})["default"].(map[string]interface{})["gpuCount"] = gpusPerNode

	bytes, err := yaml.Marshal(config)
	Expect(err).NotTo(HaveOccurred())

	topologyConfig.Data["topology.yml"] = string(bytes)
	Expect(testCtx.ControllerClient.Update(ctx, topologyConfig)).NotTo(HaveOccurred())
}
