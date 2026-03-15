// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"os"
	"strconv"
	"time"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"

	. "go.uber.org/mock/gomock"
	"gopkg.in/yaml.v2"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	enginev2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	_ "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/actions"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf_util"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
)

const (
	defaultOverQuotaWeight = 1
	schedulerName          = "kai-scheduler"
)

func CreateFakeSession(schedulerConfig *TestSessionConfig,
	nodesInfoMap map[string]*node_info.NodeInfo,
	jobInfoMap map[common_info.PodGroupID]*podgroup_info.PodGroupInfo,
	queueInfoMap map[common_info.QueueID]*queue_info.QueueInfo,
	testMetadata TestTopologyBasic,
	controller *Controller,
	createCacheMockIfNotExists bool,
	topologies []*kaiv1alpha1.Topology,
	clusterPodAffinityInfo *cache.K8sClusterPodAffinityInfo,
) *framework.Session {
	ssn := framework.Session{
		Config: &conf.SchedulerConfiguration{
			Tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{},
				},
			},
		},
		ClusterInfo: &api.ClusterInfo{
			Nodes:            nodesInfoMap,
			Queues:           queueInfoMap,
			PodGroupInfos:    jobInfoMap,
			ResourceClaims:   getResourceClaims(testMetadata),
			Topologies:       topologies,
			MinNodeGPUMemory: node_info.DefaultGpuMemory,
		},
		SchedulerParams: conf.SchedulerParams{
			QueueLabelKey: constants.DefaultQueueLabel,
		},
	}
	ssn.OverrideMaxNumberConsolidationPreemptees(-1)
	ssn.OverrideAllowConsolidatingReclaim(true)
	ssn.OverrideSchedulerName(schedulerName)

	if controller != nil || createCacheMockIfNotExists {
		ssn.Cache = GetTestCacheMock(controller, testMetadata.Mocks, getDRAObjects(testMetadata), clusterPodAffinityInfo)
	}

	if schedulerConfig != nil {
		addSessionPlugins(&ssn, schedulerConfig.Plugins, createCacheMockIfNotExists, schedulerConfig.CachePlugins)
	}

	// Some plugins are using informers wrappers (such as the DRA manager) which require a moment to sync
	// without this some tests might have flaky results.
	time.Sleep(time.Millisecond)

	return &ssn
}

func BuildQueueInfoMap(testMetadata TestTopologyBasic) map[common_info.QueueID]*queue_info.QueueInfo {
	queueInfoMap := map[common_info.QueueID]*queue_info.QueueInfo{}
	for queueIndex, queue := range testMetadata.Queues {
		maxAllowed := common_info.NoMaxAllowedResource
		if queue.MaxAllowedGPUs != 0 {
			maxAllowed = queue.MaxAllowedGPUs
		}

		queueResource := enginev2.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name:              queue.Name,
				UID:               types.UID(queue.Name),
				CreationTimestamp: metav1.Time{Time: time.Now().Add(time.Minute * time.Duration(queueIndex))},
			},
			Spec: enginev2.QueueSpec{
				DisplayName: queue.Name,
				ParentQueue: queue.ParentQueue,
				Priority:    queue.Priority,
				Resources: &enginev2.QueueResources{
					GPU: enginev2.QueueResource{
						Quota:           queue.DeservedGPUs,
						Limit:           maxAllowed,
						OverQuotaWeight: queue.GPUOverQuotaWeight,
					},
					CPU: enginev2.QueueResource{
						Quota:           common_info.NoMaxAllowedResource,
						Limit:           common_info.NoMaxAllowedResource,
						OverQuotaWeight: defaultOverQuotaWeight,
					},
					Memory: enginev2.QueueResource{
						Quota:           common_info.NoMaxAllowedResource,
						Limit:           common_info.NoMaxAllowedResource,
						OverQuotaWeight: defaultOverQuotaWeight,
					},
				},
			},
		}

		if queue.DeservedCPUs != nil {
			queueResource.Spec.Resources.CPU.Quota = *queue.DeservedCPUs
		}

		if queue.DeservedMemory != nil {
			queueResource.Spec.Resources.Memory.Quota = *queue.DeservedMemory
		}

		if queue.MaxAllowedCPUs != nil {
			queueResource.Spec.Resources.CPU.Limit = *queue.MaxAllowedCPUs
		}
		if queue.MaxAllowedMemory != nil {
			queueResource.Spec.Resources.Memory.Limit = *queue.MaxAllowedMemory
		}

		if queue.V1 {
			queueResource.Spec.Resources = nil
		}

		queueInfo := queue_info.NewQueueInfo(&queueResource)
		queueInfoMap[queueInfo.UID] = queueInfo
	}

	return queueInfoMap
}

