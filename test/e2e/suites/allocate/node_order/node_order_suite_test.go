/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package node_order

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeAffinitySpecs()
var _ = DescribeResourceTypeSpecs()

func TestNodeOrder(t *testing.T) {
	utils.SetLogger()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Placement Strategy Suite")
}
