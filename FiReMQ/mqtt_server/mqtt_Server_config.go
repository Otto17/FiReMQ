// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package mqtt_server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"FiReMQ/db"          // Локальный пакет с БД BadgerDB
	"FiReMQ/logging"     // Локальный пакет с логированием в HTML файл
	"FiReMQ/mqtt_client" // Локальный пакет MQTT клиента AutoPaho
	"FiReMQ/pathsOS"     // Локальный пакет с путями для разных платформ
	"FiReMQ/protection"  // Локальный пакет с функциями базовой защиты

	"github.com/dgraph-io/badger/v4"
)

// GetAuthInfo функция для получения информации об авторизованном админе из запроса (защита от циклического импорта)
var GetAuthInfo func(r *http.Request) (login, name string, err error)

// CheckPermSystemSettings функция для проверки права на системные настройки (защита от циклического импорта)
var CheckPermSystemSettings func(login string) bool

// MQTTAuthChange хранит информацию о текущей сессии смены MQTT авторизации
type MQTTAuthChange struct {
	CreatedAt   string                          `json:"created_at"`   // Дата создания запроса
	NewUsername string                          `json:"new_username"` // Новый логин
	NewPassword string                          `json:"new_password"` // Новый пароль (хранится временно для повторных отправок)
	Clients     map[string]MQTTAuthClientStatus `json:"clients"`      // Статусы клиентов
}

// MQTTAuthClientStatus хранит статус смены авторизации для конкретного клиента
type MQTTAuthClientStatus struct {
	ClientName string `json:"client_name"` // Имя клиента для отображения
	Status     string `json:"status"`      // pending, success, error
	Message    string `json:"message"`     // Описание ошибки (если есть)
	UpdatedAt  string `json:"updated_at"`  // Дата последнего обновления
}

// Константа для ключа активной сессии смены авторизации в БД
const mqttAuthActiveKey = "MQTT_Auth:active"

// restartMu защищает RestartMqttHandler от одновременных вызовов
var restartMu sync.Mutex

// mqttAuthSendTracker отмечает клиентов, которым уже отправлена команда в текущей сессии (сбрасывается при повторе или очистке)
var mqttAuthSendTracker sync.Map // key: clientID -> struct{}

// MQTTConfig Структура для хранения шаблона JSON-конфигурации "mqtt_config.json"
type MQTTConfig struct {
	Hooks struct {
		Auth struct {
			Comment  string `json:"_comment"`
			AllowAll bool   `json:"allow_all"`
			Ledger   struct {
				Comment string `json:"_comment"`
				Auth    []struct {
					Comment  string `json:"_comment"`
					Account  int    `json:"account"`
					Allow    bool   `json:"allow"`
					Password string `json:"password"`
					Username string `json:"username"`
				} `json:"auth"`
			} `json:"ledger"`
		} `json:"auth"`
		Debug struct {
			Comment string `json:"_comment"`
			Enable  bool   `json:"enable"`
		} `json:"debug"`
	} `json:"hooks"`
	Logging struct {
		Comment string `json:"_comment"`
		Level   string `json:"level"`
	} `json:"logging"`
}

