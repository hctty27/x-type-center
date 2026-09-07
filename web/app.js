const state = {
  namespaces: [],
  filteredNamespaces: [],
  namespacePage: 1,
  namespacePageSize: 20,
  selectedNamespace: '',
  comboItems: [],
  comboActiveIndex: -1,
  entries: {
    namespace: '',
    page: 1,
    pageSize: 20,
    total: 0,
    totalPages: 0,
    query: ''
  }
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

function namespaceLabel(ns) {
  return ns.code + '（' + (ns.displayName || '未命名') + '）';
}

function valueOrDash(value) {
  return value === null || value === undefined || value === '' ? '-' : value;
}

function formatRange(ns) {
  if (ns.minValue == null && ns.maxValue == null) return '不限';
  return valueOrDash(ns.minValue) + ' ~ ' + (ns.maxValue == null ? '∞' : ns.maxValue);
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

function renderPagination(containerId, current, totalPages, onPage) {
  const container = $(containerId);

  if (totalPages <= 1) {
    container.innerHTML = '';
    return;
  }

  const pages = paginationItems(current, totalPages);
  container.innerHTML = [
    '<button class="page-button" data-page="' + (current - 1) + '" ' + (current <= 1 ? 'disabled' : '') + '>上一页</button>',
    pages.map((page) => page === '...'
      ? '<span class="page-ellipsis">...</span>'
      : '<button class="page-button ' + (page === current ? 'active' : '') + '" data-page="' + page + '">' + page + '</button>'
    ).join(''),
    '<button class="page-button" data-page="' + (current + 1) + '" ' + (current >= totalPages ? 'disabled' : '') + '>下一页</button>'
  ].join('');

  container.querySelectorAll('[data-page]:not([disabled])').forEach((button) => {
    button.addEventListener('click', () => onPage(Number(button.dataset.page)));
  });
}

async function loadNamespaces() {
  $('workspaceMeta').textContent = '加载中...';
  const data = await api('/api/v1/namespaces');
  state.namespaces = data.items || [];

  if (!state.selectedNamespace || !state.namespaces.some((ns) => ns.code === state.selectedNamespace)) {
    state.selectedNamespace = state.namespaces[0]?.code || '';
  }

  syncComboSelection();
  applyNamespaceFilter(false);
}

function applyNamespaceFilter(resetPage = true) {
  const query = $('searchInput').value.trim().toLowerCase();

  state.filteredNamespaces = query
    ? state.namespaces.filter((ns) => [ns.code, ns.displayName, ns.description, ns.status]
        .some((value) => String(value || '').toLowerCase().includes(query)))
    : [...state.namespaces];

  if (resetPage) state.namespacePage = 1;

  const totalPages = Math.max(1, Math.ceil(state.filteredNamespaces.length / state.namespacePageSize));
  state.namespacePage = Math.min(state.namespacePage, totalPages);

  renderNamespaceTable();
  renderPagination('namespacePagination', state.namespacePage, totalPages, (page) => {
    state.namespacePage = page;
    renderNamespaceTable();
    renderNamespaceFooter();
  });
  renderNamespaceFooter();
}

function renderNamespaceFooter() {
  const total = state.filteredNamespaces.length;
  const totalPages = Math.max(1, Math.ceil(total / state.namespacePageSize));
  const usedCount = state.namespaces.reduce((sum, ns) => sum + Number(ns.usedCount || 0), 0);

  $('workspaceMeta').textContent = total
    ? '共 ' + total + ' 个 Namespace · 已登记 ' + usedCount + ' 个类型 · 第 ' + state.namespacePage + ' / ' + totalPages + ' 页'
    : '没有匹配的 Namespace';
}

function renderNamespaceTable() {
  const start = (state.namespacePage - 1) * state.namespacePageSize;
  const items = state.filteredNamespaces.slice(start, start + state.namespacePageSize);

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
          '<tr class="namespace-row" data-namespace="' + esc(ns.code) + '">',
            '<td><span class="namespace-code">' + esc(ns.code) + '</span></td>',
            '<td title="' + esc(ns.description || ns.displayName || '-') + '">' + esc(ns.displayName || '-') + '</td>',
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
    row.addEventListener('click', () => openEntries(row.dataset.namespace));
  });
}

function syncComboSelection() {
  const ns = state.namespaces.find((item) => item.code === state.selectedNamespace);
  $('allocateNamespace').value = ns?.code || '';
  $('allocateNamespaceSearch').value = ns ? namespaceLabel(ns) : '';
}

