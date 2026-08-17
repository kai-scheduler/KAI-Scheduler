# GPU Sharing

KAI Scheduler supports fractional GPU scheduling so that multiple workloads can share GPU devices.

Administrators select the cluster GPU-sharing mode, and users request fractional GPUs with pod annotations.

The intended audience for this page is cluster administrators who install and configure KAI Scheduler, and users who submit GPU-sharing workloads.

## Before you begin

Make sure the following conditions are met:

- A Kubernetes cluster is running.
- The `kubectl` command-line tool has communication with your cluster.
- Helm is installed.
- KAI Scheduler is installed or ready to be installed.
- NVIDIA GPU Operator is installed.

For NvFractions, also make sure the cluster can run the `gpu-sharing` operator and that NVIDIA CDI/NRI support is configured when runtime memory enforcement is required.

## Admins

Administrators choose one GPU-sharing mode for the cluster. The mode configures the default admission and binder plugin behavior used by KAI.

### GPU-sharing modes

| Mode | Purpose | Runtime enforcement | Main user annotations |
| --- | --- | --- | --- |
| `Disabled` | Reject fractional GPU workloads. | None. | None. |
| `NonMemoryEnforced` | Schedule fractional GPU workloads without runtime memory isolation. This is the default mode. | Not enforced by KAI. Containers may see and use more GPU memory than requested. | `gpu-fraction`, `gpu-memory` |
| `HamiCore` | Schedule fractional GPU workloads and use HAMi-core for CUDA memory isolation. | Enforced through HAMi-core. | `gpu-fraction`, `gpu-memory` |
| `NvFractions` | Schedule fractional GPU workloads using NvFractions annotations and the GPU-sharing operator. | Enforced by the NvFractions runtime path when CDI/NRI is configured. | `nvidia.com/container.<container>.gpu-memory.request`, `nvidia.com/container.<container>.gpu-memory.limit` |

Set the mode with `global.gpuSharingMode`. The legacy `global.gpuSharing` value is deprecated and should only be used for upgrades that still rely on the old boolean value.

### Install without GPU memory enforcement

Use `NonMemoryEnforced` when the scheduler should place fractional GPU pods but the cluster does not need KAI to enforce CUDA memory limits:

```bash
helm upgrade -i kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  -n kai-scheduler --create-namespace \
  --set global.gpuSharingMode=NonMemoryEnforced
```

This mode keeps the legacy GPU-sharing behavior. It creates reservation pods in the `kai-resource-reservation` namespace and uses KAI's shared GPU metadata path.

### Install with HAMi-core enforcement

Use `HamiCore` when workloads should continue to use the `gpu-fraction` and `gpu-memory` annotations, but GPU memory limits are be enforced by HAMi-core 

```bash
helm upgrade -i kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  -n kai-scheduler --create-namespace \
  --set global.gpuSharingMode=HamiCore
```

