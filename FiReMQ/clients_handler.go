// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"io"
	"net/http"

	"FiReMQ/db"         // Локальный пакет с БД BadgerDB
	"FiReMQ/logging"    // Локальный пакет с логированием в HTML файл
	"FiReMQ/protection" // Локальный пакет с функциями базовой защиты

	"github.com/dgraph-io/badger/v4"
)

// ClientInfo представляет данные клиента для веб-интерфейса
type ClientInfo struct {
	Status    string
	Name      string
	IP        string
	LocalIP   string
	ClientID  string
	Timestamp string
}

// SetNameHandler обрабатывает запросы на изменение имени клиента
func SetNameHandler(w http.ResponseWriter, r *http.Request) {
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

	var data struct {
		ClientID string `json:"clientID"`
		Name     string `json:"name"`
	}
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		http.Error(w, "Неверное тело запроса", http.StatusBadRequest)
		return
	}

	// Подготовка данных для валидации
	dataToValidate := map[string]string{
		"name": data.Name,
	}

	// Правила валидации
	rules := map[string]protection.ValidationRule{
		"name": {
			MinLength:   1,
			MaxLength:   80,
			AllowSpaces: true,
			FieldName:   "Имя клиента",
		},
	}

	// Валидация и санитизация
	sanitized, err := protection.ValidateFields(dataToValidate, rules)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Использование валидного имени
	name := sanitized["name"]
	clientID := data.ClientID
	var nameChanged bool

	// Изменения вносятся только если имя действительно изменилось
	err = db.DBInstance.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("client:" + clientID))
		if err != nil {
			return err
		}

		var current map[string]string
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &current)
		}); err != nil {
			return err
		}

		// Если имя совпадает, изменения не требуются
		if current["name"] == name {
			nameChanged = false
			return nil
		}

		// Изменяет и сохраняет
		current["name"] = name
		jsonData, err := json.Marshal(current)
		if err != nil {
			return err
		}
		if err := txn.Set([]byte("client:"+clientID), jsonData); err != nil {
			return err
		}
		nameChanged = true
		return nil
	})

	if err != nil {
		http.Error(w, "Ошибка сохранения имени клиента", http.StatusInternalServerError)
		return
	}

	if nameChanged {
		logging.LogAction("Клиенты: Админ \"%s\" (с именем: %s) изменил имя клиента с '%s' на '%s'", authInfo.Login, authInfo.Name, clientID, name)
		w.Write([]byte("Имя клиента обновлено"))
	} else {
		w.Write([]byte("Имя клиента не изменено"))
	}
}

// DeleteClientHandler обрабатывает запрос на удаление клиента
func DeleteClientHandler(w http.ResponseWriter, r *http.Request) {
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

	var data struct {
		ClientID string `json:"clientID"`
	}

	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		http.Error(w, "Неверное тело запроса", http.StatusBadRequest)
		return
	}

	if err := fullyRemoveClientAndData([]string{data.ClientID}, &authInfo); err != nil {
		logging.LogError("Клиенты: Ошибка удаления клиента %s: %v", data.ClientID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logging.LogAction("Клиенты: Админ \"%s\" (с именем: %s) успешно удалил клиент %s из БД", authInfo.Login, authInfo.Name, data.ClientID)
	w.Write([]byte("Клиент удалён"))
}

// DeleteSelectedClientsHandler обрабатывает запрос на удаление выбранных клиентов
func DeleteSelectedClientsHandler(w http.ResponseWriter, r *http.Request) {
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

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Ошибка чтения тела запроса", http.StatusInternalServerError)
		return
	}

	var clientIDs []string
	if err := json.Unmarshal(body, &clientIDs); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	// Вызов единой функции для полного удаления всех связанных данных
	if err := fullyRemoveClientAndData(clientIDs, &authInfo); err != nil {
		logging.LogError("Клиенты: Ошибка массового удаления клиентов %v: %v", clientIDs, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	logging.LogAction("Клиенты: Админ \"%s\" (с именем: %s) выполнил массовое удаление клиентов. Удалены ID: %v", authInfo.Login, authInfo.Name, clientIDs)
	w.Write([]byte("Клиенты успешно удалены"))
}

// FetchClientsByGroupHandler возвращает список клиентов по группе и/или подгруппе
func FetchClientsByGroupHandler(w http.ResponseWriter, r *http.Request) {
	group := r.URL.Query().Get("group")
	subgroup := r.URL.Query().Get("subgroup")

	var clients []ClientInfo
	var err error

	opts := badger.DefaultIteratorOptions
	opts.Prefix = []byte("client:") // Фильтрация по префиксу для эффективности

	err = db.DBInstance.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var data map[string]string
				if err := json.Unmarshal(val, &data); err != nil {
					return err
				}

				// Универсальная проверка группы и подгруппы
				targetGroup := (group == "" || data["group"] == group)
				targetSubgroup := (subgroup == "" || data["subgroup"] == subgroup)

				if targetGroup && targetSubgroup {
					client := ClientInfo{
						Status:    data["status"],
						Name:      data["name"],
						IP:        data["ip"],
						LocalIP:   data["local_ip"],
						ClientID:  data["client_id"],
						Timestamp: data["time_stamp"],
					}
					clients = append(clients, client)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		http.Error(w, "Ошибка получения данных", http.StatusInternalServerError)
		return
	}

	// Отправляет ответ в формате JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(clients)
}