function filterComboItems() {
  const selected = state.namespaces.find((ns) => ns.code === $('allocateNamespace').value);
  const rawQuery = $('allocateNamespaceSearch').value.trim();
  const query = selected && rawQuery === namespaceLabel(selected) ? '' : rawQuery.toLowerCase();

  state.comboItems = state.namespaces.filter((ns) => {
    if (!query) return true;
    return ns.code.toLowerCase().includes(query)
      || String(ns.displayName || '').toLowerCase().includes(query);
  });
  state.comboActiveIndex = state.comboItems.length ? 0 : -1;
}

function openCombo() {
  filterComboItems();
  renderComboOptions();
  $('namespaceOptions').hidden = false;
  $('allocateNamespaceSearch').setAttribute('aria-expanded', 'true');
}

function closeCombo() {
  $('namespaceOptions').hidden = true;
  $('allocateNamespaceSearch').setAttribute('aria-expanded', 'false');
  $('allocateNamespaceSearch').removeAttribute('aria-activedescendant');
  state.comboActiveIndex = -1;
}

function renderComboOptions() {
  const container = $('namespaceOptions');

  if (!state.comboItems.length) {
    container.innerHTML = '<div class="combo-empty">没有匹配的 Namespace</div>';
    return;
  }

  container.innerHTML = state.comboItems.map((ns, index) => [
    '<div',
      ' id="namespace-option-' + index + '"',
      ' class="combo-option ' + (index === state.comboActiveIndex ? 'active' : '') + '"',
      ' role="option"',
      ' aria-selected="' + (ns.code === state.selectedNamespace ? 'true' : 'false') + '"',
      ' data-namespace="' + esc(ns.code) + '">',
      '<span>' + esc(ns.code) + '</span>',
      '<small>（' + esc(ns.displayName || '未命名') + '）</small>',
    '</div>'
  ].join('')).join('');

  if (state.comboActiveIndex >= 0) {
    $('allocateNamespaceSearch').setAttribute('aria-activedescendant', 'namespace-option-' + state.comboActiveIndex);
  }

  container.querySelectorAll('[data-namespace]').forEach((option) => {
    option.addEventListener('mousedown', (event) => {
      event.preventDefault();
      selectNamespace(option.dataset.namespace);
    });
  });
}

function moveComboActive(direction) {
  if ($('namespaceOptions').hidden) openCombo();
  if (!state.comboItems.length) return;

  state.comboActiveIndex = (state.comboActiveIndex + direction + state.comboItems.length) % state.comboItems.length;
  renderComboOptions();
  document.getElementById('namespace-option-' + state.comboActiveIndex)?.scrollIntoView({ block: 'nearest' });
}

function selectNamespace(code) {
  const ns = state.namespaces.find((item) => item.code === code);
  if (!ns) return;

  state.selectedNamespace = code;
  $('allocateNamespace').value = code;
  $('allocateNamespaceSearch').value = namespaceLabel(ns);
  closeCombo();
}

async function openEntries(code) {
  const ns = state.namespaces.find((item) => item.code === code);
  if (!ns) return;

  state.selectedNamespace = code;
  syncComboSelection();

  state.entries.namespace = code;
  state.entries.page = 1;
  state.entries.query = '';
  $('entrySearchInput').value = '';
  $('entryDialogTitle').textContent = namespaceLabel(ns);
  $('entryDialogMeta').textContent = [
    '当前最大 ' + valueOrDash(ns.currentMax),
    '下一可用 ' + valueOrDash(ns.nextValue),
    '已使用 ' + Number(ns.usedCount || 0)
  ].join(' · ');

  const dialog = $('entryDialog');
  if (!dialog.open) dialog.showModal();
  await loadEntries();
}

async function loadEntries() {
  const params = new URLSearchParams({
    namespace: state.entries.namespace,
    page: String(state.entries.page),
    pageSize: String(state.entries.pageSize)
  });

  if (state.entries.query) params.set('q', state.entries.query);

  $('entryCountMeta').textContent = '加载中...';
  $('entryTable').innerHTML = '<div class="empty">加载中...</div>';

  const data = await api('/api/v1/types/search?' + params.toString());
  state.entries.page = data.page || state.entries.page;
  state.entries.total = data.total || 0;
  state.entries.totalPages = data.totalPages || 0;

  renderEntryTable(data.items || []);
  renderPagination('entryPagination', state.entries.page, state.entries.totalPages, async (page) => {
    state.entries.page = page;
    await loadEntries();
  });

  $('entryCountMeta').textContent = state.entries.total
    ? '共 ' + state.entries.total + ' 条 · 第 ' + state.entries.page + ' / ' + state.entries.totalPages + ' 页'
    : '共 0 条';
}

