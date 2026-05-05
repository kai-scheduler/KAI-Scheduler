// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

'use strict';

// ── Metric extraction mappings ────────────────────────────────────────────

// Map test names to chart configurations
const CHART_CONFIGS = [
  {
    id: 'chart-1',
    testNamePattern: /^fill cluster with single GPU Jobs$/i,
    excludePattern: /pending tasks/i,
    extractMetric: (entries, metrics) => {
      if (!metrics) metrics = findMetrics(entries);
      return metrics?.time || metrics?.total_time ? parseDuration(metrics.time || metrics.total_time) : null;
    },
    label: 'Time (seconds)',
    legendBuilder: (metrics, containerHierarchy) => {
      const nodes = metrics.nodes || '?';
      const jobs = metrics.jobs || '?';
      // Use last container context to distinguish test variants
      const context = containerHierarchy?.[containerHierarchy.length - 1] || '';
      const variant = context.includes('scheduler disabled') ? 'scheduler disabled' :
                      context.includes('running') ? 'scheduler running' : '';
      return variant ? `${nodes} nodes, ${jobs} jobs (${variant})` : `${nodes} nodes, ${jobs} jobs`;
    },
  },
  {
    id: 'chart-2',
    testNamePattern: /schedules jobs with pending tasks in background/i,
    extractMetric: (entries, metrics) => {
      if (!metrics) metrics = findMetrics(entries);
      return metrics?.time || metrics?.total_time ? parseDuration(metrics.time || metrics.total_time) : null;
    },
    label: 'Time (seconds)',
    legendBuilder: (metrics, containerHierarchy) => `${metrics.nodes || '?'} nodes, ${metrics.jobs || '?'} jobs`,
  },
  {
    id: 'chart-3',
    testNamePattern: /Allocate single distributed job with preferred topology/i,
    extractMetric: (entries, metrics) => {
      if (!metrics) metrics = findMetrics(entries);
      return metrics?.time || metrics?.total_time || metrics?.duration ?
             parseDuration(metrics.time || metrics.total_time) || metrics.duration : null;
    },
    label: 'Time (seconds)',
    legendBuilder: (metrics, containerHierarchy) => `${metrics.nodes || '?'} nodes, ${metrics.jobs || '?'} jobs`,
  },
  {
    id: 'chart-4',
    testNamePattern: /Allocate single distributed job without preferred topology/i,
    extractMetric: (entries, metrics) => {
      if (!metrics) metrics = findMetrics(entries);
      return metrics?.time || metrics?.total_time ? parseDuration(metrics.time || metrics.total_time) : null;
    },
    label: 'Time (seconds)',
    legendBuilder: (metrics, containerHierarchy) => `${metrics.nodes || '?'} nodes, ${metrics.jobs || '?'} jobs`,
  },
];

// ── Utilities ──────────────────────────────────────────────────────────────

function findMetrics(reportEntries) {
  if (!reportEntries || !Array.isArray(reportEntries)) return null;

  for (const entry of reportEntries) {
    if (entry.Name === 'Test Metrics' && entry.Value?.AsJSON) {
      try {
        return JSON.parse(entry.Value.AsJSON);
      } catch (e) {
        console.warn('Failed to parse metrics JSON:', e);
      }
    }
  }
  return null;
}

// Parse metrics from CapturedGinkgoWriterOutput log format
// Example: "Total time"="6m36.848389057s" "nodes"=500 "jobs"=4000
function parseMetricsFromOutput(output) {
  if (!output) return null;

  const metrics = {};

  // Match key="value" or key=value patterns
  const pattern = /"([^"]+)"=(?:"([^"]*)"|(\d+(?:\.\d+)?))/g;
  let match;

  while ((match = pattern.exec(output)) !== null) {
    const key = match[1].toLowerCase().replace(/\s+/g, '_');
    const value = match[2] || match[3];

    // Try to parse as number, otherwise keep as string
    const numValue = parseFloat(value);
    metrics[key] = isNaN(numValue) ? value : numValue;
  }

  return Object.keys(metrics).length > 0 ? metrics : null;
}

function parseDuration(durationStr) {
  if (!durationStr) return null;
  if (typeof durationStr === 'number') return durationStr;

  // Parse duration strings like "9m28.1s", "1h5m30s", "45.2s"
  const parts = String(durationStr).match(/(?:(\d+)h)?(?:(\d+)m)?(?:([\d.]+)s)?/);
  if (!parts) return null;

  const hours = parseInt(parts[1] || 0);
  const minutes = parseInt(parts[2] || 0);
  const seconds = parseFloat(parts[3] || 0);

  return hours * 3600 + minutes * 60 + seconds;
}

// ── Data extraction ────────────────────────────────────────────────────────

function extractMetricsFromRuns(runs, config) {
  const dataPoints = [];

  runs.forEach(run => {
    // Use suite.SpecReports which contains all test results
    const specs = run.suite?.SpecReports || run.specs || [];
    if (!Array.isArray(specs)) return;

    specs.forEach(spec => {
      const testName = spec.LeafNodeText || '';

      // Check if this spec matches the pattern
      if (!config.testNamePattern.test(testName)) return;
      if (config.excludePattern && config.excludePattern.test(testName)) return;

      // Only include passed tests for metrics
      if (spec.State !== 'passed') return;

      // Try ReportEntries first (new format), fall back to parsing output (current format)
      let metrics = findMetrics(spec.ReportEntries);
      if (!metrics) {
        metrics = parseMetricsFromOutput(spec.CapturedGinkgoWriterOutput);
      }

      // Pass metrics to extractMetric for compatibility
      const metric = config.extractMetric(spec.ReportEntries, metrics);
      if (metric === null || metric === undefined) return;

      // Include container hierarchy for distinguishing test variants
      const legend = config.legendBuilder(metrics || {}, spec.ContainerHierarchyTexts);

      dataPoints.push({
        timestamp: new Date(run.timestamp),
        value: metric,
        legend: legend,
        testName: testName,
      });
    });
  });

  // Sort by timestamp
  dataPoints.sort((a, b) => a.timestamp - b.timestamp);

  return dataPoints;
}

