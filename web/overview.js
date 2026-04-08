// overview.js
var _rolesMap = {};

function renderOverview(el) {
  el.innerHTML = '<div id="ov-providers" class="provider-grid"></div>' +
    '<div id="ov-stats" class="stats-row"></div>' +
    '<div class="card"><h3>Recent Requests</h3><div class="table-wrap"><table>' +
    '<thead><tr><th>Time</th><th>Model</th><th>Route</th><th>Latency</th><th>Input</th><th>Output</th><th>Cache</th></tr></thead>' +
    '<tbody id="ov-recent"></tbody></table></div>' +
    '<div style="margin-top:8px"><a class="link" onclick="navigateTo(\'logs\')">View all logs &rarr;</a></div></div>' +
    '<div class="upstream-row" id="ov-upstream"></div>';
  refreshOverview();
}

function refreshOverview() {
  API.get('/api/model-roles').then(function(roles) {
    roles.forEach(function(r) { _rolesMap[r.model] = r.role; });
  }).catch(function(){});

  API.get('/api/overview').then(function(d) {
    // Provider cards
    var pEl = document.getElementById('ov-providers');
    if (pEl) {
      var ps = d.providers || {};
      pEl.innerHTML = ['claude','openai','gemini'].map(function(p) {
        var info = ps[p] || {};
        var auth = info.auth || {};
        var localOK = auth.local_available === true;
        var apikeyOK = auth.apikey_available === true;
        var dot = (localOK || apikeyOK) ? '<span class="dot green"></span>' : '<span class="dot red"></span>';
        var src = auth.local_source || 'file';
        var badge = localOK ? '<span class="badge green">' + src + '</span>' : '<span class="badge">' + src + '</span>';
        if (apikeyOK) badge += ' <span class="badge green">key</span>';
        var localN = info.local || 0;
        var apikeyN = info.apikey || 0;
        var ampN = info.amp || 0;
        return '<div class="prov-card" onclick="navigateTo(\'providers\')">' +
          '<div class="prov-card-header">' + dot + '<span class="prov-card-name">' + p.charAt(0).toUpperCase() + p.slice(1) + '</span>' + badge + '</div>' +
          '<div class="prov-card-count">' + localN + 'L ' + apikeyN + 'K ' + ampN + 'A <span style="font-size:14px;color:#6b7280;font-weight:400">/ ' + (info.total||0) + '</span></div>' +
          '<div class="prov-card-label">local / key / amp</div></div>';
      }).join('');
    }

    // Stats
    var s = d.stats || {};
    var sEl = document.getElementById('ov-stats');
    if (sEl) {
      var cached = 0;
      Object.values(s.by_model || {}).forEach(function(m) { cached += m.total_cached_tokens || 0; });
      var totalTok = (s.total_input_tokens || 0) + (s.total_output_tokens || 0);
      sEl.innerHTML =
        statBox(s.total_requests, 'Requests') +
        statBox(s.total_errors, 'Errors') +
        statBox(fmtTokens(s.total_input_tokens), 'Input Tokens') +
        statBox(fmtTokens(s.total_output_tokens), 'Output Tokens') +
        statBox(fmtTokens(cached), 'Cache Hit') +
        statBox(fmtTokens(totalTok), 'Total Tokens');
    }

    // Recent logs
    var tbody = document.getElementById('ov-recent');
    if (tbody) {
      var logs = d.recent || [];
      tbody.innerHTML = logs.map(function(l) {
        var t = new Date(l.timestamp).toLocaleTimeString();
        var rc = l.route === 'LOCAL' ? 'route-local' : 'route-upstream';
        var lat = l.latency ? (l.latency / 1e6).toFixed(0) + 'ms' : '--';
        var inp = l.tokens ? fmtTokens(l.tokens.input_tokens) : '--';
        var outp = l.tokens ? fmtTokens(l.tokens.output_tokens) : '--';
        var cacheVal = l.tokens ? fmtTokens(l.tokens.cache_read_tokens) : '--';
        var cache = (l.tokens && l.tokens.cache_read_tokens > 0) ? '<span style="color:#fbbf24">' + cacheVal + '</span>' : cacheVal;
        return '<tr><td>' + t + '</td><td><code>' + (l.model||'--') + '</code></td>' +
          '<td><span class="' + rc + '">' + (l.route||'') + '</span></td><td>' + lat + '</td><td>' + inp + '</td><td>' + outp + '</td><td>' + cache + '</td></tr>';
      }).join('');
    }

    // Upstream
    var uEl = document.getElementById('ov-upstream');
    if (uEl) uEl.innerHTML = '<span class="dot green"></span> Upstream: ' + (d.upstream || 'not configured');
  }).catch(function(e) { console.error('overview error:', e); });
}

function statBox(val, label) {
  return '<div class="stat-box"><div class="stat-val">' + (val == null ? '0' : val) + '</div><div class="stat-lbl">' + label + '</div></div>';
}
