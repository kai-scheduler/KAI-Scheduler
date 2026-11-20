// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunRequiresAKeyFile(t *testing.T) {
	err := Run(context.Background(), RunOptions{Dir: t.TempDir()})
	require.ErrorContains(t, err, "hmac key file is required")

	err = Run(context.Background(), RunOptions{Dir: t.TempDir(), KeyFile: filepath.Join(t.TempDir(), "missing")})
	require.ErrorContains(t, err, "read hmac key")

	empty := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	err = Run(context.Background(), RunOptions{Dir: t.TempDir(), KeyFile: empty, Listen: "127.0.0.1:0"})
	require.ErrorContains(t, err, "hmac key is empty")
}

func TestRunServesUntilCancelled(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key")
	require.NoError(t, os.WriteFile(keyFile, []byte("replay-key"), 0o600))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{
			Listen:       address,
			Dir:          dir,
			KeyFile:      keyFile,
			RollInterval: time.Hour,
			RollBytes:    1 << 20,
		})
	}()

	var conn net.Conn
	require.Eventually(t, func() bool {
		dialed, dialErr := net.Dial("tcp", address)
		if dialErr != nil {
			return false
		}
		conn = dialed
		return true
	}, 2*time.Second, 10*time.Millisecond)
	body, err := json.Marshal(Envelope{
		Kind:   KindCycle,
		Record: mustJSON(t, map[string]any{"metadata": map[string]any{"name": "visible-name"}}),
	})
	require.NoError(t, err)
	_, err = conn.Write(append(body, '\n'))
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool {
		matches, globErr := filepath.Glob(filepath.Join(dir, "open-*.jsonl"))
		return globErr == nil && len(matches) == 1
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	require.Eventually(t, func() bool {
		select {
		case err := <-done:
			require.NoError(t, err)
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond)
}

func TestRollOnIntervalRollsAndStops(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewBatchWriter(dir, 1<<20, []byte("key"))
	require.NoError(t, err)
	require.NoError(t, writer.Append(context.Background(), mustJSON(t, Envelope{
		Kind:   KindCycle,
		Record: mustJSON(t, map[string]any{"phase": "Running"}),
	})))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RollOnInterval(ctx, writer, 20*time.Millisecond)
		close(done)
	}()
	require.Eventually(t, func() bool {
		matches, globErr := filepath.Glob(filepath.Join(dir, "records-*.jsonl.gz"))
		return globErr == nil && len(matches) == 1
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("roll loop did not stop when the context was cancelled")
	}

	RollOnInterval(context.Background(), nil, time.Second)
	RollOnInterval(context.Background(), writer, 0)
}
