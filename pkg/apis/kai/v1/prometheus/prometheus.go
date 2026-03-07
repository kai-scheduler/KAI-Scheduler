// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package prometheus

import (
	"github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1/common"
	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
	"k8s.io/utils/ptr"
)

type Prometheus struct {
	// Enabled defines whether a Prometheus instance should be deployed
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`

	// InstanceName defines the name of the Prometheus instance to be created
	// Defaults to "prometheus" if not specified
	// +kubebuilder:validation:Optional
	InstanceName *string `json:"instanceName,omitempty"`

	// RetentionPeriod defines how long to retain data (e.g., "2w", "1d", "30d")
	// +kubebuilder:validation:Optional
	RetentionPeriod *string `json:"retentionPeriod,omitempty"`

	// SampleInterval defines the interval of sampling (e.g., "1m", "30s", "5m")
	// +kubebuilder:validation:Optional
	SampleInterval *string `json:"sampleInterval,omitempty"`

	// EnablePersistentStorage defines whether to use persistent storage for the Prometheus instance
	// +kubebuilder:validation:Optional
	EnablePersistentStorage *bool `json:"enablePersistentStorage,omitempty"`

	// StorageSize defines the size of the storage (e.g., "20Gi", "30Gi")
	// +kubebuilder:validation:Optional
	StorageSize *string `json:"storageSize,omitempty"`

	// StorageClassName defines the name of the storageClass that will be used to store the TSDB data. defaults to "standard".
	// +kubebuilder:validation:Optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// ServiceMonitor defines ServiceMonitor configuration for KAI services
	// +kubebuilder:validation:Optional
	ServiceMonitor *ServiceMonitor `json:"serviceMonitor,omitempty"`

	// ExternalPrometheusUrl defines the URL of an external Prometheus instance to use
	// When set, KAI will not deploy its own Prometheus but will configure ServiceMonitors
	// for the external instance and validate connectivity
	// +kubebuilder:validation:Optional
	ExternalPrometheusUrl *string `json:"externalPrometheusUrl,omitempty"`

	// ExternalPrometheusPingConfig defines the configuration for external Prometheus connectivity validation, with defaults.
	// +kubebuilder:validation:Optional
	ExternalPrometheusHealthProbe *ExternalPrometheusHealthProbe `json:"externalPrometheusHealthProbe,omitempty"`

	// AccountingLabelKey defines the label key used to match ServiceMonitors and Prometheus rules for Prometheus discovery
	// +kubebuilder:validation:Optional
	AccountingLabelKey *string `json:"accountingLabelKey,omitempty"`

	// AccountingLabelValue defines the label value used to match ServiceMonitors and Prometheus rules for Prometheus discovery
	// +kubebuilder:validation:Optional
	AccountingLabelValue *string `json:"accountingLabelValue,omitempty"`
}

type ExternalPrometheusHealthProbe struct {
	// Interval defines the interval for external Prometheus connectivity validation (in seconds)
	// +kubebuilder:validation:Optional
	Interval *int `json:"interval,omitempty"`

	// PingsTimeout defines the timeout for external Prometheus connectivity validation (in seconds)
	// +kubebuilder:validation:Optional
	Timeout *int `json:"timeout,omitempty"`

	// PingsMaxRetries defines the maximum number of retries for external Prometheus connectivity validation
	// +kubebuilder:validation:Optional
	MaxRetries *int `json:"maxRetries,omitempty"`
}

const DefaultInstanceName = "prometheus"

func (p *Prometheus) SetDefaultsWhereNeeded() {
	if p == nil {
		return
	}
	p.Enabled = common.SetDefault(p.Enabled, ptr.To(false))
	p.InstanceName = common.SetDefault(p.InstanceName, ptr.To(DefaultInstanceName))
	p.RetentionPeriod = common.SetDefault(p.RetentionPeriod, ptr.To("2w"))
	p.SampleInterval = common.SetDefault(p.SampleInterval, ptr.To("1m"))
	p.StorageClassName = common.SetDefault(p.StorageClassName, ptr.To("standard"))
	p.ExternalPrometheusUrl = common.SetDefault(p.ExternalPrometheusUrl, nil)
	p.ExternalPrometheusHealthProbe = common.SetDefault(p.ExternalPrometheusHealthProbe, &ExternalPrometheusHealthProbe{})
	p.ExternalPrometheusHealthProbe.SetDefaultsWhereNeeded()
	p.ServiceMonitor = common.SetDefault(p.ServiceMonitor, &ServiceMonitor{})
	p.ServiceMonitor.SetDefaultsWhereNeeded()
	p.AccountingLabelKey = common.SetDefault(p.AccountingLabelKey, ptr.To(constants.DefaultAccountingLabelKey))
	p.AccountingLabelValue = common.SetDefault(p.AccountingLabelValue, ptr.To(constants.DefaultAccountingLabelValue))
}

// ServiceMonitor defines ServiceMonitor configuration for KAI services
type ServiceMonitor struct {
	// Enabled defines whether ServiceMonitor resources should be created
	// +kubebuilder:validation:Optional
	Enabled *bool `json:"enabled,omitempty"`

	// Interval defines the scrape interval for the ServiceMonitor (e.g., "30s", "1m")
	// +kubebuilder:validation:Optional
	Interval *string `json:"interval,omitempty"`

	// ScrapeTimeout defines the scrape timeout for the ServiceMonitor (e.g., "10s", "30s")
	// +kubebuilder:validation:Optional
	ScrapeTimeout *string `json:"scrapeTimeout,omitempty"`

	// BearerTokenFile defines the path to the bearer token file for authentication
	// +kubebuilder:validation:Optional
	BearerTokenFile *string `json:"bearerTokenFile,omitempty"`
}

func (s *ServiceMonitor) SetDefaultsWhereNeeded() {
	if s == nil {
		return
	}
	s.Enabled = common.SetDefault(s.Enabled, ptr.To(true))
	s.Interval = common.SetDefault(s.Interval, ptr.To("30s"))
	s.ScrapeTimeout = common.SetDefault(s.ScrapeTimeout, ptr.To("10s"))
	s.BearerTokenFile = common.SetDefault(s.BearerTokenFile, ptr.To("/var/run/secrets/kubernetes.io/serviceaccount/token"))
}

func (p *ExternalPrometheusHealthProbe) SetDefaultsWhereNeeded() {
	if p == nil {
		return
	}
	p.Interval = common.SetDefault(p.Interval, ptr.To(30))
	p.Timeout = common.SetDefault(p.Timeout, ptr.To(10))
	p.MaxRetries = common.SetDefault(p.MaxRetries, ptr.To(5))
}
