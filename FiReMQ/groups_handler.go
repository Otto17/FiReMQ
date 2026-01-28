// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"FiReMQ/logging"    // Локальный пакет с логированием в HTML файл
	"FiReMQ/protection" // Локальный пакет с функциями базовой защиты
)

// MoveClientHandler обработчик для перемещения клиента в подгруппу любой другой группы
func MoveClientHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, errs := getAuthInfoFromRequest(r)
	if errs != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	// Проверяет права текущего админа на перемещение клиентов
	currentAdmin, erro := GetAdminByLogin(authInfo.Login)
	if erro != nil {
		http.Error(w, "Ошибка получения данных текущего админа", http.StatusInternalServerError)
		return
	}

	if !currentAdmin.Perm_MoveClients {
		http.Error(w, "У вас нет прав на перемещение клиентов", http.StatusForbidden)
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
		// Возвращает ошибку, если найдены запрещенные символы
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

	// Получает текущую группу клиента для проверки прав
	currentGroup, err := GetClientGroup(clientID)
	if err != nil {
		logging.LogError("Группы: Ошибка получения текущей группы клиента [%s]: %v", clientID, err)
		http.Error(w, "Ошибка получения данных клиента", http.StatusInternalServerError)
		return
	}

	// Проверяет права на перемещение из текущей группы в новую
	if !CanMoveBetweenGroups(currentAdmin, currentGroup, newGroup) {
		var errMsg string
		if len(currentAdmin.Perm_MoveClientsGroups) > 0 {
			// Формируем список разрешённых групп через запятую
			allowedGroupsStr := "'" + strings.Join(currentAdmin.Perm_MoveClientsGroups, "', '") + "'"
			errMsg = fmt.Sprintf("Перемещение в группу '%s' запрещено! Разрешённые группы: %s", newGroup, allowedGroupsStr)
		} else {
			errMsg = fmt.Sprintf("Перемещение в группу '%s' запрещено!", newGroup)
		}
		http.Error(w, errMsg, http.StatusForbidden)
		return
	}

	err = MoveClient(clientID, newGroup, newSubgroup)
	if err != nil {
		logging.LogError("Группы: Ошибка перемещения клиента [%s] в %s/%s: %v", clientID, newGroup, newSubgroup, err)
		http.Error(w, "Ошибка перемещения клиента", http.StatusInternalServerError)
		return
	}

	logging.LogAction("Группы: Админ \"%s\" (с именем: %s) переместил клиента [%s] в группу '%s', подгруппу '%s'", authInfo.Login, authInfo.Name, clientID, newGroup, newSubgroup)
	w.Write([]byte("Клиент перемещён"))
}

// MoveSelectedClientsHandler обработчик для перемещения списка клиентов в подгруппу
func MoveSelectedClientsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, errs := getAuthInfoFromRequest(r)
	if errs != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	// Проверяет права текущего админа на перемещение клиентов
	currentAdmin, err := GetAdminByLogin(authInfo.Login)
	if err != nil {
		http.Error(w, "Ошибка получения данных текущего админа", http.StatusInternalServerError)
		return
	}

	if !currentAdmin.Perm_MoveClients {
		http.Error(w, "У вас нет прав на перемещение клиентов", http.StatusForbidden)
		return
	}

	// Читает тело запроса
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
		// Возвращает ошибку, если найдены запрещенные символы
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

	// Проверяет права на перемещение в целевую группу
	if !CanMoveToGroup(currentAdmin, payload.NewGroup) {
		var errMsg string
		if len(currentAdmin.Perm_MoveClientsGroups) > 0 {
			// Формируем список разрешённых групп через запятую
			allowedGroupsStr := "'" + strings.Join(currentAdmin.Perm_MoveClientsGroups, "', '") + "'"
			errMsg = fmt.Sprintf("Перемещение в группу '%s' запрещено! Разрешённые группы: %s", payload.NewGroup, allowedGroupsStr)
		} else {
			errMsg = fmt.Sprintf("Перемещение в группу '%s' запрещено!", payload.NewGroup)
		}
		http.Error(w, errMsg, http.StatusForbidden)
		return
	}

	// Проверяет права на перемещение из текущих групп клиентов
	var forbiddenClients []string
	for _, clientID := range payload.ClientIDs {
		currentGroup, err := GetClientGroup(clientID)
		if err != nil {
			continue // Клиент будет добавлен в notFoundIDs при перемещении
		}
		if !CanMoveToGroup(currentAdmin, currentGroup) {
			forbiddenClients = append(forbiddenClients, clientID)
		}
	}

	if len(forbiddenClients) > 0 {
		var errMsg string
		if len(currentAdmin.Perm_MoveClientsGroups) > 0 {
			// Формируем список разрешённых групп через запятую
			allowedGroupsStr := "'" + strings.Join(currentAdmin.Perm_MoveClientsGroups, "', '") + "'"
			errMsg = fmt.Sprintf("Перемещение некоторых клиентов запрещено! Разрешённые группы: %s", allowedGroupsStr)
		} else {
			errMsg = "Перемещение некоторых клиентов запрещено!"
		}
		http.Error(w, errMsg, http.StatusForbidden)
		return
	}

	// Перемещение клиентов
	notFoundIDs, err := MoveSelectedClients(payload.ClientIDs, payload.NewGroup, payload.NewSubgroup)
	if err != nil {
		logging.LogError("Группы: Массовое перемещение клиентов в %s/%s завершилось ошибкой: %v", payload.NewGroup, payload.NewSubgroup, err)
		http.Error(w, "Ошибка перемещения клиентов: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Формирует умный ответ
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

	// Логирует успешное действие
	movedCount := len(payload.ClientIDs) - len(notFoundIDs)
	if movedCount > 0 {
		logging.LogAction("Группы: Админ \"%s\" (с именем: %s) переместил %d клиентов в группу '%s', подгруппу '%s'", authInfo.Login, authInfo.Name, movedCount, payload.NewGroup, payload.NewSubgroup)
	}
	if len(notFoundIDs) > 0 {
		logging.LogError("Группы: При массовом перемещении не найдены ID: %v", notFoundIDs)
	}

	// Если все прошло идеально
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "Успех",
		"message": "Клиенты успешно перемещены",
	})
}
