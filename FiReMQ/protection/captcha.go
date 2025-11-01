// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package protection

import (
	"image/color"
	"sync"
	"time"

	"math/rand/v2"

	"github.com/mojocn/base64Captcha"
)

// Глобальные переменные для капчи и лимитов
var (
	captchaStore    = base64Captcha.DefaultMemStore // Хранилище сгенерированных CAPTCHA ID
	loginAttempts   = make(map[string]int)          // Счетчик неудачных попыток авторизации по IP
	loginAttemptsMu = &sync.Mutex{}                 // Мьютекс для потокобезопасного доступа к loginAttempts и lastAttempt
	lastAttempt     = make(map[string]time.Time)    // Время последней попытки авторизации по IP
)

// GenerateCaptcha генерирует CAPTCHA-изображение и возвращает его ID и base64-строку
func GenerateCaptcha() (string, string, error) {
	// Динамическая длина капчи (от 5 до 7 символов)
	length := rand.IntN(3) + 5 // 5, 6 или 7

	// Источник символов: Английские буквы, цифры и безопасные символы
	source := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!*@#%?"

	// Случайные шрифты из доступных в base64Captcha
	fonts := []string{
		"RitaSmith.ttf",
		"actionj.ttf",
		"chromohv.ttf",
		"wqy-microhei.ttc",
	}

	// Настройки капчи
	driver := base64Captcha.DriverString{
		Height:          60,                                                                     // Высота изображения
		Width:           200,                                                                    // Ширина изображения
		NoiseCount:      8,                                                                      // Количество шумовых элементов (увеличено)
		ShowLineOptions: base64Captcha.OptionShowHollowLine | base64Captcha.OptionShowSlimeLine, // Линии
		Length:          length,                                                                 // Динамическая длина
		Source:          source,                                                                 // Источник символов
		BgColor:         &color.RGBA{R: 255, G: 255, B: 255, A: 255},                            // Белый фон
		Fonts:           fonts,                                                                  // Случайные шрифты
	}

	// Создает новый объект CAPTCHA
	captcha := base64Captcha.NewCaptcha(&driver, captchaStore)
	id, b64s, _, err := captcha.Generate() // Генерирует CAPTCHA
	return id, b64s, err
}

// CheckCaptcha проверяет ответ пользователя на CAPTCHA
func CheckCaptcha(id, answer string) bool {
	// Проверяет ответ и удаляет CAPTCHA ID из хранилища после первой проверки
	return captchaStore.Verify(id, answer, true)
}

// IncrementLoginAttempt увеличивает счетчик неудачных попыток для данного IP-адреса
func IncrementLoginAttempt(ip string) {
	loginAttemptsMu.Lock()
	defer loginAttemptsMu.Unlock()

	// Сбрасывает счетчик, если с момента последней попытки прошло более 5 минут
	if time.Since(lastAttempt[ip]) > 5*time.Minute {
		loginAttempts[ip] = 0
	}

	loginAttempts[ip]++
	lastAttempt[ip] = time.Now()
}

// GetLoginAttempts возвращает количество неудачных попыток для данного IP-адреса
func GetLoginAttempts(ip string) int {
	loginAttemptsMu.Lock()
	defer loginAttemptsMu.Unlock()
	return loginAttempts[ip]
}

// ResetLoginAttempts сбрасывает счетчик неудачных попыток для данного IP-адреса
func ResetLoginAttempts(ip string) {
	loginAttemptsMu.Lock()
	defer loginAttemptsMu.Unlock()
	delete(loginAttempts, ip)
	delete(lastAttempt, ip)
}

// init запускает горутину для периодической очистки старых записей попыток входа
func init() {
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			loginAttemptsMu.Lock()
			for ip, last := range lastAttempt {
				// Удаляет запись, если с момента последней попытки прошло более 30 минут
				if time.Since(last) > 30*time.Minute {
					delete(loginAttempts, ip)
					delete(lastAttempt, ip)
				}
			}
			loginAttemptsMu.Unlock()
		}
	}()
}
