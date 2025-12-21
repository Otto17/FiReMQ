// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"FiReMQ/db"          // Локальный пакет с БД BadgerDB
	"FiReMQ/logging"     // Локальный пакет с логированием в HTML файл
	"FiReMQ/mqtt_client" // Локальный пакет MQTT клиента AutoPaho
	"FiReMQ/mqtt_server" // Локальный пакет MQTT клиента Mocho-MQTT
	"FiReMQ/new_cert"    // Локальный пакет для проверки и создания mTLS сертификатов
	"FiReMQ/pathsOS"     // Локальный пакет с путями для разных платформ
	"FiReMQ/protection"  // Локальный пакет с функциями базовой защиты
	"FiReMQ/update"      // Локальный пакет для обновления FiReMQ

	"github.com/dgraph-io/badger/v4"
)

// ResendLimiter структура лимитера для повторных онлайн-запросов в cmd/PowerShell и QUIC-сервере
var resendLimiter = struct {
	mu   sync.Mutex
	last map[string]time.Time
}{last: make(map[string]time.Time)}

func main() {
	args := os.Args

	// Показывает справку
	if len(args) >= 2 && (args[1] == "?" || strings.EqualFold(args[1], "-h") || strings.EqualFold(args[1], "--help")) {
		printHelp()
		return
	}

	// Показывает версию FiReMQ
	if len(args) >= 2 && strings.EqualFold(args[1], "--version") {
		fmt.Printf("Версия \"FiReMQ\": %s\n", update.CurrentVersion)
		return
	}

	// Проверяет, что все переданные аргументы являются допустимыми флагами
	for _, arg := range os.Args[1:] {
		if !strings.EqualFold(arg, "--RestoreDB") && !strings.EqualFold(arg, "--PasswdDB") {
			fmt.Printf(db.ColorBrightRed+"Ошибка: Неизвестный ключ запуска \"%s\""+db.ColorReset+"\n", arg)
			printHelp()
			os.Exit(1)
		}
	}

	// Умеренный вызов сборщика мусора
	debug.SetGCPercent(80)

	//  Временный перехват логов из пакета "pathsOS" для дальнейшей записи в HTML лог
	// Создание буфера для ранних логов (ДО инициализации главного конфига)
	type bufferedLog struct {
		Level   string // "SYSTEM" или "ERROR"
		Message string
	}
	var startupBuffer []bufferedLog

	// Записываются в консоль (чтобы видеть), и в буфер (чтобы потом записать в файл)
	pathsOS.LogSystem = func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		log.Printf("[СИСТЕМА] %s", msg) // В консоль
		startupBuffer = append(startupBuffer, bufferedLog{"SYSTEM", msg})
	}

	pathsOS.LogError = func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		log.Printf("[ОШИБКА] %s", msg) // В консоль
		startupBuffer = append(startupBuffer, bufferedLog{"ERROR", msg})
	}

	// Загрузка главного конфига
	if err := pathsOS.Init(); err != nil {
		logging.LogError("Инициализация: Ошибка инициализации главного конфига \"server.conf\": %v", err)
	}

	// Инициализация HTML логирования
	logging.InitLog()

	// Перенаправление буфера в реальный логгер
	for _, l := range startupBuffer {
		if l.Level == "ERROR" {
			logging.LogError("%s", l.Message)
		} else {
			logging.LogSystem("%s", l.Message)
		}
	}

	// Инъекции функций логирования в пакет protection и pathsOS, чтобы избежать циклических импортов
	pathsOS.LogSystem = logging.LogSystem
	pathsOS.LogError = logging.LogError
	protection.LogSecurity = logging.LogSecurity
	protection.LogSystem = logging.LogSystem
	protection.LogError = logging.LogError

	// Проверка запуска FiReMQ от суперпользователя в Linux
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		logging.LogSystem("FiReMQ запущен от root. Коррекция прав будет выполнена при завершении.")
		defer func() {
			if err := pathsOS.VerifyAndFixPermissions(); err != nil {
				logging.LogError("Ошибка. Не удалось исправить права доступа при завершении: %v", err)
			} else {
				logging.LogSystem("Коррекция прав и владельца успешно завершена.")
			}
		}()
	}

	// Обработка сигналов для корректного завершения работы
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	// Пакет "update" имеет возможность инициировать завершение так же, как по сигналу
	update.BindShutdown(func() {
		// Отправляет в done, как если бы пришёл SIGINT/SIGTERM
		done <- true
	})

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

	// Режима смены пароля WEB админки (выбор админа через интерактивное меню)
	if len(args) >= 2 && strings.EqualFold(args[1], "--PasswdDB") {
		// Запускает режим смены пароля конкретного админа в БД
		db.PerformPasswordReset()
		return
	}

	// Режим восстановления БД (аварийный режим с интерактивным меню)
	if len(args) >= 2 && strings.EqualFold(args[1], "--RestoreDB") {
		// Запускает режим восстановления БД BadgerDB
		db.PerformRestoreMode()
		return
	}

	// Определение режима (интерактив/служба) для проверки и создания mTLS сертификатов
	new_cert.InitAndCheckMTLS()

	// Проверка и исправление права доступа для Linux после загрузки конфига
	if err := pathsOS.VerifyAndFixPermissions(); err != nil {
		// Ошибки внутри уже логируются, но можно добавить общее предупреждение
		logging.LogError("Инициализация: Возникли проблемы при проверке прав доступа: %v", err)
	}

	// Проверка и корректировка прав доступа для исполняемых утилит "7zzs" и "ServerUpdater"
	pathsOS.VerifyExecutableFilesRights()

	// Загрузка HTML шаблонов после инициализации конфига
	if err := loadTemplates(); err != nil {
		logging.LogError("Инициализация: Ошибка загрузки WEB шаблонов: %v", err)
	}

	// Проверка конфига "mqtt_config.json", если отсутствует, тогда создаём его
	if err := mqtt_server.EnsureMQTTConfig(); err != nil {
		logging.LogError("Инициализация: Ошибка MQTT конфига: %v", err)
		return
	}

	// Инъекция функций из "main" пакета в пакет "mqtt_server"
	mqtt_server.SaveClientInfo = SaveClientInfo                   // Из файла "clients.go"
	mqtt_server.HandleAnswerMessage = HandleAnswerMessage         // Для cmd/PowerShell
	mqtt_server.HandleQUICAnswerMessage = HandleQUICAnswerMessage // Для Установки ПО (QUIC)

	// Инициализация БД
	if err := db.InitDB(); err != nil {
		logging.LogError("Инициализация: Ошибка инициализации БД: %v", err)
	}

	// Запуск планировщика бэкапов БД
	db.StartAutoBackup()

	defer func() { // Завершение работы с BadgerDB при завершении основной программы
		if err := db.Close(); err != nil {
			logging.LogError("Завершение: Ошибка закрытия БД: %v", err)
		}
	}()

	// Очистка возможного мусора в директории "Path_QUIC_Downloads"
	cleanupTempFiles()

	// Инициализация Coraza WAF с откатом из бэкапа при ошибках конфигурации OWASP CRS
	if err := protection.InitializeWAFWithRecovery(); err != nil {
		logging.LogError("Инициализация: Не удалось инициализировать Coraza WAF после отката: %v", err)
	}

	// Запуск mqtt-сервера
	mqtt_server.Mqtt_serv()

	// Запуск mqtt-клиента "AutoPaho" в отдельной горутине
	go mqtt_client.StartMQTTClient()

	// Запуск веб-сервера
	go StartWebServer(protection.GetCurrentWAF)

	// Запуск фонового обновления статусов
	go BackgroundStatusUpdate()

	// Контекст для управления жизненным циклом QUIC‐сервера
	ctx, cancel := context.WithCancel(context.Background())
	var wgQUIC sync.WaitGroup

	// Запуск QUIC‐сервера в горутине
	wgQUIC.Go(func() {
		StartQUICServer(ctx)
	})

	logging.LogSystem("FiReMQ запущен!")

	// Ожидание завершения
	<-done
	logging.LogSystem("Завершение работы FiReMQ...")
	// Остановка QUIC‐сервера
	cancel()

	// Ждём, пока горутина QUIC‐сервера полностью завершится
	wgQUIC.Wait()

	// Остановка клиента AutoPaho
	mqtt_client.StopMQTTClient()

	// Остановка сервера Mochi MQTT
	if err := mqtt_server.Stop(); err != nil {
		logging.LogError("Завершение: Ошибка остановки MQTT-сервера: %v", err)
	}

	logging.LogSystem("FiReMQ корректно завершён.")
}

