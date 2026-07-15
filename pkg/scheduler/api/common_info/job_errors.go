// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common_info

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	v1 "k8s.io/api/core/v1"
)

const (
	DefaultPodgroupError        = enginev2alpha2.UnschedulableReason("Unable to schedule podgroup")
	UnschedulableWorkloadReason = enginev2alpha2.UnschedulableReason("Not enough resources for the workload")
)

type JobFitError interface {
	Reason() enginev2alpha2.UnschedulableReason
	DetailedMessage() string
	Messages() []string
	ToUnschedulableExplanation() enginev2alpha2.UnschedulableExplanation
}

func JobFitErrorsToDetailedMessage(fitErrors []JobFitError) string {
	reason := DefaultPodgroupError
	if len(fitErrors) == 1 {
		reason = fitErrors[0].Reason()
	}
	reasonMessages := []string{}
	for _, fitError := range fitErrors {
		reasonMessages = append(reasonMessages, fmt.Sprintf("\n%v.", fitError.DetailedMessage()))
	}
	sort.Strings(reasonMessages)
	return "\n" + string(reason) + "." + strings.Join(reasonMessages, "")
}

func JobFitErrorsToMessage(fitErrors []JobFitError) string {
	messages := make(map[string]int)

	sortMessagesHistogram := func() []string {
		for _, fitError := range fitErrors {
			for _, reason := range fitError.Messages() {
				messages[reason]++
			}
		}

		var reasonStrings []string
		for message, messageCount := range messages {
			if messageCount > 1 {
				reasonStrings = append(reasonStrings, fmt.Sprintf("%d %v", messageCount, message))
			} else {
				reasonStrings = append(reasonStrings, message)
			}
		}
		sort.Strings(reasonStrings)
		return reasonStrings
	}
	reason := string(DefaultPodgroupError)
	if len(fitErrors) == 1 {
		reason = string(fitErrors[0].Reason())
	}

	messagesHistogram := sortMessagesHistogram()
	if len(messagesHistogram) > 0 {
		reason += fmt.Sprintf(": %v.", strings.Join(messagesHistogram, ". \n"))
	}
	return reason
}

type JobFitErrorBase struct {
	jobNamespace     string
	jobName          string
	subGroupName     string
	reason           enginev2alpha2.UnschedulableReason
	messages         []string
	detailedMessages []string
}

func NewJobFitError(jobName, subGroupName, jobNamespace string,
	reason enginev2alpha2.UnschedulableReason, messages []string, detailedMessages ...string) *JobFitErrorBase {
	fe := &JobFitErrorBase{
		jobNamespace: jobNamespace,
		jobName:      jobName,
		subGroupName: subGroupName,
		reason:       reason,
		messages:     messages,
	}
	if len(detailedMessages) > 0 {
		fe.detailedMessages = detailedMessages
	} else {
		fe.detailedMessages = messages
	}

	return fe
}

func (f *JobFitErrorBase) Reason() enginev2alpha2.UnschedulableReason {
	return f.reason
}

func (f *JobFitErrorBase) DetailedMessage() string {
	if len(f.subGroupName) == 0 || f.subGroupName == DefaultSubGroupName {
		return strings.Join(f.detailedMessages, ", ")
	}
	return fmt.Sprintf("subgroup %s: %s", f.subGroupName, strings.Join(f.detailedMessages, ", "))
}

func (f *JobFitErrorBase) Messages() []string {
	return f.messages
}

func (f *JobFitErrorBase) ToUnschedulableExplanation() enginev2alpha2.UnschedulableExplanation {
	return enginev2alpha2.UnschedulableExplanation{
		Reason:  enginev2alpha2.UnschedulableReason(f.reason),
		Message: strings.Join(f.messages, ", "),
		Details: nil,
	}
}

type TopologyFitError struct {
	JobFitErrorBase
	nodesGroupName string
}

func NewTopologyFitError(jobName, subGroupName, jobNamespace, nodesGroupName string,
	reason enginev2alpha2.UnschedulableReason, messages []string, detailedReasons ...string) *TopologyFitError {
	return &TopologyFitError{
		JobFitErrorBase: *NewJobFitError(jobName, subGroupName, jobNamespace, reason, messages, detailedReasons...),
		nodesGroupName:  nodesGroupName,
	}
}

