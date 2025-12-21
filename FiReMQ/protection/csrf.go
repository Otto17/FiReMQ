// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package protection

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CookieTime определяет время жизни сессии и CSRF-токена (сюда же относится и время автоматического разлогирования из WEB админки)
var CookieTime = 40 * time.Minute

// csrfEntry хранит информацию о текущем и предыдущем CSRF-токенах для сессии
type csrfEntry struct {
	Current   string    // Текущий активный токен
	Prev      string    // Предыдущий токен
	PrevUntil time.Time // Время, до которого принимается предыдущий токен
	Expires   time.Time // Когда истекает запись для сессии (используется скользящее продление)
}

// csrfStore хранит токены CSRF в памяти, используя map с блокировкой для потокобезопасности
var (
	csrfStore = struct {
		mu        sync.RWMutex
		bySession map[string]csrfEntry
	}{bySession: make(map[string]csrfEntry)}

	// csrfGrace определяет период времени, в течение которого принимается предыдущий токен после ротации
	csrfGrace = 30 * time.Second
)

// generateCSRFToken создаёт новый случайный CSRF-токен (32 байта, закодированные в base64)
func generateCSRFToken() string {
	b := make([]byte, 32)
	// Игнорирует ошибку, так как она крайне маловероятна
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// GetLoginAndSessionIDFromCookie извлекает логин и ID сессии из куки "session_id"
func GetLoginAndSessionIDFromCookie(r *http.Request) (login string, sessionID string, err error) {
	c, err := r.Cookie("session_id")
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(c.Value, "|")
	if len(parts) != 2 {
		return "", "", errors.New("неверный формат session_id куки")
	}
	encLogin := parts[0]
	login, err = DecryptLogin(encLogin)
	if err != nil {
		return "", "", err
	}
	return login, parts[1], nil
}

// IssueOrGetCSRF возвращает существующий токен или создаёт новый, продлевая срок жизни записи
func IssueOrGetCSRF(sessionID, login string) string {
	csrfStore.mu.Lock()
	defer csrfStore.mu.Unlock()

	now := time.Now()
	e, ok := csrfStore.bySession[sessionID]
	// Создает новый токен, если записи нет, она просрочена или текущий токен пуст
	if !ok || now.After(e.Expires) || e.Current == "" {
		e = csrfEntry{
			Current: generateCSRFToken(),
			Expires: now.Add(CookieTime),
		}
		csrfStore.bySession[sessionID] = e
		// log.Printf("[CSRF] Выдан новый токен для %s (sid=%s)", login, sessionID) // ДЛЯ ОТЛАДКИ
		return e.Current
	}

	// Выполняет скользящее продление «жизни» записи, синхронно с временем жизни куки
	e.Expires = now.Add(CookieTime)
	csrfStore.bySession[sessionID] = e
	return e.Current
}

// RotateCSRF перемещает текущий токен в предыдущий (Prev) и генерирует новый Current-токен
func RotateCSRF(sessionID, login string) string {
	csrfStore.mu.Lock()
	defer csrfStore.mu.Unlock()

	now := time.Now()
	e := csrfStore.bySession[sessionID] // Если записи нет, использует нулевую запись
	oldCurrent := e.Current
	newTok := generateCSRFToken()

	// Выполняет ротацию
	e.Prev = oldCurrent
	e.PrevUntil = now.Add(csrfGrace)
	e.Current = newTok
	e.Expires = now.Add(CookieTime)

	csrfStore.bySession[sessionID] = e
	// log.Printf("[CSRF] Ротация токена для %s (sid=%s)", login, sessionID) // ДЛЯ ОТЛАДКИ
	return newTok
}

// ValidateCSRFForRequestDetailed проверяет CSRF-токен, полученный в заголовке или теле POST-запроса
func ValidateCSRFForRequestDetailed(r *http.Request) (ok bool, reason string, debug string, matchedPrev bool) {
	if r.Method != http.MethodPost {
		return false, "не POST запрос", "skip", false // Проверяет токены только для POST-запросов
	}

	token := r.Header.Get("X-CSRF-Token")
	if token == "" {
		// Ищет токен в теле запроса, если он не найден в заголовке
		ct := r.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "application/x-www-form-urlencoded") ||
			strings.HasPrefix(ct, "multipart/form-data") {
			if err := r.ParseForm(); err == nil {
				token = r.Form.Get("csrf_token")
			}
		}
	}
	if token == "" {
		return false, "нет CSRF токена", "hdr/body=∅", false
	}

	login, sid, err := GetLoginAndSessionIDFromCookie(r)
	if err != nil {
		return false, "нет/невалидная кука session_id", "cookie=bad", false
	}

	csrfStore.mu.RLock()
	e, okStore := csrfStore.bySession[sid]
	csrfStore.mu.RUnlock()
	if !okStore {
		return false, "не найден CSRF токен для этой сессии", "sid=" + sid, false
	}
	if time.Now().After(e.Expires) {
		return false, "CSRF токен истёк", "sid=" + sid, false
	}

	// Сначала сравнивает с текущим токеном, используя ConstantTimeCompare для защиты от timing-атак
	if subtle.ConstantTimeCompare([]byte(e.Current), []byte(token)) == 1 {
		return true, "", "пользователь=" + login + " sid=" + sid + " matched=current", false
	}

	// Если не совпало, пробует сравнить с предыдущим токеном в окне грации
	if e.Prev != "" && time.Now().Before(e.PrevUntil) &&
		subtle.ConstantTimeCompare([]byte(e.Prev), []byte(token)) == 1 {
		return true, "", "пользователь=" + login + " sid=" + sid + " matched=prev", true
	}

	return false, "CSRF токен не совпадает", "sid=" + sid + " tok=" + token, false
}

