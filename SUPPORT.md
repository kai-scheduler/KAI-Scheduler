# Support Policy

KAI Scheduler follows a structured release lifecycle to ensure stability for production environments.

## Release Schedule & LTS Policy

Starting with **v0.12**, we adopt the following release cadence:

* **Standard Versions (Odd numbers, e.g., v0.13):** Feature releases supported only until the next version is released.
* **LTS Versions (Even numbers, e.g., v0.12):** Long Term Support versions.

### Support Duration
**LTS Versions** are supported publicly for **1 Year** from their release date. During this window, they receive:
* Security Patches (CVEs)
* Critical Bug Fixes

## Support Matrix

The following versions are currently supported.

| Version | Type | Release Date | End of Support | Status |
| :--- | :--- | :--- | :--- | :--- |
| **v0.13** | Standard | Mar 2026 | *Until v0.14* | **Maintenance** |
| **v0.12** | **LTS** | Dec 2025 | Dec 2026 | **Maintenance** |
| **v0.10** | Standard | Dec 2025 | *Until v0.12* | **End of Life** |
| **v0.9** | **LTS** | Sep 2025 | **Sep 2026** | **Maintenance** |
| **v0.6** | **LTS** | Jun 2025 | **Jun 2026** | **Maintenance** |
| **v0.4** | **LTS** | Apr 2025 | **Apr 2026** | **Maintenance** |
| **< v0.4** | Legacy | - | - | **End of Life** |

> **Note on Versioning:** The strict "Even numbers are LTS" policy begins with `v0.12`. The versions listed above (`v0.9`, `v0.6`, `v0.4`) are supported as transitional LTS releases.

## Reporting Bugs

If you encounter a bug, please [open an issue](https://github.com/NVIDIA/KAI-Scheduler/issues) on GitHub.
