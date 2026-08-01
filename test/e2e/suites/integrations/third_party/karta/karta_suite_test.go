/*
Copyright 2026 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/

package karta

import (
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestKartaIntegration(t *testing.T) {
	utils.SetLogger()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Karta integration Suite")
}
