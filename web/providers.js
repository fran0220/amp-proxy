// providers.js - Provider Management
var _authStatus = {};
var _provExpanded = { amp: true, claude: true, openai: true, gemini: true, custom: true };
var _testResults = {};
var _editingKeys = {};
var _editingCustomProviders = {};

function renderProviders(el) {
  el.innerHTML =
    '<div class="page-header"><h2>Provider Management</h2>' +
    '<button class="btn" onclick="refreshProviderToken()">↻ Refresh Local Tokens</button></div>' +
    '<div id="prov-sections"></div>';
  refreshProviders();
}

function refreshProviders() {
  Promise.all([
    API.get('/api/auth/status').then(function(s) { _authStatus = s; }).catch(function(){}),
    API.get('/api/config'),
    API.get('/api/amp-config').catch(function() { return {}; })
  ]).then(function(results) {
    var cfg = results[1];
    var ampCfg = results[2] || {};
    var container = document.getElementById('prov-sections');
    if (!container) return;
    container.innerHTML =
      ampUpstreamSection(ampCfg) +
      providerSection('Claude (Anthropic)', 'claude', cfg.claude || {}) +
      providerSection('OpenAI', 'openai', cfg.openai || {}) +
      providerSection('Gemini (Google)', 'gemini', cfg.gemini || {}) +
      customProvidersSection(cfg.custom || []);
  }).catch(function(e) { console.error('providers error:', e); });
}

function ampUpstreamSection(ampCfg) {
  var hasKey = ampCfg.has_key === true;
  var dot = hasKey ? '<span class="dot green"></span>' : '<span class="dot red"></span>';
  var expanded = _provExpanded.amp !== false;

  var header = '<div class="prov-section-header" onclick="toggleProv(\'amp\')">' +
    '<div class="prov-section-title">' + dot + '<span>AMP Upstream</span>' +
    (hasKey ? '<span class="badge green">Connected</span>' : '<span class="badge warn">No API Key</span>') + '</div>' +
    '<span class="prov-chevron ' + (expanded ? 'open' : '') + '">▸</span></div>';

  if (!expanded) return '<div class="prov-section">' + header + '</div>';

  var body = '<div class="prov-section-body">' +
    '<div class="prov-subsection"><div class="prov-sub-title">Upstream URL</div>' +
    '<div class="prov-auth-row"><code>' + esc(ampCfg.upstream_url || 'not set') + '</code></div></div>' +
    '<div class="prov-subsection"><div class="prov-sub-title">API Key</div>' +
    (hasKey ? '<div class="prov-auth-row"><code class="key-value">' + esc(ampCfg.api_key || '') + '</code> <span class="badge green">Set</span></div>'
            : '<div class="prov-auth-row"><span class="badge warn">Not configured</span></div>') + '</div>' +
    '<div class="prov-subsection"><div class="prov-sub-title">Update Configuration</div>' +
    '<div class="prov-add-key">' +
    '<input type="text" id="amp-url" placeholder="Upstream URL (e.g. https://ampcode.com)" value="' + esc(ampCfg.upstream_url || '') + '" />' +
    '<input type="password" id="amp-key" placeholder="AMP API Key" />' +
    '<button class="btn" onclick="saveAmpConfig()">Save</button></div></div>' +
    '<div class="dim" style="font-size:11px;margin-top:4px">Models routed to AMP will be forwarded to this upstream with the configured API key.</div>' +
    '</div>';

  return '<div class="prov-section">' + header + body + '</div>';
}

function saveAmpConfig() {
  var url = document.getElementById('amp-url');
  var key = document.getElementById('amp-key');
  var body = {};
  if (url && url.value.trim()) body.upstream_url = url.value.trim();
  if (key && key.value.trim()) body.api_key = key.value.trim();
  if (!body.upstream_url && !body.api_key) return;
  API.post('/api/amp-config', body).then(function() {
    if (key) key.value = '';
    refreshProviders();
  });
}

