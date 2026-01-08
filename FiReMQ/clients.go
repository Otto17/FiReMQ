// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"FiReMQ/db"          // Локальный пакет с БД BadgerDB
	"FiReMQ/logging"     // Локальный пакет с логированием в HTML файл
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

		prevStatus := data["status"] // Запоминает предыдущий статус

		// Проверяет, изменились ли данные
		changed := false

		if data["status"] != status {
			data["status"] = status
			data["time_stamp"] = time.Now().Format("02.01.06(15:04)")
			changed = true

			// Отмечает переход из оффлайна в онлайн
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

		// Устанавливает группу и подгруппу по умолчанию, если они не заданы
		if data["group"] == "" {
			data["group"] = "Новые клиенты"
			changed = true
		}
		if data["subgroup"] == "" {
			data["subgroup"] = "Нераспределённые"
			changed = true
		}

		// Если данные не изменились, прерывает транзакцию
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
	stopActivityTimer() // Останавливает текущий таймер, если он существует

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

	resetActivityTimer() // Запускает таймер активности

	go func() {
		defer func() {
			statusClientRunning = false
			stopActivityTimer() // Останавливает таймер при завершении работы
		}()

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		updateClientStatus() // Немедленно выполняет обновление статуса

		// Запускает цикл для периодического выполнения
		for {
			select {
			case <-statusClientStop:
				return // Останавливает цикл
			case <-ticker.C:
				updateClientStatus()
			}
		}
	}()
}