// EnsureMQTTConfig создает конфигурационный файл, если он отсутствует
func EnsureMQTTConfig() error {
	if _, err := os.Stat(pathsOS.Path_Config_MQTT); os.IsNotExist(err) {

		// Создает директорию "config" с правильными правами доступа
		if err := pathsOS.EnsureDir(filepath.Dir(pathsOS.Path_Config_MQTT)); err != nil {
			return fmt.Errorf("ошибка при создании директории для mqtt_config.json: %v", err)
		}

		// Создает конфигурацию по умолчанию
		defaultConfig := MQTTConfig{
			Hooks: struct {
				Auth struct {
					Comment  string `json:"_comment"`
					AllowAll bool   `json:"allow_all"`
					Ledger   struct {
						Comment string `json:"_comment"`
						Auth    []struct {
							Comment  string `json:"_comment"`
							Account  int    `json:"account"`
							Allow    bool   `json:"allow"`
							Password string `json:"password"`
							Username string `json:"username"`
						} `json:"auth"`
					} `json:"ledger"`
				} `json:"auth"`
				Debug struct {
					Comment string `json:"_comment"`
					Enable  bool   `json:"enable"`
				} `json:"debug"`
			}{
				Auth: struct {
					Comment  string `json:"_comment"`
					AllowAll bool   `json:"allow_all"`
					Ledger   struct {
						Comment string `json:"_comment"`
						Auth    []struct {
							Comment  string `json:"_comment"`
							Account  int    `json:"account"`
							Allow    bool   `json:"allow"`
							Password string `json:"password"`
							Username string `json:"username"`
						} `json:"auth"`
					} `json:"ledger"`
				}{
					Comment:  "Запрещает всем пользователям доступ без авторизации",
					AllowAll: false,
					Ledger: struct {
						Comment string `json:"_comment"`
						Auth    []struct {
							Comment  string `json:"_comment"`
							Account  int    `json:"account"`
							Allow    bool   `json:"allow"`
							Password string `json:"password"`
							Username string `json:"username"`
						} `json:"auth"`
					}{
						Comment: "Конфигурация для ведения учета пользователей",
						Auth: []struct {
							Comment  string `json:"_comment"`
							Account  int    `json:"account"`
							Allow    bool   `json:"allow"`
							Password string `json:"password"`
							Username string `json:"username"`
						}{
							{
								Comment:  "account - задаёт приоритет, 0 - выше приоритетом, чем 1",
								Account:  0,
								Allow:    true,
								Password: "FiReMQ",
								Username: "FiReMQ",
							},
							{
								Comment:  "allow - разрешает или запрещает доступ подключения пользователю",
								Account:  1,
								Allow:    false,
								Password: "",
								Username: "",
							},
						},
					},
				},
				Debug: struct {
					Comment string `json:"_comment"`
					Enable  bool   `json:"enable"`
				}{
					Comment: "Включает режим отладки",
					Enable:  false,
				},
			},
			Logging: struct {
				Comment string `json:"_comment"`
				Level   string `json:"level"`
			}{
				Comment: "Уровень логирования (DEBUG, INFO, WARN, ERROR)",
				Level:   "WARN",
			},
		}

		// Сериализует конфигурацию в JSON с отступами
		configBytes, err := json.MarshalIndent(defaultConfig, "", " ")
		if err != nil {
			return fmt.Errorf("ошибка при сериализации JSON: %v", err)
		}

		if err := pathsOS.WriteFile(pathsOS.Path_Config_MQTT, configBytes, pathsOS.FilePerm); err != nil {
			return fmt.Errorf("ошибка при записи файла конфигурации: %v", err)
		}

		logging.LogSystem("MQTT Serv: Создан конфигурационный файл: %s", pathsOS.Path_Config_MQTT)
	}

	return nil
}

