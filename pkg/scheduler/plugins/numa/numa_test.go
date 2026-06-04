// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	schedapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestParseDenylist(t *testing.T) {
	tests := map[string]struct {
		raw      string
		expected []v1.ResourceName
	}{
		"empty":                  {raw: "", expected: nil},
		"single":                 {raw: "memory", expected: []v1.ResourceName{"memory"}},
		"multiple with spaces":   {raw: " memory , cpu ", expected: []v1.ResourceName{"memory", "cpu"}},
		"trailing comma ignored": {raw: "memory,", expected: []v1.ResourceName{"memory"}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			args := framework.PluginArguments{}
			if test.raw != "" {
				args[denylistArg] = test.raw
			}
			denylist := parseDenylist(args)
			assert.Equal(t, len(test.expected), denylist.Len())
			for _, r := range test.expected {
				assert.True(t, denylist.Has(r), "expected %s in denylist", r)
			}
		})
	}
}

// makeTask builds a minimal task with the given QoS, request type and whole-GPU count.
func makeTask(qos v1.PodQOSClass, reqType pod_info.ResourceRequestType, gpus float64) *pod_info.PodInfo {
	return &pod_info.PodInfo{
		ResourceRequestType: reqType,
		GpuRequirement:      *resource_info.NewGpuResourceRequirementWithGpus(gpus, 0),
		Pod:                 &v1.Pod{Status: v1.PodStatus{QOSClass: qos}},
	}
}

func TestShouldHandle(t *testing.T) {
	plugin := &numaPlugin{}
	singleNUMA := &nodeTopology{policy: policySingleNUMANode}
	restricted := &nodeTopology{policy: policyRestricted}
	bestEffort := &nodeTopology{policy: policyBestEffort}

	tests := map[string]struct {
		task     *pod_info.PodInfo
		nt       *nodeTopology
		expected bool
	}{
		"guaranteed whole-GPU on single-numa-node": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 2), nt: singleNUMA, expected: true,
		},
		"guaranteed whole-GPU on restricted": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), nt: restricted, expected: true,
		},
		"nil node topology passes through": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), nt: nil, expected: false,
		},
		"best-effort policy passes through": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1), nt: bestEffort, expected: false,
		},
		"non-guaranteed QoS passes through": {
			task: makeTask(v1.PodQOSBurstable, pod_info.RequestTypeRegular, 1), nt: singleNUMA, expected: false,
		},
		"best-effort QoS passes through": {
			task: makeTask(v1.PodQOSBestEffort, pod_info.RequestTypeRegular, 0), nt: singleNUMA, expected: false,
		},
		"guaranteed fraction request is handled (cpu/memory still aligned)": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeFraction, 0), nt: singleNUMA, expected: true,
		},
		"guaranteed mig request is handled": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeMigInstance, 0), nt: singleNUMA, expected: true,
		},
		"guaranteed cpu/memory-only pod is handled": {
			task: makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 0), nt: singleNUMA, expected: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.expected, plugin.shouldHandle(test.task, test.nt))
		})
	}
}

func TestOnSessionOpenBuildsTopology(t *testing.T) {
	cpuZone := numaNodeZone("node-0", map[string]string{"cpu": "4"})

	nodes := map[string]*node_info.NodeInfo{
		"with-single-numa": {
			Name:                 "with-single-numa",
			NodeResourceTopology: nrtWithAttributes(policyValueSingleNUMANode, scopeValueContainer, cpuZone),
		},
		"with-best-effort": {
			Name:                 "with-best-effort",
			NodeResourceTopology: nrtWithAttributes(policyValueBestEffort, scopeValueContainer, cpuZone),
		},
		"without-nrt": {
			Name: "without-nrt",
		},
	}

	plugin := New(framework.PluginArguments{}).(*numaPlugin)
	ssn := &framework.Session{ClusterInfo: &schedapi.ClusterInfo{Nodes: nodes}}
	plugin.OnSessionOpen(ssn)

	// best-effort still builds a topology entry (its policy is recorded for v2 reuse), but
	// shouldHandle gates it out; only nodes with usable NUMA-node zones get an entry.
	assert.Contains(t, plugin.nodes, "with-single-numa")
	assert.Contains(t, plugin.nodes, "with-best-effort")
	assert.NotContains(t, plugin.nodes, "without-nrt", "node without NRT is a pass-through")

	assert.Equal(t, policySingleNUMANode, plugin.nodes["with-single-numa"].policy)
	assert.True(t, plugin.shouldHandle(
		makeTask(v1.PodQOSGuaranteed, pod_info.RequestTypeRegular, 1),
		plugin.nodes["with-single-numa"],
	))

	plugin.OnSessionClose(ssn)
	assert.Nil(t, plugin.nodes)
}
