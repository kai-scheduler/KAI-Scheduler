// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

'use strict';

const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

class FakeClassList {
  constructor(...values) { this.values = new Set(values); }
  add(value) { this.values.add(value); }
  remove(value) { this.values.delete(value); }
  contains(value) { return this.values.has(value); }
  toggle(value, force) {
    const enabled = force === undefined ? !this.contains(value) : force;
    if (enabled) this.add(value); else this.remove(value);
    return enabled;
  }
}

class FakeElement {
  constructor(id, elements) {
    this.id = id;
    this.elements = elements;
    this.classList = new FakeClassList();
    this.dataset = {};
    this.listeners = {};
    this.textContent = '';
    this._innerHTML = '';
  }
  set innerHTML(value) {
    this._innerHTML = value;
    for (const match of value.matchAll(/<canvas id="([^"]+)"/g)) {
      this.elements.set(match[1], new FakeElement(match[1], this.elements));
    }
  }
  get innerHTML() { return this._innerHTML; }
  addEventListener(type, listener) { this.listeners[type] = listener; }
  click() { this.listeners.click?.({ target: this }); }
  setAttribute() {}
  querySelector() { return null; }
  getContext() { return {}; }
}

function createBrowserContext(scalePayload, ginkgoPayload) {
  const elements = new Map();
  const element = (id) => {
    if (!elements.has(id)) elements.set(id, new FakeElement(id, elements));
    return elements.get(id);
  };

  const runsTab = element('runs-tab');
  runsTab.dataset.tab = 'runs';
  const metricsTab = element('metrics-tab');
  metricsTab.dataset.tab = 'metrics';
  metricsTab.classList.add('active');
  const summary = element('summary');
  summary.dataset.content = 'runs';
  summary.classList.add('hidden');
  const main = element('main');
  main.dataset.content = 'runs';
  main.classList.add('hidden');
  const metricsMain = element('metrics-main');
  metricsMain.dataset.content = 'metrics';

  ['search', 'sort-dir', 'fp-pass', 'fp-fail', 'fp-skip', 'header-stats', 'metrics-grid'].forEach(element);

  const windowListeners = {};
  const renderedCharts = [];
  class Chart {
    static register() {}
    constructor(_context, config) { this.config = config; renderedCharts.push(this); }
    destroy() {}
  }

  const manifest = {
    runs: [
      { timestamp: '2026-06-29T08:42:33Z', path: 'Public/results.json', commit: 'current' },
      { timestamp: '2026-06-28T08:42:33Z', path: 'Public/report.json', commit: 'legacy' },
    ],
  };

  const context = {
    Chart,
    CustomEvent: class CustomEvent { constructor(type) { this.type = type; } },
    console,
    setTimeout,
    clearTimeout,
    fetch: async (url) => ({
      ok: true,
      json: async () => url.endsWith('manifest.json') ? manifest
        : url.endsWith('results.json') ? scalePayload : ginkgoPayload,
    }),
  };
  context.document = {
    getElementById: id => element(id),
    querySelectorAll: selector => {
      if (selector === '.tab') return [runsTab, metricsTab];
      if (selector === '.tab-content') return [summary, main, metricsMain];
      return [];
    },
  };
  context.window = context;
  context.window.SCALE_TESTS_S3_BASE_URL = 'https://example.invalid';
  context.window.addEventListener = (type, listener) => { windowListeners[type] = listener; };
  context.window.dispatchEvent = event => windowListeners[event.type]?.(event);

  return { context: vm.createContext(context), elements, metricsTab, renderedCharts };
}

async function waitFor(predicate) {
  for (let attempt = 0; attempt < 50; attempt++) {
    if (predicate()) return;
    await new Promise(resolve => setImmediate(resolve));
  }
  throw new Error('Dashboard did not finish loading');
}

test('opens the metrics tab by default', () => {
  const html = readFileSync(join(__dirname, 'index.html'), 'utf8');

  assert.match(html, /<button class="tab" data-tab="runs">Test Runs<\/button>/);
  assert.match(html, /<button class="tab active" data-tab="metrics">Metrics<\/button>/);
  assert.match(html, /<div class="controls tab-content hidden" data-content="runs">/);
  assert.match(html, /<div class="summary tab-content hidden" data-content="runs" id="summary"/);
  assert.match(html, /<main id="main" class="tab-content hidden" data-content="runs">/);
  assert.match(html, /<main id="metrics-main" class="tab-content" data-content="metrics">/);
});

test('versions local dashboard assets with the deployed commit', () => {
  const html = readFileSync(join(__dirname, 'index.html'), 'utf8');
  const workflow = readFileSync(join(__dirname, '../../.github/workflows/deploy-scale-tests-page.yaml'), 'utf8');
  const assets = ['styles.css', 'results.js', 'results-view.js', 'metrics-data.js', 'app.js', 'metrics.js'];

  assets.forEach(asset => assert.ok(html.includes(`${asset}?v=__ASSET_VERSION__`), asset));
  assert.match(workflow, /__ASSET_VERSION__.*GITHUB_SHA/);
});

test('renders both result formats and eleven unified charts', async () => {
  const scalePayload = JSON.parse(readFileSync(join(__dirname, 'example-results.json'), 'utf8'));
  const ginkgoPayload = JSON.parse(readFileSync(join(__dirname, 'example-report.json'), 'utf8'));
  const browser = createBrowserContext(scalePayload, ginkgoPayload);

  for (const file of ['results.js', 'results-view.js', 'metrics-data.js', 'app.js']) {
    vm.runInContext(readFileSync(join(__dirname, file), 'utf8'), browser.context, { filename: file });
  }

  await waitFor(() => browser.context.window.allRuns?.length === 2);
  assert.equal(browser.renderedCharts.length, 0);
  vm.runInContext(readFileSync(join(__dirname, 'metrics.js'), 'utf8'), browser.context, { filename: 'metrics.js' });
  assert.match(browser.elements.get('main').innerHTML, /source-results/);
  assert.match(browser.elements.get('main').innerHTML, /source-legacy/);
  assert.match(browser.elements.get('main').innerHTML, /total requested gpus/);
  assert.match(browser.elements.get('main').innerHTML, /kai_scheduler_ref/);
  assert.match(browser.elements.get('main').innerHTML, /service_resources/);
  assert.doesNotMatch(browser.elements.get('main').innerHTML, /kai_commit_hash/);
  assert.doesNotMatch(browser.elements.get('main').innerHTML, /test_focus/);

  assert.equal(browser.renderedCharts.length, 11);
  assert.ok(browser.renderedCharts.every(chart => (
    chart.config.options.plugins.migrationLine.timestamp === '2026-06-29T08:42:33Z'
  )));
  assert.equal(browser.renderedCharts.flatMap(chart => chart.config.data.datasets).length, 17);
  const durationAxis = browser.renderedCharts[0].config.options.scales.y;
  assert.equal(durationAxis.title.text, 'Duration');
  assert.equal(durationAxis.ticks.callback(396.5), '6m 36.5s');
});
