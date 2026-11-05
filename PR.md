# Opening pull requests

Full guidelines (title types and scopes, checklist, DCO): [CONTRIBUTING.md](CONTRIBUTING.md).

- **Issue first** for features, behavior or API changes and non-trivial fixes ([ISSUES.md](ISSUES.md)). Link it under "Related Issues" in the description (`Fixes #N`).
- Open PRs as **draft**.
- Title: `<type>(<scope>): <description>` (conventional commits). Allowed scopes are in `.github/workflows/validate-pr-title.yaml`.
- Fill in `.github/pull_request_template.md` completely.
- Sign off every commit (`git commit -s`).
- Run `make validate` and commit all regenerated output.
- Add or update unit, integration or e2e tests for the change.

## Changelog

PRs that change behavior (feature, fix, API change, notable perf) need exactly one fragment. Never edit `CHANGELOG.md` or write fragment files by hand.

```bash
make changelog KIND=<Added|Changed|Fixed|Removed> BODY="under 20 words" AUTHOR="<github-user>" ISSUE="<pr-or-issue-number>"
```

- `AUTHOR` and `ISSUE` are required. If the PR number is not known yet, use the issue number and update the fragment once the PR is open.
- One fragment per PR; amend it instead of adding another.
- Refactor, docs, test and CI PRs get the `skip-changelog` label instead (`dependencies` for dependency bumps).
- `make changelog-preview VERSION=vX.Y.Z` previews the next release section.
