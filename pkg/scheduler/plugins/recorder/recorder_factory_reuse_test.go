// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

func TestRecorderFactoryReusesUnchangedState(t *testing.T) {
	// New creates a process-wide sender. Leave the rest of the suite's sender untouched.
	const childEnv = "KAI_TEST_RECORDER_FACTORY_REUSE_CHILD"
	if os.Getenv(childEnv) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRecorderFactoryReusesUnchangedState$", "-test.v")
		cmd.Env = append(os.Environ(), childEnv+"=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("recorder subprocess: %v\n%s", err, output)
		}
		return
	}

	registerReplayPlugins(t)
	config, params := replayConfig()
	config.Tiers[0].Plugins = append(config.Tiers[0].Plugins, conf.PluginOption{Name: Name})

	var records []*cycleRecord
	framework.RegisterPluginBuilder(Name, func(args framework.PluginArguments) framework.Plugin {
		p := New(args).(*recorderPlugin)
		// Intercept the payload before transport; use the normal factory and session lifecycle.
		p.enqueue = func(r *cycleRecord) bool {
			records = append(records, r)
			return true
		}
		return p
	})

	mux := http.NewServeMux()
	for _, sessionID := range []string{"cycle-1", "cycle-2"} {
		cache := startReplayCache(t, params, idleObjects(), nil, time.Time{})
		s, err := framework.OpenSession(cache, config, params, sessionID, mux)
		require.NoError(t, err)
		framework.CloseSession(s)
	}

	require.Len(t, records, 2)
	require.NotEmpty(t, records[0].StateID)
	require.Equal(t, records[0].StateID, records[1].StateID)
	require.NotNil(t, records[0].RawObjects)
	t.Logf("same state ID %s; embedded in cycle 1: %t; embedded in cycle 2: %t",
		records[0].StateID, records[0].RawObjects != nil, records[1].RawObjects != nil)
	require.Nil(t, records[1].RawObjects, "the second cycle should reference the first state copy")
}
