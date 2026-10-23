/* 记事本 — front-end.
 * Plain ES2020, no build step, no dependencies. The backend is a tiny
 * JSON API (see internal/server). Everything the user types is auto-saved. */
(() => {
  'use strict';

  const $ = (id) => document.getElementById(id);

  const els = {
    app: $('app'),
    list: $('note-list'),
    search: $('search'),
    count: $('note-count'),
    newBtn: $('btn-new'),
    newEmptyBtn: $('btn-new-empty'),
    empty: $('empty'),
    pane: $('editor-pane'),
    title: $('title'),
    area: $('area'),
    back: $('btn-back'),
    fontBtn: $('btn-font'),
    renameBtn: $('btn-rename'),
    deleteBtn: $('btn-delete'),
    status: $('save-status'),
    statusText: $('save-text'),
    stats: $('stats'),
    toasts: $('toasts'),
    modal: $('modal'),
    modalTitle: $('modal-title'),
    modalDesc: $('modal-desc'),
    modalInput: $('modal-input'),
    modalOk: $('modal-ok'),
    modalCancel: $('modal-cancel'),
    settingsBtn: $('btn-settings'),
    settings: $('settings'),
    settingsMenu: $('settings-menu'),
    settingsUser: $('settings-user'),
    settingsVersion: $('settings-version'),
    settingsDir: $('settings-dir'),
    settingsPw: $('settings-pw'),
    settingsLogs: $('settings-logs'),
    settingsLogout: $('settings-logout'),
    logs: $('logs'),
    logsList: $('logs-list'),
    logsBack: $('logs-back'),
    pwForm: $('settings-pwform'),
    pwCurrent: $('pw-current'),
    pwNew: $('pw-new'),
    pwConfirm: $('pw-confirm'),
    pwError: $('pw-error'),
    pwSubmit: $('pw-submit'),
    pwCancel: $('pw-cancel'),
    auth: $('auth'),
    authTitle: $('auth-title'),
    authDesc: $('auth-desc'),
    authForm: $('auth-form'),
    authUser: $('auth-user'),
    authPass: $('auth-pass'),
    authPass2: $('auth-pass2'),
    authConfirmField: $('auth-confirm-field'),
    authRemember: $('auth-remember'),
    authRememberField: $('auth-remember-field'),
    authError: $('auth-error'),
    authSubmit: $('auth-submit'),
    authFoot: $('auth-foot'),
  };

  const FONT_SIZES = [14, 15.5, 17, 18.5, 20];
  const REMEMBER_KEY = 'notepad.remember';

  const state = {
    notes: [],
    current: null,        // name of the open note
    savedContent: '',     // last content known to be persisted
    filter: '',
    inFlight: null,       // Promise of the in-progress save, if any
    lastSavedAt: null,
    fontSize: readFontSize(),
    user: '',
    version: '',
    dataDir: '',
    needsSetup: false,
    authMode: 'login',    // 'login' | 'setup'
    locked: true,         // true while the login screen is up
  };

  /* ---- tiny API client ------------------------------------------------- */

  // Requests that legitimately answer 401 without meaning "your session is
  // gone" — the login screen itself uses them.
  const PUBLIC_PATHS = ['api/session', 'api/login', 'api/setup'];

  async function api(method, path, body) {
    const opts = { method, headers: {} };
    if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    let res;
    try {
      res = await fetch(path, opts);
    } catch (err) {
      throw new Error('网络错误，无法连接到服务');
    }
    const text = await res.text();
    let data = null;
    if (text) {
      try { data = JSON.parse(text); } catch { /* non-JSON error page */ }
    }
    if (!res.ok) {
      const err = new Error((data && data.error) || `请求失败 (${res.status})`);
      err.status = res.status;
      err.needsSetup = !!(data && data.needsSetup);
      if (err.status === 401 && !PUBLIC_PATHS.includes(path)) {
        sessionExpired(err.needsSetup);
      }
      throw err;
    }
    return data || {};
  }

  /* ---- helpers --------------------------------------------------------- */

  function escapeHtml(s) {
    return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  }

  // 界面里展示的笔记名不带 .txt 后缀；真实文件名仍由服务端维护。
  function displayName(name) {
    return name.endsWith('.txt') ? name.slice(0, -4) : name;
  }

  function formatTime(ms) {
    const d = new Date(ms);
    const now = new Date();
    const sameDay = d.toDateString() === now.toDateString();
    const pad = (n) => String(n).padStart(2, '0');
    if (sameDay) return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
    if (d.getFullYear() === now.getFullYear()) return `${d.getMonth() + 1}月${d.getDate()}日`;
    return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`;
  }

  function savedLabel() {
    return state.lastSavedAt ? `已保存 · ${formatTime(state.lastSavedAt)}` : '已保存';
  }

  function toast(message, isError) {
    const el = document.createElement('div');
    el.className = 'toast' + (isError ? ' toast--error' : '');
    el.textContent = message;
    els.toasts.appendChild(el);
    setTimeout(() => {
      el.style.transition = 'opacity .2s';
      el.style.opacity = '0';
      setTimeout(() => el.remove(), 200);
    }, 2400);
  }

  function readFontSize() {
    const v = Number(localStorage.getItem('notepad.fontSize'));
    return FONT_SIZES.includes(v) ? v : 15.5;
  }

  function applyFontSize() {
    document.documentElement.style.setProperty('--editor-size', state.fontSize + 'px');
    localStorage.setItem('notepad.fontSize', String(state.fontSize));
  }

  /* ---- 账号与登录（session / login / setup） ---------------------------- */

  function b64encode(text) {
    const bytes = new TextEncoder().encode(text);
    let binary = '';
    for (const b of bytes) binary += String.fromCharCode(b);
    return btoa(binary);
  }

  function b64decode(text) {
    const binary = atob(text);
    const bytes = Uint8Array.from(binary, (c) => c.charCodeAt(0));
    return new TextDecoder().decode(bytes);
  }

  // 「记住账号密码」：账号与密码放在浏览器本地存储里，下次打开自动填好。
  // 只是 base64 混淆，不是加密——浏览器本地存储本来就能被本机使用者读到。
  function loadRemembered() {
    try {
      const raw = localStorage.getItem(REMEMBER_KEY);
      if (!raw) return null;
      const saved = JSON.parse(raw);
      if (!saved || typeof saved.u !== 'string' || !saved.u) return null;
      return { username: saved.u, password: typeof saved.p === 'string' ? b64decode(saved.p) : '' };
    } catch {
      return null;
    }
  }

  function saveRemembered(username, password) {
    try {
      localStorage.setItem(REMEMBER_KEY, JSON.stringify({ u: username, p: b64encode(password) }));
    } catch { /* 隐私模式下写不进去，忽略 */ }
  }

  function clearRemembered() {
    try { localStorage.removeItem(REMEMBER_KEY); } catch { /* 忽略 */ }
  }

  function applyAuthMode(mode) {
    state.authMode = mode;
    const setup = mode === 'setup';
    els.authTitle.textContent = setup ? '创建账号' : '登录';
    els.authDesc.textContent = setup
      ? '首次使用，请先创建一个账号，之后用这个账号登录。'
      : '请输入账号和密码继续使用。';
    els.authSubmit.textContent = setup ? '创建并进入' : '登录';
    els.authFoot.textContent = setup
      ? '密码至少 6 位。账号信息保存在 NAS 的应用数据目录里。'
      : '登录状态会保持到你主动退出，或超过 12 小时没有使用。';
    els.authConfirmField.hidden = !setup;
    els.authPass.setAttribute('autocomplete', setup ? 'new-password' : 'current-password');
  }

  function showAuth(mode) {
    state.locked = true;
    applyAuthMode(mode);
    els.authUser.value = '';
    els.authPass.value = '';
    els.authPass2.value = '';
    els.authRemember.checked = true;

    let saved = null;
    if (mode !== 'setup') {
      saved = loadRemembered();
      if (saved) {
        els.authUser.value = saved.username;
        els.authPass.value = saved.password;
      }
    }

    authError('');
    setAuthBusy(false);
    els.auth.hidden = false;
    document.body.classList.add('is-locked');
    setTimeout(() => {
      if (mode === 'setup' || !saved) els.authUser.focus();
      else els.authSubmit.focus();
    }, 40);
  }

  function hideAuth() {
    state.locked = false;
    els.auth.hidden = true;
    document.body.classList.remove('is-locked');
    els.authPass.value = '';
    els.authPass2.value = '';
    authError('');
  }

  function authError(message) {
    if (!message) {
      els.authError.hidden = true;
      els.authError.textContent = '';
      return;
    }
    els.authError.textContent = message;
    els.authError.hidden = false;
  }

  function setAuthBusy(busy) {
    els.authSubmit.disabled = busy;
    els.authSubmit.textContent = busy
      ? '请稍候…'
      : (state.authMode === 'setup' ? '创建并进入' : '登录');
  }

  async function submitAuth(event) {
    event.preventDefault();
    const setup = state.authMode === 'setup';
    const username = els.authUser.value.trim();
    const password = els.authPass.value;
    const remember = els.authRemember.checked;

    if (!username) return authError(setup ? '请输入用户名' : '请输入账号');
    if (!password) return authError('请输入密码');
    if (setup) {
      if ([...password].length < 6) return authError('密码至少需要 6 位');
      if (password !== els.authPass2.value) return authError('两次输入的密码不一致');
    }

    setAuthBusy(true);
    try {
      const data = await api('POST', setup ? 'api/setup' : 'api/login', { username, password, remember });
      if (remember) saveRemembered(username, password);
      else clearRemembered();
      hideAuth();
      await enterApp({ username: data.username || username });
      toast(setup ? '账号已创建' : '已登录');
    } catch (err) {
      authError(err.message);
      if (err.needsSetup && !setup) applyAuthMode('setup');
    } finally {
      setAuthBusy(false);
    }
  }

  // 会话在服务端失效（重启、过期、超时）时，随时把界面退回登录页。
  function sessionExpired(needsSetup) {
    if (!els.auth.hidden) return;
    closeSettings();
    resetEditor();
    state.notes = [];
    renderList();
    state.needsSetup = !!(needsSetup || state.needsSetup);
    showAuth(state.needsSetup ? 'setup' : 'login');
    toast('登录已过期，请重新登录', true);
  }

  function resetEditor() {
    clearTimeout(saveTimer);
    saveTimer = null;
    state.inFlight = null;
    state.current = null;
    state.savedContent = '';
    els.area.value = '';
    els.title.value = '';
    els.pane.hidden = true;
    els.empty.hidden = false;
    document.body.classList.remove('is-editing');
  }

  async function enterApp(session) {
    state.user = (session && session.username) || '';
    hideAuth();
    els.settingsUser.textContent = state.user || '—';

    try {
      const info = await api('GET', 'api/info');
      state.version = info.version || '';
      state.dataDir = info.dataDir || '';
      document.title = info.name || '记事本';
    } catch (err) {
      if (err.status === 401) return;
    }
    els.settingsVersion.textContent = state.version ? 'v' + state.version : '—';
    els.settingsDir.textContent = state.dataDir || '—';

    try {
      await refreshNotes();
    } catch (err) {
      if (err.status !== 401) toast(err.message, true);
      return;
    }
    if (state.notes.length > 0 && window.innerWidth > 720) {
      openNote(state.notes[0].name);
    }
  }

  /* ---- 设置（修改密码 / 退出登录） ------------------------------------- */

  function openSettings() {
    els.settingsUser.textContent = state.user || '—';
    els.settingsVersion.textContent = state.version ? 'v' + state.version : '—';
    els.settingsDir.textContent = state.dataDir || '—';
    showSettingsMenu();
    els.settings.hidden = false;
  }

  function closeSettings() {
    els.settings.hidden = true;
    els.pwCurrent.value = '';
    els.pwNew.value = '';
    els.pwConfirm.value = '';
  }

  function showSettingsMenu() {
    els.settingsMenu.hidden = false;
    els.pwForm.hidden = true;
    els.pwCurrent.value = '';
    els.pwNew.value = '';
    els.pwConfirm.value = '';
    pwError('');
  }

  function showPasswordForm() {
    els.settingsMenu.hidden = true;
    els.pwForm.hidden = false;
    els.pwCurrent.value = '';
    els.pwNew.value = '';
    els.pwConfirm.value = '';
    pwError('');
    els.pwCurrent.focus();
  }

  function pwError(message) {
    if (!message) {
      els.pwError.hidden = true;
      els.pwError.textContent = '';
      return;
    }
    els.pwError.textContent = message;
    els.pwError.hidden = false;
  }

  async function submitPasswordChange(event) {
    event.preventDefault();
    const current = els.pwCurrent.value;
    const next = els.pwNew.value;

    if (!current) return pwError('请输入当前密码');
    if ([...next].length < 6) return pwError('新密码至少需要 6 位');
    if (next !== els.pwConfirm.value) return pwError('两次输入的新密码不一致');
    if (next === current) return pwError('新密码不能和当前密码相同');

    els.pwSubmit.disabled = true;
    els.pwSubmit.textContent = '保存中…';
    try {
      await api('POST', 'api/password', { current, next });
      const saved = loadRemembered();
      if (saved && saved.username === state.user) saveRemembered(state.user, next);
      closeSettings();
      toast('密码已修改');
    } catch (err) {
      pwError(err.message);
    } finally {
      els.pwSubmit.disabled = false;
      els.pwSubmit.textContent = '保存新密码';
    }
  }

  async function logout() {
    const previous = state.user;
    try {
      await api('POST', 'api/logout');
    } catch { /* 会话本来就失效了也没关系 */ }
    clearRemembered();
    closeSettings();
    resetEditor();
    state.notes = [];
    state.user = '';
    renderList();
    showAuth('login');
    els.authUser.value = previous || '';
    els.authRemember.checked = false;
    // showAuth 内部会按"有没有记住的账号"自己决定焦点，这里排在它后面。
    setTimeout(() => els.authPass.focus(), 60);
    toast('已退出登录');
  }

  /* ---- 操作日志 --------------------------------------------------------- */

  function formatLogTime(ms) {
    const d = new Date(ms);
    const pad = (n) => String(n).padStart(2, '0');
    return `${d.getFullYear()}/${pad(d.getMonth() + 1)}/${pad(d.getDate())} ` +
      `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  }

  function renderLogs(entries) {
    if (!entries || !entries.length) {
      els.logsList.innerHTML = '<div class="log-list__empty">暂无操作记录</div>';
      return;
    }
    els.logsList.innerHTML = entries.map((e) => {
      const user = e.user ? `<span class="log-item__user">${escapeHtml(e.user)}</span>` : '';
      const detail = e.detail ? `<span class="log-item__detail">${escapeHtml(e.detail)}</span>` : '';
      return `<div class="log-item">
        <span class="log-item__time">${formatLogTime(e.time)}</span>
        <span class="log-item__action">${escapeHtml(e.action)}</span>
        ${user}${detail}
      </div>`;
    }).join('');
  }

  async function openLogs() {
    // 设置面板退到后台，日志作为独立模态框弹出。
    els.settings.hidden = true;
    els.logsList.innerHTML = '<div class="log-list__empty">加载中…</div>';
    els.logs.hidden = false;
    try {
      const data = await api('GET', 'api/logs');
      renderLogs(data.logs || []);
    } catch (err) {
      els.logsList.innerHTML = '<div class="log-list__empty">' + escapeHtml(err.message) + '</div>';
    }
  }

  function closeLogs() {
    els.logs.hidden = true;
    // 回到设置面板，方便继续操作。
    els.settings.hidden = false;
  }

  /* ---- note list ------------------------------------------------------- */

  function visibleNotes() {
    const q = state.filter.trim().toLowerCase();
    if (!q) return state.notes;
    return state.notes.filter((n) => n.name.toLowerCase().includes(q));
  }

  function renderList() {
    const items = visibleNotes();
    els.count.textContent = `${state.notes.length} 篇笔记`;

    if (state.notes.length === 0) {
      els.list.innerHTML = '<div class="sidebar__empty">还没有笔记<br>点击右上角「+」开始</div>';
      return;
    }
    if (items.length === 0) {
      els.list.innerHTML = '<div class="sidebar__empty">没有匹配的笔记</div>';
      return;
    }

    els.list.innerHTML = items.map((n) => {
      const active = n.name === state.current ? ' is-active' : '';
      const dirty = n.name === state.current && els.area.value !== state.savedContent
        ? '<span class="note-item__dot"></span>' : '';
      const size = n.size === 0 ? '空' : formatSize(n.size);
      return `<button class="note-item${active}" type="button" data-name="${escapeHtml(n.name)}">
        <span class="note-item__body">
          <span class="note-item__name"><span>${escapeHtml(displayName(n.name))}</span>${dirty}</span>
          <span class="note-item__meta">${formatTime(n.modTime)} · ${size}</span>
        </span>
      </button>`;
    }).join('');
  }

  function formatSize(bytes) {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  }

  function applyNoteMeta(note) {
    const idx = state.notes.findIndex((n) => n.name === note.name);
    if (idx >= 0) state.notes[idx] = note;
    else state.notes.unshift(note);
    state.notes.sort((a, b) => b.modTime - a.modTime);
  }

  async function refreshNotes() {
    const data = await api('GET', 'api/notes');
    state.notes = data.notes || [];
    renderList();
  }

  /* ---- status + stats -------------------------------------------------- */

  function setStatus(kind, text) {
    els.status.className = 'status is-' + kind;
    els.statusText.textContent = text;
  }

  function updateStats() {
    const value = els.area.value;
    const chars = [...value].length;
    const lines = value.length ? value.split('\n').length : 0;
    els.stats.textContent = `${chars} 字 · ${lines} 行`;
  }

  /* ---- saving ---------------------------------------------------------- */

  let saveTimer = null;

  function onInput() {
    if (!state.current) return;
    const dirty = els.area.value !== state.savedContent;
    setStatus(dirty ? 'dirty' : 'saved', dirty ? '未保存' : savedLabel());
    updateStats();
    renderList();
    clearTimeout(saveTimer);
    if (dirty) saveTimer = setTimeout(saveNow, 700);
  }

  async function saveNow() {
    clearTimeout(saveTimer);
    saveTimer = null;

    const name = state.current;
    if (!name) return;

    // Wait for any in-flight save so writes are ordered.
    if (state.inFlight) {
      try { await state.inFlight; } catch { /* handled below */ }
    }

    const content = els.area.value;
    if (content === state.savedContent) {
      setStatus('saved', savedLabel());
      return;
    }

    setStatus('saving', '保存中…');
    const p = (async () => {
      const data = await api('PUT', 'api/notes/' + encodeURIComponent(name), { content });
      return { content, note: data.note };
    })();
    state.inFlight = p;

    try {
      const { content: saved, note } = await p;
      state.lastSavedAt = Date.now();
      if (state.current === name) {
        state.savedContent = saved;
        if (els.area.value === saved) {
          setStatus('saved', savedLabel());
        } else {
          setStatus('dirty', '未保存');
        }
      }
      applyNoteMeta(note);
      renderList();
    } catch (err) {
      setStatus('error', '保存失败');
      toast(err.message, true);
    } finally {
      if (state.inFlight === p) state.inFlight = null;
    }

    // Content changed again while we were saving — schedule another pass.
    if (state.current === name && els.area.value !== state.savedContent) {
      clearTimeout(saveTimer);
      saveTimer = setTimeout(saveNow, 400);
    }
  }

  async function flush() {
    if (saveTimer) await saveNow();
    if (state.inFlight) {
      try { await state.inFlight; } catch { /* ignore */ }
    }
  }

  /* ---- open / switch --------------------------------------------------- */

  async function openNote(name) {
    if (name === state.current) {
      document.body.classList.add('is-editing');
      return;
    }
    await flush();
    let data;
    try {
      data = await api('GET', 'api/notes/' + encodeURIComponent(name));
    } catch (err) {
      toast(err.message, true);
      await refreshNotes();
      return;
    }
    state.current = data.note.name;
    state.savedContent = data.content;
    els.title.value = displayName(data.note.name);
    els.area.value = data.content;
    els.empty.hidden = true;
    els.pane.hidden = false;
    document.body.classList.add('is-editing');
    setStatus('saved', savedLabel());
    updateStats();
    renderList();
    if (window.innerWidth > 720) els.area.focus();
  }

  function closeEditor() {
    document.body.classList.remove('is-editing');
  }

  /* ---- create / rename / delete ---------------------------------------- */

  async function newNote() {
    const name = await ask({
      title: '新建笔记',
      desc: '输入文件名，留空则自动命名。',
      value: '',
      placeholder: '例如：会议记录',
      okText: '创建',
    });
    if (name === null) return;
    const finalName = name.trim() || defaultName();
    try {
      const data = await api('POST', 'api/notes', { name: finalName });
      applyNoteMeta(data.note);
      await openNote(data.note.name);
      await refreshNotes();
    } catch (err) {
      toast(err.message, true);
    }
  }

  function defaultName() {
    const d = new Date();
    const pad = (n) => String(n).padStart(2, '0');
    return `未命名 ${pad(d.getMonth() + 1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}.txt`;
  }

  async function renameCurrent() {
    if (!state.current) return;
    const name = await ask({
      title: '重命名',
      value: displayName(state.current),
      placeholder: '新的文件名',
      okText: '保存',
    });
    if (name === null) return;
    const next = name.trim();
    if (!next) return;
    await flush();
    try {
      const data = await api('POST', 'api/notes/' + encodeURIComponent(state.current) + '/rename', { name: next });
      const old = state.current;
      state.current = data.note.name;
      els.title.value = displayName(data.note.name);
      state.notes = state.notes.filter((n) => n.name !== old);
      applyNoteMeta(data.note);
      renderList();
      toast('已重命名');
    } catch (err) {
      toast(err.message, true);
      els.title.value = displayName(state.current);
    }
  }

  async function deleteCurrent() {
    if (!state.current) return;
    const ok = await ask({
      title: '删除笔记',
      desc: `确定要删除「${displayName(state.current)}」吗？此操作无法撤销。`,
      okText: '删除',
      danger: true,
    });
    if (!ok) return;
    const name = state.current;
    // Stop pending/ongoing saves so a late write can't recreate the file.
    clearTimeout(saveTimer);
    saveTimer = null;
    if (state.inFlight) {
      try { await state.inFlight; } catch { /* ignore */ }
    }
    try {
      await api('DELETE', 'api/notes/' + encodeURIComponent(name));
      state.notes = state.notes.filter((n) => n.name !== name);
      state.current = null;
      state.savedContent = '';
      els.area.value = '';
      els.pane.hidden = true;
      els.empty.hidden = false;
      document.body.classList.remove('is-editing');
      renderList();
      toast('已删除');
    } catch (err) {
      toast(err.message, true);
    }
  }

  async function commitTitle() {
    if (!state.current) return;
    const next = els.title.value.trim();
    if (!next || next === displayName(state.current)) {
      els.title.value = displayName(state.current);
      return;
    }
    try {
      const data = await api('POST', 'api/notes/' + encodeURIComponent(state.current) + '/rename', { name: next });
      const old = state.current;
      state.current = data.note.name;
      els.title.value = displayName(data.note.name);
      state.notes = state.notes.filter((n) => n.name !== old);
      applyNoteMeta(data.note);
      renderList();
    } catch (err) {
      toast(err.message, true);
      els.title.value = displayName(state.current);
    }
  }

  /* ---- modal ----------------------------------------------------------- */

  let modalResolve = null;

  function ask({ title, desc, value, placeholder, okText, danger }) {
    els.modalTitle.textContent = title;
    if (desc) { els.modalDesc.textContent = desc; els.modalDesc.hidden = false; }
    else { els.modalDesc.hidden = true; }

    const needsInput = value !== undefined;
    els.modalInput.hidden = !needsInput;
    els.modalInput.value = needsInput ? value : '';
    els.modalInput.placeholder = placeholder || '';
    els.modalOk.textContent = okText || '确定';
    els.modalOk.className = 'btn ' + (danger ? 'btn--danger' : 'btn--primary');

    els.modal.hidden = false;
    if (needsInput) {
      els.modalInput.focus();
      els.modalInput.select();
    } else {
      els.modalOk.focus();
    }

    return new Promise((resolve) => { modalResolve = resolve; });
  }

  function closeModal(result) {
    els.modal.hidden = true;
    const resolve = modalResolve;
    modalResolve = null;
    if (resolve) resolve(result);
  }

  function modalSubmit() {
    if (!els.modalInput.hidden) closeModal(els.modalInput.value);
    else closeModal(true);
  }

  /* ---- wiring ---------------------------------------------------------- */

  function bind() {
    els.newBtn.addEventListener('click', newNote);
    els.newEmptyBtn.addEventListener('click', newNote);
    els.back.addEventListener('click', closeEditor);
    els.renameBtn.addEventListener('click', renameCurrent);
    els.deleteBtn.addEventListener('click', deleteCurrent);
    els.fontBtn.addEventListener('click', () => {
      const i = FONT_SIZES.indexOf(state.fontSize);
      state.fontSize = FONT_SIZES[(i + 1) % FONT_SIZES.length];
      applyFontSize();
      toast(`字号 ${state.fontSize}px`);
    });

    els.list.addEventListener('click', (e) => {
      const item = e.target.closest('.note-item');
      if (item && item.dataset.name) openNote(item.dataset.name);
    });

    els.search.addEventListener('input', () => {
      state.filter = els.search.value;
      renderList();
    });

    els.area.addEventListener('input', onInput);
    els.area.addEventListener('blur', () => { if (state.current) saveNow(); });
    els.area.addEventListener('keydown', (e) => {
      if (e.key === 'Tab') {
        e.preventDefault();
        const start = els.area.selectionStart;
        const end = els.area.selectionEnd;
        els.area.value = els.area.value.slice(0, start) + '\t' + els.area.value.slice(end);
        els.area.selectionStart = els.area.selectionEnd = start + 1;
        onInput();
      }
    });

    els.title.addEventListener('change', commitTitle);
    els.title.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); els.title.blur(); }
      if (e.key === 'Escape') { els.title.value = displayName(state.current); els.title.blur(); }
    });

    els.modalOk.addEventListener('click', modalSubmit);
    els.modalCancel.addEventListener('click', () => closeModal(null));
    els.modal.querySelector('[data-close]').addEventListener('click', () => closeModal(null));
    els.modalInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); modalSubmit(); }
      if (e.key === 'Escape') closeModal(null);
    });

    els.authForm.addEventListener('submit', submitAuth);

    els.settingsBtn.addEventListener('click', openSettings);
    els.settingsLogout.addEventListener('click', logout);
    els.settingsPw.addEventListener('click', showPasswordForm);
    els.settingsLogs.addEventListener('click', openLogs);
    els.pwForm.addEventListener('submit', submitPasswordChange);
    els.pwCancel.addEventListener('click', showSettingsMenu);
    els.settings.querySelector('[data-close]').addEventListener('click', closeSettings);
    els.logsBack.addEventListener('click', closeLogs);
    els.logs.querySelector('[data-close]').addEventListener('click', closeLogs);

    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && !els.logs.hidden) {
        e.preventDefault();
        closeLogs();
        return;
      }
      if (e.key === 'Escape' && !els.settings.hidden) {
        e.preventDefault();
        if (els.pwForm.hidden) closeSettings(); else showSettingsMenu();
        return;
      }
      if (!els.modal.hidden || !els.settings.hidden || !els.logs.hidden || state.locked) return;
      const meta = e.metaKey || e.ctrlKey;
      if (meta && e.key.toLowerCase() === 's') { e.preventDefault(); saveNow(); }
      else if (e.altKey && e.key.toLowerCase() === 'n') { e.preventDefault(); newNote(); }
      else if (e.key === '/' && document.activeElement !== els.search && document.activeElement !== els.area) {
        e.preventDefault(); els.search.focus();
      } else if (e.key === 'Escape') {
        if (document.activeElement === els.search && els.search.value) {
          els.search.value = ''; state.filter = ''; renderList();
        } else if (window.innerWidth <= 720) {
          closeEditor();
        }
      }
    });

    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'hidden') flush();
    });

    window.addEventListener('beforeunload', (e) => {
      if (!state.locked && state.current && els.area.value !== state.savedContent) {
        e.preventDefault();
        e.returnValue = '';
      }
    });
  }

  async function boot() {
    // All URLs in this file are relative, so the document URL must end with
    // "/" — otherwise "api/..." would resolve one level too high when the app
    // is served behind the fnOS gateway path (/app/notepad).
    if (!location.pathname.endsWith('/')) {
      location.replace(location.pathname + '/' + location.search + location.hash);
      return;
    }
    applyFontSize();
    bind();

    let session = null;
    try {
      session = await api('GET', 'api/session');
    } catch (err) {
      // 服务没起来时给一句人话，而不是白屏。
      state.needsSetup = false;
      showAuth('login');
      authError('无法连接到服务：' + err.message);
      return;
    }

    state.needsSetup = !!session.needsSetup;
    if (session.needsSetup) {
      showAuth('setup');
      return;
    }
    if (!session.authenticated) {
      showAuth('login');
      return;
    }
    await enterApp(session);
  }

  boot();
})();