// GetAccountsHandler возвращает данные учетных записей из MQTT-конфигурации
func GetAccountsHandler(w http.ResponseWriter, r *http.Request) {
	// Читает конфигурацию из файла
	configBytes, err := os.ReadFile(pathsOS.Path_Config_MQTT)
	if err != nil {
		http.Error(w, "Ошибка при чтении конфигурации", http.StatusInternalServerError)
		return
	}

	var configData MQTTConfig
	if err := json.Unmarshal(configBytes, &configData); err != nil {
		http.Error(w, "Ошибка при парсинге конфигурации", http.StatusInternalServerError)
		return
	}

	// Извлекает данные учетных записей
	accounts := configData.Hooks.Auth.Ledger.Auth

	// Формирует список только с необходимыми полями
	var response []map[string]any
	for _, account := range accounts {
		filteredAccount := map[string]any{
			"account":  account.Account, // Прямой доступ к полю, без кастов
			"username": account.Username,
		}
		if account.Account == 1 {
			filteredAccount["allow"] = account.Allow
		}
		response = append(response, filteredAccount)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateAccountHandler обновляет учетные данные и меняет приоритеты между учетными записями в MQTT-конфигурации
func UpdateAccountHandler(w http.ResponseWriter, r *http.Request) {
	// Проверка прав на системные настройки
	if GetAuthInfo != nil && CheckPermSystemSettings != nil {
		login, _, err := GetAuthInfo(r)
		if err == nil && login != "" {
			if !CheckPermSystemSettings(login) {
				http.Error(w, "У вас нет прав на изменение MQTT авторизации", http.StatusForbidden)
				return
			}
		}
	}

	configBytes, err := os.ReadFile(pathsOS.Path_Config_MQTT)
	if err != nil {
		http.Error(w, "Ошибка при чтении конфигурации", http.StatusInternalServerError)
		return
	}

	var configData MQTTConfig
	if err := json.Unmarshal(configBytes, &configData); err != nil {
		http.Error(w, "Ошибка при парсинге конфигурации", http.StatusInternalServerError)
		return
	}

	var newAccountData struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&newAccountData); err != nil {
		http.Error(w, "Ошибка при парсинге данных запроса", http.StatusBadRequest)
		return
	}

	dataToValidate := map[string]string{
		"username": newAccountData.Username,
		"password": newAccountData.Password,
	}

	rules := map[string]protection.ValidationRule{
		"username": {MinLength: 1, MaxLength: 32, AllowSpaces: false, FieldName: "Логин"},
		"password": {MinLength: 1, MaxLength: 64, AllowSpaces: false, FieldName: "Пароль"},
	}

	sanitized, err := protection.ValidateFields(dataToValidate, rules)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Проверяет, нет ли активной сессии смены авторизации (защита от повторной смены до очистки предыдущей сессии)
	if hasActiveMQTTAuthSession() {
		http.Error(w, "Сначала очистите предыдущую сессию в окне «Статус»", http.StatusConflict)
		return
	}

	// Находит индексы аккаунтов с приоритетами 0 и 1
	idx0 := -1
	idx1 := -1

	for i := range configData.Hooks.Auth.Ledger.Auth {
		if configData.Hooks.Auth.Ledger.Auth[i].Account == 0 {
			idx0 = i
		}
		if configData.Hooks.Auth.Ledger.Auth[i].Account == 1 {
			idx1 = i
		}
	}

	// Возвращает ошибку, если не найдены оба обязательных аккаунта
	if idx0 == -1 || idx1 == -1 {
		http.Error(w, "Некорректная структура аккаунтов в конфигурации", http.StatusInternalServerError)
		return
	}

	// Проверяет наличие клиентов для определения необходимости резервного аккаунта
	hasClients := false
	_ = db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("client:")
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		it.Seek(opts.Prefix)
		if it.ValidForPrefix(opts.Prefix) {
			hasClients = true
		}
		return nil
	})

	// Обновляет данные аккаунта, который был с приоритетом 1 (станет основным с приоритетом 0)
	configData.Hooks.Auth.Ledger.Auth[idx1].Username = sanitized["username"]
	configData.Hooks.Auth.Ledger.Auth[idx1].Password = sanitized["password"]
	configData.Hooks.Auth.Ledger.Auth[idx1].Allow = true // Основной аккаунт всегда разрешён

	// Резервный аккаунт включается только при наличии клиентов, которым нужно время на смену пароля
	configData.Hooks.Auth.Ledger.Auth[idx0].Allow = hasClients

	// Меняет приоритеты местами
	configData.Hooks.Auth.Ledger.Auth[idx0].Account = 1
	configData.Hooks.Auth.Ledger.Auth[idx1].Account = 0

	// Записывает обновленную конфигурацию
	updatedConfigBytes, err := json.MarshalIndent(configData, "", "  ")
	if err != nil {
		http.Error(w, "Ошибка при сериализации конфигурации", http.StatusInternalServerError)
		return
	}

	if err := pathsOS.WriteFile(pathsOS.Path_Config_MQTT, updatedConfigBytes, pathsOS.FilePerm); err != nil {
		http.Error(w, "Ошибка при записи конфигурации", http.StatusInternalServerError)
		return
	}

	if err := RestartMqttHandler(); err != nil {
		http.Error(w, "Ошибка при перезапуске MQTT сервера", http.StatusInternalServerError)
		return
	}

	// Создаёт запись в БД и рассылает команды клиентам
	if err := initMQTTAuthChangeAndBroadcast(sanitized["username"], sanitized["password"]); err != nil {
		logging.LogError("MQTT Auth: Ошибка инициализации смены авторизации: %v", err)
		// Не прерываем запрос, конфиг уже обновлён
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Учетная запись MQTT успешно обновлена"))
}

// RestartMqttHandler корректно перезапускает MQTT-сервер и клиент, чтобы перечитать конфигурацию
func RestartMqttHandler() error {
	restartMu.Lock()
	defer restartMu.Unlock()

	// Останавливает клиент AutoPaho
	mqtt_client.StopMQTTClient()

	// Останавливает текущий сервер с защитой от паники (баг Mochi-MQTT: close of closed channel)
	if Server != nil {
		if err := safeCloseServer(); err != nil {
			logging.LogError("MQTT Serv: Ошибка при перезапуске MQTT (остановка): %v", err)
			return err
		}
	}

	// Перечитывает конфигурацию и запускает MQTT-сервер заново
	Mqtt_serv()

	// Запускает клиент AutoPaho заново в отдельной горутине
	go mqtt_client.StartMQTTClient()

	return nil
}

// safeCloseServer останавливает MQTT-сервер с защитой от паники при повторном закрытии
func safeCloseServer() (err error) {
	defer func() {
		if r := recover(); r != nil {
			logging.LogError("MQTT Serv: Восстановление после паники при закрытии сервера: %v", r)
			err = fmt.Errorf("паника при закрытии: %v", r)
		}
	}()
	return Server.Close()
}

