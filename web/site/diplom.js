// ─── CONFIG ───────────────────────────────────────────────────────────────────
const API_BASE = (document.body.getAttribute('data-api-base') || '/api/v1').replace(/\/$/, '');
/** Origin сайта (без /api/v1) — для подсказок; ссылки share приходят с сервера в поле view_url */
const SITE_ORIGIN = window.location.origin;

// ─── UTILS ────────────────────────────────────────────────────────────────────
function escapeHtml(s) {
  if (s == null || s === '') return '';
  const d = document.createElement('div');
  d.textContent = String(s);
  return d.innerHTML;
}

// ─── STATE ────────────────────────────────────────────────────────────────────
let currentRole = 'student';
let authToken = localStorage.getItem('token') || null;
let authRegisterMode = false;
let currentUserEmail = '';

// ─── CACHE MANAGER ────────────────────────────────────────────────────────────
class CacheManager {
  constructor() {
    this.memory = {};
    this.ttl = {
      profile: 5 * 60 * 1000,
      records: 2 * 60 * 1000,
      universities: 30 * 60 * 1000,
      verify: 10 * 60 * 1000,
    };
  }

  get(key, type = 'profile') {
    const item = this.memory[key];
    if (!item) return null;
    const now = Date.now();
    const ttl = this.ttl[type] || this.ttl.profile;
    if (now - item.timestamp > ttl) {
      delete this.memory[key];
      return null;
    }
    return item.data;
  }

  set(key, data) {
    this.memory[key] = { data, timestamp: Date.now() };
  }

  clear(pattern) {
    if (pattern) {
      Object.keys(this.memory).forEach(key => {
        if (key.includes(pattern)) delete this.memory[key];
      });
    } else {
      this.memory = {};
    }
  }

  getStats() {
    const keys = Object.keys(this.memory);
    const now = Date.now();
    const fresh = keys.filter(k => {
      const item = this.memory[k];
      return (now - item.timestamp) < 5 * 60 * 1000;
    });
    return { total: keys.length, fresh: fresh.length };
  }
}

const cache = new CacheManager();

async function cachedFetch(path, options = {}, cacheType = 'profile') {
  if (!options.method || options.method === 'GET') {
    const cached = cache.get(path, cacheType);
    if (cached) {
      console.log(`📦 Cache hit: ${path}`);
      return cached;
    }
  }
  const data = await apiFetch(path, options);
  if (!options.method || options.method === 'GET') {
    cache.set(path, data);
  }
  if (options.method && options.method !== 'GET') {
    if (path.includes('/university/upload')) {
      cache.clear('records');
      cache.clear('queue');
    }
    if (path.includes('/diplomas')) {
      cache.clear('profile');
      cache.clear('documents');
    }
  }
  return data;
}

// ─── TOAST NOTIFICATIONS ──────────────────────────────────────────────────────
let _notifyBadgeCount = 0;
const _pendingNotifications = [];
const _notifyHistory = []; // история для дропдауна

function _getOrCreateToastContainer() {
  let c = document.getElementById('toast-container');
  if (!c) {
    c = document.createElement('div');
    c.id = 'toast-container';
    c.setAttribute('aria-live', 'assertive');
    c.setAttribute('aria-atomic', 'false');
    c.style.cssText = 'position:fixed;top:20px;right:20px;z-index:10000;display:flex;flex-direction:column;gap:8px;pointer-events:none';
    document.body.appendChild(c);
  }
  return c;
}

function _updateNotifyBadge() {
  document.querySelectorAll('.notify-badge').forEach(el => {
    if (_notifyBadgeCount > 0) {
      el.textContent = _notifyBadgeCount;
      el.classList.remove('hidden');
    } else {
      el.classList.add('hidden');
    }
  });
}

// Дропдаун с историей уведомлений
function _toggleNotifyDropdown(btn) {
  let dropdown = document.getElementById('notify-dropdown');
  if (dropdown) {
    dropdown.remove();
    return;
  }

  dropdown = document.createElement('div');
  dropdown.id = 'notify-dropdown';
  dropdown.style.cssText = `
    position:absolute;top:calc(100% + 8px);right:0;
    background:var(--surface);border:1.5px solid var(--border);
    border-radius:var(--radius-lg);box-shadow:var(--shadow);
    min-width:300px;max-width:360px;z-index:9999;overflow:hidden;
  `;

  const header = document.createElement('div');
  header.style.cssText = 'padding:12px 16px;border-bottom:1px solid var(--border);display:flex;justify-content:space-between;align-items:center';
  header.innerHTML = `<span style="font-size:0.82rem;font-weight:700;text-transform:uppercase;letter-spacing:0.07em;color:var(--muted)">Уведомления</span>
    <button id="notify-clear-btn" style="font-size:0.78rem;color:var(--primary);background:none;border:none;cursor:pointer;font-weight:600">Очистить</button>`;
  dropdown.appendChild(header);

  const list = document.createElement('div');
  list.style.cssText = 'max-height:320px;overflow-y:auto';

  if (!_notifyHistory.length) {
    list.innerHTML = '<p style="padding:20px 16px;text-align:center;color:var(--muted);font-size:0.88rem">Нет уведомлений</p>';
  } else {
    const colors = { success: '#059669', warning: '#d97706', info: '#2563eb', error: '#dc2626' };
    list.innerHTML = [..._notifyHistory].reverse().map(n => `
      <div style="padding:12px 16px;border-bottom:1px solid #f1f5f9;display:flex;gap:10px;align-items:flex-start">
        <span style="width:8px;height:8px;border-radius:50%;background:${colors[n.type] || colors.info};flex-shrink:0;margin-top:5px"></span>
        <div style="flex:1;min-width:0">
          <div style="font-size:0.88rem;color:var(--text);font-weight:500">${escapeHtml(n.message)}</div>
          <div style="font-size:0.75rem;color:var(--muted);margin-top:2px">${n.time}</div>
        </div>
      </div>
    `).join('');
  }
  dropdown.appendChild(list);

  // Позиционируем относительно кнопки
  btn.style.position = 'relative';
  btn.appendChild(dropdown);

  // Закрываем при клике вне
  setTimeout(() => {
    document.addEventListener('click', function closeDropdown(e) {
      if (!dropdown.contains(e.target) && e.target !== btn) {
        dropdown.remove();
        document.removeEventListener('click', closeDropdown);
      }
    });
  }, 0);

  document.getElementById('notify-clear-btn')?.addEventListener('click', (e) => {
    e.stopPropagation();
    _notifyHistory.length = 0;
    _notifyBadgeCount = 0;
    _updateNotifyBadge();
    dropdown.remove();
  });
}

function showToast(message, type = 'info') {
  // Сохраняем в историю
  const now = new Date();
  _notifyHistory.push({
    message,
    type,
    time: now.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' }),
  });
  // Ограничиваем историю 50 записями
  if (_notifyHistory.length > 50) _notifyHistory.shift();

  if (document.hidden) {
    _pendingNotifications.push({ message, type });
    return;
  }
  const container = _getOrCreateToastContainer();

  // Максимум 5 тостов — удаляем старейший
  if (container.children.length >= 5) {
    container.firstElementChild?.remove();
  }

  const colors = { info: '#2563eb', success: '#059669', warning: '#d97706', error: '#dc2626' };
  const toast = document.createElement('div');
  toast.className = `toast toast-${type}`;
  toast.setAttribute('role', 'alert');
  toast.setAttribute('aria-live', 'assertive');
  toast.style.cssText = `
    padding:12px 18px;border-radius:10px;font-size:0.88rem;font-weight:500;
    color:#fff;background:${colors[type] || colors.info};
    box-shadow:0 4px 16px rgba(0,0,0,0.18);max-width:320px;pointer-events:auto;
    opacity:0;transform:translateX(20px);transition:opacity 0.25s,transform 0.25s;
  `;
  toast.textContent = message;
  container.appendChild(toast);

  requestAnimationFrame(() => {
    toast.style.opacity = '1';
    toast.style.transform = 'translateX(0)';
  });

  // Бейдж
  _notifyBadgeCount++;
  _updateNotifyBadge();

  // Авто-скрытие через 4 сек
  setTimeout(() => {
    toast.style.opacity = '0';
    toast.style.transform = 'translateX(20px)';
    setTimeout(() => toast.remove(), 280);
  }, 4000);
}

