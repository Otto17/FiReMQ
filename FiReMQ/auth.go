// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"math/big"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"FiReMQ/logging"    // Локальный пакет с логированием в HTML файл
	"FiReMQ/pathsOS"    // Локальный пакет с путями для разных платформ
	"FiReMQ/protection" // Локальный пакет с функциями базовой защиты
)

// authTmpl представляет шаблон для страницы авторизации
var authTmpl *template.Template

// loadTemplates загружает шаблоны, необходимые для модуля авторизации
func loadTemplates() error {
	authTemplatePath := filepath.Join(pathsOS.Path_Web_Data, "auth.html")
	tmpl, err := template.New("auth.html").
		Funcs(template.FuncMap{
			"html": func(s template.HTML) template.HTML { return s },
		}).
		ParseFiles(authTemplatePath)
	if err != nil {
		return fmt.Errorf("не удалось загрузить шаблон '%s': %w", authTemplatePath, err)
	}
	authTmpl = tmpl
	return nil
}

// GetRandBase64 генерирует случайный токен в формате Base64 и сохраняет его в базе данных для указанного пользователя
func GetRandBase64(user *User) (string, error) {
	b := make([]byte, 32)
	rand.Read(b)

	// Кодирует байты в строку base64
	token := base64.RawURLEncoding.EncodeToString(b)

	// Строка допустимых символов
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	// Функция для замены одного символа
	replaceChar := func(r rune) rune {
		if strings.ContainsRune(letters, r) {
			return r
		}
		// Генерирует случайный индекс в пределах длины letters
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		return rune(letters[num.Int64()])
	}

	// Преобразует каждый символ токена
	result := strings.Map(replaceChar, token)

	// Сохраняет токен в базе данных немедленно
	user.Auth_Session_ID = result
	err := saveAdmin(*user)
	if err != nil {
		logging.LogError("Авторизация: Ошибка при сохранении токена в базу данных: %v", err)
		return "", err
	}
	//log.Printf("Авторизация: Токен %s успешно сохранен для админа %s", result, user.Auth_Login) // ДЛЯ ОТЛАДКИ
	return result, nil
}

