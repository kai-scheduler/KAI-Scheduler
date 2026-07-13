// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"testing"

	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestNewSchemeDoesNotRegisterKartaByDefault(t *testing.T) {
	scheme := newScheme()

	_, _, err := scheme.ObjectKinds(&kartav1alpha1.Karta{})

	require.Error(t, err)
}
