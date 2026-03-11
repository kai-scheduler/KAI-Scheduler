# Metrics store requirements

This document will focus on listing a set of requirements for the metrics store which will be used for resource accounting. All requirements in this document are the minimum.

## Metrics Resolution

Given the following assumptions:
* K8s pods normally take 1-60s to start (and even longer for pods that use very large images, like many AI workloads)
* Default terminationGracePeriod for a k8s pod is 30s
* Assuming most use cases will deal with windows sized from a few hours to weeks

It's probably reasonable to require a time granularity of 1m for raw metrics for 2 hours (meaning 1 minute resolution is < 1%). It's also acceptable to have the metrics store aggregate older samples, as long as we don't lose a resolution of 1% (e.g, for a week-long window (168h), it's probably ok to have older metrics aggregated to a 1h resolution). 

## Metrics Cardinality

The cardinality of the metrics is a multiplication of the following factors:

* \# of queues (including the entire hierarchy)
* \# of users (if enabled by the admin)
* \# of managed resource types
    * Today, only CPU, GPU and Memory are managed in queue quota, but this will likely be extended in the future
* \# of nodepools

We'll do a rough estimation for these for a large scale cluster: assuming 1000 queues, 2,000 users, 5 resource types, and 10 nodepools, we get 100,000,000.

## Persistency and Backup

Some use cases could have strict requirements for the persistency and disaster recovery of the data - for example, when storing data which will be used for charging users for their resource usage. The metrics store should at least have an option for backing up (and recovering) storage to a remote site. Data should be persisted to disk close to write time (up to a few minutes delay is probably ok, given our resolution of 1m and expected window sizes).

## Time decay calculations

We expect admins to want the option of applying time-decay functions to the usage metrics, to get higher significance for recent resource allocations over older ones. We would like to avoid performing these potentially intensive calculations in the scheduler. The metrics store should allow us to apply a function, such as a half-life time decay, to the raw and aggregated metrics.

## Query duration

While by design, the querying the metrics store will not be blocking to the scheduler cycle, there sre still some limits: the current design for the metrics fetching states a default "staleness period" of 5m - meaning that the query should be at most 5m, and ideally much faster than that. It's ok for this to be achievable through scaling up of various resources (memory, cpu or storage) in higher-scale clusters, so admins in more demanding environments will be able to keep queries short by using better hardware.