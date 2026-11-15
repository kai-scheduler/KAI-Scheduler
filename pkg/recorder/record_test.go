// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckSchemaRejectsANewerRecord(t *testing.T) {
	require.NoError(t, CheckSchema(0))
	require.NoError(t, CheckSchema(SchemaVersion))
	err := CheckSchema(SchemaVersion + 1)
	require.ErrorContains(t, err, "schemaVersion")
	require.ErrorContains(t, err, "newer")
}
