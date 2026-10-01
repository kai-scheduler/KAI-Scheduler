// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scenario

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestPodByNodeScenario_VictimsTasksFromNodes(t *testing.T) {
	type fields struct {
		session               *framework.Session
		pendingJob            *podgroup_info.PodGroupInfo
		potentialVictimsTasks []*pod_info.PodInfo
		recordedVictimsJobs   []*podgroup_info.PodGroupInfo
	}
	type args struct {
		tasks     []*pod_info.PodInfo
		nodeNames []string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []*pod_info.PodInfo
	}{
		{
			name: "Single potential job, 2 pods",
			fields: fields{
				session: &framework.Session{
					ClusterInfo: &api.ClusterInfo{PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
						"pg1": podgroup_info.NewPodGroupInfo("pg1",
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "name1",
									Namespace: "n1",
									Annotations: map[string]string{
										commonconstants.PodGroupAnnotationForPod: "pg1",
									},
								},
								Spec: v1.PodSpec{
									NodeName: "node1",
								},
								Status: v1.PodStatus{
									Phase: v1.PodRunning,
								},
							}, resource_info.NewResourceVectorMap()),
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "name2",
									Namespace: "n1",
									Annotations: map[string]string{
										commonconstants.PodGroupAnnotationForPod: "pg1",
									},
								},
								Spec: v1.PodSpec{
									NodeName: "node1",
								},
								Status: v1.PodStatus{
									Phase: v1.PodRunning,
								},
							}, resource_info.NewResourceVectorMap()),
						),
						"pg2": podgroup_info.NewPodGroupInfo("pg2", pod_info.NewTaskInfo(&v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "name3",
								Namespace: "n1",
								Annotations: map[string]string{
									commonconstants.PodGroupAnnotationForPod: "pg2",
								},
							},
							Spec: v1.PodSpec{
								NodeName: "node1",
							},
							Status: v1.PodStatus{
								Phase: v1.PodRunning,
							},
						}, resource_info.NewResourceVectorMap())),
					}},
				},
				pendingJob: podgroup_info.NewPodGroupInfo("123"),
				potentialVictimsTasks: []*pod_info.PodInfo{
					pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "name1",
							Namespace: "n1",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "pg1",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "node1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					}, resource_info.NewResourceVectorMap()),
					pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "name2",
							Namespace: "n1",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "pg1",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "node1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					}, resource_info.NewResourceVectorMap()),
				},
			},
			args: args{
				tasks:     make([]*pod_info.PodInfo, 0),
				nodeNames: []string{"node1"},
			},
			want: []*pod_info.PodInfo{
				pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name1",
						Namespace: "n1",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg1",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "node1",
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				}, resource_info.NewResourceVectorMap()),
				pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name2",
						Namespace: "n1",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg1",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "node1",
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				}, resource_info.NewResourceVectorMap()),
			},
		},
		{
			name: "No pods on node",
			fields: fields{
				session: &framework.Session{
					ClusterInfo: &api.ClusterInfo{PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
						"pg1": podgroup_info.NewPodGroupInfo("pg1",
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "name1",
									Namespace: "n1",
									Annotations: map[string]string{
										commonconstants.PodGroupAnnotationForPod: "pg1",
									},
								},
								Spec: v1.PodSpec{
									NodeName: "node1",
								},
								Status: v1.PodStatus{
									Phase: v1.PodRunning,
								},
							}, resource_info.NewResourceVectorMap()),
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "name2",
									Namespace: "n1",
									Annotations: map[string]string{
										commonconstants.PodGroupAnnotationForPod: "pg1",
									},
								},
								Spec: v1.PodSpec{
									NodeName: "node1",
								},
								Status: v1.PodStatus{
									Phase: v1.PodRunning,
								},
							}, resource_info.NewResourceVectorMap()),
						),
						"pg2": podgroup_info.NewPodGroupInfo("pg2", pod_info.NewTaskInfo(&v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "name3",
								Namespace: "n1",
								Annotations: map[string]string{
									commonconstants.PodGroupAnnotationForPod: "pg2",
								},
							},
							Spec: v1.PodSpec{
								NodeName: "node1",
							},
							Status: v1.PodStatus{
								Phase: v1.PodRunning,
							},
						}, resource_info.NewResourceVectorMap())),
					}},
				},
				pendingJob: podgroup_info.NewPodGroupInfo("123"),
				potentialVictimsTasks: []*pod_info.PodInfo{
					pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "name1",
							Namespace: "n1",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "pg1",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "node1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					}, resource_info.NewResourceVectorMap()),
					pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "name2",
							Namespace: "n1",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "pg1",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "node1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					}, resource_info.NewResourceVectorMap()),
				},
			},
			args: args{
				tasks:     make([]*pod_info.PodInfo, 0),
				nodeNames: []string{"node2"},
			},
			want: nil,
		},
		{
			name: "Single potential job, return pods from same job on different nodes",
			fields: fields{
				session: &framework.Session{
					ClusterInfo: &api.ClusterInfo{PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
						"pg1": podgroup_info.NewPodGroupInfo("pg1",
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "name1",
									Namespace: "n1",
									Annotations: map[string]string{
										commonconstants.PodGroupAnnotationForPod: "pg1",
									},
								},
								Spec: v1.PodSpec{
									NodeName: "node1",
								},
								Status: v1.PodStatus{
									Phase: v1.PodRunning,
								},
							}, resource_info.NewResourceVectorMap()),
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "name2",
									Namespace: "n1",
									Annotations: map[string]string{
										commonconstants.PodGroupAnnotationForPod: "pg1",
									},
								},
								Spec: v1.PodSpec{
									NodeName: "node2",
								},
								Status: v1.PodStatus{
									Phase: v1.PodRunning,
								},
							}, resource_info.NewResourceVectorMap()),
						),
						"pg2": podgroup_info.NewPodGroupInfo("pg2", pod_info.NewTaskInfo(&v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "name3",
								Namespace: "n1",
								Annotations: map[string]string{
									commonconstants.PodGroupAnnotationForPod: "pg2",
								},
							},
							Spec: v1.PodSpec{
								NodeName: "node1",
							},
							Status: v1.PodStatus{
								Phase: v1.PodRunning,
							},
						}, resource_info.NewResourceVectorMap())),
					}},
				},
				pendingJob: podgroup_info.NewPodGroupInfo("123"),
				potentialVictimsTasks: []*pod_info.PodInfo{
					pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "name1",
							Namespace: "n1",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "pg1",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "node1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					}, resource_info.NewResourceVectorMap()),
					pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "name2",
							Namespace: "n1",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "pg1",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "node2",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					}, resource_info.NewResourceVectorMap()),
				},
			},
			args: args{
				tasks:     make([]*pod_info.PodInfo, 0),
				nodeNames: []string{"node1"},
			},
			want: []*pod_info.PodInfo{
				pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name1",
						Namespace: "n1",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg1",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "node1",
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				}, resource_info.NewResourceVectorMap()),
				pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name2",
						Namespace: "n1",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg1",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "node2",
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				}, resource_info.NewResourceVectorMap()),
			},
		},
		{
			name: "Single potential job, return pods from same job on different nodes - get tasks from AddPotentialVictimsTasks",
			fields: fields{
				session: &framework.Session{
					ClusterInfo: &api.ClusterInfo{PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
						"pg1": podgroup_info.NewPodGroupInfo("pg1",
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "name1",
									Namespace: "n1",
									Annotations: map[string]string{
										commonconstants.PodGroupAnnotationForPod: "pg1",
									},
								},
								Spec: v1.PodSpec{
									NodeName: "node1",
								},
								Status: v1.PodStatus{
									Phase: v1.PodRunning,
								},
							}, resource_info.NewResourceVectorMap()),
							pod_info.NewTaskInfo(&v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "name2",
									Namespace: "n1",
									Annotations: map[string]string{
										commonconstants.PodGroupAnnotationForPod: "pg1",
									},
								},
								Spec: v1.PodSpec{
									NodeName: "node2",
								},
								Status: v1.PodStatus{
									Phase: v1.PodRunning,
								},
							}, resource_info.NewResourceVectorMap()),
						),
						"pg2": podgroup_info.NewPodGroupInfo("pg2", pod_info.NewTaskInfo(&v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "name3",
								Namespace: "n1",
								Annotations: map[string]string{
									commonconstants.PodGroupAnnotationForPod: "pg2",
								},
							},
							Spec: v1.PodSpec{
								NodeName: "node1",
							},
							Status: v1.PodStatus{
								Phase: v1.PodRunning,
							},
						}, resource_info.NewResourceVectorMap())),
					}},
				},
				pendingJob: podgroup_info.NewPodGroupInfo("123"),
				potentialVictimsTasks: []*pod_info.PodInfo{
					pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "name1",
							Namespace: "n1",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "pg1",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "node1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					}, resource_info.NewResourceVectorMap()),
				},
			},
			args: args{
				tasks: []*pod_info.PodInfo{
					pod_info.NewTaskInfo(&v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "name2",
							Namespace: "n1",
							Annotations: map[string]string{
								commonconstants.PodGroupAnnotationForPod: "pg1",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "node2",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
						},
					}, resource_info.NewResourceVectorMap()),
				},
				nodeNames: []string{"node1"},
			},
			want: []*pod_info.PodInfo{
				pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name1",
						Namespace: "n1",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg1",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "node1",
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				}, resource_info.NewResourceVectorMap()),
				pod_info.NewTaskInfo(&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "name2",
						Namespace: "n1",
						Annotations: map[string]string{
							commonconstants.PodGroupAnnotationForPod: "pg1",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "node2",
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				}, resource_info.NewResourceVectorMap()),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pendingTasks := []*pod_info.PodInfo{}
			for _, task := range tt.fields.pendingJob.GetAllPodsMap() {
				pendingTasks = append(pendingTasks, task)
			}
			bns := NewByNodeScenario(tt.fields.session, tt.fields.pendingJob, pendingTasks, tt.fields.potentialVictimsTasks,
				tt.fields.recordedVictimsJobs)
			if tt.args.tasks != nil {
				bns.AddPotentialVictimsTasks(tt.args.tasks)
			}
			if got := bns.VictimsTasksFromNodes(tt.args.nodeNames); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("VictimsTasksFromNodes() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A scenario already holds its recorded victims, so they are not returned again as a node's
// potential victims, even when the same job also has potential victims on that node.
func TestPodByNodeScenario_VictimsTasksFromNodesLeavesOutRecordedVictims(t *testing.T) {
	task := func(name, nodeName string) *pod_info.PodInfo {
		return pod_info.NewTaskInfo(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   "n1",
				UID:         types.UID(name),
				Annotations: map[string]string{commonconstants.PodGroupAnnotationForPod: "pg1"},
			},
			Spec:   v1.PodSpec{NodeName: nodeName},
			Status: v1.PodStatus{Phase: v1.PodRunning},
		}, resource_info.NewResourceVectorMap())
	}
	recorded, potential, sameNodePotential := task("recorded", "node1"), task("potential", "node2"),
		task("same-node-potential", "node1")
	job := podgroup_info.NewPodGroupInfo("pg1", recorded, potential, sameNodePotential)
	session := &framework.Session{ClusterInfo: &api.ClusterInfo{
		PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{"pg1": job},
	}}

	bns := NewByNodeScenario(session, podgroup_info.NewPodGroupInfo("pending"), nil,
		[]*pod_info.PodInfo{potential, sameNodePotential},
		[]*podgroup_info.PodGroupInfo{job.CloneWithTasks([]*pod_info.PodInfo{recorded})})

	want := []*pod_info.PodInfo{potential, sameNodePotential}
	for _, nodeName := range []string{"node1", "node2"} {
		if got := bns.VictimsTasksFromNodes([]string{nodeName}); !reflect.DeepEqual(got, want) {
			t.Errorf("VictimsTasksFromNodes(%s) = %v, want %v", nodeName, podNames(got), podNames(want))
		}
	}
	if got := bns.VictimsTasksFromNodes([]string{"node3"}); len(got) != 0 {
		t.Errorf("VictimsTasksFromNodes(node3) = %v, want none", podNames(got))
	}
}

func podNames(tasks []*pod_info.PodInfo) []string {
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, task.Name)
	}
	return names
}
