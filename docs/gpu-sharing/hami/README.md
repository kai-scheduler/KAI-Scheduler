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

![Architecture](https://github.com/user-attachments/assets/ac7566fe-f79c-45fc-b3a1-24bc18ea6bc9)

## Prerequisites

- KAI-Scheduler deployed with GPU sharing and the `hamicore` plugin enabled:

  ```bash
  helm install kai-scheduler oci://ghcr.io/nvidia/kai-scheduler \
    --set global.gpuSharing=true \
    --set binder.plugins.hamicore.enabled=true \
    --namespace kai-scheduler --create-namespace
  ```

## Installation

Deploy kai-resource-isolator:

```bash
helm install kai-resource-isolator oci://docker.io/projecthami/kai-resource-isolator \
  --namespace kai-resource-isolator --create-namespace \
  --version 1.0.0-chart
```

Chart versions carry a `-chart` suffix (e.g. `1.0.0-chart`). Available versions are listed on [Docker Hub](https://hub.docker.com/r/projecthami/kai-resource-isolator/tags).

For Customization or detailed information, please refer to [kai-resource-isolator](https://github.com/Project-HAMi/KAI-resource-isolator)

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

### Opt-out

- **Per pod**: add annotation `kai-resource-isolator.io/inject: "false"`
- **Per namespace**: add label `kai-resource-isolator.io/webhook=ignore`

### Memory value precision

The `gpu-memory` annotation accepts an **integer in MiB** (no unit suffix). Internally, KAI-Scheduler converts this to a GPU fraction with 2-decimal precision, which is then multiplied against the total GPU memory to compute the actual limit. As a result, the value seen in `nvidia-smi` may differ slightly from the requested value. For example, requesting `4096` MiB on a `15360` MiB GPU (T4) rounds to a `0.27` fraction, yielding `4147m` as the enforced limit.

