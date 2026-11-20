// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// RunOptions is the sidecar configuration main parses from flags.
type RunOptions struct {
	Listen       string
	Dir          string
	KeyFile      string
	RollInterval time.Duration
	RollBytes    int64
}

// Run reads the HMAC key, rolls batches on the interval, and serves until ctx is cancelled.
func Run(ctx context.Context, opts RunOptions) error {
	if opts.KeyFile == "" {
		return fmt.Errorf("hmac key file is required")
	}
	key, err := os.ReadFile(opts.KeyFile)
	if err != nil {
		return fmt.Errorf("read hmac key: %w", err)
	}
	writer, err := NewBatchWriter(opts.Dir, opts.RollBytes, key)
	if err != nil {
		return err
	}
	go RollOnInterval(ctx, writer, opts.RollInterval)
	if err := Serve(ctx, opts.Listen, writer); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("recorder serve: %w", err)
	}
	return nil
}

// RollOnInterval closes the open batch on a timer. A non-positive interval does nothing.
func RollOnInterval(ctx context.Context, writer *BatchWriter, every time.Duration) {
	if every <= 0 || writer == nil {
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
