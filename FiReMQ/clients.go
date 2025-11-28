// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"FiReMQ/db"          // Локальный пакет с БД BadgerDB
	"FiReMQ/mqtt_server" // Локальный пакет MQTT клиента Mocho-MQTT
	"FiReMQ/pathsOS"     // Локальный пакет с путями для разных платформ

	"github.com/dgraph-io/badger/v4"
)

// Переменные для таймеров, обновляющих статусы клиентов
var (
	statusClientRunning bool          // Флаг запуска цикла StatusClient
	statusClientStop    chan struct{} // Канал для остановки цикла
	activityTimer       *time.Timer   // Таймер активности пользователя
)

// BackgroundStatusUpdate запускает фоновое обновление статусов клиентов, когда основной цикл неактивен
func BackgroundStatusUpdate() {
	ticker := time.NewTicker(15 * time.Second) // Интервал фонового обновления
	defer ticker.Stop()

	for range ticker.C {
		if !statusClientRunning {
			updateClientStatus()
		}
	}
}

// SaveClientInfo сохраняет информацию о клиенте в БД, не затирая существующее имя
func SaveClientInfo(status, name, ip, localIP, clientID string) error {
	var becameOnline bool // Флаг перехода клиента из оффлайн в онлайн

	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		entry, err := txn.Get([]byte("client:" + clientID))
		if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

		var data map[string]string
		if entry != nil {
			err = entry.Value(func(val []byte) error {
				return json.Unmarshal(val, &data)
			})
			if err != nil {
				return err
			}
		} else {
			data = make(map[string]string)
		}

		prevStatus := data["status"] // Запоминаем предыдущий статус

		// Проверяем, изменились ли данные
		changed := false

		if data["status"] != status {
			data["status"] = status
			data["time_stamp"] = time.Now().Format("02.01.06(15:04)")
			changed = true

			// Отмечаем переход из оффлайна в онлайн
			if prevStatus != "On" && status == "On" {
				becameOnline = true
			}
		}

		if data["name"] == "" && name != "" {
			data["name"] = name
			changed = true
		}

		if data["ip"] != ip {
			data["ip"] = ip
			changed = true
		}

		if data["local_ip"] != localIP {
			data["local_ip"] = localIP
			changed = true
		}

		if data["client_id"] != clientID {
			data["client_id"] = clientID
			changed = true
		}

		// Устанавливаем группу и подгруппу по умолчанию, если они не заданы
		if data["group"] == "" {
			data["group"] = "Новые клиенты"
			changed = true
		}
		if data["subgroup"] == "" {
			data["subgroup"] = "Нераспределённые"
			changed = true
		}

		// Если данные не изменились, прерываем транзакцию
		if !changed {
			return nil
		}

		jsonData, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return txn.Set([]byte("client:"+clientID), jsonData)
	})

	if err == nil && becameOnline {
		// При переходе в онлайн сначала проверяется очередь на удаление, а затем переотправляются команды
		exists, perr := isPendingUninstall(clientID)
		if perr == nil && !exists {
			go checkAndResendCommands(clientID) // Для команд CMD/PowerShell
			go checkAndResendQUIC(clientID)     // Для установки ПО (QUIC)
		}
		go schedulePendingUninstall(clientID) // Для самоудаления "FiReAgent"
	}

	return err
}

// DeleteClient удаляет клиента из базы данных
func DeleteClient(clientID string) error {
	return db.DBInstance.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte("client:" + clientID))
	})
}

// DeleteSelectedClients удаляет группу клиентов из базы данных
func DeleteSelectedClients(clientIDs []string) error {
	wb := db.DBInstance.NewWriteBatch() // Пакетная запись для производительности
	defer wb.Cancel()

	for _, clientID := range clientIDs {
		key := []byte("client:" + clientID)
		if err := wb.Delete(key); err != nil {
			return err
		}
	}

	return wb.Flush() // Фиксация транзакции
}

