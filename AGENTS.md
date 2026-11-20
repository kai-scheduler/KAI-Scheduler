# KAI Scheduler

Kubernetes scheduler for GPU/AI workloads, built on kube-batch with a plugin architecture. Go module `github.com/kai-scheduler/KAI-scheduler`. Services live in `cmd/` and `pkg/`; `ls cmd pkg` is the map.

## Hard rules

- **Never hand-edit generated files**: `zz_generated*`, `pkg/apis/client/`, `deployments/kai-scheduler/crds/`, `deployments/kai-scheduler/templates/rbac/`, `*_mock.go`. Change the source (types, kubebuilder markers, interfaces) and regenerate: `make generate manifests clients generate-mocks`.
- New mocks must be added to the explicit `generate-mocks` list in the `Makefile`.
- `make validate` regenerates everything and ends with `git diff --exit-code`. Commit all regenerated output or CI fails. Run it before finishing any non-trivial change.
- Sign off every commit (`git commit -s`); the DCO check fails otherwise.
- Use `git mv` when moving files.
- New files need the Apache-2.0 + NVIDIA header. `make gen-license` adds it.
- `deployments/kai-scheduler/Chart.lock` is untracked local build output (the `gpu-fractioning` sub-chart is a version range). A stale local one freezes the sub-chart version; `rm -f` it to pick up a newer release.

### API types

API types are owned by the standalone module `github.com/kai-scheduler/api` (version in `go.mod`); all Go code here imports them from there. `pkg/apis` is a mirror that `make generate manifests clients` still read to produce `deployments/kai-scheduler/crds/` and `pkg/apis/client/`.

- Do not change API types, CRD fields or API constants only in this repo. Ask first how the change should land in the `api` module and here; the two must not diverge. Process: [CONTRIBUTING.md](CONTRIBUTING.md#changing-api-types-and-shared-utilities).
- Storage versions: `grep -rn storageversion pkg/apis`. Config and SchedulingShard are `kai/v1`.
- A field change in a served version is an API change and needs a changelog fragment. CRDs are upgraded through `pkg/helmhooks` (`apply_crds.go`); check that path still works.

## Where to look

Read the file for your task before starting; load only what you need.

| If you are... | Read |
|---|---|
| Writing or testing Go code (style, commands, logging) | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Opening or triaging an issue | [ISSUES.md](docs/contributing/ISSUES.md) |
| Opening a PR, adding a changelog fragment | [PR.md](docs/contributing/PR.md) |
| Editing `pkg/scheduler` (plugins, actions, hot path) | [pkg/scheduler/README.md](pkg/scheduler/README.md) |
| Editing `test/` or writing e2e tests | [test/README.md](test/README.md) |
| Editing `pkg/operator` | [docs/developer/operator.md](docs/developer/operator.md) |
| Adding a workload type to the podgrouper | [docs/developer/pod-grouper.md](docs/developer/pod-grouper.md) |
| Needing architecture or plugin/action concepts | `docs/developer/` |
| Looking for a major feature's design | `docs/developer/designs/` (one directory per feature) |
| Needing usage examples | `examples/` |
| Diagnosing a Pending pod or PodGroup | `.agents/skills/kai-pending` |
| Capturing and replaying scheduler snapshots | `.agents/skills/snapshots` |
