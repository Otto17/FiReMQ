// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package mqtt_server

import (
	"encoding/json"
	"strings"

	"FiReMQ/logging" // Локальный пакет с логированием в HTML файл
)

// GetTopicData настраивает обработчик входящих MQTT-публикаций
func GetTopicData() {
	Server.SetOnPublishHandler(func(clientID, topic string, clientIP string, payload []byte) {

		// Нормализует IPv6 localhost в IPv4 для унификации
		if clientIP == "::1" {
			clientIP = "127.0.0.1"
		}

		// Обрабатывает топик для передачи больших бинарных данных чанками
		if strings.HasPrefix(topic, "Client/ModuleInfo/") {
			// Проверяет минимальный размер полезной нагрузки, включая метаданные
			if len(payload) < 34 { // 2 байта флаги + 32 байта метаданные
				logging.LogError("MQTT Info_Files: Некорректный размер чанка от %s", clientIP)
				return
			}

			// Читает метаданные для проверки (ЭТА ЧАСТЬ МОЖЕТ ВЫЗЫВАТЬ ПРОБЛЕМЫ С ЗАДЕРЖКАМИ ИЗ-ЗА РУЧНОГО ЗАПУСКА runtime.GC(), ПОКА ОТКЛЮЧЕНО)
			//chunkNum := binary.LittleEndian.Uint64(payload[18:26])
			//totalChunks := binary.LittleEndian.Uint64(payload[26:34])
			//isLastChunk := chunkNum == totalChunks-1 // Определяем по номеру чанка

			// Лог для отладки
			// log.Printf("Чанк %d/%d (размер: %d байт, последний: %v)", chunkNum+1, totalChunks, len(payload)-34, isLastChunk)

			// defer func() {
			// payload = nil
			// if isLastChunk {
			// runtime.GC()
			// // log.Printf("Последний чанк файла (чанк %d/%d) получен. GC выполнен.", chunkNum+1, totalChunks)
			// }
			// }()
			return
		}

		// Устанавливает жёсткое ограничение на размер сообщения для защиты от переполнения
		if len(payload) > 1024 { // 1КБ максимум
			logging.LogSecurity("MQTT от Info_Files: Сообщение от клиента %s (%d байт) превышает допустимый размер в 1024 байта. Добавление пользователя в БД отклонено!", clientID, len(payload))
			return
		}

		// Обрабатывает ответы от команд, исполненных на клиенте
		if strings.HasPrefix(topic, "Client/") && strings.Contains(topic, "/ModuleCommand") {
			// Использует поля Answer и Date_Of_Creation для валидации формата ответа
			var test struct {
				Date_Of_Creation string `json:"Date_Of_Creation"`
				Answer           string `json:"Answer"`
			}

			if err := json.Unmarshal(payload, &test); err == nil && test.Answer != "" && test.Date_Of_Creation != "" {
				// Это сообщение ответа, вызывает внешний обработчик
				logging.LogSystem("ModuleCommand: Получен ответ от клиента %s в топике %s", clientID, topic)
				if HandleAnswerMessage != nil {
					HandleAnswerMessage(clientID, payload)
				}
				return
			}
		}

		// Обрабатывает ответы о выполнении задач по установке ПО через QUIC
		if strings.HasPrefix(topic, "Client/") && strings.HasSuffix(topic, "/ModuleQUIC/Answer") {
			var resp struct {
				Date_Of_Creation string `json:"Date_Of_Creation"`
				Answer           string `json:"Answer"`
				QUIC_Execution   string `json:"QUIC_Execution"`
				Attempts         string `json:"Attempts"`
				Description      string `json:"Description"`
			}

			if err := json.Unmarshal(payload, &resp); err == nil && resp.Date_Of_Creation != "" && resp.Answer != "" {
				logging.LogSystem("ModuleQUIC: Получен ответ от клиента %s в топике %s", clientID, topic)
				if HandleQUICAnswerMessage != nil {
					HandleQUICAnswerMessage(clientID, resp.Date_Of_Creation, resp.Answer, resp.QUIC_Execution, resp.Attempts, resp.Description)
				}
			}
			return
		}

		// Обрабатывает сообщения о регистрации нового клиента
		if topic == "Data/DB" {
			localIP, err := ParseMessage(payload)
			if err != nil {
				logging.LogError("Новый клиеет в БД: Ошибка парсинга JSON: %v", err)
				return
			}

			// log.Printf("Клиент %s: clientIP='%s', localIP='%s'", clientID, clientIP, localIP) // ДЛЯ ОТЛАДКИ

			// Вызывает внешнюю функцию сохранения, если она инжектирована
			if SaveClientInfo != nil {
				err = SaveClientInfo("On", "", clientIP, localIP, clientID)
			}
			if err != nil {
				logging.LogError("Новый клиеет в БД: Ошибка сохранения данных клиента: %v", err)
			}
			return
		}
	})
}

// ParseMessage парсит LocalIP из JSON-сообщения клиента
func ParseMessage(payload []byte) (string, error) {
	var msg ClientMessage
	err := json.Unmarshal(payload, &msg)
	if err != nil {
		return "", err
	}
	return msg.LocalIP, nil
}