function renderEntryTable(items) {
  if (!items.length) {
    $('entryTable').innerHTML = '<div class="empty">该 Namespace 下暂无匹配 entries</div>';
    return;
  }

  $('entryTable').innerHTML = [
    '<table>',
      '<thead><tr>',
        '<th class="entry-value">值</th>',
        '<th class="entry-symbol">常量名</th>',
        '<th class="entry-project">项目</th>',
        '<th class="entry-description">描述</th>',
        '<th class="entry-requirement">需求/工单</th>',
        '<th class="entry-requester">申请人</th>',
        '<th class="entry-status">状态</th>',
      '</tr></thead>',
      '<tbody>',
        items.map((item) => [
          '<tr>',
            '<td><strong>' + esc(item.value) + '</strong></td>',
            '<td title="' + esc(item.symbol || '-') + '">' + esc(item.symbol || '-') + '</td>',
            '<td title="' + esc(item.project || '-') + '">' + esc(item.project || '-') + '</td>',
            '<td title="' + esc(item.description || '-') + '">' + esc(item.description || '-') + '</td>',
            '<td title="' + esc(item.requirement || '-') + '">' + esc(item.requirement || '-') + '</td>',
            '<td title="' + esc(item.requester || '-') + '">' + esc(item.requester || '-') + '</td>',
            '<td><span class="status-pill ' + (item.status === 'ACTIVE' ? 'active' : 'inactive') + '">' + esc(item.status || '-') + '</span></td>',
          '</tr>'
        ].join('')).join(''),
      '</tbody>',
    '</table>'
  ].join('');
}

function showMainError(error) {
  $('workspaceMeta').textContent = '加载失败';
  $('namespaceTable').innerHTML = '<div class="error">' + esc(error.message) + '</div>';
}

$('allocateForm').addEventListener('submit', async (event) => {
  event.preventDefault();

  const namespace = $('allocateNamespace').value;
  if (!namespace) {
    $('allocateResult').textContent = '请先从下拉列表选择 Namespace';
    $('allocateResult').className = 'form-result error-text';
    return;
  }

  $('allocateResult').textContent = '正在申请...';
  $('allocateResult').className = 'form-result muted';

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

    $('allocateResult').textContent = '申请成功 · ' + data.namespace + ' = ' + data.value
      + (data.symbol ? ' · ' + data.symbol : '');
    $('allocateResult').className = 'form-result success-text';

    state.selectedNamespace = data.namespace;
    await loadNamespaces();
  } catch (error) {
    $('allocateResult').textContent = error.message;
    $('allocateResult').className = 'form-result error-text';
  }
});

$('searchInput').addEventListener('input', () => applyNamespaceFilter(true));
$('clearSearchButton').addEventListener('click', () => {
  $('searchInput').value = '';
  applyNamespaceFilter(true);
});
$('refreshButton').addEventListener('click', () => loadNamespaces().catch(showMainError));

$('namespacePageSize').addEventListener('change', (event) => {
  state.namespacePageSize = Number(event.target.value);
  state.namespacePage = 1;
  applyNamespaceFilter(false);
});

$('allocateNamespaceSearch').addEventListener('focus', () => {
  $('allocateNamespaceSearch').select();
  openCombo();
});

$('allocateNamespaceSearch').addEventListener('input', () => {
  $('allocateNamespace').value = '';
  state.selectedNamespace = '';
  openCombo();
});

$('allocateNamespaceSearch').addEventListener('keydown', (event) => {
  if (event.key === 'ArrowDown') {
    event.preventDefault();
    moveComboActive(1);
  } else if (event.key === 'ArrowUp') {
    event.preventDefault();
    moveComboActive(-1);
  } else if (event.key === 'Enter' && state.comboActiveIndex >= 0) {
    event.preventDefault();
    selectNamespace(state.comboItems[state.comboActiveIndex].code);
  } else if (event.key === 'Escape') {
    closeCombo();
  }
});

document.addEventListener('mousedown', (event) => {
  if (!$('namespaceCombo').contains(event.target)) closeCombo();
});

$('closeEntryDialog').addEventListener('click', () => $('entryDialog').close());

$('entryDialog').addEventListener('click', (event) => {
  if (event.target === $('entryDialog')) $('entryDialog').close();
});

let entrySearchTimer;

$('entrySearchInput').addEventListener('input', () => {
  clearTimeout(entrySearchTimer);
  entrySearchTimer = setTimeout(() => {
    state.entries.query = $('entrySearchInput').value.trim();
    state.entries.page = 1;
    loadEntries().catch((error) => {
      $('entryTable').innerHTML = '<div class="error">' + esc(error.message) + '</div>';
    });
  }, 250);
});

$('entryPageSize').addEventListener('change', (event) => {
  state.entries.pageSize = Number(event.target.value);
  state.entries.page = 1;
  loadEntries().catch((error) => {
    $('entryTable').innerHTML = '<div class="error">' + esc(error.message) + '</div>';
  });
});

loadNamespaces().catch(showMainError);
