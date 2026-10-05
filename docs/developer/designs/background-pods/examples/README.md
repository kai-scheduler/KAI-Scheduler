# Background pods in a zero-quota queue

A workaround for [#1936](https://github.com/kai-scheduler/KAI-Scheduler/issues/1936): instead of teaching KAI to ignore background/health-check pods, put them in a dedicated queue whose fair share is permanently zero.
Every user queue is then always entitled to reclaim from it.

```
research-dept (64 GPU)          maintenance (0 GPU, weight 0)
├── team-a (32 GPU)
└── team-b (32 GPU)
```

The queue has `quota: 0`, `overQuotaWeight: 0`, `limit: -1`:

- **quota 0 + overQuotaWeight 0** → fair share is always 0, so everything the queue holds is above its fair share and reclaimable.
- **limit -1** → unbounded, so background pods are still placed while the cluster is idle.

`maintenance` is a single flat root queue — nothing requires a workload's queue to be a leaf, and root queues skip the parent/child quota validation in `pkg/admission/webhook/queuehooks`.
Being a root of its own is also what keeps it insulated from changes in the user hierarchy.

> Note: under `projectLevelFairness` the scheduler only snapshots queues that *have* a parent (`pkg/scheduler/cache/cluster_info/queue.go:68`), so a parentless queue would be dropped.
> This demo assumes the default `fullFairness`.

## Assumed cluster

8 GPU workers x 8 GPUs = 64 GPUs.
No node selectors anywhere — the health-check pods spread one per node using required pod anti-affinity on `kubernetes.io/hostname`, and the scheduler picks which nodes the user workloads displace.

## Run

```bash
kubectl apply -f 00-priorityclass.yaml
kubectl apply -f 01-queues.yaml
kubectl apply -f 02-background-pods.yaml

# 8 healthcheck pods Running, one per node, 8 GPUs held by `maintenance`.
kubectl get pods -o wide -l app=healthcheck
kubectl get queue maintenance -o jsonpath='{.status.allocated}{"\n"}'
```

**Demo A — targeted reclaim.** One pod needs 8 GPUs, and every node has only 7 free.
Exactly one healthcheck pod should be evicted.

```bash
kubectl apply -f 03-team-a-single-pod.yaml
kubectl get pods -o wide -l 'app in (healthcheck,user-workload)' -w
```

**Demo B — gang reclaim inside quota.** 4 x 8 GPUs = team-a's full 32-GPU quota, so four background pods go.
Delete Demo A first so the arithmetic stays exact.

```bash
kubectl delete -f 03-team-a-single-pod.yaml
kubectl apply -f 04-team-a-gang-job.yaml
```

**Demo C — drain.** team-b takes the other 32 GPUs; all eight healthcheck replicas end up Pending and stay there.

```bash
kubectl apply -f 05-team-b-gang-job.yaml
```

Watch the reclaim decisions:

```bash
kubectl get events -A --field-selector reason=Evict --sort-by=.lastTimestamp
kubectl logs -n kai-scheduler deploy/scheduler | grep -i reclaim
```

## Topology degradation

[`topology/`](topology/README.md) is a separate scenario showing a concrete failure of this workaround: background pods push a job with a preferred rack constraint into a split-rack placement, because allocate runs before reclaim and the naive placement already succeeded.

## Cleanup

```bash
kubectl delete -f 05-team-b-gang-job.yaml -f 04-team-a-gang-job.yaml \
  -f 03-team-a-single-pod.yaml -f 02-background-pods.yaml \
  -f 01-queues.yaml -f 00-priorityclass.yaml --ignore-not-found
```

## What to check while playing with this

These are the points where the workaround is expected to diverge from the real feature:

1. **Do the background pods get placed at all?** Fair share is 0, so allocation only works because the PodGroup is preemptible and `limit` is unlimited.
   If they sit Pending on an idle cluster, the workaround is dead on arrival.
   Their priority is 1, below the 100 non-preemptible threshold in `pkg/common/podgroup/preemptible.go`, which is what makes them preemptible.
2. **Every normal allocation becomes a reclaim.** That is the performance concern raised in the issue thread — worth timing session duration with and without the background pods present.
3. **Reclaim minimises victims.** The solver prefers evicting the fewest background pods, which is *better* than the "ignore capacity entirely" model but costs a scenario search per allocation.
4. **The pods still need a queue label.** This does not solve the operational burden in the original request — it only makes the queue a fixed one (`maintenance`) that never has to track hierarchy changes, since it is a root of its own.
   It also cannot cover pods whose spec you don't control.
5. **Starvation is permanent.** With `overQuotaWeight: 0` the background pods never come back while users hold the cluster.
   Real health-check pods probably want to return once capacity frees up.
6. **Anti-affinity vs. reclaim.** The replacement pod after an eviction is blocked both by GPU capacity and by anti-affinity against its surviving siblings — worth confirming the fit error reported is the one you expect.
