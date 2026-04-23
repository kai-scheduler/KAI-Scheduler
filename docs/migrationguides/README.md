# Breaking changes & Migration Guides
In this section, we will collect and describe API changes (or other breaking changes), describe the migration process and suggest backwards compatibility options for upgrades from earlier versions.


## ⚠️ Current Breaking Changes

| Version | Highlights                                 | Docs                                  |
|---------|--------------------------------------------|---------------------------------------|
| v0.6.0  | Namespace rename, queue-label key update   | [v0.6.0 Migration Guide](./v0.6.0/)   |
| v0.13.0 | Queue-controller flag removal, rollback limitation | [v0.13.0 Migration Guide](./v0.13.0/) |

> **Note:** Always check this page before upgrading to a new major/minor release.


## ⚠️ Helm Rollback Not Supported

**`helm rollback` does not work with the KAI Scheduler chart.** The chart uses Helm `lookup` guards
and `.Release.IsInstall` conditionals to create resources (namespaces, queues, service accounts)
only when they don't already exist. At install time these resources are rendered into the stored
manifest. On rollback, Helm replays the stored manifest verbatim and fails when the resources
already exist in the cluster.

**To downgrade**, use uninstall + reinstall instead:
```bash
helm uninstall kai-scheduler -n kai-scheduler
helm install kai-scheduler oci://ghcr.io/nvidia/kai-scheduler/kai-scheduler \
  --version <TARGET_VERSION> -n kai-scheduler
```
All queues, workloads, CRDs, and PodGroups are preserved across this cycle. See the
[v0.13.0 Migration Guide](./v0.13.0/) for full details.


## How to use

1. Verify whether your upgrade path traverses any versions impacted by a breaking change.
2. Click through to the version-specific folder.
3. Follow the “What Changed,” “Impact,” or “Rollback / Pin to Legacy Settings” instructions.  
4. If you need help, refer to our [issue template](https://github.com/kai-scheduler/KAI-scheduler/issues/new/choose).
