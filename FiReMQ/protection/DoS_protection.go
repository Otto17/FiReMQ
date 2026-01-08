// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package protection

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

// LogSecurity используется для логирования событий безопасности (защита от циклического импорта)
var LogSecurity func(format string, args ...any)

// DoSLogMode определяет режим логирования DoS событий
type DoSLogMode int

const (
	DoSLogDefault     DoSLogMode = iota // Обычное логирование
	DoSLogConsoleOnly                   // Логирование только в консоль
)

// IPRateLimiter хранит лимиты запросов для каждого IP-адреса
type IPRateLimiter struct {
	ips map[string]*rate.Limiter
	mu  *sync.RWMutex
	r   rate.Limit // Разрешенный лимит запросов в секунду
	b   int        // Максимальный размер "корзины" (burst)
}

// NewIPRateLimiter создает новый IPRateLimiter с заданным лимитом и размером корзины
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	return &IPRateLimiter{
		ips: make(map[string]*rate.Limiter),
		mu:  &sync.RWMutex{},
		r:   r,
		b:   b,
	}
}

// AddIP добавляет новый IP в лимитер и возвращает созданный лимитер
func (i *IPRateLimiter) AddIP(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Создает новый лимитер с параметрами, заданными при инициализации IPRateLimiter
	limiter := rate.NewLimiter(i.r, i.b)
	i.ips[ip] = limiter

	//log.Printf("DoS: Новый IP добавлен в лимитер: %s (лимит: %v запросов/сек, буфер: %d)", ip, i.r, i.b) // ДЛЯ ОТЛАДКИ

	return limiter
}

// GetLimiter возвращает существующий лимитер для IP или создает новый, если он не найден
func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	limiter, exists := i.ips[ip]

	if !exists {
		// Обязательно снимает блокировку перед вызовом AddIP, который также блокирует мьютекс
		i.mu.Unlock()
		return i.AddIP(ip)
	}

	i.mu.Unlock()

	return limiter
}

// RateLimitMiddleware создает middleware для ограничения частоты запросов на основе IP-адреса
func RateLimitMiddleware(r rate.Limit, b int, mode ...DoSLogMode) func(next http.HandlerFunc) http.HandlerFunc {

	limiter := NewIPRateLimiter(r, b)
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Получает IP-адрес клиента, учитывая прокси-заголовки
			ip := GetClientIP(r)

			// Получает или создает лимитер для данного IP
			limiter := limiter.GetLimiter(ip)

			// Определяет режим логирования DoS
			logMode := DoSLogDefault
			if len(mode) > 0 {
				logMode = mode[0]
			}

			// Проверяет, разрешён ли запрос
			if !limiter.Allow() {
				if LogSecurity != nil {
					if logMode == DoSLogConsoleOnly {
						LogSecurity("DoS: Превышен лимит запросов для IP: %s", ip, true)
					} else {
						LogSecurity("DoS: Превышен лимит запросов для IP: %s", ip)
					}
				}

				http.Error(w, "Слишком много запросов", http.StatusTooManyRequests)
				return
			}

			// Логирует успешный запрос
			//log.Printf("Запрос разрешён для IP: %s (время: %s)", ip, time.Now().Format(time.RFC3339)) // ДЛЯ ОТЛАДКИ

			// Передает управление следующему обработчику, если лимит не превышен
			next(w, r)
		}
	}
}

// GetClientIP извлекает IP-адрес клиента из запроса, проверяя стандартные заголовки прокси
func GetClientIP(r *http.Request) string {
	// Проверяет заголовок X-Real-IP, используемый многими прокси
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	// Проверяет заголовок X-Forwarded-For, используя первый IP в списке
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}

	// Использует RemoteAddr как запасной вариант
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// Возвращает RemoteAddr, если SplitHostPort не смог разобрать
		return r.RemoteAddr
	}
	return host
}
