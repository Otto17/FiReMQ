// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package update

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"FiReMQ/logging" // Локальный пакет с логированием в HTML файл
	"FiReMQ/pathsOS" // Локальный пакет с путями для разных платформ
)

// shutdownFn хранит функцию для мягкого завершения работы сервера
var shutdownFn func()

// BindShutdown устанавливает функцию, которая будет вызвана для завершения работы сервера
func BindShutdown(fn func()) { shutdownFn = fn }

// signalShutdownAfter инициирует мягкое завершение работы сервера после указанной задержки
func signalShutdownAfter(d time.Duration) {
	if shutdownFn == nil {
		return
	}
	// Запускает ожидание в отдельной горутине, чтобы не блокировать текущий обработчик
	go func() {
		time.Sleep(d)
		shutdownFn()
	}()
}

// absFromExeDir нормализует путь, делая его абсолютным относительно директории исполняемого файла FiReMQ
func absFromExeDir(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("пустой путь") // Запрещает пустые пути, чтобы избежать ошибок при объединении
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil // Возвращает абсолютный путь сразу после очистки
	}
	dir, err := exeDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить директорию FiReMQ: %w", err)
	}
	return filepath.Clean(filepath.Join(dir, p)), nil
}

// updaterPathAbs возвращает абсолютный путь к утилите ServerUpdater, которая должна находиться рядом с исполняемым файлом FiReMQ
func updaterPathAbs() (string, error) {
	dir, err := exeDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить директорию FiReMQ: %w", err)
	}

	// Утилита ServerUpdater
	p := filepath.Join(dir, updaterName)
	st, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("%s не найден: %s (%v)", updaterName, p, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("ожидался файл %s, но найден каталог: %s", updaterName, p) // Проверяет, что утилита является файлом, а не каталогом
	}
	return p, nil
}

// CheckHandler обрабатывает запрос на проверку наличия новой версии FiReMQ
func CheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не разрешен", http.StatusMethodNotAllowed) // Разрешает только метод GET для запроса статуса
		return
	}

	meta, err := CheckLatest()

	// RepoState для клиента в заголовке: ok | older | none
	repoState := "none"
	var newPtr *string

	switch {
	case err == nil:
		v := strings.TrimSpace(meta.RemoteVersion)
		if v != "" {
			if strings.TrimSpace(v) == strings.TrimSpace(CurrentVersion) {
				// Релиз равен текущей версии — показывает его
				newPtr = &v
				repoState = "ok"
			} else {
				need, cmpErr := isRemoteNewer(CurrentVersion, v)
				if cmpErr != nil {
					http.Error(w, fmt.Errorf("ошибка сравнения версий: %w", cmpErr).Error(), http.StatusBadGateway)
					return
				}
				if need {
					// Релиз новее — показывает
					newPtr = &v
					repoState = "ok"
				} else {
					// Релиз в репозитории старее — не показывает
					repoState = "older"
				}
			}
		}
	case errors.Is(err, ErrNoMatchingAsset) || errors.Is(err, ErrNoReleases):
		repoState = "none" // Релизов нет/ассетов нет
	default:
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Предоставляет информацию о версии последнего успешного бэкапа для пользователя
	var bkpPtr *string
	if backupPath, _, err2 := latestFiReMQBackupPath(); err2 == nil && backupPath != "" {
		ver, err := backupVersionFromZip(backupPath)
		if err != nil || strings.TrimSpace(ver) == "" {
			ver = extractVersionFromBackupName(filepath.Base(backupPath))
		}
		if strings.TrimSpace(ver) != "" {
			bkpPtr = &ver
		}
	}

	w.Header().Set("Content-Type", "application/json")

	// Добавляет служебный заголовок для фронта
	w.Header().Set("X-FiReMQ-Repo-State", repoState)

	_ = json.NewEncoder(w).Encode(map[string]any{
		"CurrentVersion": CurrentVersion,
		"NewVersion":     newPtr, // Равная или новее; null — если старее или релизов нет
		"BackupVersion":  bkpPtr,
	})
}

