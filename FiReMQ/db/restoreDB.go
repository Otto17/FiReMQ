// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package db

import (
	"archive/zip"
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"FiReMQ/pathsOS" // Локальный пакет с путями

	"github.com/dgraph-io/badger/v4"
)

// ANSI цветовые коды для оформления консоли
const (
	ColorReset      = "\033[0m"  //  Сброс цвета по умолчанию
	ColorRed        = "\033[31m" // Красный
	ColorBrightRed  = "\033[91m" // Ярко-красный
	ColorGreen      = "\033[32m" // Зелёный
	ColorYellow     = "\033[33m" // Жёлтый
	ColorCyan       = "\033[36m" // Бирюзовый
	ColorBrightBlue = "\033[94m" // Ярко-синий
)

// backupFile структура представляет информацию о файле бэкапа
type backupFile struct {
	Name    string    // Имя ZIP файла
	Path    string    // Полный путь к ZIP файлу
	ModTime time.Time // Время последнего изменения файла
}

// PerformRestoreMode запускает интерактивный режим восстановления БД
func PerformRestoreMode() {
	// Сбрасывает перехват сигналов (Ctrl+C), установленный в main.go
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)

	enableANSI() // Включает поддержку ANSI цветов в Windows

	// Проверяет права доступа и состояние службы (только для Linux)
	if runtime.GOOS == "linux" {
		if os.Geteuid() != 0 {
			fmt.Println("Ошибка: Откат возможен только от пользователя root!")
			os.Exit(1)
		}
		if isServiceRunning() {
			fmt.Println("Ошибка: Перед откатом остановите службу командой \"systemctl stop firemq\"!")
			os.Exit(1)
		}
	}

	// Получает список доступных бэкапов
	backups, err := getBackupList()
	if err != nil {
		fmt.Printf("Ошибка чтения директории бэкапов: %v\n", err)
		os.Exit(1)
	}

	if len(backups) == 0 {
		fmt.Println("Нет доступных бэкапов для восстановления.")
		os.Exit(0)
	}

	// Интерактивное меню выбора: показывает по 10 бэкапов, начинаем с конца
	reader := bufio.NewReader(os.Stdin)
	pageSize := 10   // До 10 строк на страницу
	currentPage := 0 // Начальная страница
	totalPages := (len(backups) + pageSize - 1) / pageSize

	currentPage = totalPages - 1

	// Цикл интерактивного выбора бэкапа (пагинация + ввод)
	for {
		startIndex := currentPage * pageSize
		endIndex := min(startIndex+pageSize, len(backups))

		fmt.Println("\n----------------------------------------")
		fmt.Println("Выберите бэкап БД для отката:")
		fmt.Println("")

		currentList := backups[startIndex:endIndex]
		for i, b := range currentList {
			globalIndex := startIndex + i
			name := strings.TrimSuffix(b.Name, ".zip")
			fmt.Printf("%d > %s\n", globalIndex, name)
		}
		fmt.Println("")

		// Формирование цветной строки запроса
		prompt := fmt.Sprintf("Введите номер бэкапа (%sa=Отмена%s", ColorGreen, ColorReset)

		if len(backups) > pageSize {
			prompt += fmt.Sprintf(", %sp=Предыдущий%s, %sn=Следующий%s",
				ColorYellow, ColorReset,
				ColorCyan, ColorReset)
		}
		prompt += "): "
		fmt.Print(prompt)

		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("\nВыход.")
			os.Exit(0)
		}
		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "a", "c", "ф", "с":
			fmt.Println("Операция отменена.")
			os.Exit(0)
		case "n", "т":
			if len(backups) > pageSize && currentPage < totalPages-1 {
				currentPage++
			} else {
				fmt.Println(">> Это последняя страница.")
			}
		case "p", "з":
			if len(backups) > pageSize && currentPage > 0 {
				currentPage--
			} else {
				fmt.Println(">> Это первая страница.")
			}
		default:
			idx, err := strconv.Atoi(input)
			if err != nil || idx < 0 || idx >= len(backups) {
				fmt.Println(">> Неверный ввод, попробуйте снова.")
				continue
			}

			selectedBackup := backups[idx]
			backupNameClean := strings.TrimSuffix(selectedBackup.Name, ".zip")

			// Запрос подтверждения
			fmt.Println("")
			fmt.Printf("Произведётся откат БД из бэкапа \"%s\", нажмите %sEnter для подтверждения%s или %s'a' для отмены%s!",
				backupNameClean,
				ColorRed, ColorReset,
				ColorGreen, ColorReset,
			)

			confirmInput, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("\n\nОперация прервана.")
				os.Exit(0)
			}

			confirmInput = strings.TrimSpace(strings.ToLower(confirmInput))

			// Отмена операции, если пользователь ввел что-либо кроме Enter
			if confirmInput != "" {
				fmt.Println("\nОтмена операции.")
				continue
			}

			// Выполнение восстановления
			fmt.Println("\nЗапуск процесса восстановления...")
			if err := restoreFromZip(selectedBackup.Path); err != nil {
				fmt.Printf("\n%sОШИБКА отката:%s %v\n", ColorRed, ColorReset, err)
				os.Exit(1)
			}

			// Финальное сообщение
			var restartMsg string
			if runtime.GOOS == "linux" {
				restartMsg = "Запустите службу вручную \"systemctl start firemq\"."
			} else {
				restartMsg = "Запустите FiReMQ."
			}

			fmt.Printf("\n%sОткат бэкапа прошёл успешно!%s %s\n", ColorGreen, ColorReset, restartMsg)
			os.Exit(0)
		}
	}
}

