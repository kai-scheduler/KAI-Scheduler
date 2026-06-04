// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/nodes_fake"
)

type testFunction func(ssn *Session, stmt *Statement)
type testMetadata struct {
	name string
	fn   testFunction
}

func TestStatement_Checkpoint(t *testing.T) {
	tests := []testMetadata{
		{
			name: "rollback evict",
			fn: func(ssn *Session, stmt *Statement) {
				err := stmt.Evict(ssn.ClusterInfo.PodGroupInfos["running_job0"].GetAllPodsMap()["running_job0-0"], "evicting task",
					eviction_info.EvictionMetadata{
						Action:           "action",
						EvictionGangSize: 1,
					})
				assert.Nil(t, err)
			},
		},
		{
			name: "rollback allocate",
			fn: func(ssn *Session, stmt *Statement) {
				err := stmt.Allocate(ssn.ClusterInfo.PodGroupInfos["pending_job0"].GetAllPodsMap()["pending_job0-0"], "node0")
				assert.Nil(t, err)
			},
		},
		{
			name: "rollback pipeline updateIfNeeded true",
			fn: func(ssn *Session, stmt *Statement) {
				err := stmt.Pipeline(ssn.ClusterInfo.PodGroupInfos["pending_job0"].GetAllPodsMap()["pending_job0-0"], "node0", true)
				assert.Nil(t, err)
			},
		},
		{
			name: "rollback pipeline updateIfNeeded false",
			fn: func(ssn *Session, stmt *Statement) {
				err := stmt.Pipeline(ssn.ClusterInfo.PodGroupInfos["pending_job0"].GetAllPodsMap()["pending_job0-0"], "node0", false)
				assert.Nil(t, err)
			},
		},
		{
			name: "rollback allocate evict",
			fn: func(ssn *Session, stmt *Statement) {
				err := stmt.Allocate(ssn.ClusterInfo.PodGroupInfos["pending_job0"].GetAllPodsMap()["pending_job0-0"], "node0")
				assert.Nil(t, err)
				err = stmt.Evict(ssn.ClusterInfo.PodGroupInfos["pending_job0"].GetAllPodsMap()["pending_job0-0"], "evicting task",
					eviction_info.EvictionMetadata{
						Action:           "action",
						EvictionGangSize: 1,
					})
				assert.Nil(t, err)
			},
		},
		{
			name: "rollback pipeline evict",
			fn: func(ssn *Session, stmt *Statement) {
				err := stmt.Pipeline(ssn.ClusterInfo.PodGroupInfos["pending_job0"].GetAllPodsMap()["pending_job0-0"], "node0", true)
				assert.Nil(t, err)
				err = stmt.Evict(ssn.ClusterInfo.PodGroupInfos["pending_job0"].GetAllPodsMap()["pending_job0-0"], "evicting task",
					eviction_info.EvictionMetadata{
						Action:           "action",
						EvictionGangSize: 1,
					})
				assert.Nil(t, err)
			},
		},
		{
			name: "rollback evict pipeline",
			fn: func(ssn *Session, stmt *Statement) {
				err := stmt.Evict(ssn.ClusterInfo.PodGroupInfos["running_job0"].GetAllPodsMap()["running_job0-0"], "evicting task",
					eviction_info.EvictionMetadata{
						Action:           "action",
						EvictionGangSize: 1,
					})
				assert.Nil(t, err)
				err = stmt.Pipeline(ssn.ClusterInfo.PodGroupInfos["running_job0"].GetAllPodsMap()["running_job0-0"], "node0", true)
				assert.Nil(t, err)
			},
		},
		{
			name: "rollback pipeline evict update false",
			fn: func(ssn *Session, stmt *Statement) {
				err := stmt.Pipeline(ssn.ClusterInfo.PodGroupInfos["pending_job0"].GetAllPodsMap()["pending_job0-0"], "node0", false)
				assert.Nil(t, err)
				err = stmt.Evict(ssn.ClusterInfo.PodGroupInfos["pending_job0"].GetAllPodsMap()["pending_job0-0"], "evicting task",
					eviction_info.EvictionMetadata{
						Action:           "action",
						EvictionGangSize: 1,
					})
				assert.Nil(t, err)
			},
		},
		{
			name: "rollback evict pipeline update false",
			fn: func(ssn *Session, stmt *Statement) {
				err := stmt.Evict(ssn.ClusterInfo.PodGroupInfos["running_job0"].GetAllPodsMap()["running_job0-0"], "evicting task",
					eviction_info.EvictionMetadata{
						Action:           "action",
						EvictionGangSize: 1,
					})
				assert.Nil(t, err)
				err = stmt.Pipeline(ssn.ClusterInfo.PodGroupInfos["running_job0"].GetAllPodsMap()["running_job0-0"], "node0", false)
				assert.Nil(t, err)
			},
		},
		{
			name: "rollback evict checkpoint pipeline update false",
			fn: func(ssn *Session, stmt *Statement) {
				err := stmt.Evict(ssn.ClusterInfo.PodGroupInfos["running_job0"].GetAllPodsMap()["running_job0-0"], "evicting task",
					eviction_info.EvictionMetadata{
						Action:           "action",
						EvictionGangSize: 1,
					})
				assert.Nil(t, err)
				cp := stmt.Checkpoint()
				err = stmt.Pipeline(ssn.ClusterInfo.PodGroupInfos["running_job0"].GetAllPodsMap()["running_job0-0"], "node0", false)
				assert.Nil(t, err)
				err = stmt.Rollback(cp)
				assert.Nil(t, err)
			},
		},
		// It's not possible to allocate a task that was evicted, only pipeline - verify that checkpoints still work
		{
			name: "rollback illegal evict allocate",
			fn: func(ssn *Session, stmt *Statement) {
				err := stmt.Evict(ssn.ClusterInfo.PodGroupInfos["running_job0"].GetAllPodsMap()["running_job0-0"], "evicting task",
					eviction_info.EvictionMetadata{
						Action:           "action",
						EvictionGangSize: 1,
					})
				assert.Nil(t, err)
				err = stmt.Allocate(ssn.ClusterInfo.PodGroupInfos["running_job0"].GetAllPodsMap()["running_job0-0"], "node0")
				assert.Error(t, err)
			},
		},
	}

	for i, test := range tests {
		t.Logf("Running test %d: %s", i, test.name)

		clusterTopology := nodes_fake.TestClusterTopology{
			Name: "test",
			Jobs: []*jobs_fake.TestJobBasic{
				{
					Name:                "running_job0",
					RequiredGPUsPerTask: 1,
					QueueName:           "queue0",
					Priority:            constants.PriorityTrainNumber,
					Tasks: []*tasks_fake.TestTaskBasic{
						{
							State:    pod_status.Running,
							NodeName: "node0",
						},
					},
				},
				{
					Name:                "pending_job0",
					RequiredGPUsPerTask: 1,
					QueueName:           "queue0",
					Priority:            constants.PriorityTrainNumber,
					Tasks: []*tasks_fake.TestTaskBasic{
						{
							State: pod_status.Pending,
						},
					},
				},
			},
			Nodes: map[string]nodes_fake.TestNodeBasic{
				"node0": {
					GPUs: 2,
				},
			},
		}
		vectorMap := resource_info.NewResourceVectorMap()
		jobsInfoMap, tasksToNodeMap, _ := jobs_fake.BuildJobsAndTasksMaps(clusterTopology.Jobs, vectorMap)
		nodesInfoMap := nodes_fake.BuildNodesInfoMap(clusterTopology.Nodes, tasksToNodeMap, nil, vectorMap)
		ssn := &Session{
			ClusterInfo: &api.ClusterInfo{},
		}
		ssn.ClusterInfo.PodGroupInfos = jobsInfoMap
		ssn.ClusterInfo.Nodes = nodesInfoMap

		s := &Statement{
			operations: []Operation{},
			ssn:        ssn,
			sessionID:  "1234",
		}

		originalJobs, originalNodes := extractSessionAssertedData(ssn)

		cp := s.Checkpoint()
		test.fn(ssn, s)
		err := s.Rollback(cp)
		assert.Nil(t, err)

		updatedJobs, updatedNodes := extractSessionAssertedData(ssn)
		assertEqualSessionData(t, updatedJobs, originalJobs, updatedNodes, originalNodes)
	}
}

