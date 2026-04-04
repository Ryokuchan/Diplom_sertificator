/**
 * DiplomaVerify — фронтенд, привязанный к API бэкенда (/api/v1).
 * Базовый URL задаётся на <body data-api-base="..."> (по умолчанию /api/v1).
 */

const API_BASE = (document.body.getAttribute('data-api-base') || '/api/v1').replace(/\/$/, '');
const LS_TOKEN = 'token';
const LS_REFRESH = 'refresh_token';
const MAX_UPLOAD_BYTES = 32 * 1024 * 1024;

let currentRole = 'student';
let authToken = localStorage.getItem(LS_TOKEN) || null;
let authRegisterMode = false;
let activeJobWs = null;
let qrAnimFrame = null;
let qrStream = null;

// ─── Роли: UI «employer» ↔ БД «hr» ───────────────────────────────────────────
function uiRoleToBackend(role) {
  if (role === 'employer') return 'hr';
  return role;
}

function backendRoleToUI(role) {
  if (role === 'hr') return 'employer';
  return role;
}

function syncRoleTabs(uiRole) {
  currentRole = uiRole;
  document.querySelectorAll('.role-tab').forEach((b) => {
    b.classList.toggle('active', b.dataset.role === uiRole);
  });
}

function escapeHtml(s) {
  if (s == null || s === '') return '';
  const d = document.createElement('div');
  d.textContent = String(s);
  return d.innerHTML;
}