Then install [`kai-resource-isolator`](hami/README.md#installation).

See [HAMi Resource Isolation](hami/README.md) for more details.

### Install with NvFractions

Use `NvFractions` when the cluster should use the GPU-sharing operator and NvFractions annotations:

```bash
helm upgrade -i kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  -n kai-scheduler --create-namespace \
  --set global.nvFractions.set=true
```

Setting `global.nvFractions.set=true` installs the `gpu-sharing` Helm dependency from `oci://ghcr.io/kai-scheduler/gpu-sharing` and renders KAI with `global.gpuSharingMode=NvFractions`.

If the `gpu-sharing` operator is already installed and managed separately, do not install the subchart from KAI. Set `global.gpuSharingMode=NvFractions` and keep `global.nvFractions.set=false`:

```bash
helm upgrade -i kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  -n kai-scheduler --create-namespace \
  --set global.gpuSharingMode=NvFractions \
  --set global.nvFractions.set=false
```

You may set the mode explicitly, but it must match:

```bash
helm upgrade -i kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  -n kai-scheduler --create-namespace \
  --set global.gpuSharingMode=NvFractions \
  --set global.nvFractions.set=true
```

The chart rejects `global.nvFractions.set=true` when `global.gpuSharingMode` is set to another mode.

### Configure the gpu-sharing subchart

KAI's global scheduling placement values are not propagated into the `gpu-sharing` subchart. Configure the subchart under the top-level `gpu-sharing` key:

```yaml
global:
  nvFractions:
    set: true

gpu-sharing:
  imagePullSecrets:
    - name: registry-credentials
  nodeSelector:
    nvidia.com/gpu.present: "true"
  tolerations:
    - key: nvidia.com/gpu
      operator: Exists
      effect: NoSchedule
```

For air-gapped environments, mirror both the KAI images and the gpu-sharing operator chart and images, or vendor the dependency before installing.

### Runtime class and CDI

KAI can auto-detect CDI and the CDI NRI plugin from the NVIDIA GPU Operator `ClusterPolicy`. When the `ClusterPolicy` indicates that CDI is enabled and selected as the default device injection path, KAI configures its CDI-aware binder plugins automatically. When NRI is enabled, KAI also enables the admission behavior that avoids injecting the legacy GPU-sharing environment variables.

Use that detection to decide whether fractional GPU pods need a runtime class:

- If the cluster uses CDI as the default GPU device injection path, fractional GPU pods usually do not need `runtimeClassName: nvidia`; set `admission.gpuFractionRuntimeClassName=null`.
- If the cluster does not use CDI as the default path and the default container runtime is not already NVIDIA-enabled, keep the default `admission.gpuFractionRuntimeClassName=nvidia` or set a custom runtime class.
- If KAI cannot read the NVIDIA `ClusterPolicy`, configure the runtime class and CDI behavior explicitly.

To suppress `runtimeClassName` injection on fractional GPU pods:

```bash
helm upgrade -i kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  -n kai-scheduler --create-namespace \
  --set admission.gpuFractionRuntimeClassName=null
```

KAI also creates GPU reservation pods for fractional GPU workloads. If reservation pods need a runtime class for GPU access, set it separately:

```bash
helm upgrade -i kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  -n kai-scheduler --create-namespace \
  --set binder.runtimeClassName=nvidia
```

You can also set `binder.cdiEnabled` or the binder plugin `cdiEnabled` argument explicitly in the KAI `Config` if CDI auto-detection is not available in your environment.

### NvFractions readiness

In `NvFractions` mode, KAI waits for the GPU-sharing operator before scheduling fractional GPU pods.

The operator must create a cluster-scoped `GpuSharingConfig` named `default` with a `Ready=True` condition:

```bash
kubectl get gpusharingconfig default -o yaml
```

GPU nodes must also have the node condition `gpu-sharing.nvidia.com/Ready=True`. If the condition is missing or false, KAI reports a fit error that includes the condition status, reason, and message.

### Admission controls

KAI admission validates fractional GPU requests before scheduling:

- A pod cannot combine a fractional GPU request with a whole `nvidia.com/gpu` request or limit.
- `gpu-fraction` must be greater than `0` and smaller than `1`.
- `gpu-memory` must be a positive integer in MiB.
- NvFractions memory values must be valid positive Kubernetes memory quantities, such as `512Mi`, `1Gi`, or `7680Mi`.
- NvFractions request must be less than or equal to limit when both are present.
- Only one container in a pod can request a fractional GPU.
- NvFractions annotations must reference an existing container.
- Users cannot create or modify `nvidia.com/container.<container>.gpus.devices`; KAI Binder writes that annotation after allocation.

## Users

Users request fractional GPUs with pod annotations. All fractional GPU pods must use the KAI scheduler and must belong to a KAI queue.

```yaml
metadata:
  labels:
    kai.scheduler/queue: default-queue
spec:
  schedulerName: kai-scheduler
```

### Choose an annotation style

Use the annotation style that matches the cluster mode selected by your administrator. In `NvFractions` mode, the legacy `gpu-fraction` and `gpu-memory` annotations are still supported for backwards compatibility, but new memory-based workloads should prefer the per-container NvFractions request annotation.

| User goal | Use this when the cluster mode is | Annotation |
| --- | --- | --- |
| Request a fraction of one GPU by proportion | `NonMemoryEnforced`, `HamiCore`, or `NvFractions` | `gpu-fraction: "0.5"` |
| Request a specific memory amount in MiB | `NonMemoryEnforced`, `HamiCore`, or `NvFractions` for compatibility | `gpu-memory: "4096"` |
| Request a specific memory amount with Kubernetes quantity syntax | `NvFractions` preferred for memory-based requests | `nvidia.com/container.<container>.gpu-memory.request: 4096Mi` |
| Set a runtime memory limit that differs from the scheduling request | `NvFractions` | `nvidia.com/container.<container>.gpu-memory.limit: 8192Mi` |
| Request fractional allocations on more than one GPU device | Any GPU-sharing mode | `gpu-fraction-num-devices: "2"` |

### Request a GPU portion

The `gpu-fraction` annotation requests a portion of one GPU. For example, `0.5` reserves half of one GPU's memory capacity:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: half-gpu
  labels:
    kai.scheduler/queue: default-queue
  annotations:
    gpu-fraction: "0.5"
spec:
  schedulerName: kai-scheduler
  containers:
    - name: gpu-workload
      image: nvidia/cuda:13.0.2-base-ubi8
      command: ["nvidia-smi"]
      args: ["--query-gpu=name,memory.total", "--format=csv,noheader"]
```

Create the example:

```bash
kubectl apply -f gpu-sharing.yaml
```

### Request GPU memory with the gpu-memory annotation

The `gpu-memory` annotation requests a specific GPU memory amount in MiB:

```yaml
metadata:
  annotations:
    gpu-memory: "4096"
```

In `NonMemoryEnforced` mode, KAI uses this value for scheduling only. In `HamiCore` mode, HAMi-core also uses it for runtime memory isolation. In `NvFractions` mode, `gpu-memory` remains supported for backwards compatibility with existing manifests, but `nvidia.com/container.<container>.gpu-memory.request` is preferred because it names the target container directly and uses Kubernetes quantity syntax.

Create the example:

```bash
kubectl apply -f gpu-memory-mib-annotation.yaml
```

### Request GPU memory with NvFractions

NvFractions uses per-container annotations. The container name is part of the annotation key:

```yaml
metadata:
  annotations:
    nvidia.com/container.gpu-workload.gpu-memory.request: 7680Mi
spec:
  containers:
    - name: gpu-workload
```

Create the example:

```bash
kubectl apply -f nv-fractions-memory.yaml
```

KAI schedules the pod using the request value. If a limit is also present, the request must be less than or equal to the limit:

```yaml
metadata:
  annotations:
    nvidia.com/container.gpu-workload.gpu-memory.request: 4096Mi
    nvidia.com/container.gpu-workload.gpu-memory.limit: 8192Mi
```

Create the request-and-limit example:

```bash
kubectl apply -f nv-fractions-request-limit.yaml
```

If only `limit` is present, KAI defaults the request to the same value.

### Request multiple fractional GPU devices

Add `gpu-fraction-num-devices` when the workload needs fractional capacity from more than one GPU:

```yaml
metadata:
  annotations:
    nvidia.com/container.gpu-workload.gpu-memory.request: 7680Mi
    gpu-fraction-num-devices: "2"
```

This asks KAI to allocate the requested fractional memory on each of two GPU devices.

KAI uses the resolved request for scheduling, pod group resource accounting, and node scale adjustment. For memory-based requests, `gpu-fraction-num-devices` multiplies the requested memory by the number of fractional devices.

### Select the target container

By default, legacy fractional requests apply to the first container. In `NonMemoryEnforced` and `HamiCore` modes, select another container with `gpu-fraction-container-name`:

```yaml
metadata:
  annotations:
    gpu-fraction: "0.5"
    gpu-fraction-container-name: gpu-workload
```

In `NvFractions` mode, the target container is already encoded in the annotation key. `gpu-fraction-container-name` is still supported for backwards compatibility with legacy annotations; if it is present together with NvFractions annotations, it must match the container name in the NvFractions annotation key.

Create the non-default-container example:

```bash
kubectl apply -f gpu-sharing-non-default-container.yaml
```

### Compute sharing mode within a fractional GPU group

Pods that share a GPU must use compatible compute sharing behavior within the same fractional GPU group. Use `kai.scheduler/gpu-compute-sharing-mode` when the workload needs to declare its compute sharing mode explicitly:

```yaml
metadata:
  annotations:
    gpu-fraction: "0.5"
    kai.scheduler/gpu-compute-sharing-mode: time-slicing
```

KAI keeps pods with incompatible compute sharing modes out of the same fractional GPU group.

`time-slicing` is the default. Use `sm-sharing` for workloads that need to run
concurrently and have MPS configured. For guidance on choosing a mode, the
memory request and limit semantics, and diagrams of both models, see
[NvFractions GPU Sharing](nv-fraction/README.md).

## Troubleshooting

### Fractional GPU pod is rejected

Check the admission error. Common causes are:

- `gpu-fraction` is `1`, `0`, negative, or not a number.
- `gpu-memory` is not a positive integer in MiB.
- NvFractions memory quantity is invalid or not positive.
- The pod requests both a fractional GPU and a whole `nvidia.com/gpu`.
- More than one container has NvFractions annotations.
- The NvFractions annotation references a container that does not exist.

### Fractional GPU pod stays pending in NvFractions mode

Start with the pod status and events:

```bash
kubectl get pod <pod-name> -n <namespace> -o wide
kubectl describe pod <pod-name> -n <namespace>
```

The events usually show whether the pod is blocked by scheduling, admission, or runtime setup.

Then check the GPU-sharing operator status:

```bash
kubectl get gpusharingconfig default -o yaml
```

Then check the target GPU node conditions:

```bash
kubectl describe node <node-name>
```

KAI requires `gpu-sharing.nvidia.com/Ready=True` on a node before scheduling NvFractions fractional GPU workloads there.

### Helm render fails when enabling NvFractions

Do not combine `global.nvFractions.set=true` with another explicit mode:

```bash
helm template kai-scheduler oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler \
  --set global.nvFractions.set=true \
  --set global.gpuSharingMode=HamiCore
```

Use `global.gpuSharingMode=NvFractions` or leave `global.gpuSharingMode` empty when `global.nvFractions.set=true`.
