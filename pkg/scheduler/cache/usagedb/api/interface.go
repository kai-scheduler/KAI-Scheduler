// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/queue_info"
	usagedb "github.com/kai-scheduler/api/usagedb"
)

// Interface stays in kai-scheduler: it returns scheduler-runtime types (queue_info)
// and cannot live in the API module. The config structs moved to
// github.com/kai-scheduler/api/usagedb and are re-exported here as aliases.
type Interface interface {
	GetResourceUsage() (*queue_info.ClusterUsage, error)
}

type (
	UsageDBConfig = usagedb.UsageDBConfig
	UsageParams   = usagedb.UsageParams
	WindowType    = usagedb.WindowType
)

const (
	SlidingWindow  = usagedb.SlidingWindow
	TumblingWindow = usagedb.TumblingWindow
	CronWindow     = usagedb.CronWindow
)