// UpdateOrInsertClient обновляет или вставляет данные клиента в базу данных
func UpdateOrInsertClient(status, name, ip, localIP, clientID string) error {
	return db.DBInstance.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("client:" + clientID))
		if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

		var data map[string]string
		if item != nil {
			err = item.Value(func(val []byte) error {
				return json.Unmarshal(val, &data)
			})
			if err != nil {
				return err
			}
		} else {
			data = make(map[string]string)
		}

		// Если данные не изменились, запись не выполняется
		if data["status"] == status && data["ip"] == ip && data["local_ip"] == localIP && data["client_id"] == clientID {
			return nil
		}

		data["status"] = status
		data["name"] = name
		data["ip"] = ip
		data["local_ip"] = localIP
		data["client_id"] = clientID
		data["time_stamp"] = time.Now().Format("02.01.06(15:04)")

		jsonData, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return txn.Set([]byte("client:"+clientID), jsonData)
	})
}

// resetActivityTimer сбрасывает таймер активности пользователя
func resetActivityTimer() {
	stopActivityTimer() // Останавливаем текущий таймер, если он существует

	activityTimer = time.AfterFunc(2*time.Minute, func() { // Время бездействия до остановки StatusClient
		if statusClientRunning {
			close(statusClientStop)
			statusClientRunning = false
		}
	})
}

// stopActivityTimer останавливает таймер активности
func stopActivityTimer() {
	if activityTimer != nil {
		activityTimer.Stop()
		activityTimer = nil
	}
}

// StatusClient запускает цикл обновления статусов подключенных клиентов
func StatusClient() {
	if statusClientRunning {
		return // Цикл уже запущен
	}

	statusClientRunning = true
	statusClientStop = make(chan struct{}) // Пересоздаём канал остановки

	resetActivityTimer() // Запускаем таймер активности

	go func() {
		defer func() {
			statusClientRunning = false
			stopActivityTimer() // Останавливаем таймер при завершении работы
		}()

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		updateClientStatus() // Немедленно выполняем обновление статуса

		// Запускаем цикл для периодического выполнения
		for {
			select {
			case <-statusClientStop:
				return // Останавливаем цикл
			case <-ticker.C:
				updateClientStatus()
			}
		}
	}()
}

// updateClientStatus обновляет статусы всех клиентов в одной транзакции
func updateClientStatus() {
	// Получаем актуальный список ID всех подключенных клиентов
	connectedClients := mqtt_server.Server.Clients.GetAll()
	onlineClientIDs := make(map[string]struct{}, len(connectedClients))
	for _, client := range connectedClients {
		onlineClientIDs[client.GetID()] = struct{}{}
	}

	var hadStatusChanges bool    // Флаг наличия изменений статусов
	var newlyOnlineIDs []string  // Клиенты, перешедшие в онлайн
	var newlyOfflineIDs []string // Клиенты, перешедшие в оффлайн

	// Запускаем одну транзакцию для обновления всех статусов
	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("client:")
		it := txn.NewIterator(opts)
		defer it.Close()

		// Итерируемся по всем клиентам в БД
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := string(item.Key())
			clientID := key[len("client:"):]

			var data map[string]string
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &data)
			})
			if err != nil {
				log.Printf("Ошибка десериализации данных для клиента %s: %v", clientID, err)
				continue // Пропускаем поврежденную запись, но не прерываем транзакцию
			}

			_, isOnline := onlineClientIDs[clientID]
			currentStatus := data["status"]
			needsUpdate := false

			if isOnline && currentStatus != "On" {
				// Случай 1: Клиент онлайн, а в БД оффлайн, меняем на "On"
				data["status"] = "On"
				newlyOnlineIDs = append(newlyOnlineIDs, clientID)
				needsUpdate = true
			} else if !isOnline && currentStatus != "Off" {
				// Случай 2: Клиент оффлайн, а в БД онлайн, меняем на "Off"
				data["status"] = "Off"
				newlyOfflineIDs = append(newlyOfflineIDs, clientID)
				needsUpdate = true
			}

			if needsUpdate {
				hadStatusChanges = true
				data["time_stamp"] = time.Now().Format("02.01.06(15:04)")
				jsonData, err := json.Marshal(data)
				if err != nil {
					return err // Критическая ошибка, прерываем транзакцию
				}
				if err := txn.Set(item.KeyCopy(nil), jsonData); err != nil {
					return err // Критическая ошибка, прерываем транзакцию
				}
			}
		}
		return nil
	})

	if err != nil {
		log.Printf("Критическая ошибка во время транзакции обновления статусов клиентов: %v", err)
	}

	// Фоновые задачи запускаются после транзакции, чтобы не блокировать БД
	for _, clientID := range newlyOnlineIDs {
		exists, perr := isPendingUninstall(clientID)
		if perr == nil && !exists {
			go checkAndResendCommands(clientID)
			go checkAndResendQUIC(clientID)
		}
		go schedulePendingUninstall(clientID)
	}

	for _, clientID := range newlyOfflineIDs {
		go markQUICResendOnOffline(clientID)
	}

	// При изменении статусов необходимо пересчитать доступ к QUIC
	if hadStatusChanges {
		RecalculateQUICAccess("статус одного или нескольких клиентов изменился")
	}
}

