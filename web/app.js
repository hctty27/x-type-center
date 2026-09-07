const state = {
  namespaces: [],
  searchMode: false
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
  renderNamespaces();
  fillNamespaceSelects();

  if (!state.searchMode) {
    $('workspaceMeta').textContent = state.namespaces.length + ' 个 Namespace · 点击卡片查看详情';
  }
}

function renderNamespaces() {
  if (!state.namespaces.length) {
    $('namespaceTable').innerHTML = '<div class="empty">暂无数据，请先导入 Excel。</div>';
    return;
  }

  $('namespaceTable').innerHTML = state.namespaces.map((ns) => [
    '<article class="namespace-card" data-namespace="' + esc(ns.code) + '">',
      '<div class="namespace-head">',
        '<div class="namespace-code">' + esc(ns.code) + '</div>',
        '<div class="namespace-name">' + esc(ns.displayName || '') + '</div>',
      '</div>',
      '<div class="namespace-stats">',
        '<div class="namespace-stat"><span>当前最大</span><strong>' + esc(ns.currentMax ?? '-') + '</strong></div>',
        '<div class="namespace-stat"><span>下一值</span><strong>' + esc(ns.nextValue ?? '-') + '</strong></div>',
        '<div class="namespace-stat"><span>已使用</span><strong>' + esc(ns.usedCount ?? 0) + '</strong></div>',
      '</div>',
    '</article>'
  ].join('')).join('');

  bindNamespaceClicks($('namespaceTable'));
}

function bindNamespaceClicks(root) {
  root.querySelectorAll('[data-namespace]').forEach((element) => {
    element.addEventListener('click', () => showNamespace(element.dataset.namespace));
  });
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

async function search() {
  const query = $('searchInput').value.trim();
  const namespace = $('namespaceFilter').value;

  if (!query && !namespace) {
    clearSearch();
    return;
  }

  const params = new URLSearchParams();
  if (query) params.set('q', query);
  if (namespace) params.set('namespace', namespace);
  params.set('limit', '10');

  const data = await api('/api/v1/types/search?' + params.toString());
  const items = data.items || [];

  state.searchMode = true;
  $('namespaceTable').classList.add('hidden');
  $('searchResult').classList.remove('hidden');
  $('workspaceTitle').textContent = '搜索结果';
  $('workspaceMeta').textContent = items.length + ' 条结果 · 单屏最多展示 10 条';

  if (!items.length) {
    $('searchResult').innerHTML = '<div class="empty">没有匹配结果</div>';
    return;
  }

  $('searchResult').innerHTML = items.map((item) => [
    '<article class="search-card" data-namespace="' + esc(item.namespace) + '">',
      '<div class="search-card-head">',
        '<span class="search-value">' + esc(item.value) + '</span>',
        '<span class="search-namespace">' + esc(item.namespace) + '</span>',
      '</div>',
      '<div class="search-symbol">' + esc(item.symbol || '-') + '</div>',
      '<div class="search-description">' + esc(item.description || '-') + '</div>',
      '<div class="search-meta">',
        '<span>项目：' + esc(item.project || '-') + '</span>',
        '<span>来源：' + esc(item.sourceRef || item.source || '-') + '</span>',
      '</div>',
    '</article>'
  ].join('')).join('');

  bindNamespaceClicks($('searchResult'));
}

function clearSearch() {
  state.searchMode = false;
  $('searchInput').value = '';
  $('namespaceFilter').value = '';
  $('searchResult').classList.add('hidden');
  $('namespaceTable').classList.remove('hidden');
  $('workspaceTitle').textContent = 'Namespace 总览';
  $('workspaceMeta').textContent = state.namespaces.length + ' 个 Namespace · 点击卡片查看详情';
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
    await showNamespace(data.namespace);
  } catch (error) {
    $('allocateResult').textContent = error.message;
  }
});

$('searchButton').addEventListener('click', () => search().catch((error) => alert(error.message)));
$('clearSearchButton').addEventListener('click', clearSearch);

$('searchInput').addEventListener('keydown', (event) => {
  if (event.key === 'Enter') search().catch((error) => alert(error.message));
});

$('namespaceFilter').addEventListener('change', () => {
  if ($('namespaceFilter').value || $('searchInput').value.trim()) {
    search().catch((error) => alert(error.message));
  } else {
    clearSearch();
  }
});

$('refreshButton').addEventListener('click', () => {
  loadNamespaces().catch((error) => alert(error.message));
});

$('tokenButton').addEventListener('click', () => {
  const value = prompt('API Token（仅保存在当前浏览器会话）', token());
  if (value !== null) sessionStorage.setItem('typeRegistryToken', value.trim());
});

loadNamespaces().catch((error) => {
  $('namespaceTable').innerHTML = '<div class="error">' + esc(error.message) + '</div>';
});
