# GPU Sharing with MPS
KAI Scheduler supports GPU sharing by efficiently scheduling multiple pods to a single GPU device.

Multi-Process Service (MPS) is an alternative, binary-compatible implementation of the CUDA Application Programming Interface (API). MPS runtime architecture is designed to transparently enable co-operative multi-process CUDA applications, typically MPI jobs, to utilize Hyper-Q capabilities on the latest NVIDIA GPUs.
See the [NVIDIA MPS documentation](https://docs.nvidia.com/deploy/mps/index.html) for more details.

There are multiple ways to enable MPS in a Kubernetes cluster. This README focuses on how to use MPS with KAI Scheduler.

### Prerequisites
To use GPU sharing, ensure the following requirements are met:
* KAI Scheduler is installed and running in your cluster, with gpu-sharing feature enabled.
* MPS server is running on all GPU-enabled hosts (`nvidia-cuda-mps-control`), with the `CUDA_MPS_PIPE_DIRECTORY` environment variable set to `/tmp/nvidia-mps`.

### MPS Enabled Pods
To submit a pod that can share a GPU device and connect to the MPS server, run the following command:
```
kubectl apply -f gpu-sharing-with-mps.yaml
```

In the `gpu-sharing-with-mps.yaml` file, the Pod defines an MPS volume using a hostPath set to `/tmp/nvidia-mps`, which is mounted to the same path within the container.

### Configuring MPS
If the MPS server on the host is configured with a custom `CUDA_MPS_PIPE_DIRECTORY` (e.g., `/other/path`), make sure the same path is mounted in the Pod YAML.

For additional MPS-related environment variables, refer to the [NVIDIA MPS documentation](https://docs.nvidia.com/deploy/mps/index.html#environment-variables).

### Running MPS Server as a Pod in the Cluster
If you're running the MPS server as a pod on a GPU node, you must ensure that the workload pods are scheduled to the same nodes.
To achieve this, label the relevant nodes and apply node affinity or a node selector to the workload pods.

For example:
1. Label GPU nodes running the MPS server: `kubectl label node <NODE_NAME> nvidia.mps/enabled: true`
2. Add nodeAffinity / nodeSelector to your workload pods:
```
nodeSelector:
    nvidia.mps/enabled: true
```
