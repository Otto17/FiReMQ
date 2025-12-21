// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package db

import (
	"archive/zip"
	"compress/flate"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"FiReMQ/logging" // Локальный пакет с логированием в HTML файл
	"FiReMQ/pathsOS" // Локальный пакет с путями для разных платформ
)

// StartAutoBackup запускает фоновый процесс периодического бэкапа БД с ротацией
func StartAutoBackup() {
	intervalStr := pathsOS.DB_Backup_Interval
	hours, err := strconv.Atoi(intervalStr)
	if err != nil || hours <= 0 {
		logging.LogSystem("Автобэкап БД: Автоматический бэкап БД отключён (интервал: %s)", intervalStr)
		return
	}

	retentionStr := pathsOS.DB_Backup_Retention_Count
	retentionCount, err := strconv.Atoi(retentionStr)
	if err != nil || retentionCount < 1 {
		retentionCount = 5 // Значение по умолчанию, если в конфиге ошибка
	}

	// log.Printf("Запущен планировщик бэкапов БД. Интервал: %d ч. Хранить копий: %d. Путь: %s", hours, retentionCount, pathsOS.Path_Backup)

	go func() {
		ticker := time.NewTicker(time.Duration(hours) * time.Hour)
		defer ticker.Stop()

		// Цикл событий для правильного создания бэкапов
		for range ticker.C {
			// Пытается создать бэкап
			if err := performHotBackup(); err != nil {
				logging.LogError("Автобэкап БД: Автоматический бэкап БД завершился ошибкой: %v", err)
				// Если ошибка при создании, старые бэкапы НЕ удаляет
			} else {
				// logging.LogSystem("Успешно создан автоматический бэкап БД") // ДЛЯ ОТЛАДКИ
				// Только если бэкап успешно создан, запускает очистку старых
				pruneOldBackups(retentionCount)
			}
		}
	}()
}

// performHotBackup выполняет "горячий" бэкап BadgerDB в ZIP архив
func performHotBackup() error {
	if DBInstance == nil {
		return fmt.Errorf("база данных не инициализирована")
	}

	// Создаёт директорию для бэкапов, если она не существует
	if err := pathsOS.EnsureDir(pathsOS.Path_Backup); err != nil {
		return err
	}

	// Формирование имени файла: Backup_DB_дд.мм.гг(в_ЧЧ.ММ.СС).zip
	now := time.Now()
	fileName := fmt.Sprintf("Backup_DB_%s.zip", now.Format("02.01.06(в_15.04.05)"))
	zipPath := filepath.Join(pathsOS.Path_Backup, fileName)

	// Создаёт файл архива
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("не удалось создать файл архива: %w", err)
	}
	defer zipFile.Close()

	// Инициализирует ZIP писатель
	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Регистрирует компрессор для уровня сжатия BestCompression (9)
	zipWriter.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestCompression)
	})

	// Создаёт заголовок файла внутри архива
	writerInZip, err := zipWriter.Create("badger_backup.data")
	if err != nil {
		return fmt.Errorf("ошибка создания файла внутри ZIP: %w", err)
	}

	// Выполняет Backup (0 - Full Backup), BadgerDB пишет данные в поток writerInZip, а ZIP сжимает их на лету
	ts, err := DBInstance.Backup(writerInZip, 0)
	if err != nil {
		return fmt.Errorf("ошибка BadgerDB Backup: %w", err)
	}

	// Принудительно закрывает zipWriter, чтобы данные записались до закрытия файла
	if err := zipWriter.Close(); err != nil {
		return fmt.Errorf("ошибка закрытия ZIP: %w", err)
	}

	// Получает размер файла для лога
	fi, _ := zipFile.Stat()
	sizeMB := float64(fi.Size()) / 1024 / 1024

	logging.LogAction("Автобэкап БД: Бэкап БД записан: %s (версия TS: %d, размер: %.2f МБ)", fileName, ts, sizeMB)
	return nil
}

// pruneOldBackups удаляет старые архивы бэкапов, оставляя только maxKeep последних
func pruneOldBackups(maxKeep int) {
	dir := pathsOS.Path_Backup
	entries, err := os.ReadDir(dir)
	if err != nil {
		logging.LogError("Автобэкап БД: Ошибка чтения директории бэкапов для очистки: %v", err)
		return
	}

	// Собирает информацию о файлах бэкапов
	type fileInfo struct {
		Name    string
		Path    string
		ModTime time.Time
	}
	var backups []fileInfo

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// Фильтрует только файлы бэкапов БД по префиксу и расширению
		name := e.Name()
		if strings.HasPrefix(name, "Backup_DB_") && strings.HasSuffix(strings.ToLower(name), ".zip") {
			info, err := e.Info()
			if err != nil {
				continue
			}
			backups = append(backups, fileInfo{
				Name:    name,
				Path:    filepath.Join(dir, name),
				ModTime: info.ModTime(),
			})
		}
	}

	// Если файлов меньше или равно лимиту, удалять нечего
	if len(backups) <= maxKeep {
		return
	}

	// Сортирует по времени изменения (сначала старые, в конце новые)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].ModTime.Before(backups[j].ModTime)
	})

	// Вычисляет, сколько нужно удалить
	toDeleteCount := len(backups) - maxKeep

	// Удаляет самый старый бэкап
	for i := 0; i < toDeleteCount; i++ {
		f := backups[i]
		if err := os.Remove(f.Path); err != nil {
			logging.LogError("Автобэкап БД: Не удалось удалить старый бэкап %s: %v", f.Name, err)
		} else {
			logging.LogSystem("Автобэкап БД: Ротация бэкапов, удалён старый архив %s", f.Name)
		}
	}
}
