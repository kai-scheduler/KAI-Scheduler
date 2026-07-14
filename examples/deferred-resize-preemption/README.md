# Deferred in-place resize preemption

When a node is full, an in-place pod resize (KEP-1287) that asks for more CPU/memory is
left `Deferred` by the kubelet — it never evicts anything to make room. KAI (enabled by
default) turns that `Deferred` state into a scheduling event: it represents the growth as a
**pending demand pinned to the resizing pod's own node** and lets its normal
preemption/reclaim free room for it — **subject to the queue's quota and fair share**, exactly
as if that growth were a new workload of that size. KAI never calls the resize subresource
itself; it only makes room, and the kubelet actuates the resize once the room exists.

This example scales an important workload up on a full node and watches a lower-priority
neighbour get preempted so the resize completes.

> **Prerequisites:** Kubernetes 1.33+ with the `InPlacePodVerticalScaling` feature (GA in
> 1.35), and a node whose free capacity is small enough that `resize-victim` +
> `resize-important` fill it. On a larger node, raise the two pods' CPU requests (and the
> queue quota) so that together they saturate the node — otherwise the resize simply
> succeeds without needing anyone evicted.
>
> The queue quota must cover the important pod's **grown** size (here 1 core): because the
> resize is treated like a normal allocation, a queue that is not entitled to the grown size
> has its resize refused (left `Deferred`) — that is the intended fairness behaviour.

## Steps

```console
# 1. Queues + namespace, then the two pods (500m each — together they fill the node).
kubectl apply -f 00-queues.yaml
kubectl apply -f 01-victim.yaml -f 02-important-pod.yaml
kubectl wait --for=condition=Ready pod/resize-victim pod/resize-important -n resize-demo

# 2. Scale the important pod's cpu request up in place. The node is full, so the kubelet
#    cannot actuate it yet.
kubectl patch pod resize-important -n resize-demo --subresource resize --patch \
  '{"spec":{"containers":[{"name":"main","resources":{"requests":{"cpu":"1"},"limits":{"cpu":"1"}}}]}}'

# 3. Observe the resize is Deferred (before KAI's next cycle acts):
kubectl get pod resize-important -n resize-demo \
  -o jsonpath='{range .status.conditions[?(@.type=="PodResizePending")]}{.reason}{"\n"}{end}'
# -> Deferred

# 4. Within a scheduling cycle KAI preempts the lower-priority victim to free the room. The
#    victim is evicted...
kubectl get pod resize-victim -n resize-demo            # -> Terminating / gone
#    ...and the kubelet then actuates the resize:
kubectl get pod resize-important -n resize-demo \
  -o jsonpath='{.status.containerStatuses[0].resources.requests.cpu}{"\n"}'
# -> 1 (the PodResizePending condition clears)
```

## What to expect

- If the resizing pod's queue is **over its quota / fair share** for the grown size, KAI
  leaves the resize `Deferred` and evicts nothing — a resize competes for its growth under the
  same fairness rules as any other workload.
- If the only neighbour were **higher priority or non-preemptible**, KAI would likewise leave
  the resize `Deferred` and evict nothing.
- An `Infeasible` resize (one the node could not satisfy even when empty) is ignored;
  freeing capacity cannot help it.

See the [design doc](../../docs/developer/designs/deferred-resize-preemption/README.md) for
the detection, the actual-size accounting, the node-pinned reservation, the no-bind guard,
and how quota and fair share are respected for free.

## Cleanup

```console
kubectl delete -f 02-important-pod.yaml -f 01-victim.yaml --ignore-not-found
kubectl delete namespace resize-demo --ignore-not-found
kubectl delete -f 00-queues.yaml --ignore-not-found
```
