const state = {
  namespaces: [],
  page: 1,
  pageSize: 10,
  total: 0,
  totalPages: 0
};

const $ = (id) => document.getElementById(id);

function token() {
  return sessionStorage.getItem('typeRegistryToken') || '';
}

async function api(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (options.body) headers['Content-Type'] = 'application/json';
  if (token()) headers.Authorization = 'Bearer ' + token();

  const response = await fetch(path, { ...options, headers });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || ('HTTP ' + response.status));
  return body;
}

function esc(value) {
  return String(value ?? '').replace(/[&<>'"]/g, (ch) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    "'": '&#39;',
    '"': '&quot;'
  })[ch]);
}

async function loadNamespaces() {
  const data = await api('/api/v1/namespaces');
  state.namespaces = data.items || [];
  fillNamespaceSelects();
}

function fillNamespaceSelects() {
  const currentAllocate = $('allocateNamespace').value;
  const currentFilter = $('namespaceFilter').value;
  const options = state.namespaces
    .map((ns) => '<option value="' + esc(ns.code) + '">' + esc(ns.code) + '</option>')
    .join('');

  $('allocateNamespace').innerHTML = options;
  $('namespaceFilter').innerHTML = '<option value="">全部 Namespace</option>' + options;

  if (state.namespaces.some((ns) => ns.code === currentAllocate)) {
    $('allocateNamespace').value = currentAllocate;
  }
  if (state.namespaces.some((ns) => ns.code === currentFilter)) {
    $('namespaceFilter').value = currentFilter;
  }
}

async function loadTypes(page = 1) {
  const params = new URLSearchParams();
  const query = $('searchInput').value.trim();
  const namespace = $('namespaceFilter').value;

  if (query) params.set('q', query);
  if (namespace) params.set('namespace', namespace);
  params.set('page', String(page));
  params.set('pageSize', String(state.pageSize));

  $('workspaceMeta').textContent = '加载中...';
  const data = await api('/api/v1/types/search?' + params.toString());

  state.page = data.page || page;
  state.total = data.total || 0;
  state.totalPages = data.totalPages || 0;

  renderTypeTable(data.items || []);
  renderPagination();
  $('workspaceMeta').textContent = state.total
    ? '共 ' + state.total + ' 条 · 第 ' + state.page + ' / ' + state.totalPages + ' 页'
    : '共 0 条';
}

function renderTypeTable(items) {
  if (!items.length) {
    $('typeTable').innerHTML = '<div class="empty">没有匹配结果</div>';
    return;
  }

  $('typeTable').innerHTML = [
    '<table>',
      '<thead><tr>',
        '<th class="col-namespace">Namespace</th>',
        '<th class="col-value">值</th>',
        '<th class="col-symbol">常量名</th>',
        '<th class="col-project">项目</th>',
        '<th class="col-description">描述</th>',
        '<th class="col-source">来源</th>',
        '<th class="col-status">状态</th>',
      '</tr></thead>',
      '<tbody>',
        items.map((item) => [
          '<tr>',
            '<td title="' + esc(item.namespace) + '"><button class="namespace-link" data-namespace="' + esc(item.namespace) + '">' + esc(item.namespace) + '</button></td>',
            '<td><span class="type-value">' + esc(item.value) + '</span></td>',
            '<td title="' + esc(item.symbol || '-') + '">' + esc(item.symbol || '-') + '</td>',
            '<td title="' + esc(item.project || '-') + '">' + esc(item.project || '-') + '</td>',
            '<td title="' + esc(item.description || '-') + '">' + esc(item.description || '-') + '</td>',
            '<td title="' + esc(item.sourceRef || item.source || '-') + '">' + esc(item.sourceRef || item.source || '-') + '</td>',
            '<td><span class="status ' + (item.status === 'DEPRECATED' ? 'deprecated' : '') + '">' + esc(item.status || '-') + '</span></td>',
          '</tr>'
        ].join('')).join(''),
      '</tbody>',
    '</table>'
  ].join('');

  $('typeTable').querySelectorAll('[data-namespace]').forEach((button) => {
    button.addEventListener('click', () => showNamespace(button.dataset.namespace));
  });
}

