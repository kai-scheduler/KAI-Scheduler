// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"github.com/kai-scheduler/kai-scheduler/pkg/scheduler/api/queue_info"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Interface interface {
	GetResourceUsage() (*queue_info.ClusterUsage, error)
}
type UsageDBConfig struct {
	ClientType             string       `yaml:"clientType" json:"clientType"`
	ConnectionString       string       `yaml:"connectionString,omitempty" json:"connectionString,omitempty"`
	ConnectionStringEnvVar string       `yaml:"connectionStringEnvVar,omitempty" json:"connectionStringEnvVar,omitempty"`
	UsageParams            *UsageParams `yaml:"usageParams,omitempty" json:"usageParams,omitempty"`
}

// GetUsageParams returns the usage params if set, and default params if not set.
func (c *UsageDBConfig) GetUsageParams() *UsageParams {
	up := UsageParams{}
	if c.UsageParams != nil {
		up = *c.UsageParams
	}
	up.SetDefaults()
	return &up
}

func (c *UsageDBConfig) DeepCopy() *UsageDBConfig {
	out := new(UsageDBConfig)
	out.ClientType = c.ClientType
	out.ConnectionString = c.ConnectionString
	out.ConnectionStringEnvVar = c.ConnectionStringEnvVar
	if c.UsageParams != nil {
		out.UsageParams = c.UsageParams.DeepCopy()
	}
	return out
}

// UsageParams defines common params for all usage db clients. Some clients may not support all the params.
type UsageParams struct {
	// Half life period of the usage. If not set, or set to 0, the usage will not be decayed.
	HalfLifePeriod *metav1.Duration `yaml:"halfLifePeriod,omitempty" json:"halfLifePeriod,omitempty"`
	// Window size of the usage. Default is 1 week.
	WindowSize *metav1.Duration `yaml:"windowSize,omitempty" json:"windowSize,omitempty"`
	// Window type for time-series aggregation. If not set, defaults to sliding.
	WindowType *WindowType `yaml:"windowType,omitempty" json:"windowType,omitempty"`
	// The start timestamp of the tumbling window. If not set, defaults to the current time.
	TumblingWindowStartTime *metav1.Time `yaml:"tumblingWindowStartTime,omitempty" json:"tumblingWindowStartTime,omitempty"`
	// The cron string defining the behavior of the cron window.
	CronString string `yaml:"cronString,omitempty" json:"cronString,omitempty"`
	// Fetch interval of the usage. Default is 1 minute.
	FetchInterval *metav1.Duration `yaml:"fetchInterval,omitempty" json:"fetchInterval,omitempty"`
	// Staleness period of the usage. Default is 5 minutes.
	StalenessPeriod *metav1.Duration `yaml:"stalenessPeriod,omitempty" json:"stalenessPeriod,omitempty"`
	// Wait timeout of the usage. Default is 1 minute.
	WaitTimeout *metav1.Duration `yaml:"waitTimeout,omitempty" json:"waitTimeout,omitempty"`

	// ExtraParams are extra parameters for the usage db client, which are client specific.
	ExtraParams map[string]string `yaml:"extraParams,omitempty" json:"extraParams,omitempty"`
}

func (p *UsageParams) DeepCopy() *UsageParams {
	out := new(UsageParams)
	if p.HalfLifePeriod != nil {
		duration := *p.HalfLifePeriod
		out.HalfLifePeriod = &duration
	}
	if p.WindowSize != nil {
		duration := *p.WindowSize
		out.WindowSize = &duration
	}
	if p.WindowType != nil {
		windowType := *p.WindowType
		out.WindowType = &windowType
	}
	if p.TumblingWindowStartTime != nil {
		startTime := *p.TumblingWindowStartTime
		out.TumblingWindowStartTime = &startTime
	}
	out.CronString = p.CronString
	if p.FetchInterval != nil {
		duration := *p.FetchInterval
		out.FetchInterval = &duration
	}
	if p.StalenessPeriod != nil {
		duration := *p.StalenessPeriod
		out.StalenessPeriod = &duration
	}
	if p.WaitTimeout != nil {
		duration := *p.WaitTimeout
		out.WaitTimeout = &duration
	}
	if p.ExtraParams != nil {
		out.ExtraParams = make(map[string]string, len(p.ExtraParams))
		for k, v := range p.ExtraParams {
			out.ExtraParams[k] = v
		}
	}
	return out
}