// AuthHandler обрабатывает запросы авторизации пользователя
func AuthHandler(w http.ResponseWriter, r *http.Request) {
	// Устанавливает заголовки безопасности
	protection.SetSecurityHeaders(w)

	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Динамически загружает учетные записи администраторов
	users, err := loadAdmins()
	if err != nil {
		logging.LogError("Авторизация: Ошибка загрузки админов: %v", err)
		// Предотвращает раскрытие внутренней структуры сервера
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	// Проверяет наличие учетных записей администраторов
	if len(users) == 0 {
		logging.LogError("Авторизация: В системе отсутствуют админы: %v", err)
		// Предотвращает раскрытие внутренней структуры сервера
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	// Определяет тип данных в теле запроса
	isJSON := strings.Contains(r.Header.Get("Content-Type"), "application/json")

	// Декодирует данные из тела запроса
	var credentials struct {
		Auth_Login    string `json:"auth_login"`
		Auth_Password string `json:"auth_password"`
		CaptchaID     string `json:"captcha_id"`
		CaptchaAnswer string `json:"captcha_answer"`
	}

	if isJSON {
		if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil {
			logging.LogSecurity("Авторизация: Ошибка парсинга данных: %v", err)
			// Предотвращает раскрытие внутренней структуры сервера
			http.Error(w, "Внутренняя ошибка сервера", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			logging.LogSecurity("Авторизация: Ошибка парсинга формы: %v", err)
			// Предотвращает раскрытие внутренней структуры сервера
			http.Error(w, "Внутренняя ошибка сервера", http.StatusBadRequest)
			return
		}
		credentials.Auth_Login = r.FormValue("auth_login")
		credentials.Auth_Password = r.FormValue("auth_password")
		credentials.CaptchaID = r.FormValue("captcha_id")
		credentials.CaptchaAnswer = r.FormValue("captcha_answer")
	}

	// Подготавливает данные для валидации
	dataToValidate := map[string]string{
		"auth_login":    credentials.Auth_Login,
		"auth_password": credentials.Auth_Password,
	}

	// Определяет правила валидации для полей
	rules := map[string]protection.ValidationRule{
		// Логин
		"auth_login": {
			MinLength:   1, // От 1 до 30 символов
			MaxLength:   30,
			AllowSpaces: false,   // Запретить пробелы
			FieldName:   "Логин", // Название поля для возврата сообщения об ошибке
		},
		// Пароль
		"auth_password": {
			MinLength:   1, // От 1 до 64 символов
			MaxLength:   64,
			AllowSpaces: false,    // Запретить пробелы
			FieldName:   "Пароль", // Название поля для возврата сообщения об ошибке
		},
	}

	// Выполняет валидацию и санитизацию входных данных
	sanitized, err := protection.ValidateFields(dataToValidate, rules)
	if err != nil {
		logging.LogSecurity("Авторизация: Ошибка валидации данных: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)

		// Обрабатывает сообщение об ошибке для пользователя
		msg := "Внутренняя ошибка сервера"
		if strings.Contains(err.Error(), "логин") || strings.Contains(err.Error(), "пароль") {

			// Изменяет регистр первой буквы сообщения об ошибке
			errStr := err.Error()
			if len(errStr) > 0 {
				runes := []rune(errStr)
				runes[0] = unicode.ToUpper(runes[0])
				msg = string(runes)
			}
		}

		// Гарантирует наличие восклицательного знака в конце сообщения
		if !strings.HasSuffix(msg, "!") {
			msg += "!"
		}

		// Формирует JSON ответ с ошибкой
		json.NewEncoder(w).Encode(map[string]any{
			"error": msg,
		})
		return
	}

	// Обновляет данные санитизированными значениями
	credentials.Auth_Login = sanitized["auth_login"]
	credentials.Auth_Password = sanitized["auth_password"]

	// Выполняет базовую санитизацию полей капчи
	credentials.CaptchaID = strings.TrimSpace(credentials.CaptchaID)
	credentials.CaptchaAnswer = strings.TrimSpace(credentials.CaptchaAnswer)

	// Валидирует длину ID капчи
	if credentials.CaptchaID != "" && (len(credentials.CaptchaID) < 10 || len(credentials.CaptchaID) > 50) {
		http.Error(w, "Недопустимая длина ID капчи", http.StatusBadRequest)
		return
	}

	// Проверяет ответ капчи на наличие допустимых символов
	if credentials.CaptchaAnswer != "" {
		captchaCharsRegex := regexp.MustCompile(`^[A-Za-z0-9!*@#%?]+$`)
		if !captchaCharsRegex.MatchString(credentials.CaptchaAnswer) {
			http.Error(w, "Капча содержит недопустимые символы", http.StatusBadRequest)
			return
		}
	}

	// Получает IP-адрес клиента
	ip := protection.GetClientIP(r)
	attempts := protection.GetLoginAttempts(ip)

	// Требует проверку капчи после двух неудачных попыток
	if attempts > 2 {
		if !protection.CheckCaptcha(credentials.CaptchaID, credentials.CaptchaAnswer) {
			// Увеличивает счетчик неудачных попыток
			protection.IncrementLoginAttempt(ip)

			if isJSON {
				// Отправляет JSON ответ с требованием капчи
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]any{
					"error":            "Неверная капча",
					"captcha_required": true,
				})
			} else {
				// Рендерит страницу авторизации с ошибкой и новой капчей для клиентов без JS
				data := struct {
					ErrorMessage    template.HTML
					CaptchaRequired bool
					CaptchaImage    string
					CaptchaID       string
				}{
					ErrorMessage:    template.HTML(html.EscapeString("Неверная капча")), // Обеспечивает экранирование для предотвращения XSS
					CaptchaRequired: true,
				}

				// Генерирует новую капчу
				id, b64s, err := protection.GenerateCaptcha()
				if err != nil {
					logging.LogError("Авторизация: Ошибка генерации капчи: %v", err)
					// Предотвращает раскрытие внутренней структуры сервера
					http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
					return
				}
				data.CaptchaID = id
				data.CaptchaImage = b64s
				w.Header().Set("Content-Type", "text/html")
				authTmpl.Execute(w, data)
			}
			return
		}
	}

	// Ищет пользователя и проверяет хеш пароля
	user, err := getAdminByLogin(credentials.Auth_Login)
	if err == nil && protection.CompareHash(user.Auth_PasswordHash, credentials.Auth_Password) {
		// Обрабатывает успешную авторизацию
		// Генерирует и сохраняет новый токен сессии
		newToken, err := GetRandBase64(&user) // Изменение токена требует передачи указателя
		if err != nil {
			logging.LogError("Авторизация: Ошибка при генерации нового токена: %v", err)
			http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
			return
		}
		user.Auth_Session_ID = newToken

		logging.LogSecurity("Авторизация: Успешная авторизация админа: \"%s\" (IP: %s)", user.Auth_Login, ip)

		// Устанавливает куки сессии
		setAuthCookie(w, user)
		expiration := time.Now().Add(protection.CookieTime).Unix()
		authToken := createAuthToken(expiration)
		http.SetCookie(w, &http.Cookie{
			Name:     "auth",
			Value:    authToken,
			Expires:  time.Unix(expiration, 0),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
			Path:     "/",
		})

		// Сбрасывает счетчик неудачных попыток для IP
		protection.ResetLoginAttempts(ip)

		// Запускает горутину StatusClient, если она еще не запущена
		if !statusClientRunning {
			StatusClient()
		}

		if isJSON {
			w.WriteHeader(http.StatusOK)
		} else {
			http.Redirect(w, r, "/", http.StatusSeeOther)
		}
		return
	}

	// Обрабатывает неудачную попытку авторизации
	protection.IncrementLoginAttempt(ip)
	attempts = protection.GetLoginAttempts(ip)

	// Определяет сообщение об ошибке и требование капчи
	errorMsg := "Неверный логин или пароль"
	captchaRequired := attempts > 2

	if isJSON {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)

		response := map[string]any{
			"error": errorMsg,
		}

		// Устанавливает флаг требования капчи
		if captchaRequired {
			response["captcha_required"] = true
		}

		json.NewEncoder(w).Encode(response)
	} else {
		// Рендерит страницу авторизации с ошибкой
		data := struct {
			ErrorMessage    template.HTML
			CaptchaRequired bool
			CaptchaImage    string
			CaptchaID       string
		}{
			ErrorMessage:    template.HTML(html.EscapeString(errorMsg)), // Обеспечивает экранирование для предотвращения XSS
			CaptchaRequired: captchaRequired,
		}
		if captchaRequired {
			// Генерирует новую капчу
			id, b64s, err := protection.GenerateCaptcha()
			if err != nil {
				logging.LogError("Авторизация: Ошибка генерации капчи: %v", err)
				// Предотвращает раскрытие внутренней структуры сервера
				http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
				return
			}
			data.CaptchaID = id
			data.CaptchaImage = b64s
		}
		w.Header().Set("Content-Type", "text/html")

		if err := authTmpl.Execute(w, data); err != nil {
			logging.LogError("Авторизация: Ошибка рендеринга шаблона авторизации: %v", err)
			http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		}
	}
}

// LogoutHandler обрабатывает выход пользователя из системы
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	// Устанавливает заголовки безопасности
	protection.SetSecurityHeaders(w)

	// Удаляет CSRF-токен из хранилища
	protection.DropCSRFForRequest(r)

	// Получение логина перед удалением кук для логирования
	var loginForLog string
	sessionCookie, err := r.Cookie("session_id")
	if err == nil {
		// Разделяет логин и токен сессии
		parts := strings.Split(sessionCookie.Value, "|")
		if len(parts) == 2 {
			encryptedLogin := parts[0]

			// Расшифровывает логин админа
			if l, e := protection.DecryptLogin(encryptedLogin); e == nil {
				loginForLog = l
			}
		}
	}

	// Удаляет куки авторизации на стороне клиента
	clearAuthCookie(w)

	// Очищает токен сессии в БД
	if loginForLog != "" {
		users, err := loadAdmins()
		if err == nil {
			for _, user := range users {
				if user.Auth_Login == loginForLog {
					user.Auth_Session_ID = ""
					saveAdmin(user)
					break
				}
			}
		}

		// Логование выхода админа
		logging.LogSecurity("Авторизация: Админ \"%s\" вышел из системы", loginForLog)
	}

	// Останавливает таймер неактивности
	stopActivityTimer()

	// Останавливает StatusClient, если он активен
	if statusClientRunning {
		close(statusClientStop) // Использует канал для завершения горутины
		statusClientRunning = false
	}

	// Перенаправляет на страницу авторизации, предотвращая Open Redirect
	http.Redirect(w, r, "/auth.html", http.StatusSeeOther)
}

