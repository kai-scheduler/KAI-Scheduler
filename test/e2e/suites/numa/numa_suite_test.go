// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeNUMAModesSpecs()
var _ = DescribeNUMAQoSSpecs()
var _ = DescribeNUMAReclaimSpecs()
var _ = DescribeNUMAPreemptSpecs()
var _ = DescribeNUMAOperandSpecs()

func TestNUMA(t *testing.T) {
	utils.SetLogger()
	RegisterFailHandler(Fail)
	RunSpecs(t, "NUMA Aware Scheduling Suite")
}