/** Из полного URL вида .../api/v1/verify/XXXX или из сырой строки — идентификатор для API. */
function extractVerifyId(raw) {
  const s = String(raw || '').trim();
  if (!s) return '';
  const m = s.match(/\/verify\/([^/?#]+)/i);
  if (m) return decodeURIComponent(m[1]);
  return s;
}

function formatDateRu(iso) {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' });
}

function yearCell(y) {
  return y != null && y !== '' && Number(y) !== 0 ? escapeHtml(y) : '—';
}

// ─── Экраны / табы ────────────────────────────────────────────────────────────
function showScreen(id) {
  document.querySelectorAll('.screen').forEach((s) => s.classList.remove('active'));
  const el = document.getElementById('screen-' + id);
  if (el) el.classList.add('active');
}

function showTab(tabId) {
  const screen = document.querySelector('.screen.active');
  if (!screen) return;
  screen.querySelectorAll('.tab').forEach((t) => t.classList.remove('active'));
  screen.querySelectorAll('.sidebar-item').forEach((i) => i.classList.remove('active'));
  const tab = document.getElementById(tabId);
  if (tab) tab.classList.add('active');
  screen.querySelector(`[data-tab="${tabId}"]`)?.classList.add('active');
}

window.showScreen = showScreen;

function badge(status) {
  const map = {
    valid: ['badge-success', 'Валиден'],
    invalid: ['badge-danger', 'Недействителен'],
    pending: ['badge-warning', 'В очереди'],
    processing: ['badge-info', 'Обрабатывается'],
    error: ['badge-danger', 'Ошибка'],
    done: ['badge-success', 'Готово'],
    verified: ['badge-success', 'Подтверждён'],
    revoked: ['badge-danger', 'Аннулирован'],
  };
  const [cls, label] = map[status] || ['badge-info', status];
  return `<span class="badge ${cls}">${escapeHtml(label)}</span>`;
}

// ─── HTTP ───────────────────────────────────────────────────────────────────
async function apiFetch(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  const body = options.body;
  if (body != null && typeof body === 'string' && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json';
  }
  if (authToken) headers.Authorization = `Bearer ${authToken}`;
  const res = await fetch(API_BASE + path, { ...options, headers });
  if (!res.ok) {
    let msg = `Ошибка ${res.status}`;
    try {
      const err = await res.json();
      if (err.error) msg = typeof err.error === 'string' ? err.error : JSON.stringify(err.error);
    } catch { /* ignore */ }
    throw new Error(msg);
  }
  const ct = res.headers.get('content-type') || '';
  if (ct.includes('application/json')) return res.json();
  return res.text();
}

/** Публичная проверка — без Authorization (гости и лендинг). */
async function verifyPublicById(rawId) {
  const id = extractVerifyId(rawId);
  if (!id) throw new Error('Пустой идентификатор');
  const res = await fetch(`${API_BASE}/verify/${encodeURIComponent(id)}`);
  if (!res.ok) {
    let msg = `Ошибка ${res.status}`;
    try {
      const err = await res.json();
      if (err.error) msg = typeof err.error === 'string' ? err.error : JSON.stringify(err.error);
    } catch { /* ignore */ }
    throw new Error(msg);
  }
  return res.json();
}

/** Проверка из кабинета: с токеном (работодатель — запись в историю на бэке). */
async function verifyWithSession(rawId) {
  const id = extractVerifyId(rawId);
  if (!id) throw new Error('Пустой идентификатор');
  return apiFetch(`/verify/${encodeURIComponent(id)}`, { method: 'GET' });
}

function renderVerifyOk(data) {
  const name = escapeHtml(data.name || '—');
  const uni = escapeHtml(data.university || '—');
  const spec = escapeHtml(data.specialty || '');
  const year = escapeHtml(data.year || '—');
  const extra = spec ? `<br><small>${spec}</small>` : '';
  return `✅ Диплом действителен<br><small>${name} · ${uni} · ${year}</small>${extra}`;
}

// ─── Сессия ─────────────────────────────────────────────────────────────────
async function tryRestoreSession() {
  if (!authToken) return false;
  try {
    const me = await apiFetch('/users/me');
    const uiRole = backendRoleToUI(me.role) || 'student';
    syncRoleTabs(uiRole);
    showScreen(uiRole);
    if (uiRole === 'student') await loadStudentProfile().catch(() => {});
    if (uiRole === 'university') await loadRecords().catch(() => {});
    if (uiRole === 'employer') await loadHistory().catch(() => {});
    return true;
  } catch {
    logout();
    return false;
  }
}

function logout() {
  stopJobWatch();
  authToken = null;
  localStorage.removeItem(LS_TOKEN);
  localStorage.removeItem(LS_REFRESH);
  showScreen('auth');
}

// ─── Auth UI ─────────────────────────────────────────────────────────────────
function updateAuthFormUI() {
  const btn = document.getElementById('auth-submit');
  const title = document.getElementById('auth-submit-title');
  const panelTitle = document.getElementById('auth-panel-title');
  const panelSub = document.getElementById('auth-panel-sub');
  if (authRegisterMode) {
    if (title) title.textContent = 'Зарегистрироваться';
    if (btn) btn.setAttribute('aria-label', 'Зарегистрироваться');
    if (panelTitle) panelTitle.textContent = 'Регистрация';
    if (panelSub) panelSub.textContent = 'Создайте учётную запись для выбранной роли';
  } else {
    if (title) title.textContent = 'Войти';
    if (btn) btn.setAttribute('aria-label', 'Войти');
    if (panelTitle) panelTitle.textContent = 'Вход в систему';
    if (panelSub) panelSub.textContent = 'Выберите роль и введите учётные данные';
  }
  document.getElementById('link-show-login')?.classList.toggle('auth-link-active', !authRegisterMode);
  document.getElementById('link-show-register')?.classList.toggle('auth-link-active', authRegisterMode);
}

document.querySelectorAll('.role-tab').forEach((btn) => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.role-tab').forEach((b) => b.classList.remove('active'));
    btn.classList.add('active');
    currentRole = btn.dataset.role;
  });
});

document.getElementById('link-show-login')?.addEventListener('click', (e) => {
  e.preventDefault();
  authRegisterMode = false;
  updateAuthFormUI();
});
document.getElementById('link-show-register')?.addEventListener('click', (e) => {
  e.preventDefault();
  authRegisterMode = true;
  updateAuthFormUI();
});

function enterCabinetAfterAuth(data) {
  authToken = data.token;
  localStorage.setItem(LS_TOKEN, authToken);
  if (data.refresh_token) localStorage.setItem(LS_REFRESH, data.refresh_token);
  const uiRole = backendRoleToUI(data.role) || currentRole;
  syncRoleTabs(uiRole);
  showScreen(uiRole);
  if (uiRole === 'student') loadStudentProfile();
  if (uiRole === 'university') loadRecords();
  if (uiRole === 'employer') loadHistory();
}

