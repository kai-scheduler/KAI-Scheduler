// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_info

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

const (
	oneCoreMilli   = 1000.0
	oneGiBytes     = 1024 * 1024 * 1024.0
	deferredReason = v1.PodReasonDeferred
)

func deferredCondition(reason string) *v1.PodCondition {
	return &v1.PodCondition{Type: v1.PodResizePending, Reason: reason, Status: v1.ConditionTrue}
}

// resizePod builds a pod whose regular containers request specReqs, whose container
// statuses report statusReqs as actually-granted, and (optionally) carries condition.
func resizePod(specReqs, statusReqs []v1.ResourceList, condition *v1.PodCondition) *v1.Pod {
	pod := &v1.Pod{}
	for _, req := range specReqs {
		pod.Spec.Containers = append(pod.Spec.Containers, v1.Container{
			Resources: v1.ResourceRequirements{Requests: req},
		})
	}
	for _, req := range statusReqs {
		status := v1.ContainerStatus{}
		if req != nil {
			status.Resources = &v1.ResourceRequirements{Requests: req}
		}
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, status)
	}
	if condition != nil {
		pod.Status.Conditions = []v1.PodCondition{*condition}
	}
	return pod
}

func rl(cpu, memory string) v1.ResourceList {
	list := v1.ResourceList{}
	if cpu != "" {
		list[v1.ResourceCPU] = resource.MustParse(cpu)
	}
	if memory != "" {
		list[v1.ResourceMemory] = resource.MustParse(memory)
	}
	return list
}

// withGPU adds an explicit nvidia.com/gpu entry to an existing ResourceList.
func withGPU(list v1.ResourceList, gpu string) v1.ResourceList {
	list["nvidia.com/gpu"] = resource.MustParse(gpu)
	return list
}