function providerSection(name, provider, provCfg) {
  var auth = _authStatus[provider] || {};
  var entries = provCfg.entries || [];
  var expanded = _provExpanded[provider] !== false;

  var localOK = auth.local_available === true;
  var apikeyOK = entries.length > 0 || auth.apikey_available === true;
  var dot = (localOK || apikeyOK) ? '<span class="dot green"></span>' : '<span class="dot red"></span>';

  var header = '<div class="prov-section-header" onclick="toggleProv(\'' + provider + '\')">' +
    '<div class="prov-section-title">' + dot + '<span>' + name + '</span>' +
    '<span class="prov-model-counts">' + entries.length + ' keys</span></div>' +
    '<span class="prov-chevron ' + (expanded ? 'open' : '') + '">▸</span></div>';

  if (!expanded) return '<div class="prov-section">' + header + '</div>';

  // Local auth
  var localHTML = '<div class="prov-subsection"><div class="prov-sub-title">Local Credentials</div>';
  if (localOK) {
    var src = auth.local_source || 'unknown';
    localHTML += '<div class="prov-auth-row"><span class="badge green">' + src + '</span>';
    if (auth.local_expires_in) localHTML += ' <span class="dim">expires in ' + auth.local_expires_in + '</span>';
    if (auth.local_email) localHTML += ' <span class="dim">(' + auth.local_email + ')</span>';
    localHTML += '</div>';
  } else {
    var errMsg = auth.local_error || 'not configured';
    localHTML += '<div class="prov-auth-row"><span class="badge warn">' + (auth.local_source || 'none') + ' ⚠</span> <span class="dim" style="color:#f87171">' + esc(errMsg) + '</span></div>';
  }
  localHTML += '</div>';

  // API Keys list
  var keysHTML = '<div class="prov-subsection"><div class="prov-sub-title">API Keys</div>';
  if (entries.length === 0) {
    keysHTML += '<div class="dim" style="font-size:12px;padding:6px 0">No API keys configured</div>';
  } else {
    keysHTML += entries.map(function(e) {
      var testId = provider + '-' + e.id;
      var draft = _editingKeys[testId];
      var testHTML = renderTestBadge(testId);
      if (draft) {
        return '<div class="key-row editing">' +
          '<div class="key-info key-edit-fields">' +
            '<input type="text" value="' + esc(draft.label || '') + '" placeholder="Label (optional)" oninput="updateKeyDraft(\'' + provider + '\',\'' + e.id + '\',\'label\',this.value)" />' +
            '<input type="password" value="" placeholder="New API key (leave blank to keep current)" oninput="updateKeyDraft(\'' + provider + '\',\'' + e.id + '\',\'api_key\',this.value)" />' +
            '<input type="text" value="' + esc(draft.base_url || '') + '" placeholder="Base URL (optional)" oninput="updateKeyDraft(\'' + provider + '\',\'' + e.id + '\',\'base_url\',this.value)" />' +
            '<span class="key-edit-note">Current key: <code class="key-value">' + (e.api_key || '***') + '</code></span>' +
          '</div>' +
          '<div class="key-actions">' +
            testHTML +
            '<button class="btn-sm" onclick="testEditedKey(\'' + provider + '\',\'' + e.id + '\')">Test</button>' +
            '<button class="btn-sm" onclick="saveKeyEdit(\'' + provider + '\',\'' + e.id + '\')">Save</button>' +
            '<button class="btn-sm" onclick="cancelKeyEdit(\'' + provider + '\',\'' + e.id + '\')">Cancel</button>' +
          '</div></div>';
      }
      return '<div class="key-row">' +
        '<div class="key-info">' +
          '<code class="key-value">' + (e.api_key || '***') + '</code>' +
          (e.label ? '<span class="badge">' + esc(e.label) + '</span>' : '') +
          (e.base_url ? '<span class="dim">' + esc(e.base_url) + '</span>' : '') +
        '</div>' +
        '<div class="key-actions">' +
          testHTML +
          '<button class="btn-sm" onclick="testKey(\'' + provider + '\',\'' + e.id + '\')">Test</button>' +
          '<button class="btn-sm" onclick="beginEditKey(\'' + provider + '\',\'' + e.id + '\',this)" data-label="' + esc(e.label || '') + '" data-base-url="' + esc(e.base_url || '') + '">Edit</button>' +
          '<button class="btn-icon delete" onclick="removeKey(\'' + provider + '\',\'' + e.id + '\')">✕</button>' +
        '</div></div>';
    }).join('');
  }
  keysHTML += '</div>';

  // Add key form
  var addHTML = '<div class="prov-subsection">' +
    '<div class="prov-add-key">' +
    '<input type="text" id="ak-label-' + provider + '" placeholder="Label (optional)" style="width:120px" />' +
    '<input type="password" id="ak-key-' + provider + '" placeholder="API Key" />' +
    '<input type="text" id="ak-url-' + provider + '" placeholder="Base URL (optional)" />' +
    '<button class="btn" onclick="addKey(\'' + provider + '\')">+ Add Key</button>' +
    '</div></div>';

  // Discover models button
  var discoverHTML = '<div class="prov-subsection">' +
    '<button class="btn-sm" onclick="discoverModels(\'' + provider + '\')">🔍 Discover Models</button>' +
    '<div id="discover-' + provider + '" style="margin-top:8px"></div></div>';

  return '<div class="prov-section">' + header +
    '<div class="prov-section-body">' + localHTML + keysHTML + addHTML + discoverHTML + '</div></div>';
}

