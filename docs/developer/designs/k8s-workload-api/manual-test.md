<!-- GuyTodo: Remove file -->
# Workload API — manual test runbook

End-to-end manual verification of the upstream Kubernetes Workload API
(`scheduling.k8s.io/v1alpha1`, KEP-4671) translation layer on a local
kind cluster. Pairs with `README.md` (the design) and the automated
specs in `pkg/podgrouper/integration_tests` and
`test/e2e/suites/workload`.

## Prerequisites

A kind cluster running k8s 1.35+ with the `GenericWorkload` feature
gate enabled and KAI Scheduler installed:

```bash
docker buildx use desktop-linux   # see notes below
./hack/setup-e2e-cluster.sh --feature-config workload-api-enabled --local-images-build
kubectl config use-context kind-e2e-kai-scheduler
```

Sanity check the API is exposed:

```bash
kubectl api-resources --api-group=scheduling.k8s.io | grep workloads
# workloads  scheduling.k8s.io/v1alpha1  true  Workload
```

If that line is missing, the feature gate didn't take.

> **Note (Mac, Docker Desktop):** the default buildx builder on this
> machine is `multiplatform-builder` (docker-container driver), which
> keeps images in the build cache only — neither `docker push` nor
> `kind load` can find them. Switch builder before running setup, as
> shown above. If a previous attempt failed and the helm release is
> stuck on `crd-upgrader` ImagePullBackOff:
>
> ```bash
> kubectl -n kai-scheduler delete job crd-upgrader post-delete-cleanup --wait=false
> helm uninstall kai-scheduler -n kai-scheduler --no-hooks
> ```
>
> then rerun setup.

## Run

Open two terminals.

### Terminal A — log tail (leave running)

```bash
kubectl -n kai-scheduler logs -l app.kubernetes.io/name=podgrouper -f
```

### Terminal B — tests

#### Setup namespace + Queue

KAI Queue resources use raw numbers, not Kubernetes resource strings:
CPU is in **millicpus** (1000 = 1 CPU), memory in **MB** (1 = 1,000,000
bytes). Pods route to a queue via the `kai.scheduler/queue` label on the
Pod (or its top owner) — namespace labels are not used.

```bash
kubectl create ns wl-demo

cat <<'EOF' | kubectl apply -f -
apiVersion: scheduling.run.ai/v2
kind: Queue
metadata: {name: demo}
spec:
  parentQueue: default-parent-queue
  resources:
    cpu:    {quota: 4000, limit: 8000, overQuotaWeight: 1}
    memory: {quota: 4096, limit: 8192, overQuotaWeight: 1}
    gpu:    {quota: 0,    limit: -1,   overQuotaWeight: 1}
EOF
```

#### Test 1 — Gang policy creates one PodGroup with the right MinMember

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: scheduling.k8s.io/v1alpha1
kind: Workload
metadata: {namespace: wl-demo, name: my-training}
spec:
  podGroups:
  - name: workers
    policy:
      gang: {minCount: 2}
EOF

for i in 0 1; do
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  namespace: wl-demo
  name: worker-$i
  labels: {kai.scheduler/queue: demo}
spec:
  schedulerName: kai-scheduler
  workloadRef: {name: my-training, podGroup: workers, podGroupReplicaKey: "0"}
  containers:
  - {name: c, image: busybox, command: ["sleep","3600"], resources: {requests: {cpu: 100m}}}
EOF
done
```

Verify:

```bash
kubectl -n wl-demo get podgroups.scheduling.run.ai
# expect: my-training-workers-0  with MinMember=2

kubectl -n wl-demo get pod -o jsonpath='{range .items[*]}{.metadata.name}{"  "}{.metadata.annotations.pod-group-name}{"\n"}{end}'
# expect: both worker-0 and worker-1 routed to my-training-workers-0
```

#### Test 2 — Multi-podGroup Workload (independent gangs)

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: scheduling.k8s.io/v1alpha1
kind: Workload
metadata: {namespace: wl-demo, name: multi}
spec:
  podGroups:
  - {name: driver,  policy: {gang: {minCount: 1}}}
  - {name: workers, policy: {gang: {minCount: 4}}}
EOF

cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  namespace: wl-demo
  name: drv
  labels: {kai.scheduler/queue: demo}
spec:
  schedulerName: kai-scheduler
  workloadRef: {name: multi, podGroup: driver}
  containers: [{name: c, image: busybox, command: ["sleep","3600"]}]
---
apiVersion: v1
kind: Pod
metadata:
  namespace: wl-demo
  name: wkr
  labels: {kai.scheduler/queue: demo}
spec:
  schedulerName: kai-scheduler
  workloadRef: {name: multi, podGroup: workers, podGroupReplicaKey: "0"}
  containers: [{name: c, image: busybox, command: ["sleep","3600"]}]
EOF
```