// Клик на колокольчик — показываем дропдаун и сбрасываем счётчик
document.addEventListener('click', (e) => {
  const btn = e.target.closest('.notify-badge-btn');
  if (btn) {
    e.stopPropagation();
    _notifyBadgeCount = 0;
    _updateNotifyBadge();
    _toggleNotifyDropdown(btn);
  }
});

// Показываем накопленные тосты когда вкладка становится активной
document.addEventListener('visibilitychange', () => {
  if (!document.hidden && _pendingNotifications.length) {
    const pending = _pendingNotifications.splice(0);
    pending.forEach(({ message, type }) => showToast(message, type));
  }
});

// ─── HELPERS ──────────────────────────────────────────────────────────────────
const ROLE_PATHS = { student: '/student', university: '/university', employer: '/employer', admin: '/admin' };
const PATH_ROLES = { '/student': 'student', '/university': 'university', '/employer': 'employer', '/admin': 'admin' };

function showScreen(id) {
  document.querySelectorAll('.screen').forEach(s => s.classList.remove('active'));
  document.getElementById('screen-' + id).classList.add('active');
  window.scrollTo({ top: 0, behavior: 'smooth' });
  const path = ROLE_PATHS[id];
  if (path && window.location.pathname !== path) {
    history.pushState({ screen: id }, '', path);
  } else if (!path && window.location.pathname !== '/') {
    history.pushState({}, '', '/');
  }
}

function showTab(tabId) {
  const screen = document.querySelector('.screen.active');
  if (!screen) return;
  screen.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
  screen.querySelectorAll('.sidebar-item').forEach(i => i.classList.remove('active'));
  document.getElementById(tabId)?.classList.add('active');
  screen.querySelector(`[data-tab="${tabId}"]`)?.classList.add('active');
  localStorage.setItem('active_tab', tabId);
  if (tabId === 'student-profile') {
    loadStudentIdentityForm().catch(() => {});
    loadStudentProfile().catch(() => {});
  }
  if (tabId === 'student-qr') {
    refreshStudentDiplomaQR(false).catch(() => {});
  }
}

// Браузерная кнопка «назад/вперёд»
window.addEventListener('popstate', (e) => {
  const role = PATH_ROLES[window.location.pathname];
  if (role && authToken) {
    document.querySelectorAll('.screen').forEach(s => s.classList.remove('active'));
    document.getElementById('screen-' + role)?.classList.add('active');
  } else {
    document.querySelectorAll('.screen').forEach(s => s.classList.remove('active'));
    document.getElementById('screen-auth')?.classList.add('active');
  }
});

function badge(status) {
  const map = {
    valid:      ['badge-success', 'Валиден'],
    invalid:    ['badge-danger',  'Недействителен'],
    pending:    ['badge-warning', 'В очереди'],
    processing: ['badge-info',    'Обрабатывается'],
    error:      ['badge-danger',  'Ошибка'],
    done:       ['badge-success', 'Готово'],
    verified:   ['badge-success', 'Подтверждён'],
    revoked:    ['badge-danger',  'Аннулирован'],
  };
  const [cls, label] = map[status] || ['badge-info', status];
  return `<span class="badge ${cls}">${label}</span>`;
}

async function apiFetch(path, options = {}) {
  const headers = { 'Content-Type': 'application/json', ...(options.headers || {}) };
  if (authToken) headers['Authorization'] = `Bearer ${authToken}`;
  const res = await fetch(API_BASE + path, { ...options, headers });
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try { const e = await res.json(); if (e.error) msg = e.error; } catch {}
    throw new Error(msg);
  }
  return res.json();
}

// ─── AUTH UI ──────────────────────────────────────────────────────────────────
function updateAuthFormUI() {
  const isUniReg = authRegisterMode && currentRole === 'university';

  // заголовок и подпись
  document.getElementById('auth-panel-title').textContent =
    authRegisterMode ? 'Регистрация' : 'Вход в систему';
  document.getElementById('auth-panel-sub').textContent =
    authRegisterMode ? 'Создайте учётную запись'
    : 'Выберите роль и введите учётные данные';

  // кнопка submit
  document.getElementById('auth-submit-title').textContent =
    authRegisterMode ? 'Зарегистрироваться' : 'Войти';

  // ссылки вход/регистрация
  document.getElementById('link-show-login')?.classList.toggle('auth-link-active', !authRegisterMode);
  document.getElementById('link-show-register')?.classList.toggle('auth-link-active', authRegisterMode);

  // форма заявки ВУЗа
  const uniWrap = document.getElementById('uni-apply-wrap');
  const authForm = document.getElementById('auth-form');
  uniWrap?.classList.toggle('hidden', !isUniReg);
  authForm?.classList.toggle('hidden', isUniReg);
}

// переключение ролей
document.querySelectorAll('.role-tab').forEach(btn => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.role-tab').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentRole = btn.dataset.role;
    updateAuthFormUI();
  });
});

// переключение вход/регистрация
document.getElementById('link-show-login')?.addEventListener('click', (e) => {
  e.preventDefault(); authRegisterMode = false; updateAuthFormUI();
});
document.getElementById('link-show-register')?.addEventListener('click', (e) => {
  e.preventDefault(); authRegisterMode = true; updateAuthFormUI();
});
document.getElementById('link-show-login-uni')?.addEventListener('click', (e) => {
  e.preventDefault(); authRegisterMode = false; updateAuthFormUI();
});

async function enterCabinet(data) {
  authToken = data.token;
  localStorage.setItem('token', authToken);
  currentUserEmail = data.email || '';
  const uiRole = data.role === 'hr' ? 'employer' : data.role;
  updateHeaderEmail(uiRole);
  showScreen(uiRole);
  if (uiRole === 'student') {
    loadStudentIdentityForm().catch(() => {});
    loadStudentProfile().catch(() => {});
  }
  if (uiRole === 'university') { loadRecords(); loadUniversityStats(); }
  if (uiRole === 'employer')   loadHistory();
  if (uiRole === 'admin')      { loadApplicationsWithCache(); loadAdminStats(); }
  startNotifyWs();
}

function updateHeaderEmail(role) {
  const emailEl = document.getElementById(`header-email-${role}`);
  if (emailEl && currentUserEmail) {
    emailEl.textContent = currentUserEmail;
    emailEl.style.display = 'inline';
  }
}

// ─── AUTH FORM SUBMIT ─────────────────────────────────────────────────────────

document.getElementById('auth-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const email    = document.getElementById('auth-login').value.trim();
  const password = document.getElementById('auth-password').value;
  const errEl    = document.getElementById('auth-error');
  errEl.classList.add('hidden');

  try {
    if (authRegisterMode) {
      const backendRole = currentRole === 'employer' ? 'hr' : currentRole;
      const data = await apiFetch('/auth/register', {
        method: 'POST',
        body: JSON.stringify({ email, password, role: backendRole }),
      });
      enterCabinet(data);
    } else {
      const data = await apiFetch('/auth/login', {
        method: 'POST',
        body: JSON.stringify({ email, password }),
      });
      enterCabinet(data);
    }
  } catch (err) {
    errEl.textContent = err.message || 'Неверный логин или пароль';
    errEl.classList.remove('hidden');
  }
});

