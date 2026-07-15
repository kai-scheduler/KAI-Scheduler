// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package elastic

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/jobs_fake"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/test_utils/tasks_fake"
)

func TestJobOrderFn(t *testing.T) {
	running := &tasks_fake.TestTaskBasic{State: pod_status.Running}

	tests := []struct {
		name string
		l    *jobs_fake.TestJobBasic
		r    *jobs_fake.TestJobBasic
		want int
	}{
		{
			name: "no pods",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(0),
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(0),
			},
			want: 0,
		},
		{
			name: "running single pod",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			want: 0,
		},
		{
			name: "allocated pod counts as allocated",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Allocated},
				},
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			want: 0,
		},
		{
			name: "bound pod counts as allocated",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Bound},
				},
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			want: 0,
		},
		{
			name: "releasing pod doesn't count as allocated",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks: []*tasks_fake.TestTaskBasic{
					{State: pod_status.Releasing},
				},
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			want: -1,
		},
		{
			name: "pod group with min pods against pod group with no pods",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
			},
			want: 1,
		},
		{
			name: "pod group with less then min pods against pod group with min pods",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(2),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(2),
				Tasks:           []*tasks_fake.TestTaskBasic{running, running},
			},
			want: -1,
		},
		{
			name: "pod group with less then min pods against pod group with more than min pods",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(2),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(2),
				Tasks:           []*tasks_fake.TestTaskBasic{running, running, running},
			},
			want: -1,
		},
		{
			name: "pod group with min pods against pod group with more than min pods",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running, running},
			},
			want: -1,
		},
		{
			name: "pod group with min pods against pod group with less than min pods",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(3),
				Tasks:           []*tasks_fake.TestTaskBasic{running, running},
			},
			want: 1,
		},
		{
			name: "pod group with more than min pods against pod group with min pods",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running, running},
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			want: 1,
		},
		{
			name: "pod group with more than min pods against pod group with less than min pods",
			l: &jobs_fake.TestJobBasic{
				Name:            "job-l",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running, running},
			},
			r: &jobs_fake.TestJobBasic{
				Name:            "job-r",
				RootSubGroupSet: jobs_fake.DefaultSubGroup(1),
				Tasks:           []*tasks_fake.TestTaskBasic{running},
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vectorMap := resource_info.NewResourceVectorMap()
			jobsMap, _, _ := jobs_fake.BuildJobsAndTasksMaps(
				[]*jobs_fake.TestJobBasic{tt.l, tt.r}, vectorMap)
			lJob := jobsMap[common_info.PodGroupID(tt.l.Name)]
			rJob := jobsMap[common_info.PodGroupID(tt.r.Name)]
			if got := JobOrderFn(lJob, rJob); got != tt.want {
				t.Errorf("JobOrderFn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_elasticPlugin_OnSessionOpen(t *testing.T) {
	type fields struct {
		pluginArguments map[string]string
	}
	type args struct {
		ssn *framework.Session
	}
	tests := []struct {
		name          string
		fields        fields
		args          args
		jobOrderSetup map[string]common_info.CompareFn
	}{
		{
			name: "Sets the elastic job order func",
			fields: fields{
				pluginArguments: map[string]string{},
			},
			args: args{
				ssn: &framework.Session{},
			},
			jobOrderSetup: map[string]common_info.CompareFn{
				"elastic": JobOrderFn,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pp := &elasticPlugin{}
			pp.OnSessionOpen(tt.args.ssn)
		})
	}
}