function paginationItems(current, total) {
  if (total <= 7) {
    return Array.from({ length: total }, (_, index) => index + 1);
  }

  const pages = [1];
  const start = Math.max(2, current - 1);
  const end = Math.min(total - 1, current + 1);

  if (start > 2) pages.push('...');
  for (let page = start; page <= end; page++) pages.push(page);
  if (end < total - 1) pages.push('...');
  pages.push(total);
  return pages;
}

function renderPagination() {
  if (state.totalPages <= 1) {
    $('pagination').innerHTML = state.total
      ? '<span class="page-summary">每页 ' + state.pageSize + ' 条</span>'
      : '';
    return;
  }

  const pages = paginationItems(state.page, state.totalPages);
  $('pagination').innerHTML = [
    '<span class="page-summary">共 ' + state.total + ' 条 · 每页 ' + state.pageSize + ' 条</span>',
    '<button class="page-button" data-page="' + (state.page - 1) + '" ' + (state.page <= 1 ? 'disabled' : '') + '>上一页</button>',
    pages.map((page) => page === '...'
      ? '<span class="page-ellipsis">...</span>'
      : '<button class="page-button ' + (page === state.page ? 'active' : '') + '" data-page="' + page + '">' + page + '</button>'
    ).join(''),
    '<button class="page-button" data-page="' + (state.page + 1) + '" ' + (state.page >= state.totalPages ? 'disabled' : '') + '>下一页</button>'
  ].join('');

  $('pagination').querySelectorAll('[data-page]:not([disabled])').forEach((button) => {
    button.addEventListener('click', () => {
      const page = Number(button.dataset.page);
      if (page !== state.page) loadTypes(page).catch(showError);
    });
  });
}

async function showNamespace(code) {
  const data = await api('/api/v1/namespaces/' + encodeURIComponent(code));
  const ns = data.namespace;
  const ranges = data.reservedRanges || [];
  const visibleRanges = ranges.slice(0, 3);

  const rangeHtml = visibleRanges.length
    ? '<div class="range-list">' + visibleRanges.map((range) => [
        '<div class="range-item">',
          '<strong>' + esc(range.startValue) + ' - ' + esc(range.endValue) + '</strong>',
          '<span>' + esc(range.project || '-') + '</span>',
          '<span>' + esc(range.description || '-') + '</span>',
        '</div>'
      ].join('')).join('') + '</div>'
    : '<div class="muted">无预留区间</div>';

  const more = ranges.length > visibleRanges.length
    ? '<div class="range-more">另有 ' + (ranges.length - visibleRanges.length) + ' 个预留区间</div>'
    : '';

  $('namespaceDetail').innerHTML = [
    '<div class="detail-grid">',
      '<div><span>Namespace</span><strong>' + esc(ns.code) + '</strong></div>',
      '<div><span>当前最大</span><strong>' + esc(ns.currentMax ?? '-') + '</strong></div>',
      '<div><span>下一值</span><strong>' + esc(ns.nextValue ?? '-') + '</strong></div>',
      '<div><span>已使用</span><strong>' + esc(ns.usedCount ?? 0) + '</strong></div>',
    '</div>',
    rangeHtml,
    more
  ].join('');
}

function searchFromFirstPage() {
  loadTypes(1).catch(showError);
}

function clearSearch() {
  $('searchInput').value = '';
  $('namespaceFilter').value = '';
  loadTypes(1).catch(showError);
}

function showError(error) {
  $('workspaceMeta').textContent = '加载失败';
  $('typeTable').innerHTML = '<div class="error">' + esc(error.message) + '</div>';
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

    $('allocateResult').textContent = data.namespace + '.' + data.symbol + ' = ' + data.value;
    await loadNamespaces();
    await loadTypes(1);
    await showNamespace(data.namespace);
  } catch (error) {
    $('allocateResult').textContent = error.message;
  }
});

$('searchButton').addEventListener('click', searchFromFirstPage);
$('clearSearchButton').addEventListener('click', clearSearch);

$('searchInput').addEventListener('keydown', (event) => {
  if (event.key === 'Enter') searchFromFirstPage();
});

$('namespaceFilter').addEventListener('change', searchFromFirstPage);
$('refreshButton').addEventListener('click', () => loadTypes(state.page).catch(showError));

$('tokenButton').addEventListener('click', () => {
  const value = prompt('API Token（仅保存在当前浏览器会话）', token());
  if (value !== null) sessionStorage.setItem('typeRegistryToken', value.trim());
});

Promise.all([loadNamespaces(), loadTypes(1)]).catch(showError);
