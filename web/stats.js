// stats.js - Token Usage Analytics
var _statsRoles = {};
var _statsFilters = {
  provider: '',
  route: '',
  model: '',
  window: '30d'
};

function renderStats(el) {
  el.innerHTML =
    '<div class="page-header"><h2>Token Usage Analytics</h2>' +
    '<button class="btn" onclick="refreshStats()">↻ Refresh</button></div>' +
    '<div class="card"><div class="toolbar">' +
      '<h3>Filters</h3>' +
      '<select id="st-filter-provider" class="filter-select">' +
        '<option value="">All Providers</option>' +
        '<option value="anthropic">anthropic</option>' +
        '<option value="openai">openai</option>' +
        '<option value="gemini">gemini</option>' +
        '<option value="upstream">upstream</option>' +
      '</select>' +
      '<select id="st-filter-route" class="filter-select">' +
        '<option value="">All Routes</option>' +
        '<option value="local">local</option>' +
        '<option value="apikey">apikey</option>' +
        '<option value="amp">amp</option>' +
        '<option value="upstream">upstream</option>' +
      '</select>' +
      '<input id="st-filter-model" class="filter-select" placeholder="Model name" style="min-width:180px" />' +
      '<select id="st-filter-window" class="filter-select">' +
        '<option value="24h">Last 24h</option>' +
        '<option value="7d">Last 7d</option>' +
        '<option value="14d">Last 14d</option>' +
        '<option value="30d">Last 30d</option>' +
        '<option value="90d">Last 90d</option>' +
        '<option value="all">All Time</option>' +
      '</select>' +
      '<button class="btn-sm" onclick="applyStatsFilters()">Apply</button>' +
      '<button class="btn-sm" onclick="resetStatsFilters()">Reset</button>' +
      '<span id="st-filter-hint" class="page-info"></span>' +
    '</div></div>' +
    '<div id="st-grid" class="stats-row"></div>' +
    '<div class="stats-two-col">' +
      '<div class="card"><h3>Token Breakdown</h3><div id="st-breakdown"></div></div>' +
      '<div class="card"><h3>Route Distribution</h3><div id="st-routes"></div></div>' +
    '</div>' +
    '<div class="card"><h3 id="st-daily-title">Daily Usage</h3><div id="st-daily"></div></div>' +
    '<div class="card"><h3>Hourly Distribution (Last 24h)</h3><div id="st-hourly"></div></div>' +
    '<div class="card"><h3>Provider Distribution</h3><div id="st-prov"></div></div>' +
    '<div class="card"><h3>By Model</h3><div class="table-wrap"><table>' +
      '<thead><tr><th>Model</th><th>Role</th><th>Provider</th><th>Requests</th><th>Errors</th><th>Prompt</th><th>Fresh</th><th>Cache Read</th><th>Output</th><th>Hit Rate</th><th>Total</th></tr></thead>' +
      '<tbody id="st-models"></tbody></table></div></div>';

  syncStatsFilterUI();
  attachStatsFilterListeners();
  refreshStats();
}

function attachStatsFilterListeners() {
  var modelInput = document.getElementById('st-filter-model');
  if (modelInput) {
    modelInput.addEventListener('keydown', function(e) {
      if (e.key === 'Enter') applyStatsFilters();
    });
  }
}

function syncStatsFilterUI() {
  var p = document.getElementById('st-filter-provider');
  var r = document.getElementById('st-filter-route');
  var m = document.getElementById('st-filter-model');
  var w = document.getElementById('st-filter-window');
  if (p) p.value = _statsFilters.provider || '';
  if (r) r.value = _statsFilters.route || '';
  if (m) m.value = _statsFilters.model || '';
  if (w) w.value = _statsFilters.window || '30d';
}

function applyStatsFilters() {
  _statsFilters.provider = (document.getElementById('st-filter-provider').value || '').trim();
  _statsFilters.route = (document.getElementById('st-filter-route').value || '').trim();
  _statsFilters.model = (document.getElementById('st-filter-model').value || '').trim();
  _statsFilters.window = (document.getElementById('st-filter-window').value || '30d').trim();
  refreshStats();
}

function resetStatsFilters() {
  _statsFilters = { provider: '', route: '', model: '', window: '30d' };
  syncStatsFilterUI();
  refreshStats();
}

function buildStatsQuery(extra) {
  var params = new URLSearchParams();
  if (_statsFilters.provider) params.set('provider', _statsFilters.provider);
  if (_statsFilters.route) params.set('route', _statsFilters.route);
  if (_statsFilters.model) params.set('model', _statsFilters.model);
  if (_statsFilters.window && _statsFilters.window !== 'all') params.set('window', _statsFilters.window);
  if (extra) {
    Object.keys(extra).forEach(function(k) {
      if (extra[k] !== undefined && extra[k] !== null && extra[k] !== '') params.set(k, String(extra[k]));
    });
  }
  var qs = params.toString();
  return qs ? ('?' + qs) : '';
}

