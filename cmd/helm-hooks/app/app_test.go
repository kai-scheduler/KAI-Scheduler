// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// noClient fails the test if a subcommand connects before its arguments are valid.
func noClient(t *testing.T) clientFactory {
	return func() (client.Client, error) {
		t.Fatal("client created before argument validation")
		return nil, errors.New("unreachable")
	}
}

func TestFlagValidationHappensBeforeConnecting(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		run     func(context.Context, clientFactory, []string) error
		flags   []string
		wantErr string
	}{
		{name: "apply-crds rejects positional args", run: applyCRDs, flags: []string{"extra"}, wantErr: "unexpected arguments"},
		{name: "apply-config requires file", run: applyConfig, flags: nil, wantErr: "--file is required"},
		{name: "migrate-topologies rejects unknown flag", run: migrateTopologies, flags: []string{"--bogus"}, wantErr: "flag provided but not defined"},
		{name: "cleanup requires namespace", run: cleanup, flags: []string{"--delete-config=kai-config"}, wantErr: "--namespace is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(ctx, noClient(t), tt.flags)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestRun_RejectsUnknownSubcommandWithoutCluster(t *testing.T) {
	err := Run([]string{"bogus"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown subcommand "bogus"`)
}
