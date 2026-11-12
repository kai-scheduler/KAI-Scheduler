// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBatchRollsRedactedGzip(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewBatchWriter(dir, 1, []byte("key"))
	require.NoError(t, err)

	body, err := json.Marshal(Envelope{
		Stream: StreamCycle,
		Seq:    1,
		Epoch:  "epoch-1",
		Kind:   KindCycle,
		Record: mustJSON(t, map[string]any{"metadata": map[string]any{"name": "visible-name"}, "phase": "Running"}),
	})
	require.NoError(t, err)
	require.NoError(t, writer.Append(context.Background(), body))
	require.NoError(t, writer.Roll(context.Background()))

	matches, err := filepath.Glob(filepath.Join(dir, "records-*.jsonl.gz"))
	require.NoError(t, err)
	require.Len(t, matches, 1)
	file, err := os.Open(matches[0])
	require.NoError(t, err)
	defer file.Close()
	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	defer reader.Close()
	payload, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "visible-name")
	require.Contains(t, string(payload), "Running")
}

func TestAppendCountsUnreadableDrops(t *testing.T) {
	writer, err := NewBatchWriter(t.TempDir(), 1<<20, []byte("key"))
	require.NoError(t, err)
	require.NoError(t, writer.Append(context.Background(), []byte("not-json")))
	require.Equal(t, uint64(1), writer.Drops())
}

func TestServeAcceptsOneRecord(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewBatchWriter(dir, 1<<20, []byte("key"))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go handleConn(ctx, conn, writer)
		}
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	body, err := json.Marshal(Envelope{
		Stream: StreamCycle,
		Seq:    2,
		Epoch:  "epoch-2",
		Kind:   KindCycle,
		Record: mustJSON(t, map[string]string{"name": "pod-a", "phase": "Pending"}),
	})
	require.NoError(t, err)
	_, err = conn.Write(append(body, '\n'))
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool {
		writer.mu.Lock()
		defer writer.mu.Unlock()
		return writer.bytes > 0
	}, time.Second, 10*time.Millisecond)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}
