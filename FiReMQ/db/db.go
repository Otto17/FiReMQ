// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package db

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"FiReMQ/logging"    // Локальный пакет с логированием в HTML файл
	"FiReMQ/pathsOS"    // Локальный пакет с путями для разных платформ
	"FiReMQ/protection" // Локальный пакет с функциями базовой защиты

	"github.com/dgraph-io/badger/v4"
)

// DBInstance предоставляет глобальный доступ к базе данных BadgerDB
var DBInstance *badger.DB

// InitDB инициализирует базу данных BadgerDB
func InitDB() error {
	// Создаёт директорию для хранения данных БД
	if err := pathsOS.EnsureDir(pathsOS.Path_DB); err != nil {
		return fmt.Errorf("не удалось создать директорию БД '%s': %w", pathsOS.Path_DB, err)
	}

	// Настройка параметров BadgerDB для оптимизации работы
	opts := badger.DefaultOptions(pathsOS.Path_DB).
		WithLoggingLevel(badger.WARNING). // Уровень логирования
		WithValueLogFileSize(64 << 20).   // Устанавливает размер value log файла в 64МБ
		WithMemTableSize(1 << 30).        // Устанавливает размер memtable в 1ГБ
		WithNumGoroutines(4)              // Использует 4 фоновых потока

	db, err := badger.Open(opts)
	if err != nil {
		return err
	}
	DBInstance = db

	// Создаёт пользователя по умолчанию, если база данных пуста
	if isUsersEmpty() {
		createDefaultUser()
		logging.LogSystem("БД: Создан первый пользователь 'FiReMQ' по умолчанию")
	} else if !hasAnyFullPermissionAdmin() {
		// Проверяет наличие учётки с полными правами (для совместимости со старым форматом БД)
		createTransitionalAdmin()
	}

	return nil
}

// IsUsersEmpty проверяет, содержит ли БД записи пользователей
func isUsersEmpty() bool {
	empty := true
	err := DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("auth:") // Ищет ключи с префиксом "auth:"
		it := txn.NewIterator(opts)
		defer it.Close()

		it.Seek(opts.Prefix)
		if it.ValidForPrefix(opts.Prefix) {
			empty = false // Найдена хотя бы одна запись пользователя
		}
		return nil
	})

	if err != nil {
		logging.LogError("БД: Ошибка проверки пользователей в БД: %v", err)
	}
	return empty
}

// CreateDefaultUser создаёт пользователя по умолчанию с логином "FiReMQ" и полными правами
func createDefaultUser() {
	now := time.Now()
	defaultDate := fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)", now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())
	defaultUser := struct {
		Auth_Name                   string   `json:"auth_name"`
		Auth_Login                  string   `json:"auth_login"`
		Auth_PasswordHash           string   `json:"auth_password_hash"`
		Auth_Date_Create            string   `json:"date_create"`
		Auth_Date_Change            string   `json:"date_change"`
		Perm_Create                 bool     `json:"perm_create"`                   // Права на создание новых учётных записей
		Perm_Update                 bool     `json:"perm_update"`                   // Права на изменение действующих учётных записей
		Perm_Delete                 bool     `json:"perm_delete"`                   // Права на удаление действующих учётных записей
		Perm_RenameClients          bool     `json:"perm_rename_clients"`           // Права на переименовывание клиентов
		Perm_RenameClientsGroups    []string `json:"perm_rename_clients_groups"`    // Список групп для переименования (пустой = все группы)
		Perm_DeleteClients          bool     `json:"perm_delete_clients"`           // Права на удаление клиентов
		Perm_DeleteClientsGroups    []string `json:"perm_delete_clients_groups"`    // Список групп для удаления (пустой = все группы)
		Perm_MoveClients            bool     `json:"perm_move_clients"`             // Права на перемещение клиентов
		Perm_MoveClientsGroups      []string `json:"perm_move_clients_groups"`      // Список разрешённых групп (пустой = все группы)
		Perm_UninstallAgents        bool     `json:"perm_uninstall_agents"`         // Права на полное удаление FiReAgent
		Perm_TerminalCommands       bool     `json:"perm_terminal_commands"`        // Права на отправку cmd/PowerShell команд
		Perm_TerminalCommandsGroups []string `json:"perm_terminal_commands_groups"` // Список групп для cmd/PowerShell (пустой = все группы)
		Perm_InstallPrograms        bool     `json:"perm_install_programs"`         // Права на установку ПО через QUIC
		Perm_InstallProgramsGroups  []string `json:"perm_install_programs_groups"`  // Список групп для установки ПО (пустой = все группы)
		Perm_SystemSettings         bool     `json:"perm_system_settings"`          // Права на системные настройки (обновление/откат OWASP CRS и FiReMQ, MQTT авторизация)
	}{
		Auth_Name:                   "Учётка по умолчанию (УДАЛИТЕ ЕЁ!)",
		Auth_Login:                  "FiReMQ",
		Auth_PasswordHash:           protection.HashPassword("FiReMQ"), // Хеширует пароль по умолчанию
		Auth_Date_Create:            defaultDate,
		Auth_Date_Change:            "--.--.--(--:--)",
		Perm_Create:                 true, // Полные права для первой учётной записи
		Perm_Update:                 true,
		Perm_Delete:                 true,
		Perm_RenameClients:          true,
		Perm_RenameClientsGroups:    []string{}, // Пустой список = все группы разрешены
		Perm_DeleteClients:          true,
		Perm_DeleteClientsGroups:    []string{},
		Perm_MoveClients:            true,
		Perm_MoveClientsGroups:      []string{},
		Perm_UninstallAgents:        true,
		Perm_TerminalCommands:       true,
		Perm_TerminalCommandsGroups: []string{},
		Perm_InstallPrograms:        true,
		Perm_InstallProgramsGroups:  []string{},
		Perm_SystemSettings:         true,
	}

	userData, err := json.Marshal(defaultUser)
	if err != nil {
		logging.LogError("БД: Ошибка маршалинга пользователя: %v", err)
		return
	}

	err = DBInstance.Update(func(txn *badger.Txn) error {
		key := []byte("auth:" + defaultUser.Auth_Login)
		return txn.Set(key, userData) // Сохраняет запись в БД
	})

	if err != nil {
		logging.LogError("БД: Ошибка сохранения пользователя: %v", err)
	}
}