// removeClientIDsFromCommandRecords удаляет ID клиентов из записей команд
func removeClientIDsFromCommandRecords(clientIDs []string) error {
	if len(clientIDs) == 0 {
		return nil
	}
	idsSet := make(map[string]struct{}, len(clientIDs))
	for _, id := range clientIDs {
		idsSet[id] = struct{}{}
	}

	return db.DBInstance.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_Command:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)

			var record map[string]any
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			}); err != nil {
				continue
			}

			mapping, ok := record["ClientID_Command"].(map[string]any)
			if !ok || len(mapping) == 0 {
				continue
			}

			changed := false
			for id := range idsSet {
				if _, ex := mapping[id]; ex {
					delete(mapping, id)
					changed = true
				}
			}
			if !changed {
				continue
			}

			// Очищаем SentFor
			if sf, ok := record["SentFor"]; ok {
				if arr, ok := sf.([]any); ok {
					filtered := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							if _, drop := idsSet[s]; !drop {
								filtered = append(filtered, s)
							}
						}
					}
					record["SentFor"] = filtered
				}
			}

			// Очищаем ResendRequested
			if rr, ok := record["ResendRequested"].(map[string]any); ok && rr != nil {
				for id := range idsSet {
					delete(rr, id)
				}
				record["ResendRequested"] = rr
			}

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
		return nil
	})
}

