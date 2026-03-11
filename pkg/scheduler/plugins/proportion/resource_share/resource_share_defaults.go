// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_share

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	consts "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	common_info "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"knative.dev/pkg/ptr"
)

const (
	defaultRequest = 100000
)

type ResourceShareOverrides struct {
	Deserved                *float64 `json:"deserved"`
	FairShare               *float64 `json:"fairShare"`
	MaxAllowed              *float64 `json:"maxAllowed"`
	OverQuotaWeight         *float64 `json:"overQuotaWeight"`
	Allocated               *float64 `json:"allocated"`
	AllocatedNotPreemptible *float64 `json:"allocatedNotPreemptible"`
	Request                 *float64 `json:"request"`
	AbsoluteUsage           *float64 `json:"absoluteUsage"`
	VacantAdjustedUsage     *float64 `json:"vacantAdjustedUsage"`
}

func (r *ResourceShareOverrides) ResourceShare() *ResourceShare {
	rs := ResourceShare{
		Deserved:                0,
		FairShare:               0,
		MaxAllowed:              consts.UnlimitedResourceQuantity,
		OverQuotaWeight:         1,
		Allocated:               0,
		AllocatedNotPreemptible: 0,
		Request:                 defaultRequest,
	}
	if r.Deserved != nil {
		rs.Deserved = *r.Deserved
	}
	if r.FairShare != nil {
		rs.FairShare = *r.FairShare
	}
	if r.MaxAllowed != nil {
		rs.MaxAllowed = *r.MaxAllowed
	}
	if r.OverQuotaWeight != nil {
		rs.OverQuotaWeight = *r.OverQuotaWeight
	}
	if r.Allocated != nil {
		rs.Allocated = *r.Allocated
	}
	if r.AllocatedNotPreemptible != nil {
		rs.AllocatedNotPreemptible = *r.AllocatedNotPreemptible
	}
	if r.Request != nil {
		rs.Request = *r.Request
	}
	return &rs
}

type QueueResourceShareOverrides struct {
	GPU    *ResourceShareOverrides `json:"gpu"`
	CPU    *ResourceShareOverrides `json:"cpu"`
	Memory *ResourceShareOverrides `json:"memory"`
}

func (q QueueResourceShareOverrides) ResourceShare() QueueResourceShare {
	if q.GPU == nil {
		q.GPU = &ResourceShareOverrides{}
	}
	if q.CPU == nil {
		q.CPU = &ResourceShareOverrides{
			Deserved: ptr.Float64(consts.UnlimitedResourceQuantity),
		}
	}
	if q.Memory == nil {
		q.Memory = &ResourceShareOverrides{
			Deserved: ptr.Float64(consts.UnlimitedResourceQuantity),
		}
	}
	rs := QueueResourceShare{
		GPU:    *q.GPU.ResourceShare(),
		CPU:    *q.CPU.ResourceShare(),
		Memory: *q.Memory.ResourceShare(),
	}
	return rs
}

type QueueOverrides struct {
	UID               common_info.QueueID         `json:"uid"`
	Name              string                      `json:"name"`
	ParentQueue       common_info.QueueID         `json:"parentQueue"`
	ChildQueues       []common_info.QueueID       `json:"childQueues"`
	CreationTimestamp *string                     `json:"creationTimestamp"`
	Priority          *int                        `json:"priority"`
	ResourceShare     QueueResourceShareOverrides `json:"resourceShare"`
}

func (qo *QueueOverrides) ToQueueAttributes() *QueueAttributes {
	qa := &QueueAttributes{
		UID:                qo.UID,
		Name:               qo.Name,
		ParentQueue:        qo.ParentQueue,
		ChildQueues:        qo.ChildQueues,
		Priority:           0,
		QueueResourceShare: qo.ResourceShare.ResourceShare(),
	}

	if qo.Priority != nil {
		qa.Priority = *qo.Priority
	}

	if qo.CreationTimestamp != nil {
		t, err := time.Parse(time.RFC3339, *qo.CreationTimestamp)
		if err == nil {
			qa.CreationTimestamp = metav1.Time{Time: t}
		} else {
			qa.CreationTimestamp = metav1.Now()
		}
	} else {
		qa.CreationTimestamp = metav1.Now()
	}

	return qa
}
