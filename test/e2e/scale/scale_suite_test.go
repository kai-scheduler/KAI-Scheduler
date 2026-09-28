// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"cmp"
	"os"
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant/labels"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// defaultScaleLabelFilter keeps the extended scenarios out of the daily benchmark run, which invokes
// ginkgo without a label filter. Override with SCALE_LABEL_FILTER or an explicit --label-filter.
const defaultScaleLabelFilter = "!" + labels.ScaleExtended

func TestScale(t *testing.T) {
	utils.SetLogger()
	RegisterFailHandler(Fail)

	suiteConfig, reporterConfig := GinkgoConfiguration()
	if suiteConfig.LabelFilter == "" {
		suiteConfig.LabelFilter = cmp.Or(os.Getenv("SCALE_LABEL_FILTER"), defaultScaleLabelFilter)
	}

	RunSpecs(t, "Scale Suite", suiteConfig, reporterConfig)
}