// UpdateHandler инициирует процесс обновления FiReMQ, запуская внешний ServerUpdater
func UpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed) // Разрешает только метод POST для выполнения команды
		return
	}

	// Под Windows кастомные обновления больше не поддерживается!
	if runtime.GOOS == "windows" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"UpdateAnswer": "Ошибка",
			"Description":  "Обновление FiReMQ недоступно под Windows. Используйте Linux для полноценной работы с FiReMQ.",
		})
		logging.LogUpdate("Обновление FiReMQ: Запрос обновления отклонён — автообновление не поддерживается под Windows. Используйте Linux для полноценной работы с FiReMQ.")
		return
	}

	zipPath, meta, err := PrepareUpdate()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Description": err.Error(),
		})
		return
	}

	updAbs, err := updaterPathAbs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	zipAbs, err := absFromExeDir(zipPath)
	if err != nil {
		http.Error(w, fmt.Errorf("не удалось нормализовать путь архива обновления: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	// Передает PID текущего процесса, чтобы ServerUpdater мог дождаться его завершения перед применением обновления
	pid := os.Getpid()
	cmd := exec.Command(updAbs, "-apply-zip", zipAbs, CurrentVersion, strconv.Itoa(pid))
	// Запускает апдейтер как отдельный процесс, чтобы он мог заменить текущий исполняемый файл
	if err := cmd.Start(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"Description": err.Error()})
		return
	}
	logging.LogUpdate("Обновление FiReMQ: Инициировано обновление сервера до версии %s. ServerUpdater запущен (PID=%d), архив: %s", meta.RemoteVersion, cmd.Process.Pid, zipAbs)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"UpdateAnswer": "Успех",
		"Version":      meta.RemoteVersion,
	})

	// Обеспечивает паузу для гарантированного старта ServerUpdater перед мягким завершением текущего процесса
	signalShutdownAfter(1 * time.Second)
}

// RollbackHandler инициирует откат к последнему сохраненному бэкапу, запуская внешний ServerUpdater
func RollbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed) // Разрешает только метод POST для выполнения команды
		return
	}

	// Под Windows откат версий больше не поддерживается!
	if runtime.GOOS == "windows" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"RollbackAnswer": "Ошибка",
			"Description":    "Откат версии FiReMQ недоступно под Windows. Используйте Linux для полноценной работы с FiReMQ.",
		})
		logging.LogUpdate("Обновление FiReMQ: Запрос отката отклонён — откат не поддерживается под Windows. Используйте Linux для полноценной работы с FiReMQ.")
		return
	}

	backupPath, backupDir, err := latestFiReMQBackupPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if backupPath == "" {
		logging.LogUpdate("Обновление FiReMQ: Откат FiReMQ отклонён: бэкапов нет в %s", backupDir)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"RollbackAnswer": "Ошибка",
			"Description":    "Бэкапа пока ещё нет, он появится после первого обновления.",
		})
		return
	}

	// Предпочитает версию из manifest.json как более надежный источник информации
	bkpVer, err := backupVersionFromZip(backupPath)
	if err != nil || strings.TrimSpace(bkpVer) == "" {
		bkpVer = extractVersionFromBackupName(filepath.Base(backupPath))
	}

	logging.LogUpdate("Обновление FiReMQ: Проверка условий отката: Текущая=%q, Бэкап=%q, Файл=%s", strings.TrimSpace(CurrentVersion), strings.TrimSpace(bkpVer), backupPath)

	// Предотвращает откат, если невозможно гарантировать целевую версию бэкапа
	if strings.TrimSpace(bkpVer) == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"RollbackAnswer": "Ошибка",
			"Description":    "Не удалось определить версию в бэкапе — откат отменён для безопасности.",
		})
		return
	}

	// Избегает ненужных операций, если текущая версия идентична версии бэкапа
	if strings.TrimSpace(bkpVer) == strings.TrimSpace(CurrentVersion) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"RollbackAnswer": "Не требуется",
			"message":        "Не требуется! Текущая версия совпадает с версией в бэкапе",
		})
		return
	}

	updAbs, err := updaterPathAbs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cmd := exec.Command(updAbs, "-rollback")
	if err := cmd.Start(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"Description": err.Error()})
		return
	}

	logging.LogUpdate("Обновление FiReMQ: Инициирован откат сервера к версии %s. ServerUpdater запущен для отката (PID=%d) с бэкапом: %s.", bkpVer, cmd.Process.Pid, backupPath)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"RollbackAnswer": "Успех",
		"message":        "Откат выполнен. Ожидайте перезагрузку страницы!",
	})

	signalShutdownAfter(1 * time.Second)
}

