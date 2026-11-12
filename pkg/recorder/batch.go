// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxRecordBytes stays well under the sidecar memory limit. A multi-megabyte
// JSON line expands several times over while it is redacted.
const maxRecordBytes = 8 << 20

// BatchWriter appends redacted records and rolls a gzip file by size or interval.
type BatchWriter struct {
	dir         string
	rollBytes   int64
	maxDirBytes int64
	hasher      Hasher
	drops       atomic.Uint64

	mu    sync.Mutex
	file  *os.File
	bytes int64
	path  string
}

func NewBatchWriter(dir string, rollBytes int64, key []byte) (*BatchWriter, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("recorder hmac key is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if rollBytes <= 0 {
		rollBytes = 64 << 20
	}
	return &BatchWriter{
		dir:         dir,
		rollBytes:   rollBytes,
		maxDirBytes: 256 << 20,
		hasher:      NewHasher(key),
	}, nil
}

// Drops is the number of records the sidecar refused.
func (w *BatchWriter) Drops() uint64 {
	return w.drops.Load()
}

// Append redacts one envelope and writes it. Unreadable or oversized lines are counted as drops.
func (w *BatchWriter) Append(ctx context.Context, line []byte) error {
	if len(line) == 0 {
		return nil
	}
	if len(line) > maxRecordBytes {
		w.drops.Add(1)
		return nil
	}
	var env Envelope
	if err := json.Unmarshal(line, &env); err != nil {
		w.drops.Add(1)
		return nil
	}
	if err := RedactEnvelope(&env, w.hasher); err != nil {
		w.drops.Add(1)
		return nil
	}
	redacted, err := json.Marshal(&env)
	if err != nil {
		w.drops.Add(1)
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ensureFile(); err != nil {
		return err
	}
	if _, err := w.file.Write(append(redacted, '\n')); err != nil {
		return err
	}
	w.bytes += int64(len(redacted) + 1)
	if w.bytes >= w.rollBytes {
		return w.rollLocked(ctx)
	}
	return nil
}

// Roll closes the open gzip file and keeps the directory under maxDirBytes.
func (w *BatchWriter) Roll(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rollLocked(ctx)
}

// Close rolls any open batch.
func (w *BatchWriter) Close(ctx context.Context) error {
	return w.Roll(ctx)
}

func (w *BatchWriter) ensureFile() error {
	if w.file != nil {
		return nil
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	w.path = filepath.Join(w.dir, "open-"+stamp+".jsonl")
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	w.file = file
	w.bytes = 0
	return nil
}

func (w *BatchWriter) rollLocked(_ context.Context) error {
	if w.file == nil || w.bytes == 0 {
		if w.file != nil {
			_ = w.file.Close()
			_ = os.Remove(w.path)
			w.file = nil
		}
		return nil
	}
	path := w.path
	if err := w.file.Close(); err != nil {
		w.file = nil
		return err
	}
	w.file = nil
	stamp := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(path), ".jsonl"), "open-")
	uploadName := "records-" + stamp + ".jsonl.gz"
	if err := gzipFile(path, filepath.Join(w.dir, uploadName)); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	w.prune()
	return nil
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	zipper := gzip.NewWriter(out)
	if _, err := io.Copy(zipper, in); err != nil {
		_ = zipper.Close()
		return err
	}
	return zipper.Close()
}

func (w *BatchWriter) prune() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	var total int64
	type fileSize struct {
		name string
		size int64
	}
	var files []fileSize
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
		files = append(files, fileSize{name: entry.Name(), size: info.Size()})
	}
	for i := 0; i < len(files) && total > w.maxDirBytes; i++ {
		if err := os.Remove(filepath.Join(w.dir, files[i].name)); err == nil {
			total -= files[i].size
		}
	}
}

// Serve accepts newline-delimited envelopes until ctx is cancelled.
func Serve(ctx context.Context, address string, writer *BatchWriter) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return writer.Close(context.Background())
			}
			continue
		}
		go handleConn(ctx, conn, writer)
	}
}

func handleConn(ctx context.Context, conn net.Conn, writer *BatchWriter) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), maxRecordBytes)
	for scanner.Scan() {
		if err := writer.Append(ctx, scanner.Bytes()); err != nil {
			writer.drops.Add(1)
			return
		}
	}
	if scanner.Err() != nil {
		writer.drops.Add(1)
	}
}
