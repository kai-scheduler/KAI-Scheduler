// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	npeapi "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/numa_placement_exporter"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/configurations/feature_flags"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
)

// DescribeNUMAOperandSpecs validates the operator's tri-state deployment of the NUMA placement exporter
// DaemonSet: auto-on-shard-enable, explicit on/off in kaiconfig, and prune-on-disable. These cases need
// no NUMA nodes, only the operator.
func DescribeNUMAOperandSpecs() bool {
	return Describe("NUMA placement exporter operand", Ordered, Serial, Label("numa", "nightly"), func() {
		var testCtx *testcontext.TestContext

		BeforeAll(func(ctx context.Context) {
			testCtx = testcontext.GetConnectivity(ctx, Default)
		})

		AfterEach(func(ctx context.Context) {
			// Restore auto (unset) config and disable the plugin so each case starts clean.
			Expect(setNPEEnabled(ctx, testCtx, nil)).To(Succeed())
			Expect(feature_flags.DisableNUMA(ctx, testCtx)).To(Succeed())
			expectNPEDaemonSet(ctx, testCtx, false)
		})

		It("auto - deploys when the numa plugin is enabled in a shard", func(ctx context.Context) {
			Expect(setNPEEnabled(ctx, testCtx, nil)).To(Succeed())
			Expect(feature_flags.EnableNUMA(ctx, testCtx, nil)).To(Succeed())
			expectNPEDaemonSet(ctx, testCtx, true)
		})

		It("explicit disabled overrides a numa-enabled shard", func(ctx context.Context) {
			Expect(setNPEEnabled(ctx, testCtx, ptr.To(false))).To(Succeed())
			Expect(feature_flags.EnableNUMA(ctx, testCtx, nil)).To(Succeed())
			expectNPEDaemonSet(ctx, testCtx, false)
		})

		It("explicit enabled deploys with no numa-enabled shard", func(ctx context.Context) {
			Expect(feature_flags.DisableNUMA(ctx, testCtx)).To(Succeed())
			Expect(setNPEEnabled(ctx, testCtx, ptr.To(true))).To(Succeed())
			expectNPEDaemonSet(ctx, testCtx, true)
		})

		It("prunes the DaemonSet when the numa plugin is disabled", func(ctx context.Context) {
			Expect(setNPEEnabled(ctx, testCtx, nil)).To(Succeed())
			Expect(feature_flags.EnableNUMA(ctx, testCtx, nil)).To(Succeed())
			expectNPEDaemonSet(ctx, testCtx, true)

			Expect(feature_flags.DisableNUMA(ctx, testCtx)).To(Succeed())
			expectNPEDaemonSet(ctx, testCtx, false)
		})
	})
}

// setNPEEnabled sets the kaiconfig tri-state for the NUMA placement exporter (nil = auto).
func setNPEEnabled(ctx context.Context, testCtx *testcontext.TestContext, enabled *bool) error {
	return configurations.PatchKAIConfig(ctx, testCtx, func(c *kaiv1.Config) {
		if c.Spec.NumaPlacementExporter == nil {
			c.Spec.NumaPlacementExporter = &npeapi.NumaPlacementExporter{}
		}
		if c.Spec.NumaPlacementExporter.Service == nil {
			c.Spec.NumaPlacementExporter.Service = &common.Service{}
		}
		c.Spec.NumaPlacementExporter.Service.Enabled = enabled
	})
}
