// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package protection

import (
	"log"
	"net/http"
	"strings"
)

// SecurityHeadersMiddleware устанавливает заголовки безопасности для каждого ответа HTTP
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Устанавливает заголовки безопасности
		SetSecurityHeaders(w)
		next.ServeHTTP(w, r)
	})
}

// OriginCheckMiddleware блокирует POST-запросы, которые кажутся пришедшими из внешнего источника
func OriginCheckMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			origin := r.Header.Get("Origin")
			// Блокирует запрос, если заголовок Origin присутствует и не совпадает с ожидаемым хостом
			if origin != "" && !strings.HasPrefix(origin, "https://"+r.Host) {
				log.Printf("[ORIGIN-CHECK][FAIL] %s %s origin=%s", r.Method, r.URL.Path, origin)
				http.Error(w, "Запрещено!", http.StatusForbidden)
				return
			}
			ref := r.Header.Get("Referer")
			// Блокирует запрос, если заголовок Referer присутствует и не совпадает с ожидаемым хостом
			if ref != "" && !strings.HasPrefix(ref, "https://"+r.Host+"/") {
				log.Printf("[ORIGIN-CHECK][FAIL] %s %s referer=%s", r.Method, r.URL.Path, ref)
				http.Error(w, "Запрещено!", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// SetSecurityHeaders устанавливает стандартный набор заголовков безопасности HTTP
func SetSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")                                                                                                                                                                // Защищает от кликджекинга
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET")                                                                                                                                              // Ограничивает HTTP-методы, разрешенные для CORS
	w.Header().Set("Access-Control-Expose-Headers", "X-CSRF-Token")                                                                                                                                          // Разрешает клиентскому коду доступ к заголовку CSRF-токена
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self';")                                         // Задает политику безопасности контента для снижения риска XSS
	w.Header().Set("X-Content-Type-Options", "nosniff")                                                                                                                                                      // Предотвращает MIME-сниффинг браузером
	w.Header().Set("Referrer-Policy", "no-referrer")                                                                                                                                                         // Контролирует отправку данных о реферере
	w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")                                                                                                              // Требует использования HTTPS для последующих запросов
	w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), fullscreen=(), payment=(), accelerometer=(), gyroscope=(), magnetometer=(), picture-in-picture=(), sync-xhr=(), usb=()") // Запрещает доступ ко всем чувствительным API браузера
	w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")                                                                                                                                              // Запрещает загрузку содержимого с других доменов с использованием Flash или PDF
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")                                                                                                                                   // Запрещает кэширование ответа
	w.Header().Set("Pragma", "no-cache")                                                                                                                                                                     // Запрещает кэширование для совместимости со старыми HTTP/10 клиентами
	w.Header().Set("Expires", "0")                                                                                                                                                                           // Указывает, что ресурс устаревает немедленно
}