function windowLabel() {
  var labels = {
    '24h': 'Last 24h',
    '7d': 'Last 7 Days',
    '14d': 'Last 14 Days',
    '30d': 'Last 30 Days',
    '90d': 'Last 90 Days',
    'all': 'All Time'
  };
  return labels[_statsFilters.window] || 'Last 30 Days';
}

function dailyDaysForWindow() {
  switch (_statsFilters.window) {
    case '24h': return 2;
    case '7d': return 7;
    case '14d': return 14;
    case '30d': return 30;
    case '90d': return 90;
    case 'all': return 180;
    default: return 30;
  }
}

function refreshStats() {
  API.get('/api/model-roles').then(function(roles) {
    roles.forEach(function(r) { _statsRoles[r.model] = r; });
  }).catch(function(){});

  var dailyDays = dailyDaysForWindow();

  Promise.all([
    API.get('/api/stats' + buildStatsQuery()),
    API.get('/api/stats/tokens' + buildStatsQuery()),
    API.get('/api/stats/daily' + buildStatsQuery({ days: dailyDays })),
    API.get('/api/stats/hourly' + buildStatsQuery({ hours: 24 })),
    API.get('/api/stats/routes' + buildStatsQuery())
  ]).then(function(results) {
    var s = results[0];
    var tokens = results[1];
    var daily = results[2] || [];
    var hourly = results[3] || [];
    var routes = results[4] || [];

    var hintEl = document.getElementById('st-filter-hint');
    if (hintEl) {
      var parts = [];
      if (_statsFilters.provider) parts.push('provider=' + _statsFilters.provider);
      if (_statsFilters.route) parts.push('route=' + _statsFilters.route);
      if (_statsFilters.model) parts.push('model=' + _statsFilters.model);
      parts.push('window=' + windowLabel());
      hintEl.textContent = parts.join(' | ');
    }

    var dailyTitle = document.getElementById('st-daily-title');
    if (dailyTitle) dailyTitle.textContent = 'Daily Usage (' + windowLabel() + ')';

    renderSummaryGrid(s, tokens);
    renderTokenBreakdown(tokens);
    renderRouteDistribution(routes);
    renderDailyChart(daily);
    renderHourlyChart(hourly);
    renderProviderDist(s);
    renderModelTable(s);
  }).catch(function(e) { console.error('stats error:', e); });
}

function renderSummaryGrid(s, tokens) {
  var grid = document.getElementById('st-grid');
  if (!grid) return;

  var prompt = (tokens && tokens.logical_input) || 0;
  var fresh = (tokens && tokens.fresh_input) || 0;
  var cacheRead = (tokens && tokens.cache_read) || 0;
  var cacheCreate = (tokens && tokens.cache_create) || 0;
  var output = (tokens && tokens.output) || 0;
  var totalTok = (tokens && tokens.total_tokens) || 0;

  grid.innerHTML =
    sBox(fmtNum(s.total_requests), 'Requests', 'blue') +
    sBox(fmtNum(s.total_errors), 'Errors', s.total_errors > 0 ? 'red' : 'dim') +
    sBox(fmtTokens(prompt), 'Prompt Tokens', 'purple') +
    sBox(fmtTokens(fresh), 'Fresh Prompt', 'blue') +
    sBox(fmtTokens(cacheRead), 'Cache Read', 'yellow') +
    sBox(fmtTokens(cacheCreate), 'Cache Write', 'dim') +
    sBox(fmtTokens(output), 'Output Tokens', 'green') +
    sBox(fmtTokens(totalTok), 'Total Tokens', 'white');
}

function renderTokenBreakdown(tokens) {
  var el = document.getElementById('st-breakdown');
  if (!el || !tokens) return;

  var items = [
    { label: 'Direct Prompt', value: tokens.direct_input || 0, color: '#a78bfa' },
    { label: 'Cache Write', value: tokens.cache_create || 0, color: '#60a5fa' },
    { label: 'Cache Read', value: tokens.cache_read || 0, color: '#fbbf24' },
    { label: 'Output', value: tokens.output || 0, color: '#34d399' },
  ];
  var total = (tokens.total_tokens || 0) || items.reduce(function(sum, i) { return sum + i.value; }, 0) || 1;

  el.innerHTML = items.map(function(item) {
    var pct = ((item.value / total) * 100).toFixed(1);
    return '<div class="pbar">' +
      '<div class="pbar-label"><span class="pbar-dot" style="background:' + item.color + '"></span>' + item.label + '</div>' +
      '<div class="pbar-track"><div class="pbar-fill" style="width:' + pct + '%;background:' + item.color + '"></div></div>' +
      '<div class="pbar-val">' + fmtTokens(item.value) + ' (' + pct + '%)</div></div>';
  }).join('');
}

