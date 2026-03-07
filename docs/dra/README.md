# Dynamic Resource Allocation (DRA)

KAI Scheduler supports DRA through its [Binder controller](https://github.com/kai-scheduler/kai-scheduler/blob/main/docs/developer/binder.md#binder) for handling resourceclaims. The scheduler and binder communicate through a custom resource called `BindRequest`. When the scheduler decides where a pod should run, it creates a BindRequest object that contains:

- The pod to be scheduled
- The selected node
- Information about resource allocations (including GPU resources)
- DRA (Dynamic Resource Allocation) binding information
- Retry settings

### Prerequisites
- The [NVIDIA k8s-dra-driver-gpu](https://github.com/NVIDIA/k8s-dra-driver-gpu) must be installed by following [instructions from here](https://github.com/NVIDIA/k8s-dra-driver-gpu/discussions/249). This driver leverages the upstream DRA API to support NVIDIA Multi-Node NVLink available in GB200 GPUs via a ComputeDomain CRD that lets you define resource templates which you can reference in your workloads.

- DRA is disabled by default. To enable it, add the following flag to the helm install command:
```
 --set scheduler.additionalArgs[0]=--feature-gates=DynamicResourceAllocation=true --set binder.additionalArgs[0]=--feature-gates=DynamicResourceAllocation=true
```

### DRA IMEX POD
To submit a pod that requests an IMEX channel, run this command:
```
kubectl apply -f gpu-imex-pod.yaml
```

The scheuler will autogenerate this BindRequest for the requested IMEX channel

```bash
kubectl get BindRequest gpu-imex-pod-8g6vlrjxpp -o yaml

apiVersion: scheduling.run.ai/v1alpha2
kind: BindRequest
metadata:
  creationTimestamp: "2025-04-08T21:55:32Z"
  generation: 1
  labels:
    pod-name: gpu-imex-pod
    selected-node: NODE_NAME
  name: gpu-imex-pod-8g6vlrjxpp
  namespace: default
  ownerReferences:
  - apiVersion: v1
    kind: Pod
    name: gpu-imex-pod
    uid: 6306ffe2-a348-467a-b9aa-7176f2e95f53
  resourceVersion: "17426791"
  uid: 3c56aca5-027c-4bcd-9e9a-755e4c61ee8b
spec:
  podName: gpu-imex-pod
  receivedGPU:
    count: 1
    portion: "1.00"
  receivedResourceType: Regular
  resourceClaimAllocations:
  - allocation:
      devices:
        config:
        - opaque:
            driver: compute-domain.nvidia.com
            parameters:
              apiVersion: resource.nvidia.com/v1beta1
              domainID: 83479f70-e292-43d6-aa67-dd4ba8adab8f
              kind: ComputeDomainChannelConfig
          requests:
          - channel
          source: FromClaim
        results:
        - device: channel-0
          driver: compute-domain.nvidia.com
          pool: NODE_NAME
          request: channel
      nodeSelector:
        nodeSelectorTerms:
        - matchFields:
          - key: metadata.name
            operator: In
            values:
            - NODE_NAME
    name: imex-channel-0
  selectedNode: NODE_NAME
status:
  phase: Succeeded
```
