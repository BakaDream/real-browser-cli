let allCookies = [];
let currentTabUrl = '';
let editingCookie = null; // null = add mode, object = edit mode

// --- Theme ---
const THEME_KEY = 'rb-plugin-theme';

function getTheme() {
  return localStorage.getItem(THEME_KEY) || 'system';
}

function applyTheme(theme) {
  const root = document.documentElement;
  if (theme === 'system') {
    root.removeAttribute('data-theme');
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    root.setAttribute('data-theme', prefersDark ? 'dark' : 'light');
  } else {
    root.setAttribute('data-theme', theme);
  }
  localStorage.setItem(THEME_KEY, theme);
  document.querySelectorAll('.theme-btn').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.theme === theme);
  });
}

function initTheme() {
  const theme = getTheme();
  applyTheme(theme);
  document.querySelectorAll('.theme-btn').forEach(btn => {
    btn.addEventListener('click', () => applyTheme(btn.dataset.theme));
  });
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if (getTheme() === 'system') applyTheme('system');
  });
}

// --- Connection Status ---
async function updateConnStatus() {
  try {
    const resp = await chrome.runtime.sendMessage({ cmd: 'status' });
    const connected = !!resp?.ok && !!resp?.data?.wsConnected;
    document.getElementById('connDot').classList.toggle('connected', connected);
    document.getElementById('connLabel').textContent = connected ? 'Connected' : 'Disconnected';
  } catch (_) {
    document.getElementById('connDot').classList.remove('connected');
    document.getElementById('connLabel').textContent = 'Disconnected';
  }
}

document.addEventListener('DOMContentLoaded', () => {
  initTheme();
  updateConnStatus();
  setInterval(updateConnStatus, 3000);
  document.getElementById('refresh').addEventListener('click', fetchCookies);
  document.getElementById('addBtn').addEventListener('click', () => showForm());
  document.getElementById('exportJson').addEventListener('click', exportJSON);
  document.getElementById('exportTxt').addEventListener('click', exportTXT);
  document.getElementById('importBtn').addEventListener('click', () => document.getElementById('importFile').click());
  document.getElementById('importFile').addEventListener('change', importJSON);
  document.getElementById('deleteAll').addEventListener('click', deleteAll);
  document.getElementById('formSave').addEventListener('click', saveForm);
  document.getElementById('formCancel').addEventListener('click', hideForm);
  document.getElementById('filter').addEventListener('input', renderTable);
  // SameSite=None requires Secure
  document.getElementById('fSameSite').addEventListener('change', (e) => {
    if (e.target.value === 'no_restriction') document.getElementById('fSecure').checked = true;
  });
  fetchCookies();
});

async function fetchCookies() {
  try {
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (!tab?.url) { setStatus('No active tab'); return; }
    currentTabUrl = tab.url;
    const resp = await chrome.runtime.sendMessage({ cmd: 'cookies', url: tab.url });
    if (!resp?.ok) { setStatus('Error: ' + (resp?.error || 'unknown')); return; }
    allCookies = resp.data || [];
    renderTable();
  } catch (e) { setStatus('Error: ' + e.message); }
}

function renderTable() {
  const body = document.getElementById('cookieBody');
  const filter = document.getElementById('filter').value.toLowerCase().trim();
  const filtered = filter
    ? allCookies.filter(c => (c.name + c.value + c.domain).toLowerCase().includes(filter))
    : allCookies;

  body.innerHTML = filtered.map((c, i) => {
    const idx = allCookies.indexOf(c);
    const flags = (c.httpOnly ? '<span class="flag">[H]</span> ' : '') +
                  (c.secure ? '<span class="flag">[S]</span> ' : '') +
                  (c.partitionKey ? '<span class="flag">[P]</span>' : '');
    const val = escHtml(c.value || '');
    const name = escHtml(c.name);
    const domain = escHtml(c.domain);
    return '<tr>' +
      '<td title="' + name + '">' + name + '</td>' +
      '<td title="' + val + '">' + val + '</td>' +
      '<td title="' + domain + '">' + domain + '</td>' +
      '<td>' + flags + '</td>' +
      '<td><button class="act-btn" data-act="edit" data-idx="' + idx + '" title="Edit">&#9998;</button>' +
      '<button class="act-btn del" data-act="del" data-idx="' + idx + '" title="Delete">&times;</button></td>' +
      '</tr>';
  }).join('');

  // Delegate click events
  body.querySelectorAll('.act-btn').forEach(btn => {
    btn.addEventListener('click', (e) => {
      const act = e.target.dataset.act;
      const idx = parseInt(e.target.dataset.idx);
      if (act === 'edit') showForm(allCookies[idx]);
      else if (act === 'del') deleteOne(idx);
    });
  });

  setStatus(allCookies.length + ' cookies' + (filter ? ' (' + filtered.length + ' shown)' : ''));
}

