// app.js - router + topbar refresh + init
var currentPage = 'overview';
var refreshInterval = null;

function navigateTo(page) {
  currentPage = page;
  document.querySelectorAll('.nav-item').forEach(function(n) {
    n.classList.toggle('active', n.getAttribute('data-page') === page);
  });
  var content = document.getElementById('content');
  content.innerHTML = '';

  if (refreshInterval) { clearInterval(refreshInterval); refreshInterval = null; }

  switch (page) {
    case 'overview':
      renderOverview(content);
      refreshInterval = setInterval(refreshOverview, 10000);
      break;
    case 'providers':
      renderProviders(content);
      break;
    case 'logs':
      renderLogs(content);
      refreshInterval = setInterval(refreshLogs, 10000);
      break;
    case 'models':
      renderModels(content);
      break;
    case 'stats':
      renderStats(content);
      refreshInterval = setInterval(refreshStats, 10000);
      break;
  }
}

// Topbar refresh
function refreshTopbar() {
  API.get('/api/status').then(function(d) {
    document.getElementById('topbar-dot').className = 'dot ' + (d.running ? 'green' : 'red');
    document.getElementById('topbar-listen').textContent = d.listen;
    document.getElementById('topbar-uptime').textContent = d.uptime;

    var tEl = document.getElementById('topbar-token');
    var auth = d.auth || {};
    var claude = auth.claude || {};
    var allOk = claude.local_available || false;
    if (allOk) {
      tEl.innerHTML = '<span class="tag green">Auth OK</span> <span class="dim">' + (claude.local_expires_in || '') + '</span>';
    } else {
      tEl.innerHTML = '<span class="tag yellow">No Local Auth</span>';
    }
  }).catch(function() {
    document.getElementById('topbar-dot').className = 'dot red';
    document.getElementById('topbar-token').innerHTML = '<span class="tag red">Disconnected</span>';
  });
}

// Sidebar click handlers
document.querySelectorAll('.nav-item').forEach(function(item) {
  item.addEventListener('click', function() {
    navigateTo(item.getAttribute('data-page'));
  });
});

// Init
refreshTopbar();
setInterval(refreshTopbar, 5000);
navigateTo('overview');
