# preemption ("can't it replace lower-priority work?")

Your pod is Pending and the cluster is full of **lower-priority** work - you expected KAI to
evict it. KAI can evict a **preemptible** holder (its priorityClass `value < 100`) to make room
for higher-priority work, but only past the victim's protection window. There is no typed reason
for "preemption didn't happen" - derive it from priorities and `minRuntime`.

**Check**
- Who holds the GPUs now: `kubectl get pods -A -o wide` on the busy nodes (free_capacity shows
  which nodes are full).
- Holder preemptible? `kubectl get pod <holder> -o jsonpath='{.spec.priorityClassName}'` then its
  `value` - `>= 100` is **non-preemptible**, never evicted.
- Your pod's priority vs the holder's - you must outrank it.
- Protection window: `kubectl get queue <queue> -o jsonpath='{.spec.preemptMinRuntime} {.spec.reclaimMinRuntime}'`
  - a victim is shielded until it has run that long.

**Answer / fix**
- Holder non-preemptible (>= 100) -> it won't yield; wait or add capacity.
- Holder preemptible but young -> wait out `minRuntime` (or lower it on the queue if eviction
  should be faster).
- The reverse complaint ("my job keeps getting evicted") -> your job is the victim: raise its
  priority, or move it to a queue with quota so it runs guaranteed.
- Note: a bare Pod victim is **deleted** on eviction (not re-Pending); only Deployment/Job-managed
  victims reappear as Pending.