func TestIsResizeDeferred(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want bool
	}{
		{name: "nil pod", pod: nil, want: false},
		{name: "no conditions", pod: &v1.Pod{}, want: false},
		{
			name: "resize pending, deferred",
			pod:  resizePod(nil, nil, deferredCondition(v1.PodReasonDeferred)),
			want: true,
		},
		{
			name: "resize pending, infeasible is not deferred",
			pod:  resizePod(nil, nil, deferredCondition(v1.PodReasonInfeasible)),
			want: false,
		},
		{
			name: "resize in progress is not pending",
			pod: &v1.Pod{Status: v1.PodStatus{Conditions: []v1.PodCondition{
				{Type: v1.PodResizeInProgress, Status: v1.ConditionTrue},
			}}},
			want: false,
		},
		{
			name: "unrelated condition",
			pod: &v1.Pod{Status: v1.PodStatus{Conditions: []v1.PodCondition{
				{Type: v1.PodReady, Status: v1.ConditionTrue},
			}}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsResizeDeferred(tt.pod); got != tt.want {
				t.Errorf("IsResizeDeferred() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResizeDeferredDelta(t *testing.T) {
	tests := []struct {
		name    string
		pod     *v1.Pod
		wantNil bool
		wantCPU float64 // expected delta milli-cpu (0 if none)
		wantMem float64 // expected delta bytes (0 if none)
	}{
		{
			name:    "not deferred returns nil even when spec exceeds status",
			pod:     resizePod([]v1.ResourceList{rl("2", "")}, []v1.ResourceList{rl("1", "")}, nil),
			wantNil: true,
		},
		{
			name:    "infeasible returns nil",
			pod:     resizePod([]v1.ResourceList{rl("2", "")}, []v1.ResourceList{rl("1", "")}, deferredCondition(v1.PodReasonInfeasible)),
			wantNil: true,
		},
		{
			name:    "cpu-only growth",
			pod:     resizePod([]v1.ResourceList{rl("2", "")}, []v1.ResourceList{rl("1", "")}, deferredCondition(deferredReason)),
			wantCPU: oneCoreMilli,
		},
		{
			name:    "memory-only growth",
			pod:     resizePod([]v1.ResourceList{rl("", "2Gi")}, []v1.ResourceList{rl("", "1Gi")}, deferredCondition(deferredReason)),
			wantMem: oneGiBytes,
		},
		{
			name:    "cpu and memory growth",
			pod:     resizePod([]v1.ResourceList{rl("2", "2Gi")}, []v1.ResourceList{rl("1", "1Gi")}, deferredCondition(deferredReason)),
			wantCPU: oneCoreMilli,
			wantMem: oneGiBytes,
		},
		{
			name: "multi-container growth sums",
			pod: resizePod(
				[]v1.ResourceList{rl("2", ""), rl("2", "")},
				[]v1.ResourceList{rl("1", ""), rl("1", "")},
				deferredCondition(deferredReason)),
			wantCPU: 2 * oneCoreMilli,
		},
		{
			name:    "actual equals desired yields empty delta",
			pod:     resizePod([]v1.ResourceList{rl("2", "2Gi")}, []v1.ResourceList{rl("2", "2Gi")}, deferredCondition(deferredReason)),
			wantNil: true,
		},
		{
			name:    "resize that lowers requests yields empty delta",
			pod:     resizePod([]v1.ResourceList{rl("1", "")}, []v1.ResourceList{rl("2", "")}, deferredCondition(deferredReason)),
			wantNil: true,
		},
		{
			name:    "missing container status treats actual as zero",
			pod:     resizePod([]v1.ResourceList{rl("2", "")}, []v1.ResourceList{nil}, deferredCondition(deferredReason)),
			wantCPU: 2 * oneCoreMilli,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delta := ResizeDeferredDelta(tt.pod)
			if tt.wantNil {
				if delta != nil {
					t.Fatalf("ResizeDeferredDelta() = %v, want nil", delta)
				}
				return
			}
			if delta == nil {
				t.Fatalf("ResizeDeferredDelta() = nil, want non-nil")
			}
			if got := delta.Get(v1.ResourceCPU); got != tt.wantCPU {
				t.Errorf("delta cpu = %v, want %v", got, tt.wantCPU)
			}
			if got := delta.Get(v1.ResourceMemory); got != tt.wantMem {
				t.Errorf("delta memory = %v, want %v", got, tt.wantMem)
			}
		})
	}
}

// A resize that grows cpu while carrying an explicit "nvidia.com/gpu: 0" request must not
// surface a GPU in the delta. The delta is taken over raw ResourceLists so the zero-valued GPU
// cancels (0 - 0) and is dropped; converting it through ResourceRequirements first would instead
// read "nvidia.com/gpu: 0" as one fractional device (count defaults to 1) - a spurious GPU delta.
func TestResizeDeferredDeltaDropsZeroGPU(t *testing.T) {
	pod := resizePod(
		[]v1.ResourceList{withGPU(rl("2", ""), "0")},
		[]v1.ResourceList{withGPU(rl("1", ""), "0")},
		deferredCondition(deferredReason),
	)

	delta := ResizeDeferredDelta(pod)
	if delta == nil {
		t.Fatalf("ResizeDeferredDelta() = nil, want non-nil (cpu grew by 1 core)")
	}
	if got := delta.Get(v1.ResourceCPU); got != oneCoreMilli {
		t.Errorf("delta cpu = %v, want %v", got, oneCoreMilli)
	}
	if got := delta.GPUs(); got != 0 {
		t.Errorf("delta GPUs() = %v, want 0 (zero-valued gpu request must not appear)", got)
	}
	if got := delta.GetNumOfGpuDevices(); got != 0 {
		t.Errorf("delta gpu devices = %v, want 0 (must not be misread as a fractional device)", got)
	}
}

// A deferred-resize pod must be accounted at its actually-granted (status) size, not its larger
// desired (spec) request, so the node reflects the pod's true footprint and the growth is charged
// separately as a pending demand. Charge + delta must equal the desired request.
func TestRegularContainerRequestsChargesActualForDeferredResize(t *testing.T) {
	tests := []struct {
		name    string
		pod     *v1.Pod
		wantCPU float64 // milli-cpu
		wantMem float64 // bytes
	}{
		{
			name:    "deferred resize charged at actual, not desired",
			pod:     resizePod([]v1.ResourceList{rl("2", "2Gi")}, []v1.ResourceList{rl("1", "1Gi")}, deferredCondition(deferredReason)),
			wantCPU: oneCoreMilli,
			wantMem: oneGiBytes,
		},
		{
			name:    "no deferred condition charged at spec",
			pod:     resizePod([]v1.ResourceList{rl("2", "2Gi")}, []v1.ResourceList{rl("1", "1Gi")}, nil),
			wantCPU: 2 * oneCoreMilli,
			wantMem: 2 * oneGiBytes,
		},
		{
			name:    "infeasible resize charged at spec",
			pod:     resizePod([]v1.ResourceList{rl("2", "")}, []v1.ResourceList{rl("1", "")}, deferredCondition(v1.PodReasonInfeasible)),
			wantCPU: 2 * oneCoreMilli,
		},
		{
			name:    "multi-container deferred resize sums actuals",
			pod:     resizePod([]v1.ResourceList{rl("2", ""), rl("2", "")}, []v1.ResourceList{rl("1", ""), rl("1", "")}, deferredCondition(deferredReason)),
			wantCPU: 2 * oneCoreMilli,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			charged := getPodResourceWithoutInitContainers(tt.pod)
			if got := charged.Get(v1.ResourceCPU); got != tt.wantCPU {
				t.Errorf("charged cpu = %v, want %v", got, tt.wantCPU)
			}
			if got := charged.Get(v1.ResourceMemory); got != tt.wantMem {
				t.Errorf("charged memory = %v, want %v", got, tt.wantMem)
			}
			// Invariant: charge + delta == desired (spec).
			if delta := ResizeDeferredDelta(tt.pod); delta != nil {
				spec := sumContainerRequests(tt.pod)
				wantSpecCPU := float64(spec.Cpu().MilliValue())
				if sum := charged.Get(v1.ResourceCPU) + delta.Get(v1.ResourceCPU); sum != wantSpecCPU {
					t.Errorf("charge+delta cpu = %v, want desired %v", sum, wantSpecCPU)
				}
			}
		})
	}
}

// resizingTaskOnNode builds a PodInfo for a running pod with a cpu-growing deferred resize (spec 2
// cores, status 1 core) placed on nodeName.
func resizingTaskOnNode(nodeName string) *PodInfo {
	pod := resizePod([]v1.ResourceList{rl("2", "")}, []v1.ResourceList{rl("1", "")}, deferredCondition(deferredReason))
	pod.Spec.NodeName = nodeName
	task := NewTaskInfo(pod, resource_info.NewResourceVectorMap())
	task.Job = "resizer-job"
	task.UID = "resizer-uid"
	task.Name = "resizer"
	task.Namespace = "ns"
	return task
}

func TestNewResizeReservationTask(t *testing.T) {
	res := NewResizeReservationTask(resizingTaskOnNode("node0"), resource_info.NewResourceVectorMap())
	if res == nil {
		t.Fatal("NewResizeReservationTask() = nil, want a reservation task")
	}
	if !res.IsResizeReservation {
		t.Error("IsResizeReservation = false, want true")
	}
	if res.Status != pod_status.Pending {
		t.Errorf("status = %v, want Pending", res.Status)
	}
	if res.NodeName != "" {
		t.Errorf("NodeName = %q, want empty (reservation is unassigned/pending)", res.NodeName)
	}
	if string(res.Job) != "resizer-job-resize-reservation" {
		t.Errorf("Job = %q, want resizer-job-resize-reservation", res.Job)
	}
	if res.Name != "resizer-resize-reservation" || res.Namespace != "ns" {
		t.Errorf("name/ns = %q/%q, want resizer-resize-reservation/ns", res.Name, res.Namespace)
	}
	// Requests the resize delta (2 - 1 = 1 core).
	if got := res.Pod.Spec.Containers[0].Resources.Requests.Cpu().MilliValue(); got != int64(oneCoreMilli) {
		t.Errorf("reservation cpu request = %vm, want %vm (the delta)", got, int64(oneCoreMilli))
	}
	// Pinned to the resizing pod's node via required hostname affinity.
	terms := res.Pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	expr := terms[0].MatchExpressions[0]
	if expr.Key != v1.LabelHostname || len(expr.Values) != 1 || expr.Values[0] != "node0" {
		t.Errorf("node affinity = %v In %v, want %s In [node0]", expr.Key, expr.Values, v1.LabelHostname)
	}
}

func TestNewResizeReservationTaskReturnsNil(t *testing.T) {
	// No deferred resize -> no reservation.
	plain := NewTaskInfo(resizePod([]v1.ResourceList{rl("2", "")}, nil, nil), resource_info.NewResourceVectorMap())
	plain.NodeName = "node0"
	if res := NewResizeReservationTask(plain, resource_info.NewResourceVectorMap()); res != nil {
		t.Errorf("NewResizeReservationTask(non-resizing) = %v, want nil", res)
	}
	// Deferred resize but unassigned (no node) -> no reservation.
	unplaced := resizingTaskOnNode("")
	if res := NewResizeReservationTask(unplaced, resource_info.NewResourceVectorMap()); res != nil {
		t.Errorf("NewResizeReservationTask(unassigned) = %v, want nil", res)
	}
}
