// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common_info

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	v1 "k8s.io/api/core/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal"
)

const (
	ResourcesWereNotFoundMsg = "no nodes with enough resources were found"
	DefaultPodError          = "Unable to schedule pod"
	OverheadMessage          = "Not enough resources due to pod overhead resources"
)

type TasksFitError struct {
	taskNamespace   string
	taskName        string
	NodeName        string
	Reasons         []string
	DetailedReasons []string
}

func NewFitErrorWithDetailedMessage(name, namespace, nodeName string, reasons []string, detailedReasons ...string) *TasksFitError {
	fe := &TasksFitError{
		taskName:        name,
		taskNamespace:   namespace,
		NodeName:        nodeName,
		Reasons:         reasons,
		DetailedReasons: detailedReasons,
	}

	if len(detailedReasons) == 0 {
		fe.DetailedReasons = reasons
	}

	return fe
}

func NewFitError(name, namespace, nodeName string, message string) *TasksFitError {
	return NewFitErrorWithDetailedMessage(name, namespace, nodeName, []string{message})
}

func NewFitErrorByReasons(name, namespace, nodeName string, err error, reasons ...string) *TasksFitError {
	message := reasons
	if len(message) == 0 && err != nil {
		message = []string{err.Error()}
	}
	return NewFitErrorWithDetailedMessage(name, namespace, nodeName, message)
}

func NewFitErrorInsufficientResource(
	name, namespace, nodeName string,
	gpuRequested *resource_info.GpuResourceRequirement,
	requestedVector, usedVector, capacityVector resource_info.ResourceVector,
	vectorMap *resource_info.ResourceVectorMap,
	capacityGpuMemory int64, gangSchedulingJob bool, messageSuffix string,
) *TasksFitError {
	availableVector := capacityVector.Clone()
	availableVector.Sub(usedVector)

	var shortMessages []string
	var detailedMessages []string

	if len(gpuRequested.MigResources()) > 0 {
		for migProfile, quant := range gpuRequested.MigResources() {
			migIdx := vectorMap.GetIndex(migProfile)
			availableMigProfilesQuant := int64(availableVector.Get(migIdx))
			capacityMigProfilesQuant := int64(capacityVector.Get(migIdx))
			if availableMigProfilesQuant < quant {
				detailedMessages = append(detailedMessages, k8s_internal.NewInsufficientResourceErrorScalarResources(
					migProfile,
					quant,
					int64(usedVector.Get(migIdx)),
					capacityMigProfilesQuant,
					gangSchedulingJob))
				shortMessages = append(shortMessages, fmt.Sprintf("node(s) didn't have enough of mig profile: %s",
					migProfile))
			}
		}
	} else {
		requestedGPUs := gpuRequested.GPUs()
		availableGPUs := availableVector.Get(resource_info.GPUIndex)
		if requestedGPUs > availableGPUs {
			detailedMessages = append(detailedMessages, k8s_internal.NewInsufficientResourceError(
				"GPUs",
				gpuRequested.GpusAsString(),
				strconv.FormatFloat(usedVector.Get(resource_info.GPUIndex), 'g', 3, 64),
				strconv.FormatFloat(capacityVector.Get(resource_info.GPUIndex), 'g', 3, 64),
				gangSchedulingJob))
			shortMessages = append(shortMessages, "node(s) didn't have enough resources: GPUs")
		}

		if gpuRequested.GpuMemory() > capacityGpuMemory {
			detailedMessages = append(detailedMessages, k8s_internal.NewInsufficientGpuMemoryCapacity(
				gpuRequested.GpuMemory(), capacityGpuMemory, gangSchedulingJob))
			shortMessages = append(shortMessages, "node(s) didn't have enough resources: GPU memory")
		}
	}

	requestedCPUs := int64(requestedVector.Get(resource_info.CPUIndex))
	availableCPUs := int64(availableVector.Get(resource_info.CPUIndex))
	if requestedCPUs > availableCPUs {
		detailedMessages = append(detailedMessages, k8s_internal.NewInsufficientResourceError(
			"CPU cores",
			humanize.FtoaWithDigits(requestedVector.Get(resource_info.CPUIndex)/resource_info.MilliCPUToCores, 3),
			humanize.FtoaWithDigits(usedVector.Get(resource_info.CPUIndex)/resource_info.MilliCPUToCores, 3),
			humanize.FtoaWithDigits(capacityVector.Get(resource_info.CPUIndex)/resource_info.MilliCPUToCores, 3),
			gangSchedulingJob))
		shortMessages = append(shortMessages, "node(s) didn't have enough resources: CPU cores")
	}

	if requestedVector.Get(resource_info.MemoryIndex) > availableVector.Get(resource_info.MemoryIndex) {
		detailedMessages = append(detailedMessages, k8s_internal.NewInsufficientResourceError(
			"memory",
			humanize.FtoaWithDigits(requestedVector.Get(resource_info.MemoryIndex)/resource_info.MemoryToGB, 3),
			humanize.FtoaWithDigits(usedVector.Get(resource_info.MemoryIndex)/resource_info.MemoryToGB, 3),
			humanize.FtoaWithDigits(capacityVector.Get(resource_info.MemoryIndex)/resource_info.MemoryToGB, 3),
			gangSchedulingJob))
		shortMessages = append(shortMessages, "node(s) didn't have enough resources: memory")
	}

	for i := 0; i < vectorMap.Len(); i++ {
		rName := vectorMap.ResourceAt(i)
		if rName == v1.ResourceCPU || rName == v1.ResourceMemory || rName == constants.GpuResource {
			continue
		}
		if resource_info.IsMigResource(v1.ResourceName(rName)) {
			continue
		}
		requestedQuant := int64(requestedVector.Get(i))
		availableQuant := int64(availableVector.Get(i))
		capacityQuant := int64(capacityVector.Get(i))
		if requestedQuant > 0 && availableQuant < requestedQuant {
			detailedMessages = append(detailedMessages, k8s_internal.NewInsufficientResourceErrorScalarResources(
				v1.ResourceName(rName),
				requestedQuant,
				int64(usedVector.Get(i)), capacityQuant,
				gangSchedulingJob))
			shortMessages = append(shortMessages, fmt.Sprintf("node(s) didn't have enough resources: %s",
				rName))
		}
	}

	if len(messageSuffix) > 0 {
		for i, msg := range shortMessages {
			shortMessages[i] = fmt.Sprintf("%s. %s", msg, messageSuffix)
		}
		for i, msg := range detailedMessages {
			detailedMessages[i] = fmt.Sprintf("%s. %s", msg, messageSuffix)
		}
	}

	return NewFitErrorWithDetailedMessage(name, namespace, nodeName, shortMessages, detailedMessages...)
}

