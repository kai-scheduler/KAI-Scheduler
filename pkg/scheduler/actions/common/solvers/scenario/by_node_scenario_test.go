// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scenario

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

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

func newTestTask(name, namespace, nodeName, uid, pgUID string, vectorMap *resource_info.ResourceVectorMap) *pod_info.PodInfo {
	return pod_info.NewTaskInfo(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       k8stypes.UID(uid),
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: pgUID,
			},
		},
		Spec: v1.PodSpec{NodeName: nodeName},
		Status: v1.PodStatus{Phase: v1.PodRunning},
	}, vectorMap)
}

func newTestJob(uid string, tasks ...*pod_info.PodInfo) *podgroup_info.PodGroupInfo {
	return podgroup_info.NewPodGroupInfo(common_info.PodGroupID(uid), tasks...)
}

func buildFingerprintSession(vm *resource_info.ResourceVectorMap) (
	*framework.Session, *podgroup_info.PodGroupInfo, *podgroup_info.PodGroupInfo,
	*pod_info.PodInfo, *pod_info.PodInfo, *pod_info.PodInfo, *pod_info.PodInfo,
) {
	taskA1 := newTestTask("a1", "ns", "node1", "uid-a1", "victimA", vm)
	taskA2 := newTestTask("a2", "ns", "node1", "uid-a2", "victimA", vm)
	taskB1 := newTestTask("b1", "ns", "node2", "uid-b1", "victimB", vm)
	taskC1 := newTestTask("c1", "ns", "node3", "uid-c1", "victimC", vm)

	jobA := newTestJob("victimA", taskA1, taskA2)
	jobB := newTestJob("victimB", taskB1)
	jobC := newTestJob("victimC", taskC1)

	preemptor := newTestJob("preemptor-uid")

	ssn := &framework.Session{ClusterInfo: &api.ClusterInfo{
		PodGroupInfos: map[common_info.PodGroupID]*podgroup_info.PodGroupInfo{
			"victimA": jobA,
			"victimB": jobB,
			"victimC": jobC,
		},
	}}
	return ssn, preemptor, newTestJob("preemptor-uid-2"), taskA1, taskA2, taskB1, taskC1
}

func TestByNodeScenario_Fingerprint(t *testing.T) {
	vm := resource_info.NewResourceVectorMap()

	t.Run("same victims different insertion order → same fingerprint", func(t *testing.T) {
		ssn, preemptor, _, taskA1, taskA2, taskB1, _ := buildFingerprintSession(vm)
		s1 := NewByNodeScenario(ssn, preemptor, nil, []*pod_info.PodInfo{taskA1, taskA2, taskB1}, nil)
		s2 := NewByNodeScenario(ssn, preemptor, nil, []*pod_info.PodInfo{taskB1, taskA1, taskA2}, nil)
		if s1.Fingerprint() != s2.Fingerprint() {
			t.Errorf("expected same fingerprint for same victim set, got %d vs %d", s1.Fingerprint(), s2.Fingerprint())
		}
	})

	t.Run("different victim sets → different fingerprints", func(t *testing.T) {
		ssn, preemptor, _, taskA1, _, taskB1, taskC1 := buildFingerprintSession(vm)
		s1 := NewByNodeScenario(ssn, preemptor, nil, []*pod_info.PodInfo{taskA1, taskB1}, nil)
		s2 := NewByNodeScenario(ssn, preemptor, nil, []*pod_info.PodInfo{taskA1, taskC1}, nil)
		if s1.Fingerprint() == s2.Fingerprint() {
			t.Errorf("expected different fingerprints for different victim sets, got %d", s1.Fingerprint())
		}
	})

	t.Run("same victim job different task count → different fingerprints", func(t *testing.T) {
		ssn, preemptor, _, taskA1, taskA2, _, _ := buildFingerprintSession(vm)
		s1 := NewByNodeScenario(ssn, preemptor, nil, []*pod_info.PodInfo{taskA1}, nil)
		s2 := NewByNodeScenario(ssn, preemptor, nil, []*pod_info.PodInfo{taskA1, taskA2}, nil)
		if s1.Fingerprint() == s2.Fingerprint() {
			t.Errorf("expected different fingerprints for different task counts, got %d", s1.Fingerprint())
		}
	})

	t.Run("empty scenario fingerprint is stable", func(t *testing.T) {
		ssn, preemptor, _, _, _, _, _ := buildFingerprintSession(vm)
		s1 := NewByNodeScenario(ssn, preemptor, nil, nil, nil)
		s2 := NewByNodeScenario(ssn, preemptor, nil, nil, nil)
		if s1.Fingerprint() != s2.Fingerprint() {
			t.Errorf("expected stable fingerprint for empty scenario, got %d vs %d", s1.Fingerprint(), s2.Fingerprint())
		}
	})

	t.Run("different preemptors → different fingerprints", func(t *testing.T) {
		ssn, preemptor, preemptor2, taskA1, _, _, _ := buildFingerprintSession(vm)
		s1 := NewByNodeScenario(ssn, preemptor, nil, []*pod_info.PodInfo{taskA1}, nil)
		s2 := NewByNodeScenario(ssn, preemptor2, nil, []*pod_info.PodInfo{taskA1}, nil)
		if s1.Fingerprint() == s2.Fingerprint() {
			t.Errorf("expected different fingerprints for different preemptors, got %d", s1.Fingerprint())
		}
	})

	t.Run("same job different tasks (different node assignment) → different fingerprints", func(t *testing.T) {
		// taskA1 and taskA2 belong to the same job (victimA) but are on different nodes.
		// Evicting only taskA1 vs only taskA2 are not equivalent scenarios.
		ssn, preemptor, _, taskA1, taskA2, _, _ := buildFingerprintSession(vm)
		s1 := NewByNodeScenario(ssn, preemptor, nil, []*pod_info.PodInfo{taskA1}, nil)
		s2 := NewByNodeScenario(ssn, preemptor, nil, []*pod_info.PodInfo{taskA2}, nil)
		if s1.Fingerprint() == s2.Fingerprint() {
			t.Errorf("expected different fingerprints for different task selections from the same job, got %d", s1.Fingerprint())
		}
	})
}
