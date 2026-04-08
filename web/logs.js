// logs.js
var _logRoles = {};
var _logPage = 0;
var _logLimit = 50;
var _logFilters = { provider: '', route: '', status: '' };

function renderLogs(el) {
  el.innerHTML =
    '<div class="card">' +
      '<div class="toolbar">' +
        '<h3>Request Logs</h3>' +
        '<select class="filter-select" id="lf-provider" onchange="applyLogFilter()"><option value="">All Providers</option><option value="anthropic">Claude</option><option value="openai">OpenAI</option><option value="google">Gemini</option><option value="upstream">Upstream</option></select>' +
        '<select class="filter-select" id="lf-route" onchange="applyLogFilter()"><option value="">All Routes</option><option value="LOCAL">LOCAL</option><option value="UPSTREAM">UPSTREAM</option></select>' +
        '<select class="filter-select" id="lf-status" onchange="applyLogFilter()"><option value="">All Status</option><option value="400">Errors (4xx+)</option></select>' +
        '<button class="btn" onclick="refreshLogs()">Refresh</button>' +
      '</div>' +
      '<div class="table-wrap"><table>' +
        '<thead><tr><th>Time</th><th>Model</th><th>Role</th><th>Provider</th><th>Route</th><th>Status</th><th>Latency</th><th>Input</th><th>Output</th><th>Cache</th></tr></thead>' +
        '<tbody id="logs-tbody"></tbody>' +
      '</table></div>' +
      '<div class="toolbar" style="margin-top:8px;justify-content:flex-end">' +
        '<button class="btn-sm" onclick="logsPrev()">Prev</button>' +
        '<span class="page-info" id="logs-page-info"></span>' +
        '<button class="btn-sm" onclick="logsNext()">Next</button>' +
      '</div>' +
    '</div>' +
    '<div class="card"><h3>Error Details</h3><div id="logs-errors"></div></div>';

  API.get('/api/model-roles').then(function(roles) {
    roles.forEach(function(r) { _logRoles[r.model] = r.role; });
  }).catch(function(){});

  _logPage = 0;
  refreshLogs();
}

function applyLogFilter() {
  _logFilters.provider = document.getElementById('lf-provider').value;
  _logFilters.route = document.getElementById('lf-route').value;
  _logFilters.status = document.getElementById('lf-status').value;
  _logPage = 0;
  refreshLogs();
}

function refreshLogs() {
  var offset = _logPage * _logLimit;
  var qs = '?limit=' + _logLimit + '&offset=' + offset;
  if (_logFilters.provider) qs += '&provider=' + _logFilters.provider;
  if (_logFilters.route) qs += '&route=' + _logFilters.route;
  if (_logFilters.status) qs += '&status=' + _logFilters.status;

  API.get('/api/logs' + qs).then(function(logs) {
    logs = logs || [];
    var tbody = document.getElementById('logs-tbody');
    if (!tbody) return;
    tbody.innerHTML = logs.map(function(l) {
      var t = new Date(l.timestamp).toLocaleString();
      var rc = l.route === 'LOCAL' ? 'route-local' : 'route-upstream';
      var lat = l.latency ? (l.latency / 1e6).toFixed(0) + 'ms' : '--';
      var role = _logRoles[l.model] || '';
      var inp = l.tokens ? fmtTokens(l.tokens.input_tokens) : '--';
      var outp = l.tokens ? fmtTokens(l.tokens.output_tokens) : '--';
      var cacheVal = l.tokens ? fmtTokens(l.tokens.cache_read_tokens) : '--';
      var cache = (l.tokens && l.tokens.cache_read_tokens > 0) ? '<span style="color:#fbbf24">' + cacheVal + '</span>' : cacheVal;
      var st = l.status || '';
      if (l.status >= 400) st = '<span style="color:#f87171">' + l.status + '</span>';
      else if (l.status >= 200) st = '<span style="color:#34d399">' + l.status + '</span>';
      return '<tr><td class="dim">' + t + '</td><td><code>' + (l.model||'--') + '</code></td>' +
        '<td class="dim">' + role + '</td><td class="dim">' + (l.provider||'') + '</td>' +
        '<td><span class="' + rc + '">' + (l.route||'') + '</span></td>' +
        '<td>' + st + '</td><td>' + lat + '</td><td>' + inp + '</td><td>' + outp + '</td><td>' + cache + '</td></tr>';
    }).join('');
    var info = document.getElementById('logs-page-info');
    if (info) info.textContent = 'Page ' + (_logPage + 1) + (logs.length < _logLimit ? ' (last)' : '');
  }).catch(function(e) { console.error('logs error:', e); });

  // Errors
  API.get('/api/logs/errors?limit=10').then(function(errs) {
    errs = errs || [];
    var el = document.getElementById('logs-errors');
    if (!el) return;
    if (errs.length === 0) { el.innerHTML = '<div class="dim" style="font-size:12px">No errors recorded.</div>'; return; }
    el.innerHTML = errs.map(function(e) {
      var t = new Date(e.timestamp).toLocaleString();
      return '<div class="error-item"><div class="error-header">' + t + ' - ' + (e.model||'') + ' - Status ' + e.status +
        (e.retries ? ' (' + e.retries + ' retries)' : '') + '</div>' +
        '<div class="dim" style="font-size:11px;margin-bottom:3px">' + esc(e.error) + '</div>' +
        (e.response_body ? '<details><summary>Response body</summary><pre>' + esc(e.response_body) + '</pre></details>' : '') +
        '</div>';
    }).join('');
  }).catch(function(){});
}

function logsPrev() { if (_logPage > 0) { _logPage--; refreshLogs(); } }
function logsNext() { _logPage++; refreshLogs(); }
