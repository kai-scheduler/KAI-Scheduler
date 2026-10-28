// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"encoding/json"
	"os"
	"testing"

	"k8s.io/utils/ptr"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	"github.com/kai-scheduler/api/kai/v1/binder"
	kaiv1common "github.com/kai-scheduler/api/kai/v1/common"
	"github.com/kai-scheduler/api/kai/v1/pod_grouper"
)

func TestServiceClientConfig(t *testing.T) {
	spec := &kaiv1.ConfigSpec{
		Binder: &binder.Binder{MaxConcurrentReconciles: ptr.To(100)},
		PodGrouper: &pod_grouper.PodGrouper{
			K8sClientConfig:         &kaiv1common.K8sClientConfig{QPS: ptr.To(500), Burst: ptr.To(1000)},
			MaxConcurrentReconciles: ptr.To(50),
		},
	}

	config := serviceClientConfig(spec)

	assertClientConfig(t, config["binder"], 20, 100, ptr.To(100))
	assertClientConfig(t, config["podGrouper"], 500, 1000, ptr.To(50))
	assertClientConfig(t, config["queueController"], 20, 100, nil)
	if spec.Binder.Service != nil {
		t.Fatal("serviceClientConfig changed the input spec")
	}
}

func TestServiceClientConfigSurvivesTestResults(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := updateSuiteResult(func(suiteResult *SuiteResult) {
		suiteResult.ServiceClientConfig = serviceClientConfig(&kaiv1.ConfigSpec{})
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeTestResults("test", true, map[string]int{"jobs": 1}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outputFileName)
	if err != nil {
		t.Fatal(err)
	}
	var suiteResult SuiteResult
	if err := json.Unmarshal(data, &suiteResult); err != nil {
		t.Fatal(err)
	}
	if suiteResult.Status != "success" || len(suiteResult.Tests) != 1 {
		t.Fatalf("unexpected suite result: %s", data)
	}
	assertClientConfig(t, suiteResult.ServiceClientConfig["binder"], 20, 100, nil)
}

func assertClientConfig(t *testing.T, config ServiceClientConfig, qps, burst int, maxConcurrentReconciles *int) {
	t.Helper()
	if config.K8sClientConfig == nil {
		t.Fatal("missing k8sClientConfig")
	}
	if *config.K8sClientConfig.QPS != qps || *config.K8sClientConfig.Burst != burst {
		t.Fatalf("got qps %d burst %d, want qps %d burst %d",
			*config.K8sClientConfig.QPS, *config.K8sClientConfig.Burst, qps, burst)
	}
	if maxConcurrentReconciles == nil {
		if config.MaxConcurrentReconciles != nil {
			t.Fatalf("got maxConcurrentReconciles %d, want unset", *config.MaxConcurrentReconciles)
		}
		return
	}
	if config.MaxConcurrentReconciles == nil || *config.MaxConcurrentReconciles != *maxConcurrentReconciles {
		t.Fatalf("got maxConcurrentReconciles %v, want %d", config.MaxConcurrentReconciles, *maxConcurrentReconciles)
	}
}
