# Semi-Preemptible Demo

Two scenarios against a KAI cluster with the [semi-preemptible](../docs/elastic/README.md#semi-preemptible-workloads)
mode built in. Both use plain CPU pods, so any kind cluster will do.

> **Alpha.** Semi-preemptible behavior may change between releases.

## Scenario A — the elastic tier absorbs the hit

```bash
kubectl apply -f 00-setup.yaml
kubectl apply -f 01-low-semi-preemptible.yaml
```

Four leaf subgroups, `minSubGroup: 2`. All four are schedulable, so the job bursts to 400m while
only the two core subgroups are charged as non-preemptible:

```console
$ kubectl get podgroup low-job -n kai-demo-low -o jsonpath='{.status.schedulingState}'
{"corePods":["low-sg-0","low-sg-1"]}

$ kubectl get queue demo-low -o jsonpath='{.status.allocated} {.status.allocatedNonPreemptible}'
{"cpu":"400m"} {"cpu":"200m"}
```

`corePods` names exactly which pods the scheduler protects. Now bring in a higher-priority
non-preemptible job that needs 200m the parent queue does not have spare:

```bash
kubectl apply -f 02-high-non-preemptible.yaml
```

`low-sg-2` and `low-sg-3` are reclaimed, `low-sg-0` and `low-sg-1` keep running, and both queues
settle at 200m allocated / 200m non-preemptible. The elastic tier took the whole hit and the core
never moved.

## Scenario B — a subgroup that cannot form its gang

Reset first (`00-setup.yaml` must stay applied — `03` re-cuts only the queue numbers):

```bash
kubectl delete -f 02-high-non-preemptible.yaml -f 01-low-semi-preemptible.yaml
kubectl apply -f 03-broken-gang.yaml
```

`sg-0` and `sg-1` each get their 2 pods and form; `sg-2` gets 1 of the 2 it needs. That pod is never
admitted — a partial gang does no work, so it would hold capacity for nothing:

```console
$ kubectl get pods -n kai-demo-low
broken-sg-0-a        Running
broken-sg-0-b        Running
broken-sg-1-a        Running
broken-sg-1-b        Running
broken-sg-2-orphan   Pending
```

The job is also **not stale**: gang staleness honors `minSubGroup`, so meeting 2 of 3 keeps the job
alive rather than evicting all of it once the grace period elapses.

```bash
kubectl apply -f 04-reclaimer.yaml
```

The reclaimer takes the 100m of headroom `sg-2` never claimed. Nothing is reclaimed and the four
core pods are untouched — capacity a partial gang cannot use goes to a workload that can.

## Teardown

```bash
kubectl delete queue,ns,priorityclass -l app=demo-semi-preemptible
```
