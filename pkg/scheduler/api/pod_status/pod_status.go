/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_status

// PodStatus defines the status of a task/pod.
type PodStatus int

const (
	// Pending means the task is pending in the apiserver.
	Pending PodStatus = 1 << iota

	// Gated means the task is gated in the apiserver.
	Gated

	// Allocated means the scheduler assigns a host to it.
	Allocated

	// Pipelined means the scheduler assigns a host to wait for releasing resource.
	Pipelined

	// Binding means the scheduler send Bind request to apiserver.
	Binding

	// Bound means the task/Pod bounds to a host.
	Bound

	// Running means a task is running on the host.
	Running

	// Releasing means a task/pod is deleted.
	Releasing

	// Succeeded means that all containers in the pod have voluntarily terminated
	// with a container exit code of 0, and the system is not going to restart any of these containers.
	Succeeded

	// Failed means that all containers in the pod have terminated, and at least one container has
	// terminated in a failure (exited with a non-zero exit code or was stopped by the system).
	Failed

	// Unknown means the status of task/pod is unknown to the scheduler.
	Unknown

	Deleted
)

// Status group constants using bitwise OR
const (
	activeUsedStatuses      = Allocated | Pipelined | Binding | Bound | Running | Releasing
	activeAllocatedStatuses = Allocated | Pipelined | Binding | Bound | Running
	aliveStatuses           = Allocated | Pipelined | Binding | Bound | Running | Pending | Gated
	boundStatuses           = Allocated | Bound | Running | Releasing
	allocatedStatuses       = Allocated | Bound | Binding | Running
)

func (ps PodStatus) String() string {
	switch ps {
	case Pending:
		return "Pending"
	case Gated:
		return "Gated"
	case Allocated:
		return "Allocated"
	case Pipelined:
		return "Pipelined"
	case Binding:
		return "Binding"
	case Bound:
		return "Bound"
	case Running:
		return "Running"
	case Releasing:
		return "Releasing"
	case Succeeded:
		return "Succeeded"
	case Failed:
		return "Failed"
	case Deleted:
		return "Deleted"
	default:
		return "Unknown"
	}
}

func IsAliveStatus(statusInput PodStatus) bool {
	return aliveStatuses&statusInput != 0
}

func IsActiveUsedStatus(statusInput PodStatus) bool {
	return activeUsedStatuses&statusInput != 0
}

func IsActiveAllocatedStatus(statusInput PodStatus) bool {
	return activeAllocatedStatuses&statusInput != 0
}

func IsPodBound(statusInput PodStatus) bool {
	return boundStatuses&statusInput != 0
}

func AllocatedStatus(status PodStatus) bool {
	return allocatedStatuses&status != 0
}
