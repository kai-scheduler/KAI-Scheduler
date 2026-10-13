# KAI Scheduler

Kubernetes scheduler for GPU/AI workloads, built on kube-batch with a plugin architecture. Go module `github.com/kai-scheduler/KAI-scheduler`. Services live in `cmd/` and `pkg/`; `ls cmd pkg` is the map.

Before editing under `pkg/apis`, `pkg/scheduler`, `pkg/operator`, `pkg/podgrouper` or `test/`, read the `AGENTS.md` in that directory.

## Hard rules

- **Never hand-edit generated files**: `zz_generated*`, `pkg/apis/client/`, `deployments/kai-scheduler/crds/`, `deployments/kai-scheduler/templates/rbac/`, `*_mock.go`. Change the source (types, kubebuilder markers, interfaces) and regenerate: `make generate manifests clients generate-mocks`.
- New mocks must be added to the explicit `generate-mocks` list in the `Makefile`.
- `make validate` regenerates everything and ends with `git diff --exit-code`. Commit all regenerated output or CI fails.
- Sign off every commit (`git commit -s`); the DCO check fails otherwise.
- Use `git mv` when moving files.
- New files need the Apache-2.0 + NVIDIA header. `make gen-license` adds it.

## Commands

```bash
make validate                                  # run before finishing any non-trivial change
go test ./pkg/scheduler/actions/allocate       # most packages use plain go test
go test -run TestHandleAllocation ./pkg/scheduler/actions/allocate
go tool ginkgo -v --focus "pattern" ./pkg/binder/controllers/integration_tests   # Ginkgo suites
make envtest                                   # then run go test with KUBEBUILDER_ASSETS (see build/makefile/testenv.mk)
./hack/run-e2e-kind.sh                         # e2e on kind; add --preserve-cluster to keep it
go tool ginkgo -r --randomize-all --label-filter '!autoscale && !scale && !upgrade && !gitops' ./test/e2e/suites   # CI e2e filter
```

- `make test` and `make build` run in Docker and are slow; prefer the targeted commands above while iterating.
- Test files live next to the code and end in `_test.go`.
- Not every package is Ginkgo. Check for a `suite_test.go` before using `ginkgo`.

## Code conventions

Only what linters and `gofmt` do not enforce:

- Imports in three groups: stdlib, external, internal.
- `ctx context.Context` is the first parameter.
- Comments explain why, not what. Keep them short, with no pronouns (`I`, `we`). Do not add obvious comments.
- Logging follows [CONTRIBUTING.md](CONTRIBUTING.md#logging-practices). V(5+) scheduler logs must be guarded with `.Do(...)`.
- Add RBAC through kubebuilder markers, e.g. `// +kubebuilder:rbac:groups=core,resources=pods,verbs=get`, then run `make manifests`.

## Pull requests

- **Issue first** for features, behavior or API changes and non-trivial fixes: find or open an issue (templates in `.github/ISSUE_TEMPLATE/`) before the PR, and link it under "Related Issues" (`Fixes #N`). Typos, docs and trivial fixes do not need one.
- Open PRs as draft. Title is `<type>(<scope>): <description>` (conventional commits); allowed scopes are in `.github/workflows/validate-pr-title.yaml`.
- Fill in `.github/pull_request_template.md`.
- **Changelog**: PRs that change behavior (feature, fix, API change, notable perf) need one fragment. Never edit `CHANGELOG.md` or write fragment files by hand.
  ```bash
  make changelog KIND=<Added|Changed|Fixed|Removed> BODY="under 20 words" AUTHOR="<github-user>" ISSUE="<pr-or-issue-number>"
  ```
  One fragment per PR; amend it instead of adding another. Refactor, docs, test and CI PRs get the `skip-changelog` label instead (`dependencies` for dependency bumps).

## Where to look

| Need | Location |
|---|---|
| Architecture, plugin/action framework, concepts | `docs/developer/` |
| Design docs for major features | `docs/developer/designs/` (one directory per feature; `ls` to browse) |
| Usage examples | `examples/` |
| Diagnose a Pending pod or PodGroup | `.agents/skills/kai-pending` |
| Capture and replay scheduler snapshots | `.agents/skills/snapshots` |
| Logging, PR process | `CONTRIBUTING.md` |