// latestFiReMQBackupPath находит путь к самому новому ZIP-архиву бэкапа FiReMQ
func latestFiReMQBackupPath() (backupPath string, backupDir string, err error) {
	exeBase, err := exeDir()
	if err != nil {
		return "", "", fmt.Errorf("не удалось определить директорию FiReMQ: %w", err)
	}
	backupDir = strings.TrimSpace(pathsOS.Path_Backup)
	if backupDir == "" {
		backupDir = "Backup"
	}
	if !filepath.IsAbs(backupDir) {
		backupDir = filepath.Join(exeBase, backupDir)
	}

	ents, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", backupDir, nil // Обрабатывает случай, когда директория бэкапов отсутствует
		}
		return "", backupDir, fmt.Errorf("ошибка чтения директории %s: %v", backupDir, err)
	}

	type item struct {
		name string
		ts   time.Time
	}
	var items []item

	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, "bak_") && strings.HasSuffix(n, "_FiReMQ.zip") {
			// Использует стандартизированное имя файла для определения порядка бэкапов
			ts := parseBackupTimestampFromName(n)
			// Использует время модификации файла, если парсинг из имени не удался
			if ts.IsZero() {
				if fi, err := os.Stat(filepath.Join(backupDir, n)); err == nil {
					ts = fi.ModTime()
				}
			}
			items = append(items, item{name: n, ts: ts})
		}
	}
	if len(items) == 0 {
		return "", backupDir, nil
	}

	// Сортирует по времени создания, чтобы определить самый новый бэкап
	sort.Slice(items, func(i, j int) bool { return items[i].ts.After(items[j].ts) }) // по времени убыв
	return filepath.Join(backupDir, items[0].name), backupDir, nil
}

// parseBackupTimestampFromName извлекает и парсит временную метку из имени файла бэкапа
func parseBackupTimestampFromName(name string) time.Time {
	s := strings.TrimPrefix(name, "bak_")
	// Разделяет имя файла, чтобы точно выделить строку с датой и временем
	end := strings.Index(s, "_ver=")
	if end < 0 {
		end = strings.Index(s, "_FiReMQ.zip")
		if end < 0 {
			return time.Time{}
		}
	}
	tsStr := s[:end]
	t, err := time.Parse(backupTimestampLayout, tsStr)
	if err != nil {
		return time.Time{}
	}
	return t
}

// backupVersionFromZip извлекает информацию о версии из файла manifestBackup.json внутри ZIP-архива
func backupVersionFromZip(zipPath string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == "manifestBackup.json" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return "", err
			}
			// Обеспечивает корректный JSON парсинг, если файл был сохранен с BOM
			if len(data) >= 3 && bytes.Equal(data[:3], []byte{0xEF, 0xBB, 0xBF}) {
				data = data[3:]
			}
			var mf manifestBackup
			if err := json.Unmarshal(data, &mf); err != nil {
				return "", err
			}
			return strings.TrimSpace(mf.Version), nil
		}
	}
	return "", fmt.Errorf("manifestBackup.json не найден в %s", zipPath)
}

// extractVersionFromBackupName парсит версию из имени ZIP-файла в качестве запасного механизма
func extractVersionFromBackupName(name string) string {
	idx := strings.Index(name, "ver=")
	if idx < 0 {
		return "" // Прекращает работу, если префикс версии не найден
	}
	rest := name[idx+len("ver="):]
	end := strings.Index(rest, "_FiReMQ.zip")
	if end < 0 {
		end = strings.Index(rest, ".zip")
		if end < 0 {
			return ""
		}
	}
	return strings.TrimSpace(rest[:end])
}
