// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common_info

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"

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
	resourceRequested *resource_info.ResourceRequirements,
	usedVector, capacityVector resource_info.ResourceVector,
	vectorMap *resource_info.ResourceVectorMap,
	capacityGpuMemory int64, gangSchedulingJob bool, messageSuffix string,
) *TasksFitError {
	availableVector := capacityVector.Clone()
	availableVector.Sub(usedVector)

	var shortMessages []string
	var detailedMessages []string

	if len(resourceRequested.MigResources()) > 0 {
		for migProfile, quant := range resourceRequested.MigResources() {
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
		requestedGPUs := resourceRequested.GPUs()
		availableGPUs := availableVector.Get(resource_info.GPUIndex)
		if requestedGPUs > availableGPUs {
			detailedMessages = append(detailedMessages, k8s_internal.NewInsufficientResourceError(
				"GPUs",
				resourceRequested.GpusAsString(),
				strconv.FormatFloat(usedVector.Get(resource_info.GPUIndex), 'g', 3, 64),
				strconv.FormatFloat(capacityVector.Get(resource_info.GPUIndex), 'g', 3, 64),
				gangSchedulingJob))
			shortMessages = append(shortMessages, "node(s) didn't have enough resources: GPUs")
		}

		if resourceRequested.GpuMemory() > capacityGpuMemory {
			detailedMessages = append(detailedMessages, k8s_internal.NewInsufficientGpuMemoryCapacity(
				resourceRequested.GpuMemory(), capacityGpuMemory, gangSchedulingJob))
			shortMessages = append(shortMessages, "node(s) didn't have enough resources: GPU memory")
		}
	}

	requestedCPUs := int64(resourceRequested.Cpu())
	availableCPUs := int64(availableVector.Get(resource_info.CPUIndex))
	if requestedCPUs > availableCPUs {
		detailedMessages = append(detailedMessages, k8s_internal.NewInsufficientResourceError(
			"CPU cores",
			humanize.FtoaWithDigits(resourceRequested.Cpu()/resource_info.MilliCPUToCores, 3),
			humanize.FtoaWithDigits(usedVector.Get(resource_info.CPUIndex)/resource_info.MilliCPUToCores, 3),
			humanize.FtoaWithDigits(capacityVector.Get(resource_info.CPUIndex)/resource_info.MilliCPUToCores, 3),
			gangSchedulingJob))
		shortMessages = append(shortMessages, "node(s) didn't have enough resources: CPU cores")
	}

	if resourceRequested.Memory() > availableVector.Get(resource_info.MemoryIndex) {
		detailedMessages = append(detailedMessages, k8s_internal.NewInsufficientResourceError(
			"memory",
			humanize.FtoaWithDigits(resourceRequested.Memory()/resource_info.MemoryToGB, 3),
			humanize.FtoaWithDigits(usedVector.Get(resource_info.MemoryIndex)/resource_info.MemoryToGB, 3),
			humanize.FtoaWithDigits(capacityVector.Get(resource_info.MemoryIndex)/resource_info.MemoryToGB, 3),
			gangSchedulingJob))
		shortMessages = append(shortMessages, "node(s) didn't have enough resources: memory")
	}

	for requestedResourceName, requestedResourceQuant := range resourceRequested.ScalarResources() {
		scalarIdx := vectorMap.GetIndex(requestedResourceName)
		availableResourceQuant := int64(availableVector.Get(scalarIdx))
		capacityResourceQuant := int64(capacityVector.Get(scalarIdx))
		if availableResourceQuant < requestedResourceQuant {
			detailedMessages = append(detailedMessages, k8s_internal.NewInsufficientResourceErrorScalarResources(
				requestedResourceName,
				requestedResourceQuant,
				int64(usedVector.Get(scalarIdx)), capacityResourceQuant,
				gangSchedulingJob))
			shortMessages = append(shortMessages, fmt.Sprintf("node(s) didn't have enough resources: %s",
				requestedResourceName))
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
	nodes map[string]*TasksFitError
	err   string
}

func NewFitErrors() *TasksFitErrors {
	f := new(TasksFitErrors)
	f.nodes = make(map[string]*TasksFitError)
	return f
}

func (f *TasksFitErrors) SetError(err string) {
	f.err = err
}

func (f *TasksFitErrors) SetNodeError(nodeName string, err error) {
	var fe *TasksFitError
	switch obj := err.(type) {
	case *TasksFitError:
		obj.NodeName = nodeName
		fe = obj
	default:
		fe = NewFitError("", "", nodeName, err.Error())
	}

	f.nodes[nodeName] = fe
}

func (f *TasksFitErrors) AddNodeErrors(errors *TasksFitErrors) {
	for nodeName, fitError := range errors.nodes {
		f.nodes[nodeName] = fitError
	}
}

func (f *TasksFitErrors) DetailedError() string {
	if f.err == "" {
		f.err = ResourcesWereNotFoundMsg
	}
	reasonMessages := []string{"\n" + f.err + "."}
	for _, node := range f.nodes {
		reasonMessages = append(reasonMessages,
			fmt.Sprintf("\n<%v>: %v.", node.NodeName, strings.Join(node.DetailedReasons, ", ")))
	}
	sort.Strings(reasonMessages)
	return strings.Join(reasonMessages, "")
}

func (f *TasksFitErrors) Error() string {
	reasons := make(map[string]int)

	sortReasonsHistogram := func() []string {
		for _, node := range f.nodes {
			for _, reason := range node.Reasons {
				reasons[reason]++
			}
		}

		var reasonStrings []string
		for k, v := range reasons {
			reasonStrings = append(reasonStrings, fmt.Sprintf("%v %v", v, k))
		}
		sort.Strings(reasonStrings)
		return reasonStrings
	}
	if f.err == "" {
		f.err = ResourcesWereNotFoundMsg
	}
	reasonMsg := f.err

	nodeReasonsHistogram := sortReasonsHistogram()
	if len(nodeReasonsHistogram) > 0 {
		reasonMsg += fmt.Sprintf(": %v.", strings.Join(nodeReasonsHistogram, ". \n"))
	}
	return reasonMsg
}

type NotFoundError struct {
	Name string
}

func (e *NotFoundError) Error() string { return e.Name + ": not found" }