function groupByLegend(dataPoints) {
  const groups = {};

  dataPoints.forEach(point => {
    if (!groups[point.legend]) {
      groups[point.legend] = [];
    }
    groups[point.legend].push(point);
  });

  return groups;
}

// ── Chart rendering ────────────────────────────────────────────────────────

// Dark theme colors for Chart.js
const CHART_COLORS = [
  '#58a6ff', // accent blue
  '#3fb950', // pass green
  '#d29922', // skip yellow
  '#f85149', // fail red
  '#a371f7', // purple
  '#ea6045', // orange
  '#79c0ff', // light blue
  '#56d364', // light green
];

function createChart(canvasId, dataPoints, config) {
  const canvas = document.getElementById(canvasId);
  if (!canvas) {
    console.warn(`Canvas ${canvasId} not found`);
    return;
  }

  const ctx = canvas.getContext('2d');
  const grouped = groupByLegend(dataPoints);

  const datasets = Object.entries(grouped).map(([legend, points], idx) => ({
    label: legend,
    data: points.map(p => ({ x: p.timestamp, y: p.value })),
    borderColor: CHART_COLORS[idx % CHART_COLORS.length],
    backgroundColor: CHART_COLORS[idx % CHART_COLORS.length] + '20',
    borderWidth: 2,
    pointRadius: 4,
    pointHoverRadius: 6,
    tension: 0.1,
  }));

  new Chart(ctx, {
    type: 'line',
    data: { datasets },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: {
        mode: 'nearest',
        axis: 'x',
        intersect: false,
      },
      plugins: {
        legend: {
          display: true,
          position: 'top',
          labels: {
            color: '#e6edf3',
            font: { size: 11 },
            padding: 10,
            usePointStyle: true,
          },
        },
        tooltip: {
          backgroundColor: '#161b22',
          titleColor: '#e6edf3',
          bodyColor: '#e6edf3',
          borderColor: '#30363d',
          borderWidth: 1,
          padding: 10,
          displayColors: true,
          callbacks: {
            title: (items) => {
              if (items[0]) {
                return new Date(items[0].parsed.x).toLocaleDateString('en-US', {
                  year: 'numeric',
                  month: 'short',
                  day: 'numeric',
                  hour: '2-digit',
                  minute: '2-digit',
                });
              }
              return '';
            },
            label: (context) => {
              const value = context.parsed.y;
              return `${context.dataset.label}: ${value.toFixed(3)}s`;
            },
          },
        },
      },
      scales: {
        x: {
          type: 'time',
          time: {
            unit: 'day',
            displayFormats: {
              day: 'MMM d',
            },
          },
          grid: {
            color: '#30363d',
            drawBorder: false,
          },
          ticks: {
            color: '#8b949e',
            font: { size: 10 },
          },
        },
        y: {
          beginAtZero: false,
          grace: '10%',
          grid: {
            color: '#30363d',
            drawBorder: false,
          },
          ticks: {
            color: '#8b949e',
            font: { size: 10 },
            callback: (value) => `${value.toFixed(3)}s`,
          },
          title: {
            display: true,
            text: config.label,
            color: '#8b949e',
            font: { size: 11 },
          },
        },
      },
    },
  });
}

// ── Initialization ─────────────────────────────────────────────────────────

function initializeMetrics() {
  // Wait for data to be loaded from app.js
  if (!window.allRuns || window.allRuns.length === 0) {
    console.log('[metrics] No data available yet');
    return;
  }

  console.log(`[metrics] Rendering charts from ${window.allRuns.length} runs`);

  CHART_CONFIGS.forEach(config => {
    const dataPoints = extractMetricsFromRuns(window.allRuns, config);
    console.log(`[metrics] ${config.id}: extracted ${dataPoints.length} data points`);
    createChart(config.id, dataPoints, config);
  });
}

// ── Tab switching ──────────────────────────────────────────────────────────

function initializeTabs() {
  const tabs = document.querySelectorAll('.tab');
  const contents = document.querySelectorAll('.tab-content');

  tabs.forEach(tab => {
    tab.addEventListener('click', () => {
      const targetTab = tab.dataset.tab;

      // Update active tab
      tabs.forEach(t => t.classList.remove('active'));
      tab.classList.add('active');

      // Show/hide content
      contents.forEach(content => {
        if (content.dataset.content === targetTab) {
          content.classList.remove('hidden');
        } else {
          content.classList.add('hidden');
        }
      });

      // Initialize metrics on first view
      if (targetTab === 'metrics' && !window._metricsInitialized) {
        window._metricsInitialized = true;
        initializeMetrics();
      }
    });
  });
}

// ── Start ──────────────────────────────────────────────────────────────────

// Initialize tabs immediately
initializeTabs();

// Listen for data loaded event from app.js
window.addEventListener('scale-tests:data-loaded', () => {
  console.log('[metrics] Data loaded event received');

  // If metrics tab is currently visible, initialize now
  const metricsTab = document.getElementById('metrics-main');
  if (metricsTab && !metricsTab.classList.contains('hidden')) {
    window._metricsInitialized = false;  // Reset flag to allow initialization
    initializeMetrics();
  }
});
