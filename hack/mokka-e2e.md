# Testing GPU Operator with Mokka

The default local runner and the PR, upgrade and GitOps jobs install NVIDIA GPU Operator v26.7.2 with Mokka instead of FGO. Production charts are unchanged. GPU sharing remains `NonMemoryEnforced`; this PR does not switch the Scheduler tests to NvFractions.

`install-mokka-gpu-operator.sh` builds a pinned public Mokka commit: PR 1109's GPU Operator integration at c7605a7 plus the NVML memory-limit acknowledgement from PR 1130. The source is pinned to `fe859af25b05f1ea4e4e91e56c89491b8e6dc7b4` on the public `JRosenboimNVIDIA/mokka` fork until the integration is released upstream. It builds the unmodified Mokka Dockerfile and loads the image into kind. Set `MOKKA_REPOSITORY`, `MOKKA_REF` or `GPU_OPERATOR_VERSION` to test a different revision.

The driver and toolkit operands stay enabled and use NVIDIA images. Mokka uses its kernel simulation mode and NRI integration. The driver image is the multi-architecture Ubuntu 24.04 digest, so the new CI jobs use Ubuntu 24.04. CUDA workload validation is disabled because simulated GPUs cannot execute CUDA. MIG is disabled, matching the existing test setup.

Four GPU workers each expose eight A100 GPUs; the CPU worker and control plane have no simulated GPUs. The real device plugin and GFD publish capacity and memory. Reservation pods use RuntimeClass `nvidia`; FGO's reservation annotation patcher and its RBAC are removed from this path.

## Run

```sh
hack/run-e2e-kind.sh --preserve-cluster
```

Only disposable kind clusters are supported. This script creates its own cluster; it does not update an existing RunAI installation. The build and driver setup can take several minutes on a cold runner.

## Excluded paths

The Mokka run filters out `numa`, `kwok`, `scale` and `autoscale` tests. NUMA simulation and the KWOK scale harness are not changed. The release compatibility matrix keeps FGO, including its older Kubernetes, DRA and NUMA prerequisites. Existing dedicated DRA/NUMA local layouts remain available with `GPU_STACK=fake`; the Mokka experiment rejects those feature layouts rather than silently skipping their setup.

```sh
GPU_STACK=fake hack/setup-e2e-cluster.sh --feature-config numa-full
```

This is a draft test-infrastructure experiment. Full e2e results must be collected before making it the supported replacement.
