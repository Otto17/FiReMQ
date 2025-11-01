// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"FiReMQ/protection" // Локальный пакет с функциями базовой защиты
)

// MoveClientHandler обработчик для перемещения клиента в подгруппу любой другой группы
func MoveClientHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	var data struct {
		ClientID      string `json:"clientID"`
		NewGroupID    string `json:"newGroupID"`
		NewSubgroupID string `json:"newSubgroupID"`
	}
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		http.Error(w, "Неверное тело запроса", http.StatusBadRequest)
		return
	}

	// Подготовка данных для валидации
	dataToValidate := map[string]string{
		"newGroup":    data.NewGroupID,
		"newSubgroup": data.NewSubgroupID,
	}

	// Правило валидации
	rules := map[string]protection.ValidationRule{
		// Группа
		"newGroup": {
			MinLength:   1, // От 1 до 20 символов
			MaxLength:   20,
			AllowSpaces: true,         // Разрешить пробелы
			FieldName:   "Имя группы", // Название поля для сообщений об ошибках
		},
		// Подгруппа
		"newSubgroup": {
			MinLength:   1, // От 1 до 20 символов
			MaxLength:   20,
			AllowSpaces: true,            // Разрешить пробелы
			FieldName:   "Имя подгруппы", // Название поля для сообщений об ошибках
		},
	}

	// Валидация и санитизация
	sanitized, err := protection.ValidateFields(dataToValidate, rules)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Использование валидных данных
	clientID := data.ClientID
	newGroup := sanitized["newGroup"]
	newSubgroup := sanitized["newSubgroup"]

	if clientID == "" || newGroup == "" || newSubgroup == "" {
		http.Error(w, "Некорректные параметры запроса", http.StatusBadRequest)
		return
	}

	err = MoveClient(clientID, newGroup, newSubgroup)
	if err != nil {
		http.Error(w, "Ошибка перемещения клиента", http.StatusInternalServerError)
		return
	}

	w.Write([]byte("Клиент перемещён"))
}

// MoveSelectedClientsHandler обработчик для перемещения списка клиентов в подгруппу
func MoveSelectedClientsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Читаем тело запроса
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Ошибка чтения тела запроса", http.StatusInternalServerError)
		return
	}

	// Структура для парсинга данных из JSON
	type MoveClientsPayload struct {
		ClientIDs   []string `json:"clientIDs"`
		NewGroup    string   `json:"newGroup"`
		NewSubgroup string   `json:"newSubgroup"`
	}

	var payload MoveClientsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	// Подготовка данных для валидации
	dataToValidate := map[string]string{
		"newGroup":    payload.NewGroup,
		"newSubgroup": payload.NewSubgroup,
	}

	// Правило валидации
	rules := map[string]protection.ValidationRule{
		// Группа
		"newGroup": {
			MinLength:   1, // От 1 до 20 символов
			MaxLength:   20,
			AllowSpaces: true,         // Разрешить пробелы
			FieldName:   "Имя группы", // Название поля для сообщений об ошибках
		},
		// Подгруппа
		"newSubgroup": {
			MinLength:   1, // От 1 до 20 символов
			MaxLength:   20,
			AllowSpaces: true,            // Разрешить пробелы
			FieldName:   "Имя подгруппы", // Название поля для сообщений об ошибках
		},
	}

	// Валидация и санитизация
	sanitized, err := protection.ValidateFields(dataToValidate, rules)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Проверка входных данных
	if len(payload.ClientIDs) == 0 {
		http.Error(w, "Список клиентов не может быть пустым", http.StatusBadRequest)
		return
	}

	// Использование валидных данных
	payload.NewGroup = sanitized["newGroup"]
	payload.NewSubgroup = sanitized["newSubgroup"]

	// Перемещение клиентов
	notFoundIDs, err := MoveSelectedClients(payload.ClientIDs, payload.NewGroup, payload.NewSubgroup)
	if err != nil {
		http.Error(w, "Ошибка перемещения клиентов: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Формируем умный ответ
	if len(notFoundIDs) > 0 {
		// Если некоторые клиенты не были найдены
		message := fmt.Sprintf("Операция завершена. %d клиент(ов) успешно перемещено. Не найдены: %s", len(payload.ClientIDs)-len(notFoundIDs), strings.Join(notFoundIDs, ", "))
		response := map[string]string{
			"status":  "Предупреждение",
			"message": message,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Если все прошло идеально
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "Успех",
		"message": "Клиенты успешно перемещены",
	})
}
