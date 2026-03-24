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
- Log Changes – For behavior-affecting changes (features, fixes, API changes), update the [changelog](CHANGELOG.md) file under the "Unreleased" section. Follow the format at [keepachangelog.com](https://keepachangelog.com/en/1.1.0/). Skip logging internal changes like refactoring or tests.
- Submit a PR – Open a pull request and reference any relevant issues or discussions.
- Approval Policy – PRs from external contributors require approval from 2 trusted reviewers (organization members or collaborators) before merging.
- Coverage - Please look at the coverage change details and create unit tests, integration tests or end-to-end tests to cover new functionality or changes.

### Pull Request Checklist
Before introducing major changes, we strongly recommend opening a PR that outlines your proposed design.
Each pull request should meet the following requirements:
- All tests pass – Run the full test suite locally with: `make build validate test`
- Test coverage – Add or update tests for any affected code.
- Documentation – Update relevant documentation to reflect your changes.
- Changes logged - If your changes warrant logging - like behavior changes (including bugfixes) or new features - add them to the [Changelog](CHANGELOG.md)

## Getting Help
Need support or have a question? We're here to help:
- Report issues or ask questions by [opening an issue on GitHub](https://github.com/NVIDIA/KAI-Scheduler/issues).
- Join the conversation in the [#batch-wg](https://cloud-native.slack.com/archives/C02Q5DFF3MM) Slack channel to connect with the community and contributors.

## License
By contributing, you agree that your contributions will be licensed under the Apache License 2.0.

Thank you for your interest and happy coding!