func (f *TopologyFitError) DetailedMessage() string {
	return fmt.Sprintf("<%v>: %v", f.nodesGroupName, f.JobFitErrorBase.DetailedMessage())
}

func NewTopologyInsufficientResourcesError(
	jobName, subGroupName, namespace, domainID string,
	resourceRequested resource_info.ResourceVector, availableResource resource_info.ResourceVector,
	vectorMap *resource_info.ResourceVectorMap,
) *TopologyFitError {
	var shortMessages []string
	var detailedMessages []string

	for i := 0; i < vectorMap.Len(); i++ {
		resourceName := v1.ResourceName(vectorMap.ResourceAt(i))
		requested := resourceRequested.Get(i)
		available := availableResource.Get(i)
		if requested <= available {
			continue
		}

		if resource_info.IsMigResource(resourceName) {
			detailedMessages = append(detailedMessages,
				fmt.Sprintf("%s didn't have enough resource: %s, requested: %d, available: %d",
					domainID, resourceName, int64(requested), int64(available)))
			shortMessages = append(shortMessages, fmt.Sprintf("node-group(s) didn't have enough of mig profile: %s",
				resourceName))
		} else if resourceName == constants.GpuResource {
			detailedMessages = append(detailedMessages, fmt.Sprintf("%s didn't have enough resource: GPUs, requested: %s, available: %s",
				domainID,
				strconv.FormatFloat(requested, 'g', 3, 64),
				strconv.FormatFloat(available, 'g', 3, 64),
			))
			shortMessages = append(shortMessages, "node-group(s) didn't have enough resources: GPUs")
		} else if resourceName == v1.ResourceCPU {
			detailedMessages = append(detailedMessages, fmt.Sprintf("%s didn't have enough resources: CPU cores, requested: %s, available: %s",
				domainID,
				humanize.FtoaWithDigits(requested/resource_info.MilliCPUToCores, 3),
				humanize.FtoaWithDigits(available/resource_info.MilliCPUToCores, 3),
			))
			shortMessages = append(shortMessages, "node-group(s) didn't have enough resources: CPU cores")
		} else if resourceName == v1.ResourceMemory {
			detailedMessages = append(detailedMessages, fmt.Sprintf("%s didn't have enough resources: memory, requested: %s, available: %s",
				domainID,
				humanize.FtoaWithDigits(requested/resource_info.MemoryToGB, 3),
				humanize.FtoaWithDigits(available/resource_info.MemoryToGB, 3),
			))
			shortMessages = append(shortMessages, "node-group(s) didn't have enough resources: memory")
		} else {
			detailedMessages = append(detailedMessages, fmt.Sprintf("%s didn't have enough resource: %s, requested: %d, available: %d",
				domainID, resourceName, int64(requested), int64(available)))
			shortMessages = append(shortMessages, fmt.Sprintf("node-group(s) didn't have enough resources: %s",
				resourceName))
		}
	}

	return NewTopologyFitError(jobName, subGroupName, namespace, domainID, UnschedulableWorkloadReason, shortMessages, detailedMessages...)
}

type JobFitErrorWithQueueContext struct {
	JobFitErrorBase
	context *enginev2alpha2.UnschedulableExplanationDetails
}

func NewJobFitErrorWithQueueContext(jobName, subGroupName, jobNamespace string, reason enginev2alpha2.UnschedulableReason,
	message string, context *enginev2alpha2.UnschedulableExplanationDetails) *JobFitErrorWithQueueContext {
	return &JobFitErrorWithQueueContext{
		JobFitErrorBase: *NewJobFitError(jobName, subGroupName, jobNamespace, reason, []string{message}),
		context:         context,
	}
}

func (f *JobFitErrorWithQueueContext) ToUnschedulableExplanation() enginev2alpha2.UnschedulableExplanation {
	return enginev2alpha2.UnschedulableExplanation{
		Reason:  enginev2alpha2.UnschedulableReason(f.reason),
		Message: strings.Join(f.messages, ", "),
		Details: f.context,
	}
}
