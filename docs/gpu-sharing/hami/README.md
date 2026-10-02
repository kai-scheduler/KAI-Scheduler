# HAMi Resource Isolation

KAI Scheduler's GPU sharing feature allows multiple pods to share a single GPU, but by default it does **not** enforce memory limits at the CUDA level — a container requesting 2000 MiB could still see (and use) the full GPU memory via `nvidia-smi` and CUDA APIs.

[kai-resource-isolator](https://github.com/Project-HAMi/KAI-resource-isolator) solves this by deploying [HAMi-core](https://github.com/Project-HAMi/HAMi-core), a CNCF-incubated CUDA interception library. HAMi-core hooks CUDA memory allocation calls via `LD_PRELOAD` and enforces per-container GPU memory limits, ensuring each container can only allocate up to its requested amount.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     KAI-Scheduler                        │
│                                                         │
│  1. Schedules pod to a GPU node                         │
│  2. Injects CUDA_DEVICE_MEMORY_LIMIT env var             │
│     based on gpu-fraction or gpu-memory annotation       │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│                kai-resource-isolator                     │
│                                                         │
│  3. Mutating webhook injects:                            │
│     - hostPath volume mount (/usr/local/vgpu)            │
│     - /etc/ld.so.preload → libvgpu.so                    │
│     - POD_UID / CONTAINER_NAME / CONTAINER_VGPU_MOUNT    │
│       (for per-container VRAM metrics)                   │
└─────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│                     Container                            │
│                                                         │
│  4. libvgpu.so intercepts CUDA memory allocation calls   │
│  5. Enforces limit set by CUDA_DEVICE_MEMORY_LIMIT       │
└─────────────────────────────────────────────────────────┘
```

`kai-resource-isolator` combines:

| Component | Role |
|---|---|
| DaemonSet (libsync) | Copies `libvgpu.so` (HAMi-core) to `/usr/local/vgpu` on every GPU node |
| Mutating webhook | Injects the `libvgpu` hostPath volume and `ld.so.preload` into pods that request GPU sharing |
| DaemonSet (monitor, optional) | Reads per-container shared-memory caches and exposes HAMi-compatible VRAM metrics on `:9394` |

![Architecture](https://github.com/user-attachments/assets/ac7566fe-f79c-45fc-b3a1-24bc18ea6bc9)

## Prerequisites

- **KAI-Scheduler version**: ≥ v0.17.0
- KAI-Scheduler deployed with GPU sharing and the `hamicore` plugin enabled:

  ```bash
  helm install kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
    --set global.gpuSharing=true \
    --set binder.plugins.hamicore.enabled=true \
    --namespace kai-scheduler --create-namespace \
    --version v0.17.0
  ```

## Installation

Deploy kai-resource-isolator (with optional per-container VRAM metrics):

```bash
helm install kai-resource-isolator oci://docker.io/projecthami/kai-resource-isolator \
  --namespace kai-resource-isolator --create-namespace \
  --set monitor.enabled=true \
  --set monitor.serviceMonitor.enabled=true \
  --version 1.1.0-chart
```

Chart versions carry a `-chart` suffix (e.g. `1.1.0-chart`). Available versions are listed on [Docker Hub](https://hub.docker.com/r/projecthami/kai-resource-isolator/tags).

The default `monitor.nodeSelector` is `nvidia.com/gpu.present: "true"` (NVIDIA GPU feature discovery). Set `monitor.runtimeClassName=nvidia` if NVML is only available through the NVIDIA runtime handler in your cluster.

For customization or more detail, see [kai-resource-isolator](https://github.com/Project-HAMi/KAI-resource-isolator).

## Usage

Once both KAI-Scheduler and kai-resource-isolator are deployed, any pod requesting GPU sharing via `gpu-fraction` or `gpu-memory` annotations will automatically receive memory isolation:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-sharing-with-isolation
  labels:
    kai.scheduler/queue: default-queue
  annotations:
    gpu-memory: "4096"  # in MiB, no suffix
spec:
  schedulerName: kai-scheduler
  containers:
    - name: gpu-workload
      image: nvidia/cuda:12.9.2-base-ubuntu24.04
      command: ["sleep", "infinity"]
```

After the pod starts, `nvidia-smi` inside the container will show only the allocated memory instead of the full GPU memory:

```
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 580.159.03             Driver Version: 580.159.03     CUDA Version: 13.0     |
+-----------------------------------------+------------------------+----------------------+
| GPU  Name                 Persistence-M | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap |           Memory-Usage | GPU-Util  Compute M. |
|                                         |                        |               MIG M. |
|=========================================+========================+======================|
|   0  Tesla T4                       On  |   00000000:00:04.0 Off |                    0 |
| N/A   43C    P8             16W /   70W |       0MiB /   4147MiB |      0%      Default |
|                                         |                        |                  N/A |
+-----------------------------------------+------------------------+----------------------+

+-----------------------------------------------------------------------------------------+
| Processes:                                                                              |
|  GPU   GI   CI              PID   Type   Process name                        GPU Memory |
|        ID   ID                                                               Usage      |
|=========================================================================================|
|  No running processes found                                                             |
+-----------------------------------------------------------------------------------------+
```

### Per-container VRAM metrics

When `monitor.enabled=true`, `kai-vgpu-monitor` reads the shared-memory cache that `libvgpu.so` writes for each GPU container and exposes HAMi-compatible gauges such as:

- `hami_vgpu_memory_used_bytes`
- `hami_vgpu_memory_limit_bytes`
- `hami_container_device_utilization_ratio`

Scrape them with:

```bash
curl {pod-ip}:9394/metrics
```


### Opt-out

- **Per pod**: add annotation `kai-resource-isolator.io/inject: "false"`
- **Per namespace**: add label `kai-resource-isolator.io/webhook=ignore`

### Memory value precision

The `gpu-memory` annotation accepts an **integer in MiB** (no unit suffix). Internally, KAI-Scheduler converts this to a GPU fraction with 2-decimal precision, which is then multiplied against the total GPU memory to compute the actual limit. As a result, the value seen in `nvidia-smi` may differ slightly from the requested value. For example, requesting `4096` MiB on a `15360` MiB GPU (T4) rounds to a `0.27` fraction, yielding `4147m` as the enforced limit.

## Local e2e testing

The HAMi-core e2e suite lives at `test/e2e/suites/integrations/third_party/hamicore/`. It checks KAI’s isolation contract (`CUDA_DEVICE_MEMORY_LIMIT` injection and limited `nvidia-smi` visible memory). It is **not wired into CI**: KAI PR e2e runs on kind with the fake GPU operator and has no real GPUs, so these specs soft-skip unless the `hamicore` binder plugin and the `kai-resource-isolator` mutating webhook are present.

Use a machine with a real NVIDIA GPU (for example minikube with `--gpus=all`). Docker must be able to run `docker run --rm --gpus all nvidia/cuda:12.6.0-base-ubuntu22.04 nvidia-smi` before you start.

### 1. Start minikube with GPU access

```bash
minikube start --driver=docker --gpus=all --cpus=6 --memory=12288
kubectl config use-context minikube
```

### 2. NVIDIA device plugin + node labels

The binder and e2e helpers read `nvidia.com/gpu.memory` (MiB per GPU). Label the node with the card’s total from `nvidia-smi` (example below is an 11264 MiB 2080 Ti):

```bash
kubectl apply -f https://raw.githubusercontent.com/NVIDIA/k8s-device-plugin/v0.17.1/deployments/static/nvidia-device-plugin.yml

kubectl label node minikube \
  nvidia.com/gpu.present=true \
  nvidia.com/gpu.memory=11264 \
  --overwrite

kubectl -n kube-system rollout status ds/nvidia-device-plugin-daemonset --timeout=180s
kubectl get node minikube -o jsonpath='{.status.allocatable.nvidia\.com/gpu}{"\n"}{.metadata.labels.nvidia\.com/gpu.memory}{"\n"}'
```

### 3. Install KAI with GPU sharing + hamicore

If the cluster has no `RuntimeClass` named `nvidia` (common on bare device-plugin minikube), clear the default runtime-class settings so admission does not reject fraction pods:

```bash
helm upgrade -i kai-scheduler \
  oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  --namespace kai-scheduler --create-namespace \
  --version v0.17.0 \
  --set global.gpuSharing=true \
  --set binder.plugins.hamicore.enabled=true \
  --set binder.resourceReservation.runtimeClassName="" \
  --set admission.gpuFractionRuntimeClassName="" \
  --wait
```

### 4. Install kai-resource-isolator

Prefer the helper under `hack/hami/` (also used by `--test-hami` in kind cluster setup):

```bash
# from the KAI-Scheduler repo root
./hack/hami/deploy_isolator.sh

# or a local isolator chart checkout:
# ISOLATOR_CHART_REF=/path/to/KAI-resource-isolator/chart/kai-resource-isolator \
#   ./hack/hami/deploy_isolator.sh
```

Confirm the webhook exists:

```bash
kubectl get mutatingwebhookconfiguration kai-resource-isolator-mutating
kubectl -n kai-resource-isolator get deploy,ds,pods
```

### 5. Run the suite

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
go install github.com/onsi/ginkgo/v2/ginkgo@latest

ginkgo -v --trace ./test/e2e/suites/integrations/third_party/hamicore/
```

### Optional: kind helper flag

`hack/setup-e2e-cluster.sh --test-hami` (and `hack/run-e2e-kind.sh --test-hami`) enables `binder.plugins.hamicore.enabled=true` and runs `hack/hami/deploy_isolator.sh`. That is useful for install plumbing on kind, but the hamicore specs still need a real GPU and will skip or fail against the fake GPU operator alone.