// ─── UNI APPLY INLINE FORM ────────────────────────────────────────────────────
document.getElementById('uni-apply-form-inline')?.addEventListener('submit', async (e) => {
  e.preventDefault();
  const statusEl = document.getElementById('uni-apply-status');
  statusEl.className = 'status-bar info';
  statusEl.textContent = '⏳ Отправляем заявку…';
  statusEl.classList.remove('hidden');

  const fd = new FormData();
  fd.append('email',             document.getElementById('uni-apply-email').value.trim());
  fd.append('password',          document.getElementById('uni-apply-password').value);
  fd.append('organization_name', document.getElementById('uni-apply-org').value.trim());
  fd.append('notes',             document.getElementById('uni-apply-notes').value.trim());
  const files = document.getElementById('uni-apply-docs').files;
  Array.from(files).forEach(f => fd.append('documents', f));

  try {
    const res = await fetch(`${API_BASE}/auth/university/apply`, { method: 'POST', body: fd });
    const data = await res.json();
    if (!res.ok) { statusEl.className = 'status-bar error'; statusEl.textContent = '❌ ' + (data.error || `Ошибка ${res.status}`); return; }
    statusEl.className = 'status-bar success';
    statusEl.textContent = 'Заявка отправлена! Ожидайте подтверждения администратора.';
    document.getElementById('uni-apply-form-inline').reset();
  } catch {
    statusEl.className = 'status-bar error';
    statusEl.textContent = 'Не удалось подключиться к серверу';
  }
});

updateAuthFormUI();

function logout() {
  authToken = null;
  localStorage.removeItem('token');
  localStorage.removeItem('active_screen');
  localStorage.removeItem('active_tab');
  history.pushState({}, '', '/');
  showScreen('auth');
}

['student', 'university', 'employer', 'admin'].forEach(role => {
  document.getElementById(`logout-${role}`)?.addEventListener('click', logout);
});

// ─── SIDEBAR NAVIGATION ───────────────────────────────────────────────────────
document.querySelectorAll('.sidebar-item').forEach(item => {
  item.addEventListener('click', () => showTab(item.dataset.tab));
});

// ─── STUDENT ──────────────────────────────────────────────────────────────────
const studentProfileSourceLabels = {
  registry: 'Реестр вуза (привязано)',
  profile: 'Только ваш ввод (ждёт сверки)',
};
const studentDiplomaStatusLabels = {
  verified: 'Подтверждён',
  pending: 'На рассмотрении',
  revoked: 'Аннулирован',
};

function formatStudentYear(y) {
  if (y == null || y === '') return '—';
  if (typeof y === 'string' && y.trim()) return y.trim();
  const n = Number(y);
  if (!Number.isNaN(n) && n !== 0) return String(n);
  return '—';
}

async function loadStudentIdentityForm() {
  if (!document.getElementById('form-student-identity')) return;
  try {
    const me = await apiFetch('/users/me');
    const set = (id, v) => {
      const el = document.getElementById(id);
      if (el) el.value = v ?? '';
    };
    set('student-pass-last', me.passport_last_name);
    set('student-pass-first', me.passport_first_name);
    set('student-pass-pat', me.passport_patronymic);
    set('student-identity-diploma-num', me.claimed_diploma_number);
    set('student-identity-univ', me.claimed_university_full);
    set('student-identity-spec', me.claimed_specialty);
    set('student-identity-year', me.claimed_graduation_year);
    set('student-identity-registry-id', '');
  } catch {
    console.warn('Не удалось загрузить форму профиля');
  }
}

async function loadStudentProfile() {
  try {
    const d = await cachedFetch('/student/profile', {}, 'profile');
    const srcEl = document.getElementById('s-source');
    if (srcEl) srcEl.textContent = studentProfileSourceLabels[d.source] || d.source || '—';
    const lid = document.getElementById('s-linked-id');
    if (lid) {
      lid.textContent =
        d.linked_diploma_id != null && d.linked_diploma_id !== '' ? String(d.linked_diploma_id) : '—';
    }
    const n = document.getElementById('s-name');
    if (n) n.textContent = d.name || '—';
    const sp = document.getElementById('s-spec');
    if (sp) sp.textContent = d.specialty || '—';
    const y = document.getElementById('s-year');
    if (y) y.textContent = formatStudentYear(d.year);
    const dn = document.getElementById('s-diploma-num');
    if (dn) dn.textContent = d.diplomaNumber || '—';
    const u = document.getElementById('s-university');
    if (u) u.textContent = d.university || '—';
    const st = document.getElementById('s-diploma-status');
    if (st) {
      st.textContent = d.diploma_status
        ? studentDiplomaStatusLabels[d.diploma_status] || d.diploma_status
        : '—';
    }
  } catch {
    console.warn('Не удалось загрузить профиль студента');
  }
}

async function loadStudentDocs() {
  const list = document.getElementById('docs-list');
  try {
    const docs = await cachedFetch('/student/documents', {}, 'profile');
    if (!docs.length) {
      list.innerHTML = '<p class="placeholder-text">Нет загруженных документов</p>';
      return;
    }
    list.innerHTML = docs.map(d => `
      <div class="doc-item">
        <span>📄 ${d.name}</span>
        ${badge(d.status)}
      </div>
    `).join('');
  } catch {
    list.innerHTML = '<p class="placeholder-text">Ошибка загрузки</p>';
  }
}

document.getElementById('btn-load-profile')?.addEventListener('click', () => {
  loadStudentIdentityForm().catch(() => {});
  loadStudentProfile().catch(() => {});
});

document.getElementById('form-student-identity')?.addEventListener('submit', async (e) => {
  e.preventDefault();
  const statusEl = document.getElementById('student-identity-status');
  if (!statusEl) return;
  statusEl.className = 'status-bar info';
  statusEl.textContent = '⏳ Сохранение и сверка с реестром…';
  statusEl.classList.remove('hidden');
  const regRaw = document.getElementById('student-identity-registry-id')?.value?.trim() || '';
  const regId = regRaw ? parseInt(regRaw, 10) : 0;
  const body = {
    passport_last_name: document.getElementById('student-pass-last')?.value?.trim() || '',
    passport_first_name: document.getElementById('student-pass-first')?.value?.trim() || '',
    passport_patronymic: document.getElementById('student-pass-pat')?.value?.trim() || '',
    claimed_diploma_number: document.getElementById('student-identity-diploma-num')?.value?.trim() || '',
    claimed_university_full: document.getElementById('student-identity-univ')?.value?.trim() || '',
    claimed_specialty: document.getElementById('student-identity-spec')?.value?.trim() || '',
    claimed_graduation_year: document.getElementById('student-identity-year')?.value?.trim() || '',
    registry_diploma_id: regId > 0 && !Number.isNaN(regId) ? regId : 0,
  };
  try {
    const res = await apiFetch('/users/me', { method: 'PUT', body: JSON.stringify(body) });
    if (res.auto_linked) {
      statusEl.className = 'status-bar success';
      statusEl.textContent = '' + (res.detail || 'Данные совпали с реестром, диплом привязан.');
    } else {
      statusEl.className = 'status-bar info';
      statusEl.textContent =
        'Сохранено. Автосверка не выполнена: нет совпадения с «свободной» записью реестра (проверьте Excel, номер и ФИО) или укажите верный ID строки из кабинета вуза.';
    }
    await loadStudentProfile().catch(() => {});
  } catch (err) {
    statusEl.className = 'status-bar error';
    statusEl.textContent = '' + (err.message || 'Ошибка');
  }
});

document.getElementById('btn-load-docs').addEventListener('click', loadStudentDocs);

// ─── Студент: QR и одноразовая ссылка (бэкенд: POST /diplomas/:id/share, GET /d/:token) ───
let lastStudentShareViewUrl = '';
let lastStudentShareToken = '';

function renderStudentQrImage(viewUrl) {
  const host = document.getElementById('student-qr-host');
  if (!host || !viewUrl) return;
  const enc = encodeURIComponent(viewUrl);
  host.innerHTML = `<img width="220" height="220" alt="QR-код ссылки подтверждения" src="https://api.qrserver.com/v1/create-qr-code/?size=220x220&amp;margin=8&amp;data=${enc}" />`;
}