Verify:

```bash
kubectl -n wl-demo get podgroups.scheduling.run.ai
# expect: multi-driver (MinMember=1) AND multi-workers-0 (MinMember=4)
```

#### Test 3 — Mutation propagation

This is the path the orphan-guard fix unblocks (Pods with only a
`workloadRef` and no controller owner). Pin a non-default starting value
first — `train` happens to be the default fallback when no
`priorityClassName` label is set, so a `→ train` mutation would read
identically before and after and prove nothing.

```bash
# Pin a non-default starting value and wait for it to land.
kubectl -n wl-demo label workload my-training priorityClassName=build --overwrite
until [ "$(kubectl -n wl-demo get podgroup my-training-workers-0 -o jsonpath='{.spec.priorityClassName}')" = "build" ]; do sleep 1; done
echo "PG before: $(kubectl -n wl-demo get podgroup my-training-workers-0 -o jsonpath='{.spec.priorityClassName}')"

# Mutate to a different real PriorityClass and wait for propagation.
kubectl -n wl-demo label workload my-training priorityClassName=inference --overwrite
until [ "$(kubectl -n wl-demo get podgroup my-training-workers-0 -o jsonpath='{.spec.priorityClassName}')" = "inference" ]; do sleep 1; done
echo "PG after:  $(kubectl -n wl-demo get podgroup my-training-workers-0 -o jsonpath='{.spec.priorityClassName}')"
```

`build` → `inference` round-trip proves the watcher → reconciler →
`ApplyOverride` → `ApplyToCluster` path is wired end-to-end.

#### Test 4 — Spec is immutable (upstream sanity check)

```bash
kubectl -n wl-demo edit workload my-training
# Try to change spec.podGroups[0].policy.gang.minCount from 2 to 5 and save.
# Expect rejection: "podGroups: Invalid value: ...: field is immutable"
```

Apiserver behaviour, not KAI — confirms why mutation tests must go through
mutable label/annotation surfaces.

#### Test 5 — Deletion preserves the existing PodGroup

```bash
kubectl -n wl-demo delete workload my-training

# PodGroup must remain — the existing gang isn't disrupted.
kubectl -n wl-demo get podgroup my-training-workers-0
# expect: still there

# A new pod referencing the now-missing Workload stays Pending and gets no PodGroup.
cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  namespace: wl-demo
  name: orphan
  labels: {kai.scheduler/queue: demo}
spec:
  schedulerName: kai-scheduler
  workloadRef: {name: my-training, podGroup: workers}
  containers: [{name: c, image: busybox, command: ["sleep","3600"]}]
EOF

# Tail in Terminal A — you should see:
#   "Pod references a missing Workload; staying pending"

sleep 3
kubectl -n wl-demo get pod orphan
# expect: Pending
kubectl -n wl-demo get podgroup my-training-workers
# expect: Error from server (NotFound)
```

#### Test 6 — Instant recovery

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: scheduling.k8s.io/v1alpha1
kind: Workload
metadata: {namespace: wl-demo, name: my-training}
spec:
  podGroups:
  - name: workers
    policy: {gang: {minCount: 1}}
EOF

sleep 3
kubectl -n wl-demo get podgroup my-training-workers
kubectl -n wl-demo get pod orphan -o jsonpath='{.metadata.annotations.pod-group-name}{"\n"}'
# expect: my-training-workers
```

#### Test 7 — Opt-out

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  namespace: wl-demo
  name: optout
  labels: {kai.scheduler/queue: demo}
  annotations: {kai.scheduler/ignore-workload-api: "true"}
spec:
  schedulerName: kai-scheduler
  workloadRef: {name: my-training, podGroup: workers}
  containers: [{name: c, image: busybox, command: ["sleep","3600"]}]
EOF

sleep 3
kubectl -n wl-demo get pod optout -o jsonpath='{.metadata.annotations.pod-group-name}{"\n"}'
# expect: pg-optout-<uid>, NOT my-training-workers
```

#### Test 8 — Top owner + Workload, override wins on collision

A `batch/v1.Job` is the top owner (built-in, no operator install needed).
PyTorchJob, MPIJob, and any other top-owner plugin behave identically — the
Workload override is post-dispatch, so it layers on top of whatever base
metadata the top-owner plugin produced.

The Job sets `kai.scheduler/queue=demo` and would normally drive a
`pg-{job}-{uid}` PodGroup with `MinMember=1`. The Workload overrides queue,
name, and `MinMember` simultaneously.

