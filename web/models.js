// models.js - Model Routing
var _modRoles = {};
var _modTiers = {};
var _modAuth = {};

function renderModels(el) {
  el.innerHTML =
    '<div class="page-header"><h2>Model Routing</h2></div>' +
    '<div class="routing-legend">' +
      '<span><span class="badge green">Local</span> Auto-detect CLI credentials</span>' +
      '<span><span class="badge" style="background:#1e293b;color:#60a5fa">Key</span> Manual API key</span>' +
      '<span><span class="badge" style="background:#422006;color:#fbbf24">AMP</span> Forward to ampcode.com</span>' +
    '</div>' +
    '<div id="mod-sections"></div>';
  refreshModels();
}

function refreshModels() {
  Promise.all([
    API.get('/api/model-roles').then(function(roles) {
      roles.forEach(function(r) { _modRoles[r.model] = r; });
    }).catch(function(){}),
    API.get('/api/model-tiers').then(function(t) {
      _modTiers = t;
    }).catch(function(){}),
    API.get('/api/auth/status').then(function(s) {
      _modAuth = s;
    }).catch(function(){})
  ]).then(function() {
    API.get('/api/config').then(function(cfg) {
      var container = document.getElementById('mod-sections');
      if (!container) return;
      container.innerHTML =
        modelSection('Claude (Anthropic)', 'claude', cfg.claude || {}) +
        modelSection('OpenAI', 'openai', cfg.openai || {}) +
        modelSection('Gemini (Google)', 'gemini', cfg.gemini || {});
    });
  });
}

function modelSection(name, provider, provCfg) {
  var models = provCfg.models || [];
  var auth = _modAuth[provider] || {};
  var localOK = auth.local_available === true;
  var apikeyOK = auth.apikey_available === true;

  // Auth indicators
  var authHTML = '<div class="mod-auth-info">';
  if (localOK) authHTML += '<span class="badge green">Local ✓</span> ';
  else authHTML += '<span class="badge">Local ✗</span> ';
  if (apikeyOK) authHTML += '<span class="badge green">Key ✓</span> ';
  else authHTML += '<span class="badge">Key ✗</span> ';
  authHTML += '<span class="badge" style="background:#422006;color:#fbbf24">AMP ✓</span>';
  authHTML += '</div>';

  // Count routes
  var countLocal = 0, countKey = 0, countAmp = 0;
  models.forEach(function(m) {
    var r = m.route || 'amp';
    if (r === 'local') countLocal++;
    else if (r === 'apikey') countKey++;
    else countAmp++;
  });

  var headerHTML = '<div class="mod-section-header">' +
    '<div><strong>' + name + '</strong> <span class="dim">(' + models.length + ' models: ' + countLocal + 'L/' + countKey + 'K/' + countAmp + 'A)</span></div>' +
    authHTML + '</div>';

  // Bulk controls
  var bulkHTML = '<div class="bulk-row" style="margin-bottom:8px">' +
    '<button class="btn-sm" onclick="bulkRoute(\'' + provider + '\',\'local\')">All Local</button>' +
    '<button class="btn-sm" onclick="bulkRoute(\'' + provider + '\',\'apikey\')">All Key</button>' +
    '<button class="btn-sm" onclick="bulkRoute(\'' + provider + '\',\'amp\')">All AMP</button></div>';

  // Model rows
  var modelsHTML = '<div class="model-list">' + models.map(function(m) {
    var info = _modRoles[m.name];
    var roleTag = info ? '<span class="role-tag">' + info.role + '</span>' : '';
    var descTag = info ? '<span class="dim" style="font-size:10px;margin-left:4px">' + info.description + '</span>' : '';
    var route = m.route || 'amp';
    var isDefault = !!_modRoles[m.name];
    var deleteBtn = !isDefault ? '<button class="btn-icon delete" onclick="deleteModel(\'' + provider + '\',\'' + m.name + '\')" title="Remove">✕</button>' : '';

    return '<div class="model-row">' +
      '<div class="model-info"><span class="model-name">' + m.name + '</span>' + roleTag + descTag + '</div>' +
      '<div class="model-actions">' +
        '<div class="route-group">' +
          modRouteBtn(provider, m.name, 'amp', route) +
          modRouteBtn(provider, m.name, 'local', route) +
          modRouteBtn(provider, m.name, 'apikey', route) +
        '</div>' + deleteBtn +
      '</div></div>';
  }).join('') + '</div>';

  // Add model
  var addHTML = '<div class="prov-add-model">' +
    '<input type="text" id="add-mod-' + provider + '" placeholder="Add model name..." />' +
    '<select id="add-rt-' + provider + '"><option value="amp">AMP</option><option value="local">Local</option><option value="apikey">Key</option></select>' +
    '<button class="btn-sm" onclick="addModel(\'' + provider + '\')">+ Add</button></div>';

  return '<div class="card">' + headerHTML + bulkHTML + modelsHTML + addHTML + '</div>';
}

function modRouteBtn(provider, model, route, current) {
  var labels = { amp: 'AMP', local: 'Local', apikey: 'Key' };
  var tiers = _modTiers[model] || ['amp'];
  var supported = tiers.indexOf(route) !== -1;
  var cls = 'route-btn' + (current === route ? ' active' : '') + (!supported ? ' disabled' : '');
  if (!supported) {
    return '<button class="' + cls + '" disabled title="Not available">' + labels[route] + '</button>';
  }
  return '<button class="' + cls + '" onclick="setModelRoute(\'' + provider + '\',\'' + model + '\',\'' + route + '\',this)">' +
    labels[route] + '</button>';
}

function setModelRoute(provider, model, route, btn) {
  API.post('/api/auth/route', { provider: provider, model: model, route: route }).then(function() {
    var group = btn.parentNode;
    group.querySelectorAll('.route-btn').forEach(function(b) { b.classList.remove('active'); });
    btn.classList.add('active');
  });
}

function bulkRoute(provider, route) {
  API.get('/api/config').then(function(cfg) {
    var models = (cfg[provider] || {}).models || [];
    var promises = models.map(function(m) {
      return API.post('/api/auth/route', { provider: provider, model: m.name, route: route });
    });
    Promise.all(promises).then(refreshModels);
  });
}

function addModel(provider) {
  var nameEl = document.getElementById('add-mod-' + provider);
  var routeEl = document.getElementById('add-rt-' + provider);
  if (!nameEl || !nameEl.value.trim()) return;
  API.post('/api/provider/add-model', {
    provider: provider,
    model: nameEl.value.trim(),
    route: routeEl ? routeEl.value : 'amp'
  }).then(function() {
    nameEl.value = '';
    refreshModels();
  });
}

function deleteModel(provider, model) {
  if (!confirm('Remove model ' + model + '?')) return;
  API.post('/api/provider/delete-model', { provider: provider, model: model }).then(refreshModels);
}
