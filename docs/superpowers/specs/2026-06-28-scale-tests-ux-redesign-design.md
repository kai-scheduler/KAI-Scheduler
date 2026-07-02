# Scale Tests Dashboard — UX Redesign

**Date:** 2026-06-28  
**Branch:** `siormeir/enhance-scale-test-page`  
**Files in scope:** `docs/scale-tests/index.html`, `docs/scale-tests/styles.css`, `docs/scale-tests/app.js`, `docs/scale-tests/metrics.js`

---

## Goal

The dashboard's primary purpose is displaying scale behavior and performance trends over time — not CI health monitoring. The redesign makes the Metrics view the default landing experience, aligns the visual identity with KAI's brand, and adds at-a-glance stat chips so visitors understand current performance without reading chart axes.

---

## Changes

### 1. Tab order and default

- Swap the order of tabs in `<nav class="tabs">`: **Metrics** appears first, **Test Runs** second.
- Metrics tab is `active` by default; Test Runs tab starts `hidden`.
- The controls bar (`data-content="runs"`) is already conditioned on tab visibility — no changes needed there.
- In `metrics.js`: the `scale-tests:data-loaded` listener already checks `!metricsTab.classList.contains('hidden')`. With Metrics now the default visible tab, `initializeMetrics()` fires automatically on data load without any logic changes.

### 2. KAI brand color scheme

Replace the GitHub-gray-dark palette with KAI navy/pink.

#### Dark mode (default)

| Variable    | Old        | New        |
|-------------|------------|------------|
| `--bg`      | `#0d1117`  | `#0c1524`  |
| `--surface` | `#161b22`  | `#111e2f`  |
| `--surface2`| `#21262d`  | `#182436`  |
| `--border`  | `#30363d`  | `#1f3450`  |
| `--muted`   | `#8b949e`  | `#7a8fa6`  |
| `--accent`  | `#58a6ff`  | `#e8196c`  |

`--text`, `--pass`, `--fail`, `--skip`, `--radius` are unchanged.

Hardcoded hex values in `metrics.js` (tooltip backgrounds, grid lines, tick colors, first `CHART_COLORS` entry) are updated to match.

#### Light mode

Light mode variables are scoped to `[data-theme="light"]` on the `<html>` element:

| Variable    | Light value |
|-------------|-------------|
| `--bg`      | `#f5f7fa`   |
| `--surface` | `#ffffff`   |
| `--surface2`| `#eef1f5`   |
| `--border`  | `#d0d7e0`   |
| `--text`    | `#1a2540`   |
| `--muted`   | `#5a6a80`   |
| `--accent`  | `#e8196c`   |
| `--pass`    | `#1a8f37`   |
| `--fail`    | `#d13027`   |
| `--skip`    | `#a07010`   |

The `metrics.js` chart hardcodes (tooltip background, grid, ticks) are updated to use the CSS variables where possible; where Chart.js requires resolved values, a helper reads the computed CSS variable at chart-creation time.

### 3. Light/dark toggle

- A sun/moon `<button id="theme-toggle">` is added to the page header, right of the logo/title and left of `header-stats`.
- On load: read `localStorage.getItem('kai-theme')` first; if absent, fall back to `window.matchMedia('(prefers-color-scheme: light)')`.
- On click: toggle `data-theme` between `"light"` and `"dark"` on `<html>`, persist to `localStorage`.
- Icon: ☀ in dark mode (click to go light), ☽ in light mode (click to go dark). Plain text characters, no external icon dependency.
- Styled minimally — same button style as existing `.btn-group button`, no border, sits flush in the header.

### 4. Per-chart latest-value stat chips

Each chart card in `index.html` gets a `<div class="chart-stats" id="stats-N">` inserted between the `<h3 class="chart-title">` and `<div class="chart-wrapper">`.

After `metrics.js` creates a chart it calls `renderChartStats(statsId, grouped)` which populates that div with one chip per legend group:

```
[legend label]   4m 32s  ↓
[legend label]   1m 08s  →
```

**Trend calculation:** compare the latest value to the mean of the previous 3 data points for that legend group.
- **↓** (green) — latest is >5% lower than recent mean (faster = better)
- **↑** (red) — latest is >5% higher than recent mean (slower = worse)
- **→** (muted) — within ±5%
- No arrow shown if fewer than 2 data points exist for that legend.

**Duration formatting:** convert raw seconds to `Xm Ys` or `Xs` display format (same `nsToHuman` style, adapted for seconds input).

**CSS:** `.chart-stats` is a flex row with `gap: 8px; flex-wrap: wrap; margin-bottom: 12px`. Each `.stat-chip` is a small `inline-flex` pill with monospace value and colored trend indicator.

---

## Known limitations

- Chart.js tooltip/grid colors are resolved from CSS variables at chart-creation time. Switching themes after charts are rendered requires a page reload to fully update chart internals. The page chrome (header, tabs, cards) updates immediately.

## Out of scope

- Structural layout changes to the Test Runs tab
- New chart types or additional metric patterns
- Any backend / S3 changes

---

## Testing

A local mock environment exists at `docs/scale-tests/Public/` (manifest + report copied from `example-report.json`). Run `python3 -m http.server 8080` from `docs/scale-tests/` and open `http://localhost:8080`.
