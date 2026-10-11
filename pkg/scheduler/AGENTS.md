# pkg/scheduler

Hot path: code under `plugins/`, `actions/` and `api/` runs for every pod in every scheduling cycle.

- Guard V(5+) logs with `.Do(...)` ([CONTRIBUTING.md](../../CONTRIBUTING.md#logging-practices)). Do not build strings, slices or maps outside the guard.
- Run `make benchmark` (or `go test -bench` in `actions/`) when touching allocation-heavy code.

## Adding a plugin

1. Create `plugins/<name>/` with `New(framework.PluginArguments) framework.Plugin` (`framework/plugins.go`).
2. Register it in `InitDefaultPlugins` in `plugins/factory.go`.
3. A registered plugin only runs if it is in the shard's plugin list. Default plugins and their priorities are defined in `schedulingshard_types.go` of the `kai/v1` API (module `github.com/kai-scheduler/api`, mirrored in `pkg/apis/kai/v1`; see `pkg/apis/AGENTS.md`); add it there if it must be on by default.

## Adding an action

Implement `framework.Action` (`framework/interface.go`), then add it to `InitDefaultActions` in `actions/factory.go`. Simulated changes go through a `framework.Statement`; roll back or discard it on every failure path.

## Tests

- Action unit tests are plain `go test` (`func Test...`), not Ginkgo; run with `go test -run <Name> ./pkg/scheduler/actions/<action>`.
- Build clusters with `test_utils/` (`nodes_fake`, `jobs_fake`, `tasks_fake`, ...) instead of hand-rolling objects.
- Multi-action scenarios go in `actions/integration_tests/<scenario>`.
- Mocks (`cache_mock.go`, ...) are generated: see the root `AGENTS.md`.

Design context: `docs/developer/` (plugin framework, actions, fairness).
