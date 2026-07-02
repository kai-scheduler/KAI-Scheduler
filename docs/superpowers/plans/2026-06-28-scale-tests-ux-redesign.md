# Scale Tests UX Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Metrics tab the default landing view, add KAI light mode, and inject per-chart latest-value stat chips.

**Architecture:** Pure client-side HTML/CSS/JS — no build step. Four tasks each touch a focused slice of the three files (`index.html`, `styles.css`, `metrics.js`) with one task also touching `app.js`. Verification is visual: a local Python HTTP server serves the page with mock data.

**Tech Stack:** Vanilla JS (ES2020), Chart.js 4.4, CSS custom properties, `localStorage` for theme persistence.

## Global Constraints

- No new external dependencies — Chart.js and chartjs-adapter-date-fns are already loaded via CDN in `index.html`.
- Apache-2.0 + NVIDIA copyright headers are already present in all files — do not remove them.
- Dark mode CSS variables are **already applied** to `docs/scale-tests/styles.css` `:root` block (done during design preview). Do not re-apply them.
- A local test server and mock data already exist. Run `python3 -m http.server 8080` from `docs/scale-tests/` and open `http://localhost:8080` to verify each task.
- `esc()` is defined globally in `app.js` and available to `metrics.js` since both load in the same page.

---

### Task 1: Swap tab order and fix metrics initialization

Default landing tab changes from Test Runs → Metrics. Also fixes a bug where switching away from Metrics and back would try to re-create already-existing charts.

**Files:**
- Modify: `docs/scale-tests/index.html`
- Modify: `docs/scale-tests/metrics.js`

**Interfaces:**
- Produces: Metrics tab is `active` and visible on load; Test Runs tab and its controls/summary strip start `hidden`.

- [ ] **Step 1: Swap tabs and flip visibility in `index.html`**

In `<nav class="tabs">`, move the Metrics button before Test Runs and flip `active`:
```html
<nav class="tabs">
  <button class="tab active" data-tab="metrics">Metrics</button>
  <button class="tab" data-tab="runs">Test Runs</button>
</nav>
```

The controls bar, summary strip, and runs `<main>` all carry `data-content="runs"` — add `hidden` to each so they start hidden. The metrics `<main>` loses its `hidden` class:
```html
<!-- Controls: add hidden -->
<div class="controls tab-content hidden" data-content="runs">
  ...
</div>

<!-- Summary: add hidden -->
<div class="summary tab-content hidden" data-content="runs" id="summary" aria-live="polite">Loading…</div>

<!-- Runs main: add hidden -->
<main id="main" class="tab-content hidden" data-content="runs">
  <div class="msg"><div class="spinner"></div><br>Fetching scale test results…</div>
</main>

<!-- Metrics main: remove hidden -->
<main id="metrics-main" class="tab-content" data-content="metrics">
  ...
</main>
```

- [ ] **Step 2: Fix the `_metricsInitialized` flag in `metrics.js`**

In the `scale-tests:data-loaded` listener (around line 375), the flag is incorrectly reset to `false` before calling `initializeMetrics()`. Change it to `true` so clicking away and back to Metrics doesn't try to create duplicate charts:

```js
window.addEventListener('scale-tests:data-loaded', () => {
  console.log('[metrics] Data loaded event received');

  const metricsTab = document.getElementById('metrics-main');
  if (metricsTab && !metricsTab.classList.contains('hidden')) {
    window._metricsInitialized = true;
    initializeMetrics();
  }
});
```

- [ ] **Step 3: Verify in browser**

With the local server running (`python3 -m http.server 8080` from `docs/scale-tests/`):
- Open `http://localhost:8080` — Metrics tab should be active and the chart grid visible immediately.
- Click "Test Runs" — the run cards list appears; controls/search/filters appear.
- Click "Metrics" again — charts are still there, no JS errors in console.

- [ ] **Step 4: Commit**

```bash
git add docs/scale-tests/index.html docs/scale-tests/metrics.js
git commit -m "feat(docs): make Metrics the default tab, fix re-init bug"
```

---

### Task 2: Light/dark theme toggle

Adds a sun/moon button to the header. Theme is persisted in `localStorage` and applied before first paint to prevent flash.

