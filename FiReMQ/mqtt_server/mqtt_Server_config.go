// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package mqtt_server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"FiReMQ/mqtt_client" // Локальный пакет MQTT клиента AutoPaho
	"FiReMQ/pathsOS"     // Локальный пакет с путями для разных платформ
	"FiReMQ/protection"  // Локальный пакет с функциями базовой защиты
)

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

		fmt.Println("Конфигурационный файл mqtt_config.json создан")
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

	// 1. Обновляет данные аккаунта, который был с приоритетом 1
	configData.Hooks.Auth.Ledger.Auth[idx1].Username = sanitized["username"]
	configData.Hooks.Auth.Ledger.Auth[idx1].Password = sanitized["password"]
	configData.Hooks.Auth.Ledger.Auth[idx1].Allow = true

	// 2. Меняет приоритеты местами
	configData.Hooks.Auth.Ledger.Auth[idx0].Account = 1
	configData.Hooks.Auth.Ledger.Auth[idx1].Account = 0

	// Записывает обновленную конфигурацию
	updatedConfigBytes, err := json.MarshalIndent(configData, "", " ")
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

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Учетная запись MQTT успешно обновлена"))
}

// RestartMqttHandler корректно перезапускает MQTT-сервер и клиент, чтобы перечитать конфигурацию
func RestartMqttHandler() error {
	// Останавливает клиент AutoPaho
	mqtt_client.StopMQTTClient()

	// Останавливает текущий сервер, если он запущен
	if Server != nil {
		if err := Server.Close(); err != nil {
			//log.Printf("Ошибка при остановке MQTT сервера: %v", err)
			return err // Возвращает ошибку, если остановка не удалась
		}
	}

	// Перечитывает конфигурацию и запускает MQTT-сервер заново
	Mqtt_serv()

	// Запускает клиент AutoPaho заново в отдельной горутине
	go mqtt_client.StartMQTTClient()

	return nil // Возвращает nil, если всё прошло успешно
}

// UpdateAllowHandler обновляет значение allow для учетной записи с приоритетом 1
func UpdateAllowHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

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
