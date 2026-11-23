// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package version

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRegisterMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	RegisterMetrics(registry, "scheduler")

	want := fmt.Sprintf(`# HELP kai_build_info Build information for a KAI component.
# TYPE kai_build_info gauge
kai_build_info{component="scheduler",git_commit=%q,go_version=%q,version=%q} 1
`, gitCommit, runtime.Version(), gitVersion)
	if err := testutil.GatherAndCompare(registry, strings.NewReader(want), "kai_build_info"); err != nil {
		t.Fatal(err)
	}
}