function customProvidersSection(customs) {
  var expanded = _provExpanded.custom !== false;
  var header = '<div class="prov-section-header" onclick="toggleProv(\'custom\')">' +
    '<div class="prov-section-title"><span class="dot blue"></span><span>Custom Providers (OpenAI Compatible)</span>' +
    '<span class="prov-model-counts">' + customs.length + ' providers</span></div>' +
    '<span class="prov-chevron ' + (expanded ? 'open' : '') + '">▸</span></div>';

  if (!expanded) return '<div class="prov-section">' + header + '</div>';

  var listHTML = '';
  if (customs.length === 0) {
    listHTML = '<div class="dim" style="font-size:12px;padding:6px 0">No custom providers configured</div>';
  } else {
    listHTML = customs.map(function(cp) {
      var testId = 'custom-' + cp.id;
      var draft = _editingCustomProviders[cp.id];
      var primaryKey = (cp.entries || [])[0] || {};
      var keysInfo = (cp.entries || []).map(function(e) {
        return '<code class="key-value">' + (e.api_key || '***') + '</code>';
      }).join(' ');
      var testHTML = renderTestBadge(testId);
      if (draft) {
        return '<div class="key-row editing">' +
          '<div class="key-info key-edit-fields">' +
            '<input type="text" value="' + esc(draft.name || '') + '" placeholder="Provider name" oninput="updateCustomProviderDraft(\'' + cp.id + '\',\'name\',this.value)" />' +
            '<input type="text" value="' + esc(draft.base_url || '') + '" placeholder="Base URL" oninput="updateCustomProviderDraft(\'' + cp.id + '\',\'base_url\',this.value)" />' +
            '<input type="password" value="" placeholder="New API key (leave blank to keep current)" oninput="updateCustomProviderDraft(\'' + cp.id + '\',\'api_key\',this.value)" />' +
            '<span class="key-edit-note">Current key: <code class="key-value">' + (primaryKey.api_key || '***') + '</code></span>' +
          '</div>' +
          '<div class="key-actions">' +
            testHTML +
            '<button class="btn-sm" onclick="testEditedCustomProvider(\'' + cp.id + '\')">Test</button>' +
            '<button class="btn-sm" onclick="saveCustomProviderEdit(\'' + cp.id + '\')">Save</button>' +
            '<button class="btn-sm" onclick="cancelCustomProviderEdit(\'' + cp.id + '\')">Cancel</button>' +
          '</div></div>';
      }
      return '<div class="key-row">' +
        '<div class="key-info">' +
          '<strong>' + esc(cp.name) + '</strong>' +
          '<span class="dim">' + esc(cp.base_url) + '</span>' +
          (keysInfo ? ' ' + keysInfo : '') +
        '</div>' +
        '<div class="key-actions">' +
          testHTML +
          '<button class="btn-sm" onclick="testCustomProvider(\'' + cp.id + '\')">Test</button>' +
          '<button class="btn-sm" onclick="beginEditCustomProvider(\'' + cp.id + '\',this)" data-name="' + esc(cp.name || '') + '" data-base-url="' + esc(cp.base_url || '') + '">Edit</button>' +
          '<button class="btn-sm" onclick="discoverCustomModels(\'' + cp.id + '\')">🔍 Models</button>' +
          '<button class="btn-icon delete" onclick="removeCustomProvider(\'' + cp.id + '\')">✕</button>' +
        '</div></div>';
    }).join('');
  }

  var addHTML = '<div class="prov-add-key" style="margin-top:8px">' +
    '<input type="text" id="cp-name" placeholder="Provider name" style="width:140px" />' +
    '<input type="text" id="cp-url" placeholder="Base URL (e.g. https://api.x.ai/v1)" />' +
    '<input type="password" id="cp-key" placeholder="API Key" />' +
    '<button class="btn" onclick="addCustomProvider()">+ Add Provider</button></div>';

  return '<div class="prov-section">' + header +
    '<div class="prov-section-body">' + listHTML + addHTML +
    '<div id="discover-custom" style="margin-top:8px"></div></div></div>';
}

