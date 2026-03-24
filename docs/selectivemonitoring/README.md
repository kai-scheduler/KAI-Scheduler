# Selective Pod Monitoring
In some environments, admins may prefer to deploy KAI Scheduler in a limited scope, monitoring only a subset of pods, to avoid unintended interactions with unrelated workloads. 
This is especially relevant because the Mutating and Validating admission webhooks used by KAI Scheduler cannot be restricted solely based on `pod.spec.schedulerName` field. 
By allowing scoping through pod or namespace label selectors, admins gain precise control over which workloads are affected by the scheduler, enabling safer and more targeted adoption.

KAI Scheduler supports two mechanisms for scoping the set of monitored pods. 

## Pod Label Selector
The first option is PodLabelSelector, which restricts monitoring to pods that carry specific labels. This allows admins to define one or more key-value pairs, and only pods matching all specified labels will be processed by KAI. 
This approach is particularly useful for targeting well-defined workloads. To configure the PodLabelSelector, pass the desired label set as Helm parameters during installation.
For example:
```
--set "global.podLabelSelector.labelName=labelValue"
```
This ensures that only pods with the label labelName=labelValue will be monitored by the scheduler. Multiple labels can be set using additional `--set` flags.

## Namespace Label Selector
The second scoping mechanism is NamespaceLabelSelector, which functions similarly to PodLabelSelector but applies filtering based on labels assigned to namespaces rather than individual pods. 
With this approach, KAI Scheduler will monitor only those pods that reside in namespaces matching all specified label key-value pairs.
Pods in namespaces that do not match the configured label set will be excluded from KAI’s monitoring scope.
This is particularly effective for managing workloads grouped by environment, team, or tenant at the namespace level.
To configure this filter, pass the relevant labels as Helm parameters during installation. 
For example:
```
--set "global.namespaceLabelSelector.namespaceLabelName=namespaceLabelValue"
```
Multiple labels can be specified using additional `--set` flags.


KAI Scheduler also supports combining both PodLabelSelector and NamespaceLabelSelector to enable more granular control over the set of monitored pods. When both selectors are configured, only pods that satisfy all specified conditions will be monitored. 
Specifically, a pod must have labels that match all key-value pairs defined in PodLabelSelector, and reside in a namespace whose labels match all key-value pairs defined in NamespaceLabelSelector.
This combined filtering mechanism ensures precise targeting of workloads, allowing admins to apply scheduling logic only to pods that meet both workload-specific and environment-level criteria. Configuration is done by passing both selector sets during Helm installation:

```
--set "global.podLabelSelector.app=batch"
--set "global.namespaceLabelSelector.team=ml"
```
In the above example, only pods labeled `app=batch` and running in namespaces labeled `team=ml` will be included in KAI Scheduler's monitoring scope.