package resourceaware

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

type resourceAwarePlugin struct {
	mode string
}

func New(arguments framework.PluginArguments) framework.Plugin {
	mode := arguments.GetString("mode", "prefer-larger")
	if mode != "prefer-larger" && mode != "prefer-smaller" {
		log.InfraLogger.Warningf("resourceaware: unrecognized mode %q, defaulting to prefer-larger", mode)
		mode = "prefer-larger"
	}
	return &resourceAwarePlugin{mode: mode}
}

func (rp *resourceAwarePlugin) Name() string {
	return "resourceaware"
}

func (rp *resourceAwarePlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddJobOrderFn(rp.JobOrderFn)
}

func (rp *resourceAwarePlugin) JobOrderFn(l, r interface{}) int {
	lv := l.(*podgroup_info.PodGroupInfo)
	rv := r.(*podgroup_info.PodGroupInfo)

	if lv.Priority != rv.Priority {
		return 0
	}

	lGPU := lv.GetAliveTasksRequestedGPUs()
	rGPU := rv.GetAliveTasksRequestedGPUs()

	switch rp.mode {
	case "prefer-smaller":
		if lGPU < rGPU {
			return -1
		}
		if lGPU > rGPU {
			return 1
		}
	default: // "prefer-larger"
		if lGPU > rGPU {
			return -1
		}
		if lGPU < rGPU {
			return 1
		}
	}
	return 0
}

func (rp *resourceAwarePlugin) OnSessionClose(_ *framework.Session) {}
