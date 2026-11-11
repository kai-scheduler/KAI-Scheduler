# Testing GPU Operator with Mokka

The default local runner and the PR, upgrade and GitOps jobs install NVIDIA GPU Operator v26.7.2 with Mokka instead of FGO. Production charts are unchanged. GPU sharing remains `NonMemoryEnforced`; this PR does not switch the Scheduler tests to NvFractions.

`install-mokka-gpu-operator.sh` builds public Mokka commit `7085b09b5c80ffc984e6e8f5ca1bce1f19b24506` from the `JRosenboimNVIDIA/mokka` fork. It includes Roman's PR 1109 GPU Operator integration, the NVML memory-limit acknowledgement from PR 1130, and the current NUMA implementation from PR 1148, integrated in PR 1147. It includes the shared NUMA enricher, direct apply/restore calls, and boot cleanup registration with an empty marker. It builds the unmodified Mokka Dockerfile and loads the image into kind. Set `MOKKA_REPOSITORY`, `MOKKA_REF` or `GPU_OPERATOR_VERSION` to test a different revision. The dedicated NUMA workflow uses the setup in `README-mokka-numa.md`; standard suites keep NUMA simulation disabled.

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