// UpdateAllowHandler обновляет значение allow для учетной записи с приоритетом 1
func UpdateAllowHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Проверка прав на системные настройки
	if GetAuthInfo != nil && CheckPermSystemSettings != nil {
		login, _, err := GetAuthInfo(r)
		if err == nil && login != "" {
			if !CheckPermSystemSettings(login) {
				http.Error(w, `{"success": false, "message": "У вас нет прав на изменение MQTT авторизации"}`, http.StatusForbidden)
				return
			}
		}
	}

	configBytes, err := os.ReadFile(pathsOS.Path_Config_MQTT)
	if err != nil {
		http.Error(w, `{"success": false, "message": "Ошибка при чтении конфигурации"}`, http.StatusInternalServerError)
		return
	}

	var configData MQTTConfig
	if err := json.Unmarshal(configBytes, &configData); err != nil {
		http.Error(w, `{"success": false, "message": "Ошибка при парсинге конфигурации"}`, http.StatusInternalServerError)
		return
	}

	var requestData struct {
		Allow bool `json:"allow"`
	}
	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		http.Error(w, `{"success": false, "message": "Ошибка при парсинге данных запроса"}`, http.StatusBadRequest)
		return
	}

	found := false
	for i := range configData.Hooks.Auth.Ledger.Auth {
		if configData.Hooks.Auth.Ledger.Auth[i].Account == 1 {
			configData.Hooks.Auth.Ledger.Auth[i].Allow = requestData.Allow
			found = true
			break
		}
	}

	if !found {
		http.Error(w, `{"success": false, "message": "Учетная запись с приоритетом 1 не найдена"}`, http.StatusNotFound)
		return
	}

	updatedConfigBytes, err := json.MarshalIndent(configData, "", " ")
	if err != nil {
		http.Error(w, `{"success": false, "message": "Ошибка при сериализации конфигурации"}`, http.StatusInternalServerError)
		return
	}

	if err := pathsOS.WriteFile(pathsOS.Path_Config_MQTT, updatedConfigBytes, pathsOS.FilePerm); err != nil {
		http.Error(w, `{"success": false, "message": "Ошибка при записи конфигурации"}`, http.StatusInternalServerError)
		return
	}

	if err := RestartMqttHandler(); err != nil {
		http.Error(w, `{"success": false, "message": "Ошибка при перезапуске MQTT сервера"}`, http.StatusInternalServerError)
		return
	}

	response := map[string]any{
		"success": true,
		"message": "Операция выполнена успешно",
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// initMQTTAuthChangeAndBroadcast создаёт запись в БД со всеми клиентами и рассылает им команды
func initMQTTAuthChangeAndBroadcast(username, password string) error {
	now := time.Now()
	createdAt := now.Format("02.01.06(15:04:05)")

	// Собирает список всех клиентов из БД
	clients := make(map[string]MQTTAuthClientStatus)

	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("client:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(opts.Prefix); it.ValidForPrefix(opts.Prefix); it.Next() {
			item := it.Item()
			clientID := strings.TrimPrefix(string(item.Key()), "client:")

			var clientData map[string]string
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &clientData)
			}); err != nil {
				continue
			}

			clientName := clientData["name"]
			if clientName == "" {
				clientName = clientID // Использует ID, если имя не задано
			}

			clients[clientID] = MQTTAuthClientStatus{
				ClientName: clientName,
				Status:     "pending",
				Message:    "",
				UpdatedAt:  createdAt,
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ошибка чтения клиентов из БД: %w", err)
	}

	// Если клиентов нет, не создаём запись
	if len(clients) == 0 {
		logging.LogSystem("MQTT Auth: Нет клиентов для рассылки новых учётных данных")
		return nil
	}

	// Создаёт запись о смене авторизации
	authChange := MQTTAuthChange{
		CreatedAt:   createdAt,
		NewUsername: username,
		NewPassword: password,
		Clients:     clients,
	}

	// Сохраняет в БД
	authChangeBytes, err := json.Marshal(authChange)
	if err != nil {
		return fmt.Errorf("ошибка сериализации записи авторизации: %w", err)
	}

	err = db.DBInstance.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(mqttAuthActiveKey), authChangeBytes)
	})
	if err != nil {
		return fmt.Errorf("ошибка сохранения записи в БД: %w", err)
	}

	logging.LogSystem("MQTT Auth: Создана запись смены авторизации для %d клиентов", len(clients))

	// Очищает трекер от возможных остатков предыдущей сессии
	mqttAuthSendTracker.Range(func(key, _ any) bool {
		mqttAuthSendTracker.Delete(key)
		return true
	})

	// Запускает отложенную рассылку всем клиентам через 5 секунд (даёт время на переподключение после перезапуска MQTT сервера)
	go func() {
		time.Sleep(5 * time.Second)
		broadcastMQTTAuthCommand(username, password)
	}()

	return nil
}

