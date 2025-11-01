// Заголовок
const CSRF_HEADER = "X-CSRF-Token";

// Глобально доступный токен
window.CSRF_TOKEN = null;

// Промис начальной инициализации, чтобы не плодить параллельные GET /csrf-token
let csrfInitPromise = null;

// Обновляем токен из заголовка ответа (если сервер его прислал)
function updateCsrfFromResponse(resp) {
  try {
    const tok = resp.headers?.get?.(CSRF_HEADER);
    if (tok) window.CSRF_TOKEN = tok;
  } catch (_) {}
}

// Явная загрузка токена с сервера (используется для bootstrap и ретраев)
async function fetchCsrfToken() {
  const resp = await fetch("/csrf-token", {
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
  });
  if (!resp.ok) throw new Error("CSRF: не удалось получить токен");

  updateCsrfFromResponse(resp);

  let tok = null;
  try {
    const data = await resp.json();
    tok = data?.csrf_token || null;
  } catch (_) {
    // если JSON не пришёл — ок, может быть токен пришёл только в заголовке
  }

  if (!tok) tok = window.CSRF_TOKEN;
  if (!tok) throw new Error("CSRF: сервер вернул пустой токен");

  window.CSRF_TOKEN = tok;
  return tok;
}

// Гарант наличия токена: если нет — один раз тянем с сервера
async function ensureCsrfToken() {
  if (window.CSRF_TOKEN) return window.CSRF_TOKEN;
  if (!csrfInitPromise) {
    csrfInitPromise = fetchCsrfToken().finally(() => {
      csrfInitPromise = null;
    });
  }
  return csrfInitPromise;
}

// Предзагрузка токена при загрузке страницы
document.addEventListener("DOMContentLoaded", () => {
  ensureCsrfToken().catch(err => {
    console.error("CSRF bootstrap error:", err);
    if (typeof showPush === 'function') {
      showPush("Не удалось получить CSRF токен. Обновите страницу!", "#ff4d4d");
    }
  });
});

// Синхронный геттер (оставил для совместимости с XHR)
function getCsrfSyncOrThrow() {
  if (!window.CSRF_TOKEN) {
    throw new Error("CSRF токен ещё не получен. Подождите или обновите страницу.");
  }
  return window.CSRF_TOKEN;
}

// Универсальный POST JSON с автоподхватом нового токена и ретраем на 403
async function apiPostJson(url, data, options = {}) {
  await ensureCsrfToken();

  const headers = Object.assign(
    { "Content-Type": "application/json", [CSRF_HEADER]: window.CSRF_TOKEN },
    options.headers || {}
  );

  const doFetch = (headersToUse) => fetch(url, {
    method: "POST",
    headers: headersToUse,
    body: JSON.stringify(data),
    credentials: "same-origin",
    ...options,
  });

  let resp = await doFetch(headers);
  updateCsrfFromResponse(resp);

  if (resp.status === 403) {
    // Один автоматический рефреш токена и повтор
    await fetchCsrfToken();
    const headersRetry = Object.assign({}, headers, { [CSRF_HEADER]: window.CSRF_TOKEN });
    resp = await doFetch(headersRetry);
    updateCsrfFromResponse(resp);
  }
  return resp;
}

// Универсальный POST FormData с автоподхватом нового токена и ретраем на 403
async function apiPostForm(url, formData, options = {}) {
  await ensureCsrfToken();

  const headers = Object.assign({ [CSRF_HEADER]: window.CSRF_TOKEN }, options.headers || {});
  const doFetch = (headersToUse) => fetch(url, {
    method: "POST",
    headers: headersToUse,       // Content-Type для FormData выставит браузер
    body: formData,
    credentials: "same-origin",
    ...options,
  });

  let resp = await doFetch(headers);
  updateCsrfFromResponse(resp);

  if (resp.status === 403) {
    await fetchCsrfToken();
    const headersRetry = Object.assign({}, headers, { [CSRF_HEADER]: window.CSRF_TOKEN });
    resp = await doFetch(headersRetry);
    updateCsrfFromResponse(resp);
  }
  return resp;
}

// Глобальный экспорт CSRF-API для использования в других JS-файлах
window.ensureCsrfToken = ensureCsrfToken;
window.getCsrfSyncOrThrow = getCsrfSyncOrThrow;
window.updateCsrfFromResponse = updateCsrfFromResponse;
window.apiPostJson = apiPostJson;
window.apiPostForm = apiPostForm;