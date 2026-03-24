# Migration Guide: v0.6.0

## 1. What Changed

### **Resource Reservation Namespace**  
  - **Old:** `runai-reservation`  
  - **New:** `kai-resource-reservation`

The name of the resource reservation namespace was changed from `runai-reservation` to `kai-resource-reservation`. 
In order to safely migrate from the previous namespace, GPU sharing workloads must be deleted before the upgrade and reservation pods should not be found in `runai-reservation` namespace.

The following command should return zero pods before you upgrade:
```
kubectl get pods -n runai-reservation
```

### **Scheduling queue label key**  
  - **Old:** `runai/queue` 
  - **New:** `kai.scheduler/queue`

The label key for a scheduling queue was changed from `runai/queue` to `kai.scheduler/queue`.
In order to adopt the new label key, all workloads must have the new label with the name of the respective queue before the upgrade.

The following command should result without any existing pods:
```
kubectl get pods -A -l 'runai/queue'
```

### **Documentation and examples**  

Docs and examples have been updated to reflect these changes.


## 2. Impact

- Any existing reservation pods or GPU-sharing workloads still in `runai-reservation` will not be managed after upgrade.
- Workloads labeled with `runai/queue` will not be scheduled by the upgraded version of the scheduler.


## 3. Pin to Legacy Settings

If adopting these changes is not possible at current time, you can keep using the old namespace and label key by overriding the defaults with  `values.yaml`. To do this, add the following flag to the `helm upgrade` command. This will configure KAI Scheduler to use the old values until the cluster is updated to the new values:
```
--values ./docs/migrationguides/v0.6.0/values.yaml
```

