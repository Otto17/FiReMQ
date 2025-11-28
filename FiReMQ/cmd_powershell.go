// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"FiReMQ/db"          // Локальный пакет с БД BadgerDB
	"FiReMQ/mqtt_client" // Локальный пакет с MQTT клиентом AutoPaho

	"github.com/dgraph-io/badger/v4"
)

// cmdClientQueue Очередь отправки по клиенту
type cmdClientQueue struct {
	mu       sync.Mutex
	running  bool
	lastSend time.Time
}

var cmdSendQueues sync.Map // key: clientID -> *cmdClientQueue

// cmdQueueInterval Интервал между отправками запросов одному клиенту
const cmdQueueInterval = 10 * time.Second

// parseCmdDate Парсит "Date_Of_Creation"
func parseCmdDate(s string) time.Time {
	idx := strings.LastIndex(s, ":")
	if idx == -1 {
		if t, err := time.Parse("02.01.06(15:04:05)", s); err == nil {
			return t
		}
		return time.Time{}
	}
	base := s[:idx]
	msStr := s[idx+1:]
	t, err := time.Parse("02.01.06(15:04:05)", base)
	if err != nil {
		return time.Time{}
	}
	ms, _ := strconv.Atoi(msStr)
	return t.Add(time.Duration(ms) * time.Millisecond)
}

// prepareNextTerminalMessage Подготавливает следующие к отправке записи (начиная с самой старой)
func prepareNextTerminalMessage(clientID string) (topic string, payload []byte, ok bool) {
	var (
		chosenKey    []byte
		chosenRecord map[string]any
		chosenTime   time.Time
		choose       bool

		outPayload []byte
		outTopic   string
	)

	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_Command:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var record map[string]any
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			}); err != nil {
				continue
			}

			mapping, ok := record["ClientID_Command"].(map[string]any)
			if !ok {
				continue
			}
			ce, _ := mapping[clientID].(map[string]any)
			if ce == nil {
				continue
			}

			// Должен быть незавершённый (Answer пуст)
			ans, _ := ce["Answer"].(string)
			if strings.TrimSpace(ans) != "" {
				continue
			}

			// alreadySent?
			alreadySent := false
			if sentForAny, ok := record["SentFor"].([]any); ok {
				for _, v := range sentForAny {
					if s, ok := v.(string); ok && s == clientID {
						alreadySent = true
						break
					}
				}
			}

			// ResendRequested?
			rr := false
			if rrMap, ok := record["ResendRequested"].(map[string]any); ok {
				if b, ok := rrMap[clientID].(bool); ok && b {
					rr = true
				}
			}

			// Если уже отправляли и не стоит флаг повторной — пропускаем
			if alreadySent && !rr {
				continue
			}

			dateStr, _ := record["Date_Of_Creation"].(string)
			t := parseCmdDate(dateStr)
			if !choose || (!t.IsZero() && t.Before(chosenTime)) || (chosenTime.IsZero() && !t.IsZero()) {
				choose = true
				chosenKey = item.KeyCopy(nil)
				chosenRecord = record
				chosenTime = t
			}
		}

		if !choose {
			return nil
		}

		// Готовим payload
		cmdStr, ok := chosenRecord["Team_Command"].(string)
		if !ok || cmdStr == "" {
			return nil
		}
		outPayload = []byte(cmdStr)
		outTopic = "Client/" + clientID + "/ModuleCommand"

		// Обновим SentFor
		var sentFor []string
		if s, ok := chosenRecord["SentFor"]; ok {
			if arr, ok := s.([]any); ok {
				for _, vv := range arr {
					if ss, ok := vv.(string); ok {
						sentFor = append(sentFor, ss)
					}
				}
			}
		}
		found := false
		for _, s := range sentFor {
			if s == clientID {
				found = true
				break
			}
		}
		if !found {
			sentFor = append(sentFor, clientID)
			chosenRecord["SentFor"] = sentFor
		}

		// Снимем флаг ResendRequested для этого клиента
		if rrMap, ok := chosenRecord["ResendRequested"].(map[string]any); ok {
			if _, ex := rrMap[clientID]; ex {
				delete(rrMap, clientID)
				chosenRecord["ResendRequested"] = rrMap
			}
		}

		// Сохраняем изменения
		newBytes, err := json.Marshal(chosenRecord)
		if err != nil {
			return err
		}
		return txn.Set(chosenKey, newBytes)
	})

	if err != nil || !choose {
		return "", nil, false
	}
	return outTopic, outPayload, true
}