document.getElementById('auth-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const login = document.getElementById('auth-login').value.trim();
  const password = document.getElementById('auth-password').value;
  const errEl = document.getElementById('auth-error');
  errEl.classList.add('hidden');

  try {
    if (authRegisterMode) {
      const data = await apiFetch('/auth/register', {
        method: 'POST',
        body: JSON.stringify({
          email: login,
          password,
          role: uiRoleToBackend(currentRole),
        }),
      });
      enterCabinetAfterAuth(data);
      return;
    }
    const data = await apiFetch('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email: login, password }),
    });
    enterCabinetAfterAuth(data);
  } catch (err) {
    errEl.textContent = err.message || 'Неверный логин или пароль';
    errEl.classList.remove('hidden');
  }
});

updateAuthFormUI();

['student', 'university', 'employer'].forEach((role) => {
  document.getElementById(`logout-${role}`).addEventListener('click', logout);
});

document.querySelectorAll('.sidebar-item').forEach((item) => {
  item.addEventListener('click', () => showTab(item.dataset.tab));
});

// ─── Лендинг: публичная проверка ─────────────────────────────────────────────
document.getElementById('landing-verify-input')?.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    e.preventDefault();
    document.getElementById('btn-landing-verify')?.click();
  }
});

document.getElementById('btn-landing-verify')?.addEventListener('click', async () => {
  const input = document.getElementById('landing-verify-input');
  const out = document.getElementById('landing-verify-result');
  const raw = input.value.trim();
  if (!raw) return;
  out.classList.remove('hidden', 'valid', 'invalid');
  out.textContent = '⏳ Проверяем…';
  try {
    const data = await verifyPublicById(raw);
    if (data.valid) {
      out.classList.add('valid');
      out.innerHTML = renderVerifyOk(data);
    } else {
      out.classList.add('invalid');
      out.textContent = '❌ Диплом не найден или недействителен';
    }
  } catch (err) {
    out.classList.add('invalid');
    out.textContent = '❌ ' + (err.message || 'Ошибка запроса');
  }
});

// ─── Студент ─────────────────────────────────────────────────────────────────
async function loadStudentProfile() {
  try {
    const d = await apiFetch('/student/profile');
    document.getElementById('s-name').textContent = d.name || '—';
    document.getElementById('s-spec').textContent = d.specialty || '—';
    document.getElementById('s-year').textContent =
      d.year != null && d.year !== '' && Number(d.year) !== 0 ? String(d.year) : '—';
    document.getElementById('s-diploma-num').textContent = d.diplomaNumber || '—';
    document.getElementById('s-university').textContent = d.university || '—';
  } catch {
    console.warn('Не удалось загрузить профиль студента');
  }
}

async function loadStudentDocs() {
  const list = document.getElementById('docs-list');
  try {
    const docs = await apiFetch('/student/documents');
    if (!docs.length) {
      list.innerHTML = '<p class="placeholder-text">Нет загруженных документов</p>';
      return;
    }
    list.innerHTML = docs
      .map(
        (d) => `
      <div class="doc-item">
        <span>📄 ${escapeHtml(d.name)}</span>
        ${badge(d.status)}
      </div>
    `,
      )
      .join('');
  } catch {
    list.innerHTML = '<p class="placeholder-text">Ошибка загрузки</p>';
  }
}

document.getElementById('btn-load-profile').addEventListener('click', loadStudentProfile);
document.getElementById('btn-load-docs').addEventListener('click', loadStudentDocs);

document.getElementById('s-verify-id')?.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') document.getElementById('btn-student-verify')?.click();
});

document.getElementById('btn-student-verify').addEventListener('click', async () => {
  const raw = document.getElementById('s-verify-id').value;
  const id = extractVerifyId(raw);
  const result = document.getElementById('s-verify-result');
  if (!id) return;

  result.className = 'verify-result loading';
  result.textContent = '⏳ Проверяем...';
  result.classList.remove('hidden');

  try {
    const data = await verifyPublicById(raw);
    if (data.valid) {
      result.className = 'verify-result valid';
      result.innerHTML = renderVerifyOk(data);
    } else {
      result.className = 'verify-result invalid';
      result.textContent = '❌ Диплом не найден или недействителен';
    }
  } catch {
    result.className = 'verify-result invalid';
    result.textContent = '❌ Ошибка при проверке';
  }
});

