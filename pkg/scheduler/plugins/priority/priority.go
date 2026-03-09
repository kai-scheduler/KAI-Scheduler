/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package priority

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

type priorityPlugin struct{}

func New(_ framework.PluginArguments) framework.Plugin {
	return &priorityPlugin{}
}

func (pp *priorityPlugin) Name() string {
	return "priority"
}

func (pp *priorityPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddJobOrderFn(JobOrderFn)
}

func JobOrderFn(l, r interface{}) int {
	lv := l.(*podgroup_info.PodGroupInfo)
	rv := r.(*podgroup_info.PodGroupInfo)

	if lv.Priority > rv.Priority {
		return -1
	}

	if lv.Priority < rv.Priority {
		return 1
	}

	return 0
}

func (pp *priorityPlugin) OnSessionClose(_ *framework.Session) {}