// broadcastMQTTAuthCommand отправляет MQTT команды онлайн-клиентам в статусе pending
func broadcastMQTTAuthCommand(username, password string) {
	// Перечитывает актуальные статусы из БД (клиент мог уже ответить)
	var authChange MQTTAuthChange
	var found bool

	err := db.DBInstance.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(mqttAuthActiveKey))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &authChange)
		})
	})

	if err != nil || !found {
		return
	}

	// Формирует JSON команду смены MQTT авторизации (топик уже определяет тип команды)
	command := map[string]string{
		"Username_MQTT": username,
		"Password_MQTT": password,
	}

	commandBytes, err := json.Marshal(command)
	if err != nil {
		logging.LogError("MQTT Auth: Ошибка сериализации команды: %v", err)
		return
	}

	sentCount := 0
	for clientID, client := range authChange.Clients {
		// Отправляет только клиентам, которые ещё не подтвердили смену
		if client.Status != "pending" {
			continue
		}

		// Пропускает, если уже отправлено в текущей сессии
		if _, alreadySent := mqttAuthSendTracker.Load(clientID); alreadySent {
			continue
		}

		// Пропускает офлайн клиентов (отправка произойдёт при подключении через CheckAndResendMQTTAuth)
		if _, ok := Server.Clients.Get(clientID); !ok {
			continue
		}

		// Атомарно помечает клиента для отправки (защита от дублей)
		if _, alreadySent := mqttAuthSendTracker.LoadOrStore(clientID, struct{}{}); alreadySent {
			continue
		}

		topic := fmt.Sprintf("Server/%s/UpdateMQTTAuth", clientID)
		if err := mqtt_client.Publish(topic, commandBytes, 2); err != nil {
			logging.LogError("MQTT Auth: Ошибка отправки команды клиенту %s: %v", clientID, err)
			mqttAuthSendTracker.Delete(clientID) // Сбрасывает при ошибке для возможности повторной попытки
		} else {
			sentCount++
		}
	}

	if sentCount > 0 {
		logging.LogSystem("MQTT Auth: Отправлены команды смены авторизации %d клиентам", sentCount)
	}
}

// CheckAndResendMQTTAuth проверяет наличие pending записи для клиента и отправляет ему команду с задержкой
func CheckAndResendMQTTAuth(clientID string) {
	// Задержка 5 секунд для стабильного подключения и подписки клиента
	time.Sleep(5 * time.Second)

	// Проверяет, что сообщение ещё не было отправлено этому клиенту в текущей сессии
	if _, alreadySent := mqttAuthSendTracker.Load(clientID); alreadySent {
		return
	}

	// Проверяет, что клиент реально подключён к MQTT серверу
	if _, ok := Server.Clients.Get(clientID); !ok {
		return // Клиент не подключён, отправка произойдёт при следующем подключении
	}

	// Читает текущую запись из БД
	var authChange MQTTAuthChange
	var found bool

	err := db.DBInstance.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(mqttAuthActiveKey))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil
			}
			return err
		}

		found = true
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &authChange)
		})
	})

	if err != nil || !found {
		return
	}

	clientStatus, exists := authChange.Clients[clientID]
	if !exists || clientStatus.Status != "pending" {
		return // Отправляет только клиентам в статусе "pending"
	}

	// Атомарно помечает клиента для отправки (защита от дублей при параллельных вызовах)
	if _, alreadySent := mqttAuthSendTracker.LoadOrStore(clientID, struct{}{}); alreadySent {
		return
	}

	// Формирует JSON команду смены MQTT авторизации (топик уже определяет тип команды)
	command := map[string]string{
		"Username_MQTT": authChange.NewUsername,
		"Password_MQTT": authChange.NewPassword,
	}

	commandBytes, err := json.Marshal(command)
	if err != nil {
		logging.LogError("MQTT Auth: Ошибка сериализации команды для %s: %v", clientID, err)
		mqttAuthSendTracker.Delete(clientID) // Сбрасывает при ошибке для возможности повторной попытки
		return
	}

	topic := fmt.Sprintf("Server/%s/UpdateMQTTAuth", clientID)
	if err := mqtt_client.Publish(topic, commandBytes, 2); err != nil {
		logging.LogError("MQTT Auth: Ошибка отправки команды клиенту %s: %v", clientID, err)
		mqttAuthSendTracker.Delete(clientID) // Сбрасывает при ошибке для возможности повторной попытки
	} else {
		logging.LogSystem("MQTT Auth: Команда смены авторизации отправлена клиенту %s", clientID)
	}
}

