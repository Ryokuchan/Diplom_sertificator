// ─── CONFIG ───────────────────────────────────────────────────────────────────
const API_BASE = 'http://localhost:8080/api/v1';
/** Origin сайта (без /api/v1) — для подсказок; ссылки share приходят с сервера в поле view_url */
const SITE_ORIGIN = API_BASE.replace(/\/api\/v1\/?$/, '');

// ─── STATE ────────────────────────────────────────────────────────────────────
let currentRole = 'student';
let authToken = localStorage.getItem('token') || null;
let authRegisterMode = false;

// ─── HELPERS ──────────────────────────────────────────────────────────────────
function showScreen(id) {
  document.querySelectorAll('.screen').forEach(s => s.classList.remove('active'));
  document.getElementById('screen-' + id).classList.add('active');
}

function showTab(tabId) {
  const screen = document.querySelector('.screen.active');
  if (!screen) return;
  screen.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
  screen.querySelectorAll('.sidebar-item').forEach(i => i.classList.remove('active'));
  document.getElementById(tabId)?.classList.add('active');
  screen.querySelector(`[data-tab="${tabId}"]`)?.classList.add('active');
  if (tabId === 'student-profile') {
    loadStudentIdentityForm().catch(() => {});
    loadStudentProfile().catch(() => {});
  }
  if (tabId === 'student-qr') {
    refreshStudentDiplomaQR(false).catch(() => {});
  }
}

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
  const isAdmin = currentRole === 'admin';
  const isUniReg = authRegisterMode && currentRole === 'university';

  // заголовок и подпись
  document.getElementById('auth-panel-title').textContent =
    authRegisterMode ? 'Регистрация' : isAdmin ? 'Вход администратора' : 'Вход в систему';
  document.getElementById('auth-panel-sub').textContent =
    isAdmin ? 'Вход для модерации заявок ВУЗов'
    : authRegisterMode ? 'Создайте учётную запись'
    : 'Выберите роль и введите учётные данные';

  // кнопка submit
  document.getElementById('auth-submit-title').textContent =
    authRegisterMode ? 'Зарегистрироваться' : 'Войти';

  // ссылки вход/регистрация
  document.getElementById('link-show-login')?.classList.toggle('auth-link-active', !authRegisterMode);
  document.getElementById('link-show-register')?.classList.toggle('auth-link-active', authRegisterMode);

  // скрываем ссылки для админа
  const modeLinks = document.querySelector('.auth-mode-links');
  if (modeLinks) modeLinks.style.display = isAdmin ? 'none' : '';

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
    if (currentRole === 'admin') authRegisterMode = false;
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

function enterCabinet(data) {
  authToken = data.token;
  localStorage.setItem('token', authToken);
  const uiRole = data.role === 'hr' ? 'employer' : data.role;
  showScreen(uiRole);
  if (uiRole === 'student') {
    loadStudentIdentityForm().catch(() => {});
    loadStudentProfile().catch(() => {});
    if (document.getElementById('screen-student')?.classList.contains('active')) {
      const activeTab = document.querySelector('#screen-student .tab.active');
      if (activeTab?.id === 'student-qr') refreshStudentDiplomaQR(false).catch(() => {});
    }
  }
  if (uiRole === 'university') loadRecords();
  if (uiRole === 'employer')   loadHistory();
  if (uiRole === 'admin')      loadApplicationsWithCache();
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
    statusEl.textContent = '✅ Заявка отправлена! Ожидайте подтверждения администратора.';
    document.getElementById('uni-apply-form-inline').reset();
  } catch {
    statusEl.className = 'status-bar error';
    statusEl.textContent = '❌ Не удалось подключиться к серверу';
  }
});

updateAuthFormUI();