// startCmdQueueForClient Запускает очередь отправки для клиента
func startCmdQueueForClient(clientID string) {
	val, _ := cmdSendQueues.LoadOrStore(clientID, &cmdClientQueue{})
	q := val.(*cmdClientQueue)

	q.mu.Lock()
	if q.running {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.mu.Unlock()

	go func() {
		defer func() {
			q.mu.Lock()
			q.running = false
			q.mu.Unlock()
		}()

		for {
			// Прерываем, если клиент офлайн
			online, _ := isClientOnline(clientID)
			if !online {
				return
			}

			// Ждём интервал между отправками
			q.mu.Lock()
			now := time.Now()
			wait := cmdQueueInterval - now.Sub(q.lastSend)
			q.mu.Unlock()
			if wait > 0 {
				time.Sleep(wait)
			}

			// Берём самую старую подходящую запись
			topic, payload, ok := prepareNextTerminalMessage(clientID)
			if !ok {
				return // Нечего слать
			}

			if err := mqtt_client.Publish(topic, payload, 2); err != nil {
				log.Printf("CMD очередь: ошибка публикации для %s: %v", clientID, err)
				time.Sleep(3 * time.Second)
				continue
			}

			// Отметим момент отправки
			q.mu.Lock()
			q.lastSend = time.Now()
			q.mu.Unlock()
		}
	}()
}

// checkAndResendCommands Запускает очередь после небольшой задержки
func checkAndResendCommands(clientID string) {
	// Ждем 3 секунды, чтобы клиент успел корректно запуститься
	time.Sleep(3 * time.Second)
	startCmdQueueForClient(clientID)
}

// HandleAnswerMessage Обрабатывает ответ клиента, обновляя соответствующую запись в БД
func HandleAnswerMessage(clientID string, payload []byte) {
	var ansMsg struct {
		Date_Of_Creation string `json:"Date_Of_Creation"`
		Answer           string `json:"Answer"`
	}

	if err := json.Unmarshal(payload, &ansMsg); err != nil {
		log.Printf("Ошибка парсинга ответа от клиента %s: %v", clientID, err)
		return
	}

	// Формируем ключ по Date_Of_Creation (ключ: "FiReMQ_Command:<Date_Of_Creation>")
	dbKey := "FiReMQ_Command:" + ansMsg.Date_Of_Creation
	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(dbKey))
		if err != nil {
			return err
		}

		var record map[string]any
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &record)
		}); err != nil {
			return err
		}

		mapping, ok := record["ClientID_Command"].(map[string]any)
		if !ok {
			return nil
		}

		clientEntryRaw, exists := mapping[clientID]
		if !exists {
			return nil
		}

		clientEntry, ok := clientEntryRaw.(map[string]any)
		if !ok {
			return nil
		}

		// Если для этого клиента уже установлен ответ, ничего не меняем
		if ans, _ := clientEntry["Answer"].(string); ans != "" {
			return nil
		}

		clientEntry["Answer"] = ansMsg.Answer
		mapping[clientID] = clientEntry
		record["ClientID_Command"] = mapping
		newBytes, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return txn.Set([]byte(dbKey), newBytes)
	})

	if err != nil {
		log.Printf("Ошибка обновления записи для ответа от клиента %s: %v", clientID, err)
	} else {
		log.Printf("Обновлена запись команды %s для клиента %s", ansMsg.Date_Of_Creation, clientID)
	}
}

// getClientName Возвращает имя клиента (поле "name") из записи с ключом "client:<clientID>".
func getClientName(clientID string) (string, error) {
	var name string
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("client:" + clientID))
		if err != nil {
			return err
		}
		var data map[string]string
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &data)
		}); err != nil {
			return err
		}
		name = data["name"]
		return nil
	})
	return name, err
}