// removeClientIDsFromQUICRecords удаляет ID клиентов из записей QUIC
func removeClientIDsFromQUICRecords(clientIDs []string) error {
	if len(clientIDs) == 0 {
		return nil
	}
	idsSet := make(map[string]struct{}, len(clientIDs))
	for _, id := range clientIDs {
		idsSet[id] = struct{}{}
	}

	var filesToMaybeDelete []string

	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_QUIC:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)

			var record map[string]any
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			}); err != nil {
				continue
			}

			mapping, ok := record["ClientID_QUIC"].(map[string]any)
			if !ok || len(mapping) == 0 {
				continue
			}

			changed := false
			for id := range idsSet {
				if _, ex := mapping[id]; ex {
					delete(mapping, id)
					changed = true
				}
			}
			if !changed {
				continue
			}

			// Очищаем SentFor
			if sf, ok := record["SentFor"]; ok {
				if arr, ok := sf.([]any); ok {
					filtered := make([]string, 0, len(arr))
					for _, v := range arr {
						if s, ok := v.(string); ok {
							if _, drop := idsSet[s]; !drop {
								filtered = append(filtered, s)
							}
						}
					}
					record["SentFor"] = filtered
				}
			}

			// Очищаем ResendRequested
			if rr, ok := record["ResendRequested"].(map[string]any); ok && rr != nil {
				for id := range idsSet {
					delete(rr, id)
				}
				record["ResendRequested"] = rr
			}

			if len(mapping) == 0 {
				// Если запись стала пустой, удаляем её и, возможно, связанный файл
				if fn, err := extractFileNameFromQUICRecord(record); err == nil && strings.TrimSpace(fn) != "" {
					filesToMaybeDelete = append(filesToMaybeDelete, fn)
				} else if err != nil {
					log.Printf("Не удалось извлечь имя файла из QUIC записи: %v", err)
				}
				if err := txn.Delete(key); err != nil {
					return err
				}
			} else {
				record["ClientID_QUIC"] = mapping
				newBytes, err := json.Marshal(record)
				if err != nil {
					return err
				}
				if err := txn.Set(key, newBytes); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Удаляем файл, если он больше не используется
	if len(filesToMaybeDelete) > 0 {
		uniq := make(map[string]struct{})
		for _, f := range filesToMaybeDelete {
			f = strings.TrimSpace(f)
			if f != "" {
				uniq[f] = struct{}{}
			}
		}
		for f := range uniq {
			deleteQUICFileIfUnreferenced(f)
		}
	}

	// После изменений пересчитываем доступ к QUIC
	RecalculateQUICAccess("удаление клиента(ов) из QUIC-отчетов")
	return nil
}

// cleanupClientsRuntimeState очищает runtime-состояния удаленных клиентов
func cleanupClientsRuntimeState(clientIDs []string) {
	for _, id := range clientIDs {
		cmdSendQueues.Delete(id)  // Очереди отправки CMD/PowerShell
		quicSendQueues.Delete(id) // Очереди отправки QUIC
		// Активные QUIC-сессии/токены
		sessionMutex.Lock()
		if s, ok := sessionStore[id]; ok {
			if s.Cancel != nil {
				close(s.Cancel)
			}
			delete(sessionStore, id)
		}
		sessionMutex.Unlock()
	}
}

// fullyRemoveClientAndData инкапсулирует логику полного удаления клиента и связанных с ним данных
func fullyRemoveClientAndData(clientIDs []string) error {
	if len(clientIDs) == 0 {
		return nil
	}

	// Удаляем клиентов из основной БД
	if err := DeleteSelectedClients(clientIDs); err != nil {
		// Если не удалось удалить из БД, продолжать нет смысла
		return fmt.Errorf("ошибка удаления клиентов из БД: %w", err)
	}

	// Удаляем файлы отчетов, логируя некритичные ошибки
	for _, clientID := range clientIDs {
		filePathLite := filepath.Join(pathsOS.Path_Info, "Lite_"+clientID+".html.xz")
		if err := os.Remove(filePathLite); err != nil && !os.IsNotExist(err) {
			log.Printf("Ошибка удаления файла отчета %s: %v", filePathLite, err)
		}
		filePathAida := filepath.Join(pathsOS.Path_Info, "Aida_"+clientID+".html.xz")
		if err := os.Remove(filePathAida); err != nil && !os.IsNotExist(err) {
			log.Printf("Ошибка удаления файла отчета %s: %v", filePathAida, err)
		}
	}

	// Очищаем все отчёты
	if err := removeClientIDsFromCommandRecords(clientIDs); err != nil {
		log.Printf("Ошибка удаления клиентов из отчётов CMD/PowerShell: %v", err)
	}
	if err := removeClientIDsFromQUICRecords(clientIDs); err != nil {
		log.Printf("Ошибка удаления клиентов из отчётов Установки ПО: %v", err)
	}

	// Очищаем runtime-состояния (очереди, сессии)
	cleanupClientsRuntimeState(clientIDs)

	return nil
}
