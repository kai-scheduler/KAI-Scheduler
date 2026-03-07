// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package kubeflow

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/framework"
)

func TestTaskOrderFn(t *testing.T) {
	type args struct {
		l interface{}
		r interface{}
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			name: "non kubeflow pods",
			args: args{
				l: &pod_info.PodInfo{
					Pod: &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{},
						},
					},
				},
				r: &pod_info.PodInfo{
					Pod: &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{},
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "one kubeflow pod, one non-kubeflow",
			args: args{
				l: &pod_info.PodInfo{
					Pod: &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"training.kubeflow.org/job-name": "A",
								"training.kubeflow.org/job-role": "worker",
							},
						},
					},
				},
				r: &pod_info.PodInfo{
					Pod: &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{},
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "same kubeflow job, one launcher one worker",
			args: args{
				l: &pod_info.PodInfo{
					Pod: &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"training.kubeflow.org/job-name": "A",
								"training.kubeflow.org/job-role": "launcher",
							},
						},
					},
				},
				r: &pod_info.PodInfo{
					Pod: &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"training.kubeflow.org/job-name": "A",
								"training.kubeflow.org/job-role": "worker",
							},
						},
					},
				},
			},
			want: -1,
		},
		{
			name: "same kubeflow job, one master one worker",
			args: args{
				l: &pod_info.PodInfo{
					Pod: &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"training.kubeflow.org/job-name": "A",
								"training.kubeflow.org/job-role": "worker",
							},
						},
					},
				},
				r: &pod_info.PodInfo{
					Pod: &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"training.kubeflow.org/job-name": "A",
								"training.kubeflow.org/job-role": "master",
							},
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "same kubeflow job, two workers",
			args: args{
				l: &pod_info.PodInfo{
					Pod: &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"training.kubeflow.org/job-name": "A",
								"training.kubeflow.org/job-role": "worker",
							},
						},
					},
				},
				r: &pod_info.PodInfo{
					Pod: &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"training.kubeflow.org/job-name": "A",
								"training.kubeflow.org/job-role": "worker",
							},
						},
					},
				},
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TaskOrderFn(tt.args.l, tt.args.r); got != tt.want {
				t.Errorf("TaskOrderFn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_kubeflowPlugin_OnSessionOpen(t *testing.T) {
	type fields struct {
		pluginArguments map[string]string
	}
	type args struct {
		ssn *framework.Session
	}
	tests := []struct {
		name           string
		fields         fields
		args           args
		taskOrderSetup map[string]common_info.CompareFn
	}{
		{
			name: "Sets the kubeflow pod order func",
			fields: fields{
				pluginArguments: map[string]string{},
			},
			args: args{
				ssn: &framework.Session{},
			},
			taskOrderSetup: map[string]common_info.CompareFn{
				"kubeflow": TaskOrderFn,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pp := &kubeflowPlugin{}
			pp.OnSessionOpen(tt.args.ssn)
		})
	}
}