**Files:**
- Modify: `docs/scale-tests/index.html` — inline FOUC-prevention script in `<head>`, toggle button in `<header>`
- Modify: `docs/scale-tests/styles.css` — `[data-theme="light"]` variable overrides, toggle button style
- Modify: `docs/scale-tests/app.js` — click handler and icon sync

**Interfaces:**
- Produces: `document.documentElement` carries `data-theme="dark"` or `data-theme="light"` at all times. `localStorage` key is `kai-theme`.

- [ ] **Step 1: Add FOUC-prevention script to `<head>` in `index.html`**

Insert immediately before `</head>` so the theme attribute is set before any CSS renders:

```html
  <script>
    (function(){
      var t = localStorage.getItem('kai-theme') ||
              (window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');
      document.documentElement.setAttribute('data-theme', t);
    })();
  </script>
</head>
```

- [ ] **Step 2: Add toggle button to `<header>` in `index.html`**

Add the button as the last child of `<header class="page-header">`, after `<div class="header-stats">`:

```html
  <button id="theme-toggle" class="theme-btn" title="Toggle light/dark mode" aria-label="Toggle theme">☀</button>
</header>
```

- [ ] **Step 3: Add light-mode CSS variables and toggle button style to `styles.css`**

Append to the end of `styles.css`:

```css
/* ── Light mode ──────────────────────────────────────────────────────────── */

[data-theme="light"] {
  --bg:       #f5f7fa;
  --surface:  #ffffff;
  --surface2: #eef1f5;
  --border:   #d0d7e0;
  --text:     #1a2540;
  --muted:    #5a6a80;
  --pass:     #1a8f37;
  --fail:     #d13027;
  --skip:     #a07010;
}

/* ── Theme toggle button ─────────────────────────────────────────────────── */

.theme-btn {
  background: transparent;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  color: var(--muted);
  font-size: 14px;
  padding: 4px 8px;
  cursor: pointer;
  transition: color 0.1s, border-color 0.1s;
  flex-shrink: 0;
}
.theme-btn:hover { color: var(--text); border-color: var(--accent); }
```

- [ ] **Step 4: Add toggle logic to `app.js`**

Append to the end of `app.js` (after `init()`):

```js
// ── Theme toggle ───────────────────────────────────────────────────────────

(function () {
  const btn = document.getElementById('theme-toggle');

  function syncIcon() {
    btn.textContent = document.documentElement.getAttribute('data-theme') === 'dark' ? '☀' : '☽';
  }

  syncIcon();

  btn.addEventListener('click', () => {
    const next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', next);
    localStorage.setItem('kai-theme', next);
    syncIcon();
  });
})();
```

- [ ] **Step 5: Verify in browser**

- Page loads in dark mode by default (or light if your OS is set to light).
- Click the ☀/☽ button — the entire page color scheme switches instantly.
- Reload — the chosen theme persists.
- In DevTools console: `localStorage.getItem('kai-theme')` returns `"light"` or `"dark"`.

- [ ] **Step 6: Commit**

```bash
git add docs/scale-tests/index.html docs/scale-tests/styles.css docs/scale-tests/app.js
git commit -m "feat(docs): add light/dark theme toggle with localStorage persistence"
```

---

### Task 3: Theme-aware chart colors in `metrics.js`

Chart.js options use hardcoded hex values for tooltips, grids, and ticks that don't respond to CSS variables. This task replaces them with values read from CSS at chart-creation time, and updates the first palette color from blue to KAI pink.

**Files:**
- Modify: `docs/scale-tests/metrics.js`

**Interfaces:**
- Consumes: CSS custom properties on `document.documentElement` set by Task 2.
- Produces: `cssVar(name)` helper available within `metrics.js`; `CHART_COLORS[0]` is KAI pink.

- [ ] **Step 1: Add `cssVar` helper and update `CHART_COLORS` in `metrics.js`**

Add the helper immediately after the `'use strict';` line (around line 4):

```js
function cssVar(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}
```

Replace the `CHART_COLORS` array (around line 182):

```js
const CHART_COLORS = [
  '#e8196c', // KAI pink
  '#3fb950', // pass green
  '#d29922', // skip yellow
  '#f85149', // fail red
  '#a371f7', // purple
  '#ea6045', // orange
  '#79c0ff', // light blue
  '#56d364', // light green
];
```

