// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
)

func main() {
	listen := flag.String("listen", envOr("KAI_RECORDER_LISTEN", "127.0.0.1:9095"), "address that accepts scheduler records")
	dir := flag.String("dir", envOr("KAI_RECORDER_DIR", "/var/lib/kai-recorder"), "directory for open batches and the local sink")
	sinkDir := flag.String("sink-dir", envOr("KAI_RECORDER_SINK_DIR", ""), "directory the local sink uploads into; defaults to --dir")
	keyFile := flag.String("hmac-key-file", envOr("KAI_RECORDER_HMAC_KEY_FILE", ""), "file containing the redaction key")
	rollInterval := flag.Duration("roll-interval", durationEnv("KAI_RECORDER_ROLL_INTERVAL", 5*time.Minute), "roll a batch at least this often")
	rollBytes := flag.Int64("roll-bytes", int64Env("KAI_RECORDER_ROLL_BYTES", 64<<20), "roll a batch once it reaches this many bytes")
	flag.Parse()

	if *keyFile == "" {
		fmt.Fprintln(os.Stderr, "hmac key file is required")
		os.Exit(1)
	}
	key, err := os.ReadFile(*keyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read hmac key: %v\n", err)
		os.Exit(1)
	}
	if *sinkDir == "" {
		*sinkDir = *dir
	}

	writer, err := recorder.NewBatchWriter(*dir, recorder.LocalSink{Dir: *sinkDir}, *rollBytes, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "recorder: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go rollOnInterval(ctx, writer, *rollInterval)

	if err := recorder.Serve(ctx, *listen, writer); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "recorder serve: %v\n", err)
		os.Exit(1)
	}
}

func rollOnInterval(ctx context.Context, writer *recorder.BatchWriter, every time.Duration) {
	if every <= 0 {
		return
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := writer.Roll(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "recorder roll: %v\n", err)
			}
		}
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func int64Env(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	var parsed int64
	if _, err := fmt.Sscan(value, &parsed); err != nil {
		return fallback
	}
	return parsed
}