function renderRouteDistribution(routes) {
  var el = document.getElementById('st-routes');
  if (!el) return;

  if (!routes || routes.length === 0) {
    el.innerHTML = '<div class="dim" style="font-size:12px">No data yet</div>';
    return;
  }

  var total = routes.reduce(function(s, r) { return s + r.requests; }, 0) || 1;
  var colors = { local: '#34d399', apikey: '#60a5fa', amp: '#fbbf24', upstream: '#f87171', unknown: '#6b7280' };

  el.innerHTML = routes.map(function(r) {
    var pct = ((r.requests / total) * 100).toFixed(1);
    var label = (r.route || 'unknown').toLowerCase();
    var c = colors[label] || '#6b7280';
    var tok = r.total_tokens || ((r.logical_input_tokens || r.input_tokens || 0) + (r.output_tokens || 0));
    return '<div class="pbar">' +
      '<div class="pbar-label"><span class="pbar-dot" style="background:' + c + '"></span>' + label + '</div>' +
      '<div class="pbar-track"><div class="pbar-fill" style="width:' + pct + '%;background:' + c + '"></div></div>' +
      '<div class="pbar-val">' + r.requests + ' (' + pct + '%) | ' + fmtTokens(tok) + ' tok</div></div>';
  }).join('');
}

function renderDailyChart(daily) {
  var el = document.getElementById('st-daily');
  if (!el) return;

  if (daily.length === 0) {
    el.innerHTML = '<div class="dim" style="font-size:12px">No data yet</div>';
    return;
  }

  var maxReq = Math.max.apply(null, daily.map(function(d) { return d.requests; })) || 1;
  var maxTok = Math.max.apply(null, daily.map(function(d) { return d.total_tokens || (d.logical_input_tokens + d.output_tokens); })) || 1;

  el.innerHTML = '<div class="daily-chart">' + daily.map(function(d) {
    var dayLabel = d.day.slice(5);
    var reqPct = ((d.requests / maxReq) * 100).toFixed(0);
    var totalTok = d.total_tokens || (d.logical_input_tokens + d.output_tokens);
    var tokPct = ((totalTok / maxTok) * 100).toFixed(0);
    return '<div class="daily-row">' +
      '<div class="daily-label">' + dayLabel + '</div>' +
      '<div class="daily-bars">' +
        '<div class="daily-bar-wrap">' +
          '<div class="daily-bar req" style="width:' + reqPct + '%"></div>' +
          '<div class="daily-bar tok" style="width:' + tokPct + '%"></div>' +
        '</div>' +
      '</div>' +
      '<div class="daily-val">' + d.requests + ' req / ' + fmtTokens(totalTok) + ' tok' +
        (d.cached_tokens > 0 ? ' / <span style="color:#fbbf24">' + fmtTokens(d.cached_tokens) + ' cache read</span>' : '') +
        (d.cache_create_tokens > 0 ? ' / <span style="color:#60a5fa">' + fmtTokens(d.cache_create_tokens) + ' cache write</span>' : '') +
        (d.errors > 0 ? ' <span style="color:#f87171">' + d.errors + ' err</span>' : '') + '</div></div>';
  }).join('') + '</div>' +
  '<div class="chart-legend">' +
    '<span><span class="pbar-dot" style="background:#60a5fa"></span> Requests</span>' +
    '<span><span class="pbar-dot" style="background:#a78bfa"></span> Tokens</span></div>';
}

function renderHourlyChart(hourly) {
  var el = document.getElementById('st-hourly');
  if (!el) return;

  if (hourly.length === 0) {
    el.innerHTML = '<div class="dim" style="font-size:12px">No data yet</div>';
    return;
  }

  var byHour = {};
  hourly.forEach(function(h) {
    var hourPart = h.hour.split(' ')[1] || h.hour;
    byHour[hourPart] = h;
  });

  var maxReq = Math.max.apply(null, hourly.map(function(h) { return h.requests; })) || 1;

  var cells = '';
  for (var i = 0; i < 24; i++) {
    var hKey = (i < 10 ? '0' : '') + i + ':00';
    var data = byHour[hKey];
    var intensity = data ? Math.min(((data.requests / maxReq) * 100), 100) : 0;
    var bg = intensity > 0 ? 'rgba(96,165,250,' + (0.15 + intensity * 0.0085) + ')' : '#1e1e2a';
    var totalTok = data ? (data.total_tokens || ((data.logical_input_tokens || 0) + (data.output_tokens || 0))) : 0;
    var title = hKey + ': ' + (data ? data.requests + ' req, ' + fmtTokens(totalTok) + ' tok' + (data.cached_tokens > 0 ? ', ' + fmtTokens(data.cached_tokens) + ' cache read' : '') + (data.cache_create_tokens > 0 ? ', ' + fmtTokens(data.cache_create_tokens) + ' cache write' : '') : 'no data');
    cells += '<div class="hour-cell" style="background:' + bg + '" title="' + title + '">' +
      '<div class="hour-label">' + (i < 10 ? '0' : '') + i + '</div>' +
      (data ? '<div class="hour-val">' + data.requests + '</div>' : '') + '</div>';
  }

  el.innerHTML = '<div class="hour-grid">' + cells + '</div>';
}

