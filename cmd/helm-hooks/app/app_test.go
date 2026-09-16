// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFlagValidation(t *testing.T) {
	c := fake.NewClientBuilder().Build()
	tests := []struct {
		name    string
		err     error
		wantErr string
	}{
		{name: "apply-crds rejects positional args", err: applyCRDs(context.Background(), c, []string{"extra"}), wantErr: "unexpected arguments"},
		{name: "apply-config requires file", err: applyConfig(context.Background(), c, nil), wantErr: "--file is required"},
		{name: "migrate-topologies rejects unknown flag", err: migrateTopologies(context.Background(), c, []string{"--bogus"}), wantErr: "flag provided but not defined"},
		{name: "cleanup requires namespace", err: cleanup(context.Background(), c, []string{"--delete-config=kai-config"}), wantErr: "--namespace is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.err)
			assert.Contains(t, tt.err.Error(), tt.wantErr)
		})
	}
}