// AuthMiddleware проверяет авторизацию и продлевает куки
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Исключает обработку путей, связанных с авторизацией
		if r.URL.Path == "/auth.html" || r.URL.Path == "/auth" {
			next.ServeHTTP(w, r)
			return
		}

		// Проверяет наличие куки авторизации
		authCookie, err := r.Cookie("auth")
		if err != nil {
			clearAuthCookie(w) // Удаляет обе куки при отсутствии авторизационной куки
			http.Redirect(w, r, "/auth.html", http.StatusSeeOther)
			return
		}

		// Парсит токен и проверяет срок его действия
		expiration, valid := protection.ParseAuthToken(authCookie.Value)
		if !valid || time.Now().Unix() > expiration {
			clearAuthCookie(w) // Удаляет обе куки при истечении срока действия токена
			http.Redirect(w, r, "/auth.html", http.StatusSeeOther)
			return
		}

		// Обновляет срок действия авторизационной куки
		refreshAuthCookie(w, r)

		// Сбрасывает таймер неактивности сессии
		resetActivityTimer()

		// Обеспечивает запуск StatusClient при первой активности
		if !statusClientRunning {
			StatusClient()
		}

		next.ServeHTTP(w, r)
	})
}

// setAuthCookie устанавливает куку session_id при успешной авторизации
func setAuthCookie(w http.ResponseWriter, user User) {
	expiration := time.Now().Add(protection.CookieTime).Unix() // Определяет время жизни куки

	// Шифрует логин администратора для куки
	encryptedLogin, err := protection.EncryptLogin(user.Auth_Login)
	if err != nil {
		logging.LogError("Авторизация: Ошибка при шифровании логина для куки: %v", err)
		// Предотвращает раскрытие внутренней структуры сервера
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	// Формирует значение куки session_id
	session_id := fmt.Sprintf("%s|%s", encryptedLogin, user.Auth_Session_ID)
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session_id,
		Expires:  time.Unix(expiration, 0),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	})
	// log.Printf("Авторизация: Авторизация: Установлена кука 'session_id' для админа %s (%s) с токеном %s", user.Auth_Login, encryptedLogin, session_id) // ДЛЯ ОТЛАДКИ
}

