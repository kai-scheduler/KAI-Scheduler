// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"bufio"
	"encoding/json"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	record "github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestManySessionsShareOneSender(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	var mu sync.Mutex
	var lines []string
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					mu.Lock()
					lines = append(lines, scanner.Text())
					mu.Unlock()
				}
			}(conn)
		}
	}()

	before := senderLoops()
	address := listener.Addr().String()
	const sessions = 5
	for i := 0; i < sessions; i++ {
		plugin := New(framework.PluginArguments{"address": address}).(*recorderPlugin)
		session := &framework.Session{ID: "cycle"}
		plugin.OnSessionOpen(session)
		plugin.onCommit(&framework.CommitEvent{
			SessionID: session.ID,
			Action:    "allocate",
			Ops:       []framework.CommitOp{{Kind: "allocate", Name: "pod"}},
		})
		plugin.OnSessionClose(session)
	}

	require.Equal(t, before+1, senderLoops())
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(lines) >= sessions
	}, 2*time.Second, 10*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, lines, sessions)
	var epoch string
	for i, line := range lines {
		var env record.Envelope
		require.NoError(t, json.Unmarshal([]byte(line), &env))
		require.Equal(t, uint64(i+1), env.Seq)
		if i == 0 {
			epoch = env.Epoch
			require.NotEmpty(t, epoch)
			continue
		}
		require.Equal(t, epoch, env.Epoch)
	}
}

func senderLoops() int {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n]), "created by github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/recorder.newSender")
		}
		buf = make([]byte, len(buf)*2)
	}
}