// hasAnyFullPermissionAdmin проверяет, есть ли хотя бы одна учётка с полными правами
func hasAnyFullPermissionAdmin() bool {
	found := false
	err := DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("auth:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(opts.Prefix); it.ValidForPrefix(opts.Prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				// Используем анонимную структуру для чтения только полей разрешений
				var user struct {
					Perm_Create                 bool     `json:"perm_create"`
					Perm_Update                 bool     `json:"perm_update"`
					Perm_Delete                 bool     `json:"perm_delete"`
					Perm_RenameClients          bool     `json:"perm_rename_clients"`
					Perm_RenameClientsGroups    []string `json:"perm_rename_clients_groups"`
					Perm_DeleteClients          bool     `json:"perm_delete_clients"`
					Perm_DeleteClientsGroups    []string `json:"perm_delete_clients_groups"`
					Perm_MoveClients            bool     `json:"perm_move_clients"`
					Perm_MoveClientsGroups      []string `json:"perm_move_clients_groups"`
					Perm_UninstallAgents        bool     `json:"perm_uninstall_agents"`
					Perm_TerminalCommands       bool     `json:"perm_terminal_commands"`
					Perm_TerminalCommandsGroups []string `json:"perm_terminal_commands_groups"`
					Perm_InstallPrograms        bool     `json:"perm_install_programs"`
					Perm_InstallProgramsGroups  []string `json:"perm_install_programs_groups"`
					Perm_SystemSettings         bool     `json:"perm_system_settings"`
				}
				if err := json.Unmarshal(val, &user); err != nil {
					return err
				}

				// Полные права на переименование = флаг true И пустой список групп (все группы разрешены)
				hasRenameFullPerm := user.Perm_RenameClients && len(user.Perm_RenameClientsGroups) == 0

				// Полные права на перемещение = флаг true И пустой список групп (все группы разрешены)
				hasMoveFullPerm := user.Perm_MoveClients && len(user.Perm_MoveClientsGroups) == 0

				// Полные права на удаление клиентов = флаг true И пустой список групп
				hasDeleteFullPerm := user.Perm_DeleteClients && len(user.Perm_DeleteClientsGroups) == 0

				// Полные права на cmd/PowerShell = флаг true И пустой список групп
				hasTerminalFullPerm := user.Perm_TerminalCommands && len(user.Perm_TerminalCommandsGroups) == 0

				// Полные права на установку ПО = флаг true И пустой список групп
				hasInstallFullPerm := user.Perm_InstallPrograms && len(user.Perm_InstallProgramsGroups) == 0

				// Проверяет наличие всех разрешений
				if user.Perm_Create && user.Perm_Update && user.Perm_Delete && hasRenameFullPerm && hasDeleteFullPerm && hasMoveFullPerm && user.Perm_UninstallAgents && hasTerminalFullPerm && hasInstallFullPerm && user.Perm_SystemSettings {
					found = true
				}
				return nil
			})
			if err != nil {
				return err
			}
			// Прекращает поиск, если нашли учётку с полными правами
			if found {
				break
			}
		}
		return nil
	})

	if err != nil {
		logging.LogError("БД: Ошибка проверки прав администраторов: %v", err)
	}
	return found
}

