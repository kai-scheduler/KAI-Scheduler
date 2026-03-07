/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package predicates

import (
	"testing"

	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeAffinitySpecs()
var _ = DescribeConfigMapSpecs()
var _ = DescribePVCSpecs()
var _ = DescribeTaintTolerationSpecs()

func TestPredicates(t *testing.T) {
	utils.SetLogger()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Predicates Suite")
}
