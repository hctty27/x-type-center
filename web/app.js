const state = { namespaces: [] };
const $ = (id) => document.getElementById(id);

function token() {
  return sessionStorage.getItem('typeRegistryToken') || '';
}

async function api(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (options.body) headers['Content-Type'] = 'application/json';
  if (token()) headers.Authorization = `Bearer ${token()}`;
  const response = await fetch(path, { ...options, headers });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `HTTP ${response.status}`);
  return body;
}

function esc(value) {
  return String(value ?? '').replace(/[&<>'"]/g, (ch) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;'
  })[ch]);
}

async function loadNamespaces() {
  const data = await api('/api/v1/namespaces');
  state.namespaces = data.items || [];
  renderNamespaces();
  fillNamespaceSelects();
}

function renderNamespaces() {
  if (!state.namespaces.length) {
    $('namespaceTable').innerHTML = '<div class="empty">暂无数据，请先导入 Excel。</div>';
    return;
  }
  const rows = state.namespaces.map((ns) => `
    <tr class="clickable" data-namespace="${esc(ns.code)}">
      <td><strong>${esc(ns.code)}</strong><div class="small">${esc(ns.displayName)}</div></td>
      <td>${ns.currentMax ?? '-'}</td>
      <td>${ns.nextValue ?? '-'}</td>
      <td>${ns.usedCount ?? 0}</td>
    </tr>`).join('');
  $('namespaceTable').innerHTML = `
    <table><thead><tr><th>Namespace</th><th>当前最大值</th><th>分配游标</th><th>已使用</th></tr></thead>
    <tbody>${rows}</tbody></table>`;
  document.querySelectorAll('[data-namespace]').forEach((row) => {
    row.addEventListener('click', () => showNamespace(row.dataset.namespace));
  });
}

function fillNamespaceSelects() {
  const options = state.namespaces.map((ns) => `<option value="${esc(ns.code)}">${esc(ns.code)}</option>`).join('');
  $('allocateNamespace').innerHTML = options;
  $('namespaceFilter').innerHTML = `<option value="">全部 Namespace</option>${options}`;
}

async function showNamespace(code) {
  const data = await api(`/api/v1/namespaces/${encodeURIComponent(code)}`);
  const ns = data.namespace;
  const ranges = data.reservedRanges || [];
  $('namespaceDetail').innerHTML = `
    <div class="detail-grid">
      <div><span>Namespace</span><strong>${esc(ns.code)}</strong></div>
      <div><span>当前最大值</span><strong>${ns.currentMax ?? '-'}</strong></div>
      <div><span>分配游标</span><strong>${ns.nextValue}</strong></div>
      <div><span>已使用</span><strong>${ns.usedCount}</strong></div>
    </div>
    <h3>预留区间</h3>
    ${ranges.length ? `<table><thead><tr><th>区间</th><th>项目</th><th>说明</th></tr></thead><tbody>${ranges.map((r) => `
      <tr><td>${r.startValue} - ${r.endValue}</td><td>${esc(r.project || '-')}</td><td>${esc(r.description || '-')}</td></tr>`).join('')}</tbody></table>` : '<div class="muted">无预留区间</div>'}`;
}

async function search() {
  const params = new URLSearchParams();
  const query = $('searchInput').value.trim();
  const namespace = $('namespaceFilter').value;
  if (query) params.set('q', query);
  if (namespace) params.set('namespace', namespace);
  params.set('limit', '100');
  const data = await api(`/api/v1/types/search?${params}`);
  const items = data.items || [];
  $('searchResult').innerHTML = items.length ? `
    <table><thead><tr><th>Namespace</th><th>值</th><th>常量</th><th>项目</th><th>描述</th><th>来源</th></tr></thead>
    <tbody>${items.map((e) => `<tr>
      <td>${esc(e.namespace)}</td><td><strong>${e.value}</strong></td><td>${esc(e.symbol || '-')}</td>
      <td>${esc(e.project || '-')}</td><td>${esc(e.description || '-')}</td><td>${esc(e.sourceRef || e.source || '-')}</td>
    </tr>`).join('')}</tbody></table>` : '<div class="empty">没有匹配结果</div>';
}

$('allocateForm').addEventListener('submit', async (event) => {
  event.preventDefault();
  $('allocateResult').textContent = '提交中...';
  try {
    const data = await api('/api/v1/types/allocate', {
      method: 'POST',
      body: JSON.stringify({
        namespace: $('allocateNamespace').value,
        project: $('project').value.trim(),
        symbol: $('symbol').value.trim(),
        description: $('description').value.trim(),
        requirement: $('requirement').value.trim(),
        requester: $('requester').value.trim()
      })
    });
    $('allocateResult').textContent = `${data.namespace}.${data.symbol} = ${data.value}`;
    await loadNamespaces();
  } catch (error) {
    $('allocateResult').textContent = error.message;
  }
});

$('searchButton').addEventListener('click', () => search().catch((e) => alert(e.message)));
$('searchInput').addEventListener('keydown', (event) => {
  if (event.key === 'Enter') search().catch((e) => alert(e.message));
});
$('refreshButton').addEventListener('click', () => loadNamespaces().catch((e) => alert(e.message)));
$('tokenButton').addEventListener('click', () => {
  const value = prompt('API Token（仅保存在当前浏览器会话）', token());
  if (value !== null) sessionStorage.setItem('typeRegistryToken', value.trim());
});

loadNamespaces().catch((error) => {
  $('namespaceTable').innerHTML = `<div class="error">${esc(error.message)}</div>`;
});