// GetTimestampWithMs форматирует дату/время с миллисекундами (минимум 2 знака) – используется для даты создания запроса в "Date_Of_Creation"
func getTimestampWithMs(t time.Time) string {
	base := t.Format("02.01.06(15:04:05)")
	ms := t.Nanosecond() / 1e6 // миллисекунды
	return fmt.Sprintf("%s:%02d", base, ms)
}

// CleanupTempFiles удаляет возможные мусорные файлы из директории "pathsOS.Path_QUIC_Downloads"
func cleanupTempFiles() {
	// Проверяет и создаёт директорию, если она отсутствует
	if err := pathsOS.EnsureDir(pathsOS.Path_QUIC_Downloads); err != nil {
		logging.LogError("Очистка Downloads: Ошибка создания директории %s: %v", pathsOS.Path_QUIC_Downloads, err)
		return
	}

	// Читает содержимое директории
	entries, err := os.ReadDir(pathsOS.Path_QUIC_Downloads)
	if err != nil {
		logging.LogError("Очистка Downloads: Ошибка чтения директории %s: %v", pathsOS.Path_QUIC_Downloads, err)
		return
	}

	// Поиск и удаление временных файлов с префиксом "upload-"
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), "upload-") {
			filePath := filepath.Join(pathsOS.Path_QUIC_Downloads, e.Name())

			if err := os.Remove(filePath); err != nil {
				logging.LogError("Очистка Downloads: Ошибка удаления временного файла %s: %v", filePath, err)
			} else {
				logging.LogSystem("Очистка Downloads: Удалён временный файл: %s", filePath)
			}
		}
	}

	// Если БД ещё не инициализирована — дальше не пытается чистить "осиротевшие" файлы
	if db.DBInstance == nil {
		return
	}

	// Сборка множества имён файлов, которые сейчас используются в QUIC-записях БД
	referenced := make(map[string]struct{})
	err = db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_QUIC:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			var record map[string]any
			if err := it.Item().Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			}); err != nil {
				continue
			}

			quicStr, ok := record["QUIC_Command"].(string)
			if !ok || quicStr == "" {
				continue
			}

			var payload QUICPayload
			if err := json.Unmarshal([]byte(quicStr), &payload); err != nil {
				continue
			}

			base := baseNameAnyOS(payload.DownloadRunPath)
			if base != "" {
				referenced[base] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		logging.LogError("Очистка Downloads: Ошибка чтения БД при сборе ссылок на файлы QUIC: %v", err)
		return
	}

	// Повторное чтение каталога (после удаления upload-*)
	entries, err = os.ReadDir(pathsOS.Path_QUIC_Downloads)
	if err != nil {
		logging.LogError("Очистка Downloads: Ошибка повторного чтения директории %s: %v", pathsOS.Path_QUIC_Downloads, err)
		return
	}

	// Удаление из папки всех файлов, которые не упомянуты ни в одной записи БД
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "upload-") {
			continue // Уже обработано
		}
		if _, inUse := referenced[name]; inUse {
			continue // Файл используется хотя бы одним запросом — не трогает
		}

		filePath := filepath.Join(pathsOS.Path_QUIC_Downloads, name)
		maxRetries := 3
		var lastErr error
		for i := 0; i < maxRetries; i++ {
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				lastErr = nil
				break
			}
			if err := os.Remove(filePath); err != nil {
				lastErr = err
				if i < maxRetries-1 {
					time.Sleep(100 * time.Millisecond)
					continue
				}
			} else {
				logging.LogSystem("Очистка Downloads: Удалён неиспользуемый файл: %s", filePath)
				lastErr = nil
				break
			}
		}
		if lastErr != nil {
			logging.LogError("Очистка Downloads: Не удалось удалить неиспользуемый файл %s: %v", filePath, lastErr)
		}
	}
}

