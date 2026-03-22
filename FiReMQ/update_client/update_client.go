package update_client

import (
	"encoding/json"
	"errors"
	"net/http"

	"FiReMQ/db"      // Локальный пакет с БД BadgerDB
	"FiReMQ/logging" // Локальный пакет с логированием в HTML файл

	"github.com/dgraph-io/badger/v4"
)

// Инъекции функций (внедряются из main.go для избежания циклического импорта)
var (
	// PublishMQTTMessage публикует MQTT-сообщение на указанный топик
	PublishMQTTMessage func(topic string, payload []byte, qos byte) error

	// GetAuthInfo получает информацию об авторизованном админе из HTTP-запроса
	GetAuthInfo func(r *http.Request) (login, name string, err error)
)

// UpdateClientData структура данных обновлений клиента в БД
type UpdateClientData struct {
	LastCheck string            `json:"last_check"` // Дата/время последней проверки обновлений (от клиента, формат "дд.мм.гг(ЧЧ:ММ:СС)")
	FiReAgent string            `json:"FiReAgent"`  // Версия основного исполняемого файла FiReAgent (формат "дд.мм.гг")
	Modules   map[string]string `json:"modules"`    // Динамический список модулей и их версий (формат версии "дд.мм.гг")
}

// ClientUpdateResponse структура ответа для GET запроса (основные данные клиента + информация об обновлениях)
type ClientUpdateResponse struct {
	ClientID  string            `json:"client_id"`
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	LastCheck string            `json:"last_check"`
	FiReAgent string            `json:"FiReAgent"`
	Modules   map[string]string `json:"modules"`
}

// SaveUpdateInfo сохраняет информацию об обновлениях клиента в БД
func SaveUpdateInfo(clientID string, data UpdateClientData) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return db.DBInstance.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("update_client:"+clientID), jsonData)
	})
}

// GetAllClientsWithUpdateInfo возвращает список всех клиентов с информацией об обновлениях
func GetAllClientsWithUpdateInfo() ([]ClientUpdateResponse, error) {
	// Собирает информацию об обновлениях из записей "update_client:"
	updateData := make(map[string]UpdateClientData)
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("update_client:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			clientID := string(item.Key())[len("update_client:"):]

			var data UpdateClientData
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &data)
			}); err != nil {
				logging.LogError("Обновления клиентов: Ошибка чтения данных обновлений клиента %s: %v", clientID, err)
				continue
			}
			updateData[clientID] = data
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Собирает основную информацию о клиентах из записей "client:" и объединяет с данными обновлений
	var result []ClientUpdateResponse
	err = db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("client:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			clientID := string(item.Key())[len("client:"):]

			var clientData map[string]string
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &clientData)
			}); err != nil {
				continue
			}

			resp := ClientUpdateResponse{
				ClientID: clientID,
				Name:     clientData["name"],
				Status:   clientData["status"],
			}

			// Добавляет информацию об обновлениях, если она есть для данного клиента
			if ud, exists := updateData[clientID]; exists {
				resp.LastCheck = ud.LastCheck
				resp.FiReAgent = ud.FiReAgent
				resp.Modules = ud.Modules
			}

			result = append(result, resp)
		}
		return nil
	})

	return result, err
}

// HandleUpdateVersions обрабатывает входящее MQTT-сообщение с версиями модулей от клиента
func HandleUpdateVersions(clientID string, payload []byte) {
	var data UpdateClientData
	if err := json.Unmarshal(payload, &data); err != nil {
		logging.LogError("Обновления клиентов: Ошибка парсинга JSON от клиента %s: %v", clientID, err)
		return
	}

	// Валидация обязательных полей (дата/время формируется на стороне клиента)
	if data.LastCheck == "" {
		logging.LogError("Обновления клиентов: Пустое поле 'last_check' от клиента %s", clientID)
		return
	}
	
	if data.FiReAgent == "" {
		logging.LogError("Обновления клиентов: Пустое поле 'FiReAgent' от клиента %s", clientID)
		return
	}

	if err := SaveUpdateInfo(clientID, data); err != nil {
		logging.LogError("Обновления клиентов: Ошибка сохранения данных обновлений клиента %s: %v", clientID, err)
		return
	}

	logging.LogSystem("Обновления клиентов: Получены версии модулей от клиента %s (последняя проверка: %s)", clientID, data.LastCheck)
}

// SendCheckUpdateToOnlineClients отправляет команду проверки обновлений всем онлайн-клиентам
func SendCheckUpdateToOnlineClients() (sent int, skipped int, err error) {
	if PublishMQTTMessage == nil {
		return 0, 0, errors.New("функция публикации MQTT не инициализирована")
	}

	// Формирует JSON-команду для клиента
	command := map[string]string{"UpdateAgent": "check_update"}
	cmdJSON, err := json.Marshal(command)
	if err != nil {
		return 0, 0, err
	}

	// Перебирает всех клиентов из БД и отправляет команду только онлайн-клиентам
	err = db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("client:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			clientID := string(item.Key())[len("client:"):]

			var clientData map[string]string
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &clientData)
			}); err != nil {
				continue
			}

			// Пропускает офлайн-клиентов (они проверяют обновления автоматически при запуске FiReAgent)
			if clientData["status"] != "On" {
				skipped++
				continue
			}

			// Отправляет MQTT-команду на принудительную проверку обновлений
			topic := "Server/" + clientID + "/CheckUpdateAgent"
			if pubErr := PublishMQTTMessage(topic, cmdJSON, 2); pubErr != nil {
				logging.LogError("Обновления клиентов: Ошибка отправки команды клиенту %s: %v", clientID, pubErr)
				skipped++
				continue
			}

			sent++
		}
		return nil
	})

	return sent, skipped, err
}

// GetUpdateClientsHandler обрабатывает GET запрос — возвращает список всех клиентов с информацией об обновлениях
func GetUpdateClientsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Разрешены только GET запросы", http.StatusMethodNotAllowed)
		return
	}

	clients, err := GetAllClientsWithUpdateInfo()
	if err != nil {
		logging.LogError("Обновления клиентов: Ошибка получения данных: %v", err)
		http.Error(w, "Ошибка получения данных", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(clients)
}

// SendCheckUpdateHandler обрабатывает POST запрос — отправляет команду проверки обновлений всем онлайн-клиентам
func SendCheckUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	sent, skipped, err := SendCheckUpdateToOnlineClients()
	if err != nil {
		logging.LogError("Обновления клиентов: Ошибка отправки команды проверки обновлений: %v", err)
		http.Error(w, "Ошибка отправки команды", http.StatusInternalServerError)
		return
	}

	// Логирует действие администратора
	if GetAuthInfo != nil {
		if login, name, authErr := GetAuthInfo(r); authErr == nil {
			logging.LogAction("Обновления клиентов: Админ \"%s\" (с именем: %s) запросил принудительную проверку обновлений. Отправлено: %d, пропущено (офлайн): %d", login, name, sent, skipped)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"sent":    sent,
		"skipped": skipped,
	})
}

// DeleteUpdateInfo удаляет информацию об обновлениях клиента из БД (вызывается при удалении клиента)
func DeleteUpdateInfo(clientID string) error {
	return db.DBInstance.Update(func(txn *badger.Txn) error {
		key := []byte("update_client:" + clientID)
		_, err := txn.Get(key)
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil // Запись не существует, удалять нечего
			}
			return err
		}
		return txn.Delete(key)
	})
}
