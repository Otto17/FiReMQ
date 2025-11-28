// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
	"time"

	"FiReMQ/db"          // Локальный пакет с БД BadgerDB
	"FiReMQ/mqtt_client" // Локальный пакет с MQTT клиентом AutoPaho

	"github.com/dgraph-io/badger/v4"
)

// CommandRequest Структура для получения данных с POST запроса
type CommandRequest struct {
	ClientIDs                     []string `json:"client_ids"`
	TerminalCommand               string   `json:"terminal_command"`
	Command                       string   `json:"command"`
	WorkingFolder                 string   `json:"working_folder"`
	RunWhetherUserIsLoggedOnOrNot bool     `json:"run_whether_user_is_logged_on_or_not"`
	UserName                      string   `json:"user_name,omitempty"`
	UserPassword                  string   `json:"user_password,omitempty"`
	RunWithHighestPrivileges      bool     `json:"run_with_highest_privileges"`
}

// MQTTCommand Структура для отправки данных в MQTT топики
type MQTTCommand struct {
	Terminal                      string `json:"Terminal"`
	Command                       string `json:"Command"`
	WorkingFolder                 string `json:"WorkingFolder"`
	RunWhetherUserIsLoggedOnOrNot bool   `json:"RunWhetherUserIsLoggedOnOrNot"`
	User                          string `json:"User"`
	Password                      string `json:"Password"`
	RunWithHighestPrivileges      bool   `json:"RunWithHighestPrivileges"`
}

// FullMQTTCommand Расширенная структура для отправки по MQTT, включающая дату создания
type FullMQTTCommand struct {
	Date_Of_Creation string `json:"Date_Of_Creation"`
	MQTTCommand
}

// GetCommandsHandler Возвращает все записи команд из БД GET запросом
func GetCommandsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Только GET запросы поддерживаются", http.StatusMethodNotAllowed)
		return
	}

	// Загружаем текущих админов
	usersMap, err := loadAdminsMap()
	if err != nil {
		log.Printf("Ошибка загрузки админов: %v", err)
	}

	// Перебираем все записи команд в БД
	var results []map[string]any
	err = db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_Command:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var record map[string]any
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			})
			if err != nil {
				continue
			}

			// Проверяем обновление имени админа
			if login, ok := record["Created_By_Login"].(string); ok && usersMap != nil {
				if user, exists := usersMap[login]; exists {
					// Просто подменяем имя в ответе, не меняя запись в БД
					record["Created_By"] = user.Auth_Name
				}
			}

			// Удаляем "Password" и ненужный дублирующий "Date_Of_Creation"
			if teamCommandStr, ok := record["Team_Command"].(string); ok {
				var teamCommandMap map[string]any
				if err := json.Unmarshal([]byte(teamCommandStr), &teamCommandMap); err == nil {
					delete(teamCommandMap, "Password")
					delete(teamCommandMap, "Date_Of_Creation")
					if updatedTeamCommand, err := json.Marshal(teamCommandMap); err == nil {
						record["Team_Command"] = string(updatedTeamCommand)
					}
				}
			}

			// Формируем ответ, поле "ClientID_Command" уже содержит вложенную структуру с Answer для каждого клиента
			itemResponse := map[string]any{
				"Date_Of_Creation": record["Date_Of_Creation"],
				"Team_Command":     record["Team_Command"],
				"ClientID_Command": record["ClientID_Command"],
				"Created_By":       record["Created_By"], // Отправляем имя админа, создавшего запрос
			}
			results = append(results, itemResponse)
		}
		return nil
	})

	if err != nil {
		http.Error(w, "Ошибка чтения из БД", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// DeleteCommandsByDateHandler Удаляет все записи в запросе команд с указанной датой создания
func DeleteCommandsByDateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Ожидаем JSON: {"Date_Of_Creation": "02.01.06(15:04:05)"}
	var req struct {
		Date_Of_Creation string `json:"Date_Of_Creation"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Ошибка парсинга данных: "+err.Error(), http.StatusBadRequest)
		return
	}

	//Флаг для ошибки, если неверно указана дата/время
	found := false

	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_Command:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			var record map[string]any
			item := it.Item()
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			})
			if err != nil {
				continue
			}
			if record["Date_Of_Creation"] == req.Date_Of_Creation {
				found = true
				if err := txn.Delete(item.Key()); err != nil {
					return err
				}
			}
		}
		return nil
	})

	if err != nil {
		http.Error(w, "Ошибка удаления запроса: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !found {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Ошибка",
			"message": "Запрос с указанной датой и временем не найден",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "Успех",
		"message": "Запрос удалён",
	})
}

// DeleteClientFromCommandByDateHandler Удаляет указанный ID клиента из записи с заданной датой создания
func DeleteClientFromCommandByDateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Ожидаем JSON: {"Date_Of_Creation": "<timestamp>", "client_id": "id"}
	var req struct {
		Date_Of_Creation string `json:"Date_Of_Creation"`
		ClientID         string `json:"client_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Ошибка парсинга данных: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.ClientID == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Ошибка",
			"message": "Не указан ID клиента для удаления",
		})
		return
	}

	// Счётчик найденных клиентов для удаления
	var deletedCount int

	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_Command:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			key := it.Item().KeyCopy(nil)
			var record map[string]any
			err := it.Item().Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			})
			if err != nil {
				continue
			}
			if record["Date_Of_Creation"] == req.Date_Of_Creation {
				mapping, ok := record["ClientID_Command"].(map[string]any)
				if !ok {
					continue
				}

				// Удаляем указанный "client_id" из карты
				if _, exists := mapping[req.ClientID]; exists {
					delete(mapping, req.ClientID)
					deletedCount++
				}

				// Если после удаления карта пустая, удаляем весь запрос
				if len(mapping) == 0 {
					if err := txn.Delete(key); err != nil {
						return err
					}
				} else {
					record["ClientID_Command"] = mapping
					newBytes, err := json.Marshal(record)
					if err != nil {
						return err
					}
					if err := txn.Set(key, newBytes); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		http.Error(w, "Ошибка обновления запросов: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if deletedCount == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Ошибка",
			"message": "Клиент не найден или не удалён из запроса",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "Успех",
		"message": "Клиент удалён из запроса",
	})
}

