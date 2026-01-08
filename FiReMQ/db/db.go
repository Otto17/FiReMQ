// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package db

import (
	"encoding/json"
	"fmt"
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

// CreateDefaultUser создаёт пользователя по умолчанию с логином "FiReMQ"
func createDefaultUser() {
	now := time.Now()
	defaultDate := fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)", now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())
	defaultUser := struct {
		Auth_Name         string `json:"auth_name"`
		Auth_Login        string `json:"auth_login"`
		Auth_PasswordHash string `json:"auth_password_hash"`
		Auth_Date_Create  string `json:"date_create"`
		Auth_Date_Change  string `json:"date_change"`
	}{
		Auth_Name:         "Учётка по умолчанию (УДАЛИТЕ ЕЁ!)",
		Auth_Login:        "FiReMQ",
		Auth_PasswordHash: protection.HashPassword("FiReMQ"), // Хеширует пароль по умолчанию
		Auth_Date_Create:  defaultDate,
		Auth_Date_Change:  "--.--.--(--:--)",
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

// Close корректно закрывает базу данных
func Close() error {
	if DBInstance != nil {
		return DBInstance.Close()
	}
	return nil
}
