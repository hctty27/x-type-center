const state = {
  namespaces: [],
  filtered: [],
  page: 1,
  pageSize: 10,
  selectedNamespace: ''
};

const $ = (id) => document.getElementById(id);

async function api(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (options.body) headers['Content-Type'] = 'application/json';

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

function valueOrDash(value) {
  return value === null || value === undefined || value === '' ? '-' : value;
}

function formatRange(ns) {
  if (ns.minValue == null && ns.maxValue == null) return '不限';
  return valueOrDash(ns.minValue) + ' ~ ' + (ns.maxValue == null ? '∞' : ns.maxValue);
}

async function loadNamespaces() {
  $('workspaceMeta').textContent = '加载中...';
  const data = await api('/api/v1/namespaces');
  state.namespaces = data.items || [];

  if (!state.selectedNamespace || !state.namespaces.some((ns) => ns.code === state.selectedNamespace)) {
    state.selectedNamespace = state.namespaces[0]?.code || '';
  }

  fillAllocateSelect();
  renderSummary();
  applyFilter(false);

  if (state.selectedNamespace) {
    await showNamespace(state.selectedNamespace, false);
  } else {
    $('namespaceDetail').innerHTML = '<div class="detail-empty">暂无 Namespace。</div>';
  }
}

function fillAllocateSelect() {
  const current = state.selectedNamespace || $('allocateNamespace').value;
  $('allocateNamespace').innerHTML = state.namespaces
    .map((ns) => '<option value="' + esc(ns.code) + '">' + esc(ns.code) + '</option>')
    .join('');

  if (current && state.namespaces.some((ns) => ns.code === current)) {
    $('allocateNamespace').value = current;
  }
}

function renderSummary() {
  $('namespaceCount').textContent = state.namespaces.length;
  $('activeCount').textContent = state.namespaces.filter((ns) => ns.status === 'ACTIVE').length;
  $('usedCount').textContent = state.namespaces.reduce((sum, ns) => sum + Number(ns.usedCount || 0), 0);
}

function applyFilter(resetPage = true) {
  const query = $('searchInput').value.trim().toLowerCase();

  state.filtered = query
    ? state.namespaces.filter((ns) => [
        ns.code,
        ns.displayName,
        ns.description,
        ns.status
      ].some((value) => String(value || '').toLowerCase().includes(query)))
    : [...state.namespaces];

  if (resetPage) state.page = 1;

  const totalPages = Math.max(1, Math.ceil(state.filtered.length / state.pageSize));
  state.page = Math.min(state.page, totalPages);

  renderNamespaceTable();
  renderPagination();

  $('workspaceMeta').textContent = state.filtered.length
    ? '共 ' + state.filtered.length + ' 个 Namespace · 第 ' + state.page + ' / ' + totalPages + ' 页'
    : '没有匹配的 Namespace';
}

function renderNamespaceTable() {
  const start = (state.page - 1) * state.pageSize;
  const items = state.filtered.slice(start, start + state.pageSize);

  if (!items.length) {
    $('namespaceTable').innerHTML = '<div class="empty">没有匹配结果</div>';
    return;
  }

  $('namespaceTable').innerHTML = [
    '<table>',
      '<thead><tr>',
        '<th class="col-code">Namespace</th>',
        '<th class="col-name">名称</th>',
        '<th class="col-number">当前最大</th>',
        '<th class="col-number">下一可用</th>',
        '<th class="col-number">已使用</th>',
        '<th class="col-range">值范围</th>',
        '<th class="col-status">状态</th>',
      '</tr></thead>',
      '<tbody>',
        items.map((ns) => [
          '<tr class="namespace-row ' + (ns.code === state.selectedNamespace ? 'selected' : '') + '" data-namespace="' + esc(ns.code) + '">',
            '<td><span class="namespace-code">' + esc(ns.code) + '</span></td>',
            '<td title="' + esc(ns.description || ns.displayName || '-') + '">' + esc(ns.displayName || ns.description || '-') + '</td>',
            '<td><strong>' + esc(valueOrDash(ns.currentMax)) + '</strong></td>',
            '<td><span class="next-value">' + esc(valueOrDash(ns.nextValue)) + '</span></td>',
            '<td>' + esc(ns.usedCount || 0) + '</td>',
            '<td>' + esc(formatRange(ns)) + '</td>',
            '<td><span class="status-pill ' + (ns.status === 'ACTIVE' ? 'active' : 'inactive') + '">' + esc(ns.status || '-') + '</span></td>',
          '</tr>'
        ].join('')).join(''),
      '</tbody>',
    '</table>'
  ].join('');

  $('namespaceTable').querySelectorAll('[data-namespace]').forEach((row) => {
    row.addEventListener('click', () => showNamespace(row.dataset.namespace));
  });
}

function paginationItems(current, total) {
  if (total <= 7) return Array.from({ length: total }, (_, index) => index + 1);

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
  const totalPages = Math.ceil(state.filtered.length / state.pageSize);

  if (totalPages <= 1) {
    $('pagination').innerHTML = state.filtered.length
      ? '<span class="page-summary">每页最多 ' + state.pageSize + ' 个 Namespace</span>'
      : '';
    return;
  }

  const pages = paginationItems(state.page, totalPages);
  $('pagination').innerHTML = [
    '<span class="page-summary">每页 ' + state.pageSize + ' 个</span>',
    '<button class="page-button" data-page="' + (state.page - 1) + '" ' + (state.page <= 1 ? 'disabled' : '') + '>上一页</button>',
    pages.map((page) => page === '...'
      ? '<span class="page-ellipsis">...</span>'
      : '<button class="page-button ' + (page === state.page ? 'active' : '') + '" data-page="' + page + '">' + page + '</button>'
    ).join(''),
    '<button class="page-button" data-page="' + (state.page + 1) + '" ' + (state.page >= totalPages ? 'disabled' : '') + '>下一页</button>'
  ].join('');

  $('pagination').querySelectorAll('[data-page]:not([disabled])').forEach((button) => {
    button.addEventListener('click', () => {
      state.page = Number(button.dataset.page);
      renderNamespaceTable();
      renderPagination();
      const total = Math.max(1, Math.ceil(state.filtered.length / state.pageSize));
      $('workspaceMeta').textContent = '共 ' + state.filtered.length + ' 个 Namespace · 第 ' + state.page + ' / ' + total + ' 页';
    });
  });
}

async function showNamespace(code, rerender = true) {
  state.selectedNamespace = code;
  if ($('allocateNamespace').value !== code) $('allocateNamespace').value = code;
  if (rerender) renderNamespaceTable();

  $('namespaceDetail').innerHTML = '<div class="detail-empty">加载中...</div>';
  const data = await api('/api/v1/namespaces/' + encodeURIComponent(code));
  const ns = data.namespace;
  const ranges = data.reservedRanges || [];
  const visibleRanges = ranges.slice(0, 4);

  const rangeHtml = visibleRanges.length
    ? '<div class="range-list">' + visibleRanges.map((range) => [
        '<div class="range-item">',
          '<strong>' + esc(range.startValue) + ' - ' + esc(range.endValue) + '</strong>',
          '<span class="range-project">' + esc(range.project || '全局') + '</span>',
          '<span title="' + esc(range.description || '') + '">' + esc(range.description || '无说明') + '</span>',
        '</div>'
      ].join('')).join('') + '</div>'
    : '<div class="no-range">无预留区间</div>';

  const more = ranges.length > visibleRanges.length
    ? '<div class="range-more">另有 ' + (ranges.length - visibleRanges.length) + ' 个预留区间</div>'
    : '';

  $('namespaceDetail').innerHTML = [
    '<div class="detail-header">',
      '<div>',
        '<span class="detail-label">Namespace</span>',
        '<strong>' + esc(ns.code) + '</strong>',
      '</div>',
      '<span class="status-pill ' + (ns.status === 'ACTIVE' ? 'active' : 'inactive') + '">' + esc(ns.status || '-') + '</span>',
    '</div>',
    '<div class="detail-grid">',
      '<div><span>当前最大</span><strong>' + esc(valueOrDash(ns.currentMax)) + '</strong></div>',
      '<div><span>下一可用</span><strong>' + esc(valueOrDash(ns.nextValue)) + '</strong></div>',
      '<div><span>已使用</span><strong>' + esc(ns.usedCount || 0) + '</strong></div>',
      '<div><span>值范围</span><strong>' + esc(formatRange(ns)) + '</strong></div>',
    '</div>',
    '<div class="description-box">',
      '<span>说明</span>',
      '<p>' + esc(ns.description || ns.displayName || '暂无说明') + '</p>',
    '</div>',
    '<div class="range-title"><span>预留区间</span><strong>' + ranges.length + '</strong></div>',
    rangeHtml,
    more
  ].join('');
}

function clearSearch() {
  $('searchInput').value = '';
  applyFilter(true);
}

function showError(error) {
  $('workspaceMeta').textContent = '加载失败';
  $('namespaceTable').innerHTML = '<div class="error">' + esc(error.message) + '</div>';
}

$('allocateForm').addEventListener('submit', async (event) => {
  event.preventDefault();

  const namespace = $('allocateNamespace').value;
  if (!namespace) {
    $('allocateResult').textContent = '没有可用的 Namespace';
    $('allocateResult').className = 'allocate-result error-text';
    return;
  }

  $('allocateResult').textContent = '正在申请...';
  $('allocateResult').className = 'allocate-result muted';

  try {
    const data = await api('/api/v1/types/allocate', {
      method: 'POST',
      body: JSON.stringify({
        namespace,
        project: $('project').value.trim(),
        symbol: $('symbol').value.trim(),
        description: $('description').value.trim(),
        requirement: $('requirement').value.trim(),
        requester: $('requester').value.trim()
      })
    });

    const symbol = data.symbol ? ' · ' + data.symbol : '';
    $('allocateResult').textContent = '申请成功 · ' + data.namespace + ' = ' + data.value + symbol;
    $('allocateResult').className = 'allocate-result success-text';

    state.selectedNamespace = data.namespace;
    await loadNamespaces();
    await showNamespace(data.namespace);
  } catch (error) {
    $('allocateResult').textContent = error.message;
    $('allocateResult').className = 'allocate-result error-text';
  }
});

$('searchButton').addEventListener('click', () => applyFilter(true));
$('clearSearchButton').addEventListener('click', clearSearch);
$('searchInput').addEventListener('keydown', (event) => {
  if (event.key === 'Enter') applyFilter(true);
});
$('searchInput').addEventListener('input', () => applyFilter(true));
$('allocateNamespace').addEventListener('change', (event) => showNamespace(event.target.value).catch(showError));
$('refreshButton').addEventListener('click', () => loadNamespaces().catch(showError));

loadNamespaces().catch(showError);
