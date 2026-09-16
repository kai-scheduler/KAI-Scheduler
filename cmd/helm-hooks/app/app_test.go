// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "no subcommand", args: nil, wantErr: "subcommand required"},
		{name: "unknown subcommand", args: []string{"bogus"}, wantErr: `unknown subcommand "bogus"`},
		{name: "apply-crds", args: []string{"apply-crds"}},
		{name: "apply-crds rejects positional args", args: []string{"apply-crds", "extra"}, wantErr: "unexpected arguments"},
		{name: "apply-config", args: []string{"apply-config", "--file=/manifest/kai-config.yaml"}},
		{name: "apply-config requires file", args: []string{"apply-config"}, wantErr: "--file is required"},
		{name: "migrate-topologies", args: []string{"migrate-topologies"}},
		{name: "cleanup", args: []string{"cleanup", "--namespace=kai", "--delete-config=kai-config"}},
		{name: "cleanup without config", args: []string{"cleanup", "--namespace=kai"}},
		{name: "cleanup requires namespace", args: []string{"cleanup", "--delete-config=kai-config"}, wantErr: "--namespace is required"},
		{name: "unknown flag", args: []string{"cleanup", "--bogus=1"}, wantErr: "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run, err := parseArgs(tt.args)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, run)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, run)
		})
	}
}
