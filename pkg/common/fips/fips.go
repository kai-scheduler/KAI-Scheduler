// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package fips

import (
	v1 "k8s.io/api/core/v1"
)

const (
	GODEBUGEnvName   = "GODEBUG"
	OnlyGODEBUGValue = "fips140=only,tlsmlkem=0"
)

func OnlyEnv(enabled bool) []v1.EnvVar {
	if !enabled {
		return nil
	}
	return []v1.EnvVar{{Name: GODEBUGEnvName, Value: OnlyGODEBUGValue}}
}
