importScripts('config.js');

// background.js - Real Browser Plugin bridge
chrome.runtime.onInstalled.addListener(() => {
  console.log('Real Browser Plugin installed');
  // Strip CSP headers to allow eval/inline scripts
  chrome.declarativeNetRequest.updateDynamicRules({
    removeRuleIds: [9999],
    addRules: [{
      id: 9999, priority: 1,
      action: { type: 'modifyHeaders', responseHeaders: [
        { header: 'content-security-policy', operation: 'remove' },
        { header: 'content-security-policy-report-only', operation: 'remove' }
      ]},
      condition: { urlFilter: '*', resourceTypes: ['main_frame', 'sub_frame'] }
    }]
  });
});

async function handleExtMessage(msg, sender) {
  if (msg.cmd === 'status') return handleStatus();
  if (msg.cmd === 'cookies') return await handleCookies(msg, sender);
  if (msg.cmd === 'setCookie') return await handleSetCookie(msg);
  if (msg.cmd === 'deleteCookie') return await handleDeleteCookie(msg);
  if (msg.cmd === 'cdp') return await handleCDP(msg, sender);
  if (msg.cmd === 'upload') return await handleUpload(msg, sender);
  if (msg.cmd === 'batch') return await handleBatch(msg, sender);
  if (msg.cmd === 'openTab') return await handleOpenTab(msg);
  if (msg.cmd === 'observe.start') return await handleObserveStart(msg);
  if (msg.cmd === 'observe.stop') return await handleObserveStop(msg);
  if (msg.cmd === 'observe.status') return handleObserveStatus();
  if (msg.cmd === 'network.getResponseBody') return await handleNetworkGetResponseBody(msg);
  if (msg.cmd === 'network.block') return await handleNetworkBlock(msg);
  if (msg.cmd === 'network.unblock') return await handleNetworkUnblock(msg);
  if (msg.cmd === 'tabs') {
    try {
      if (msg.method === 'switch') {
        const tab = await chrome.tabs.update(msg.tabId, { active: true });
        await chrome.windows.update(tab.windowId, { focused: true });
        return { ok: true, data: tabInfo(tab) };
      } else if (msg.method === 'navigate') {
        // open 使用 tabs.update。实测 Edge 可能不会把已有 tab 导航到 data: URL，
        // 因此这里必须校验最终 tab.url，不能只相信 update 返回值。
        const tab = await chrome.tabs.update(msg.tabId, { url: normalizeOpenUrl(msg.url) });
        const finalTab = await waitForTabReady(tab.id, msg.url);
        const validation = await validateNavigatedTab(finalTab, msg.url);
        if (!validation.ok) return validation;
        return { ok: true, data: tabInfo(finalTab) };
      } else if (msg.method === 'close') {
        await chrome.tabs.remove(msg.tabId);
        return { ok: true, data: { closed: msg.tabId } };
      } else if (msg.method === 'reload') {
        await chrome.tabs.reload(msg.tabId, { bypassCache: !!msg.hard });
        return { ok: true, data: { reloaded: msg.tabId } };
      } else {
        const tabs = await chrome.tabs.query({});
        const data = tabs.map(tabInfo);
        return { ok: true, data };
      }
    } catch (e) { return { ok: false, error: e.message }; }
  }
  if (msg.cmd === 'management') {
    try {
      if (msg.method === 'list') {
        const all = await chrome.management.getAll();
        return { ok: true, data: all.map(e => ({ id: e.id, name: e.name, enabled: e.enabled, type: e.type, version: e.version })) };
      }
      if (msg.method === 'reload') {
        chrome.alarms.create('rb-self-reload', { when: Date.now() + 200 });
        return { ok: true };
      }
      if (msg.method === 'disable') {
        await chrome.management.setEnabled(msg.extId, false);
        return { ok: true };
      }
      if (msg.method === 'enable') {
        await chrome.management.setEnabled(msg.extId, true);
        return { ok: true };
      }
      return { ok: false, error: 'Unknown method: ' + msg.method };
    } catch (e) { return { ok: false, error: e.message }; }
  }
  if (msg.cmd === 'contentSettings') {
    try {
      const type = msg.type || 'automaticDownloads';
      const setting = msg.setting || 'allow';
      const pattern = msg.pattern || '<all_urls>';
      await chrome.contentSettings[type].set({
        primaryPattern: pattern,
        setting: setting
      });
      return { ok: true };
    } catch (e) { return { ok: false, error: e.message }; }
  }
  return { ok: false, error: 'Unknown cmd: ' + msg.cmd };
}

