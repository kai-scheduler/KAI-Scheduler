# KAI Scheduler Recorder

## Overview

The recorder stores one record for every scheduling cycle, including a cycle that places nothing. The record is the cluster state that cycle used, the commits it made, the groups still pending afterwards, and the time the cycle opened.

Use it when you need to see why a pod was placed, preempted, or left waiting, and when you want to run that same cycle again.

The recorder stays off until a SchedulingShard turns it on.

## What a record contains

Each line in a record file is one JSON envelope:

- `schemaVersion` is the shape of the record. This build writes `2`. Bump it whenever the record shape changes. A replay that sees a higher number stops with an error instead of reading a shape it does not understand.
- `kaiVersion` is the KAI build that wrote the record.
- `record` is the cycle itself:
  - `openedAt` is the time the cycle opened.
  - `stateId` names the cluster-state copy this cycle used. `config`, `schedulerParams`, `rawObjects`, and `usage` are that copy: the scheduler settings, the pods, nodes, queues, pod groups, and related objects, and the queue usage the fair-share calculation used. The copy is written when it differs from the last saved copy, and again when the saved copy is older than 5 minutes. A later cycle that used the same copy keeps `stateId` and leaves the body out. That memory is kept for the life of the process, one entry per cluster and shard, so the next session can reuse it. The id ignores the creation time of the parent queue named `default`, because that queue is stamped with the current time on every snapshot.
  - `stateMissing` is set when that copy was dropped before it was stored, or when replay cannot find it in the loaded records. Replay stops with an error instead of running against an empty cluster.
  - `decisions` is the commits, in order. Each commit names the action (`allocate`, `preempt`, `reclaim`, `consolidation`, `stalegangeviction`, or `session-close` for work done while plugins close) and the operations inside it (`allocate`, `pipeline`, `evict`). An operation has `failed` set when its commit returned an error, and then `reason` is a code for that failure (`NodeNotFound`, `JobNotFound`, `PodGroupNotFound`, `TaskUpdateFailed`, `BindFailed`, or `CommitFailed`). `notAttempted` is set when an earlier allocation failure stopped the commit before the operation ran. An eviction carries `reason` (`Preempted`, `Reclaimed`, `Consolidated`, `StaleGang`, or `BackgroundPod`) together with the action, the gang size, and the preemptor.
  - `pending` is the pod groups that still had a pending pod when the cycle closed. `attempted` is true when an action entered its placement function for that group in the cycle. `reasonCodes` are the codes KAI recorded for the group during the cycle. A group that was filtered out before a placement attempt has `attempted` false. Codes are left out when KAI did not record one. The pod group's stored status is not copied here.

Undo operations are not stored. A statement that nets to nothing is not stored. The plugin argument `stateInterval` changes how long an unchanged state copy is reused. When the argument is absent the interval is 5 minutes. An interval of `0s` stores a copy on every cycle.

## Turning it on

Set the defaults on the KAI Config, under `spec.scheduler.recorder`. A SchedulingShard can set `spec.recorder` to override those defaults for that shard. The shard value wins when both are set.

With Helm, the same settings live under `scheduler.recorder`. The chart renders them only when `scheduler.recorder.enabled` is true, so a default install does not add the field.

```yaml
kind: Config
metadata:
  name: kai-config
spec:
  scheduler:
    recorder:
      enabled: true
---
kind: SchedulingShard
metadata:
  name: default
spec:
  recorder:
    enabled: true
```

Enabling the recorder adds a `recorder` plugin to that shard's scheduler config and a sidecar container on the scheduler pod. The plugin arguments carry the listen address, the cluster name, and the shard name. The sidecar image, resource requests, and roll settings come from the same spec. When the spec omits them, the operator fills the listen address `127.0.0.1:9095`, a 5 minute roll interval, a 64 MiB roll size, and a small CPU and memory request.

## A decision

One committed preempt looks like this, after names have been hashed:

```text
preempt evict <namespace-hash>/<victim-hash>@<node-hash>
preempt pipeline <namespace-hash>/<preemptor-hash>@<node-hash>
```

`evict` is the pod that lost its node. `pipeline` is the pod placed on the node that the eviction frees. `allocate` is a placement that did not need an eviction.

## Hash key

The operator creates one Secret, `kai-recorder-hash-key`, owned by the Config. Every shard that records uses that same key, so a name hashes to the same value on every shard. The operator does not rotate the key on later reconciles.

Record files stay on the node, in an emptyDir on the scheduler pod.

## Sidecar

The sidecar listens on localhost and writes the scheduler's records to `/var/lib/kai-recorder`.

| Flag | Meaning |
| --- | --- |
| `--listen` | Address that accepts records from the scheduler. Default `127.0.0.1:9095`. |
| `--dir` | Directory for the record files. |
| `--hmac-key-file` | File containing the hash key. |
| `--roll-interval` | How often to close the current record file and gzip it, even if it is not full. Default 5 minutes. |
| `--roll-bytes` | Close and gzip the current record file once it reaches this size. Default 64 MiB. |

The directory is kept at or under 256 MiB. After a file is rolled, the sidecar deletes the oldest files until the directory fits. That cap is fixed in the sidecar.

## Redaction

Before a record is written, names are replaced with the first 63 hex characters of HMAC-SHA256 under the hash key. The same input always produces the same hash, so a pod in a decision still matches that pod in the snapshot.

Label keys the scheduler matches on are hashed the same way on the object and in the selector. A topology level `nodeLabel` and its `alias` are hashed the same way as those label keys, and so are a pod group's required and preferred topology level and a pod affinity `topologyKey`. The namespace list on an affinity term is hashed the same way as a pod namespace. Taint values and toleration values are hashed with the same label-value rule, scoped by the taint key, so an Equal toleration still matches only the taint value it named. A toleration that omits its operator and effect is still Equal, and its value is hashed the same way. `Gt` and `Lt` values stay decimal integers, on tolerations and on node affinity. Priority class names are hashed and the numeric priority is kept. A pod group that names no priority class is recorded with the priority it inherited, so replay uses that same number. DRA attribute strings stay readable. Plugin names stay readable so a replay can load the same plugins. Free-text reasons that can hold a name are dropped. Reason codes stay.

## Replay

A recorded cycle can be loaded back into the scheduler. Replay finds the state copy named by `stateId` when the cycle itself does not carry one, installs `usage` when it is present, and runs the actions named in `config`. The decision clock starts at `openedAt` and then moves forward with real elapsed time, so a minimum runtime, a preemption delay, or a search budget sees the same starting instant and still elapses while the replay runs. A cycle whose state copy is missing stops the replay with an error.

The recorded decisions are the expected result. A replay that places the same pods on the same nodes produced the same commits.

## Limits

Recording is best effort. The scheduler does not wait for the sidecar. Each commit is copied into a bounded buffer and a separate goroutine sends it. When the buffer is full, or the sidecar cannot be reached, the record is dropped and the drop is counted. A dropped record leaves a gap in the sequence numbers of that process.

The scheduler keeps one sender for the life of the process, so the sequence keeps increasing across cycles.

## Roadmap

- Make the 256 MiB storage cap configurable, with a sidecar flag and a Helm value, where it is hard-coded today.
- Add a metric that counts files deleted because the directory was over the cap.
- Upload rolled batches to storage.
- Publish the recorder types from `kai-scheduler/api` and delete the temporary copy in this repository.
