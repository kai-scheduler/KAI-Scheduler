# Kubernetes Workload API

Kubernetes 1.35 introduces the **Workload API** ([KEP-4671](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/4671-workload-api)) — a standard, vendor-neutral way to describe gang scheduling requirements. KAI Scheduler reads `Workload` resources alongside its existing PodGroup auto-discovery, so you can drive scheduling either way.

## Prerequisites

The Workload API is `Alpha` in Kubernetes 1.35 and off by default. Enable it on the apiserver:

```
--feature-gates=GenericWorkload=true
--runtime-config=scheduling.k8s.io/v1alpha1=true
```

Verify the resource is exposed:

```bash
kubectl api-resources --api-group=scheduling.k8s.io | grep workloads
```

KAI auto-detects the API at startup. No KAI-side configuration is required — when the feature gate is on, Workload-aware grouping kicks in for any Pod that carries a `spec.workloadRef`.

## How it works

A `Workload` declares one or more `podGroups`, each with a scheduling policy. Pods reference a Workload + a podGroup name (and optionally a `podGroupReplicaKey`) via `spec.workloadRef`. KAI translates each `(Workload, podGroup, replicaKey)` triple into a KAI `PodGroup`:

| Workload field | Becomes |
|----------------|---------|
| `policy.gang.minCount` | `PodGroup.Spec.MinMember` |
| `policy.basic` | `PodGroup.Spec.MinMember=1` (replica keys collapse) |
| `metadata.name` + `podGroups[].name` (+ `replicaKey`) | `PodGroup.metadata.name` |

## Gang scheduling

```bash
kubectl apply -f gang-workload.yaml
```

Two pods, both pinned to `(my-training, workers)`, converge into a single PodGroup `my-training-workers-0` with `MinMember=2`. Either both pods schedule together or neither does.

## Multiple gangs in one Workload

```bash
kubectl apply -f multi-podgroup-workload.yaml
```

A driver and a worker pool are declared in the same Workload but produce two **independent** PodGroups (`distributed-train-driver` with `MinMember=1`, `distributed-train-workers-0` with `MinMember=4`) — there is no co-scheduling between them.

## Per-Workload scheduling overrides

Place these on the `Workload` resource (labels for the first three, annotations for topology) to override the defaults that KAI would derive from the Pod's top owner:

| Label / annotation | Effect |
|--------------------|--------|
| `kai.scheduler/queue: <name>` | Routes the PodGroup to a specific Queue |
| `priorityClassName: <name>` | Sets `PodGroup.Spec.PriorityClassName` |
| `kai.scheduler/preemptibility: preemptible \| non-preemptible` | Sets preemptibility |
| `kai.scheduler/topology: <topology-name>` (annotation) | Selects a Topology |
| `kai.scheduler/topology-required-placement` (annotation) | Required topology level |
| `kai.scheduler/topology-preferred-placement` (annotation) | Preferred topology level |

If the Workload doesn't declare a label/annotation, the value falls back to whatever the top-owner-derived metadata produced.

## Opting out

Set `kai.scheduler/ignore-workload-api: "true"` as an **annotation** on either the Pod or its top owner to bypass the Workload override and use only the top-owner-based grouping path.

## Limitations

- `Workload.spec` is **immutable** upstream once created — to change `gang.minCount` or restructure `podGroups`, delete and recreate the Workload. Mutating labels and annotations is fine and the changes propagate to the existing KAI PodGroup.
- Workload `subGroups` are not yet part of the upstream API; if your top-owner controller would have produced KAI SubGroups, they're dropped when a Workload override is active.
- Deleting a `Workload` does **not** delete the KAI PodGroup it created — running gangs aren't disrupted. New Pods referencing the deleted Workload stay `Pending` until a matching Workload reappears.