func extractSessionAssertedData(ssn *Session) (
	jobs map[common_info.PodGroupID]*podgroup_info.PodGroupInfo,
	nodes map[string]*nodeAssertedInfo,
) {
	jobs = make(map[common_info.PodGroupID]*podgroup_info.PodGroupInfo, len(ssn.ClusterInfo.PodGroupInfos))
	for id, job := range ssn.ClusterInfo.PodGroupInfos {
		jobs[id] = job.Clone()
	}

	nodes = make(map[string]*nodeAssertedInfo, len(ssn.ClusterInfo.Nodes))
	for name, node := range ssn.ClusterInfo.Nodes {
		nodes[name] = extractNodeAssertedInfo(node)
	}

	return
}

func assertEqualSessionData(t *testing.T,
	jobs, originalJobs map[common_info.PodGroupID]*podgroup_info.PodGroupInfo,
	nodes, originalNodes map[string]*nodeAssertedInfo,
) {
	for id, job := range jobs {
		originalJob := originalJobs[id]
		assert.Equal(t, originalJob.AllocatedVector, job.AllocatedVector)
		for name, actualTask := range job.GetAllPodsMap() {
			originalTask := originalJob.GetAllPodsMap()[name]
			assert.Equal(t, actualTask.NodeName, originalTask.NodeName)
			assert.Equal(t, originalTask.Status, actualTask.Status)
			assert.Equal(t, actualTask.GpuRequirement, originalTask.GpuRequirement)
			assert.Equal(t, actualTask.ResReqVector, originalTask.ResReqVector)
		}
	}

	for name, node := range nodes {
		assert.Equal(t, originalNodes[name], node)
	}
}
