// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package options

import (
	"reflect"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/diff"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/kubernetes/pkg/features"
)

func TestAddFlags(t *testing.T) {
	fs := pflag.NewFlagSet("addflagstest", pflag.ContinueOnError)
	s := NewServerOption()
	s.AddFlags(fs)

	args := []string{
		"--schedule-period=5m",
		"--feature-gates=DynamicResourceAllocation=true,VolumeCapacityPriority=false",
	}
	fs.Parse(args)

	// This is a snapshot of expected options parsed by args.
	expected := &ServerOption{
		SchedulerName:                     defaultSchedulerName,
		Namspace:                          defaultNamespace,
		MetricsNamespace:                  defaultMetricsNamespace,
		ResourceReservationAppLabel:       defaultResourceReservationAppLabel,
		SchedulePeriod:                    5 * time.Minute,
		PrintVersion:                      true,
		ListenAddress:                     defaultListenAddress,
		ProfilerApiPort:                   defaultProfilerApiPort,
		Verbosity:                         defaultVerbosityLevel,
		MaxNumberConsolidationPreemptees:  defaultMaxConsolidationPreemptees,
		FullHierarchyFairness:             true,
		QPS:                               50,
		Burst:                             300,
		DetailedFitErrors:                 false,
		UseSchedulingSignatures:           true,
		NodeLevelScheduler:                false,
		AllowConsolidatingReclaim:         true,
		PyroscopeBlockProfilerRate:        DefaultPyroscopeBlockProfilerRate,
		PyroscopeMutexProfilerRate:        DefaultPyroscopeMutexProfilerRate,
		GlobalDefaultStalenessGracePeriod: defaultStalenessGracePeriod,
		NumOfStatusRecordingWorkers:       defaultNumOfStatusRecordingWorkers,
		NodePoolLabelKey:                  defaultNodePoolLabelKey,
		PluginServerPort:                  8081,
		CPUWorkerNodeLabelKey:             defaultCPUWorkerNodeLabelKey,
		GPUWorkerNodeLabelKey:             defaultGPUWorkerNodeLabelKey,
		MIGWorkerNodeLabelKey:             defaultMIGWorkerNodeLabelKey,
	}

	if !reflect.DeepEqual(expected, s) {
		difference := diff.ObjectGoPrintSideBySide(expected, s)
		t.Errorf("Got different run options than expected.\nGot: %+v\nExpected: %+v\ndiff: %s", s, expected, difference)
	}

	// Test that the feature gates are set correctly.
	if !utilfeature.DefaultFeatureGate.Enabled(features.DynamicResourceAllocation) {
		t.Errorf("DynamicResourceAllocation feature gate should be enabled")
	}
}
