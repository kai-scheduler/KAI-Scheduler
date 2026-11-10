// Copyright 2025 NVIDIA CORPORATION
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
	"sync"
	"time"
)

const maxRecordBytes = 512 << 20

// Sink receives one compressed batch. Implementations must be safe for a single caller.
type Sink interface {
	Upload(ctx context.Context, name string, r io.Reader) error
}

// LocalSink writes batches as files in Dir.
type LocalSink struct {
	Dir string
}

func (s LocalSink) Upload(_ context.Context, name string, r io.Reader) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.Dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, r)
	return err
}

// BatchWriter appends redacted records and rolls the file by size or when Roll is called.
type BatchWriter struct {
	dir       string
	sink      Sink
	rollBytes int64
	hasher    Hasher
	mu        sync.Mutex
	file      *os.File
	bytes     int64
	seq       int
	opened    time.Time
}

func NewBatchWriter(dir string, sink Sink, rollBytes int64, key []byte) (*BatchWriter, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("recorder hmac key is empty")
	}
	if sink == nil {
		return nil, fmt.Errorf("recorder sink is nil")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if rollBytes <= 0 {
		rollBytes = 64 << 20
	}
	return &BatchWriter{
		dir:       dir,
		sink:      sink,
		rollBytes: rollBytes,
		hasher:    NewHasher(key),
	}, nil
}

// Append redacts one envelope and writes it. Oversized or unreadable lines are skipped.
func (w *BatchWriter) Append(ctx context.Context, line []byte) error {
	var env Envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return nil
	}
	if err := RedactEnvelope(&env, w.hasher); err != nil {
		return nil
	}
	redacted, err := json.Marshal(&env)
	if err != nil {
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

// Roll compresses the open batch and uploads it.
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
	w.seq++
	path := filepath.Join(w.dir, fmt.Sprintf("open-%d.jsonl", w.seq))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	w.file = file
	w.bytes = 0
	w.opened = time.Now().UTC()
	return nil
}

func (w *BatchWriter) rollLocked(ctx context.Context) error {
	if w.file == nil || w.bytes == 0 {
		if w.file != nil {
			path := w.file.Name()
			_ = w.file.Close()
			w.file = nil
			_ = os.Remove(path)
		}
		return nil
	}
	path := w.file.Name()
	if err := w.file.Close(); err != nil {
		w.file = nil
		return err
	}
	w.file = nil
	uploadName := fmt.Sprintf("records-%s-%06d.jsonl.gz", w.opened.Format("20060102T150405Z"), w.seq)
	if err := w.compressAndUpload(ctx, path, uploadName); err != nil {
		return err
	}
	return os.Remove(path)
}

func (w *BatchWriter) compressAndUpload(ctx context.Context, path, name string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	gzipPath := path + ".gz"
	out, err := os.OpenFile(gzipPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	zipper := gzip.NewWriter(out)
	if _, err := io.Copy(zipper, in); err != nil {
		_ = zipper.Close()
		_ = out.Close()
		return err
	}
	if err := zipper.Close(); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	defer os.Remove(gzipPath)
	gzipFile, err := os.Open(gzipPath)
	if err != nil {
		return err
	}
	defer gzipFile.Close()
	return w.sink.Upload(ctx, name, gzipFile)
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
			return
		}
	}
}
