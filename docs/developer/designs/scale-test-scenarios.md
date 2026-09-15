# Customer-driven scale test scenarios

Tracking issue: [#1981](https://github.com/kai-scheduler/KAI-Scheduler/issues/1981)

## Context

Issue #1981 asks to expand the scale benchmark beyond today's "fill the cluster / reclaim single-GPU jobs"
set with five scenarios drawn from real customer stories (Nebius disaggregated inference, frontier-lab hero
jobs and RL pipelines, robotics fractions).

The suite in [`test/e2e/scale/`](../../../test/e2e/scale) already has the whole harness: KWOK node pools
driven by kwok-operator, a generated 3-level topology tree, fake-gpu-operator, and `writeTestResults` →
S3 → dashboard. What is missing is scenario coverage: **no sub-groups, no required topology, no elastic
gangs, no multi-queue mix, no GPU fractions** anywhere in the scale suite.

Outcome: five new specs, tagged so the daily run skips them, sharing a small set of new creation helpers.
Setup cost stays at zero — every new spec nests inside an existing `Ordered` context that already builds its
node pools.

## Constraints

- New specs must not run in the daily benchmark. The runner lives in `github.com/runai/runai-engine`, not
  in this repo — this repo only provides the specs and the label to filter on.
- Setup/teardown overhead must stay minimal → reuse the existing `Topology` and `Big cluster` contexts.
- Must use KWOK + kwok-operator, like the rest of the suite.

## Placement (no new cluster setup)

| # | Scenario | Host context in `kwok_test.go` | Cluster |
|---|---|---|---|
| 1 | Disaggregated inference allocate + reclaim | `Context("Topology")` | 512 nodes, zone/block/rack |
| 3 | Hero job reclaim + required topology | `Context("Topology")` | same |
| 2 | Elastic distributed large job reclaim | `Big cluster` → `Whole GPU Tests` → `Reclaim` | `NODE_COUNT` nodes |
| 4 | Multi-queue small-mid mixed workloads | `Big cluster` → new `Context("Mixed Workloads")` | same |
| 5 | Fractions allocate | `Big cluster` → new `Context("Fractions")` | same |

## Exclusion from the daily run

1. Add `ScaleExtended = "scale-extended"` and `Fractions = "fractions"` to
   `test/e2e/modules/constant/labels/labels.go`.
2. Tag every new spec: `It("...", Label(labels.ScaleExtended), func(ctx context.Context) {...})`. Specs still
   inherit `Label(labels.Scale)` from the top-level `Describe`, so they run under an explicit
   `--label-filter 'scale-extended'`.
3. The fractions context carries **both** labels — `Context("Fractions", Label(labels.ScaleExtended, labels.Fractions), ...)`
   — so the fraction specs can be selected (`'fractions'`) or skipped (`'scale-extended && !fractions'`)
   on their own. They are the only scenario with an external dependency (reservation pods, see below), so
   they need to be togglable independently of the other four. Same mechanism `labels.NCCL` already uses.
4. Default the filter **inside the suite** so no runai-engine change is required. `scale_suite_test.go`
   currently calls bare `RunSpecs`; make it seed the label filter, keeping an explicit `--label-filter`
   authoritative:

   ```go
   cfg, rep := GinkgoConfiguration()
   if cfg.LabelFilter == "" {
       cfg.LabelFilter = cmp.Or(os.Getenv("SCALE_LABEL_FILTER"), defaultScaleLabelFilter)
   }
   RunSpecs(t, "Scale Suite", cfg, rep)
   ```

   with `defaultScaleLabelFilter = "!scale-extended"`. The daily run keeps invoking ginkgo exactly as it does
   today and simply stops before the new specs.
5. Document both labels in [`docs/developer/scale-tests.md`](../scale-tests.md).

## Helper changes

### `test/e2e/scale/kwok_job_creation.go` — collapse three wrappers into one

`createDistributedJobForKwok` / `submitDistributedJobForKwok` take a fixed positional parameter list that
already needs `MinMember` (elastic), `PriorityClassName` and `NamePrefix`. Replace both with an options
pass-through — a net deletion:

```go
func kwokJobOpts(opts rd.DistributedBatchJobOptions) rd.DistributedBatchJobOptions {
	opts.PodSpecMutator = addKWOKTaintsAndAffinity
	return opts
}
```

Callers use `rd.CreateDistributedBatchJob(ctx, testCtx.ControllerClient, q, kwokJobOpts(rd.DistributedBatchJobOptions{...}))`
directly. Update the ~6 existing call sites in `kwok_test.go` / `kwok_test_functions.go`.
`rd.DistributedBatchJobOptions` (`test/e2e/modules/resources/rd/distributed_batch_job.go`) already carries
`MinMember`, `TopologyConstraint`, `Preemptibility` and `PriorityClassName` — nothing new is needed there.

### New `test/e2e/scale/kwok_subgroups.go` — sub-group pod groups on KWOK

`pod_group.CreateWithHierarchy` (`test/e2e/modules/resources/rd/pod_group/pod_group.go`) cannot be reused
as-is: it creates pods serially and has no `PodSpecMutator`, so pods never get the KWOK toleration/affinity.
Add a scale-local builder (~50 lines) that reuses `pod_group.Create`, `v2alpha2.SubGroup`,
`rd.CreatePodWithPodGroupReference`, `constants.SubGroupLabelKey`, `addKWOKTaintsAndAffinity` and
`rd.CreateObjectWithRetries`, creating pods concurrently:

```go
type subGroupSpec struct {
	name      string
	pods      int
	resources v1.ResourceRequirements
	topology  *v2alpha2.TopologyConstraint // per-subgroup constraint
}

func createSubGroupPodGroupForKwok(
	ctx context.Context, testCtx *testcontext.TestContext, q *v2.Queue,
	pgName string, pgTopology *v2alpha2.TopologyConstraint, subGroups []subGroupSpec,
) (*v2alpha2.PodGroup, []*v1.Pod, error)
```

Sets `Spec.MinMember = nil`, `Spec.MinSubGroup = len(subGroups)`, per-subgroup `MinMember` and
`TopologyConstraint`, and `Spec.TopologyConstraint` from `pgTopology`. Webhook rules in
`podgroup_webhook.go` are satisfied: flat leaves only, `MinSubGroup` on the PodGroup.

### `test/e2e/scale/kwok_test_functions.go` — new resource vars and one metric helper

- `CPUOnlyRequirement` (2 CPU / 4Gi) alongside `SingleGPURequirement` / `FullNodeGPURequirement`.
- `fractionJobRequirement(fraction string)` → job whose pod template carries the `constants.GpuFraction`
  annotation and empty resources (pattern from `test/e2e/suites/allocate/resources/resources_specs.go`).
- `measureTimeToScheduleAll(ctx, testCtx, q, expectedPods) time.Duration` — thin wrapper over the existing
  `waitForAllJobsToSchedule`, so every new spec ends in the standard `writeTestResults(name, true, map[...])`.

## Scenarios

All sizes derive from `numberOfNodes` / `totalNodes` so the specs scale with `NODE_COUNT`.

### 1. Disaggregated inference — allocate + reclaim (`Topology` context)

One "inference deployment" = a PodGroup with three sub-groups, PG-level `RequiredTopologyLevel: zone`:

| Sub-group | Pods | Per-pod | Sub-group constraint |
|---|---|---|---|
| `prefill` | 4 | 8 GPU | required `rack` |
| `decode` | 8 | 4 GPU | preferred `block` |
| `frontend` | 2 | CPU only | none |

- **Allocate**: scheduler disabled, submit `totalNodes / nodesPerInferenceJob` deployments into the victim
  queue, enable scheduler, measure time until all pods are scheduled.
- **Reclaim**: victim queue quota set to 0; submit one identically-structured deployment into a second queue
  with quota, and measure time until all of its pods are scheduled.
- Metrics: `time to allocate`, `time to reclaim`, deployment count, pod count. Expectation: a couple of minutes.

### 2. Elastic distributed large job reclaim (`Reclaim` context)

- Victim: 1 elastic PodGroup modeling a distributed job, with `numberOfNodes` pods × 8 GPU and
  `MinMember = pod count/2` (the "1/2x → 1/x" scale-in), in the 0-quota queue. Standalone pods prevent a
  workload controller from replacing expected reclaim victims.
- Reclaimer: gang job of `numberOfNodes/2` pods × 8 GPU in the quota queue.
- Assert the reclaimer schedules and the victim retains exactly `MinMember` running pods — i.e. it was
  shrunk, not killed — via `waitForElasticVictimState`.
- Metrics: time to reclaim, surviving victim pods.

### 3. Hero job reclaim + required topology (`Topology` context)

- Fill the topology cluster with single-GPU victims in the 0-quota queue (`fillClusterWithJobs`).
- Hero job: `totalNodes/4` pods × 8 GPU with `RequiredTopologyLevel: zone`.
- Sizing deviation: with today's tree (4 zones × 8 blocks × 8 racks × 2 nodes) one zone = 128 nodes = 1/4 of
  the cluster, while the story asks for 1/2. Matching 1/2 exactly would mean reshaping `topologyLevels`
  (e.g. 2 zones × 4 nodes per domain), changing the shape the existing daily topology specs run against.
  Sizing is a const and easy to revisit.
- Assert all hero pods land under a single zone label value. Metric: time to reclaim. Expectation: < 10 min.

### 4. Multi-queue small-mid mixed workloads (new `Mixed Workloads` context)

- 5 sibling queues under `parentQueue`, each deserved 1/5 of cluster GPUs.
- Per queue, concurrently:
  - **RL hierarchical gang**: PodGroup with `learner` (4 pods × 4 GPU) + `env` (16 pods, CPU only),
    `MinSubGroup = 2` — via `createSubGroupPodGroupForKwok`.
  - **Data preprocessing**: 50 CPU-only non-gang batch jobs (`createJobObjectForKwok` + `CPUOnlyRequirement`).
  - Each job sized 5–10% of the cluster.
- Metrics: max per-job scheduling latency, plus unschedulable-decision latency for one deliberately oversized
  job, reusing `measureUnschedulableDelayInSeconds`. Target < 2 min.

### 5. Fractions allocate (new `Fractions` context, `Label(labels.ScaleExtended, labels.Fractions)`)

- Target 35% of cluster GPU capacity. Each fraction configuration gets 17.5%: half the participating GPUs
  run 8 × `0.125` pods and half run 2 × `0.5` pods. Round each configuration down to whole GPUs to
  preserve the equal split. This creates 7,000 pods at 500 nodes and 28,000 pods at 2,000 nodes.
- Create fraction jobs with at most 300 workers while the scheduler is disabled. Even a node filled only
  with `0.125` pods has 64 workload pods plus 8 reservation pods, below the 110 pods/node KWOK limit.
- Scheduler disabled during submission and enabled to start the clock — the existing `fillClusterWithJobs` idiom.
- Metric: time to schedule all fraction pods. Expectation: seconds to a couple of minutes.

**Prerequisite spike (do this first).** Fraction binding blocks until the fake-gpu-operator status-updater
annotates each reservation pod with `run.ai/gpu-index` (see
`pkg/binder/binding/resourcereservation/resource_reservation.go`, `waitForGPUReservationPodAllocation`).
Reservation pods are created with `NodeName` set, so they bypass scheduling, but this path has never run
against KWOK nodes and `hack/setup-scale-test-env.sh` deletes the device-plugin and dcgm-exporter. Verify
manually on a scale cluster: submit ~10 `gpu-fraction: "0.5"` pods with KWOK affinity and confirm they reach
`Running`. If they hang, drop scenario 5 from this PR and open a separate issue for fraction support on KWOK.

## Running the new scenarios manually

The CI that provisions the scale cluster and deploys the test runner pod lives in
`github.com/runai/runai-engine`; this repo contains no scale workflow. `NODE_COUNT` already reaches the suite
as an environment variable on that pod, so `SCALE_LABEL_FILTER` (above) rides the same plumbing and needs no
runner code change — set it on the runner pod, or export it before a local run.

| Goal | Invocation |
|---|---|
| Daily run (unchanged) | `ginkgo -v ./test/e2e/scale` → filter defaults to `!scale-extended` |
| All five new scenarios | `SCALE_LABEL_FILTER=scale-extended ginkgo -v ./test/e2e/scale` |
| Only fractions | `SCALE_LABEL_FILTER=fractions ginkgo -v ./test/e2e/scale` |
| New scenarios except fractions | `SCALE_LABEL_FILTER='scale-extended && !fractions' ginkgo -v ./test/e2e/scale` |
| Everything, old and new | `SCALE_LABEL_FILTER='' ginkgo -v --label-filter '' ./test/e2e/scale` |

An explicit `--label-filter` on the ginkgo command line always wins over the env var, so the runai-engine
runner can also be switched to `'scale && !nccl && !scale-extended'` later without touching the suite.

Prerequisite in every case: a cluster with KAI installed and `./hack/setup-scale-test-env.sh` already applied.
Run from inside the cluster (runner pod) for meaningful numbers; a local kind cluster with `NODE_COUNT=24` is
fine for checking that a spec works at all, but its timings are not benchmark data.

## Files

- `test/e2e/modules/constant/labels/labels.go` — add `ScaleExtended` and `Fractions`.
- `test/e2e/scale/scale_suite_test.go` — seed the default label filter from `SCALE_LABEL_FILTER`.
- `test/e2e/scale/kwok_job_creation.go` — collapse wrappers into the options pass-through.
- `test/e2e/scale/kwok_subgroups.go` — **new**, sub-group PodGroup builder.
- `test/e2e/scale/kwok_test_functions.go` — new resource vars, fraction job helper, scenario bodies.
- `test/e2e/scale/kwok_test.go` — five new specs, two new contexts, reclaim/mixed queues.
- `docs/developer/scale-tests.md` — document `scale-extended` and the new scenarios.
- No changelog fragment — tests only; apply the `skip-changelog` label.

## Verification

1. `make lint` and `make validate`.
2. Label wiring, no cluster needed:
   - `ginkgo --dry-run -v ./test/e2e/scale` — the default filter applies; must list exactly today's specs,
     none of the new five.
   - `SCALE_LABEL_FILTER=scale-extended ginkgo --dry-run -v ./test/e2e/scale` — exactly the five new specs.
   - `SCALE_LABEL_FILTER=fractions ginkgo --dry-run -v ./test/e2e/scale` — only the fraction specs.
   - `ginkgo --dry-run -v --label-filter 'scale-extended' ./test/e2e/scale` — the explicit CLI flag overrides
     the default, same five specs.
3. Small-cluster smoke run against a kind cluster prepared with `hack/setup-scale-test-env.sh`:
   `NODE_COUNT=24 ginkgo -v --label-filter 'scale-extended' ./test/e2e/scale`.
   Confirm each spec schedules, `kwok_scale_test.json` gains one entry per scenario with sane timings, and
   the `AfterAll` teardown restores node pools (`kubectl get nodepools`, `kubectl get nodes -l type=kwok`).
4. Fractions spike (above) before wiring scenario 5 into the suite.
5. Full-scale run at `NODE_COUNT=500` on the scale cluster before merging.
