// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package fips

import (
	v1 "k8s.io/api/core/v1"
)

const (
	GODEBUGEnvName = "GODEBUG"
	// OnlyGODEBUGValue forces FIPS 140-3 mode at runtime. tlsmlkem=0 works around a crypto/tls gap
	// where its default, FIPS-allowed X25519MLKEM768 curve preference internally calls the plain
	// X25519 primitive, which unconditionally errors under fips140=only - breaking every outbound
	// TLS handshake (e.g. to the API server via client-go) unless the hybrid curve is disabled.
	// See https://github.com/kubernetes/kubernetes/issues/133743.
	OnlyGODEBUGValue = "fips140=only,tlsmlkem=0"
)

// OnlyEnv returns the GODEBUG env var that enforces FIPS 140-3 mode at runtime when enabled,
// or nil otherwise. See GlobalConfig.FIPSOnly for the runtime panic risk this carries.
func OnlyEnv(enabled bool) []v1.EnvVar {
	if !enabled {
		return nil
	}
	return []v1.EnvVar{{Name: GODEBUGEnvName, Value: OnlyGODEBUGValue}}
}
