# Mokka NUMA CI experiment

This setup is part of upstream draft PR #2543. The dedicated workflow builds KAI
from the PR and runs on a GitHub-hosted runner, without adding a VM.

## Stack

The `numa-full` kind layout has four GPU workers, with eight A100 GPUs each,
split 4+4 between two zones. Two workers use `single-numa-node`; one uses
`restricted`; one uses `best-effort`. A CPU worker runs the control services.

Mokka is pinned to `7085b09b5c80ffc984e6e8f5ca1bce1f19b24506` in the public
`JRosenboimNVIDIA/mokka` fork. It includes Roman's GPU Operator support, the
memory-limit fix, the current NUMA review changes, and the node-bus fix that lets
NFD read memory capacity in every simulated zone. It also includes bidirectional
host mounts, the PodResources socket health check, and a shared NUMA layout
resolved by the agent enricher before all staging. Each selected node uses:

```yaml
nodeAgent:
  numa:
    enabled: true
    topologyManagerPolicy: restricted # varies by pool
    cpuManagerPolicy: static
    memoryManagerPolicy: static
    systemReservedMemory: 256Mi
    systemReservedCPU: 100m
```

Mokka configures a fractional CPU reservation and whole shared-pool CPU IDs, configures memory
reservations, backs up manager checkpoints, and restarts kubelet. The privileged
pod applies simulation and bind-mounts the modified kubelet configuration. The
node agent registers boot cleanup with an empty persistent marker. The helper
clears manager checkpoints before kubelet starts after a node reboot. Restore
simulation before changing active policies; use fresh or drained test nodes.

CPU Manager assigns real CPU IDs. Memory Manager keeps simulated memory groups
in its own state, exposed by the real kubelet pod-resources socket. Mokka's NRI
plugin maps simulated memory-node masks to kernel-allowed nodes when creating
or updating containers. CPU IDs and memory limits are preserved. This tests
scheduler decisions and observations; it does not prove physical memory locality.

GPU Operator v26.7.2 runs its real driver, toolkit, and device plugin. Its NFD
Topology Updater publishes GPU, CPU, and memory resources. NFD snapshots capacity
at startup, so setup restarts it after all eight GPUs register on every GPU worker.
There is no FGO, fake kubelet socket, or manually created NRT object.

## CI resource budget

On a four-CPU runner the width-two probe can request all three exclusive CPUs.
Control Deployments are placed on the CPU worker. A `100m` system CPU reservation
makes kubelet keep one shared CPU ID while advertising 3.9 allocatable CPU on a
four-CPU node; the real exclusive CPU inventory remains three. This leaves room
for ordinary agent requests alongside a three-CPU probe. Kindnet stays Burstable
so it does not hold a permanent single-zone memory group. No CPU capacity is added.

The registry is moved before building/pushing KAI images. It has ephemeral
storage; moving it after pushing would discard those images.

## Tests and reports

The full `numa` label includes nightly cases. Existing expectations are unchanged.
Added cases check CPU/GPU placement, and that real containers become Ready with
CPU, GPU, and memory allocations in both zones. JSON/JUnit reports and cluster
logs are uploaded even if setup or tests fail.

The unchanged `Burstable cross-zone request passes through` test expects admission
that conflicts with current KAI GPU alignment rules. It can fail, causing the next
case in that ordered group to skip. CPU/memory cases must execute with this setup;
missing topology fails the initial check instead of silently skipping all of them.

The local scheduler and placement exporter must include the current memory-group
support. v0.18.6 does not publish the observed memory-group annotation used by
these tests. CI builds the current PR components.

## Run on a disposable Docker host

```bash
export CLUSTER_NAME=mokka-numa-ci GPU_STACK=mokka FEATURE_CONFIG=numa-full
export PACKAGE_VERSION=0.0.0-numa-local DOCKER_BUILDX_ADDITIONAL_ARGS=--load
./hack/setup-e2e-cluster.sh --local-images-build --skip-kai-install --feature-config numa-full
python3 hack/check-mokka-numa.py
helm upgrade --install kai-scheduler "./charts/kai-scheduler-$PACKAGE_VERSION.tgz" \
  --namespace kai-scheduler --create-namespace \
  --values hack/kai-scheduler-mokka-values.yaml \
  --values hack/kai-scheduler-mokka-numa-values.yaml \
  --set global.registry=localhost:30100 --set prometheus.enabled=true \
  --wait --timeout 10m
go tool ginkgo --keep-going --trace -vv --label-filter numa --timeout 45m \
  --output-dir=numa-results --json-report=report.json --junit-report=junit.xml \
  ./test/e2e/suites/numa
kind delete cluster --name "$CLUSTER_NAME"
```
