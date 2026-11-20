# Testing GPU Operator with Mokka

The default local runner and the PR, upgrade and GitOps jobs install NVIDIA GPU Operator v26.7.2 with the upstream #2959 operand manifests and Mokka instead of FGO. Production charts are unchanged. GPU sharing remains `NonMemoryEnforced`; this PR does not switch the Scheduler tests to NvFractions.

`install-mokka-gpu-operator.sh` builds public Mokka commit `4c5978a05a57c15fd00aa6511517afcf04354fab` from the `JRosenboimNVIDIA/mokka` fork. This is main-based NUMA PR #1148; it does not include Roman's kernel-driver or GPUCluster branches. It builds the unmodified Mokka Dockerfile and loads the image into kind. The dedicated NUMA workflow uses the setup in `README-mokka-numa.md`; standard suites keep NUMA simulation disabled.

The installer builds a small GPU Operator image from the official v26.7.2 image and upstream assets at `158ec57fa19eb71ad484d5e337f05a2b612ca170`, the release-26.7 backport of #2959. This commit changes six operand manifests and their unit test; the release binaries stay unchanged. The startup check reads `/sys/module/nvidia/initstate`, which Mokka supplies through its simulated module tree. Setup checks the deployed gate before waiting for readiness.

The driver and toolkit operands are disabled. Mokka supplies the driver files and installs the NVIDIA container runtime in its userspace mode. The real GPU Operator controller, device plugin, GFD, DCGM exporter, and validator run. CUDA workload validation is disabled because simulated GPUs cannot execute CUDA. MIG is disabled, matching the existing test setup.

Set `MOKKA_REPOSITORY`, `MOKKA_REF`, `GPU_OPERATOR_VERSION`, or `GPU_OPERATOR_REF` to test another revision. The Operator version selects the release image and Helm chart; its source ref selects the manifest assets.

Four GPU workers each expose eight A100 GPUs; the CPU worker and control plane have no simulated GPUs. The real device plugin and GFD publish capacity and memory. Reservation pods use RuntimeClass `nvidia`; FGO's reservation annotation patcher and its RBAC are removed from this path.

## Run

```sh
hack/run-e2e-kind.sh --preserve-cluster
```

Only disposable kind clusters are supported. This script creates its own cluster; it does not update an existing RunAI installation. The image build and runtime setup can take several minutes on a cold runner.

## Excluded paths

The regular Mokka run filters out `numa`, `kwok`, `scale` and `autoscale` tests. NUMA runs in its dedicated workflow; the KWOK scale harness is unchanged. The release compatibility matrix keeps FGO, including its older Kubernetes, DRA and NUMA prerequisites. Existing dedicated DRA/NUMA local layouts remain available with `GPU_STACK=fake`; the Mokka experiment rejects unsupported feature layouts rather than silently skipping their setup.

```sh
GPU_STACK=fake hack/setup-e2e-cluster.sh --feature-config numa-full
```

This is a draft test-infrastructure experiment. Full e2e results must be collected before making it the supported replacement.