// restoreFromZip выполняет физическое восстановление данных из ZIP архива
func restoreFromZip(zipPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("не удалось открыть ZIP файл: %w", err)
	}
	defer r.Close()

	// Ищет файл данных BadgerDB внутри ZIP архива
	var dataFile *zip.File
	for _, f := range r.File {
		if f.Name == "badger_backup.data" {
			dataFile = f
			break
		}
	}
	if dataFile == nil {
		return fmt.Errorf("в архиве отсутствует файл 'badger_backup.data'")
	}

	rc, err := dataFile.Open()
	if err != nil {
		return fmt.Errorf("ошибка чтения файла из архива: %w", err)
	}
	defer rc.Close()

	// Очищает старую директорию БД, чтобы избежать конфликтов при восстановлении
	log.Println("Очистка текущей директории базы данных...")
	if err := os.RemoveAll(pathsOS.Path_DB); err != nil {
		return fmt.Errorf("не удалось очистить текущую директорию БД: %w", err)
	}
	if err := pathsOS.EnsureDir(pathsOS.Path_DB); err != nil {
		return fmt.Errorf("не удалось создать директорию БД: %w", err)
	}

	opts := badger.DefaultOptions(pathsOS.Path_DB).WithLoggingLevel(badger.WARNING)

	// Открывает BadgerDB в пустой директории для записи восстановленных данных
	dbRestore, err := badger.Open(opts)
	if err != nil {
		return fmt.Errorf("ошибка открытия BadgerDB для восстановления: %w", err)
	}
	defer dbRestore.Close()

	// Загружает данные из потока бэкапа (rc) в новую БД
	log.Println("Применение данных из бэкапа...")
	if err := dbRestore.Load(rc, 16); err != nil {
		return fmt.Errorf("BadgerDB Load failed: %w", err)
	}

	if runtime.GOOS == "linux" {
		// Восстанавление необходимых прав доступа для службы FiReMQ в Linux
		log.Println("Восстановление прав доступа...")
		if err := pathsOS.VerifyAndFixPermissions(); err != nil {
			log.Printf("Предупреждение: не удалось исправить права после восстановления: %v", err)
		}
	}

	return nil
}

// getBackupList сканирует директорию бэкапов и возвращает список доступных ZIP файлов
func getBackupList() ([]backupFile, error) {
	dir := pathsOS.Path_Backup

	// Проверка существования директории бэкапов
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, fmt.Errorf("директория бэкапов не найдена: %s", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var backups []backupFile

	// Фильтрует записи, оставляя только файлы бэкапов в формате "Backup_DB_*.zip"
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "Backup_DB_") && strings.HasSuffix(strings.ToLower(name), ".zip") {
			info, err := e.Info()
			if err != nil {
				continue
			}
			backups = append(backups, backupFile{
				Name:    name,
				Path:    filepath.Join(dir, name),
				ModTime: info.ModTime(),
			})
		}
	}

	// Сортирует список бэкапов по времени создания (от старых к новым)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].ModTime.Before(backups[j].ModTime)
	})

	return backups, nil
}

// isServiceRunning проверяет, активна ли служба firemq в systemd (только для Linux)
func isServiceRunning() bool {
	cmd := exec.Command("systemctl", "is-active", "firemq")
	output, _ := cmd.Output()
	status := strings.TrimSpace(string(output))
	return status == "active"
}