- [ ] **Step 2: Replace hardcoded hex values in `createChart()` with `cssVar()` calls**

In the `new Chart(ctx, { options: { ... } })` block inside `createChart()`, replace all hardcoded color strings:

```js
plugins: {
  legend: {
    display: true,
    position: 'top',
    labels: {
      color: cssVar('--text'),
      font: { size: 11 },
      padding: 10,
      usePointStyle: true,
    },
  },
  tooltip: {
    backgroundColor: cssVar('--surface'),
    titleColor: cssVar('--text'),
    bodyColor: cssVar('--text'),
    borderColor: cssVar('--border'),
    borderWidth: 1,
    padding: 10,
    displayColors: true,
    callbacks: {
      title: (items) => {
        if (items[0]) {
          const lines = [
            new Date(items[0].parsed.x).toLocaleDateString('en-US', {
              year: 'numeric', month: 'short', day: 'numeric',
              hour: '2-digit', minute: '2-digit',
            }),
          ];
          const raw = items[0].chart.data.datasets[items[0].datasetIndex].data[items[0].dataIndex];
          if (raw.commit) lines.push(`Commit: ${raw.commit.substring(0, 8)}`);
          return lines;
        }
        return '';
      },
      label: (context) => `${context.dataset.label}: ${context.parsed.y.toFixed(3)}s`,
    },
  },
},
scales: {
  x: {
    type: 'time',
    time: {
      unit: 'day',
      displayFormats: { day: 'MMM d' },
    },
    grid: {
      color: cssVar('--border'),
      drawBorder: false,
    },
    ticks: {
      color: cssVar('--muted'),
      font: { size: 10 },
    },
  },
  y: {
    beginAtZero: false,
    grace: '10%',
    grid: {
      color: cssVar('--border'),
      drawBorder: false,
    },
    ticks: {
      color: cssVar('--muted'),
      font: { size: 10 },
      callback: (value) => `${value.toFixed(3)}s`,
    },
    title: {
      display: true,
      text: config.label,
      color: cssVar('--muted'),
      font: { size: 11 },
    },
  },
},
```

- [ ] **Step 3: Verify in browser**

- Load page in dark mode — charts render with navy grids and pink first-series color.
- Toggle to light mode — reload (theme-switch-after-render is a known limitation per spec). Charts should render with light backgrounds.

- [ ] **Step 4: Commit**

```bash
git add docs/scale-tests/metrics.js
git commit -m "feat(docs): use CSS variables for chart colors, switch first color to KAI pink"
```

---

### Task 4: Per-chart latest-value stat chips

Each chart card gets a row of chips showing the most recent measured value per legend group, with a trend arrow comparing it to the previous 3 data points.

**Files:**
- Modify: `docs/scale-tests/index.html` — add `<div class="chart-stats" id="stats-N">` to each chart card
- Modify: `docs/scale-tests/styles.css` — `.chart-stats`, `.stat-chip`, `.stat-label`, `.stat-value`, `.stat-trend` styles
- Modify: `docs/scale-tests/metrics.js` — `formatSeconds()`, `trendArrow()`, `renderChartStats()` helpers; call from `createChart()`

**Interfaces:**
- Consumes: `grouped` object from `groupByLegend()` (already available inside `createChart()`); `esc()` from `app.js` (global).
- Produces: `renderChartStats(statsId, grouped)` populates the stats div for a given chart.

- [ ] **Step 1: Add stats divs to each chart card in `index.html`**

Inside each `.chart-card`, insert `<div class="chart-stats" id="stats-N">` between the `<h3>` title and the `<div class="chart-wrapper">`:

```html
<div class="chart-card">
  <h3 class="chart-title">Fill Cluster with single GPU Jobs</h3>
  <div class="chart-stats" id="stats-1"></div>
  <div class="chart-wrapper"><canvas id="chart-1"></canvas></div>
</div>
<div class="chart-card">
  <h3 class="chart-title">Schedules Jobs with Pending Tasks in Background</h3>
  <div class="chart-stats" id="stats-2"></div>
  <div class="chart-wrapper"><canvas id="chart-2"></canvas></div>
</div>
<div class="chart-card">
  <h3 class="chart-title">Allocate Single Distributed Job (with preferred topology)</h3>
  <div class="chart-stats" id="stats-3"></div>
  <div class="chart-wrapper"><canvas id="chart-3"></canvas></div>
</div>
<div class="chart-card">
  <h3 class="chart-title">Allocate Single Distributed Job (without preferred topology)</h3>
  <div class="chart-stats" id="stats-4"></div>
  <div class="chart-wrapper"><canvas id="chart-4"></canvas></div>
</div>
```

