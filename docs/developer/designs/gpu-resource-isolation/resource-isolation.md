# Resource isolation design

Be Noticed that this document is under evaluation and not been implemented yet.

## Background

Currently, KAI-scheduler does not enforce resource-isolation when using gpu-sharing feature, related issues: [issue#49](https://github.com/NVIDIA/KAI-Scheduler/issues/49), [issue#45](https://github.com/NVIDIA/KAI-Scheduler/issues/45). This document introduces an approach to implement that. 

## Principle

The principle behind this design is by introducing an open source component called [HAMi-core](https://github.com/Project-HAMi/HAMi-core). It can force resource limitations inside container as the following figure shows.

![image](images/sample_nvidia-smi.png)


## Architect

The integration consists of two independently deployed components:

1. **kai-resource-isolator** (external, hosted under HAMI) — a DaemonSet that deploys HAMI Core libraries to GPU nodes, and a mutating webhook that injects volume mounts into GPU-sharing pods.
2. **KAI Scheduler** — injects a `GPU_MEMORY_LIMIT` environment variable into containers requesting shared GPUs. For GPU-memory requests, the value is known at pod creation. For GPU-fraction requests, the value is resolved after the scheduling decision determines which GPU node the pod lands on.

Flow once both components are deployed:
1. Pod requesting GPU sharing is submitted
2. HAMI mutating webhook injects a volume mount for the HAMI Core library
3. KAI Scheduler determines the appropriate node and sets `GPU_MEMORY_LIMIT` accordingly
4. Container runs with HAMI Core enforcing the memory limit

After both components are implemented and tested, a user guide will be added to the documentation.

[!image](images/resource-isolation-design.png)

## HAMi-core daemonset installation

You can directly install HAMi-core on your nodes by deploying the DaemonSet with the YAML file provided below.

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: hami-core-distribute
  namespace: default
spec:
  selector:
    matchLabels:
      koord-app: hami-core-distribute
  template:
    metadata:
      labels:
        koord-app: hami-core-distribute
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: node-type
                operator: In
                values:
                - "gpu"
      containers:
      - command:
        - /bin/sh
        - -c
        - |
          cp -f /k8s-vgpu/lib/nvidia/libvgpu.so /usr/local/vgpu && sleep 3600000
        image: docker.m.daocloud.io/projecthami/hamicore:latest
        imagePullPolicy: Always
        name: name
        resources:
          limits:
            cpu: 200m
            memory: 256Mi
          requests:
            cpu: "0"
            memory: "0"
        volumeMounts:
        - mountPath: /usr/local/vgpu
          name: vgpu-hook
        - mountPath: /tmp/vgpulock
          name: vgpu-lock
      tolerations:
      - operator: Exists
      volumes:
      - hostPath:
          path: /usr/local/vgpu
          type: DirectoryOrCreate
        name: vgpu-hook
     # https://github.com/Project-HAMi/HAMi/issues/696
      - hostPath:
          path: /tmp/vgpulock
          type: DirectoryOrCreate
        name: vgpu-lock
```

## Q&A

Q: Will it effect pods using GPU exclusively?

A: No.

Q: What happens when enabling gpu-sharing, but not prepared HAMi-core on corresponding GPU node?

A: Same as what it is now, the task won't fail or crash, simply doesn't have resource isolation inside container.