// ─── ВУЗ: загрузка + WebSocket статуса ───────────────────────────────────────
function stopJobWatch() {
  if (activeJobWs) {
    try {
      activeJobWs.close();
    } catch { /* ignore */ }
    activeJobWs = null;
  }
}

function startJobWatch(jobId, statusEl) {
  stopJobWatch();
  if (!authToken || !jobId) return;
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${proto}//${window.location.host}${API_BASE}/ws/jobs/${encodeURIComponent(jobId)}?access_token=${encodeURIComponent(authToken)}`;
  const ws = new WebSocket(url);
  activeJobWs = ws;

  ws.onmessage = (ev) => {
    try {
      const msg = JSON.parse(ev.data);
      if (msg.error) {
        statusEl.className = 'status-bar error';
        statusEl.textContent = 'Ошибка: ' + msg.error;
        stopJobWatch();
        return;
      }
      const pct = msg.progress ?? 0;
      statusEl.className = 'status-bar info';
      statusEl.textContent = `Задача ${msg.job_id}: ${msg.status} (${pct}%)${msg.summary ? ' — ' + msg.summary : ''}`;
      if (msg.status === 'done') {
        statusEl.className = 'status-bar success';
        statusEl.textContent = `✅ Обработка завершена. ${msg.summary || ''}`;
        stopJobWatch();
        loadQueue().catch(() => {});
      }
      if (msg.status === 'failed') {
        statusEl.className = 'status-bar error';
        statusEl.textContent = '❌ Ошибка: ' + (msg.summary || 'сбой');
        stopJobWatch();
      }
    } catch { /* ignore */ }
  };
}

const dropzone = document.getElementById('dropzone');
const fileInput = document.getElementById('file-input');
let selectedFile = null;

function selectFile(file) {
  if (!file) return;
  selectedFile = file;
  document.getElementById('upload-filename').textContent = `📄 ${file.name}`;
  document.getElementById('upload-preview').classList.remove('hidden');
}

dropzone.addEventListener('dragover', (e) => {
  e.preventDefault();
  dropzone.classList.add('drag-over');
});
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
  stopJobWatch();
});

document.getElementById('btn-upload-send').addEventListener('click', async () => {
  if (!selectedFile) return;
  if (selectedFile.size > MAX_UPLOAD_BYTES) {
    const statusEl = document.getElementById('upload-status');
    statusEl.className = 'status-bar error';
    statusEl.textContent = `❌ Файл больше ${MAX_UPLOAD_BYTES / (1024 * 1024)} МБ`;
    statusEl.classList.remove('hidden');
    return;
  }

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
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      const msg = data.error || res.statusText || 'Ошибка загрузки';
      throw new Error(typeof msg === 'string' ? msg : JSON.stringify(msg));
    }
    statusEl.className = 'status-bar success';
    statusEl.textContent = `✅ Файл принят. ID задачи: ${data.job_id}`;
    document.getElementById('upload-preview').classList.add('hidden');
    selectedFile = null;
    fileInput.value = '';
    startJobWatch(data.job_id, statusEl);
    showTab('uni-queue');
    loadQueue().catch(() => {});
  } catch (err) {
    statusEl.className = 'status-bar error';
    statusEl.textContent = '❌ ' + (err.message || 'Ошибка загрузки');
  }
});

async function loadRecords() {
  const tbody = document.getElementById('records-tbody');
  try {
    const records = await apiFetch('/university/records');
    if (!records.length) {
      tbody.innerHTML = '<tr><td colspan="5" class="placeholder-text">Нет данных</td></tr>';
      return;
    }
    tbody.innerHTML = records
      .map(
        (r) => `
      <tr>
        <td>${escapeHtml(r.name)}</td>
        <td>${escapeHtml(r.specialty)}</td>
        <td>${yearCell(r.year)}</td>
        <td>${escapeHtml(r.diplomaNumber)}</td>
        <td>${badge(r.status)}</td>
      </tr>
    `,
      )
      .join('');
  } catch {
    tbody.innerHTML = '<tr><td colspan="5" class="placeholder-text">Ошибка загрузки</td></tr>';
  }
}

