// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package config

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

var (
	suite = "Config Utility Functions"
)

func TestControllerUtils(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, suite)
}