// generateSafeRandomString генерирует рандомную строку из разрешённых символов
func generateSafeRandomString(length int) string {
	// Используем только безопасные ASCII символы, соответствующие patternAlphaNumRuEn
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	result := make([]byte, length)
	for i := range result {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			// Fallback при ошибке генерации
			result[i] = charset[i%len(charset)]
			continue
		}
		result[i] = charset[num.Int64()]
	}
	return string(result)
}

// createTransitionalAdmin создаёт переходную учётку с рандомным логином и паролем
func createTransitionalAdmin() {
	// Генерирует рандомный логин и пароль
	login := "Admin_" + generateSafeRandomString(8)
	password := generateSafeRandomString(16)

	now := time.Now()
	dateCreate := fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)", now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())

	transitionalUser := struct {
		Auth_Name                   string   `json:"auth_name"`
		Auth_Login                  string   `json:"auth_login"`
		Auth_PasswordHash           string   `json:"auth_password_hash"`
		Auth_Date_Create            string   `json:"date_create"`
		Auth_Date_Change            string   `json:"date_change"`
		Perm_Create                 bool     `json:"perm_create"`                   // Права на создание новых учётных записей
		Perm_Update                 bool     `json:"perm_update"`                   // Права на изменение действующих учётных записей
		Perm_Delete                 bool     `json:"perm_delete"`                   // Права на удаление действующих учётных записей
		Perm_RenameClients          bool     `json:"perm_rename_clients"`           // Права на переименовывание клиентов
		Perm_RenameClientsGroups    []string `json:"perm_rename_clients_groups"`    // Список групп для переименования (пустой = все группы)
		Perm_DeleteClients          bool     `json:"perm_delete_clients"`           // Права на удаление клиентов
		Perm_DeleteClientsGroups    []string `json:"perm_delete_clients_groups"`    // Список групп для удаления (пустой = все группы)
		Perm_MoveClients            bool     `json:"perm_move_clients"`             // Права на перемещение клиентов
		Perm_MoveClientsGroups      []string `json:"perm_move_clients_groups"`      // Список разрешённых групп (пустой = все группы)
		Perm_UninstallAgents        bool     `json:"perm_uninstall_agents"`         // Права на полное удаление FiReAgent
		Perm_TerminalCommands       bool     `json:"perm_terminal_commands"`        // Права на отправку cmd/PowerShell команд
		Perm_TerminalCommandsGroups []string `json:"perm_terminal_commands_groups"` // Список групп для cmd/PowerShell (пустой = все группы)
		Perm_InstallPrograms        bool     `json:"perm_install_programs"`         // Права на установку ПО через QUIC
		Perm_InstallProgramsGroups  []string `json:"perm_install_programs_groups"`  // Список групп для установки ПО (пустой = все группы)
		Perm_SystemSettings         bool     `json:"perm_system_settings"`          // Права на системные настройки (обновление/откат OWASP CRS и FiReMQ, MQTT авторизация)
	}{
		Auth_Name:                   fmt.Sprintf("ПЕРЕХОДНАЯ УЧЁТКА! Пароль: %s", password),
		Auth_Login:                  login,
		Auth_PasswordHash:           protection.HashPassword(password),
		Auth_Date_Create:            dateCreate,
		Auth_Date_Change:            "--.--.--(--:--)",
		Perm_Create:                 true, // Полные права для переходной учётной записи
		Perm_Update:                 true,
		Perm_Delete:                 true,
		Perm_RenameClients:          true,
		Perm_RenameClientsGroups:    []string{}, // Пустой список = все группы разрешены
		Perm_DeleteClients:          true,
		Perm_DeleteClientsGroups:    []string{},
		Perm_MoveClients:            true,
		Perm_MoveClientsGroups:      []string{},
		Perm_UninstallAgents:        true,
		Perm_TerminalCommands:       true,
		Perm_TerminalCommandsGroups: []string{},
		Perm_InstallPrograms:        true,
		Perm_InstallProgramsGroups:  []string{},
		Perm_SystemSettings:         true,
	}

	userData, err := json.Marshal(transitionalUser)
	if err != nil {
		logging.LogError("БД: Ошибка маршалинга переходного пользователя: %v", err)
		return
	}

	err = DBInstance.Update(func(txn *badger.Txn) error {
		key := []byte("auth:" + transitionalUser.Auth_Login)
		return txn.Set(key, userData)
	})

	if err != nil {
		logging.LogError("БД: Ошибка сохранения переходного пользователя: %v", err)
		return
	}

	logging.LogSystem("БД: В БД не найдено учётных записей с полными правами (старый формат)")
	logging.LogSystem("БД: Создана переходная учётка '%s' с полными правами (пароль указан в имени учётки)", login)
}

// Close корректно закрывает базу данных
func Close() error {
	if DBInstance != nil {
		return DBInstance.Close()
	}
	return nil
}