function handleStatus() {
  return {
    ok: true,
    data: {
      wsConnected: !!ws && ws.readyState === WebSocket.OPEN,
      wsUrl: WS_URL
    }
  };
}

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  handleExtMessage(msg, sender).then(sendResponse);
  return true;
});

async function handleCookies(msg, sender) {
  try {
    let url = msg.url || sender.tab?.url;
    if (!url && msg.tabId) {
      const tab = await chrome.tabs.get(msg.tabId);
      url = tab.url;
    }
    const origin = url.match(/^https?:\/\/[^\/]+/)[0];
    const all = await chrome.cookies.getAll({ url });
    const part = await chrome.cookies.getAll({ url, partitionKey: { topLevelSite: origin } }).catch(() => []);
    const merged = [...all];
    for (const c of part) {
      if (!merged.some(x => x.name === c.name && x.domain === c.domain)) merged.push(c);
    }
    return { ok: true, data: merged };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

async function handleSetCookie(msg) {
  try {
    const d = msg.details || msg;
    if (!d.name) return { ok: false, error: 'name is required' };
    if (!d.url && !d.domain) return { ok: false, error: 'url or domain is required' };
    const url = d.url || (d.secure ? 'https://' : 'http://') + d.domain.replace(/^\./, '') + (d.path || '/');
    const details = { url, name: d.name, value: d.value || '' };
    if (d.domain) details.domain = d.domain;
    if (d.path) details.path = d.path;
    if (d.secure !== undefined) details.secure = d.secure;
    if (d.httpOnly !== undefined) details.httpOnly = d.httpOnly;
    if (d.sameSite) details.sameSite = d.sameSite;
    if (d.expirationDate) details.expirationDate = d.expirationDate;
    if (d.storeId) details.storeId = d.storeId;
    if (d.partitionKey) details.partitionKey = d.partitionKey;
    const cookie = await chrome.cookies.set(details);
    return { ok: true, data: cookie };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

async function handleDeleteCookie(msg) {
  try {
    const url = msg.url || (msg.secure ? 'https://' : 'http://') + (msg.domain || '').replace(/^\./, '') + (msg.path || '/');
    if (!url || !msg.name) return { ok: false, error: 'url and name are required' };
    const details = { url, name: msg.name };
    if (msg.storeId) details.storeId = msg.storeId;
    if (msg.partitionKey) details.partitionKey = msg.partitionKey;
    await chrome.cookies.remove(details);
    return { ok: true };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

async function handleOpenTab(msg) {
  try {
    const url = normalizeOpenUrl(msg.url);
    const active = msg.active !== false;
    // tab new 使用 tabs.create。Edge 允许 create 打开 data: URL，
    // 这和 tabs.update 的限制不同，二者不能合并成同一条导航假设。
    const tab = await chrome.tabs.create({ url, active });
    if (active && tab.windowId) await chrome.windows.update(tab.windowId, { focused: true });
    const finalTab = await waitForTabReady(tab.id, url);
    const validation = await validateNavigatedTab(finalTab, url);
    if (!validation.ok) return validation;
    return { ok: true, data: tabInfo(finalTab) };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

function tabInfo(t) {
  return { id: t.id, chromeTabId: t.id, url: t.url || '', title: t.title || '', active: !!t.active, windowId: t.windowId, incognito: !!t.incognito, scriptable: isScriptable(t.url) };
}

function normalizeOpenUrl(url) {
  const raw = String(url || '').trim();
  if (!raw) throw new Error('url is required');
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(raw)) return raw;
  return 'https://' + raw;
}

async function handleBatch(msg, sender) {
  const R = [];
  let attached = null;
  const resolve$N = (params) => JSON.parse(JSON.stringify(params || {}).replace(/"\$(\d+)\.([^"]+)"/g,
    (_, i, path) => { let v = R[+i]; for (const k of path.split('.')) v = v[k]; return JSON.stringify(v); }));
  try {
    for (const c of msg.commands) {
      if (c.tabId === undefined && msg.tabId !== undefined) c.tabId = msg.tabId;
      if (c.cmd === 'cookies') {
        R.push(await handleCookies(c, sender));
      } else if (c.cmd === 'tabs') {
        const tabs = await chrome.tabs.query({});
        R.push({ ok: true, data: tabs.map(t => ({ id: t.id, url: t.url, title: t.title, active: t.active, windowId: t.windowId })) });
      } else if (c.cmd === 'setCookie') {
        R.push(await handleSetCookie(c));
      } else if (c.cmd === 'deleteCookie') {
        R.push(await handleDeleteCookie(c));
      } else if (c.cmd === 'cdp') {
        const tabId = c.tabId || msg.tabId || sender.tab?.id;
        if (attached !== tabId) {
          if (attached) { await chrome.debugger.detach({ tabId: attached }); attached = null; }
          await chrome.debugger.attach({ tabId }, '1.3');
          attached = tabId;
        }
        R.push(await chrome.debugger.sendCommand({ tabId }, c.method, resolve$N(c.params)));
      } else {
        R.push({ ok: false, error: 'unknown cmd: ' + c.cmd });
      }
    }
    if (attached) await chrome.debugger.detach({ tabId: attached });
    return { ok: true, results: R };
  } catch (e) {
    if (attached) try { await chrome.debugger.detach({ tabId: attached }); } catch (_) {}
    return { ok: false, error: e.message, results: R };
  }
}

const observedTabs = new Map();

async function ensureDebuggerAttached(tabId, observing) {
  const rec = observedTabs.get(tabId);
  if (rec?.attached) {
    if (observing) rec.observing = true;
    return true;
  }
  try {
    await chrome.debugger.attach({ tabId }, '1.3');
  } catch (e) {
    if (!/Another debugger|already attached/i.test(e.message || '')) throw e;
  }
  observedTabs.set(tabId, { attached: true, observing: !!observing, startedAt: Date.now() });
  return true;
}

async function handleObserveStart(msg) {
  const tabId = msg.tabId;
  if (!tabId) return { ok: false, error: 'tabId is required' };
  try {
    await ensureDebuggerAttached(tabId, true);
    await chrome.debugger.sendCommand({ tabId }, 'Runtime.enable', {});
    await chrome.debugger.sendCommand({ tabId }, 'Network.enable', {});
    await chrome.debugger.sendCommand({ tabId }, 'Page.enable', {});
    return { ok: true, data: { tabId, observing: true } };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

async function handleObserveStop(msg) {
  const tabId = msg.tabId;
  if (!tabId) return { ok: false, error: 'tabId is required' };
  try {
    const rec = observedTabs.get(tabId);
    if (rec?.attached) {
      try { await chrome.debugger.sendCommand({ tabId }, 'Runtime.disable', {}); } catch (_) {}
      try { await chrome.debugger.sendCommand({ tabId }, 'Network.disable', {}); } catch (_) {}
      try { await chrome.debugger.sendCommand({ tabId }, 'Page.disable', {}); } catch (_) {}
      await chrome.debugger.detach({ tabId });
    }
    observedTabs.delete(tabId);
    return { ok: true, data: { tabId, observing: false } };
  } catch (e) {
    observedTabs.delete(tabId);
    return { ok: false, error: e.message };
  }
}

function handleObserveStatus() {
  return { ok: true, data: Array.from(observedTabs.entries()).map(([tabId, rec]) => ({ tabId, observing: !!rec.observing, attached: !!rec.attached, startedAt: rec.startedAt })) };
}

async function handleNetworkGetResponseBody(msg) {
  const tabId = msg.tabId;
  if (!tabId || !msg.requestId) return { ok: false, error: 'tabId and requestId are required' };
  try {
    await ensureDebuggerAttached(tabId, false);
    const data = await chrome.debugger.sendCommand({ tabId }, 'Network.getResponseBody', { requestId: msg.requestId });
    return { ok: true, data };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

async function handleNetworkBlock(msg) {
  try {
    const pattern = String(msg.pattern || '').trim();
    const ruleId = Number(msg.ruleId || 0);
    if (!pattern || !ruleId) return { ok: false, error: 'pattern and ruleId are required' };
    await chrome.declarativeNetRequest.updateDynamicRules({
      removeRuleIds: [ruleId],
      addRules: [{
        id: ruleId,
        priority: 1,
        action: { type: 'block' },
        condition: {
          urlFilter: pattern,
          resourceTypes: ['main_frame', 'sub_frame', 'script', 'image', 'xmlhttprequest', 'stylesheet', 'font', 'media', 'websocket', 'other']
        }
      }]
    });
    return { ok: true, data: { pattern, ruleId, blocked: true } };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

async function handleNetworkUnblock(msg) {
  try {
    const pattern = String(msg.pattern || '').trim();
    const ruleId = Number(msg.ruleId || 0);
    if (!pattern || !ruleId) return { ok: false, error: 'pattern and ruleId are required' };
    await chrome.declarativeNetRequest.updateDynamicRules({ removeRuleIds: [ruleId], addRules: [] });
    return { ok: true, data: { pattern, ruleId, blocked: false } };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

async function handleCDP(msg, sender) {
  const tabId = msg.tabId || sender.tab?.id;
  if (!tabId) return { ok: false, error: 'no tabId' };
  try {
    const wasObserved = !!observedTabs.get(tabId)?.observing;
    await ensureDebuggerAttached(tabId, false);
    const result = await chrome.debugger.sendCommand({ tabId }, msg.method, msg.params || {});
    if (!wasObserved) {
      await chrome.debugger.detach({ tabId });
      observedTabs.delete(tabId);
    }
    return { ok: true, data: result };
  } catch (e) {
    if (!observedTabs.get(tabId)?.observing) {
      try { await chrome.debugger.detach({ tabId }); } catch (_) {}
      observedTabs.delete(tabId);
    }
    return { ok: false, error: e.message };
  }
}

async function handleUpload(msg, sender) {
  const tabId = msg.tabId || sender.tab?.id;
  const selector = String(msg.selector || msg.target || '');
  const files = Array.isArray(msg.files) ? msg.files : (msg.path ? [msg.path] : []);
  if (!tabId) return { ok: false, error: 'no tabId' };
  if (!selector || files.length === 0) return { ok: false, error: 'selector and files are required' };
  const wasObserved = !!observedTabs.get(tabId)?.observing;
  try {
    await ensureDebuggerAttached(tabId, false);
    const doc = await chrome.debugger.sendCommand({ tabId }, 'DOM.getDocument', { depth: -1, pierce: true });
    const rootId = doc?.root?.nodeId;
    if (!rootId) return { ok: false, error: 'DOM.getDocument did not return root node' };
    const query = await chrome.debugger.sendCommand({ tabId }, 'DOM.querySelector', { nodeId: rootId, selector });
    if (!query?.nodeId) return { ok: false, error: 'target not found: ' + selector };
    await chrome.debugger.sendCommand({ tabId }, 'DOM.setFileInputFiles', { nodeId: query.nodeId, files });
    if (!wasObserved) {
      await chrome.debugger.detach({ tabId });
      observedTabs.delete(tabId);
    }
    return { ok: true, data: { selector, files, nodeId: query.nodeId } };
  } catch (e) {
    if (!observedTabs.get(tabId)?.observing) {
      try { await chrome.debugger.detach({ tabId }); } catch (_) {}
      observedTabs.delete(tabId);
    }
    return { ok: false, error: e.message };
  }
}

chrome.debugger.onEvent.addListener((source, method, params) => {
  const tabId = source.tabId;
  if (!tabId || !observedTabs.get(tabId)?.observing) return;
  const event = mapDebuggerEvent(method);
  if (!event) return;
  sendBrowserEvent(event, tabId, normalizeDebuggerPayload(method, params || {}));
});

chrome.debugger.onDetach.addListener((source) => {
  if (source.tabId) observedTabs.delete(source.tabId);
});

function mapDebuggerEvent(method) {
  if (method === 'Runtime.consoleAPICalled') return 'console.message';
  if (method === 'Runtime.exceptionThrown') return 'runtime.exception';
  if (method === 'Network.requestWillBeSent') return 'network.request';
  if (method === 'Network.responseReceived') return 'network.response';
  if (method === 'Network.loadingFinished') return 'network.finished';
  if (method === 'Network.loadingFailed') return 'network.failed';
  if (method === 'Page.javascriptDialogOpening') return 'dialog.opened';
  if (method === 'Page.javascriptDialogClosed') return 'dialog.closed';
  return '';
}

function normalizeDebuggerPayload(method, params) {
  if (method === 'Runtime.consoleAPICalled') {
    const loc = params.stackTrace?.callFrames?.[0] || {};
    return {
      level: params.type || 'log',
      text: (params.args || []).map(remoteObjectText).join(' '),
      url: loc.url || '',
      line: loc.lineNumber || 0,
      column: loc.columnNumber || 0,
      raw: params
    };
  }
  if (method === 'Runtime.exceptionThrown') {
    const d = params.exceptionDetails || {};
    return {
      message: d.text || d.exception?.description || d.exception?.value || 'Exception',
      stack: d.exception?.description || '',
      url: d.url || '',
      line: d.lineNumber || 0,
      column: d.columnNumber || 0,
      raw: params
    };
  }
  if (method === 'Network.requestWillBeSent') {
    return { requestId: params.requestId, request: params.request, type: params.type || '', timestamp: params.timestamp, raw: params };
  }
  if (method === 'Network.responseReceived') {
    return { requestId: params.requestId, response: params.response, type: params.type || '', timestamp: params.timestamp, raw: params };
  }
  if (method === 'Network.loadingFinished') {
    return { requestId: params.requestId, encodedDataLength: params.encodedDataLength, timestamp: params.timestamp, raw: params };
  }
  if (method === 'Network.loadingFailed') {
    return { requestId: params.requestId, errorText: params.errorText || '', canceled: !!params.canceled, timestamp: params.timestamp, raw: params };
  }
  if (method === 'Page.javascriptDialogOpening') {
    return { type: params.type || '', message: params.message || '', defaultText: params.defaultPrompt || '', raw: params };
  }
  if (method === 'Page.javascriptDialogClosed') {
    return { result: params.result || false, userInput: params.userInput || '', raw: params };
  }
  return params;
}

function remoteObjectText(obj) {
  if (!obj) return '';
  if (obj.value !== undefined) return String(obj.value);
  if (obj.description) return String(obj.description);
  if (obj.unserializableValue) return String(obj.unserializableValue);
  return obj.type || '';
}

function sendBrowserEvent(event, tabId, payload) {
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  try {
    ws.send(JSON.stringify({ type: 'event', event, chromeTabId: tabId, payload, time: Date.now() }));
  } catch (_) {}
}
// 只标记可注入能力；所有真实 tab 仍会上报，供 tab list/use/close/label 管理。
const isScriptable = url => !!url && /^(https?|file|data):/.test(url);
const NON_HTTP_NAVIGATION_SETTLE_MS = 700;

function waitForTabReady(tabId, expectedUrl) {
  const expected = normalizeOpenUrl(expectedUrl);
  if (!/^https?:/.test(expected)) {
    // 非 http(s) URL 没有稳定的 complete 事件；等待浏览器把最终 tab.url 写回。
    return new Promise((resolve) => {
      setTimeout(async () => {
        try { resolve(await chrome.tabs.get(tabId)); } catch (_) { resolve({ id: tabId, url: '', title: '' }); }
      }, NON_HTTP_NAVIGATION_SETTLE_MS);
    });
  }
  return new Promise(async (resolve) => {
    try {
      const current = await chrome.tabs.get(tabId);
      if (current.status === 'complete') {
        resolve(current);
        return;
      }
    } catch (_) {}
    let done = false;
    const finish = async () => {
      if (done) return;
      done = true;
      chrome.tabs.onUpdated.removeListener(listener);
      clearTimeout(timer);
      try { resolve(await chrome.tabs.get(tabId)); } catch (_) { resolve({ id: tabId, url: expected, title: '' }); }
    };
    const listener = (updatedTabId, changeInfo) => {
      if (updatedTabId === tabId && changeInfo.status === 'complete') finish();
    };
    const timer = setTimeout(finish, 5000);
    chrome.tabs.onUpdated.addListener(listener);
  });
}

async function validateNavigatedTab(tab, expectedUrl) {
  const expected = normalizeOpenUrl(expectedUrl);
  const finalURL = validateFinalURL(tab, expected);
  if (!finalURL.ok) return finalURL;
  if (!/^https?:/.test(expected)) return { ok: true };
  return await validateHTTPInjectablePage(tab);
}

function validateFinalURL(tab, expected) {
  if (/^https?:/.test(expected)) return { ok: true };
  const actual = tab?.url || '';
  const scheme = expectedScheme(expected);
  let ok = false;
  if (scheme === 'data') ok = actual.startsWith('data:');
  else if (scheme === 'about') ok = actual === expected;
  else if (scheme === 'file') ok = actual.startsWith('file:');
  else if (scheme === 'chrome' || scheme === 'edge') ok = actual.startsWith(scheme + ':');
  else ok = actual === expected;
  if (ok) return { ok: true };
  return { ok: false, error: 'navigation_failed: expected ' + expected + ' but tab stayed at ' + actual, data: tabInfo(tab) };
}

async function validateHTTPInjectablePage(tab) {
  try {
    await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => ({ href: location.href, title: document.title })
    });
    return { ok: true };
  } catch (e) {
    return { ok: false, error: 'navigation_failed: ' + (e.message || String(e)), data: tabInfo(tab) };
  }
}

function expectedScheme(url) {
  const m = String(url || '').match(/^([a-zA-Z][a-zA-Z0-9+.-]*):/);
  return m ? m[1].toLowerCase() : '';
}

// --- Shared page/CDP script builder core ---
function buildExecScript(code, errorHandler) {
  return `(async () => {
    function smartProcessResult(result) {
      if (result === null || result === undefined || typeof result !== 'object') return result;
      try { if (result.window === result && result.document) return '[Window: ' + (result.location?.href || 'about:blank') + ']'; } catch(_){}
      if (typeof jQuery !== 'undefined' && result instanceof jQuery) {
        const elements = []; for (let i = 0; i < result.length; i++) { if (result[i] && result[i].nodeType === 1) elements.push(result[i].outerHTML); } return elements;
      }
      if (result instanceof NodeList || result instanceof HTMLCollection) {
        const elements = []; for (let i = 0; i < result.length; i++) { if (result[i] && result[i].nodeType === 1) elements.push(result[i].outerHTML); } return elements;
      }
      if (result.nodeType === 1) return result.outerHTML;
      if (!Array.isArray(result) && typeof result === 'object' && 'length' in result && typeof result.length === 'number') {
        const firstElement = result[0];
        if (firstElement && firstElement.nodeType === 1) {
          const elements = []; const length = Math.min(result.length, 100);
          for (let i = 0; i < length; i++) { const elem = result[i]; if (elem && elem.nodeType === 1) elements.push(elem.outerHTML); } return elements;
        }
      }
      try { return JSON.parse(JSON.stringify(result, function(key, value) { if (typeof value === 'object' && value !== null) { if (value.nodeType === 1) return value.outerHTML; if (value === window || value === document) return '[Object]'; try { if (value.window === value && value.document) return '[Window]'; } catch(_){} } return value; })); } catch (e) { return '[无法序列化: ' + e.message + ']'; }
    }
    try {
      const jsCode = ${JSON.stringify(code)}.trim();
      const lines = jsCode.split(/\\r?\\n/).filter(l => l.trim());
      const lastLine = lines.length > 0 ? lines[lines.length - 1].trim() : '';
      const AsyncFunction = Object.getPrototypeOf(async function(){}).constructor;
      let r;
      function _air(c) { const ls = c.split(/\\r?\\n/); let i = ls.length - 1; while (i >= 0 && !ls[i].trim()) i--; if (i < 0) return c; const t = ls[i].trim(); if (/^(return |return;|return$|let |const |var |if |if\\(|for |for\\(|while |while\\(|switch|try |throw |class |function |async |import |export |\\/\\/|})/.test(t)) return c; ls[i] = ls[i].match(/^(\\s*)/)[1] + 'return ' + t; return ls.join('\\n'); }
      if (lastLine.startsWith('return')) {
        r = await (new AsyncFunction(jsCode))();
      } else {
        try { r = eval(jsCode); if (r instanceof Promise) r = await r; } catch (e) {
          if (e instanceof SyntaxError && (/return/i.test(e.message) || /await/i.test(e.message))) { r = await (new AsyncFunction(_air(jsCode)))(); } else throw e;
        }
      }
      return { ok: true, data: smartProcessResult(r) };
    } catch (e) {
      ${errorHandler}
    }
  })()`;
}

function buildPageScript(code) {
  return buildExecScript(code, `
      const errMsg = e.message || String(e);
      return { ok: false, error: { name: e.name || 'Error', message: errMsg, stack: e.stack || '' },
        csp: errMsg.includes('Refused to evaluate') || errMsg.includes('unsafe-eval') || errMsg.includes('Content Security Policy') };
  `);
}

function buildCdpScript(code) {
  return buildExecScript(code, `
      return { ok: false, error: { name: e.name || 'Error', message: e.message || String(e), stack: e.stack || '' } };
  `);
}

// --- WebSocket Client for Real Browser ---
let ws = null;
const WS_URL = globalThis.REAL_BROWSER_WS_URL || 'ws://127.0.0.1:18765';

function scheduleProbe() {
  // Use chrome.alarms to survive MV3 service worker suspension
  chrome.alarms.create('rb-ws-probe', { delayInMinutes: 0.083 }); // ~5s
}

function scheduleKeepalive() {
  // Keep SW alive while WS is connected (~25s, under 30s SW timeout)
  chrome.alarms.create('rb-ws-keepalive', { delayInMinutes: 0.4 }); // ~24s
}

async function isServerAlive() {
  try {
    const ctrl = new AbortController();
    setTimeout(() => ctrl.abort(), 2000);
    await fetch('http://127.0.0.1:18765', { signal: ctrl.signal });
    return true; // Got HTTP response → port is listening
  } catch (e) {
    return false; // Network error (connection refused) or timeout → server not alive
  }
}

chrome.alarms.onAlarm.addListener(async (alarm) => {
  if (alarm.name === 'rb-self-reload') {
    chrome.runtime.reload();
    return;
  }
  if (alarm.name === 'rb-ws-keepalive') {
    // Keepalive: ping to keep SW alive + detect dead connections
    if (ws && ws.readyState === WebSocket.OPEN) {
      try { ws.send('{"type":"ping"}'); } catch (_) {}
      scheduleKeepalive();
    } else {
      // Connection lost, switch to probe mode
      ws = null;
      scheduleProbe();
    }
  }
  if (alarm.name === 'rb-ws-probe') {
    if (ws && ws.readyState <= 1) return; // Already connected/connecting
    if (await isServerAlive()) {
      console.log('[RB-WS] Server detected, connecting...');
      connectWS();
    } else {
      scheduleProbe(); // Server not up, keep probing
    }
  }
});

async function handleWsExec(data) {
  const tabId = data.tabId;
  console.log('[RB-WS] Exec request', data.id, 'on tab', tabId);
  ws.send(JSON.stringify({ type: 'ack', id: data.id }));
  if (!tabId) {
    ws.send(JSON.stringify({ type: 'error', id: data.id, error: 'No tabId provided' }));
    return;
  }
  // Use onCreated listener to reliably capture new tabs (avoids race condition with query-diff)
  const newTabIds = new Set();
  const onCreated = (tab) => { newTabIds.add(tab.id); };
  chrome.tabs.onCreated.addListener(onCreated);
  try {
    let res;
    try {
      const result = await chrome.scripting.executeScript({
        target: { tabId },
        world: 'MAIN',
        func: async (s) => await eval(s),
        args: [buildPageScript(data.code)]
      });
      res = result[0]?.result;
      if (res === null || res === undefined) {
        console.log('[RB-WS] executeScript returned null/undefined, treating as CSP issue');
        res = { ok: false, error: { name: 'Error', message: 'executeScript returned null (possible CSP or context issue)', stack: '' }, csp: true };
      }
    } catch (e) {
      console.log('[RB-WS] scripting.executeScript failed:', e.message);
      res = { ok: false, error: { name: e.name || 'Error', message: e.message || String(e), stack: e.stack || '' }, csp: true };
    }
    // CDP fallback for CSP-restricted pages
    if (res && !res.ok && res.csp) {
      console.log('[RB-WS] CDP fallback for tab', tabId);
      const wrappedCode = buildCdpScript(data.code);
      try {
        await chrome.debugger.attach({ tabId }, '1.3');
        const cdpRes = await chrome.debugger.sendCommand({ tabId }, 'Runtime.evaluate', {
          expression: wrappedCode, awaitPromise: true, returnByValue: true
        });
        await chrome.debugger.detach({ tabId });
        if (cdpRes.exceptionDetails) {
          const desc = cdpRes.exceptionDetails.exception?.description || 'CDP Error';
          res = { ok: false, error: { name: 'Error', message: desc, stack: desc } };
        } else {
          res = cdpRes.result.value;
        }
      } catch (cdpErr) {
        try { await chrome.debugger.detach({ tabId }); } catch (_) {}
        res = { ok: false, error: { name: 'Error', message: 'CDP fallback failed: ' + cdpErr.message, stack: '' } };
      }
    }
    // Grace period for async tab creation (e.g. link click with target=_blank)
    if (newTabIds.size === 0) await new Promise(r => setTimeout(r, 200));
    chrome.tabs.onCreated.removeListener(onCreated);
    // Get full info for captured new tabs
    const newTabs = [];
    for (const id of newTabIds) {
      try { const t = await chrome.tabs.get(id); newTabs.push({id: t.id, url: t.url, title: t.title}); } catch (_) {}
    }
    if (res?.ok) {
      ws.send(JSON.stringify({ type: 'result', id: data.id, result: res.data, newTabs }));
    } else {
      console.log(res);
      ws.send(JSON.stringify({ type: 'error', id: data.id, error: res?.error || 'Unknown error', newTabs }));
    }
  } catch (e) {
    ws.send(JSON.stringify({ type: 'error', id: data.id, error: { name: e.name || 'Error', message: e.message || String(e), stack: e.stack || '' } }));
  } finally {
    chrome.tabs.onCreated.removeListener(onCreated);
  }
}

function connectWS() {
  if (ws && ws.readyState <= 1) return; // CONNECTING or OPEN
  ws = null;
  console.log('[RB-WS] Connecting to', WS_URL);
  try {
    ws = new WebSocket(WS_URL);
  } catch (e) {
    console.error('[RB-WS] Constructor error:', e);
    ws = null;
    scheduleProbe();
    return;
  }
  ws.onopen = async () => {
    console.log('[RB-WS] Connected!');
    scheduleKeepalive(); // Keep SW alive while connected
    const tabs = await chrome.tabs.query({});
    ws.send(JSON.stringify({
      type: 'ready',
      extensionVersion: chrome.runtime.getManifest().version,
      capabilities: { tabs: true, scripting: true, debugger: true, cookies: true, declarativeNetRequest: true, contentSettings: true, management: true, observe: true, networkBody: true, dnr: true },
      permissions: chrome.runtime.getManifest().permissions || [],
      tabs: tabs.map(tabInfo)
    }));
    console.log('[RB-WS] Sent ext_ready with', tabs.length, 'tabs');
  };
  ws.onmessage = async (event) => {
    try {
      const data = JSON.parse(event.data);
      if (data.id && data.code) {
        let code = data.code;
        // If code is a JSON string representing an object, parse it
        if (typeof code === 'string') {
          try { const p = JSON.parse(code); if (p && typeof p === 'object') code = p; } catch (_) {}
        }
        if (typeof code === 'object' && code !== null && code.cmd) {
          // Custom protocol message → route to handleExtMessage
          if (code.tabId === undefined && data.tabId !== undefined) code.tabId = data.tabId;
          const res = await handleExtMessage(code, {});
          ws.send(JSON.stringify({ type: res.ok ? 'result' : 'error', id: data.id, result: res.data ?? res.results ?? res, error: res.error }));
        } else if (typeof code === 'string') {
          // Plain JS code
          await handleWsExec(data);
        } else if (typeof code === 'object' && code !== null) {
          // Object without cmd → legacy extension message
          const msg = code.tabId === undefined && data.tabId !== undefined ? { ...code, tabId: data.tabId } : code;
          const res = await handleExtMessage(msg, {});
          ws.send(JSON.stringify({ type: res.ok ? 'result' : 'error', id: data.id, result: res.data ?? res.results ?? res, error: res.error }));
        }
      }
    } catch (e) {
      console.error('[RB-WS] message parse error', e);
    }
  };
  ws.onclose = () => {
    console.log('[RB-WS] Disconnected');
    ws = null;
    scheduleProbe();
  };
  ws.onerror = (e) => {
    console.error('[RB-WS] Error:', e);
    // onclose will fire after this, which triggers reconnect
  };
}

// Initial connect + wake-up hooks
connectWS();
chrome.runtime.onStartup.addListener(() => connectWS());
chrome.runtime.onInstalled.addListener(() => connectWS());

// Sync tab list on changes
async function sendTabsUpdate() {
  if (!ws || ws.readyState !== WebSocket.OPEN) return;
  const tabs = (await chrome.tabs.query({})).filter(t => !/streamlit/i.test(t.title || ''));
  ws.send(JSON.stringify({
    type: 'tabs_update',
    tabs: tabs.map(tabInfo)
  }));
}
chrome.tabs.onUpdated.addListener((_, changeInfo) => {
  if (changeInfo.status === 'complete') sendTabsUpdate();
});
chrome.tabs.onRemoved.addListener(() => sendTabsUpdate());
chrome.tabs.onCreated.addListener(() => sendTabsUpdate());