function toggleProv(provider) {
  _provExpanded[provider] = !_provExpanded[provider];
  refreshProviders();
}

function addKey(provider) {
  var label = document.getElementById('ak-label-' + provider);
  var key = document.getElementById('ak-key-' + provider);
  var url = document.getElementById('ak-url-' + provider);
  if (!key || !key.value.trim()) { alert('API Key is required'); return; }
  API.post('/api/keys/add', {
    provider: provider,
    label: label ? label.value.trim() : '',
    api_key: key.value.trim(),
    base_url: url ? url.value.trim() : ''
  }).then(function() {
    key.value = '';
    if (label) label.value = '';
    if (url) url.value = '';
    refreshProviders();
  });
}

function removeKey(provider, id) {
  if (!confirm('Remove this API key?')) return;
  API.post('/api/keys/remove', { provider: provider, id: id }).then(refreshProviders);
}

function renderTestBadge(testId) {
  var testResult = _testResults[testId];
  if (!testResult) return '';
  if (testResult === 'testing') return '<span class="dim">testing...</span>';
  if (testResult.success) return '<span class="badge green">' + esc(testResult.message) + ' (' + testResult.latency_ms + 'ms)</span>';
  return '<span class="badge warn">' + esc(testResult.message) + '</span>';
}

function runKeyTest(testId, payload) {
  _testResults[testId] = 'testing';
  refreshProviders();
  API.post('/api/keys/test', payload).then(function(r) {
    _testResults[testId] = r;
    refreshProviders();
  }).catch(function(e) {
    _testResults[testId] = { success: false, message: e.message };
    refreshProviders();
  });
}

function testKey(provider, id) {
  runKeyTest(provider + '-' + id, { provider: provider, id: id });
}

function beginEditKey(provider, id, btn) {
  _editingKeys[provider + '-' + id] = {
    label: btn.getAttribute('data-label') || '',
    base_url: btn.getAttribute('data-base-url') || '',
    api_key: ''
  };
  refreshProviders();
}

function updateKeyDraft(provider, id, field, value) {
  var draft = _editingKeys[provider + '-' + id];
  if (!draft) return;
  draft[field] = value;
}

function cancelKeyEdit(provider, id) {
  delete _editingKeys[provider + '-' + id];
  refreshProviders();
}

function saveKeyEdit(provider, id) {
  var draft = _editingKeys[provider + '-' + id];
  if (!draft) return;
  var body = {
    provider: provider,
    id: id,
    label: (draft.label || '').trim(),
    base_url: (draft.base_url || '').trim()
  };
  if ((draft.api_key || '').trim()) body.api_key = draft.api_key.trim();
  API.post('/api/keys/update', body).then(function() {
    delete _editingKeys[provider + '-' + id];
    refreshProviders();
  });
}

function testEditedKey(provider, id) {
  var draft = _editingKeys[provider + '-' + id] || {};
  var body = {
    provider: provider,
    id: id,
    base_url: (draft.base_url || '').trim()
  };
  if ((draft.api_key || '').trim()) body.api_key = draft.api_key.trim();
  runKeyTest(provider + '-' + id, body);
}