Setup the second Queue first (Workload routes here, Job routes to `demo`):

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: scheduling.run.ai/v2
kind: Queue
metadata: {name: ml-training}
spec:
  parentQueue: default-parent-queue
  resources:
    cpu:    {quota: 4000, limit: 8000, overQuotaWeight: 1}
    memory: {quota: 4096, limit: 8192, overQuotaWeight: 1}
    gpu:    {quota: 0,    limit: -1,   overQuotaWeight: 1}
EOF
```

Now create the Workload + Job:

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: scheduling.k8s.io/v1alpha1
kind: Workload
metadata:
  namespace: wl-demo
  name: cross-training
  labels: {kai.scheduler/queue: ml-training}
spec:
  podGroups:
  - name: workers
    policy: {gang: {minCount: 2}}
---
apiVersion: batch/v1
kind: Job
metadata:
  namespace: wl-demo
  name: trainer
  labels: {kai.scheduler/queue: demo}
spec:
  parallelism: 2
  completions: 2
  template:
    metadata:
      labels: {kai.scheduler/queue: demo}
    spec:
      schedulerName: kai-scheduler
      restartPolicy: Never
      workloadRef:
        name: cross-training
        podGroup: workers
        podGroupReplicaKey: "0"
      containers:
      - {name: c, image: busybox, command: ["sleep","3600"], resources: {requests: {cpu: 100m}}}
EOF
```

Verify the Workload-derived metadata wins, but ownership stays with the Job:

```bash
# Name comes from the Workload, NOT pg-trainer-<uid>.
kubectl -n wl-demo get podgroups.scheduling.run.ai
# expect: cross-training-workers-0  with MinMember=2

# Queue is the Workload's, not the Job's.
kubectl -n wl-demo get podgroup cross-training-workers-0 -o jsonpath='{.spec.queue}{"\n"}'
# expect: ml-training

# MinMember is the Workload's gang.minCount, not the Job plugin's default of 1.
kubectl -n wl-demo get podgroup cross-training-workers-0 -o jsonpath='{.spec.minMember}{"\n"}'
# expect: 2

# Ownership stays with the top owner (the Job), not the Workload.
kubectl -n wl-demo get podgroup cross-training-workers-0 -o jsonpath='{.metadata.ownerReferences[0].kind}/{.metadata.ownerReferences[0].name}{"\n"}'
# expect: Job/trainer
```

#### Test 9 — Top owner + Workload, base metadata falls through

When the Workload doesn't carry a key, the override is a no-op for that
field — the base metadata produced by the top-owner plugin survives. Here
the Job sets `priorityClassName=build` and the Workload sets neither
priority nor queue, so the PodGroup ends up with `build` (from the Job)
but its name still comes from the Workload.

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: scheduling.k8s.io/v1alpha1
kind: Workload
metadata: {namespace: wl-demo, name: trainer-fallthrough}
spec:
  podGroups:
  - name: workers
    policy: {gang: {minCount: 1}}
---
apiVersion: batch/v1
kind: Job
metadata:
  namespace: wl-demo
  name: trainer-ft
  labels:
    kai.scheduler/queue: demo
    priorityClassName: build
spec:
  parallelism: 1
  completions: 1
  template:
    metadata:
      labels:
        kai.scheduler/queue: demo
        priorityClassName: build
    spec:
      schedulerName: kai-scheduler
      restartPolicy: Never
      workloadRef: {name: trainer-fallthrough, podGroup: workers, podGroupReplicaKey: "0"}
      containers:
      - {name: c, image: busybox, command: ["sleep","3600"], resources: {requests: {cpu: 100m}}}
EOF
```

Verify:

```bash
# Name comes from the Workload (override path), but...
kubectl -n wl-demo get podgroup trainer-fallthrough-workers-0 -o jsonpath='{.metadata.name}{"\n"}'
# expect: trainer-fallthrough-workers-0

# ...priorityClassName falls through from the Job's label since the Workload
# carries no priorityClassName label.
kubectl -n wl-demo get podgroup trainer-fallthrough-workers-0 -o jsonpath='{.spec.priorityClassName}{"\n"}'
# expect: build
```

## Cleanup

```bash
kubectl delete ns wl-demo                    # keeps the cluster
kind delete cluster --name e2e-kai-scheduler # tears it down
```

## Coverage map

| Test | Design section | Automated coverage |
|------|----------------|--------------------|
| 1    | §1, §2 Gang    | unit + integration + e2e |
| 2    | §1 multi-podGroup | integration |
| 3    | §3 fallback chain, mutation contract | integration |
| 4    | upstream apiserver | n/a (k8s) |
| 5    | §4 deletion contract | integration |
| 6    | §4 instant recovery | e2e |
| 7    | §5 opt-out | unit + integration + e2e |
| 8    | §3 override precedence (Workload wins) on a real top owner | unit + integration |
| 9    | §3 fallback chain (Workload absent → base survives) on a real top owner | unit + integration |