func addDefaultDepartmentIfNeeded(testMetadata *TestTopologyBasic) {
	if len(testMetadata.Departments) > 0 {
		return
	}

	if testMetadata.DisableDefaultDepartment {
		return
	}

	for index := range testMetadata.Queues {
		testMetadata.Queues[index].ParentQueue = "default"
	}
	testMetadata.Departments = []TestDepartmentBasic{{
		Name:           "default",
		DeservedGPUs:   common_info.NoMaxAllowedResource,
		MaxAllowedGPUs: common_info.NoMaxAllowedResource,
	}}
}

func BuildDepartmentInfoMap(testMetadata TestTopologyBasic) map[common_info.QueueID]*queue_info.QueueInfo {
	departmentInfoMap := map[common_info.QueueID]*queue_info.QueueInfo{}
	for departmentIndex, department := range testMetadata.Departments {
		maxAllowedGpus := common_info.NoMaxAllowedResource
		if department.MaxAllowedGPUs != 0 {
			maxAllowedGpus = department.MaxAllowedGPUs
		}
		departmentResource := enginev2.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name:              department.Name,
				UID:               types.UID(department.Name),
				CreationTimestamp: metav1.Time{Time: time.Now().Add(time.Minute * time.Duration(departmentIndex))},
			},
			Spec: enginev2.QueueSpec{
				Resources: &enginev2.QueueResources{
					GPU: enginev2.QueueResource{
						Quota:           department.DeservedGPUs,
						Limit:           maxAllowedGpus,
						OverQuotaWeight: department.DeservedGPUs,
					},
					CPU: enginev2.QueueResource{
						Quota:           common_info.NoMaxAllowedResource,
						Limit:           common_info.NoMaxAllowedResource,
						OverQuotaWeight: defaultOverQuotaWeight,
					},
					Memory: enginev2.QueueResource{
						Quota:           common_info.NoMaxAllowedResource,
						Limit:           common_info.NoMaxAllowedResource,
						OverQuotaWeight: defaultOverQuotaWeight,
					},
				},
			},
		}

		if department.MaxAllowedCPUs != nil {
			departmentResource.Spec.Resources.CPU.Limit = *department.MaxAllowedCPUs
		}
		if department.MaxAllowedMemory != nil {
			departmentResource.Spec.Resources.Memory.Limit = *department.MaxAllowedMemory
		}

		queueInfo := queue_info.NewQueueInfo(&departmentResource)
		departmentInfoMap[queueInfo.UID] = queueInfo
	}

	return departmentInfoMap
}

func BuildPlugins(testMetadata TestTopologyBasic) []conf.Tier {
	plugins.InitDefaultPlugins()
	confFileName := ""

	if testMetadata.Mocks != nil && testMetadata.Mocks.SchedulerConf != nil {
		// Create temp file to store the config file.
		confFile, err := os.CreateTemp("", "scheduler_test_conf_")
		if err != nil {
			panic(err)
		}
		defer os.Remove(confFile.Name())

		// Marshal the scheduler config to yaml format.
		data, err := yaml.Marshal(testMetadata.Mocks.SchedulerConf)
		if err != nil {
			panic(err)
		}
		if _, err := confFile.Write(data); err != nil {
			panic(err)
		}
		confFile.Close()

		confFileName = confFile.Name()
	}

	config, err := conf_util.ResolveConfigurationFromFile(confFileName)
	if err != nil {
		panic(err)
	}

	return config.Tiers
}

func BuildSession(testMetadata TestTopologyBasic, controller *Controller) *framework.Session {
	confPlugins := BuildPlugins(testMetadata)
	schedulerConfig := TestSessionConfig{
		Plugins: confPlugins,
		CachePlugins: map[string]bool{
			"predicates": true,
		},
	}

	addDefaultDepartmentIfNeeded(&testMetadata)

	vectorMap := resource_info.NewResourceVectorMap()
	jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps(testMetadata.Jobs, vectorMap, getDRAObjects(testMetadata)...)

	clusterPodAffinityInfo := cache.NewK8sClusterPodAffinityInfo()
	nodesInfoMap := nodes_fake.BuildNodesInfoMap(testMetadata.Nodes, tasksToNodeMap, clusterPodAffinityInfo, vectorMap, getDRAObjects(testMetadata)...)
	queueInfoMap := BuildQueueInfoMap(testMetadata)

	departmentInfoMap := BuildDepartmentInfoMap(testMetadata)
	queueInfoMap = mergeQueues(queueInfoMap, departmentInfoMap)
	cluster_info.UpdateQueueHierarchy(queueInfoMap)

	createCacheMockIfNotExists := testMetadata.Mocks != nil &&
		controller != nil &&
		testMetadata.Mocks.CacheRequirements != nil &&
		testMetadata.Mocks.Cache == nil

	return CreateFakeSession(&schedulerConfig, nodesInfoMap, jobsInfoMap, queueInfoMap, testMetadata,
		controller, createCacheMockIfNotExists, testMetadata.Topologies, clusterPodAffinityInfo)
}

