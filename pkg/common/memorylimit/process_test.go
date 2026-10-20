// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package memorylimit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunMainHonorsExplicitBudgetAndRunsApplication(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "1GiB")
	t.Setenv(RatioEnv, "invalid")
	ran := false
	RunMain(func() { ran = true })
	assert.True(t, ran)
}