// updateClientStatus обновляет статусы всех клиентов
func updateClientStatus() {
	// Получает список тех, кто реально подключен к MQTT серверу сейчас
	connectedClients := mqtt_server.Server.Clients.GetAll()
	onlineClientIDs := make(map[string]struct{}, len(connectedClients))
	for _, client := range connectedClients {
		onlineClientIDs[client.GetID()] = struct{}{}
	}

	// Карта для хранения ID клиентов, статус которых нужно изменить
	clientsToUpdate := make(map[string]string) // Ключ: ID клиента, Значение: Новый статус ("On" или "Off")

	// Чтение (View): происходит поиск расхождений между БД и реальными статусами
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("client:")
		it := txn.NewIterator(opts)
		defer it.Close()

		// Итерация по всем клиентам в БД
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			clientID := string(item.Key())[len("client:"):] // Отрезает префикс, получая чистый ID

			var data map[string]string
			// Создаёт безопасную копию значения для работы вне итератора
			valCopy, err := item.ValueCopy(nil)
			if err != nil {
				continue
			}

			if err := json.Unmarshal(valCopy, &data); err != nil {
				continue
			}

			_, isOnline := onlineClientIDs[clientID]
			currentStatus := data["status"]

			// Сравнивает статус в БД с реальным подключением и фиксирует расхождения
			if isOnline && currentStatus != "On" {
				clientsToUpdate[clientID] = "On"
			} else if !isOnline && currentStatus != "Off" {
				clientsToUpdate[clientID] = "Off"
			}
		}
		return nil
	})

	if err != nil {
		logging.LogError("Клиенты: Ошибка чтения БД при проверке статусов: %v", err)
		return
	}

	// Если обновлять нечего - выходит, не создавая нагрузку на запись
	if len(clientsToUpdate) == 0 {
		return
	}

	// Списки для фоновых задач (заполняется после успешной записи)
	var newlyOnlineIDs []string
	var newlyOfflineIDs []string

	// Запись (Update): обновляет только найденные расхождения
	maxRetries := 3 // Повторные попытки на случай конфликта (по конкретным ключам, а не по всем записям)
	for i := range maxRetries {
		err = db.DBInstance.Update(func(txn *badger.Txn) error {
			// Очистка списков перед каждой попыткой транзакции
			newlyOnlineIDs = nil
			newlyOfflineIDs = nil

			for clientID, newStatus := range clientsToUpdate {
				key := []byte("client:" + clientID)
				item, err := txn.Get(key)
				if err != nil {
					if err == badger.ErrKeyNotFound {
						continue // Клиент мог быть удалён между View и Update
					}
					return err
				}

				var data map[string]string

				// Читает актуальные данные внутри транзакции (защита от гонки данных)
				val, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}

				if err := json.Unmarshal(val, &data); err != nil {
					return err
				}

				// Повторная проверка статуса (если другая горутина уже обновила его, запись пропускается)
				if data["status"] == newStatus {
					continue
				}

				// Применяет изменения и обновляет метку времени
				data["status"] = newStatus
				data["time_stamp"] = time.Now().Format("02.01.06(15:04)")

				jsonData, err := json.Marshal(data)
				if err != nil {
					return err
				}

				// Фиксирует изменения в БД
				if err := txn.Set(key, jsonData); err != nil {
					return err
				}

				// Распределяет ID по спискам для последующего запуска фоновых задач
				if newStatus == "On" {
					newlyOnlineIDs = append(newlyOnlineIDs, clientID)
				} else {
					newlyOfflineIDs = append(newlyOfflineIDs, clientID)
				}
			}
			return nil
		})

		if err == nil {
			break // Успех, выход из цикла повторов
		}

		// Если конфликт - пробует ещё раз
		if errors.Is(err, badger.ErrConflict) {
			if i == maxRetries-1 {
				logging.LogError("Клиенты: Не удалось обновить статусы после %d попыток (конфликт): %v", maxRetries, err)
			}
			time.Sleep(10 * time.Millisecond) // Небольшая пауза перед повтором
			continue
		}

		// Если другая ошибка - логирует и выходит
		logging.LogError("Клиенты: Критическая ошибка записи статусов в БД: %v", err)
		return
	}

	// Запуск фоновых задач (только если запись прошла успешно)
	for _, clientID := range newlyOnlineIDs {
		// Проверяет, не стоит ли клиент в очереди на самоудаление
		exists, perr := isPendingUninstall(clientID)

		// Если удаления не планируется, запускает переотправку недоставленных команд и файлов
		if perr == nil && !exists {
			go checkAndResendCommands(clientID) // cmd/PowerShell
			go checkAndResendQUIC(clientID)     // QUIC
		}
		go schedulePendingUninstall(clientID) // Планирует проверку самоудаления
	}

	for _, clientID := range newlyOfflineIDs {
		// Отмечает сбой для ушедшего в оффлайн клиента
		go markQUICResendOnOffline(clientID)
	}

	// Пересчёт доступа к порту QUIC (если состав онлайн-клиентов изменился)
	if len(newlyOnlineIDs) > 0 || len(newlyOfflineIDs) > 0 {
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

		// Итерация по всем клиентам в БД
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

			// Очищает SentFor
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

			// Очищает ResendRequested
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
func removeClientIDsFromQUICRecords(clientIDs []string, authInfo *AuthInfo) error {
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

		// Итерация по всем клиентам в БД
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

			// Очищает SentFor
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

			// Очищает ResendRequested
			if rr, ok := record["ResendRequested"].(map[string]any); ok && rr != nil {
				for id := range idsSet {
					delete(rr, id)
				}
				record["ResendRequested"] = rr
			}

			if len(mapping) == 0 {
				// Если запись стала пустой, удаляет её и, возможно, связанный файл
				if fn, err := extractFileNameFromQUICRecord(record); err == nil && strings.TrimSpace(fn) != "" {
					filesToMaybeDelete = append(filesToMaybeDelete, fn)
				} else if err != nil {
					logging.LogError("Клиенты: Не удалось извлечь имя файла из QUIC записи: %v", err)
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

	// Удаляет файл, если он больше не используется
	if len(filesToMaybeDelete) > 0 {
		uniq := make(map[string]struct{})
		for _, f := range filesToMaybeDelete {
			f = strings.TrimSpace(f)
			if f != "" {
				uniq[f] = struct{}{}
			}
		}
		for f := range uniq {
			deleteQUICFileIfUnreferenced(f, authInfo)
		}
	}

	// После изменений пересчитывает доступ к QUIC
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
func fullyRemoveClientAndData(clientIDs []string, authInfo *AuthInfo) error {
	if len(clientIDs) == 0 {
		return nil
	}

	// Удаляет клиентов из основной БД
	if err := DeleteSelectedClients(clientIDs); err != nil {
		// Если не удалось удалить из БД, продолжать нет смысла
		return fmt.Errorf("ошибка удаления клиентов из БД: %w", err)
	}

	// Удаляет файлы отчетов, логируя некритичные ошибки
	for _, clientID := range clientIDs {
		filePathLite := filepath.Join(pathsOS.Path_Info, "Lite_"+clientID+".html.xz")
		if err := os.Remove(filePathLite); err != nil && !os.IsNotExist(err) {
			logging.LogError("Клиенты: Ошибка удаления файла отчета %s: %v", filePathLite, err)
		}
		filePathAida := filepath.Join(pathsOS.Path_Info, "Aida_"+clientID+".html.xz")
		if err := os.Remove(filePathAida); err != nil && !os.IsNotExist(err) {
			logging.LogError("Клиенты: Ошибка удаления файла отчета %s: %v", filePathAida, err)
		}
	}

	// Очищает все отчёты
	if err := removeClientIDsFromCommandRecords(clientIDs); err != nil {
		logging.LogError("Клиенты: Ошибка удаления клиентов из отчётов CMD/PowerShell: %v", err)
	}
	if err := removeClientIDsFromQUICRecords(clientIDs, authInfo); err != nil {
		logging.LogError("Клиенты: Ошибка удаления клиентов из отчётов Установки ПО: %v", err)
	}

	// Очищает runtime-состояния (очереди, сессии)
	cleanupClientsRuntimeState(clientIDs)

	return nil
}
