# API Repository Separation — Migration Design

**Status:** Proposed · **Scope:** Extract user-facing CRD APIs + shared utilities into `github.com/kai-scheduler/api`

This document describes **how** to move KAI-Scheduler's public API surface into a standalone Go module and
consume it back as a dependency. It is a migration design, not a rationale essay — it assumes the decision to
separate has been made and focuses on mechanics against the current `main`.

---

## 1. Goals

- A standalone module `github.com/kai-scheduler/api` owning every CRD type that has a generated client — the
  `scheduling.run.ai` group plus the runtime-consumed `kai.scheduler` Topology — their generated clients, and
  the shared utilities that depend on them.
- kai-scheduler imports it as a normal, semver-tagged dependency; the API contract versions independently of
  the scheduler, and kai-scheduler stops running `k8s.io/code-generator` altogether.
- Zero behavior change: identical CRDs, identical runtime wiring, no consumer-visible API change.

**Non-goals (this phase):** breaking/renaming any API; moving the operator-internal `kai.scheduler/v1` config
types (Config, SchedulingShard); a monorepo-to-multirepo of the *services*. Only the client-consumed API types
+ their leaf utilities move.

---

## 2. Current state on `main`

Module `github.com/kai-scheduler/KAI-scheduler`, Go 1.26.3, `k8s.io/* v0.35.4`,
`sigs.k8s.io/controller-runtime v0.23.3`. Single go.mod (no nested modules).

**API groups** under `pkg/apis/`:

| Group | Version | Types | Generated client? | Package |
|---|---|---|---|---|
| `scheduling.run.ai` | `v1alpha2` | BindRequest, NumaPlacementRequest | yes | `pkg/apis/scheduling/v1alpha2` |
| `scheduling.run.ai` | `v2` | Queue | yes | `pkg/apis/scheduling/v2` |
| `scheduling.run.ai` | `v2alpha2` | PodGroup (+ webhook) | yes | `pkg/apis/scheduling/v2alpha2` |
| `kai.scheduler` | `v1alpha1` | Topology | yes (lister consumed by scheduler) | `pkg/apis/kai/v1alpha1` |
| `kai.scheduler` | `v1` | Config, SchedulingShard | no (controller-runtime only) | `pkg/apis/kai/v1` |

**Generated code** (`pkg/apis/client/`): clientset (`versioned` + `fake` + `scheme`), listers, informers — a
single clientset/informer factory spanning **both** groups.

**Shared utilities** under `pkg/common/`: `constants`, `resources`, `podgroup` (candidates to move) and
`k8s_utils`, `feature_gates`, `flags` (stay — scheduler-framework glue / runtime behavior, not API).