function renderProviderDist(s) {
  var models = Object.values(s.by_model || {});
  var summary = {};
  models.forEach(function(m) {
    var p = m.provider || 'unknown';
    if (!summary[p]) summary[p] = { requests: 0, total: 0, errors: 0 };
    summary[p].requests += m.total_requests;
    summary[p].total += m.total_tokens || ((m.total_logical_input_tokens || 0) + (m.total_output_tokens || 0));
    summary[p].errors += m.total_errors;
  });

  var pEl = document.getElementById('st-prov');
  if (!pEl) return;

  if (Object.keys(summary).length === 0) {
    pEl.innerHTML = '<div class="dim" style="font-size:12px">No data yet</div>';
    return;
  }

  var total = Object.values(summary).reduce(function(sv, v) { return sv + v.requests; }, 0) || 1;
  var colors = { claude: '#a78bfa', anthropic: '#a78bfa', openai: '#34d399', google: '#60a5fa', gemini: '#60a5fa', upstream: '#fbbf24' };

  pEl.innerHTML = Object.keys(summary).map(function(p) {
    var v = summary[p];
    var pct = ((v.requests / total) * 100).toFixed(1);
    var c = colors[p] || '#6b7280';
    return '<div class="pbar"><div class="pbar-label"><span class="pbar-dot" style="background:' + c + '"></span>' + p + '</div>' +
      '<div class="pbar-track"><div class="pbar-fill" style="width:' + pct + '%;background:' + c + '"></div></div>' +
      '<div class="pbar-val">' + v.requests + ' (' + pct + '%) | ' + fmtTokens(v.total) + ' tok</div></div>';
  }).join('');
}

function renderModelTable(s) {
  var models = Object.values(s.by_model || {});
  var tbody = document.getElementById('st-models');
  if (!tbody) return;

  if (models.length === 0) {
    tbody.innerHTML = '<tr><td colspan="11" class="dim" style="text-align:center">No data yet</td></tr>';
    return;
  }

  models.sort(function(a, b) { return b.total_requests - a.total_requests; });
  tbody.innerHTML = models.map(function(m) {
    var info = _statsRoles[m.model] || {};
    var role = info.role || '';
    var prov = m.provider || info.provider || '';
    var totalTok = m.total_tokens || ((m.total_logical_input_tokens || 0) + (m.total_output_tokens || 0));
    var hitBase = m.total_logical_input_tokens || 0;
    var hitRate = hitBase > 0 ? ((m.total_cached_tokens || 0) / hitBase) * 100 : 0;
    return '<tr><td><code>' + m.model + '</code></td>' +
      '<td><span class="role-tag">' + role + '</span></td>' +
      '<td class="dim">' + prov + '</td>' +
      '<td>' + fmtNum(m.total_requests) + '</td>' +
      '<td>' + (m.total_errors > 0 ? '<span style="color:#f87171">' + m.total_errors + '</span>' : '0') + '</td>' +
      '<td>' + fmtTokens(m.total_logical_input_tokens) + '</td>' +
      '<td>' + fmtTokens(m.total_fresh_input_tokens) + '</td>' +
      '<td>' + fmtTokens(m.total_cached_tokens) + '</td>' +
      '<td>' + fmtTokens(m.total_output_tokens) + '</td>' +
      '<td>' + (function() {
        var hr = hitBase > 0 ? hitRate.toFixed(1) : 0;
        var hrStr = hitBase > 0 ? hr + '%' : '--';
        var hrColor = hr > 50 ? '#34d399' : hr > 20 ? '#fbbf24' : '#6b7280';
        return '<span style="color:' + hrColor + '">' + hrStr + '</span>';
      })() + '</td>' +
      '<td><strong>' + fmtTokens(totalTok) + '</strong></td></tr>';
  }).join('');
}

function sBox(val, label, color) {
  var cls = 'stat-box';
  if (color === 'red') cls += ' stat-red';
  else if (color === 'green') cls += ' stat-green';
  else if (color === 'blue') cls += ' stat-blue';
  else if (color === 'purple') cls += ' stat-purple';
  else if (color === 'yellow') cls += ' stat-yellow';
  return '<div class="' + cls + '"><div class="stat-val">' + val + '</div><div class="stat-lbl">' + label + '</div></div>';
}