async function loadQueue() {
  const list = document.getElementById('queue-list');
  try {
    const jobs = await apiFetch('/university/queue');
    if (!jobs.length) {
      list.innerHTML = '<p class="placeholder-text">Нет задач</p>';
      return;
    }
    list.innerHTML = jobs
      .map((j) => {
        const jid = escapeHtml(j.job_id || j.jobId || '');
        const fn = escapeHtml(j.filename || '');
        return `
      <div class="queue-item" data-job-id="${jid}">
        <div class="q-header">
          <span>📁 ${fn}</span>
          ${badge(j.status)}
        </div>
        <small class="queue-job-id">ID: ${jid || '—'}</small>
        ${j.status === 'processing' ? `<div class="progress-bar"><div class="progress-fill" style="width:${j.progress || 0}%"></div></div>` : ''}
        ${j.error ? `<small style="color:var(--danger)">${escapeHtml(j.error)}</small>` : ''}
        <div class="queue-actions">
          <button type="button" class="btn-secondary btn-job-report" data-job-id="${jid}">Отчёт</button>
        </div>
      </div>
    `;
      })
      .join('');

    list.querySelectorAll('.btn-job-report').forEach((btn) => {
      btn.addEventListener('click', () => openJobReport(btn.getAttribute('data-job-id')));
    });
  } catch {
    list.innerHTML = '<p class="placeholder-text">Ошибка загрузки</p>';
  }
}

document.getElementById('btn-load-records').addEventListener('click', loadRecords);
document.getElementById('btn-load-queue').addEventListener('click', loadQueue);

document.getElementById('records-search').addEventListener('input', function () {
  const q = this.value.toLowerCase();
  document.querySelectorAll('#records-tbody tr').forEach((row) => {
    row.style.display = row.textContent.toLowerCase().includes(q) ? '' : 'none';
  });
});

// ─── Отчёт по job (модалка) ──────────────────────────────────────────────────
function closeJobReportModal() {
  const modal = document.getElementById('job-report-modal');
  modal.classList.add('hidden');
  modal.setAttribute('aria-hidden', 'true');
}

async function openJobReport(jobId) {
  if (!jobId) return;
  const modal = document.getElementById('job-report-modal');
  const body = document.getElementById('job-report-body');
  modal.classList.remove('hidden');
  modal.setAttribute('aria-hidden', 'false');
  body.textContent = 'Загрузка…';
  try {
    const report = await apiFetch(`/university/jobs/${encodeURIComponent(jobId)}/report`);
    body.textContent = JSON.stringify(report, null, 2);
  } catch (e) {
    body.textContent = e.message || 'Ошибка';
  }
}

document.getElementById('job-report-close')?.addEventListener('click', closeJobReportModal);
document.getElementById('job-report-modal')?.addEventListener('click', (e) => {
  if (e.target.id === 'job-report-modal') closeJobReportModal();
});

// ─── Работодатель ────────────────────────────────────────────────────────────
document.getElementById('emp-diploma-id')?.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') document.getElementById('btn-check-id')?.click();
});

document.getElementById('btn-check-id').addEventListener('click', async () => {
  const raw = document.getElementById('emp-diploma-id').value;
  const id = extractVerifyId(raw);
  const result = document.getElementById('emp-result');
  if (!id) return;

  result.className = 'verify-result loading';
  result.textContent = '⏳ Проверяем...';
  result.classList.remove('hidden');

  try {
    const data = await verifyWithSession(raw);
    if (data.valid) {
      result.className = 'verify-result valid';
      const name = escapeHtml(data.name || '—');
      const spec = escapeHtml(data.specialty || '—');
      const uni = escapeHtml(data.university || '—');
      const year = escapeHtml(data.year || '—');
      result.innerHTML = `
        ✅ Диплом действителен<br>
        <small>
          <b>${name}</b> · ${spec}<br>
          ${uni} · ${year}
        </small>`;
    } else {
      result.className = 'verify-result invalid';
      result.textContent = '❌ Диплом не найден или недействителен';
    }
  } catch {
    result.className = 'verify-result invalid';
    result.textContent = '❌ Ошибка при проверке';
  }
});

