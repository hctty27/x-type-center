const state = {
  namespaces: [],
  filteredNamespaces: [],
  namespacePage: 1,
  namespacePageSize: 20,
  selectedNamespace: '',
  comboItems: [],
  comboActiveIndex: -1,
  projects: [],
  projectComboItems: [],
  projectComboActiveIndex: -1,
  namespaceEditor: {
    mode: 'create',
    code: ''
  },
  revoke: {
    mode: '',
    entryId: 0,
    allocationId: '',
    requester: ''
  },
  entries: {
    namespace: '',
    page: 1,
    pageSize: 20,
    total: 0,
    totalPages: 0,
    query: '',
    aliases: []
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

async function loadVersionInfo() {
  const badge = $('versionBadge');
  try {
    const data = await api('/api/v1/version', { cache: 'no-store' });
    const serverVersion = data.server?.version || 'unknown';
    const skillVersion = data.skill?.latestVersion || 'unknown';
    const packageVersion = data.skill?.packageVersion || '未配置';
    const packageReady = data.skill?.packageReady === true;

    badge.textContent = '系统 ' + serverVersion + ' · Skill v' + skillVersion;
    badge.classList.toggle('warning', !packageReady);
    badge.title = [
      '系统版本：' + serverVersion,
      'Commit：' + (data.server?.commit || 'unknown'),
      '构建时间：' + (data.server?.buildTime || 'unknown'),
      'Skill 最新：' + skillVersion,
      '最低支持：' + (data.skill?.minSupportedVersion || 'unknown'),
      '下载包版本：' + packageVersion,
      packageReady ? '下载包已同步' : '下载包未同步，请更新服务端 Skill ZIP'
    ].join('\n');
  } catch (_) {
    badge.textContent = '版本未知';
    badge.classList.add('warning');
    badge.title = '版本信息加载失败';
  }
}

async function loadSkillPackageAvailability() {
  const button = $('skillDownloadButton');

  try {
    const response = await fetch('/api/v1/skill-package', {
      method: 'HEAD',
      cache: 'no-store'
    });

    if (!response.ok) throw new Error('skill package unavailable');

    button.href = '/api/v1/skill-package';
    button.setAttribute('download', '');
    button.classList.remove('disabled');
    button.removeAttribute('aria-disabled');
    button.title = '下载 Type Registry 技能包';
  } catch (_) {
    button.removeAttribute('href');
    button.removeAttribute('download');
    button.classList.add('disabled');
    button.setAttribute('aria-disabled', 'true');
    button.title = '技能包未配置或文件不存在';
  }
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

function namespaceAliases(ns) {
  return Array.isArray(ns.aliases) ? ns.aliases : [];
}

function aliasSummary(ns) {
  const aliases = namespaceAliases(ns);
  return aliases.length ? aliases.join('、') : '-';
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

  if (state.selectedNamespace && !state.namespaces.some((ns) => ns.code === state.selectedNamespace && ns.status === 'ACTIVE')) {
    state.selectedNamespace = '';
  }

  syncComboSelection();
  applyNamespaceFilter(false);
}

async function loadProjects() {
  const data = await api('/api/v1/projects');
  state.projects = data.items || [];

  if (!$('projectOptions').hidden) {
    openProjectCombo();
  }
}


function openCreateNamespace() {
  state.namespaceEditor = { mode: 'create', code: '' };
  $('namespaceDialogTitle').textContent = '新增 Namespace';
  $('namespaceDialogMeta').textContent = 'Code 创建后不可直接修改。';
  $('namespaceCode').readOnly = false;
  $('namespaceCode').value = '';
  $('namespaceDisplayName').value = '';
  $('namespaceDescription').value = '';
  $('namespaceStartValue').value = '1';
  $('namespaceMinValue').value = '';
  $('namespaceMaxValue').value = '';
  $('namespaceNextValue').value = '';
  $('namespaceStatus').value = 'ACTIVE';
  $('namespaceStartField').hidden = false;
  $('namespaceNextField').hidden = true;
  $('namespaceStatusField').hidden = true;
  $('namespaceMessage').textContent = '';
  $('saveNamespaceButton').textContent = '创建';
  $('saveNamespaceButton').disabled = false;

  const dialog = $('namespaceDialog');
  if (!dialog.open) dialog.showModal();
  $('namespaceCode').focus();
}

function openEditNamespace(code) {
  const ns = state.namespaces.find((item) => item.code === code);
  if (!ns) return;

  state.namespaceEditor = { mode: 'edit', code: ns.code };
  $('namespaceDialogTitle').textContent = '编辑 Namespace';
  $('namespaceDialogMeta').textContent = 'Code 为稳定标识，不支持直接修改。';
  $('namespaceCode').readOnly = true;
  $('namespaceCode').value = ns.code;
  $('namespaceDisplayName').value = ns.displayName || '';
  $('namespaceDescription').value = ns.description || '';
  $('namespaceStartValue').value = '';
  $('namespaceMinValue').value = ns.minValue == null ? '' : String(ns.minValue);
  $('namespaceMaxValue').value = ns.maxValue == null ? '' : String(ns.maxValue);
  $('namespaceNextValue').value = ns.nextValue == null ? '' : String(ns.nextValue);
  $('namespaceStatus').value = ns.status === 'DEPRECATED' ? 'DEPRECATED' : 'ACTIVE';
  $('namespaceStartField').hidden = true;
  $('namespaceNextField').hidden = false;
  $('namespaceStatusField').hidden = false;
  $('namespaceMessage').textContent = '';
  $('saveNamespaceButton').textContent = '保存';
  $('saveNamespaceButton').disabled = false;

  const dialog = $('namespaceDialog');
  if (!dialog.open) dialog.showModal();
  $('namespaceDisplayName').focus();
}

function closeNamespaceDialog() {
  if ($('namespaceDialog').open) {
    $('namespaceDialog').close();
  }
  state.namespaceEditor = { mode: 'create', code: '' };
}

function namespaceInteger(id, label, required = false) {
  const raw = $(id).value.trim();
  if (!raw) {
    if (required) throw new Error(label + '不能为空');
    return null;
  }

  const value = Number(raw);
  if (!Number.isSafeInteger(value)) {
    throw new Error(label + '必须是安全整数');
  }
  return value;
}

function applyNamespaceFilter(resetPage = true) {
  const query = $('searchInput').value.trim().toLowerCase();

  state.filteredNamespaces = query
    ? state.namespaces.filter((ns) => [ns.code, ns.displayName, ns.description, ns.status, ...namespaceAliases(ns)]
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
        '<th class="col-alias">别名</th>',
        '<th class="col-number">当前最大</th>',
        '<th class="col-number">下一可用</th>',
        '<th class="col-number">已使用</th>',
        '<th class="col-range">值范围</th>',
        '<th class="col-status">状态</th>',
        '<th class="col-action">操作</th>',
      '</tr></thead>',
      '<tbody>',
        items.map((ns) => [
          '<tr class="namespace-row">',
            '<td class="namespace-open-cell" data-open-namespace="' + esc(ns.code) + '"><span class="namespace-code">' + esc(ns.code) + '</span></td>',
            '<td title="' + esc(ns.description || ns.displayName || '-') + '">' + esc(ns.displayName || '-') + '</td>',
            '<td title="' + esc(aliasSummary(ns)) + '">' + esc(aliasSummary(ns)) + '</td>',
            '<td><strong>' + esc(valueOrDash(ns.currentMax)) + '</strong></td>',
            '<td><span class="next-value">' + esc(valueOrDash(ns.nextValue)) + '</span></td>',
            '<td>' + esc(ns.usedCount || 0) + '</td>',
            '<td>' + esc(formatRange(ns)) + '</td>',
            '<td><span class="status-pill ' + (ns.status === 'ACTIVE' ? 'active' : 'inactive') + '">' + esc(ns.status || '-') + '</span></td>',
            '<td><button class="namespace-edit-button" type="button" data-edit-namespace="' + esc(ns.code) + '">编辑</button></td>',
          '</tr>'
        ].join('')).join(''),
      '</tbody>',
    '</table>'
  ].join('');

  $('namespaceTable').querySelectorAll('[data-open-namespace]').forEach((cell) => {
    cell.addEventListener('click', () => openEntries(cell.dataset.openNamespace));
  });

  $('namespaceTable').querySelectorAll('[data-edit-namespace]').forEach((button) => {
    button.addEventListener('click', () => {
      openEditNamespace(button.dataset.editNamespace);
    });
  });
}

function syncComboSelection() {
  const ns = state.namespaces.find((item) => item.code === state.selectedNamespace && item.status === 'ACTIVE');
  $('allocateNamespace').value = ns?.code || '';
  $('allocateNamespaceSearch').value = ns ? namespaceLabel(ns) : '';
}

function filterComboItems() {
  const selected = state.namespaces.find((ns) => ns.code === $('allocateNamespace').value);
  const rawQuery = $('allocateNamespaceSearch').value.trim();
  const query = selected && rawQuery === namespaceLabel(selected) ? '' : rawQuery.toLowerCase();

  state.comboItems = state.namespaces.filter((ns) => {
    if (ns.status !== 'ACTIVE') return false;
    if (!query) return true;
    return ns.code.toLowerCase().includes(query)
      || String(ns.displayName || '').toLowerCase().includes(query)
      || namespaceAliases(ns).some((alias) => String(alias).toLowerCase().includes(query));
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

function filterProjectComboItems() {
  const query = $('project').value.trim().toLowerCase();

  state.projectComboItems = state.projects.filter((item) => {
    if (!query) return true;
    return String(item.name || '').toLowerCase().includes(query);
  });
  state.projectComboActiveIndex = -1;
}

function openProjectCombo() {
  filterProjectComboItems();
  renderProjectComboOptions();
  $('projectOptions').hidden = false;
  $('project').setAttribute('aria-expanded', 'true');
}

function closeProjectCombo() {
  $('projectOptions').hidden = true;
  $('project').setAttribute('aria-expanded', 'false');
  $('project').removeAttribute('aria-activedescendant');
  state.projectComboActiveIndex = -1;
}

function renderProjectComboOptions() {
  const container = $('projectOptions');
  const typedProject = $('project').value.trim();

  if (!state.projectComboItems.length) {
    container.innerHTML = typedProject
      ? '<div class="combo-empty">未找到，申请成功后将自动新增「' + esc(typedProject) + '」</div>'
      : '<div class="combo-empty">暂无项目</div>';
    return;
  }

  container.innerHTML = state.projectComboItems.map((item, index) => [
    '<div',
      ' id="project-option-' + index + '"',
      ' class="combo-option ' + (index === state.projectComboActiveIndex ? 'active' : '') + '"',
      ' role="option"',
      ' aria-selected="' + (item.name === typedProject ? 'true' : 'false') + '"',
      ' data-project="' + esc(item.name) + '">',
      '<span>' + esc(item.name) + '</span>',
    '</div>'
  ].join('')).join('');

  if (state.projectComboActiveIndex >= 0) {
    $('project').setAttribute('aria-activedescendant', 'project-option-' + state.projectComboActiveIndex);
  } else {
    $('project').removeAttribute('aria-activedescendant');
  }

  container.querySelectorAll('[data-project]').forEach((option) => {
    option.addEventListener('mousedown', (event) => {
      event.preventDefault();
      selectProject(option.dataset.project);
    });
  });
}

function moveProjectComboActive(direction) {
  if ($('projectOptions').hidden) {
    openProjectCombo();
  }
  if (!state.projectComboItems.length) return;

  if (state.projectComboActiveIndex < 0) {
    state.projectComboActiveIndex = direction > 0 ? 0 : state.projectComboItems.length - 1;
  } else {
    state.projectComboActiveIndex = (
      state.projectComboActiveIndex + direction + state.projectComboItems.length
    ) % state.projectComboItems.length;
  }

  renderProjectComboOptions();
  document.getElementById('project-option-' + state.projectComboActiveIndex)?.scrollIntoView({ block: 'nearest' });
}

function selectProject(name) {
  $('project').value = name;
  closeProjectCombo();
}


async function openEntries(code) {
  const ns = state.namespaces.find((item) => item.code === code);
  if (!ns) return;

  state.selectedNamespace = ns.status === 'ACTIVE' ? code : '';
  syncComboSelection();

  state.entries.namespace = code;
  state.entries.page = 1;
  state.entries.query = '';
  state.entries.aliases = [];
  $('entrySearchInput').value = '';
  $('aliasInput').value = '';
  $('aliasMessage').textContent = '';
  $('entryDialogTitle').textContent = namespaceLabel(ns);
  $('entryDialogMeta').textContent = [
    '当前最大 ' + valueOrDash(ns.currentMax),
    '下一可用 ' + valueOrDash(ns.nextValue),
    '已使用 ' + Number(ns.usedCount || 0)
  ].join(' · ');

  const dialog = $('entryDialog');
  if (!dialog.open) dialog.showModal();
  await Promise.all([loadAliases(), loadEntries()]);
}

async function loadAliases() {
  if (!state.entries.namespace) return;

  const data = await api('/api/v1/namespaces/' + encodeURIComponent(state.entries.namespace) + '/aliases');
  state.entries.aliases = data.items || [];
  renderNamespaceAliases();
}

function renderNamespaceAliases() {
  const container = $('aliasChips');
  const items = state.entries.aliases || [];

  if (!items.length) {
    container.innerHTML = '<span class="alias-empty">暂无别名</span>';
    return;
  }

  container.innerHTML = items.map((item) => [
    '<span class="alias-chip">',
      '<span>' + esc(item.alias) + '</span>',
      '<button type="button" data-alias-id="' + esc(item.id) + '" aria-label="删除别名 ' + esc(item.alias) + '">×</button>',
    '</span>'
  ].join('')).join('');

  container.querySelectorAll('[data-alias-id]').forEach((button) => {
    button.addEventListener('click', async () => {
      $('aliasMessage').textContent = '';
      try {
        await api(
          '/api/v1/namespaces/' + encodeURIComponent(state.entries.namespace) + '/aliases/' + encodeURIComponent(button.dataset.aliasId),
          { method: 'DELETE' }
        );
        await loadAliases();
        await loadNamespaces();
      } catch (error) {
        $('aliasMessage').textContent = error.message;
      }
    });
  });
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

function entryStatusClass(status) {
  if (status === 'ACTIVE') return 'active';
  if (status === 'REVOKED') return 'revoked';
  return 'inactive';
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
        '<th class="entry-action">操作</th>',
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
            '<td title="' + esc(item.revokeReason || '') + '"><span class="status-pill ' + entryStatusClass(item.status) + '">' + esc(item.status || '-') + '</span></td>',
            '<td>' + (item.status === 'ACTIVE'
              ? '<button class="entry-revoke-button" type="button" data-revoke-entry="' + esc(item.id) + '">撤回</button>'
              : '<span class="entry-action-empty">-</span>') + '</td>',
          '</tr>'
        ].join('')).join(''),
      '</tbody>',
    '</table>'
  ].join('');

  $('entryTable').querySelectorAll('[data-revoke-entry]').forEach((button) => {
    button.addEventListener('click', () => {
      const item = items.find((entry) => String(entry.id) === button.dataset.revokeEntry);
      if (!item) return;
      openRevokeDialog({
        mode: 'entry',
        entryId: item.id,
        requester: item.requester || '',
        title: '撤回类型',
        meta: [
          item.namespace + ' · ' + item.value,
          item.project || '未填写项目',
          item.symbol || '未填写常量名'
        ].join(' · ')
      });
    });
  });
}

function renderAllocationLoading(count) {
  $('allocateResult').className = 'allocation-feedback is-loading';
  $('allocateResult').innerHTML = [
    '<div class="feedback-card loading">',
      '<div class="feedback-icon">…</div>',
      '<div>',
        '<strong>正在申请</strong>',
        '<small>准备分配 ' + count + ' 个可用值</small>',
      '</div>',
    '</div>'
  ].join('');
}

function renderAllocationSuccess(data) {
  const values = Array.isArray(data.values) ? data.values : [];
  const rangeText = values.length <= 1
    ? String(values[0] ?? '-')
    : values[0] + ' ~ ' + values[values.length - 1];

  $('allocateResult').className = 'allocation-feedback is-success';
  $('allocateResult').innerHTML = [
    '<div class="feedback-card success">',
      '<div class="feedback-head">',
        '<div class="feedback-icon">✓</div>',
        '<div>',
          '<strong>申请成功</strong>',
          '<small>' + esc(data.namespace) + ' · ' + esc(data.count) + ' 个</small>',
        '</div>',
      '</div>',
      '<div class="feedback-range">',
        '<span>分配结果</span>',
        '<strong>' + esc(rangeText) + '</strong>',
      '</div>',
      '<div class="feedback-values">',
        values.map((value) => '<code>' + esc(value) + '</code>').join(''),
      '</div>',
      data.allocationId
        ? '<button class="feedback-revoke-button" type="button" data-revoke-allocation="' + esc(data.allocationId) + '">撤回本次申请</button>'
        : '',
    '</div>'
  ].join('');

  const revokeButton = $('allocateResult').querySelector('[data-revoke-allocation]');
  if (revokeButton) {
    revokeButton.addEventListener('click', () => {
      openRevokeDialog({
        mode: 'allocation',
        allocationId: data.allocationId,
        requester: data.items?.[0]?.requester || $('requester').value.trim(),
        title: '撤回本次申请',
        meta: data.namespace + ' · ' + data.count + ' 个 · ' + rangeText
      });
    });
  }
}

function renderAllocationRevoked(data) {
  $('allocateResult').className = 'allocation-feedback is-revoked';
  $('allocateResult').innerHTML = [
    '<div class="feedback-card revoked">',
      '<div class="feedback-head">',
        '<div class="feedback-icon">↶</div>',
        '<div>',
          '<strong>已撤回</strong>',
          '<small>' + esc(data.count || 0) + ' 个类型已标记为 REVOKED</small>',
        '</div>',
      '</div>',
      '<div class="feedback-revoke-note">这些类型值已释放，后续申请可重新分配。</div>',
    '</div>'
  ].join('');
}

function openRevokeDialog(target) {
  state.revoke = {
    mode: target.mode || '',
    entryId: Number(target.entryId || 0),
    allocationId: target.allocationId || '',
    requester: target.requester || ''
  };

  $('revokeDialogTitle').textContent = target.title || '撤回类型';
  $('revokeDialogMeta').textContent = target.meta || '';
  $('revokeRequester').value = target.requester || '';
  $('revokeReason').value = '';
  $('revokeMessage').textContent = '';
  $('confirmRevokeButton').disabled = false;

  const dialog = $('revokeDialog');
  if (!dialog.open) dialog.showModal();
  $('revokeReason').focus();
}

function closeRevokeDialog() {
  if ($('revokeDialog').open) {
    $('revokeDialog').close();
  }
  state.revoke = {
    mode: '',
    entryId: 0,
    allocationId: '',
    requester: ''
  };
}

function renderAllocationError(message) {
  $('allocateResult').className = 'allocation-feedback is-error';
  $('allocateResult').innerHTML = [
    '<div class="feedback-card failure">',
      '<div class="feedback-icon">!</div>',
      '<div>',
        '<strong>申请失败</strong>',
        '<small>' + esc(message) + '</small>',
      '</div>',
    '</div>'
  ].join('');
}

function syncBulkSymbolState() {
  const count = Number($('allocateCount').value);
  const bulk = Number.isInteger(count) && count > 1;
  $('symbol').disabled = bulk;
  $('symbolHint').textContent = bulk ? '批量时不使用' : '可选';
}

function showMainError(error) {
  $('workspaceMeta').textContent = '加载失败';
  $('namespaceTable').innerHTML = '<div class="error">' + esc(error.message) + '</div>';
}

$('allocateForm').addEventListener('submit', async (event) => {
  event.preventDefault();

  const namespace = $('allocateNamespace').value;
  const count = Number($('allocateCount').value);
  const project = $('project').value.trim();

  if (!namespace) {
    renderAllocationError('请先从下拉列表选择 Namespace');
    return;
  }
  if (!Number.isInteger(count) || count < 1 || count > 100) {
    renderAllocationError('申请数量必须是 1 到 100 的整数');
    return;
  }

  renderAllocationLoading(count);

  try {
    const data = await api('/api/v1/types/allocate-batch', {
      method: 'POST',
      body: JSON.stringify({
        namespace,
        count,
        project,
        symbol: count === 1 ? $('symbol').value.trim() : '',
        description: $('description').value.trim(),
        requirement: $('requirement').value.trim(),
        requester: $('requester').value.trim()
      })
    });

    renderAllocationSuccess(data);
    state.selectedNamespace = data.namespace;
    $('project').value = data.items?.[0]?.project || project;
    await Promise.all([loadNamespaces(), loadProjects()]);
  } catch (error) {
    renderAllocationError(error.message);
  }
});

$('searchInput').addEventListener('input', () => applyNamespaceFilter(true));
$('clearSearchButton').addEventListener('click', () => {
  $('searchInput').value = '';
  applyNamespaceFilter(true);
});
$('refreshButton').addEventListener('click', () => loadNamespaces().catch(showMainError));
$('createNamespaceButton').addEventListener('click', openCreateNamespace);
$('closeNamespaceDialog').addEventListener('click', closeNamespaceDialog);
$('cancelNamespaceButton').addEventListener('click', closeNamespaceDialog);

$('namespaceDialog').addEventListener('click', (event) => {
  if (event.target === $('namespaceDialog')) closeNamespaceDialog();
});

$('namespaceForm').addEventListener('submit', async (event) => {
  event.preventDefault();

  const mode = state.namespaceEditor.mode;
  $('namespaceMessage').textContent = '';
  $('saveNamespaceButton').disabled = true;

  try {
    const minValue = namespaceInteger('namespaceMinValue', '最小值');
    const maxValue = namespaceInteger('namespaceMaxValue', '最大值');
    if (minValue != null && maxValue != null && minValue > maxValue) {
      throw new Error('最小值不能大于最大值');
    }

    let data;
    if (mode === 'create') {
      const startValue = namespaceInteger('namespaceStartValue', '起始值', true);
      if (minValue != null && startValue < minValue) {
        throw new Error('起始值不能小于最小值');
      }
      if (maxValue != null && startValue > maxValue) {
        throw new Error('起始值不能大于最大值');
      }

      data = await api('/api/v1/namespaces', {
        method: 'POST',
        body: JSON.stringify({
          code: $('namespaceCode').value.trim(),
          displayName: $('namespaceDisplayName').value.trim(),
          description: $('namespaceDescription').value.trim(),
          startValue,
          minValue,
          maxValue
        })
      });
    } else {
      data = await api('/api/v1/namespaces/' + encodeURIComponent(state.namespaceEditor.code), {
        method: 'PUT',
        body: JSON.stringify({
          displayName: $('namespaceDisplayName').value.trim(),
          description: $('namespaceDescription').value.trim(),
          minValue,
          maxValue,
          status: $('namespaceStatus').value
        })
      });
    }

    closeNamespaceDialog();
    state.selectedNamespace = data.status === 'ACTIVE' ? data.code : '';
    await loadNamespaces();
  } catch (error) {
    $('namespaceMessage').textContent = error.message;
    $('saveNamespaceButton').disabled = false;
  }
});

$('namespacePageSize').addEventListener('change', (event) => {
  state.namespacePageSize = Number(event.target.value);
  state.namespacePage = 1;
  applyNamespaceFilter(false);
});

$('allocateCount').addEventListener('input', syncBulkSymbolState);
syncBulkSymbolState();

const namespaceSearch = $('allocateNamespaceSearch');

namespaceSearch.addEventListener('focus', () => {
  namespaceSearch.select();

  if ($('namespaceOptions').hidden) {
    openCombo();
  }
});

namespaceSearch.addEventListener('click', () => {
  if ($('namespaceOptions').hidden) {
    openCombo();
  }
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

const projectSearch = $('project');

projectSearch.addEventListener('focus', () => {
  projectSearch.select();

  if ($('projectOptions').hidden) {
    openProjectCombo();
  }
});

projectSearch.addEventListener('click', () => {
  if ($('projectOptions').hidden) {
    openProjectCombo();
  }
});

projectSearch.addEventListener('input', () => {
  openProjectCombo();
});

projectSearch.addEventListener('keydown', (event) => {
  if (event.key === 'ArrowDown') {
    event.preventDefault();
    moveProjectComboActive(1);
  } else if (event.key === 'ArrowUp') {
    event.preventDefault();
    moveProjectComboActive(-1);
  } else if (event.key === 'Enter' && state.projectComboActiveIndex >= 0) {
    event.preventDefault();
    selectProject(state.projectComboItems[state.projectComboActiveIndex].name);
  } else if (event.key === 'Escape') {
    closeProjectCombo();
  } else if (event.key === 'Enter') {
    closeProjectCombo();
  }
});

document.addEventListener('mousedown', (event) => {
  if (!$('namespaceCombo').contains(event.target)) closeCombo();
  if (!$('projectCombo').contains(event.target)) closeProjectCombo();
});

$('closeRevokeDialog').addEventListener('click', closeRevokeDialog);
$('cancelRevokeButton').addEventListener('click', closeRevokeDialog);

$('revokeDialog').addEventListener('click', (event) => {
  if (event.target === $('revokeDialog')) closeRevokeDialog();
});

$('revokeForm').addEventListener('submit', async (event) => {
  event.preventDefault();

  const target = { ...state.revoke };
  const reason = $('revokeReason').value.trim();
  const requester = $('revokeRequester').value.trim();

  const path = target.mode === 'allocation'
    ? '/api/v1/allocations/' + encodeURIComponent(target.allocationId) + '/revoke'
    : '/api/v1/types/' + encodeURIComponent(target.entryId) + '/revoke';

  $('revokeMessage').textContent = '正在撤回...';
  $('confirmRevokeButton').disabled = true;

  try {
    const data = await api(path, {
      method: 'POST',
      body: JSON.stringify({ requester, reason })
    });

    closeRevokeDialog();

    if (target.mode === 'allocation') {
      renderAllocationRevoked(data);
    }

    await loadNamespaces();
    if ($('entryDialog').open) {
      await loadEntries();
    }
  } catch (error) {
    $('revokeMessage').textContent = error.message;
    $('confirmRevokeButton').disabled = false;
  }
});

$('aliasForm').addEventListener('submit', async (event) => {
  event.preventDefault();

  const namespace = state.entries.namespace;
  const alias = $('aliasInput').value.trim();
  $('aliasMessage').textContent = '';

  if (!namespace) {
    $('aliasMessage').textContent = '未选择 Namespace';
    return;
  }
  if (!alias) {
    $('aliasMessage').textContent = '请输入别名';
    return;
  }

  try {
    await api('/api/v1/namespaces/' + encodeURIComponent(namespace) + '/aliases', {
      method: 'POST',
      body: JSON.stringify({ alias })
    });
    $('aliasInput').value = '';
    await loadAliases();
    await loadNamespaces();
  } catch (error) {
    $('aliasMessage').textContent = error.message;
  }
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

loadVersionInfo();
loadSkillPackageAvailability();
loadNamespaces().catch(showMainError);
loadProjects().catch((error) => {
  renderAllocationError('项目列表加载失败：' + error.message);
});