function logout() {
  authToken = null;
  localStorage.removeItem('token');
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
    const d = await apiFetch('/student/profile');
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
    // GET /api/student/documents  →  [{ name, status }]
    const docs = await apiFetch('/student/documents');
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
      statusEl.textContent = '✅ ' + (res.detail || 'Данные совпали с реестром, диплом привязан.');
    } else {
      statusEl.className = 'status-bar info';
      statusEl.textContent =
        'Сохранено. Автосверка не выполнена: нет совпадения с «свободной» записью реестра (проверьте Excel, номер и ФИО) или укажите верный ID строки из кабинета вуза.';
    }
    await loadStudentProfile().catch(() => {});
  } catch (err) {
    statusEl.className = 'status-bar error';
    statusEl.textContent = '❌ ' + (err.message || 'Ошибка');
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
  hint.textContent = '⏳ Загружаем список дипломов и создаём ссылку…';

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
    hint.textContent = '❌ ' + (e.message || 'Не удалось создать ссылку');
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
    const h = document.getElementById('student-qr-hint');
    if (h) {
      const t = h.textContent;
      h.textContent = '✅ Ссылка скопирована в буфер обмена';
      setTimeout(() => {
        h.textContent = t;
      }, 2000);
    }
  } catch {
    alert(lastStudentShareViewUrl);
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
      if (err && err.name !== 'AbortError') alert('Не удалось открыть меню «Поделиться»');
    }
  } else {
    try {
      await navigator.clipboard.writeText(lastStudentShareViewUrl);
      alert('Ссылка скопирована (нет встроенного «Поделиться» в этом браузере).');
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
    // GET /api/verify/:id  →  { valid, name, university, year }
    const data = await apiFetch(`/verify/${encodeURIComponent(id)}`);
    if (data.valid) {
      result.className = 'verify-result valid';
      result.innerHTML = `✅ Диплом действителен<br><small><b>${data.name}</b> · ${data.specialty}<br>${data.university} · ${data.year}</small>`;
    } else {
      result.className = 'verify-result invalid';
      result.textContent = '❌ Диплом не найден или недействителен';
    }
  } catch {
    result.className = 'verify-result invalid';
    result.textContent = '❌ Ошибка при проверке';
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
    // POST /api/university/upload  →  { jobId, message }
    const res = await fetch(`${API_BASE}/university/upload`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${authToken}` },
      body: formData,
    });
    if (!res.ok) throw new Error();
    const data = await res.json();
    statusEl.className = 'status-bar success';
    statusEl.textContent = `✅ Файл принят. ID задачи: ${data.job_id}`;
    document.getElementById('upload-preview').classList.add('hidden');
    selectedFile = null;
    fileInput.value = '';
    // подключаемся по WS и обновляем данные когда job завершится
    watchJobWS(data.job_id);
  } catch {
    statusEl.className = 'status-bar error';
    statusEl.textContent = '❌ Ошибка загрузки. Попробуйте снова.';
  }
});

async function loadRecords() {
  const tbody = document.getElementById('records-tbody');
  try {
    const records = await apiFetch('/university/records');
    if (!records.length) {
      tbody.innerHTML = '<tr><td colspan="6" class="placeholder-text">Нет данных</td></tr>';
      return;
    }
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
  } catch {
    tbody.innerHTML = '<tr><td colspan="6" class="placeholder-text">Ошибка загрузки</td></tr>';
  }
}

async function loadQueue() {
  const list = document.getElementById('queue-list');
  try {
    // GET /api/university/queue  →  [{ jobId, filename, status, progress, error }]
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

document.getElementById('records-search').addEventListener('input', function () {
  const q = this.value.toLowerCase();
  document.querySelectorAll('#records-tbody tr').forEach(row => {
    row.style.display = row.textContent.toLowerCase().includes(q) ? '' : 'none';
  });
});

// ─── EMPLOYER ─────────────────────────────────────────────────────────────────
document.getElementById('btn-check-id').addEventListener('click', async () => {
  const id     = document.getElementById('emp-diploma-id').value.trim();
  const result = document.getElementById('emp-result');
  if (!id) return;

  result.className = 'verify-result loading';
  result.textContent = '⏳ Проверяем...';
  result.classList.remove('hidden');

  try {
    // GET /api/verify/:id  →  { valid, name, university, specialty, year }
    const data = await apiFetch(`/verify/${encodeURIComponent(id)}`);
    if (data.valid) {
      result.className = 'verify-result valid';
      result.innerHTML = `
        ✅ Диплом действителен<br>
        <small>
          <b>${data.name}</b> · ${data.specialty}<br>
          ${data.university} · ${data.year}
        </small>
      `;
    } else {
      result.className = 'verify-result invalid';
      result.textContent = '❌ Диплом не найден или недействителен';
    }
  } catch {
    result.className = 'verify-result invalid';
    result.textContent = '❌ Ошибка при проверке';
  }
});

// QR — заглушка, реальный скан подключит бек/девопс
document.getElementById('btn-scan-qr').addEventListener('click', () => {
  alert('QR-сканирование будет доступно после подключения камеры (реализация на стороне бека/девопса)');
});

async function loadHistory() {
  const tbody = document.getElementById('history-tbody');
  try {
    // GET /api/employer/history  →  [{ date, diplomaId, name, result }]
    const history = await apiFetch('/employer/history');
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

// ─── INIT ─────────────────────────────────────────────────────────────────────
showScreen('auth');

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
          <button class="btn-ghost" style="padding:8px 16px;font-size:0.88rem" onclick="viewApp(${a.id})">🔍 Подробнее</button>
          ${a.status === 'pending' ? `
            <button class="btn-primary" style="padding:8px 16px;font-size:0.88rem" onclick="approveApp(${a.id})">✅ Подтвердить</button>
            <button class="btn-ghost" style="padding:8px 16px;font-size:0.88rem;color:var(--danger);border-color:var(--danger)" onclick="rejectApp(${a.id})">❌ Отклонить</button>
          ` : a.status === 'approved'
            ? `<span style="color:var(--success);font-size:0.88rem;font-weight:600;align-self:center">✅ Одобрено</span>`
            : `<span style="color:var(--danger);font-size:0.88rem;font-weight:600;align-self:center">❌ Отклонено${a.review_note ? ': ' + a.review_note : ''}</span>`}
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
            <button class="btn-primary" style="padding:8px 16px;font-size:0.88rem" onclick="approveApp(${a.id})">✅ Подтвердить</button>
            <button class="btn-ghost" style="padding:8px 16px;font-size:0.88rem;color:var(--danger);border-color:var(--danger)" onclick="rejectApp(${a.id})">❌ Отклонить</button>
          ` : a.status === 'approved'
            ? `<span style="color:var(--success);font-size:0.88rem;font-weight:600;align-self:center">✅ Одобрено</span>`
            : `<span style="color:var(--danger);font-size:0.88rem;font-weight:600;align-self:center">❌ Отклонено${a.review_note ? ': ' + a.review_note : ''}</span>`}
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
        <button class="btn-primary" style="flex:1" onclick="approveApp(${a.id});document.getElementById('app-detail-modal').classList.add('hidden')">✅ Подтвердить</button>
        <button class="btn-ghost" style="flex:1;color:var(--danger);border-color:var(--danger)" onclick="rejectApp(${a.id});document.getElementById('app-detail-modal').classList.add('hidden')">❌ Отклонить</button>
      </div>` : ''}
  `;
  document.getElementById('app-detail-modal').classList.remove('hidden');
}

async function approveApp(id) {
  try {
    await apiFetch(`/admin/university-applications/${id}/approve`, { method: 'POST' });
    loadApplicationsWithCache();
  } catch (e) {
    alert('Ошибка подтверждения');
  }
}

async function rejectApp(id) {
  const note = prompt('Причина отклонения (необязательно):') || '';
  try {
    await apiFetch(`/admin/university-applications/${id}/reject`, {
      method: 'POST',
      body: JSON.stringify({ note }),
    });
    loadApplicationsWithCache();
  } catch {
    alert('Ошибка отклонения');
  }
}

document.getElementById('btn-load-applications').addEventListener('click', loadApplicationsWithCache);
document.getElementById('app-status-filter').addEventListener('change', loadApplicationsWithCache);

// ─── WEBSOCKET JOB WATCHER ────────────────────────────────────────────────────
function watchJobWS(jobId) {
  const wsUrl = `ws://localhost:8080/api/v1/ws/jobs/${jobId}?access_token=${authToken}`;
  const ws = new WebSocket(wsUrl);
  const statusEl = document.getElementById('upload-status');

  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data);
    if (msg.error) { ws.close(); return; }

    if (msg.status === 'processing') {
      statusEl.className = 'status-bar info';
      statusEl.textContent = `⏳ Обрабатывается... ${msg.progress}%`;
    }

    if (msg.status === 'done') {
      statusEl.className = 'status-bar success';
      statusEl.textContent = `✅ Обработка завершена. ${msg.summary || ''}`;
      loadRecords();
      loadQueue();
      ws.close();
    }

    if (msg.status === 'failed') {
      statusEl.className = 'status-bar error';
      statusEl.textContent = `❌ Ошибка обработки: ${msg.summary || ''}`;
      ws.close();
    }
  };

  ws.onerror = () => {
    // WS недоступен — тихо игнорируем, данные можно обновить вручную
    ws.close();
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
