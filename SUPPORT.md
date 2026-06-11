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
| **v0.15** | Standard | Jun 2026 | *Until v0.16* | **Active** |
| **v0.14** | **LTS** | Mar 2026 | Mar 2027 | **Maintenance** |
| **v0.13** | Standard | Mar 2026 | *Until v0.14* | **End of Life** |
| **v0.12** | **LTS** | Dec 2025 | Dec 2026 | **Maintenance** |
| **v0.10** | Standard | Dec 2025 | *Until v0.12* | **End of Life** |
| **v0.9** | **LTS** | Sep 2025 | **Sep 2026** | **Maintenance** |
| **v0.6** | **LTS** | Jun 2025 | **Jun 2026** | **Maintenance** |
| **v0.4** | **LTS** | Apr 2025 | **Apr 2026** | **End of Life** |
| **< v0.4** | Legacy | - | - | **End of Life** |

> **Note on Versioning:** The strict "Even numbers are LTS" policy begins with `v0.12`. The versions listed above (`v0.9`, `v0.6`, `v0.4`) are supported as transitional LTS releases.

## Kubernetes Compatibility Matrix

The release workflow matrix below is the current KAI-to-Kubernetes support window.
Historical support additions are listed after the table.

| KAI Release Line | Kubernetes Versions Validated | Notes |
| :--- | :--- | :--- |
| `v0.14.x` and `v0.15.x` tags | `v1.31.6`, `v1.32.3` (`default`, `dra-enabled`), `v1.33.4` (`default`, `dra-enabled`), `v1.34.0`, `v1.35.0` | This matrix landed in `5f09d6dc` and is present through tag `v0.15.2`. |
| `main` / next unreleased line | `v1.28.13`, `v1.29.8`, `v1.30.4`, `v1.31.6`, `v1.32.3` (`default`, `dra-enabled`), `v1.33.4` (`default`, `dra-enabled`), `v1.34.0`, `v1.35.0`, `v1.36.1` | Release workflow validation for the main line. |

### Historical Support Notes

* `v0.13.x` added version-aware DRA handling, including the `1.32`/`1.33` runtime-config split and DRA tracker gating (`62391cc4`, `032d7641`, `2f429935`).
* `v0.9.x`, `v0.6.x`, and `v0.4.x` explicitly added `v1.34` DRA support (`281f4269`, `2279b43d`, `95c963e2`).

## Reporting Bugs

If you encounter a bug, please [open an issue](https://github.com/kai-scheduler/KAI-scheduler/issues) on GitHub.