function escHtml(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function showForm(cookie) {
  editingCookie = cookie || null;
  const f = (id, val) => document.getElementById(id).value = val || '';
  if (cookie) {
    f('fName', cookie.name);
    f('fValue', cookie.value);
    f('fDomain', cookie.domain);
    f('fPath', cookie.path);
    document.getElementById('fSecure').checked = !!cookie.secure;
    document.getElementById('fHttpOnly').checked = !!cookie.httpOnly;
    f('fSameSite', cookie.sameSite || 'lax');
    f('fExpires', cookie.expirationDate || 0);
  } else {
    f('fName', '');
    f('fValue', '');
    f('fDomain', '');
    f('fPath', '/');
    document.getElementById('fSecure').checked = false;
    document.getElementById('fHttpOnly').checked = false;
    f('fSameSite', 'lax');
    f('fExpires', 0);
  }
  document.getElementById('cookieForm').style.display = 'block';
  document.getElementById('cookieTable').style.display = 'none';
}

function hideForm() {
  editingCookie = null;
  document.getElementById('cookieForm').style.display = 'none';
  document.getElementById('cookieTable').style.display = 'block';
}

async function saveForm() {
  const name = document.getElementById('fName').value.trim();
  const domain = document.getElementById('fDomain').value.trim();
  if (!name) { setStatus('Error: name is required'); return; }
  if (!domain && !editingCookie) { setStatus('Error: domain is required'); return; }

  const sameSite = document.getElementById('fSameSite').value;
  const secure = sameSite === 'no_restriction' ? true : document.getElementById('fSecure').checked;

  const details = {
    name,
    value: document.getElementById('fValue').value,
    domain: domain || editingCookie?.domain,
    path: document.getElementById('fPath').value || '/',
    secure,
    httpOnly: document.getElementById('fHttpOnly').checked,
    sameSite,
  };
  // Build url for chrome.cookies.set()
  if (editingCookie?.url) {
    details.url = editingCookie.url;
  } else {
    details.url = (secure ? 'https://' : 'http://') + details.domain.replace(/^\./, '') + details.path;
  }
  const exp = parseInt(document.getElementById('fExpires').value) || 0;
  if (exp > 0) details.expirationDate = exp;

  // Preserve partitionKey/storeId from original cookie if editing
  if (editingCookie) {
    if (editingCookie.partitionKey) details.partitionKey = editingCookie.partitionKey;
    if (editingCookie.storeId) details.storeId = editingCookie.storeId;
  }

  try {
    const resp = await chrome.runtime.sendMessage({ cmd: 'setCookie', details });
    if (!resp?.ok) { setStatus('Error: ' + (resp?.error || 'save failed')); return; }
    hideForm();
    await fetchCookies();
    setStatus('Cookie saved: ' + name);
  } catch (e) { setStatus('Error: ' + e.message); }
}

async function deleteOne(idx) {
  const c = allCookies[idx];
  if (!c) return;
  const url = c.url || (c.secure ? 'https://' : 'http://') + c.domain.replace(/^\./, '') + (c.path || '/');
  try {
    const details = { url, name: c.name, secure: c.secure, domain: c.domain, path: c.path };
    if (c.storeId) details.storeId = c.storeId;
    if (c.partitionKey) details.partitionKey = c.partitionKey;
    const resp = await chrome.runtime.sendMessage({ cmd: 'deleteCookie', ...details });
    if (!resp?.ok) { setStatus('Error: ' + (resp?.error || 'delete failed')); return; }
    await fetchCookies();
    setStatus('Deleted: ' + c.name);
  } catch (e) { setStatus('Error: ' + e.message); }
}

async function deleteAll() {
  if (!allCookies.length) { setStatus('No cookies to delete'); return; }
  if (!confirm('Delete all ' + allCookies.length + ' cookies?')) return;
  let ok = 0, fail = 0;
  for (const c of allCookies) {
    const url = c.url || (c.secure ? 'https://' : 'http://') + c.domain.replace(/^\./, '') + (c.path || '/');
    const details = { url, name: c.name, secure: c.secure, domain: c.domain, path: c.path };
    if (c.storeId) details.storeId = c.storeId;
    if (c.partitionKey) details.partitionKey = c.partitionKey;
    try {
      const resp = await chrome.runtime.sendMessage({ cmd: 'deleteCookie', ...details });
      resp?.ok ? ok++ : fail++;
    } catch (_) { fail++; }
  }
  await fetchCookies();
  setStatus('Deleted ' + ok + ' cookies' + (fail ? ', ' + fail + ' failed' : ''));
}

function getFiltered() {
  const filter = document.getElementById('filter').value.toLowerCase().trim();
  return filter
    ? allCookies.filter(c => (c.name + c.value + c.domain).toLowerCase().includes(filter))
    : allCookies;
}

function exportJSON() {
  const data = getFiltered().map(c => ({
    name: c.name, value: c.value, domain: c.domain, path: c.path,
    secure: c.secure, httpOnly: c.httpOnly, sameSite: c.sameSite,
    expirationDate: c.expirationDate || null, hostOnly: c.hostOnly,
    session: c.session, storeId: c.storeId, partitionKey: c.partitionKey || null
  }));
  downloadBlob(JSON.stringify(data, null, 2), 'cookies-' + getDomain() + '-' + Date.now() + '.json', 'application/json');
  setStatus('Exported ' + data.length + ' cookies as JSON');
}

function exportTXT() {
  const content = getFiltered().map(c => c.name + '=' + c.value).join('\n');
  downloadBlob(content, 'cookies-' + getDomain() + '-' + Date.now() + '.txt', 'text/plain');
  setStatus('Exported ' + getFiltered().length + ' cookies as TXT');
}

function downloadBlob(content, filename, mime) {
  const blob = new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

async function importJSON(e) {
  const file = e.target.files[0];
  if (!file) return;
  e.target.value = ''; // Reset so same file can be re-imported
  try {
    const text = await file.text();
    const items = JSON.parse(text);
    if (!Array.isArray(items)) { setStatus('Error: JSON must be an array'); return; }
    let ok = 0, fail = 0;
    for (const item of items) {
      if (!item.name) { fail++; continue; }
      if (!item.url && !item.domain) {
        // Derive domain from current tab
        try {
          const u = new URL(currentTabUrl);
          item.domain = u.hostname;
        } catch (_) { fail++; continue; }
      }
      if (!item.url) {
        item.url = (item.secure ? 'https://' : 'http://') + (item.domain || '').replace(/^\./, '') + (item.path || '/');
      }
      try {
        const resp = await chrome.runtime.sendMessage({ cmd: 'setCookie', details: item });
        resp?.ok ? ok++ : fail++;
      } catch (_) { fail++; }
    }
    await fetchCookies();
    setStatus('Imported ' + ok + ' cookies' + (fail ? ', ' + fail + ' failed' : ''));
  } catch (err) { setStatus('Import error: ' + err.message); }
}

function getDomain() {
  try { return new URL(currentTabUrl).hostname.replace(/^www\./, ''); } catch (_) { return 'unknown'; }
}

function setStatus(msg) {
  document.getElementById('status').textContent = msg;
}