// HandleMQTTAuthAnswer обрабатывает ответ клиента о результате смены авторизации
func HandleMQTTAuthAnswer(clientID string, payload []byte) {
	var response struct {
		Answer      string `json:"Answer"`      // success или error
		Description string `json:"Description"` // Описание ошибки (только при error)
	}

	if err := json.Unmarshal(payload, &response); err != nil {
		logging.LogError("MQTT Auth: Ошибка парсинга ответа от %s: %v", clientID, err)
		return
	}

	now := time.Now()
	updatedAt := now.Format("02.01.06(15:04:05)")

	// Обновляет статус клиента в БД
	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(mqttAuthActiveKey))
		if err != nil {
			return fmt.Errorf("запись смены авторизации не найдена: %w", err)
		}

		var authChange MQTTAuthChange
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &authChange)
		}); err != nil {
			return err
		}

		clientStatus, exists := authChange.Clients[clientID]
		if !exists {
			// Клиент не в списке текущей сессии смены — игнорирует
			return nil
		}

		// Обновляет статус
		clientStatus.Status = response.Answer
		clientStatus.Message = response.Description
		clientStatus.UpdatedAt = updatedAt
		authChange.Clients[clientID] = clientStatus

		// Сохраняет обновлённую запись
		authChangeBytes, err := json.Marshal(authChange)
		if err != nil {
			return err
		}
		return txn.Set([]byte(mqttAuthActiveKey), authChangeBytes)
	})

	if err != nil {
		logging.LogError("MQTT Auth: Ошибка обновления статуса клиента %s: %v", clientID, err)
		return
	}

	if response.Answer == "success" {
		logging.LogSystem("MQTT Auth: Клиент %s успешно обновил авторизацию", clientID)
	} else {
		logging.LogError("MQTT Auth: Клиент %s не смог обновить авторизацию: %s", clientID, response.Description)
	}

	// Проверяет, все ли клиенты успешно обновились (в горутине, чтобы не блокировать обработчик MQTT)
	go checkAndDisableReserveAccount()
}

// checkAndDisableReserveAccount проверяет, все ли клиенты успешно сменили пароль, и отключает резервный аккаунт
func checkAndDisableReserveAccount() {
	var allSuccess bool

	err := db.DBInstance.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(mqttAuthActiveKey))
		if err != nil {
			return err
		}

		var authChange MQTTAuthChange
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &authChange)
		}); err != nil {
			return err
		}

		// Проверяет статусы всех клиентов
		allSuccess = true
		for _, client := range authChange.Clients {
			if client.Status != "success" {
				allSuccess = false
				break
			}
		}
		return nil
	})

	if err != nil {
		// Запись не найдена или ошибка — ничего не делает
		return
	}

	if !allSuccess {
		return
	}

	// Все клиенты успешно обновились — отключает резервный аккаунт
	logging.LogSystem("MQTT Auth: Все клиенты успешно обновили авторизацию, отключаем резервный аккаунт")

	if err := disableReserveAccount(); err != nil {
		logging.LogError("MQTT Auth: Ошибка отключения резервного аккаунта: %v", err)
	}
}

// disableReserveAccount отключает резервный аккаунт (account == 1)
func disableReserveAccount() error {
	configBytes, err := os.ReadFile(pathsOS.Path_Config_MQTT)
	if err != nil {
		return fmt.Errorf("ошибка чтения конфигурации: %w", err)
	}

	var configData MQTTConfig
	if err := json.Unmarshal(configBytes, &configData); err != nil {
		return fmt.Errorf("ошибка парсинга конфигурации: %w", err)
	}

	// Находит и отключает аккаунт с приоритетом 1
	found := false
	for i := range configData.Hooks.Auth.Ledger.Auth {
		if configData.Hooks.Auth.Ledger.Auth[i].Account == 1 {
			if !configData.Hooks.Auth.Ledger.Auth[i].Allow {
				// Уже отключён
				return nil
			}
			configData.Hooks.Auth.Ledger.Auth[i].Allow = false
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("аккаунт с приоритетом 1 не найден")
	}

	// Записывает обновлённую конфигурацию
	updatedConfigBytes, err := json.MarshalIndent(configData, "", "  ")
	if err != nil {
		return fmt.Errorf("ошибка сериализации конфигурации: %w", err)
	}

	if err := pathsOS.WriteFile(pathsOS.Path_Config_MQTT, updatedConfigBytes, pathsOS.FilePerm); err != nil {
		return fmt.Errorf("ошибка записи конфигурации: %w", err)
	}

	// Перезапускает MQTT сервер для применения изменений
	if err := RestartMqttHandler(); err != nil {
		return fmt.Errorf("ошибка перезапуска MQTT сервера: %w", err)
	}

	logging.LogSystem("MQTT Auth: Резервный аккаунт успешно отключен")
	return nil
}

// GetMQTTAuthStatusHandler возвращает статус смены авторизации для всех клиентов (GET)
func GetMQTTAuthStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var authChange MQTTAuthChange
	var found bool

	err := db.DBInstance.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(mqttAuthActiveKey))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil // Запись не найдена
			}
			return err
		}

		found = true
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &authChange)
		})
	})

	if err != nil {
		http.Error(w, `{"success": false, "message": "Ошибка чтения БД"}`, http.StatusInternalServerError)
		return
	}

	if !found {
		// Нет активной сессии смены авторизации
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"active":  false,
			"message": "Нет активной сессии смены авторизации",
		})
		return
	}

	// Формирует ответ без пароля
	response := map[string]any{
		"success":      true,
		"active":       true,
		"created_at":   authChange.CreatedAt,
		"new_username": authChange.NewUsername,
		"clients":      authChange.Clients,
	}

	// Считает статистику
	var pending, success, errorCount int
	for _, client := range authChange.Clients {
		switch client.Status {
		case "pending":
			pending++
		case "success":
			success++
		case "error":
			errorCount++
		}
	}
	response["stats"] = map[string]int{
		"total":   len(authChange.Clients),
		"pending": pending,
		"success": success,
		"error":   errorCount,
	}

	json.NewEncoder(w).Encode(response)
}