// DropCSRFForRequest удаляет CSRF-токен для текущей сессии
func DropCSRFForRequest(r *http.Request) {
	_, sid, err := GetLoginAndSessionIDFromCookie(r)
	if err == nil {
		csrfStore.mu.Lock()
		delete(csrfStore.bySession, sid) // Удаляет запись токена из памяти
		csrfStore.mu.Unlock()
		// log.Printf("[CSRF] Токен удалён для sid=%s", sid) // ДЛЯ ОТЛАДКИ
	}
}

// CSRFTokenHandler обрабатывает GET-запрос и выдаёт CSRF-токен в JSON
func CSRFTokenHandler(w http.ResponseWriter, r *http.Request) {
	SetSecurityHeaders(w)
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	// Проверяет авторизацию по куке "auth"
	authCookie, err := r.Cookie("auth")
	if err != nil {
		http.Error(w, "Вы не авторизованы", http.StatusUnauthorized)
		return
	}
	exp, valid := ParseAuthToken(authCookie.Value)
	if !valid || time.Now().Unix() > exp {
		http.Error(w, "Вы не авторизованы", http.StatusUnauthorized)
		return
	}

	// Получает логин и sessionID
	login, sid, err := GetLoginAndSessionIDFromCookie(r)
	if err != nil {
		http.Error(w, "Вы не авторизованы", http.StatusUnauthorized)
		return
	}

	// Выполняет ротацию токена при запросе
	token := RotateCSRF(sid, login)

	// Возвращает токен в теле ответа и в заголовке X-CSRF-Token
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-CSRF-Token", token) // на всякий
	_ = json.NewEncoder(w).Encode(map[string]string{"csrf_token": token})
}

// ParseAuthToken разбирает токен "auth" и возвращает время его истечения
func ParseAuthToken(token string) (int64, bool) {
	parts := strings.Split(token, "|")
	if len(parts) != 2 {
		return 0, false
	}

	// Парсит время истечения из второй части токена
	expiration, err := time.Parse("20060102150405", parts[1])
	if err != nil {
		return 0, false
	}

	return expiration.Unix(), true
}
