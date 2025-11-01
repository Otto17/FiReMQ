// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"FiReMQ/db"          // Локальный пакет с БД BadgerDB
	"FiReMQ/mqtt_client" // Локальный пакет MQTT клиента AutoPaho
	"FiReMQ/pathsOS"     // Локальный пакет с путями для разных платформ

	"github.com/dgraph-io/badger/v4"
)

// deletePrefix Префикс в БД, под которым храним записи с ID для самоудаления
const deletePrefix = "Delete_FiReAgent:"

// pendingDeleteValue Структура для хранения времени постановки в очередь удаления
type pendingDeleteValue struct {
	QueuedAt string `json:"queued_at"`
}

// pendingUninstallWorkers Дедупликация: чтобы не запускать несколько обработчиков для одного клиента одновременно
var pendingUninstallWorkers sync.Map // key: clientID -> struct{}

// uninstallMsg Формат сообщения для команды самоудаления
type uninstallMsg struct {
	Uninstall string `json:"Uninstall"`
}

// publishUninstallCommand Отправляет MQTT-команду на само удаление конкретному клиенту
func publishUninstallCommand(clientID string) error {
	topic := fmt.Sprintf("Client/%s/Uninstaller", clientID)
	payloadBytes, err := json.Marshal(uninstallMsg{Uninstall: clientID})
	if err != nil {
		return fmt.Errorf("marshal uninstall payload: %w", err)
	}

	// QoS = 2 (Exactly once)
	if err := mqtt_client.Publish(topic, payloadBytes, 2); err != nil {
		return fmt.Errorf("ошибка публикации MQTT для клиента %s: %w", clientID, err)
	}
	return nil
}

// addPendingUninstallBatch Пакетно добавляет оффлайн-ID в очередь удаления в БД
func addPendingUninstallBatch(ids []string) error {
	wb := db.DBInstance.NewWriteBatch() // Используем батчу
	defer wb.Cancel()

	now := time.Now().Format(time.RFC3339Nano)
	for _, id := range ids {
		if id == "" {
			continue
		}
		data, _ := json.Marshal(pendingDeleteValue{QueuedAt: now})
		if err := wb.Set([]byte(deletePrefix+id), data); err != nil {
			return err
		}
	}
	return wb.Flush()
}

// removePendingUninstall Удаляет ключ ожидания удаления для ID из БД
func removePendingUninstall(id string) error {
	return db.DBInstance.Update(func(txn *badger.Txn) error {
		if err := txn.Delete([]byte(deletePrefix + id)); err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		return nil
	})
}

// isPendingUninstall Проверяет наличие ID в очереди удаления
func isPendingUninstall(id string) (bool, error) {
	exists := false
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(deletePrefix + id))
		if err == badger.ErrKeyNotFound {
			exists = false
			return nil
		}
		if err != nil {
			return err
		}
		exists = true
		return nil
	})
	return exists, err
}

// listPendingUninstallIDs Возвращает список всех ID в очереди удаления
func listPendingUninstallIDs() ([]string, error) {
	ids := make([]string, 0, 32)
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(deletePrefix)
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			key := it.Item().KeyCopy(nil)
			ids = append(ids, string(key[len(deletePrefix):]))
		}
		return nil
	})
	return ids, err
}

// schedulePendingUninstall Планирует удаление для клиента при онлайне (с задержкой и проверками)
func schedulePendingUninstall(clientID string) {
	// Сначала проверим, есть ли вообще запись ожидания
	exists, err := isPendingUninstall(clientID)
	if err != nil || !exists {
		return
	}

	// Дедупликация параллельных запусков
	if _, loaded := pendingUninstallWorkers.LoadOrStore(clientID, struct{}{}); loaded {
		return // Другой воркер уже запущен для этого клиента, выходим.
	}

	go func() {
		defer pendingUninstallWorkers.Delete(clientID)

		// Ждем 3 секунды, чтобы клиент успел корректно запуститься
		time.Sleep(3 * time.Second)

		// Повторная проверка: ожидание ещё актуально?
		exists, err := isPendingUninstall(clientID)
		if err != nil || !exists {
			return
		}

		// Клиент всё ещё онлайн?
		online, _ := isClientOnline(clientID)
		if !online {
			return
		}

		// Вызываем функцию, которая выполняет все шаги по удалению
		if err := fullyDeleteClient(clientID); err != nil {
			log.Printf("Delete_FiReAgent: ошибка при выполнении полного удаления для %s: %v", clientID, err)
			return
		}

		// Если ошибок не было, логируем успешное завершение
		log.Printf("Delete_FiReAgent: клиент %s удалён (после входа онлайн) и исключён из очереди", clientID)
	}()
}

// fullyDeleteClient Инкапсулирует всю логику полного удаления клиента и связанных с ним данных
func fullyDeleteClient(clientID string) error {
	// Отправляем команду на самоудаление
	if err := publishUninstallCommand(clientID); err != nil {
		// Возвращаем ошибку, чтобы вызывающий код мог решить, что делать дальше
		return fmt.Errorf("ошибка публикации MQTT для %s: %w", clientID, err)
	}

	// Удаляем клиента из основной БД
	if err := DeleteClient(clientID); err != nil {
		// Аналогично, возвращаем ошибку
		return fmt.Errorf("ошибка удаления клиента %s из БД: %w", clientID, err)
	}

	// Удаляем файлы отчётов (ошибки здесь только логируем, они не критичны)
	filePathLite := filepath.Join(pathsOS.Path_Info, "Lite_"+clientID+".html.xz")
	if err := os.Remove(filePathLite); err != nil && !os.IsNotExist(err) {
		log.Printf("Ошибка удаления файла отчета %s: %v", filePathLite, err)
	}
	filePathAida := filepath.Join(pathsOS.Path_Info, "Aida_"+clientID+".html.xz")
	if err := os.Remove(filePathAida); err != nil && !os.IsNotExist(err) {
		log.Printf("Ошибка удаления файла отчета %s: %v", filePathAida, err)
	}

	// Очистка отчётов и runtime-состояния
	if err := removeClientIDsFromCommandRecords([]string{clientID}); err != nil {
		log.Printf("Ошибка очистки из CMD отчетов для %s: %v", clientID, err)
	}
	if err := removeClientIDsFromQUICRecords([]string{clientID}); err != nil {
		log.Printf("Ошибка очистки из QUIC отчетов для %s: %v", clientID, err)
	}
	cleanupClientsRuntimeState([]string{clientID})

	// На случай, если этот клиент раньше был в очереди — удалим ключ ожидания
	_ = removePendingUninstall(clientID)

	log.Printf("Процесс полного удаления для клиента %s инициирован.", clientID)
	return nil
}
