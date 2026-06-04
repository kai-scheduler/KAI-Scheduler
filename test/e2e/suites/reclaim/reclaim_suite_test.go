/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package reclaim

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeHierarchyLevelFairnessSpecs()
var _ = DescribeReclaimDRASpecs()
var _ = DescribeReclaimDistributedSpecs()
var _ = DescribeReclaimMinSubGroupSpecs()

func TestReclaim(t *testing.T) {
	utils.SetLogger()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Reclaim Suite")
}
