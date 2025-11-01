package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"time"
)

// Структура для JSON-запроса
type AuthRequest struct {
	AuthLogin    string `json:"auth_login"`
	AuthPassword string `json:"auth_password"`
	CaptchaID    string `json:"captcha_id"`
	CaptchaAnswer string `json:"captcha_answer"`
}

// Генерация случайной строки
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

// Отправка POST-запроса
func sendRequest(url string, authReq AuthRequest) {
	jsonData, err := json.Marshal(authReq)
	if err != nil {
		log.Fatalf("Ошибка при маршалинге JSON: %v", err)
	}

	// Создаём кастомный HTTP-клиент с отключённой проверкой сертификата
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Игнорируем проверку сертификата
		},
	}

	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Ошибка при отправке запроса: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Printf("Успешный запрос: Логин: %s, Пароль: %s", authReq.AuthLogin, authReq.AuthPassword)
	} else {
		log.Printf("Неудачный запрос: Логин: %s, Пароль: %s, Код ответа: %d", authReq.AuthLogin, authReq.AuthPassword, resp.StatusCode)
	}
}

func main() {
	rand.Seed(time.Now().UnixNano()) // Инициализация генератора случайных чисел

	url := "https://localhost:8080/auth" // URL для отправки запросов

	for i := 0; i < 1000; i++ { // Количество запросов
		authReq := AuthRequest{
			AuthLogin:    randomString(8),  // Случайный логин из 8 символов
			AuthPassword: randomString(12), // Случайный пароль из 12 символов
			CaptchaID:    "",               // Пока не используем капчу
			CaptchaAnswer: "",              // Пока не используем капчу
		}

		sendRequest(url, authReq)

		// Пауза между запросами (например, 100 мс)
		time.Sleep(100 * time.Millisecond)
	}
}