function discoverModels(provider) {
  var el = document.getElementById('discover-' + provider);
  if (!el) return;
  el.innerHTML = '<span class="dim">Discovering models...</span>';
  API.post('/api/keys/discover', { provider: provider }).then(function(models) {
    if (!models || models.length === 0) {
      el.innerHTML = '<span class="dim">No models found (need API key)</span>';
      return;
    }
    el.innerHTML = '<div class="discover-list">' + models.map(function(m) {
      return '<div class="discover-item"><code>' + m.id + '</code>' +
        (m.name && m.name !== m.id ? ' <span class="dim">' + esc(m.name) + '</span>' : '') + '</div>';
    }).join('') + '</div>';
  }).catch(function(e) {
    el.innerHTML = '<span class="badge warn">' + esc(e.message) + '</span>';
  });
}

function discoverCustomModels(cpId) {
  var el = document.getElementById('discover-custom');
  if (!el) return;
  el.innerHTML = '<span class="dim">Discovering models...</span>';
  API.post('/api/keys/discover', { provider: 'custom', custom_id: cpId }).then(function(models) {
    if (!models || models.length === 0) {
      el.innerHTML = '<span class="dim">No models found</span>';
      return;
    }
    el.innerHTML = '<div class="discover-list">' + models.map(function(m) {
      return '<div class="discover-item"><code>' + m.id + '</code></div>';
    }).join('') + '</div>';
  }).catch(function(e) {
    el.innerHTML = '<span class="badge warn">' + esc(e.message) + '</span>';
  });
}

function beginEditCustomProvider(id, btn) {
  _editingCustomProviders[id] = {
    name: btn.getAttribute('data-name') || '',
    base_url: btn.getAttribute('data-base-url') || '',
    api_key: ''
  };
  refreshProviders();
}

function updateCustomProviderDraft(id, field, value) {
  var draft = _editingCustomProviders[id];
  if (!draft) return;
  draft[field] = value;
}

function cancelCustomProviderEdit(id) {
  delete _editingCustomProviders[id];
  refreshProviders();
}

function saveCustomProviderEdit(id) {
  var draft = _editingCustomProviders[id];
  if (!draft) return;
  if (!(draft.name || '').trim()) { alert('Provider name is required'); return; }
  if (!(draft.base_url || '').trim()) { alert('Base URL is required'); return; }
  var body = {
    id: id,
    name: draft.name.trim(),
    base_url: draft.base_url.trim()
  };
  if ((draft.api_key || '').trim()) body.api_key = draft.api_key.trim();
  API.post('/api/custom-provider', body).then(function() {
    delete _editingCustomProviders[id];
    refreshProviders();
  });
}

function testCustomProvider(id) {
  runKeyTest('custom-' + id, { provider: 'custom', custom_id: id });
}

function testEditedCustomProvider(id) {
  var draft = _editingCustomProviders[id] || {};
  var body = {
    provider: 'custom',
    custom_id: id,
    base_url: (draft.base_url || '').trim()
  };
  if ((draft.api_key || '').trim()) body.api_key = draft.api_key.trim();
  runKeyTest('custom-' + id, body);
}

function addCustomProvider() {
  var name = document.getElementById('cp-name');
  var url = document.getElementById('cp-url');
  var key = document.getElementById('cp-key');
  if (!name || !name.value.trim()) { alert('Provider name is required'); return; }
  if (!url || !url.value.trim()) { alert('Base URL is required'); return; }
  API.post('/api/custom-provider', {
    name: name.value.trim(),
    base_url: url.value.trim(),
    api_key: key ? key.value.trim() : ''
  }).then(function() {
    name.value = '';
    url.value = '';
    if (key) key.value = '';
    refreshProviders();
  });
}

function removeCustomProvider(id) {
  if (!confirm('Remove this custom provider?')) return;
  fetch('/api/custom-provider', {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id: id })
  }).then(function(r) {
    if (!r.ok) throw new Error(r.status + ' ' + r.statusText);
    return r.json();
  }).then(refreshProviders);
}

function refreshProviderToken() {
  API.post('/api/token/refresh', {}).then(function() {
    refreshProviders();
  }).catch(function(e) { alert('Token refresh failed: ' + e.message); });
}