func mergeQueues(queuesMaps ...map[common_info.QueueID]*queue_info.QueueInfo) map[common_info.QueueID]*queue_info.QueueInfo {
	mergedQueues := map[common_info.QueueID]*queue_info.QueueInfo{}
	for _, queuesMap := range queuesMaps {
		for k, v := range queuesMap {
			mergedQueues[k] = v
		}
	}
	return mergedQueues
}

func addSessionPlugins(ssn *framework.Session, tiers []conf.Tier, cacheMockExists bool, cacheMockPlugins map[string]bool) {
	ssn.Config.Tiers = tiers

	for _, tier := range tiers {
		for _, plugin := range tier.Plugins {
			if !cacheMockExists {
				if _, found := cacheMockPlugins[plugin.Name]; found {
					continue
				}
			}

			pb, found := framework.GetPluginBuilder(plugin.Name)
			if !found {
				log.InfraLogger.Errorf("Failed to get plugin %s.", plugin.Name)
				continue
			}

			pluginObj := pb(plugin.Arguments)
			pluginObj.OnSessionOpen(ssn)
		}
	}
}

func getDRAObjects(testMetadata TestTopologyBasic) []runtime.Object {
	var objects []runtime.Object
	deviceClasses := getDeviceClasses(testMetadata)
	for _, deviceClass := range deviceClasses {
		objects = append(objects, deviceClass)
	}

	resourceSlices := getResourceSlices(testMetadata)
	for _, resourceSlice := range resourceSlices {
		objects = append(objects, resourceSlice)
	}

	resourceClaims := getResourceClaims(testMetadata)
	for _, resourceClaim := range resourceClaims {
		objects = append(objects, resourceClaim)
	}

	return objects
}

func getResourceClaims(testMetadata TestTopologyBasic) []*resourceapi.ResourceClaim {
	var objects []*resourceapi.ResourceClaim
	for _, resourceClaim := range testMetadata.ResourceClaims {
		resourceClaimObject := resourceapi.ResourceClaim{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ResourceClaim",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:            resourceClaim.Name,
				Namespace:       resourceClaim.Namespace,
				ResourceVersion: "0",
				Labels:          resourceClaim.Labels,
			},
			Spec: resourceapi.ResourceClaimSpec{
				Devices: resourceapi.DeviceClaim{
					Requests: []resourceapi.DeviceRequest{
						{
							Name: "request",
							Exactly: &resourceapi.ExactDeviceRequest{
								DeviceClassName: resourceClaim.DeviceClassName,
								AllocationMode:  resourceapi.DeviceAllocationModeExactCount,
								Count:           resourceClaim.Count,
							},
						},
					},
				},
			},
		}
		if resourceClaim.ClaimStatus != nil {
			resourceClaimObject.Status = *resourceClaim.ClaimStatus
		}
		objects = append(objects, &resourceClaimObject)
	}
	return objects
}

func getResourceSlices(testMetadata TestTopologyBasic) []*resourceapi.ResourceSlice {
	var objects []*resourceapi.ResourceSlice
	for _, resourceSlice := range testMetadata.ResourceSlices {
		resourceSliceObject := resourceapi.ResourceSlice{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ResourceSlice",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:            resourceSlice.Name,
				ResourceVersion: "0",
			},
			Spec: resourceapi.ResourceSliceSpec{
				Driver: "nvidia.com/gpu",
				Pool: resourceapi.ResourcePool{
					Name:               resourceSlice.NodeName,
					ResourceSliceCount: int64(len(testMetadata.ResourceSlices)),
				},
				NodeSelector: resourceSlice.NodeSelector,
				NodeName:     ptr.To(resourceSlice.NodeName),
				AllNodes:     ptr.To(resourceSlice.AllNodes),
			},
		}

		devices := make([]resourceapi.Device, resourceSlice.Count)
		for i := range devices {
			devices[i].Name = strconv.Itoa(i)
			devices[i].Capacity = map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
				"gpu": {
					Value: resource.MustParse("1"),
				},
			}
		}
		resourceSliceObject.Spec.Devices = devices

		objects = append(objects, &resourceSliceObject)
	}
	return objects
}

func getDeviceClasses(testMetadata TestTopologyBasic) []*resourceapi.DeviceClass {
	var objects []*resourceapi.DeviceClass
	for _, deviceClass := range testMetadata.DeviceClasses {
		deviceClassObject := resourceapi.DeviceClass{
			TypeMeta: metav1.TypeMeta{
				Kind:       "DeviceClass",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:            deviceClass,
				ResourceVersion: "0",
			},
			Spec: resourceapi.DeviceClassSpec{},
		}
		objects = append(objects, &deviceClassObject)
	}
	return objects
}
