// Copyright 2026 NVIDIA CORPORATION
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
	listen := flag.String("listen", "127.0.0.1:9095", "address that accepts scheduler records")
	dir := flag.String("dir", "/var/lib/kai-recorder", "directory for recorded batches")
	keyFile := flag.String("hmac-key-file", "", "file containing the redaction key")
	rollInterval := flag.Duration("roll-interval", 5*time.Minute, "roll a batch at least this often")
	rollBytes := flag.Int64("roll-bytes", 64<<20, "roll a batch once it reaches this many bytes")
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

	writer, err := recorder.NewBatchWriter(*dir, *rollBytes, key)
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