**Code generation** (`Makefile`):
- `generate` → `controller-gen object:headerFile="./hack/boilerplate.go.txt" paths="./pkg/apis/..."` (deepcopy)
- `manifests` → `controller-gen crd:allowDangerousTypes=true,generateEmbeddedObjectMeta=true,headerFile="./hack/boilerplate.yaml.txt" paths="./pkg/apis/..." output:crd:artifacts:config=deployments/kai-scheduler/crds` (all 6 CRDs) + per-service RBAC roles
- `clients` → `hack/update-client.sh` (runs `k8s.io/code-generator`'s `kube_codegen.sh` over `./pkg/apis`, output to `pkg/apis/client`)

**CRDs** (6) live at `deployments/kai-scheduler/crds/*.yaml`, embedded via `deployments/kai-scheduler/crds/embed.go`
(`//go:embed *.yaml`, `LoadEmbeddedCRDs()`), consumed by the Helm chart and envtest (`pkg/env-tests/setup.go`).

**Blast radius:** 328 files / 519 imports reference `pkg/apis`; 44 files import the scheduling clientset;
`pkg/common/k8s_utils` has 3 importers.

---

## 3. Target architecture

**`github.com/kai-scheduler/api`** — flat layout (no `api/api/…` nesting):

```
github.com/kai-scheduler/api
├── go.mod                     # module github.com/kai-scheduler/api  (Go 1.26.3)
├── scheduling/
│   ├── v1alpha2/              # BindRequest, NumaPlacementRequest
│   ├── v2/                    # Queue
│   └── v2alpha2/              # PodGroup + podgroup_webhook*.go
├── kai/
│   └── v1alpha1/              # Topology  (← pkg/apis/kai/v1alpha1)
├── constants/                 # ← pkg/common/constants
├── utilities/
│   ├── resources/             # ← pkg/common/resources
│   └── podgroup/              # ← pkg/common/podgroup
├── client/                    # generated clientset/listers/informers (scheduling + kai/v1alpha1)
├── config/crd/                # generated scheduling.run.ai_*.yaml + kai.scheduler_topologies.yaml
└── hack/                      # boilerplate + update-client.sh
```

**kai-scheduler after:** removes the moved dirs, keeps only `pkg/apis/kai/v1` (Config, SchedulingShard) and
`pkg/common/{feature_gates,flags}`. It imports the api module for everything else and **drops
`k8s.io/code-generator` entirely** — the retained `kai/v1` types have no generated client, so only
controller-gen deepcopy (already needed for their CRDs) runs locally.

Dependency edge is one-way (`kai-scheduler → api`). Confirmed safe: the moving utilities import only
`scheduling/v2alpha2` (`pkg/common/podgroup/preemptible.go`); `constants`/`resources` import no `pkg/apis`.
Nothing in the moving set imports `kai/v1`, `feature_gates`, `flags`, or `k8s_utils`, so the api module has no
back-edge.

---

## 4. Scope — what moves vs stays

The dividing line is **not** "scheduling group vs kai group" — it is **"has a generated client vs
controller-runtime only"**. Everything with a generated client (all of `scheduling.*` plus Topology) moves
together, which is what lets kai-scheduler shed its code-generation tooling; the two controller-runtime-only
config types stay.

| Item | Move? | Reason |
|---|---|---|
| `pkg/apis/scheduling/{v1alpha2,v2,v2alpha2}` | **Move** | Public CRD contract |
| `pkg/apis/kai/v1alpha1` (Topology) | **Move** | Runtime-consumed lister; has a generated client (see §4.1) |
| `pkg/apis/client/*` (all) | **Move** | Generated clients cover both moving groups |
| `pkg/common/constants` | **Move** | Shared API-level constants (labels/annotations) |
| `pkg/common/{resources,podgroup}` | **Move** | Leaf utilities consumed by external API users |
| `pkg/apis/kai/v1` (Config, SchedulingShard) | **Stay** | Operator-internal; controller-runtime only, no typed clients |
| `pkg/common/k8s_utils` | **Stay** | Scheduler-framework glue; imports `feature_gates` (back-edge → cycle) and pulls `k8s.io/kubernetes`; only internal importers |
| `pkg/common/{feature_gates,flags}` | **Stay** | Runtime behavior, not API contract |
| Per-service RBAC generation | **Stay** | Derives from service packages, not API types |

### 4.1 Why Topology moves (and Config/SchedulingShard don't)

`k8s.io/code-generator` produces a **single** clientset and informer factory spanning every group fed to it. On
`main`, the scheduler consumes all four client-backed types through one factory object — including
`kubeAiSchedulerInformerFactory.Kai().V1alpha1().Topologies().Lister()`
(`pkg/scheduler/cache/cluster_info/data_lister/kubernetes_lister.go:95`).

If Topology stayed behind while the scheduling types left, the api module's factory could **not** expose a
`.Kai()` branch without importing kai-scheduler's Topology types back — an import cycle. kai-scheduler would
then have to keep `k8s.io/code-generator` alive solely for Topology **and** run a second informer factory,
splitting the wiring in `cache.go` and `kubernetes_lister.go`.

Moving Topology with the scheduling types avoids both: the api module's single factory keeps serving all four
listers, and kai-scheduler needs no code generation at all. Topology is also a natural fit for the public
module — it is a runtime scheduling input (topology-aware scheduling), semantically closer to Queue/PodGroup
than to the operator's deploy-time Config/SchedulingShard.

Cost accepted: the `kai.scheduler` group is now sourced from two repos — `v1alpha1` (Topology) in the api
module, `v1` (Config, SchedulingShard) in kai-scheduler. These are distinct packages producing distinct CRD
files (`kai.scheduler_topologies.yaml` vs `kai.scheduler_configs.yaml` / `_schedulingshards.yaml`), so it is
legal and invisible at runtime — only mildly unusual on paper.

---

## 5. Migration mechanics

### 5.1 Seed the `api` repo (fresh, from `main`)

The private repo already exists but its contents are re-seeded from scratch — no history preservation; Apache
2.0 attribution is retained via the per-file copyright headers already present in every source file.

1. From a clean `main` checkout, copy the moving dirs into the flat layout:
   - `pkg/apis/scheduling/*` → `scheduling/*`
   - `pkg/apis/kai/v1alpha1` → `kai/v1alpha1`
   - `pkg/common/constants` → `constants`
   - `pkg/common/{resources,podgroup}` → `utilities/{resources,podgroup}`
2. Rewrite intra-module imports inside the copied tree (same mapping as §5.4).
3. `go mod init github.com/kai-scheduler/api`; set `go 1.26.3`; add the deps the moved code actually uses
   (`k8s.io/{api,apimachinery,client-go} v0.35.4`, `sigs.k8s.io/controller-runtime v0.23.3` for the webhook,
   `k8s.io/code-generator` + `sigs.k8s.io/controller-tools` as tool deps); `go mod tidy`.
4. `go build ./... && go test ./...` green in isolation.

### 5.2 Relocate code generation into the `api` repo

Port `hack/` (boilerplate files + `update-client.sh`) and add Makefile targets, feeding **both** owned groups:

- **deepcopy:** `controller-gen object:headerFile="./hack/boilerplate.go.txt" paths="./scheduling/..." paths="./kai/..."`
- **CRDs:** `controller-gen crd:allowDangerousTypes=true,generateEmbeddedObjectMeta=true,headerFile="./hack/boilerplate.yaml.txt" paths="./scheduling/..." paths="./kai/..." output:crd:artifacts:config=config/crd`
- **clients:** `update-client.sh` with `--output-dir client --output-pkg github.com/kai-scheduler/api/client`
  and input dir `.` (covers `scheduling` + `kai`); keep the `replace_headers.sh` v1alpha2 fix-up if still needed.

CI: lint + test + a `generate`-verify job (fail if generated code is stale). Tag `v0.1.0` once green
(`v0.x` keeps room for pre-1.0 breaking changes).

### 5.3 CRD ownership & Helm sourcing

The api repo's `config/crd` becomes the source of truth for the 3 `scheduling.run.ai_*.yaml` CRDs **and**
`kai.scheduler_topologies.yaml`. The Helm chart and `embed.go` still require the YAMLs physically present at
`deployments/kai-scheduler/crds/`. Keep the chart self-contained by syncing (not symlinking) from the resolved
module:

```makefile
API_CRD_DIR := $(shell go list -m -f '{{.Dir}}' github.com/kai-scheduler/api)/config/crd
sync-api-crds:
	cp $(API_CRD_DIR)/scheduling.run.ai_*.yaml $(API_CRD_DIR)/kai.scheduler_topologies.yaml deployments/kai-scheduler/crds/
```

Run `sync-api-crds` on every api bump. `embed.go` is unchanged (`//go:embed *.yaml`). The remaining
`kai.scheduler_configs.yaml` and `kai.scheduler_schedulingshards.yaml` are still generated locally by
kai-scheduler's own `manifests` target.

### 5.4 kai-scheduler changes (on current `main`)

1. **go.mod:** `require github.com/kai-scheduler/api v0.1.0` and, for local multi-repo dev,
   `replace github.com/kai-scheduler/api => ../api` (local-only; release builds use the tag).
2. **Import rewrite** (mechanical; ~328 files). GNU sed shown; on macOS use `sed -i ''`:
   ```bash
   find . -type f -name '*.go' -not -path './vendor/*' -exec sed -i \
     -e 's|kai-scheduler/KAI-scheduler/pkg/apis/scheduling|kai-scheduler/api/scheduling|g' \
     -e 's|kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1|kai-scheduler/api/kai/v1alpha1|g' \
     -e 's|kai-scheduler/KAI-scheduler/pkg/apis/client|kai-scheduler/api/client|g' \
     -e 's|kai-scheduler/KAI-scheduler/pkg/common/constants|kai-scheduler/api/constants|g' \
     -e 's|kai-scheduler/KAI-scheduler/pkg/common/resources|kai-scheduler/api/utilities/resources|g' \
     -e 's|kai-scheduler/KAI-scheduler/pkg/common/podgroup|kai-scheduler/api/utilities/podgroup|g' \
     {} +
   ```
   The `kai/v1alpha1` rule is intentionally version-specific — it does **not** match `pkg/apis/kai/v1` (Config,
   SchedulingShard), which must stay. Leave `…/pkg/common/{feature_gates,flags,k8s_utils}` untouched. Review the diff to
   confirm no `kai/v1` import was rewritten.
3. **Remove moved code:** `git rm -r pkg/apis/scheduling pkg/apis/kai/v1alpha1 pkg/apis/client pkg/common/{constants,resources,podgroup,k8s_utils}`.
4. **Webhook registration:** `cmd/podgroupcontroller/app/app.go:98`
   `(&v2alpha2.PodGroup{}).SetupWebhookWithManager(mgr)` now resolves `v2alpha2` from
   `github.com/kai-scheduler/api/scheduling/v2alpha2` — import-path change only.
5. **Drop code generation.** Delete the `clients` Makefile target and `hack/update-client.sh` (+ `boilerplate.go.kb.txt`,
   `replace_headers.sh`) — kai-scheduler no longer generates clients. Narrow the remaining targets to the
   config group: `generate` / `manifests` → `paths="./pkg/apis/kai/v1/..."` (deepcopy + the two
   `kai.scheduler_configs.yaml` / `_schedulingshards.yaml` CRDs). RBAC generation unchanged.
6. **Informer factory (single, from the api module).** No factory split. `sc.kubeAiSchedulerInformerFactory`
   is now built from the api module's clientset and still serves all four listers —
   `.Scheduling().V2alpha2().PodGroups()`, `.Scheduling().V2().Queues()`, `.Scheduling().V1alpha2().BindRequests()`,
   and `.Kai().V1alpha1().Topologies()`. The only changes in `pkg/scheduler/cache/cache.go` and
   `pkg/scheduler/cache/cluster_info/data_lister/kubernetes_lister.go` are the import paths of the
   clientset/informer packages.
7. **CRD sync:** add and run `make sync-api-crds` (§5.3).

### 5.5 Validation gates

- **api repo:** `go build ./... && go test ./...`; `make generate manifests clients` verify-clean (both groups); CI green.
- **kai-scheduler** (with `replace => ../api`): `go mod tidy` clean; `make validate` (generated code current +
  fmt/vet); `make test` (unit + integration + envtest).
- **CRD contract unchanged:** byte-diff the post-migration
  `deployments/kai-scheduler/crds/{scheduling.run.ai_*,kai.scheduler_topologies}.yaml` (synced from the api
  repo) against the same files on pre-migration `main` — must be identical.
- **E2E:** `./hack/run-e2e-kind.sh` (or `ginkgo -r --randomize-all --focus api ./test/e2e/suites`) — CRDs install,
  Queue/PodGroup/BindRequest reconcile, Topology lister populates.

---

## 6. Versioning & release workflow

- **api repo:** change → test → semver tag (`vX.Y.Z`) → automated GitHub release. Semantic versioning on the
  API contract; `v0.x` until the contract is declared stable.
- **kai-scheduler:** `go get github.com/kai-scheduler/api@vX.Y.Z` → test → bump PR. `replace => ../api` is for
  local development only and must not be committed to release branches.
- **Automation:** Dependabot on the api module in kai-scheduler; one line in `CLAUDE.md` — "scheduling/Topology
  API changes land in `github.com/kai-scheduler/api` first, then bump kai-scheduler."

---

## 7. Risks & rollback

| Risk | Mitigation |
|---|---|
| 328-file import rewrite touches a `kai/v1` import by mistake | `kai/v1alpha1` rule is version-specific; review diff; `make validate` + compile catch stragglers |
| `kai.scheduler` group sourced from two repos | Distinct version packages + distinct CRD files; runtime-invisible; documented in §4.1 |
| Scheduling/Topology CRD drift between repos | api repo is source of truth; `sync-api-crds` + byte-diff gate |
| Accidental circular dependency | Verified none today; CI `go build` in the api repo fails fast if a back-edge is introduced |
| Two-repo dev friction | `replace => ../api` for local iteration; documented in CLAUDE.md |

**Rollback:** the migration lands as one kai-scheduler PR (go.mod bump + import rewrite + moved-dir removal +
codegen trim). Reverting that single PR restores the in-tree packages and drops the dependency; the api repo is
left untouched. No data migration, no CRD change — rollback is a `git revert`.

---

## 8. Ordered checklist

1. [ ] Reconcile & land this design doc.
2. [ ] Seed `github.com/kai-scheduler/api` from `main` — scheduling + Topology + utilities (§5.1); `go build`/`go test` green.
3. [ ] Relocate codegen for both groups; CI green; tag `v0.1.0` (§5.2).
4. [ ] Branch kai-scheduler: go.mod `require` + `replace`; import rewrite; remove moved dirs (§5.4 steps 1–4).
5. [ ] Delete kai-scheduler's client codegen; narrow deepcopy/CRD to `kai/v1` (§5.4 step 5).
6. [ ] Repoint the single informer factory + listers to the api module (§5.4 step 6).
7. [ ] `sync-api-crds`; `make validate` + `make test`; CRD byte-diff; e2e (§5.5).
8. [ ] Establish versioning/Dependabot; update CLAUDE.md (§6).
9. [ ] Delete the stale `siormeir/migrate-to-api-repo` branch + stash.
