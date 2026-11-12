// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := recorder.Run(ctx, recorder.RunOptions{
		Listen:       *listen,
		Dir:          *dir,
		KeyFile:      *keyFile,
		RollInterval: *rollInterval,
		RollBytes:    *rollBytes,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "recorder: %v\n", err)
		os.Exit(1)
	}
}