// ResendMQTTAuthHandler повторно отправляет команды клиентам с ошибками (POST)
func ResendMQTTAuthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Проверка прав на системные настройки
	if GetAuthInfo != nil && CheckPermSystemSettings != nil {
		login, _, err := GetAuthInfo(r)
		if err == nil && login != "" {
			if !CheckPermSystemSettings(login) {
				http.Error(w, `{"success": false, "message": "У вас нет прав на изменение MQTT авторизации"}`, http.StatusForbidden)
				return
			}
		}
	}

	var authChange MQTTAuthChange
	var found bool

	// Читает текущую запись
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(mqttAuthActiveKey))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil
			}
			return err
		}

		found = true
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &authChange)
		})
	})

	if err != nil {
		http.Error(w, `{"success": false, "message": "Ошибка чтения БД"}`, http.StatusInternalServerError)
		return
	}

	if !found {
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"message": "Нет активной сессии смены авторизации",
		})
		return
	}

	// Собирает клиентов только с ошибками
	errorClients := make(map[string]MQTTAuthClientStatus)
	now := time.Now()
	updatedAt := now.Format("02.01.06(15:04:05)")

	for clientID, client := range authChange.Clients {
		if client.Status == "error" {
			// Сбрасывает статус на pending перед повторной отправкой
			client.Status = "pending"
			client.Message = ""
			client.UpdatedAt = updatedAt
			errorClients[clientID] = client
			authChange.Clients[clientID] = client
		}
	}

	if len(errorClients) == 0 {
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"message": "Нет клиентов с ошибками для повторной отправки",
			"resent":  0,
		})
		return
	}

	// Обновляет запись в БД (сбрасываем статусы на pending)
	authChangeBytes, err := json.Marshal(authChange)
	if err != nil {
		http.Error(w, `{"success": false, "message": "Ошибка сериализации"}`, http.StatusInternalServerError)
		return
	}

	err = db.DBInstance.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(mqttAuthActiveKey), authChangeBytes)
	})
	if err != nil {
		http.Error(w, `{"success": false, "message": "Ошибка обновления БД"}`, http.StatusInternalServerError)
		return
	}

	// Сбрасывает трекер и запускает отправку команд клиентам с ошибками (с задержкой 5 секунд)
	for clientID := range errorClients {
		mqttAuthSendTracker.Delete(clientID) // Разрешает повторную отправку
		go CheckAndResendMQTTAuth(clientID)
	}

	logging.LogSystem("MQTT Auth: Запущена повторная отправка для %d клиентов с ошибками", len(errorClients))

	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": fmt.Sprintf("Команды повторно отправлены %d клиентам", len(errorClients)),
		"resent":  len(errorClients),
	})
}

// hasActiveMQTTAuthSession проверяет, существует ли в БД активная сессия смены авторизации
func hasActiveMQTTAuthSession() bool {
	exists := false
	_ = db.DBInstance.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(mqttAuthActiveKey))
		if err == nil {
			exists = true
		}
		return nil
	})
	return exists
}

// ClearMQTTAuthSessionHandler очищает запись о смене авторизации из БД (POST)
func ClearMQTTAuthSessionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Проверка прав на изменение системных настроек
	if GetAuthInfo != nil && CheckPermSystemSettings != nil {
		login, _, err := GetAuthInfo(r)
		if err == nil && login != "" {
			if !CheckPermSystemSettings(login) {
				http.Error(w, `{"success": false, "message": "У вас нет прав на эту операцию"}`, http.StatusForbidden)
				return
			}
		}
	}

	// Флаг: была ли активная сессия смены авторизации
	hadActiveSession := false

	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(mqttAuthActiveKey))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil // Сессии нет — ничего не удаляет
			}
			return err
		}

		// Сессия была — удаляет её
		hadActiveSession = true
		return txn.Delete([]byte(mqttAuthActiveKey))
	})

	if err != nil {
		http.Error(w, `{"success": false, "message": "Ошибка удаления записи"}`, http.StatusInternalServerError)
		return
	}

	// Очищает трекер отправки при очистке сессии
	mqttAuthSendTracker.Range(func(key, _ any) bool {
		mqttAuthSendTracker.Delete(key)
		return true
	})

	// Если сессия реально была очищена — резервный аккаунт больше не нужен, отключает его (если резервный аккаунт был включён вручную без сессии — его не трогает)
	if hadActiveSession {
		go func() {
			if err := disableReserveAccount(); err != nil {
				logging.LogError("MQTT Auth: Ошибка отключения резервного аккаунта при очистке сессии: %v", err)
			}
		}()
	}

	logging.LogSystem("MQTT Auth: Сессия смены авторизации очищена")

	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "Сессия смены авторизации очищена",
	})
}