async function refreshStudentDiplomaQR(forceNew = false) {
  const host = document.getElementById('student-qr-host');
  const hint = document.getElementById('student-qr-hint');
  const wrap = document.getElementById('student-qr-wrap');
  const dipWrap = document.getElementById('student-qr-diploma-wrap');
  const sel = document.getElementById('student-qr-diploma');
  const linkInp = document.getElementById('student-qr-link');
  const meta = document.getElementById('student-qr-meta');
  if (!host || !hint) return;

  hint.className = 'hint-box';
  hint.textContent = 'Загружаем список дипломов и создаём ссылку…';

  try {
    const res = await apiFetch('/diplomas?page=1&limit=50');
    const rows = res.data || [];
    const verified = rows.filter((d) => d.status === 'verified');
    if (!verified.length) {
      lastStudentShareViewUrl = '';
      lastStudentShareToken = '';
      host.innerHTML = '';
      if (linkInp) linkInp.value = '';
      if (meta) meta.textContent = '';
      wrap?.classList.add('hidden');
      dipWrap?.classList.add('hidden');
      hint.className = 'hint-box';
      hint.textContent =
        'Нет подтверждённых дипломов. Сначала привяжите диплом во вкладке «Мои данные» (сверка с реестром вуза) или дождитесь подтверждения заявки.';
      return;
    }

    dipWrap?.classList.remove('hidden');
    const prev = sel?.value;
    if (sel) {
      sel.innerHTML = verified
        .map(
          (d) =>
            `<option value="${Number(d.id)}">${escapeAttr(String(d.diploma_number || d.id))} (id ${Number(d.id)})</option>`,
        )
        .join('');
      if (prev && verified.some((d) => String(d.id) === prev)) sel.value = prev;
    }

    const diplomaId = Number((sel && sel.value) || verified[0].id);
    const share = await apiFetch(`/diplomas/${diplomaId}/share`, {
      method: 'POST',
      body: JSON.stringify({
        view_once: true,
        ttl_hours: 24,
        force_new: !!forceNew,
      }),
    });

    const viewUrl = share.view_url || '';
    lastStudentShareViewUrl = viewUrl;
    lastStudentShareToken = share.token || '';

    renderStudentQrImage(viewUrl);
    if (linkInp) linkInp.value = viewUrl;
    if (meta) {
      const exp = share.expires_at ? new Date(share.expires_at).toLocaleString('ru-RU') : '—';
      const reused = share.reused ? ' (тот же токен, пока ссылка не открывали)' : '';
      meta.textContent = `Действует до: ${exp}. Режим: одноразовое открытие.${reused}`;
    }
    wrap?.classList.remove('hidden');
    hint.className = 'hint-box';
    hint.textContent =
      `Покажите QR или отправьте ссылку. После первого открытия страницы токен сгорает — «Новый QR» выдаёт новый. Ссылки строятся от базового адреса ${SITE_ORIGIN} (переменная PUBLIC_BASE_URL в API). Код: internal/api/handlers/share.go.`;
  } catch (e) {
    hint.className = 'hint-box';
    hint.style.borderColor = 'var(--danger)';
    hint.style.background = 'var(--danger-bg)';
    hint.style.color = 'var(--danger)';
    hint.textContent = '' + (e.message || 'Не удалось создать ссылку');
    wrap?.classList.add('hidden');
  }
}

function escapeAttr(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;');
}

document.getElementById('student-qr-diploma')?.addEventListener('change', () => refreshStudentDiplomaQR(false).catch(() => {}));
document.getElementById('btn-student-qr-refresh')?.addEventListener('click', () => refreshStudentDiplomaQR(false).catch(() => {}));
document.getElementById('btn-student-qr-new')?.addEventListener('click', () => refreshStudentDiplomaQR(true).catch(() => {}));
document.getElementById('btn-student-qr-copy')?.addEventListener('click', async () => {
  if (!lastStudentShareViewUrl) return;
  try {
    await navigator.clipboard.writeText(lastStudentShareViewUrl);
    showToast('Ссылка скопирована в буфер обмена', 'success');
  } catch {
    prompt('Скопируйте ссылку:', lastStudentShareViewUrl);
  }
});
document.getElementById('btn-student-qr-share')?.addEventListener('click', async () => {
  if (!lastStudentShareViewUrl) return;
  const payload = {
    title: 'Подтверждение диплома',
    text: 'Откройте ссылку, чтобы проверить диплом',
    url: lastStudentShareViewUrl,
  };
  if (navigator.share) {
    try {
      await navigator.share(payload);
    } catch (err) {
      if (err && err.name !== 'AbortError') {
        showToast('Не удалось открыть меню «Поделиться»', 'warning');
      }
    }
  } else {
    try {
      await navigator.clipboard.writeText(lastStudentShareViewUrl);
      showToast('Ссылка скопирована (нет встроенного «Поделиться»)', 'info');
    } catch {
      prompt('Скопируйте ссылку:', lastStudentShareViewUrl);
    }
  }
});

document.getElementById('btn-student-verify').addEventListener('click', async () => {
  const id     = document.getElementById('s-verify-id').value.trim();
  const result = document.getElementById('s-verify-result');
  if (!id) return;

  result.className = 'verify-result loading';
  result.textContent = '⏳ Проверяем...';
  result.classList.remove('hidden');

  try {
    const data = await apiFetch(`/verify/${encodeURIComponent(id)}`);
    if (data.valid) {
      result.className = 'verify-result valid';
      result.innerHTML = `Диплом действителен<br><small><b>${data.name}</b> · ${data.specialty}<br>${data.university} · ${data.year}</small>`;
    } else {
      result.className = 'verify-result invalid';
      result.textContent = 'Диплом не найден или недействителен';
    }
  } catch {
    result.className = 'verify-result invalid';
    result.textContent = 'Ошибка при проверке';
  }
});

// ─── UNIVERSITY ───────────────────────────────────────────────────────────────

// Drag & Drop
const dropzone  = document.getElementById('dropzone');
const fileInput = document.getElementById('file-input');
let selectedFile = null;

function selectFile(file) {
  if (!file) return;
  selectedFile = file;
  document.getElementById('upload-filename').textContent = `📄 ${file.name}`;
  document.getElementById('upload-preview').classList.remove('hidden');
}

dropzone.addEventListener('dragover', (e) => { e.preventDefault(); dropzone.classList.add('drag-over'); });
dropzone.addEventListener('dragleave', () => dropzone.classList.remove('drag-over'));
dropzone.addEventListener('drop', (e) => {
  e.preventDefault();
  dropzone.classList.remove('drag-over');
  selectFile(e.dataTransfer.files[0]);
});
fileInput.addEventListener('change', () => selectFile(fileInput.files[0]));

document.getElementById('btn-upload-cancel').addEventListener('click', () => {
  selectedFile = null;
  fileInput.value = '';
  document.getElementById('upload-preview').classList.add('hidden');
  document.getElementById('upload-status').classList.add('hidden');
});

