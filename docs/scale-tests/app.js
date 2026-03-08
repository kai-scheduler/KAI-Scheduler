// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

'use strict';

// Injected by the deploy workflow. Falls back to window override or example data.
// When __S3_BASE_URL__ has not been replaced, '.' is used so that fetch paths like
// ./public/manifest.json resolve correctly relative to the page.
const _s3raw = typeof window.SCALE_TESTS_S3_BASE_URL !== 'undefined'
  ? window.SCALE_TESTS_S3_BASE_URL : '__S3_BASE_URL__';
const S3_BASE_URL = (_s3raw === '__S3_BASE_URL__' ? '.' : _s3raw).replace(/\/$/, '');

const THIRTY_DAYS_MS = 30 * 24 * 60 * 60 * 1000;

// ── State ──────────────────────────────────────────────────────────────────

let allRuns    = [];   // { timestamp, path, suite, specs }[]  (populated progressively)
let sortKey    = 'time';
let sortAsc    = false;
let filterPass = true;
let filterFail = true;
let filterSkip = true;
let searchQuery = '';

// ── Utilities ──────────────────────────────────────────────────────────────

function esc(s) {
  return String(s ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

function nsToHuman(ns) {
  if (!ns) return '—';
  const s = ns / 1e9;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${Math.round(s % 60)}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

function fmtDate(iso) {
  if (!iso) return '—';
  const d = new Date(iso);
  return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' }) +
    ' ' + d.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' });
}

function fmtTime(iso) {
  if (!iso) return '—';
  return new Date(iso).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function stateIcon(state) {
  switch (state) {
    case 'passed':  return '<span class="c-pass" aria-label="passed">✓</span>';
    case 'failed':  return '<span class="c-fail" aria-label="failed">✗</span>';
    case 'skipped': return '<span class="c-skip" aria-label="skipped">—</span>';
    case 'pending': return '<span class="c-muted" aria-label="pending">○</span>';
    default:        return '<span class="c-muted">?</span>';
  }
}

function badgeHtml(state) {
  const cls = state === 'passed' ? 'pass' : state === 'failed' ? 'fail' : 'skip';
  return `<span class="badge badge-${cls}">${stateIcon(state)} ${esc(state)}</span>`;
}

function specFullText(spec) {
  return [...(spec.ContainerHierarchyTexts || []), spec.LeafNodeText || ''].join(' › ');
}

// ── Filtering ──────────────────────────────────────────────────────────────

function specSearchable(spec) {
  const parts = [
    specFullText(spec),
    spec.State || '',
    ...(spec.LeafNodeLabels || []),
    spec.CapturedGinkgoWriterOutput || '',
    spec.CapturedStdOutErr || '',
    spec.Failure?.Message || '',
    spec.Failure?.Location ? `${spec.Failure.Location.FileName}:${spec.Failure.Location.LineNumber}` : '',
    ...(spec.SpecialSuiteFailureReasons || []),
    ...(spec.ReportEntries || []).map(e => `${e.Name} ${e.Value?.AsJSON || JSON.stringify(e.Value || '')}`),
    ...(spec.AdditionalFailures || []).map(f => f.Message || ''),
  ];
  return parts.join(' ').toLowerCase();
}

function specVisible(spec) {
  if (!filterPass && spec.State === 'passed')   return false;
  if (!filterFail && spec.State === 'failed')   return false;
  if (!filterSkip && (spec.State === 'skipped' || spec.State === 'pending')) return false;
  if (searchQuery && !specSearchable(spec).includes(searchQuery.toLowerCase())) return false;
  return true;
}

// ── Rendering ──────────────────────────────────────────────────────────────

let _detailUid = 0;
function uid() { return `d${++_detailUid}`; }

function renderFailure(f) {
  if (!f?.Message) return '';
  const loc = f.Location ? `${esc(f.Location.FileName || '')}:${f.Location.LineNumber || ''}` : '';
  return `
    <div class="detail-section">
      <div class="detail-title">Failure</div>
      <div class="pre err">${esc(f.Message)}</div>
      ${loc ? `<div style="margin-top:6px;font-size:11px;color:var(--muted);font-family:monospace">${loc}</div>` : ''}
      ${f.ForwardedPanics ? `<div class="pre err" style="margin-top:8px">${esc(f.ForwardedPanics)}</div>` : ''}
    </div>`;
}

function renderReportEntries(entries) {
  if (!entries?.length) return '';
  const html = entries.map(e => {
    let val = '';
    try {
      val = e.Value?.AsJSON
        ? JSON.stringify(JSON.parse(e.Value.AsJSON), null, 2)
        : JSON.stringify(e.Value ?? '', null, 2);
    } catch { val = String(e.Value ?? ''); }
    return `<div class="entry-block">
      <div class="entry-name">${esc(e.Name)}</div>
      <div class="entry-val">${esc(val)}</div>
    </div>`;
  }).join('');
  return `<div class="detail-section"><div class="detail-title">Report Entries</div>${html}</div>`;
}

function renderSpecDetail(spec, detailId) {
  const hasContent = spec.Failure?.Message
    || spec.ReportEntries?.length
    || spec.CapturedGinkgoWriterOutput
    || spec.CapturedStdOutErr
    || spec.AdditionalFailures?.length;

  if (!hasContent) return '';

  const loc = spec.LeafNodeLocation
    ? `${esc(spec.LeafNodeLocation.FileName || '')}:${spec.LeafNodeLocation.LineNumber || ''}`
    : '—';

  const sections = [
    `<div class="detail-section">
      <div class="detail-title">Spec Info</div>
      <div class="kv">
        <span class="kv-k">Type</span>      <span class="kv-v">${esc(spec.LeafNodeType || '')}</span>
        <span class="kv-k">State</span>     <span class="kv-v">${esc(spec.State || '')}</span>
        <span class="kv-k">Location</span>  <span class="kv-v">${loc}</span>
        <span class="kv-k">Started</span>   <span class="kv-v">${esc(fmtTime(spec.StartTime))}</span>
        <span class="kv-k">Ended</span>     <span class="kv-v">${esc(fmtTime(spec.EndTime))}</span>
        <span class="kv-k">Duration</span>  <span class="kv-v">${esc(nsToHuman(spec.RunTime))}</span>
        <span class="kv-k">Attempts</span>  <span class="kv-v">${spec.NumAttempts ?? 1}${spec.MaxFlakeAttempts ? ` / ${spec.MaxFlakeAttempts} max flake` : ''}</span>
        ${spec.IsSerial ? `<span class="kv-k">Serial</span><span class="kv-v">yes</span>` : ''}
        ${spec.IsInOrderedContainer ? `<span class="kv-k">Ordered</span><span class="kv-v">yes</span>` : ''}
      </div>
    </div>`,

    renderFailure(spec.Failure),

    ...(spec.AdditionalFailures || []).map((f, i) =>
      renderFailure({ ...f, Message: `[Additional failure ${i + 1}] ${f.Message || ''}` })
    ),

    renderReportEntries(spec.ReportEntries),

    spec.CapturedGinkgoWriterOutput ? `
      <div class="detail-section">
        <div class="detail-title">Output</div>
        <div class="pre">${esc(spec.CapturedGinkgoWriterOutput)}</div>
      </div>` : '',

    spec.CapturedStdOutErr ? `
      <div class="detail-section">
        <div class="detail-title">Stdout / Stderr</div>
        <div class="pre">${esc(spec.CapturedStdOutErr)}</div>
      </div>` : '',
  ];

  return `<div class="spec-detail" id="${detailId}">${sections.join('')}</div>`;
}

function renderSpec(spec) {
  if (!specVisible(spec)) return '';

  const hierarchy = (spec.ContainerHierarchyTexts || []).join(' › ');
  const labels    = (spec.LeafNodeLabels || []).map(l => `<span class="label">${esc(l)}</span>`).join('');
  const detailId  = uid();
  const detail    = renderSpecDetail(spec, detailId);
  const isMatch   = searchQuery && specSearchable(spec).includes(searchQuery.toLowerCase());

  return `
    <div class="spec-row${isMatch ? ' match' : ''}">
      <div class="spec-state-icon">${stateIcon(spec.State)}</div>
      <div class="spec-body">
        ${hierarchy ? `<div class="spec-path">${esc(hierarchy)}</div>` : ''}
        <div class="spec-leaf${spec.State === 'failed' ? ' c-fail' : ''}">${esc(spec.LeafNodeText || '')}</div>
        ${labels ? `<div class="spec-labels">${labels}</div>` : ''}
        <div class="spec-meta">
          <span class="mono">${esc(nsToHuman(spec.RunTime))}</span>
          <span>${esc(fmtTime(spec.StartTime))}</span>
          ${spec.NumAttempts > 1 ? `<span class="c-skip">${spec.NumAttempts} attempts</span>` : ''}
        </div>
        ${detail}
      </div>
      ${detail ? `<button class="detail-btn" onclick="toggleDetail('${detailId}')">details</button>` : ''}
    </div>`;
}

function renderSuiteMeta(suite) {
  const warnings = suite.SpecialSuiteFailureReasons || [];
  const pre      = suite.PreRunStats || {};
  return `
    <div class="suite-meta">
      <span>Suite: <strong>${esc(suite.SuiteDescription || 'Scale Suite')}</strong></span>
      <span>Start: <strong>${esc(fmtDate(suite.StartTime))}</strong></span>
      <span>Duration: <strong>${esc(nsToHuman(suite.RunTime))}</strong></span>
      ${pre.TotalSpecs     != null ? `<span>Total specs: <strong>${pre.TotalSpecs}</strong></span>` : ''}
      ${pre.SpecsThatWillRun != null ? `<span>Will run: <strong>${pre.SpecsThatWillRun}</strong></span>` : ''}
      ${suite.RunningInParallel ? `<span>Parallel: <strong>yes</strong></span>` : ''}
      ${warnings.map(w => `<span class="suite-warn">${esc(w)}</span>`).join('')}
    </div>`;
}

// ── Sorting ────────────────────────────────────────────────────────────────

function sortedSpecs(specs) {
  const copy = [...specs];
  if (sortKey === 'name') {
    copy.sort((a, b) => {
      const diff = specFullText(a).localeCompare(specFullText(b));
      return sortAsc ? diff : -diff;
    });
  } else if (sortKey === 'status') {
    const order = { failed: 0, skipped: 1, pending: 2, passed: 3 };
    copy.sort((a, b) => {
      const diff = (order[a.State] ?? 4) - (order[b.State] ?? 4);
      return sortAsc ? -diff : diff;
    });
  }
  // 'time': keep original execution order from ginkgo report
  return copy;
}

function sortedRuns() {
  const copy = [...allRuns];
  if (sortKey === 'time') {
    copy.sort((a, b) => {
      const diff = new Date(a.timestamp) - new Date(b.timestamp);
      return sortAsc ? diff : -diff;
    });
  } else if (sortKey === 'status') {
    copy.sort((a, b) => {
      const af   = a.specs.some(s => s.State === 'failed') ? 0 : 1;
      const bf   = b.specs.some(s => s.State === 'failed') ? 0 : 1;
      const diff = af - bf;
      return sortAsc ? -diff : diff;
    });
  } else if (sortKey === 'name') {
    copy.sort((a, b) => {
      const diff = (a.suite?.SuiteDescription || '').localeCompare(b.suite?.SuiteDescription || '');
      return sortAsc ? diff : -diff;
    });
  }
  return copy;
}

// ── Run card ───────────────────────────────────────────────────────────────

function renderRun(run, runIdx) {
  const specs    = sortedSpecs(run.specs);
  const specHtml = specs.map(renderSpec).join('');
  if (!specHtml) return '';

  const tsStr  = fmtDate(run.timestamp);
  const passed  = run.specs.filter(s => s.State === 'passed').length;
  const failed  = run.specs.filter(s => s.State === 'failed').length;
  const skipped = run.specs.filter(s => s.State === 'skipped' || s.State === 'pending').length;
  const overall = failed > 0 ? 'failed' : 'passed';
  const autoOpen = searchQuery.length > 0 || failed > 0;
  const runId  = `run-${runIdx}`;

  return `
    <div class="run-card${autoOpen ? ' open' : ''}" id="${runId}">
      <div class="run-header" onclick="toggleRun('${runId}')" role="button" tabindex="0"
           aria-expanded="${autoOpen}" onkeydown="if(event.key==='Enter'||event.key===' ')toggleRun('${runId}')">
        <span class="chevron" aria-hidden="true">▶</span>
        <span class="run-ts">${esc(tsStr)}</span>
        <span class="run-desc">${esc(run.suite?.SuiteDescription || 'Scale Suite')}</span>
        <span class="run-dur">${esc(nsToHuman(run.suite?.RunTime))}</span>
        <div class="run-counts">
          ${passed  > 0 ? `<span class="c-pass">✓ ${passed}</span>`  : ''}
          ${failed  > 0 ? `<span class="c-fail">✗ ${failed}</span>`  : ''}
          ${skipped > 0 ? `<span class="c-skip">— ${skipped}</span>` : ''}
          ${badgeHtml(overall)}
        </div>
      </div>
      <div class="run-body">
        ${renderSuiteMeta(run.suite || {})}
        <div class="specs-container">${specHtml}</div>
      </div>
    </div>`;
}

// ── Page render ────────────────────────────────────────────────────────────

function renderAll() {
  const sorted  = sortedRuns();
  const visible = sorted.filter(r => r.specs.some(specVisible));

  const totalRuns  = allRuns.length;
  const passedRuns = allRuns.filter(r => !r.specs.some(s => s.State === 'failed')).length;
  const failedRuns = totalRuns - passedRuns;
  document.getElementById('header-stats').innerHTML = `
    <div class="header-stat">
      <span class="header-stat-val">${totalRuns}</span>
      runs (30d)
    </div>
    <div class="header-stat">
      <span class="header-stat-val c-pass">${passedRuns}</span>
      all passed
    </div>
    ${failedRuns > 0 ? `<div class="header-stat">
      <span class="header-stat-val c-fail">${failedRuns}</span>
      with failures
    </div>` : ''}`;

  const totalSpecs = allRuns.reduce((n, r) => n + r.specs.length, 0);
  document.getElementById('summary').innerHTML =
    `Showing <strong>${visible.length}</strong> of <strong>${totalRuns}</strong> runs
     &nbsp;·&nbsp; <strong>${totalSpecs}</strong> total specs
     ${searchQuery ? `&nbsp;·&nbsp; filtering by <strong>"${esc(searchQuery)}"</strong>` : ''}`;

  const html = visible.map((r, i) => renderRun(r, i)).join('');
  document.getElementById('main').innerHTML =
    html || '<div class="msg">No results match your filters.</div>';
}

// ── Toggle helpers (called from inline onclick attributes) ─────────────────

function toggleRun(id) {
  const el = document.getElementById(id);
  if (!el) return;
  const open = el.classList.toggle('open');
  el.querySelector('.run-header')?.setAttribute('aria-expanded', String(open));
}

function toggleDetail(id) {
  const el = document.getElementById(id);
  if (el) el.classList.toggle('open');
}

// ── Data loading ───────────────────────────────────────────────────────────

async function loadManifest() {
  const res = await fetch(`${S3_BASE_URL}/public/manifest.json`, { cache: 'no-cache' });
  if (!res.ok) throw new Error(`manifest.json: HTTP ${res.status}`);
  return res.json();
}

async function loadReport(path) {
  const res = await fetch(`${S3_BASE_URL}/${path}`, { cache: 'no-cache' });
  if (!res.ok) throw new Error(`${path}: HTTP ${res.status}`);
  return res.json();
}

function processReport(reportJson, meta) {
  // ginkgo --json-report produces an array of Report objects
  const reports = Array.isArray(reportJson) ? reportJson : [reportJson];
  const suite   = reports[0] || {};
  // Show "It" specs plus any non-It nodes that failed (e.g. BeforeAll failures)
  const specs   = (suite.SpecReports || []).filter(
    s => s.LeafNodeType === 'It' || s.State === 'failed'
  );
  return { timestamp: meta.timestamp, path: meta.path, suite, specs };
}

async function init() {
  const mainEl = document.getElementById('main');
  try {
    const manifest = await loadManifest();
    const cutoff   = Date.now() - THIRTY_DAYS_MS;
    const recent   = (manifest.runs || [])
      .filter(r => new Date(r.timestamp).getTime() >= cutoff)
      .sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp));

    document.getElementById('summary').innerHTML =
      `Loading <strong>${recent.length}</strong> runs from the last 30 days…`;

    if (!recent.length) {
      mainEl.innerHTML = '<div class="msg">No runs found in the last 30 days.</div>';
      return;
    }

    // Fetch reports in batches and render progressively
    const BATCH = 5;
    for (let i = 0; i < recent.length; i += BATCH) {
      const batch   = recent.slice(i, i + BATCH);
      const results = await Promise.allSettled(batch.map(r => loadReport(r.path)));
      results.forEach((result, j) => {
        if (result.status === 'fulfilled') {
          allRuns.push(processReport(result.value, batch[j]));
        } else {
          console.warn('[scale-tests] Failed to load report:', batch[j].path, result.reason);
        }
      });
      renderAll();
    }
  } catch (err) {
    mainEl.innerHTML = `<div class="msg err">
      <strong>Error loading results</strong><br><br>${esc(err.message)}
      <br><br><span style="font-size:12px;color:var(--muted)">
        S3 base URL: <code>${esc(S3_BASE_URL)}</code>
      </span>
    </div>`;
    document.getElementById('summary').textContent = '';
    console.error('[scale-tests]', err);
  }
}

// ── Event wiring ───────────────────────────────────────────────────────────

document.getElementById('search').addEventListener('input', e => {
  searchQuery = e.target.value.trim();
  renderAll();
});

document.querySelectorAll('.btn-group[aria-label="Sort order"] button').forEach(btn => {
  btn.addEventListener('click', () => {
    const key = btn.dataset.sort;
    if (sortKey === key) {
      sortAsc = !sortAsc;
    } else {
      sortKey = key;
      sortAsc = key === 'name';
    }
    document.querySelectorAll('.btn-group[aria-label="Sort order"] button')
      .forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    document.getElementById('sort-dir').textContent = sortAsc ? '▲' : '▼';
    renderAll();
  });
});

document.getElementById('sort-dir').addEventListener('click', () => {
  sortAsc = !sortAsc;
  document.getElementById('sort-dir').textContent = sortAsc ? '▲' : '▼';
  renderAll();
});

[['fp-pass', () => { filterPass = !filterPass; return filterPass; }],
 ['fp-fail', () => { filterFail = !filterFail; return filterFail; }],
 ['fp-skip', () => { filterSkip = !filterSkip; return filterSkip; }],
].forEach(([id, toggle]) => {
  document.getElementById(id).addEventListener('click', function () {
    const active = toggle();
    this.classList.toggle('active', active);
    this.setAttribute('aria-pressed', String(active));
    renderAll();
  });
});

init();
