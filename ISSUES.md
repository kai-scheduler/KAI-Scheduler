# Opening issues

Open an issue before a PR for features, behavior or API changes and non-trivial fixes, so maintainers can confirm the direction. Typos, docs and trivial fixes do not need one. Search existing issues first.

Use one of the [issue templates](.github/ISSUE_TEMPLATE/).

## Bug

Describe only the problem. Discuss possible solutions in the comments afterwards.

- What you expected and what happened instead.
- How to reproduce: manifests, queue/PodGroup setup, scheduler config, and the scheduler version.
- Relevant logs or events (for a Pending pod, the PodGroup's scheduling condition and the scheduler's fit errors).
- Best of all: a branch with an e2e, integration or unit test that reproduces it.

## Feature or enhancement

- **Use case**: who needs this and what they are trying to do.
- **Current pain**: what they do today and why it falls short.
- **Proposed solution**: how it would address the pain.
- **Expected user experience**: for example easy to configure, fast allocation, no change for existing workloads.

Large features may need a design doc under `docs/developer/designs/` before implementation; agree on that in the issue.

## Documentation

Say which page is wrong or missing and what it should say.