// ResendCommandHandler Обрабатывает POST запрос для повторной отправки команды конкретному клиенту
func ResendCommandHandler(w http.ResponseWriter, r *http.Request) {
	// Если клиент онлайн – команда отправляется сразу (не чаще 1 раза в 10 секунд на клиента)
	// Если клиент офлайн – выставляется флаг, чтобы при переходе в онлайн команда была отправлена один раз (независимо от кол-ва запросов)

	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Ожидаем JSON с двумя параметрами: "client_id" и "Date_Of_Creation"
	var req struct {
		ClientID         string `json:"client_id"`
		Date_Of_Creation string `json:"Date_Of_Creation"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClientID == "" || req.Date_Of_Creation == "" {
		http.Error(w, "Ошибка парсинга данных или отсутствует client_id/Date_Of_Creation", http.StatusBadRequest)
		return
	}

	var (
		processed        bool // Были ли изменения в записи
		alreadyRequested bool // Флаг уже установлен
		commandSent      bool // Была ли отправлена команда
		throttled        bool // Флаг ограничения лимита запросов
		waitSeconds      int  // Время ожидания истичения лимита
	)

	// Формируем ключ в БД: "FiReMQ_Command:" + Date_Of_Creation
	dbKey := "FiReMQ_Command:" + req.Date_Of_Creation

	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(dbKey))
		if err != nil {
			// Если записи с таким ключом не найдено, ничего не обрабатывается
			return nil
		}

		var record map[string]any
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &record)
		}); err != nil {
			return nil
		}

		// Проверяем наличие client_id в карте ClientID_Command
		mapping, ok := record["ClientID_Command"].(map[string]any)
		if !ok || mapping[req.ClientID] == nil {
			return nil // Клиент не найден
		}

		clientEntry, ok := mapping[req.ClientID].(map[string]any)
		if !ok {
			return nil
		}

		// Проверяем, онлайн ли клиент
		online, err := isClientOnline(req.ClientID)
		if err != nil {
			return nil
		}

		// Клиент онлайн
		if online {
			// Ограничение частоты, не чаще 1 раза в 10 секунд для онлайн-клиента
			allowed, wait := allowResend(req.ClientID, 10*time.Second)
			if !allowed {
				throttled = true
				waitSeconds = int(wait.Seconds()) + 1
				return nil
			}

			// Очистка Answer, только если реальная отправка
			if ans, _ := clientEntry["Answer"].(string); ans != "" {
				clientEntry["Answer"] = ""
				mapping[req.ClientID] = clientEntry
				record["ClientID_Command"] = mapping
				processed = true
			}

			// Подготовка команды
			cmdPayload, ok := record["Team_Command"].(string)
			if !ok {
				return nil
			}

			// Отправка в топик через клиента AutoPaho
			topic := fmt.Sprintf("Client/%s/ModuleCommand", req.ClientID)
			if err := mqtt_client.Publish(topic, []byte(cmdPayload), 2); err != nil {
				log.Printf("Ошибка повторной публикации команды в топик %s: %v", topic, err)
			} else {
				log.Printf("Повторная отправка команды клиенту %s выполнена", req.ClientID)
				commandSent = true // Команда отправлена
			}

			// После успешной публикации — обновим lastSend очереди, чтобы соблюсти 10 сек до следующей отправки
			val, _ := cmdSendQueues.LoadOrStore(req.ClientID, &cmdClientQueue{})
			q := val.(*cmdClientQueue)
			q.mu.Lock()
			q.lastSend = time.Now()
			q.mu.Unlock()

			// Обновление "SentFor"
			var sentFor []string
			if s, exists := record["SentFor"]; exists {
				for _, v := range s.([]any) {
					if str, ok := v.(string); ok {
						sentFor = append(sentFor, str)
					}
				}
			}

			if !slices.Contains(sentFor, req.ClientID) {
				sentFor = append(sentFor, req.ClientID)
				record["SentFor"] = sentFor
				processed = true
			}

		} else {
			// Оффлайн: установка флага на будущую отправку, если ещё не стоял
			var rr map[string]any
			if v, exists := record["ResendRequested"]; exists {
				rr, _ = v.(map[string]any)
			} else {
				rr = make(map[string]any)
			}

			if _, exists := rr[req.ClientID]; exists {
				alreadyRequested = true // Флаг уже установлен
			} else {
				rr[req.ClientID] = true
				record["ResendRequested"] = rr
				log.Printf("Установлен флаг повторной отправки для клиента %s", req.ClientID)
				processed = true

				// Очистка Answer, только при первом выставлении флага
				if ans, _ := clientEntry["Answer"].(string); ans != "" {
					clientEntry["Answer"] = ""
					mapping[req.ClientID] = clientEntry
					record["ClientID_Command"] = mapping
				}
			}
		}

		// Сохранение записи, только если были изменения
		if processed {
			newBytes, err := json.Marshal(record)
			if err != nil {
				return err
			}

			if err := txn.Set([]byte(dbKey), newBytes); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		http.Error(w, "Ошибка обработки запроса: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Определение ответа сервера
	w.Header().Set("Content-Type", "application/json")

	// При частых, повторных попытках
	if throttled {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Ограничение",
			"message": fmt.Sprintf("Повторная отправка не доступна. Подождите ещё %d сек.", waitSeconds),
		})
		return
	}

	if commandSent || processed {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Успех",
			"message": "Запрос на повторную отправку команды отправлен",
		})
	} else if alreadyRequested {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Уже запрошено",
			"message": "Повторная отправка уже запрошена ранее!",
		})
	} else {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Не найдено",
			"message": "Команда или клиент не найдены",
		})
	}
}

// SendCommandHandler Обрабатывает POST запрос, сохраняет команду в БД и отправляет её онлайн клиентам
func SendCommandHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	var cmdReq CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&cmdReq); err != nil {
		http.Error(w, "Ошибка парсинга данных: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Проверка, есть ли пароль без имени пользователя
	if cmdReq.UserName == "" && cmdReq.UserPassword != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Ошибка",
			"message": "Пароль НЕ может быть без указания пользователя!",
		})
		return
	}

	if len(cmdReq.ClientIDs) == 0 {
		http.Error(w, "Не указаны ID клиентов", http.StatusBadRequest)
		return
	}

	if cmdReq.TerminalCommand != "cmd" && cmdReq.TerminalCommand != "powershell" {
		http.Error(w, "Недопустимая терминальная команда", http.StatusBadRequest)
		return
	}

	// Если имя пользователя не указано, ставим значение по умолчанию "СИСТЕМА"
	user := cmdReq.UserName
	if user == "" {
		user = "СИСТЕМА"
	}

	// Формирование временных меток: Date_Of_Creation с миллисекундами
	now := time.Now()
	dateOfCreation := getTimestampWithMs(now)

	// Формирование базовой команды
	mqttCmd := MQTTCommand{
		Terminal:                      cmdReq.TerminalCommand,
		Command:                       cmdReq.Command,
		WorkingFolder:                 cmdReq.WorkingFolder,
		RunWhetherUserIsLoggedOnOrNot: cmdReq.RunWhetherUserIsLoggedOnOrNot,
		User:                          user,
		Password:                      cmdReq.UserPassword,
		RunWithHighestPrivileges:      cmdReq.RunWithHighestPrivileges,
	}

	// Объект команды для отправки – расширенная структура
	fullCmd := FullMQTTCommand{
		Date_Of_Creation: dateOfCreation,
		MQTTCommand:      mqttCmd,
	}

	payload, err := json.Marshal(fullCmd)
	if err != nil {
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	// Получаем информацию о админе
	authInfo, err := getAuthInfoFromRequest(r)
	if err != nil {
		log.Printf("Ошибка получения информации о админах: %v", err)
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	// Формируем карту для ClientID_Command: id -> { "ClientName": <client name>, "Answer": "" }
	clientMapping := map[string]any{}
	for _, cid := range cmdReq.ClientIDs {
		name, err := getClientName(cid)
		if err != nil {
			log.Printf("Ошибка получения имени для клиента %s: %v", cid, err)
			clientMapping[cid] = map[string]string{
				"ClientName": "",
				"Answer":     "",
			}
		} else {
			clientMapping[cid] = map[string]string{
				"ClientName": name,
				"Answer":     "",
			}
		}
	}

	// Подготавливаем данные для записи в БД
	entry := map[string]any{
		"Date_Of_Creation": dateOfCreation,
		"Team_Command":     string(payload),
		"ClientID_Command": clientMapping,
		"SentFor":          []string{},        // Список клиентов, которым уже отправлена команда
		"ResendRequested":  map[string]bool{}, // Флаг для повторной отправки для каждого клиента (ключ – clientID, значение bool)
		"Created_By":       authInfo.Name,     // Имя админа, создавшего запрос
		"Created_By_Login": authInfo.Login,    // Логин админа, создавшего запрос
	}

	entryBytes, err := json.Marshal(entry)
	if err != nil {
		http.Error(w, "Ошибка подготовки данных для БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Формируем ключ и подготавливаем запись
	dbKey := "FiReMQ_Command:" + dateOfCreation

	// Запись в BadgerDB с использованием батчи
	wb := db.DBInstance.NewWriteBatch()
	defer wb.Cancel() // Отменяем батч, если не произойдёт Flush

	if err := wb.Set([]byte(dbKey), entryBytes); err != nil {
		http.Error(w, "Ошибка записи в БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := wb.Flush(); err != nil {
		http.Error(w, "Ошибка при сохранении батча в БД: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Отправка команды сразу онлайн клиентам через AutoPaho
	var sentTo []string
	for _, clientID := range cmdReq.ClientIDs {
		online, err := isClientOnline(clientID)
		if err != nil {
			log.Printf("Ошибка проверки статуса клиента %s: %v", clientID, err)
			continue
		}
		if online {
			topic := fmt.Sprintf("Client/%s/ModuleCommand", clientID)
			if err := mqtt_client.Publish(topic, payload, 2); err != nil {
				log.Printf("Не удалось опубликовать в топик %s: %v", topic, err)
				continue
			}
			sentTo = append(sentTo, clientID)

			// Синхронизируем очередь: считаем, что мы только что отправили → выдержим паузу 10 сек до следующей отправки
			val, _ := cmdSendQueues.LoadOrStore(clientID, &cmdClientQueue{})
			q := val.(*cmdClientQueue)
			q.mu.Lock()
			q.lastSend = time.Now()
			q.mu.Unlock()
		}
	}

	// Обновляем SentFor в записи — чтобы очередь не переслала дубликаты
	if len(sentTo) > 0 {
		if err := db.DBInstance.Update(func(txn *badger.Txn) error {
			item, err := txn.Get([]byte(dbKey))
			if err != nil {
				return nil
			}
			var record map[string]any
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			}); err != nil {
				return nil
			}

			// Считываем текущий SentFor
			var sentFor []string
			if s, exists := record["SentFor"]; exists {
				if arr, ok := s.([]any); ok {
					for _, v := range arr {
						if ss, ok := v.(string); ok {
							sentFor = append(sentFor, ss)
						}
					}
				}
			}
			// Объединяем с sentTo (без дублей)
			for _, id := range sentTo {
				found := slices.Contains(sentFor, id)
				if !found {
					sentFor = append(sentFor, id)
				}
			}
			record["SentFor"] = sentFor

			newBytes, err := json.Marshal(record)
			if err != nil {
				return err
			}
			return txn.Set([]byte(dbKey), newBytes)
		}); err != nil {
			log.Printf("Ошибка обновления SentFor в БД: %v", err)
		}
	}

	// Отправляем ответ, что команда сохранена и отправлена онлайн клиентам
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "Успех",
		"message": "Команда сохранена и отправлена онлайн клиентам",
	})
}
