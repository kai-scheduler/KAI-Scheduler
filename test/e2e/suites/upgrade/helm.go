/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package upgrade

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	helmUpgradeTimeout = 5 * time.Minute
	kaiReleaseName     = "kai-scheduler"
	kaiNamespace       = "kai-scheduler"
)

func upgradeKAIScheduler(chartPath string) {
	args := []string{
		"upgrade", kaiReleaseName, chartPath,
		"-n", kaiNamespace,
		"--set", "global.gpuSharing=true",
		"--set", "global.registry=localhost:30100",
		"--wait",
		"--timeout", helmUpgradeTimeout.String(),
	}

	GinkgoWriter.Printf("Running: helm %v\n", args)
	cmd := exec.Command("helm", args...)
	output, err := cmd.CombinedOutput()
	GinkgoWriter.Printf("Helm upgrade output:\n%s\n", string(output))
	Expect(err).NotTo(HaveOccurred(),
		fmt.Sprintf("helm upgrade failed: %s", string(output)))
}