- [ ] **Step 2: Add stat chip styles to `styles.css`**

Append after the theme toggle styles added in Task 2:

```css
/* ── Stat chips ──────────────────────────────────────────────────────────── */

.chart-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
  min-height: 28px;
}

.stat-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 10px;
  background: var(--surface2);
  border: 1px solid var(--border);
  border-radius: 20px;
  font-size: 12px;
}

.stat-label { color: var(--muted); }

.stat-value {
  font-family: monospace;
  font-weight: 600;
  color: var(--text);
}

.stat-trend { font-weight: 700; font-size: 13px; }
.stat-trend.down { color: var(--pass); }
.stat-trend.up   { color: var(--fail); }
.stat-trend.flat { color: var(--muted); }
```

- [ ] **Step 3: Add helper functions to `metrics.js`**

Add these three functions after the `cssVar` helper added in Task 3:

```js
function formatSeconds(s) {
  if (s === null || s === undefined) return '—';
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  return `${m}m ${Math.round(s % 60)}s`;
}

function trendArrow(latest, prevMean) {
  const pct = (latest - prevMean) / prevMean;
  if (pct < -0.05) return '<span class="stat-trend down">↓</span>';
  if (pct >  0.05) return '<span class="stat-trend up">↑</span>';
  return '<span class="stat-trend flat">→</span>';
}

function renderChartStats(statsId, grouped) {
  const el = document.getElementById(statsId);
  if (!el) return;

  const chips = Object.entries(grouped).map(([legend, points]) => {
    const latest = points.length ? points[points.length - 1].value : null;
    const prev   = points.length >= 2 ? points.slice(-4, -1).map(p => p.value) : null;
    const prevMean = prev && prev.length
      ? prev.reduce((a, b) => a + b, 0) / prev.length
      : null;
    const arrow = latest !== null && prevMean !== null
      ? trendArrow(latest, prevMean)
      : '';
    return `<span class="stat-chip">
      <span class="stat-label">${esc(legend)}</span>
      <span class="stat-value">${formatSeconds(latest)}</span>
      ${arrow}
    </span>`;
  }).join('');

  el.innerHTML = chips;
}
```

- [ ] **Step 4: Call `renderChartStats` from `createChart()` in `metrics.js`**

`createChart` ends with `new Chart(ctx, { ... })`. Add two lines immediately after that closing `);`:

```js
  // existing last line of createChart:
  new Chart(ctx, { type: 'line', data: { datasets }, options: { ... } });

  // ADD THESE TWO LINES:
  const statsId = canvasId.replace('chart-', 'stats-');
  renderChartStats(statsId, grouped);
}   // end of createChart
```

`grouped` is already in scope — it's computed earlier in `createChart` via `const grouped = groupByLegend(dataPoints);`.
```

- [ ] **Step 5: Verify in browser**

- Open `http://localhost:8080` — Metrics tab loads, each chart card shows stat chips between the title and the chart.
- The mock data has 3 runs for the same report, so each chart may show chips with `→` trend (values are identical across runs). This is correct behavior.
- If a chart has no matching data, its `chart-stats` div stays empty — verify no JS errors occur.
- Toggle light mode (reload) — stat chips render with correct light-mode colors.

- [ ] **Step 6: Commit**

```bash
git add docs/scale-tests/index.html docs/scale-tests/styles.css docs/scale-tests/metrics.js
git commit -m "feat(docs): add per-chart latest-value stat chips with trend arrows"
```

---

## Cleanup

The mock data created during design (`docs/scale-tests/Public/`) should **not** be committed — it's only for local testing. Verify it's gitignored or remove it before opening a PR.

```bash
# Check if it's tracked
git status docs/scale-tests/Public/

# If tracked, remove from index but keep locally
git rm -r --cached docs/scale-tests/Public/
echo "docs/scale-tests/Public/" >> .gitignore
git commit -m "chore: gitignore local mock data directory"
```
