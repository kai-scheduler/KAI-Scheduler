// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common_info

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	enginev2alpha2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/resource_info"
)

func TestJobFitErrorsToDetailedMessage(t *testing.T) {
	tests := []struct {
		name      string
		fitErrors []JobFitError
		want      string
	}{
		{
			name: "Single fit error",
			fitErrors: []JobFitError{
				NewJobFitError("job1", "subgroup1", "namespace1",
					UnschedulableWorkloadReason,
					[]string{"not enough CPU"},
					"detailed: not enough CPU"),
			},
			want: "\nNot enough resources for the workload.\nsubgroup subgroup1: detailed: not enough CPU.",
		},
		{
			name: "Multiple fit errors",
			fitErrors: []JobFitError{
				NewJobFitError("job1", "subgroup1", "namespace1",
					UnschedulableWorkloadReason,
					[]string{"not enough CPU"},
					"detailed: not enough CPU"),
				NewJobFitError("job1", "subgroup1", "namespace1",
					UnschedulableWorkloadReason,
					[]string{"not enough memory"},
					"detailed: not enough memory"),
			},
			want: "\nUnable to schedule podgroup.\nsubgroup subgroup1: detailed: not enough CPU.\nsubgroup subgroup1: detailed: not enough memory.",
		},
		{
			name: "Multiple fit errors with sorting",
			fitErrors: []JobFitError{
				NewJobFitError("job1", "subgroup1", "namespace1",
					UnschedulableWorkloadReason,
					[]string{"not enough memory"},
					"z-message"),
				NewJobFitError("job1", "subgroup1", "namespace1",
					UnschedulableWorkloadReason,
					[]string{"not enough CPU"},
					"a-message"),
			},
			want: "\nUnable to schedule podgroup.\nsubgroup subgroup1: a-message.\nsubgroup subgroup1: z-message.",
		},
		{
			name: "Multiple fit errors with topology fit errors",
			fitErrors: []JobFitError{
				NewTopologyFitError("job1", "subgroup1", "namespace1", "zone1",
					UnschedulableWorkloadReason,
					[]string{"not enough CPU"},
					"detailed: not enough CPU"),
				NewTopologyFitError("job1", "subgroup1", "namespace1", "zone2",
					UnschedulableWorkloadReason,
					[]string{"not enough memory"},
					"detailed: not enough memory"),
			},
			want: "\nUnable to schedule podgroup.\n<zone1>: subgroup subgroup1: detailed: not enough CPU.\n<zone2>: subgroup subgroup1: detailed: not enough memory.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JobFitErrorsToDetailedMessage(tt.fitErrors); got != tt.want {
				t.Errorf("JobFitErrorsToDetailedMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobFitErrorsToMessage(t *testing.T) {
	tests := []struct {
		name      string
		fitErrors []JobFitError
		want      string
	}{
		{
			name: "Single fit error",
			fitErrors: []JobFitError{
				NewJobFitError("job1", "subgroup1", "namespace1",
					UnschedulableWorkloadReason,
					[]string{"not enough CPU"}),
			},
			want: "Not enough resources for the workload: not enough CPU.",
		},
		{
			name: "Multiple fit errors with same message",
			fitErrors: []JobFitError{
				NewJobFitError("job1", "subgroup1", "namespace1",
					UnschedulableWorkloadReason,
					[]string{"node-group(s) didn't have enough resources: CPU cores"}),
				NewJobFitError("job1", "subgroup1", "namespace1",
					UnschedulableWorkloadReason,
					[]string{"node-group(s) didn't have enough resources: CPU cores"}),
			},
			want: "Unable to schedule podgroup: 2 node-group(s) didn't have enough resources: CPU cores.",
		},
		{
			name: "Multiple fit errors with different messages",
			fitErrors: []JobFitError{
				NewJobFitError("job1", "subgroup1", "namespace1",
					UnschedulableWorkloadReason,
					[]string{"not enough CPU", "not enough memory"}),
				NewJobFitError("job1", "subgroup1", "namespace1",
					UnschedulableWorkloadReason,
					[]string{"not enough GPUs"}),
			},
			want: "Unable to schedule podgroup: not enough CPU. \nnot enough GPUs. \nnot enough memory.",
		},
		{
			name:      "Empty fit errors",
			fitErrors: []JobFitError{},
			want:      "Unable to schedule podgroup",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JobFitErrorsToMessage(tt.fitErrors); got != tt.want {
				t.Errorf("JobFitErrorsToMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewJobFitError(t *testing.T) {
	type args struct {
		jobName          string
		subGroupName     string
		jobNamespace     string
		reason           enginev2alpha2.UnschedulableReason
		messages         []string
		detailedMessages []string
	}
	tests := []struct {
		name string
		args args
		want *JobFitErrorBase
	}{
		{
			name: "Create fit error with messages only",
			args: args{
				jobName:      "job1",
				subGroupName: "subgroup1",
				jobNamespace: "namespace1",
				reason:       UnschedulableWorkloadReason,
				messages:     []string{"not enough CPU"},
			},
			want: &JobFitErrorBase{
				jobNamespace:     "namespace1",
				jobName:          "job1",
				subGroupName:     "subgroup1",
				reason:           UnschedulableWorkloadReason,
				messages:         []string{"not enough CPU"},
				detailedMessages: []string{"not enough CPU"},
			},
		},
		{
			name: "Create fit error with messages and detailed messages",
			args: args{
				jobName:          "job1",
				subGroupName:     "subgroup1",
				jobNamespace:     "namespace1",
				reason:           UnschedulableWorkloadReason,
				messages:         []string{"not enough CPU"},
				detailedMessages: []string{"detailed: not enough CPU cores"},
			},
			want: &JobFitErrorBase{
				jobNamespace:     "namespace1",
				jobName:          "job1",
				subGroupName:     "subgroup1",
				reason:           UnschedulableWorkloadReason,
				messages:         []string{"not enough CPU"},
				detailedMessages: []string{"detailed: not enough CPU cores"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewJobFitError(tt.args.jobName, tt.args.subGroupName, tt.args.jobNamespace,
				tt.args.reason, tt.args.messages, tt.args.detailedMessages...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewJobFitError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTopologyFitError(t *testing.T) {
	type args struct {
		jobName         string
		subGroupName    string
		jobNamespace    string
		nodesGroupName  string
		reason          enginev2alpha2.UnschedulableReason
		messages        []string
		detailedReasons []string
	}
	tests := []struct {
		name string
		args args
		want *TopologyFitError
	}{
		{
			name: "Create topology fit error",
			args: args{
				jobName:        "job1",
				subGroupName:   "subgroup1",
				jobNamespace:   "namespace1",
				nodesGroupName: "domain1",
				reason:         UnschedulableWorkloadReason,
				messages:       []string{"not enough CPU"},
			},
			want: &TopologyFitError{
				JobFitErrorBase: JobFitErrorBase{
					jobNamespace:     "namespace1",
					jobName:          "job1",
					subGroupName:     "subgroup1",
					reason:           UnschedulableWorkloadReason,
					messages:         []string{"not enough CPU"},
					detailedMessages: []string{"not enough CPU"},
				},
				nodesGroupName: "domain1",
			},
		},
		{
			name: "Create topology fit error with detailed reasons",
			args: args{
				jobName:         "job1",
				subGroupName:    "subgroup1",
				jobNamespace:    "namespace1",
				nodesGroupName:  "domain1",
				reason:          UnschedulableWorkloadReason,
				messages:        []string{"not enough CPU"},
				detailedReasons: []string{"detailed: not enough CPU cores"},
			},
			want: &TopologyFitError{
				JobFitErrorBase: JobFitErrorBase{
					jobNamespace:     "namespace1",
					jobName:          "job1",
					subGroupName:     "subgroup1",
					reason:           UnschedulableWorkloadReason,
					messages:         []string{"not enough CPU"},
					detailedMessages: []string{"detailed: not enough CPU cores"},
				},
				nodesGroupName: "domain1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewTopologyFitError(tt.args.jobName, tt.args.subGroupName, tt.args.jobNamespace,
				tt.args.nodesGroupName, tt.args.reason, tt.args.messages, tt.args.detailedReasons...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewTopologyFitError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTopologyFitError_DetailedMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *TopologyFitError
		want string
	}{
		{
			name: "Get detailed message with domain",
			err: &TopologyFitError{
				JobFitErrorBase: JobFitErrorBase{
					subGroupName:     "default",
					detailedMessages: []string{"message1", "message2"},
				},
				nodesGroupName: "domain1",
			},
			want: "<domain1>: message1, message2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.DetailedMessage(); got != tt.want {
				t.Errorf("TopologyFitError.DetailedMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTopologyInsufficientResourcesError(t *testing.T) {
	type args struct {
		jobName           string
		subGroupName      string
		namespace         string
		domainID          string
		resourceRequested *resource_info.Resource
		availableResource *resource_info.Resource
	}
	tests := []struct {
		name string
		args args
		want *TopologyFitError
	}{
		{
			name: "Not enough CPU",
			args: args{
				jobName:           "job1",
				subGroupName:      "subgroup1",
				namespace:         "namespace1",
				domainID:          "domain1",
				resourceRequested: BuildResource("1500m", "1M"),
				availableResource: BuildResource("1000m", "2M"),
			},
			want: &TopologyFitError{
				JobFitErrorBase: JobFitErrorBase{
					jobNamespace:     "namespace1",
					jobName:          "job1",
					subGroupName:     "subgroup1",
					reason:           UnschedulableWorkloadReason,
					messages:         []string{"node-group(s) didn't have enough resources: CPU cores"},
					detailedMessages: []string{"domain1 didn't have enough resources: CPU cores, requested: 1.5, available: 1"},
				},
				nodesGroupName: "domain1",
			},
		},
		{
			name: "Not enough memory",
			args: args{
				jobName:           "job1",
				subGroupName:      "subgroup1",
				namespace:         "namespace1",
				domainID:          "domain1",
				resourceRequested: BuildResource("1000m", "3M"),
				availableResource: BuildResource("2000m", "2M"),
			},
			want: &TopologyFitError{
				JobFitErrorBase: JobFitErrorBase{
					jobNamespace:     "namespace1",
					jobName:          "job1",
					subGroupName:     "subgroup1",
					reason:           UnschedulableWorkloadReason,
					messages:         []string{"node-group(s) didn't have enough resources: memory"},
					detailedMessages: []string{"domain1 didn't have enough resources: memory, requested: 0.003, available: 0.002"},
				},
				nodesGroupName: "domain1",
			},
		},
		{
			name: "Not enough whole GPUs",
			args: args{
				jobName:           "job1",
				subGroupName:      "subgroup1",
				namespace:         "namespace1",
				domainID:          "domain1",
				resourceRequested: BuildResourceWithGpu("1000m", "1M", "2", "1"),
				availableResource: BuildResourceWithGpu("2000m", "2M", "1", "110"),
			},
			want: &TopologyFitError{
				JobFitErrorBase: JobFitErrorBase{
					jobNamespace:     "namespace1",
					jobName:          "job1",
					subGroupName:     "subgroup1",
					reason:           UnschedulableWorkloadReason,
					messages:         []string{"node-group(s) didn't have enough resources: GPUs"},
					detailedMessages: []string{"domain1 didn't have enough resource: GPUs, requested: 2, available: 1"},
				},
				nodesGroupName: "domain1",
			},
		},
		{
			name: "Not enough MIG profiles",
			args: args{
				jobName:      "job1",
				subGroupName: "subgroup1",
				namespace:    "namespace1",
				domainID:     "domain1",
				resourceRequested: resource_info.ResourceFromResourceList(
					BuildResourceListWithMig("1000m", "1M", "nvidia.com/mig-1g.5gb", "nvidia.com/mig-1g.5gb"),
				),
				availableResource: resource_info.ResourceFromResourceList(
					BuildResourceListWithMig("2000m", "2M", "nvidia.com/mig-1g.5gb"),
				),
			},
			want: &TopologyFitError{
				JobFitErrorBase: JobFitErrorBase{
					jobNamespace: "namespace1",
					jobName:      "job1",
					subGroupName: "subgroup1",
					reason:       UnschedulableWorkloadReason,
					// MIG resources appear twice: once in MigResources() and once in ScalarResources()
					messages: []string{
						"node-group(s) didn't have enough of mig profile: nvidia.com/mig-1g.5gb",
						"node-group(s) didn't have enough resources: nvidia.com/mig-1g.5gb",
					},
					detailedMessages: []string{
						"domain1 didn't have enough resource: nvidia.com/mig-1g.5gb, requested: 2, available: 1",
						"domain1 didn't have enough resource: nvidia.com/mig-1g.5gb, requested: 2, available: 1",
					},
				},
				nodesGroupName: "domain1",
			},
		},
		{
			name: "Not enough custom scalar resources",
			args: args{
				jobName:      "job1",
				subGroupName: "subgroup1",
				namespace:    "namespace1",
				domainID:     "domain1",
				resourceRequested: resource_info.ResourceFromResourceList(
					v1.ResourceList{
						v1.ResourceCPU:                   resource.MustParse("1000m"),
						v1.ResourceMemory:                resource.MustParse("1M"),
						v1.ResourceName("custom.io/res"): resource.MustParse("5"),
					},
				),
				availableResource: resource_info.ResourceFromResourceList(
					v1.ResourceList{
						v1.ResourceCPU:                   resource.MustParse("2000m"),
						v1.ResourceMemory:                resource.MustParse("2M"),
						v1.ResourceName("custom.io/res"): resource.MustParse("3"),
					},
				),
			},
			want: &TopologyFitError{
				JobFitErrorBase: JobFitErrorBase{
					jobNamespace:     "namespace1",
					jobName:          "job1",
					subGroupName:     "subgroup1",
					reason:           UnschedulableWorkloadReason,
					messages:         []string{"node-group(s) didn't have enough resources: custom.io/res"},
					detailedMessages: []string{"domain1 didn't have enough resource: custom.io/res, requested: 5000, available: 3000"},
				},
				nodesGroupName: "domain1",
			},
		},
		{
			name: "Multiple insufficient resources",
			args: args{
				jobName:           "job1",
				subGroupName:      "subgroup1",
				namespace:         "namespace1",
				domainID:          "domain1",
				resourceRequested: BuildResourceWithGpu("2000m", "3M", "2", "1"),
				availableResource: BuildResourceWithGpu("1000m", "2M", "1", "110"),
			},
			want: &TopologyFitError{
				JobFitErrorBase: JobFitErrorBase{
					jobNamespace: "namespace1",
					jobName:      "job1",
					subGroupName: "subgroup1",
					reason:       UnschedulableWorkloadReason,
					messages: []string{
						"node-group(s) didn't have enough resources: GPUs",
						"node-group(s) didn't have enough resources: CPU cores",
						"node-group(s) didn't have enough resources: memory",
					},
					detailedMessages: []string{
						"domain1 didn't have enough resource: GPUs, requested: 2, available: 1",
						"domain1 didn't have enough resources: CPU cores, requested: 2, available: 1",
						"domain1 didn't have enough resources: memory, requested: 0.003, available: 0.002",
					},
				},
				nodesGroupName: "domain1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewTopologyInsufficientResourcesError(
				tt.args.jobName,
				tt.args.subGroupName,
				tt.args.namespace,
				tt.args.domainID,
				tt.args.resourceRequested,
				tt.args.availableResource,
			)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewTopologyInsufficientResourcesError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewJobFitErrorWithQueueContext(t *testing.T) {
	type args struct {
		jobName      string
		subGroupName string
		jobNamespace string
		reason       enginev2alpha2.UnschedulableReason
		message      string
		context      *enginev2alpha2.UnschedulableExplanationDetails
	}
	tests := []struct {
		name string
		args args
		want *JobFitErrorWithQueueContext
	}{
		{
			name: "Create job fit error with queue context",
			args: args{
				jobName:      "job1",
				subGroupName: "subgroup1",
				jobNamespace: "namespace1",
				reason:       UnschedulableWorkloadReason,
				message:      "queue is full",
				context: &enginev2alpha2.UnschedulableExplanationDetails{
					QueueDetails: &enginev2alpha2.QuotaDetails{
						Name: "queue1",
					},
				},
			},
			want: &JobFitErrorWithQueueContext{
				JobFitErrorBase: JobFitErrorBase{
					jobNamespace:     "namespace1",
					jobName:          "job1",
					subGroupName:     "subgroup1",
					reason:           UnschedulableWorkloadReason,
					messages:         []string{"queue is full"},
					detailedMessages: []string{"queue is full"},
				},
				context: &enginev2alpha2.UnschedulableExplanationDetails{
					QueueDetails: &enginev2alpha2.QuotaDetails{
						Name: "queue1",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewJobFitErrorWithQueueContext(tt.args.jobName, tt.args.subGroupName, tt.args.jobNamespace,
				tt.args.reason, tt.args.message, tt.args.context)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewJobFitErrorWithQueueContext() = %v, want %v", got, tt.want)
			}
		})
	}
}