document.getElementById('btn-upload-send').addEventListener('click', async () => {
  if (!selectedFile) return;
  const statusEl = document.getElementById('upload-status');
  statusEl.className = 'status-bar info';
  statusEl.textContent = '⏳ Загружаем файл...';
  statusEl.classList.remove('hidden');

  const formData = new FormData();
  formData.append('file', selectedFile);

  try {
    const res = await fetch(`${API_BASE}/university/upload`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${authToken}` },
      body: formData,
    });
    if (!res.ok) throw new Error();
    const data = await res.json();
    statusEl.className = 'status-bar success';
    statusEl.textContent = `Файл принят. ID задачи: ${data.job_id}`;
    document.getElementById('upload-preview').classList.add('hidden');
    selectedFile = null;
    fileInput.value = '';
    watchJobWS(data.job_id);
  } catch {
    statusEl.className = 'status-bar error';
    statusEl.textContent = 'Ошибка загрузки. Попробуйте снова.';
  }
});

// Состояние пагинации записей ВУЗа
let recordsPage  = 1;
let recordsLimit = 25;

async function loadRecords(page, limit) {
  page  = page  || recordsPage;
  limit = limit || recordsLimit;
  const tbody = document.getElementById('records-tbody');
  try {
    cache.clear('records');
    const res = await apiFetch(`/university/records?page=${page}&limit=${limit}`);
    // Поддержка нового формата {data, page, limit, total} и старого массива
    const records = Array.isArray(res) ? res : (res.data || []);
    const total   = res.total ?? records.length;
    recordsPage   = res.page  ?? page;
    recordsLimit  = res.limit ?? limit;

    if (!records.length) {
      tbody.innerHTML = '<tr><td colspan="6" class="placeholder-text">Нет данных</td></tr>';
    } else {
      tbody.innerHTML = records.map(r => `
        <tr>
          <td><code>${r.id}</code></td>
          <td>${r.name}</td>
          <td>${r.specialty}</td>
          <td>${r.year}</td>
          <td>${r.diplomaNumber}</td>
          <td>${badge(r.status)}</td>
        </tr>
      `).join('');
    }

    // Обновляем контролы пагинации
    const totalPages = Math.max(1, Math.ceil(total / recordsLimit));
    const pageInfo = document.getElementById('records-page-info');
    if (pageInfo) pageInfo.textContent = `Стр. ${recordsPage} из ${totalPages}`;
    const prevBtn = document.getElementById('btn-records-prev');
    const nextBtn = document.getElementById('btn-records-next');
    if (prevBtn) prevBtn.disabled = recordsPage <= 1;
    if (nextBtn) nextBtn.disabled = recordsPage >= totalPages;
  } catch {
    tbody.innerHTML = '<tr><td colspan="6" class="placeholder-text">Ошибка загрузки</td></tr>';
  }
}

async function loadQueue() {
  const list = document.getElementById('queue-list');
  try {
    const jobs = await apiFetch('/university/queue');
    if (!jobs.length) {
      list.innerHTML = '<p class="placeholder-text">Нет активных задач</p>';
      return;
    }
    list.innerHTML = jobs.map(j => `
      <div class="queue-item">
        <div class="q-header">
          <span>📁 ${j.filename}</span>
          ${badge(j.status)}
        </div>
        ${j.status === 'processing' ? `
          <div class="progress-bar"><div class="progress-fill" style="width:${j.progress || 0}%"></div></div>
        ` : ''}
        ${j.error ? `<small style="color:var(--danger)">${j.error}</small>` : ''}
      </div>
    `).join('');
  } catch {
    list.innerHTML = '<p class="placeholder-text">Ошибка загрузки</p>';
  }
}

document.getElementById('btn-load-records').addEventListener('click', loadRecords);
document.getElementById('btn-load-queue').addEventListener('click', loadQueue);

// Экспорт реестра CSV
document.getElementById('btn-export-records')?.addEventListener('click', async () => {
  try {
    const res = await fetch(`${API_BASE}/university/records/export`, {
      headers: { Authorization: `Bearer ${authToken}` },
    });
    if (!res.ok) throw new Error('Ошибка экспорта');
    const blob = await res.blob();
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `records_${Date.now()}.csv`;
    a.click();
  } catch (err) {
    showToast('Ошибка экспорта: ' + err.message, 'error');
  }
});

// Ручное создание диплома
document.getElementById('form-manual-diploma')?.addEventListener('submit', async (e) => {
  e.preventDefault();
  const statusEl = document.getElementById('manual-diploma-status');
  statusEl.className = 'status-bar info';
  statusEl.textContent = '⏳ Создаём диплом…';
  statusEl.classList.remove('hidden');
  try {
    const data = await apiFetch('/university/diplomas', {
      method: 'POST',
      body: JSON.stringify({
        diploma_number: document.getElementById('manual-diploma-number').value.trim(),
        full_name:      document.getElementById('manual-full-name').value.trim(),
        specialty:      document.getElementById('manual-specialty').value.trim(),
        university:     document.getElementById('manual-university').value.trim(),
        year:           document.getElementById('manual-year').value.trim(),
      }),
    });
    statusEl.className = 'status-bar success';
    statusEl.textContent = `✅ Диплом создан. ID: ${data.id}, хеш: ${data.hash || '—'}`;
    document.getElementById('form-manual-diploma').reset();
    cache.clear('records');
  } catch (err) {
    statusEl.className = 'status-bar error';
    statusEl.textContent = '❌ ' + (err.message || 'Ошибка');
  }
});

// Пагинация
document.getElementById('btn-records-prev')?.addEventListener('click', () => {
  if (recordsPage > 1) loadRecords(recordsPage - 1, recordsLimit);
});
document.getElementById('btn-records-next')?.addEventListener('click', () => {
  loadRecords(recordsPage + 1, recordsLimit);
});
document.getElementById('records-limit-select')?.addEventListener('change', function () {
  recordsLimit = parseInt(this.value, 10) || 25;
  loadRecords(1, recordsLimit);
});

document.getElementById('records-search').addEventListener('input', function () {
  const q = this.value.toLowerCase();
  clearTimeout(this._searchTimeout);
  this._searchTimeout = setTimeout(() => {
    document.querySelectorAll('#records-tbody tr').forEach(row => {
      row.style.display = row.textContent.toLowerCase().includes(q) ? '' : 'none';
    });
  }, 300);
});

// ─── EMPLOYER ─────────────────────────────────────────────────────────────────
document.getElementById('btn-check-id').addEventListener('click', async () => {
  const raw    = document.getElementById('emp-diploma-id').value.trim();
  const result = document.getElementById('emp-result');
  if (!raw) return;

  result.className = 'verify-result loading';
  result.textContent = 'Проверяем...';
  result.classList.remove('hidden');

  try {
    let data;

    // Ссылка вида /d/<token> или http://host/d/<token> — share токен студента
    const shareMatch = raw.match(/\/d\/([^/?#\s]+)/i);
    if (shareMatch) {
      const token = shareMatch[1];
      const shared = await apiFetch(`/shared/${encodeURIComponent(token)}`);
      // shared: { diploma_number, status, metadata: { name, university, specialty, year } }
      const m = shared.metadata || {};
      data = {
        valid:      shared.status === 'verified',
        name:       m.name       || '—',
        specialty:  m.specialty  || '—',
        university: m.university || '—',
        year:       m.year       || '—',
      };
    } else {
      // Обычный ID или номер диплома
      data = await apiFetch(`/verify/${encodeURIComponent(raw)}`);
    }

    if (data.valid) {
      result.className = 'verify-result valid';
      result.innerHTML = `
        Диплом действителен<br>
        <small>
          <b>${data.name}</b> · ${data.specialty}<br>
          ${data.university} · ${data.year}
        </small>
      `;
    } else {
      result.className = 'verify-result invalid';
      result.textContent = 'Диплом не найден или недействителен';
    }
  } catch {
    result.className = 'verify-result invalid';
    result.textContent = 'Ошибка при проверке';
  }
});

// QR-сканирование с камеры (BarcodeDetector + fallback jsQR)
let _qrStream = null;
let _qrAnimFrame = null;
let _qrCameras = [];
let _qrCamIdx = 0;

async function openQrScanner() {
  const modal = document.getElementById('qr-scan-modal');
  const status = document.getElementById('qr-scan-status');
  const switchBtn = document.getElementById('qr-switch-camera');
  modal.classList.remove('hidden');
  modal.setAttribute('aria-hidden', 'false');
  status.textContent = 'Запрашиваем доступ к камере…';

  if (location.protocol !== 'https:' && location.hostname !== 'localhost' && location.hostname !== '127.0.0.1') {
    status.textContent = '⚠️ Камера доступна только по HTTPS или на localhost. Введите ID диплома вручную.';
    return;
  }

  if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
    status.textContent = '⚠️ Ваш браузер не поддерживает доступ к камере. Введите ID вручную.';
    return;
  }

  try {
    _qrStream = await navigator.mediaDevices.getUserMedia({ video: true });
    const video = document.getElementById('qr-video');
    video.srcObject = _qrStream;
    await video.play();
    status.textContent = 'Наведите на QR-код…';
    tickQr();

    const devices = await navigator.mediaDevices.enumerateDevices();
    _qrCameras = devices.filter(d => d.kind === 'videoinput');
    _qrCamIdx = 0;
    switchBtn.style.display = _qrCameras.length > 1 ? '' : 'none';
  } catch (e) {
    status.textContent = '⚠️ Не удалось открыть камеру. Проверьте разрешения браузера.';
  }
}

async function startQrCamera() {
  stopQrScanner(false);
  const status = document.getElementById('qr-scan-status');
  const video = document.getElementById('qr-video');

  const cam = _qrCameras[_qrCamIdx % _qrCameras.length];
  try {
    _qrStream = await navigator.mediaDevices.getUserMedia({
      video: cam ? { deviceId: { exact: cam.deviceId } } : true
    });
    video.srcObject = _qrStream;
    await video.play();
    status.textContent = 'Наведите на QR-код…';
    tickQr();
  } catch (e) {
    status.textContent = '⚠️ Не удалось переключить камеру.';
  }
}

function tickQr() {
  const video = document.getElementById('qr-video');
  const canvas = document.getElementById('qr-canvas');
  const status = document.getElementById('qr-scan-status');

  if (!_qrStream || video.readyState < 2) {
    _qrAnimFrame = requestAnimationFrame(tickQr);
    return;
  }

  canvas.width = video.videoWidth;
  canvas.height = video.videoHeight;
  const ctx = canvas.getContext('2d');
  ctx.drawImage(video, 0, 0);

  if ('BarcodeDetector' in window) {
    const detector = new BarcodeDetector({ formats: ['qr_code'] });
    detector.detect(video).then(codes => {
      if (codes && codes.length) {
        onQrDetected(codes[0].rawValue);
      } else {
        _qrAnimFrame = requestAnimationFrame(tickQr);
      }
    }).catch(() => {
      _qrAnimFrame = requestAnimationFrame(tickQr);
    });
  } else if (window.jsQR) {
    const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height);
    const code = jsQR(imageData.data, imageData.width, imageData.height);
    if (code) {
      onQrDetected(code.data);
    } else {
      _qrAnimFrame = requestAnimationFrame(tickQr);
    }
  } else {
    status.textContent = 'QR-сканер не поддерживается этим браузером. Введите ID вручную.';
  }
}

function onQrDetected(raw) {
  stopQrScanner(true);
  document.getElementById('emp-diploma-id').value = raw.trim();
  document.getElementById('btn-check-id').click();
}

function stopQrScanner(closeModal = true) {
  if (_qrAnimFrame) { cancelAnimationFrame(_qrAnimFrame); _qrAnimFrame = null; }
  if (_qrStream) { _qrStream.getTracks().forEach(t => t.stop()); _qrStream = null; }
  if (closeModal) {
    const modal = document.getElementById('qr-scan-modal');
    modal.classList.add('hidden');
    modal.setAttribute('aria-hidden', 'true');
  }
}

document.getElementById('btn-scan-qr').addEventListener('click', openQrScanner);
document.getElementById('qr-scan-cancel').addEventListener('click', () => stopQrScanner(true));
document.getElementById('qr-scan-close').addEventListener('click', () => stopQrScanner(true));
document.getElementById('qr-scan-modal').addEventListener('click', (e) => {
  if (e.target.id === 'qr-scan-modal') stopQrScanner(true);
});
document.getElementById('qr-switch-camera').addEventListener('click', () => {
  _qrCamIdx++;
  startQrCamera();
});

async function loadHistory() {
  const tbody = document.getElementById('history-tbody');
  try {
    const history = await cachedFetch('/employer/history', {}, 'profile');
    if (!history.length) {
      tbody.innerHTML = '<tr><td colspan="4" class="placeholder-text">Нет истории</td></tr>';
      return;
    }
    tbody.innerHTML = history.map(h => `
      <tr>
        <td>${new Date(h.date).toLocaleDateString('ru-RU')}</td>
        <td>${h.diplomaId}</td>
        <td>${h.name}</td>
        <td>${badge(h.result ? 'valid' : 'invalid')}</td>
      </tr>
    `).join('');
  } catch {
    tbody.innerHTML = '<tr><td colspan="4" class="placeholder-text">Ошибка загрузки</td></tr>';
  }
}

document.getElementById('btn-load-history').addEventListener('click', loadHistory);

// Экспорт истории CSV
document.getElementById('btn-export-history')?.addEventListener('click', async (e) => {
  e.preventDefault();
  try {
    const res = await fetch(`${API_BASE}/employer/history/export`, {
      headers: { Authorization: `Bearer ${authToken}` },
    });
    if (!res.ok) throw new Error('Ошибка экспорта');
    const blob = await res.blob();
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `history_${Date.now()}.csv`;
    a.click();
  } catch (err) {
    showToast('Ошибка экспорта: ' + err.message, 'error');
  }
});

// ─── Статистика ВУЗа ─────────────────────────────────────────────────────────
async function loadUniversityStats() {
  try {
    const s = await apiFetch('/university/stats');
    const set = (id, v) => { const el = document.getElementById(id); if (el) el.textContent = v ?? '—'; };
    set('stat-total',    s.total);
    set('stat-verified', s.verified);
    set('stat-revoked',  s.revoked);
  } catch { /* тихо */ }
}

// ─── Пакетная проверка ───────────────────────────────────────────────────────
let _batchResults = [];

document.getElementById('btn-batch-verify')?.addEventListener('click', async () => {
  const raw = document.getElementById('emp-batch-input').value;
  const ids = raw.split('\n').map(s => s.trim()).filter(Boolean);
  if (!ids.length) return;

  const resultEl = document.getElementById('emp-batch-result');
  resultEl.innerHTML = '<p class="placeholder-text">⏳ Проверяем…</p>';

  try {
    const data = await apiFetch('/verify/batch', {
      method: 'POST',
      body: JSON.stringify({ identifiers: ids }),
    });
    _batchResults = data.results || [];

    const summary = `<p style="margin-bottom:12px;font-size:0.9rem;color:var(--muted)">
      Всего: <b>${data.total}</b> · Действительных: <b style="color:var(--success)">${data.valid}</b> · Недействительных: <b style="color:var(--danger)">${data.invalid}</b>
    </p>`;

    const rows = _batchResults.map(r => `
      <tr>
        <td>${escapeHtml(r.identifier)}</td>
        <td>${escapeHtml(r.name || '—')}</td>
        <td>${escapeHtml(r.university || '—')}</td>
        <td>${badge(r.valid ? 'valid' : 'invalid')}</td>
      </tr>
    `).join('');

    resultEl.innerHTML = summary + `
      <div class="table-wrap">
        <table>
          <thead><tr><th>Номер диплома</th><th>ФИО</th><th>ВУЗ</th><th>Результат</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;

    document.getElementById('btn-batch-export').style.display = '';
  } catch (err) {
    resultEl.innerHTML = `<p class="status-bar error">❌ ${escapeHtml(err.message || 'Ошибка')}</p>`;
  }
});

document.getElementById('btn-batch-export')?.addEventListener('click', () => {
  if (!_batchResults.length) return;
  const rows = [['Номер диплома', 'ФИО', 'ВУЗ', 'Специальность', 'Год', 'Результат']];
  _batchResults.forEach(r => rows.push([
    r.identifier, r.name || '', r.university || '', r.specialty || '', r.year || '',
    r.valid ? 'Действителен' : 'Недействителен',
  ]));
  const csv = rows.map(r => r.map(v => `"${String(v).replace(/"/g, '""')}"`).join(',')).join('\n');
  const blob = new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `batch_verify_${Date.now()}.csv`;
  a.click();
});

// ─── Статистика администратора ───────────────────────────────────────────────
async function loadAdminStats() {
  try {
    const s = await apiFetch('/admin/stats');
    const set = (id, v) => { const el = document.getElementById(id); if (el) el.textContent = v ?? '—'; };
    set('astat-users',        s.users?.total);
    set('astat-students',     s.users?.students);
    set('astat-universities', s.users?.universities);
    set('astat-employers',    s.users?.employers);
    set('astat-diplomas',     s.diplomas?.total);
    set('astat-checks',       s.checks);
  } catch { /* тихо */ }
}

document.getElementById('btn-load-admin-stats')?.addEventListener('click', loadAdminStats);

// ─── INIT ─────────────────────────────────────────────────────────────────────
(async () => {
  const requestedRole = PATH_ROLES[window.location.pathname]; // роль из URL, если есть

  if (!authToken) {
    // Нет токена — всегда на главную
    if (requestedRole) history.replaceState({}, '', '/');
    showScreen('auth');
    return;
  }

  try {
    const me = await apiFetch('/users/me');
    const uiRole = me.role === 'hr' ? 'employer' : me.role;
    currentUserEmail = me.email || '';
    updateHeaderEmail(uiRole);

    // Если URL запрашивает чужую роль — редиректим на свою
    if (requestedRole && requestedRole !== uiRole) {
      history.replaceState({}, '', ROLE_PATHS[uiRole] || '/');
    }

    showScreen(uiRole);

    // Восстанавливаем активный таб
    const savedTab = localStorage.getItem('active_tab');
    if (savedTab && document.getElementById(savedTab)) {
      showTab(savedTab);
    }

    if (uiRole === 'student') {
      loadStudentIdentityForm().catch(() => {});
      loadStudentProfile().catch(() => {});
    }
    if (uiRole === 'university') { loadRecords(); loadUniversityStats(); }
    if (uiRole === 'employer')   loadHistory();
    if (uiRole === 'admin')      { loadApplicationsWithCache(); loadAdminStats(); }
    startNotifyWs();
  } catch {
    authToken = null;
    localStorage.removeItem('token');
    localStorage.removeItem('active_tab');
    history.replaceState({}, '', '/');
    showScreen('auth');
  }
})();

// ─── ADMIN ────────────────────────────────────────────────────────────────────
document.getElementById('logout-admin').addEventListener('click', logout);

async function loadApplications() {
  const list = document.getElementById('applications-list');
  const status = document.getElementById('app-status-filter').value;
  try {
    const apps = await apiFetch(`/admin/university-applications?status=${status}`);
    if (!apps.length) {
      list.innerHTML = '<p class="placeholder-text">Нет заявок</p>';
      return;
    }
    list.innerHTML = apps.map(a => `
      <div class="queue-item" id="app-${a.id}">
        <div class="q-header">
          <span style="font-size:1.05rem">${a.organization_name}</span>
          ${badge(a.status === 'pending' ? 'pending' : a.status === 'approved' ? 'done' : 'error')}
        </div>
        <div style="font-size:0.9rem;color:var(--muted);margin-bottom:10px">
          ${a.email} · Подано: ${new Date(a.created_at).toLocaleDateString('ru-RU')}
        </div>
        <div style="display:flex;gap:10px;flex-wrap:wrap;margin-top:8px">
          <button class="btn-ghost" style="padding:8px 16px;font-size:0.88rem" onclick="viewApp(${a.id})">Подробнее</button>
          ${a.status === 'pending' ? `
            <button class="btn-primary" style="padding:8px 16px;font-size:0.88rem" onclick="approveApp(${a.id})">Подтвердить</button>
            <button class="btn-ghost" style="padding:8px 16px;font-size:0.88rem;color:var(--danger);border-color:var(--danger)" onclick="rejectApp(${a.id})">Отклонить</button>
          ` : a.status === 'approved'
            ? `<span style="color:var(--success);font-size:0.88rem;font-weight:600;align-self:center">Одобрено</span>`
            : `<span style="color:var(--danger);font-size:0.88rem;font-weight:600;align-self:center">Отклонено${a.review_note ? ': ' + a.review_note : ''}</span>`}
        </div>
      </div>
    `).join('');
  } catch {
    list.innerHTML = '<p class="placeholder-text">Ошибка загрузки</p>';
  }
}

// кэш заявок для просмотра деталей
let _appsCache = [];

async function loadApplicationsWithCache() {
  const list = document.getElementById('applications-list');
  const status = document.getElementById('app-status-filter').value;
  try {
    _appsCache = await apiFetch(`/admin/university-applications?status=${status}`);
    if (!_appsCache.length) {
      list.innerHTML = '<p class="placeholder-text">Нет заявок</p>';
      return;
    }
    list.innerHTML = _appsCache.map(a => `
      <div class="queue-item" id="app-${a.id}">
        <div class="q-header">
          <span style="font-size:1.05rem">${a.organization_name}</span>
          ${badge(a.status === 'pending' ? 'pending' : a.status === 'approved' ? 'done' : 'error')}
        </div>
        <div style="font-size:0.9rem;color:var(--muted);margin-bottom:10px">
          ${a.email} · Подано: ${new Date(a.created_at).toLocaleDateString('ru-RU')}
        </div>
        <div style="display:flex;gap:10px;flex-wrap:wrap;margin-top:8px">
          <button class="btn-ghost" style="padding:8px 16px;font-size:0.88rem" onclick="viewApp(${a.id})">🔍 Подробнее</button>
          ${a.status === 'pending' ? `
            <button class="btn-primary" style="padding:8px 16px;font-size:0.88rem" onclick="approveApp(${a.id})">Подтвердить</button>
            <button class="btn-ghost" style="padding:8px 16px;font-size:0.88rem;color:var(--danger);border-color:var(--danger)" onclick="rejectApp(${a.id})">Отклонить</button>
          ` : a.status === 'approved'
            ? `<span style="color:var(--success);font-size:0.88rem;font-weight:600;align-self:center">Одобрено</span>`
            : `<span style="color:var(--danger);font-size:0.88rem;font-weight:600;align-self:center">Отклонено${a.review_note ? ': ' + a.review_note : ''}</span>`}
        </div>
      </div>
    `).join('');
  } catch {
    list.innerHTML = '<p class="placeholder-text">Ошибка загрузки</p>';
  }
}

function viewApp(id) {
  const a = _appsCache.find(x => x.id === id);
  if (!a) return;

  const docs = (a.documents || []).map(d => `
    <div class="uni-doc-item">
      <span>📄 ${d.original}</span>
      <div style="display:flex;gap:8px">
        <a href="${API_BASE}/admin/university-applications/${a.id}/file?f=${encodeURIComponent(d.stored)}&inline=1&access_token=${authToken}"
           target="_blank" style="color:var(--primary);font-size:0.82rem;font-weight:600;text-decoration:none">Открыть</a>
        <a href="${API_BASE}/admin/university-applications/${a.id}/file?f=${encodeURIComponent(d.stored)}&access_token=${authToken}"
           download="${d.original}" style="color:var(--muted);font-size:0.82rem;text-decoration:none">↓</a>
      </div>
    </div>
  `).join('') || '<p style="color:var(--muted);font-size:0.88rem">Нет документов</p>';

  document.getElementById('app-detail-body').innerHTML = `
    <div class="info-card" style="max-width:100%;margin-bottom:16px">
      <div class="info-card-header">Данные организации</div>
      <div class="info-row"><span>Название</span><strong>${a.organization_name}</strong></div>
      <div class="info-row"><span>Email</span><strong>${a.email}</strong></div>
      <div class="info-row"><span>Статус</span><strong>${a.status}</strong></div>
      <div class="info-row"><span>Подано</span><strong>${new Date(a.created_at).toLocaleString('ru-RU')}</strong></div>
      ${a.notes ? `<div class="info-row"><span>Примечание</span><strong>${a.notes}</strong></div>` : ''}
      ${a.review_note ? `<div class="info-row"><span>Причина отказа</span><strong style="color:var(--danger)">${a.review_note}</strong></div>` : ''}
    </div>
    <div style="font-size:0.82rem;font-weight:700;text-transform:uppercase;letter-spacing:0.07em;color:var(--muted);margin-bottom:10px">Документы</div>
    <div style="display:flex;flex-direction:column;gap:8px">${docs}</div>
    ${a.status === 'pending' ? `
      <div style="display:flex;gap:10px;margin-top:24px">
        <button class="btn-primary" style="flex:1" onclick="approveApp(${a.id});document.getElementById('app-detail-modal').classList.add('hidden')">Подтвердить</button>
        <button class="btn-ghost" style="flex:1;color:var(--danger);border-color:var(--danger)" onclick="rejectApp(${a.id});document.getElementById('app-detail-modal').classList.add('hidden')">Отклонить</button>
      </div>` : ''}
  `;
  document.getElementById('app-detail-modal').classList.remove('hidden');
}

async function approveApp(id) {
  try {
    await apiFetch(`/admin/university-applications/${id}/approve`, { method: 'POST' });
    showToast('Заявка подтверждена', 'success');
    loadApplicationsWithCache();
  } catch (e) {
    showToast('Ошибка подтверждения: ' + (e.message || 'неизвестная ошибка'), 'error');
  }
}

async function rejectApp(id) {
  const note = prompt('Причина отклонения (необязательно):') || '';
  try {
    await apiFetch(`/admin/university-applications/${id}/reject`, {
      method: 'POST',
      body: JSON.stringify({ note }),
    });
    showToast('Заявка отклонена', 'success');
    loadApplicationsWithCache();
  } catch (e) {
    showToast('Ошибка отклонения: ' + (e.message || 'неизвестная ошибка'), 'error');
  }
}

document.getElementById('btn-load-applications').addEventListener('click', loadApplicationsWithCache);
document.getElementById('app-status-filter').addEventListener('change', loadApplicationsWithCache);

// ─── ADMIN: UNIVERSITIES LIST ─────────────────────────────────────────────────
async function loadUniversitiesList() {
  const list = document.getElementById('universities-list');
  try {
    const unis = await apiFetch('/admin/universities');
    if (!unis.length) {
      list.innerHTML = '<p class="placeholder-text">Нет зарегистрированных ВУЗов</p>';
      return;
    }
    list.innerHTML = unis.map(u => `
      <div class="queue-item" id="uni-${u.id}">
        <div class="q-header">
          <span style="font-size:1.05rem">${u.email}</span>
          ${badge(u.role === 'university' ? 'done' : 'pending')}
        </div>
        <div style="font-size:0.9rem;color:var(--muted);margin-bottom:10px">
          ID: ${u.id} · Создан: ${new Date(u.created_at).toLocaleDateString('ru-RU')}
        </div>
        <div style="display:flex;gap:10px;flex-wrap:wrap;margin-top:8px">
          <button class="btn-ghost" style="padding:8px 16px;font-size:0.88rem;color:var(--danger);border-color:var(--danger)" onclick="deleteUniversity(${u.id}, '${u.email}')">🗑️ Удалить</button>
        </div>
      </div>
    `).join('');
  } catch {
    list.innerHTML = '<p class="placeholder-text">Ошибка загрузки</p>';
  }
}

async function deleteUniversity(id, email) {
  if (!confirm(`Удалить ВУЗ "${email}"? Все связанные данные будут удалены!`)) return;
  try {
    await apiFetch(`/admin/universities/${id}`, { method: 'DELETE' });
    showToast('ВУЗ удалён', 'success');
    loadUniversitiesList();
  } catch (e) {
    showToast('Ошибка удаления: ' + (e.message || 'неизвестная ошибка'), 'error');
  }
}

document.getElementById('btn-load-universities')?.addEventListener('click', loadUniversitiesList);

// ─── CACHE CONTROL BUTTON ─────────────────────────────────────────────────────
document.getElementById('btn-clear-cache')?.addEventListener('click', () => {
  const stats = cache.getStats();
  if (confirm(`Очистить кэш?\n\nВсего записей: ${stats.total}\nСвежих: ${stats.fresh}`)) {
    cache.clear();
    showToast('Кэш очищен', 'success');
  }
});

document.getElementById('btn-cache-stats')?.addEventListener('click', () => {
  const stats = cache.getStats();
  showToast(`Кэш: ${stats.fresh} свежих из ${stats.total} записей`, 'info');
});

// ─── WEBSOCKET NOTIFY (push-уведомления) ─────────────────────────────────────
let _notifyWs = null;

function startNotifyWs() {
  if (_notifyWs || !authToken) return;
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const ws = new WebSocket(`${proto}//${window.location.host}${API_BASE}/ws/notify?access_token=${authToken}`);
  _notifyWs = ws;

  ws.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data);
      const toastMap = {
        job_done:     ['success', 'Задача завершена'],
        app_approved: ['success', 'Заявка одобрена'],
        app_rejected: ['warning', 'Заявка отклонена'],
        reload:       ['info',    'Данные обновлены'],
      };
      const toastEntry = toastMap[msg.type];
      if (toastEntry) showToast(toastEntry[1], toastEntry[0]);

      if (msg.type === 'job_done') {
        cache.clear('records');
        cache.clear('queue');
        loadRecords(recordsPage, recordsLimit).catch(() => {});
        loadQueue().catch(() => {});
      } else if (msg.type === 'reload' || msg.type === 'app_approved' || msg.type === 'app_rejected') {
        const screen = document.querySelector('.screen.active');
        const id = screen?.id?.replace('screen-', '');
        if (id === 'university') { cache.clear('records'); loadRecords().catch(() => {}); loadQueue().catch(() => {}); }
        if (id === 'student')    { cache.clear('profile'); loadStudentProfile().catch(() => {}); }
        if (id === 'employer')   { cache.clear('history'); loadHistory().catch(() => {}); }
        if (id === 'admin')      loadApplicationsWithCache().catch(() => {});
      }
    } catch { /* ignore */ }
  };

  ws.onclose = () => {
    _notifyWs = null;
    if (authToken) setTimeout(startNotifyWs, 3000);
  };
  ws.onerror = () => ws.close();
}

