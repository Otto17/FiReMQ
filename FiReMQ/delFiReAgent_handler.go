// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"FiReMQ/logging" // Локальный пакет с логированием в HTML файл
	// Локальный пакет с функциями базовой защиты
)

// UninstallFiReAgentHandler Инициирует самоудаление клиентов (онлайн: сразу; оффлайн: в очередь из БД)
func UninstallFiReAgentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Ошибка чтения тела запроса", http.StatusInternalServerError)
		return
	}

	var clientIDsRaw []string
	if err := json.Unmarshal(body, &clientIDsRaw); err != nil {
		http.Error(w, "Ошибка парсинга данных (ожидается JSON массив с ID клиентов)", http.StatusBadRequest)
		return
	}

	// Санитизация: удаляет дубликаты и пустые ID
	seen := make(map[string]struct{}, len(clientIDsRaw))
	clientIDs := make([]string, 0, len(clientIDsRaw))
	for _, id := range clientIDsRaw {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clientIDs = append(clientIDs, id)
	}

	if len(clientIDs) == 0 {
		http.Error(w, "Пустой список ID клиентов", http.StatusBadRequest)
		return
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, errs := getAuthInfoFromRequest(r)
	if errs != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	var firstError string
	var offlineIDs []string

	for _, id := range clientIDs {
		online, err := isClientOnline(id)
		if err != nil {
			firstError = fmt.Sprintf("ошибка проверки статуса клиента %s: %v", id, err)
			break
		}

		if online {
			// Онлайн — вызывает единую функцию полного удаления
			if err := fullyDeleteClient(id, &authInfo); err != nil {
				firstError = err.Error()
				break
			}
			// Лог об успешном прямом удалении
			logging.LogAction("Удаление Агента: Админ \"%s\" (с именем: %s) удалил онлайн-клиента: %s", authInfo.Login, authInfo.Name, id)
		} else {
			// Оффлайн — добавит в очередь (с проверкой на дубликаты ниже)
			offlineIDs = append(offlineIDs, id)
		}
	}

	// Фильтрует офлайн-ID: не добавляет те, что уже есть в очереди удаления
	var toAdd []string
	var alreadyPending []string
	if firstError == "" && len(offlineIDs) > 0 {
		for _, id := range offlineIDs {
			exists, err := isPendingUninstall(id)
			if err != nil {
				firstError = fmt.Sprintf("ошибка проверки очереди удаления для %s: %v", id, err)
				break
			}
			if exists {
				alreadyPending = append(alreadyPending, id)
			} else {
				toAdd = append(toAdd, id)
			}
		}

		// Пакетно сохранят только новые офлайн-ID
		if firstError == "" && len(toAdd) > 0 {
			if err := addPendingUninstallBatch(toAdd); err != nil {
				firstError = fmt.Sprintf("ошибка сохранения офлайн клиентов в БД: %v", err)
			} else {
				logging.LogAction("Удаление Агента: Админ \"%s\" (с именем: %s) добавил в очередь на удаление (оффлайн) клиентов: %v", authInfo.Login, authInfo.Name, toAdd)
			}
		}
	}

	// Ответы
	w.Header().Set("Content-Type", "application/json")

	if firstError != "" {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "Ошибка",
			"message": firstError,
		})
		return
	}

	// Если были повторы — возвращает предупреждение (HTTP 200)
	if len(alreadyPending) > 0 {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":          "Предупреждение",
			"message":         "Пропущены (дубликаты): " + strings.Join(alreadyPending, ", ") + ". Остальные добавлены (если были).",
			"already_pending": alreadyPending,
		})
		return
	}

	// Обычный успех
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "Успех",
		"message": "Запрос отправлен (онлайн удалены, офлайн ждут входа)!",
	})
}

// GetPendingUninstallListHandler Возвращает список ID в очереди удаления
func GetPendingUninstallListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Только GET запросы поддерживаются", http.StatusMethodNotAllowed)
		return
	}
	ids, err := listPendingUninstallIDs()
	if err != nil {
		http.Error(w, "Ошибка чтения очереди удаления: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ids)
}

// CancelPendingUninstallHandler Отменяет удаление конкретного оффлайн-клиента (удаляет ключ из очереди Delete_FiReAgent:<ID>)
func CancelPendingUninstallHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Ошибка парсинга данных: "+err.Error(), http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(req.ClientID)

	if id == "" {
		http.Error(w, "Не указан ID клиента", http.StatusBadRequest)
		return
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, errs := getAuthInfoFromRequest(r)
	if errs != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	if err := removePendingUninstall(id); err != nil {
		http.Error(w, "Ошибка отмены удаления: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logging.LogAction("Удаление Агента: Админ \"%s\" (с именем: %s) отменил удаление клиента %s", authInfo.Login, authInfo.Name, id)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "Успех",
		"message": "Удаление \"FiReAgent\" отменено для: " + id,
	})
}
