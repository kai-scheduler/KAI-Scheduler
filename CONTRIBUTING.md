# Contributing to KAI Scheduler

Thank you for your interest in contributing to KAI Scheduler! This document provides guidelines and instructions to help you get started with contributing to our project.

Make sure to read our [Contributor License Agreement](CLA.md) and [Code of Conduct](code_of_conduct.md).

## Getting Started
### New Contributors
We're excited to help you make your first contribution! Whether you're looking to file issues, develop features, fix bugs, or improve documentation, we're here to support you through the process.

Browse issues labeled [good first issue] or [help wanted] on GitHub for an easy introduction.

### Developers
The main building blocks of KAI Scheduler are documented in the `docs/developer` folder. Here are the key components:
- [Action Framework](docs/developer/action-framework.md) - Core scheduler logic
- [Plugin Framework](docs/developer/plugin-framework.md) - Extensible plugin system
- [Pod Grouper](docs/developer/pod-grouper.md) - Group scheduling functionality
- [Binder](docs/developer/binder.md) - Binding logic

We recommend reviewing these documents to understand the system architecture before making significant contributions.

## How to Contribute
### Reporting Issues
Open an issue with a clear description, steps to reproduce, and relevant environment details.

### Improving Documentation
Help us keep the docs clear and useful by fixing typos, updating outdated information, or adding examples.

### Contributing changes
- Fork and Clone – Begin by forking the repository and cloning it to your local machine.
- Create a Branch – Use a descriptive branch name, such as feature/add-cool-feature or bugfix/fix-issue123.
- Make Changes – Keep your commits small, focused, and well-documented. For detailed build and test instructions, refer to [Building from Source](docs/developer/building-from-source.md).
- Log Changes – For behavior-affecting changes (features, fixes, API changes), update the [changelog](CHANGELOG.md) file under the "Unreleased" section. Follow the format at [keepachangelog.com](https://keepachangelog.com/en/1.1.0/). Skip logging internal changes like refactoring or tests. CI checks for changelog updates; add the `skip-changelog` label to your PR to skip if your change doesn't require a changelog entry.
- Submit a PR – Open a pull request and reference any relevant issues or discussions.
- Approval Policy – PRs from external contributors require approval from 2 trusted reviewers (organization members or collaborators) before merging.
- Coverage - Please look at the coverage change details and create unit tests, integration tests or end-to-end tests to cover new functionality or changes.

### PR Title Guidelines

PR titles must follow the [Conventional Commits](https://www.conventionalcommits.org/) title specification. The format is:

```
<type>[optional scope]: <description>
```

#### Types

- **feat**: A new feature
- **fix**: A bug fix
- **docs**: Documentation only changes
- **style**: Changes that don't affect code meaning (formatting, whitespace)
- **refactor**: Code changes that neither fix a bug nor add a feature
- **perf**: Performance improvements
- **test**: Adding or updating tests
- **build**: Changes to build system or dependencies
- **ci**: Changes to CI/CD configuration
- **chore**: Other changes that don't modify src or test files
- **revert**: Reverts a previous commit

#### Scopes (Optional)

Common scopes for KAI Scheduler:
- 'scheduler'
- 'binder'
- 'podgrouper'
- 'admission'
- 'operator'
- 'queue-controller'
- 'pod-group-controller'
- 'resource-reservation'
- 'chart'
- 'api'
- 'node-scale-adjuster'
- 'ci'
- 'release'
- 'docs'

#### Breaking Changes

Breaking changes MUST be indicated by adding `!` after the type/scope: `feat(api)!: remove deprecated field`

#### Examples

```
feat(scheduler): add GPU topology-aware scheduling
fix(binder): resolve race condition in pod binding
docs: update installation guide
refactor(api): simplify PodGroup status structure
feat(api)!: remove deprecated field from CRD
```

#### Tips

- Use the imperative mood: "add feature" not "added feature"
- Don't end with a period

### Pull Request Checklist

Before introducing major changes, we strongly recommend opening a PR that outlines your proposed design.
Each pull request should meet the following requirements:
- All tests pass – Run the full test suite locally with: `make build validate test`
- Test coverage – Add or update tests for any affected code.
- Documentation – Update relevant documentation to reflect your changes.
- Changes logged - If your changes warrant logging - like behavior changes (including bugfixes) or new features - add them to the [Changelog](CHANGELOG.md). Use the `skip-changelog` label to opt out if not needed.
- PR description – Fill out the pull request template completely

## Getting Help
Need support or have a question? We're here to help:
- Report issues or ask questions by [opening an issue on GitHub](https://github.com/NVIDIA/KAI-Scheduler/issues).
- Join the conversation in the [#batch-wg](https://cloud-native.slack.com/archives/C02Q5DFF3MM) Slack channel to connect with the community and contributors.

## License
By contributing, you agree that your contributions will be licensed under the Apache License 2.0.

Thank you for your interest and happy coding!