func (f *TasksFitError) Error() string {
	return fmt.Sprintf("Pod %s/%s cannot be scheduled on node %s. reasons: %s", f.taskNamespace, f.taskName,
		f.NodeName, strings.Join(f.Reasons, ". \n"))
}

type TasksFitErrors struct {
	reasonCounts map[string]int
	err          string
}

func NewFitErrors() *TasksFitErrors {
	return &TasksFitErrors{reasonCounts: make(map[string]int)}
}

func (f *TasksFitErrors) SetError(err string) {
	f.err = err
}

func (f *TasksFitErrors) AddReasonCounts(errors *TasksFitErrors) {
	if errors == nil {
		return
	}
	if f.reasonCounts == nil {
		f.reasonCounts = make(map[string]int)
	}
	for reason, count := range errors.reasonCounts {
		f.reasonCounts[reason] += count
	}
}

func (f *TasksFitErrors) AddNodeError(err error) {
	if err == nil {
		return
	}
	if f.reasonCounts == nil {
		f.reasonCounts = make(map[string]int)
	}
	var reasons []string
	if fitError, ok := err.(*TasksFitError); ok {
		if fitError == nil {
			return
		}
		reasons = fitError.Reasons
	} else {
		reasons = []string{err.Error()}
	}
	for _, reason := range reasons {
		f.reasonCounts[reason]++
	}
}

func (f *TasksFitErrors) HasNodeErrors() bool {
	return len(f.reasonCounts) != 0
}

func (f *TasksFitErrors) ReasonCount(reason string) int {
	return f.reasonCounts[reason]
}

func (f *TasksFitErrors) UniqueReasonCount() int {
	return len(f.reasonCounts)
}

func (f *TasksFitErrors) DetailedError(nodeErrors ...[]*TasksFitError) string {
	baseError := f.err
	if baseError == "" {
		baseError = ResourcesWereNotFoundMsg
	}
	reasonMessages := []string{"\n" + baseError + "."}
	if len(nodeErrors) == 0 {
		return strings.Join(reasonMessages, "")
	}
	for _, node := range nodeErrors[0] {
		reasonMessages = append(reasonMessages,
			fmt.Sprintf("\n<%v>: %v.", node.NodeName, strings.Join(node.DetailedReasons, ", ")))
	}
	sort.Strings(reasonMessages)
	return strings.Join(reasonMessages, "")
}

func (f *TasksFitErrors) Error() string {
	reasonStrings := make([]string, 0, len(f.reasonCounts))
	for reason, count := range f.reasonCounts {
		reasonStrings = append(reasonStrings, fmt.Sprintf("%v %v", count, reason))
	}
	sort.Strings(reasonStrings)

	baseError := f.err
	if baseError == "" {
		baseError = ResourcesWereNotFoundMsg
	}
	if len(reasonStrings) > 0 {
		baseError += fmt.Sprintf(": %v.", strings.Join(reasonStrings, ". \n"))
	}
	return baseError
}

type NotFoundError struct {
	Name string
}

func (e *NotFoundError) Error() string { return e.Name + ": not found" }
