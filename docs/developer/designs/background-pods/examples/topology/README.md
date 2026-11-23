# Background pods degrade topology placement

A failure mode of the zero-quota-queue workaround: **allocate runs before reclaim**, so the scheduler settles for a feasible-but-topologically-worse placement instead of reclaiming background pods to get the placement the workload asked for.

Because the background pods are real, queued, accounted workloads, the only thing that could remove them is reclaim — and reclaim is never reached, because naive allocation already succeeded.

## Setup

8 GPU workers x 8 GPUs, split into two racks of four.
Four background pods (1 GPU each) sit on two nodes in each rack:

```
rack-0:  node0 [7 free]  node1 [7 free]  node2 [8 free]  node3 [8 free]
rack-1:  node4 [7 free]  node5 [7 free]  node6 [8 free]  node7 [8 free]
```

team-a submits 4 pods x 8 GPUs (32 GPUs, exactly its quota) with `kai.scheduler/topology-preferred-placement: rack`.

- **What should happen:** reclaim the 2 background pods in one rack, run all 4 pods there, preferred topology satisfied.
- **What actually happens:** allocate finds 4 fully-free nodes — 2 in rack-0 and 2 in rack-1 — admits the job split across both racks, and never attempts reclaim.

## Prerequisites

Apply the priority class and queues from the parent directory first:

```bash
kubectl apply -f ../00-priorityclass.yaml -f ../01-queues.yaml
```

## Label the nodes

```bash
for i in 0 1 2 3; do
  kubectl label node runai-cluster-worker-0-$i accelerator.nvidia.com/rack=rack-0 --overwrite
done
for i in 4 5 6 7; do
  kubectl label node runai-cluster-worker-0-$i accelerator.nvidia.com/rack=rack-1 --overwrite
done

kubectl get nodes -L accelerator.nvidia.com/rack
```

## Run

```bash
kubectl apply -f 01-topology.yaml
kubectl apply -f 02-background-pods.yaml

# Expect 4 pods on 4 distinct nodes, 2 per rack.
kubectl get pods -l app=healthcheck \
  -o custom-columns=POD:.metadata.name,NODE:.spec.nodeName
```

```bash
kubectl apply -f 03-team-a-topology-job.yaml

# The interesting output: which racks did the 4 job pods land in?
kubectl get pods -l app=user-workload -o json | jq -r \
  '.items[] | .metadata.name + " " + .spec.nodeName' | while read pod node; do
    rack=$(kubectl get node "$node" -o jsonpath='{.metadata.labels.accelerator\.nvidia\.com/rack}')
    echo "$pod  $node  $rack"
  done
```

Expected: 2 pods in `rack-0`, 2 in `rack-1`, and all four healthcheck pods still Running.

## Control: the same job with no background pods

Proves the preference itself works, and that the background pods are what degraded it.

```bash
kubectl delete -f 03-team-a-topology-job.yaml
kubectl delete -f 02-background-pods.yaml
kubectl apply -f 03-team-a-topology-job.yaml
# Re-run the rack query above - all 4 pods should now be in a single rack.
```

## Cleanup

```bash
kubectl delete -f 03-team-a-topology-job.yaml -f 02-background-pods.yaml \
  -f 01-topology.yaml --ignore-not-found
kubectl label nodes -l accelerator.nvidia.com/rack accelerator.nvidia.com/rack-
```

## Why this matters for the design

Under the proposed feature the background pods are invisible to the scheduler, so *every* node looks fully free and the topology solver picks 4 nodes in one rack directly — the correct answer, reached without a reclaim scenario search.

The flip side is that the scheduler has no reason to prefer a rack needing fewer evictions over one needing more, since background pods carry no weight in node selection.
It may well pick rack-1 here and displace those two instead.
That is accepted: disruption is limited by attempting to place each background pod back on its own node afterwards, not by steering the workload away from them.