// refreshAuthCookie обновляет время жизни куки auth
func refreshAuthCookie(w http.ResponseWriter, r *http.Request) {
	// Извлекает куку auth из запроса
	authCookie, err := r.Cookie("auth")
	if err != nil {
		// log.Printf("Авторизация: Кука 'auth' отсутствует: %v", err) // ДЛЯ ОТЛАДКИ
		return
	}

	// Парсит токен и проверяет его валидность
	expiration, valid := protection.ParseAuthToken(authCookie.Value)
	if !valid || time.Now().Unix() > expiration {
		// log.Printf("Авторизация: Токен 'auth' невалиден или истек") // ДЛЯ ОТЛАДКИ
		return
	}

	// Переустанавливает куку auth с обновленным сроком действия
	newExpiration := time.Now().Add(protection.CookieTime).Unix()
	authToken := createAuthToken(newExpiration)
	http.SetCookie(w, &http.Cookie{
		Name:     "auth",
		Value:    authToken,
		Expires:  time.Unix(newExpiration, 0),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	})
}

// clearAuthCookie удаляет куки авторизации
func clearAuthCookie(w http.ResponseWriter) {
	// Удаляет куку auth
	http.SetCookie(w, &http.Cookie{
		Name:     "auth",
		Value:    "",
		Expires:  time.Now().Add(-protection.CookieTime),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	})

	// Удаляет куку session_id
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Expires:  time.Now().Add(-protection.CookieTime),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	})
}

// createAuthToken создает токен, содержащий текущее время и время истечения
func createAuthToken(expiration int64) string {
	return time.Now().Format("20060102150405") + "|" + time.Unix(expiration, 0).Format("20060102150405")
}