function closeQrModal() {
  const modal = document.getElementById('qr-modal');
  modal.classList.add('hidden');
  modal.setAttribute('aria-hidden', 'true');
  if (qrAnimFrame) {
    cancelAnimationFrame(qrAnimFrame);
    qrAnimFrame = null;
  }
  const video = document.getElementById('qr-video');
  if (video && video.srcObject) {
    video.srcObject.getTracks().forEach((t) => t.stop());
    video.srcObject = null;
  }
  qrStream = null;
}

document.getElementById('qr-modal-cancel')?.addEventListener('click', closeQrModal);
document.getElementById('qr-modal')?.addEventListener('click', (e) => {
  if (e.target.id === 'qr-modal') closeQrModal();
});

document.getElementById('btn-scan-qr').addEventListener('click', async () => {
  const modal = document.getElementById('qr-modal');
  const video = document.getElementById('qr-video');
  const status = document.getElementById('qr-modal-status');
  status.textContent = '';

  if (!('BarcodeDetector' in window)) {
    status.textContent =
      'В этом браузере нет встроенного QR (обычно нужны Chrome или Edge). Вставьте ссылку или код из QR в поле слева.';
    modal.classList.remove('hidden');
    modal.setAttribute('aria-hidden', 'false');
    return;
  }

  let stream;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } });
  } catch {
    status.textContent = 'Не удалось открыть камеру. Разрешите доступ или введите данные вручную.';
    modal.classList.remove('hidden');
    modal.setAttribute('aria-hidden', 'false');
    return;
  }

  qrStream = stream;
  video.srcObject = stream;
  await video.play();
  modal.classList.remove('hidden');
  modal.setAttribute('aria-hidden', 'false');

  const detector = new BarcodeDetector({ formats: ['qr_code'] });

  const tick = async () => {
    if (modal.classList.contains('hidden') || !qrStream) return;
    try {
      const codes = await detector.detect(video);
      if (codes && codes.length) {
        const raw = codes[0].rawValue;
        closeQrModal();
        const vid = extractVerifyId(raw);
        document.getElementById('emp-diploma-id').value = vid;
        document.getElementById('btn-check-id').click();
        return;
      }
    } catch { /* ignore frame errors */ }
    qrAnimFrame = requestAnimationFrame(tick);
  };
  qrAnimFrame = requestAnimationFrame(tick);
});

async function loadHistory() {
  const tbody = document.getElementById('history-tbody');
  try {
    const history = await apiFetch('/employer/history');
    if (!history.length) {
      tbody.innerHTML = '<tr><td colspan="4" class="placeholder-text">Нет истории</td></tr>';
      return;
    }
    tbody.innerHTML = history
      .map(
        (h) => `
      <tr>
        <td>${escapeHtml(formatDateRu(h.date))}</td>
        <td>${escapeHtml(h.diplomaId)}</td>
        <td>${escapeHtml(h.name)}</td>
        <td>${badge(h.result ? 'valid' : 'invalid')}</td>
      </tr>
    `,
      )
      .join('');
  } catch {
    tbody.innerHTML = '<tr><td colspan="4" class="placeholder-text">Ошибка загрузки</td></tr>';
  }
}

document.getElementById('btn-load-history').addEventListener('click', loadHistory);

// ─── Init ───────────────────────────────────────────────────────────────────
if (new URLSearchParams(window.location.search).get('dev') === '1') {
  document.body.classList.add('dev-mode');
}

(async () => {
  if (await tryRestoreSession()) return;
  showScreen('auth');
})();

const featureObserver = new IntersectionObserver(
  (entries) => {
    entries.forEach((entry, i) => {
      if (entry.isIntersecting) {
        setTimeout(() => entry.target.classList.add('visible'), i * 120);
        featureObserver.unobserve(entry.target);
      }
    });
  },
  { threshold: 0.15 },
);

document.querySelectorAll('.landing-feature-item').forEach((el) => {
  featureObserver.observe(el);
});