// RemoveClientsFromMQTTAuthSession удаляет указанных клиентов из активной сессии смены логина/пароля MQTT
func RemoveClientsFromMQTTAuthSession(clientIDs []string) {
	if len(clientIDs) == 0 {
		return
	}

	idsSet := make(map[string]struct{}, len(clientIDs))
	for _, id := range clientIDs {
		idsSet[id] = struct{}{}
	}

	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(mqttAuthActiveKey))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil // Нет активной сессии — ничего не делает
			}
			return err
		}

		var authChange MQTTAuthChange
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &authChange)
		}); err != nil {
			return err
		}

		// Удаляет клиентов из записи
		changed := false
		for id := range idsSet {
			if _, exists := authChange.Clients[id]; exists {
				delete(authChange.Clients, id)
				changed = true
			}
		}

		if !changed {
			return nil
		}

		// Если клиентов не осталось, удаляет всю запись
		if len(authChange.Clients) == 0 {
			return txn.Delete([]byte(mqttAuthActiveKey))
		}

		// Сохраняет обновлённую запись
		authChangeBytes, err := json.Marshal(authChange)
		if err != nil {
			return err
		}
		return txn.Set([]byte(mqttAuthActiveKey), authChangeBytes)
	})

	if err != nil {
		logging.LogError("MQTT Auth: Ошибка удаления клиентов из сессии смены авторизации: %v", err)
	}

	// Очищает трекер отправки для удалённых клиентов
	for _, id := range clientIDs {
		mqttAuthSendTracker.Delete(id)
	}

	// После удаления клиентов проверяет, нужно ли отключить резервный аккаунт
	go func() {
		// Если запись была удалена целиком (клиентов не осталось) или все оставшиеся успешны — отключает резервный аккаунт
		var needDisable bool

		err := db.DBInstance.View(func(txn *badger.Txn) error {
			item, err := txn.Get([]byte(mqttAuthActiveKey))
			if err != nil {
				if err == badger.ErrKeyNotFound {
					// Запись удалена целиком — резервный аккаунт больше не нужен
					needDisable = true
					return nil
				}
				return err
			}

			var authChange MQTTAuthChange
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &authChange)
			}); err != nil {
				return err
			}

			// Проверяет, все ли оставшиеся клиенты в статусе success
			needDisable = true
			for _, client := range authChange.Clients {
				if client.Status != "success" {
					needDisable = false
					break
				}
			}
			return nil
		})

		if err != nil {
			logging.LogError("MQTT Auth: Ошибка проверки статусов после удаления клиентов: %v", err)
			return
		}

		if needDisable {
			logging.LogSystem("MQTT Auth: После удаления клиентов все оставшиеся успешно обновили авторизацию, отключаем резервный аккаунт")
			if err := disableReserveAccount(); err != nil {
				logging.LogError("MQTT Auth: Ошибка отключения резервного аккаунта: %v", err)
			}
		}
	}()
}

// UpdateClientNameInMqttAuth обновляет имя клиента в активной сессии смены MQTT авторизации (вызывается при переименовании клиента)
func UpdateClientNameInMqttAuth(clientID, newName string) error {
	return db.DBInstance.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(mqttAuthActiveKey))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil // Нет активной сессии — обновлять нечего
			}
			return err
		}

		var authChange MQTTAuthChange
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &authChange)
		}); err != nil {
			return err
		}

		// Проверяет, есть ли клиент в текущей сессии
		clientStatus, exists := authChange.Clients[clientID]
		if !exists {
			return nil
		}

		// Если имя уже актуальное — пропускает
		if clientStatus.ClientName == newName {
			return nil
		}

		// Обновляет имя
		clientStatus.ClientName = newName
		authChange.Clients[clientID] = clientStatus

		// Сохраняет обновлённую запись
		authChangeBytes, err := json.Marshal(authChange)
		if err != nil {
			return err
		}
		return txn.Set([]byte(mqttAuthActiveKey), authChangeBytes)
	})
}