// IsClientOnline проверяет, находится ли клиент в онлайне (поле "status" == "On")
func isClientOnline(clientID string) (bool, error) {
	var online bool
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("client:" + clientID))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil // Клиент отсутствует в БД
			}
			return err
		}

		var data map[string]string
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &data)
		}); err != nil {
			return err
		}
		if data["status"] == "On" {
			online = true
		}
		return nil
	})
	return online, err
}

// AllowResend ограничитель повторных онлайн-отправок в cmd/PowerShell и QUIC-сервере
func allowResend(clientID string, window time.Duration) (bool, time.Duration) {
	resendLimiter.mu.Lock()
	defer resendLimiter.mu.Unlock()
	now := time.Now()
	if t, ok := resendLimiter.last[clientID]; ok {
		if since := now.Sub(t); since < window {
			return false, window - since
		}
	}
	resendLimiter.last[clientID] = now
	return true, 0
}

// printHelp выводит справку по доступным ключам запуска
func printHelp() {
	// Ярко-синий цвет ключей, для контрастности
	blue := db.ColorBrightBlue
	reset := db.ColorReset

	fmt.Println("Доступные ключи запуска FiReMQ:")
	fmt.Printf("    %s?%s, %s-h%s, %s--help%s          — Вызов справки.\n", blue, reset, blue, reset, blue, reset)
	fmt.Printf("    %s--version%s              — Узнать версию FiReMQ.\n", blue, reset)
	fmt.Printf("    %s--RestoreDB%s            — Режим восстановления БД из бэкапа (интерактивный режим), запускать от root и остановленной службой firemq.\n", blue, reset)
	fmt.Printf("    %s--PasswdDB%s             — Режим смены пароля WEB админки (интерактивный режим), запускать от root и остановленной службой firemq.\n", blue, reset)
}
