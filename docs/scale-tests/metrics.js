// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

'use strict';

const CHART_COLORS = [
  '#58a6ff', '#3fb950', '#d29922', '#f85149', '#a371f7', '#ea6045',
  '#79c0ff', '#56d364', '#e3b341', '#ff7b72', '#d2a8ff', '#ffa657',
];

const chartInstances = new Map();

const migrationLinePlugin = {
  id: 'migrationLine',
  afterDatasetsDraw(chart, _args, options) {
    if (!options?.timestamp) return;
    const x = chart.scales.x.getPixelForValue(new Date(options.timestamp));
    const { top, bottom, left, right } = chart.chartArea;
    if (x < left || x > right) return;

    const { ctx } = chart;
    ctx.save();
    ctx.strokeStyle = '#f0b429';
    ctx.lineWidth = 1.5;
    ctx.setLineDash([5, 4]);
    ctx.beginPath();
    ctx.moveTo(x, top);
    ctx.lineTo(x, bottom);
    ctx.stroke();
    ctx.setLineDash([]);
    ctx.fillStyle = '#f0b429';
    ctx.font = '600 10px "IBM Plex Mono", monospace';
    ctx.textAlign = x > right - 150 ? 'right' : 'left';
    ctx.fillText('New results format', x + (ctx.textAlign === 'right' ? -6 : 6), top + 12);
    ctx.restore();
  },
};

Chart.register(migrationLinePlugin);

function renderChartCards() {
  const grid = document.getElementById('metrics-grid');
  grid.innerHTML = ScaleTestMetrics.CHART_CONFIGS.map(config => `
    <section class="chart-card">
      <h3 class="chart-title">${config.title}</h3>
      <div class="chart-wrapper"><canvas id="chart-${config.id}"></canvas></div>
    </section>`).join('');
}

function tooltipDetails(context) {
  return ScaleTestMetrics.buildTooltipLines(context.raw.observation);
}

function createChart(config, observations, migrationTimestamp) {
  const canvasId = `chart-${config.id}`;
  const canvas = document.getElementById(canvasId);
  if (!canvas) return;

  chartInstances.get(config.id)?.destroy();
  const groups = ScaleTestMetrics.groupCompatibleObservations(observations);
  const datasets = groups.map((group, index) => ({
    label: group.seriesLabel,
    data: group.observations.map(observation => ({
      x: new Date(observation.timestamp),
      y: observation.timingSeconds,
      observation,
    })),
    borderColor: CHART_COLORS[index % CHART_COLORS.length],
    backgroundColor: `${CHART_COLORS[index % CHART_COLORS.length]}20`,
    borderWidth: 2,
    pointRadius: 4,
    pointHoverRadius: 6,
    tension: 0.1,
  }));

  const chart = new Chart(canvas.getContext('2d'), {
    type: 'line',
    data: { datasets },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: 'nearest', intersect: false },
      plugins: {
        legend: {
          display: true,
          position: 'bottom',
          labels: { color: '#c9d1d9', boxWidth: 18, boxHeight: 2, font: { size: 10 } },
        },
        migrationLine: { timestamp: migrationTimestamp },
        tooltip: {
          callbacks: {
            title: items => items.length ? new Date(items[0].raw.x).toLocaleString() : '',
            label: context => `${context.dataset.label}: ${context.raw.observation.rawTiming}`,
            afterLabel: tooltipDetails,
          },
        },
      },
      scales: {
        x: {
          type: 'time',
          time: { unit: 'day', tooltipFormat: 'PPpp' },
          grid: { color: '#30363d', drawBorder: false },
          ticks: { color: '#8b949e', font: { size: 10 } },
        },
        y: {
          beginAtZero: false,
          grace: '10%',
          grid: { color: '#30363d', drawBorder: false },
          ticks: {
            color: '#8b949e',
            font: { size: 10 },
            callback: ScaleTestMetrics.formatDuration,
          },
          title: { display: true, text: config.label, color: '#8b949e', font: { size: 11 } },
        },
      },
    },
  });
  chartInstances.set(config.id, chart);
}

function initializeMetrics() {
  if (!window.allRuns?.length) {
    console.log('[metrics] No data available yet');
    return;
  }

  renderChartCards();
  const observations = ScaleTestMetrics.extractChartObservations(window.allRuns);
  const migrationTimestamp = ScaleTestMetrics.findMigrationTimestamp(window.allRuns);

  ScaleTestMetrics.CHART_CONFIGS.forEach(config => {
    const chartObservations = observations.filter(observation => observation.chartId === config.id);
    createChart(config, chartObservations, migrationTimestamp);
  });
}

function initializeVisibleMetrics() {
  const metricsMain = document.getElementById('metrics-main');
  if (!metricsMain || metricsMain.classList.contains('hidden') || !window.allRuns?.length) return false;

  window._metricsInitialized = true;
  initializeMetrics();
  return true;
}

function initializeTabs() {
  const tabs = document.querySelectorAll('.tab');
  const contents = document.querySelectorAll('.tab-content');

  tabs.forEach(tab => {
    tab.addEventListener('click', () => {
      const targetTab = tab.dataset.tab;
      tabs.forEach(item => item.classList.remove('active'));
      tab.classList.add('active');
      contents.forEach(content => content.classList.toggle('hidden', content.dataset.content !== targetTab));

      if (targetTab === 'metrics' && !window._metricsInitialized) initializeVisibleMetrics();
    });
  });
}

initializeTabs();
initializeVisibleMetrics();

window.addEventListener('scale-tests:data-loaded', () => {
  if (!initializeVisibleMetrics()) window._metricsInitialized = false;
});