// CheckAuthHandler проверяет статус авторизации администратора
func CheckAuthHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("auth")
	if err != nil {
		clearAuthCookie(w) // Удаляет куки при отсутствии авторизационной куки
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	expiration, valid := protection.ParseAuthToken(cookie.Value)
	if !valid || time.Now().Unix() > expiration {
		clearAuthCookie(w)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Возвращает статус OK, если авторизация действительна
	w.WriteHeader(http.StatusOK)
}

// RefreshTokenHandler обновляет время жизни куки session_id, если токен auth действителен
func RefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	session_idCookie, err := r.Cookie("session_id")
	if err != nil {
		logging.LogSecurity("Авторизация: Кука 'session_id' отсутствует: %v", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Разделяет логин и токен сессии
	parts := strings.Split(session_idCookie.Value, "|")
	if len(parts) != 2 {
		logging.LogSecurity("Авторизация: Неверный формат куки 'session_id': %s", session_idCookie.Value)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	encryptedLogin := parts[0]

	// Расшифровывает логин администратора
	login, err := protection.DecryptLogin(encryptedLogin)
	if err != nil {
		logging.LogError("Авторизация: Ошибка при расшифровке логина: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Проверяет существование пользователя по расшифрованному логину
	users, err := loadAdmins()
	if err != nil {
		logging.LogError("Авторизация: Ошибка при загрузке админов: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var foundUser User
	for _, user := range users {
		if user.Auth_Login == login {
			foundUser = user
			break
		}
	}

	if foundUser.Auth_Login == "" {
		logging.LogSecurity("Авторизация: Админ с логином %s не найден", login)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Извлекает срок действия из куки auth
	authCookie, err := r.Cookie("auth")
	if err != nil {
		logging.LogError("Авторизация: Ошибка при получении куки 'auth': %v", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	expiration, valid := protection.ParseAuthToken(authCookie.Value)
	if !valid {
		logging.LogSecurity("Авторизация: Неверный токен 'auth'")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Устанавливает куку session_id с обновленным сроком действия
	newsession_id := fmt.Sprintf("%s|%s", encryptedLogin, foundUser.Auth_Session_ID)
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    newsession_id,
		Expires:  time.Unix(expiration, 0),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Path:     "/",
	})

	// Возвращает пустой ответ для предотвращения перезагрузки страницы
	w.WriteHeader(http.StatusOK)
	w.Write([]byte{}) // Отправляет пустой ответ
}

// AuthPageHandler отображает страницу авторизации, динамически управляя требованием капчи
func AuthPageHandler(w http.ResponseWriter, r *http.Request) {
	// Устанавливает заголовки безопасности
	protection.SetSecurityHeaders(w)

	// Проверяет наличие активной сессии
	cookie, err := r.Cookie("auth")
	if err == nil { // Если кука существует
		expiration, valid := protection.ParseAuthToken(cookie.Value)
		if valid && time.Now().Unix() < expiration {
			// Админ авторизован, перенаправляет на главную страницу
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	// Извлекает IP-адрес клиента для проверки попыток
	ip := protection.GetClientIP(r)
	attempts := protection.GetLoginAttempts(ip)
	captchaRequired := attempts > 2
	// log.Printf("Страница авторизации: IP %s, попытки %d, Запрошена капча %v", ip, attempts, captchaRequired) // ДЛЯ ОТЛАДКИ

	data := struct {
		ErrorMessage    template.HTML
		CaptchaRequired bool
		CaptchaImage    string
		CaptchaID       string
	}{
		ErrorMessage:    "", // Инициализирует с пустым сообщением об ошибке
		CaptchaRequired: captchaRequired,
	}

	if captchaRequired {
		// Генерирует новую капчу
		id, b64s, err := protection.GenerateCaptcha()
		if err != nil {
			logging.LogError("Авторизация: Ошибка генерации капчи: %v", err)
			// Предотвращает раскрытие внутренней структуры сервера
			http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
			return
		}
		data.CaptchaID = id
		data.CaptchaImage = b64s
	} else {
		// Инициализирует ID и изображение капчи пустыми значениями
		data.CaptchaImage = ""
		data.CaptchaID = ""
	}

	w.Header().Set("Content-Type", "text/html")

	if err := authTmpl.Execute(w, data); err != nil {
		logging.LogError("Авторизация: Ошибка рендеринга шаблона: %v", err)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
	}
}