// ─── WEBSOCKET JOB WATCHER ────────────────────────────────────────────────────
function watchJobWS(jobId, retries = 3) {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${window.location.host}${API_BASE}/ws/jobs/${jobId}?access_token=${authToken}`;
  const ws = new WebSocket(wsUrl);
  const statusEl = document.getElementById('upload-status');

  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data);
    if (msg.error) { ws.close(); return; }

    if (msg.status === 'processing') {
      statusEl.className = 'status-bar info';
      statusEl.textContent = `Обрабатывается... ${msg.progress}%`;
    }

    if (msg.status === 'done') {
      statusEl.className = 'status-bar success';
      statusEl.textContent = `Обработка завершена. ${msg.summary || ''}`;
      loadRecords();
      loadQueue();
      ws.close();
    }

    if (msg.status === 'failed') {
      statusEl.className = 'status-bar error';
      statusEl.textContent = `Ошибка обработки: ${msg.summary || ''}`;
      ws.close();
    }
  };

  ws.onerror = () => {
    ws.close();
    // Переподключение при обрыве соединения
    if (retries > 0) {
      console.log(`WebSocket error, retrying... (${retries} attempts left)`);
      setTimeout(() => watchJobWS(jobId, retries - 1), 2000);
    }
  };
}

// ─── SMOOTH SCROLL ────────────────────────────────────────────────────────────
document.getElementById('btn-scroll-login')?.addEventListener('click', (e) => {
  e.preventDefault();
  document.getElementById('auth-panel').scrollIntoView({ behavior: 'smooth', block: 'start' });
});

// ─── LANDING SCROLL ANIMATIONS ────────────────────────────────────────────────
// ─── FEATURE EXPAND ON CLICK ──────────────────────────────────────────────────
document.querySelectorAll('.landing-feature-item').forEach(item => {
  item.addEventListener('click', () => {
    const isOpen = item.classList.contains('expanded');
    document.querySelectorAll('.landing-feature-item').forEach(el => el.classList.remove('expanded'));
    if (!isOpen) item.classList.add('expanded');
  });
});

// ─── LANDING SCROLL ANIMATIONS ────────────────────────────────────────────────
const featureObserver = new IntersectionObserver((entries) => {
  entries.forEach((entry, i) => {
    if (entry.isIntersecting) {
      setTimeout(() => entry.target.classList.add('visible'), i * 150);
      featureObserver.unobserve(entry.target);
    }
  });
}, { threshold: 0.15 });

document.querySelectorAll('.landing-feature-item').forEach(el => {
  featureObserver.observe(el);
});
