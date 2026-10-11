// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	k8splugins "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/plugins"
)

func TestPluginWithoutPodAffinity(t *testing.T) {
	cacheMock := cache.NewMockCache(gomock.NewController(t))
	cacheMock.EXPECT().InternalK8sPlugins().Return(&k8splugins.K8sPlugins{})
	ssn := &framework.Session{Cache: cacheMock}
	pp := &predicatesPlugin{ssn: ssn}
	pp.initializeBindReadiness()
	require.Empty(t, ssn.BindReadyFns)
}
