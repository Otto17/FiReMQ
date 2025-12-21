// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package protection

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/corazawaf/coraza/v3"

	"FiReMQ/pathsOS" // Локальный пакет с путями для разных платформ
)

// LogSecurity используется для логирования событий безопасности (защита от циклического импорта)
var LogSystem func(format string, args ...any)
var LogError func(format string, args ...any)

// currentWAF хранит текущий активный экземпляр Coraza WAF
var currentWAF coraza.WAF

// wafMutex обеспечивает потокобезопасный доступ к currentWAF
var wafMutex sync.RWMutex

// GitHubRelease представляет структуру JSON ответа от GitHub API для получения информации о релизе
type GitHubRelease struct {
	TagName string `json:"tag_name"` // Тег релиза
	Assets  []struct {
		Name        string `json:"name"`                 // Имя файла ассета
		DownloadURL string `json:"browser_download_url"` // URL для скачивания ассета
	} `json:"assets"`
}

// VersionResponse представляет структуру JSON ответа для проверки новой версии
type VersionResponse struct {
	CurrentVersion string  `json:"CurrentVersion"` // Текущая версия
	NewVersion     string  `json:"NewVersion"`     // Новая версия
	BackupVersion  *string `json:"BackupVersion"`  // Версия из бэкапа (null, если нет)
}

// RollbackResponse представляет структуру JSON ответа для отката
type RollbackResponse struct {
	RollbackAnswer  string `json:"RollbackAnswer"`            // Результат операции ("Успех" или "Ошибка")
	RollbackVersion string `json:"RollbackVersion,omitempty"` // Версия после отката
	Description     string `json:"Description,omitempty"`     // Описание ошибки
}

// UpdateResponse представляет структуру JSON ответа для обновления
type UpdateResponse struct {
	UpdateAnswer string `json:"UpdateAnswer"`          // Результат операции ("Успех", "Ошибка" или "Обновление не требуется")
	Version      string `json:"Version,omitempty"`     // Версия, до которой было выполнено обновление
	Description  string `json:"Description,omitempty"` // Описание ошибки или результата
}

// CheckOWASPHandler проверяет наличие новой версии правил OWASP CRS и возвращает JSON-ответ
func CheckOWASPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не разрешен", http.StatusMethodNotAllowed)
		return
	}

	latestVersion, _, err := getLatestReleaseInfo()
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка получения информации о релизе: %v", err), http.StatusInternalServerError)
		return
	}

	currentVersion, err := getCurrentVersion(pathsOS.Path_Setup_OWASP_CRS)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка чтения текущей версии: %v", err), http.StatusInternalServerError)
		return
	}

	// Определяет версию из последнего бэкапа, если он существует
	var backupPtr *string
	if backupFile, errBk := findLatestBackup(); errBk == nil && backupFile != "" {
		if bv := extractVersionFromBackupFilename(filepath.Base(backupFile)); strings.TrimSpace(bv) != "" {
			backupPtr = &bv
		}
	}

	response := VersionResponse{
		CurrentVersion: currentVersion,
		NewVersion:     latestVersion,
		BackupVersion:  backupPtr,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateOWASPHandler выполняет обновление правил OWASP CRS
func UpdateOWASPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	latestVersion, downloadURL, err := getLatestReleaseInfo()
	if err != nil {
		response := UpdateResponse{
			UpdateAnswer: "Ошибка",
			Description:  fmt.Sprintf("Ошибка получения информации о релизе: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	currentVersion, err := getCurrentVersion(pathsOS.Path_Setup_OWASP_CRS)
	if err != nil {
		response := UpdateResponse{
			UpdateAnswer: "Ошибка",
			Description:  fmt.Sprintf("Ошибка чтения текущей версии: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Отклоняет обновление, если текущая версия уже не старше последней доступной
	if compareVersions(currentVersion, latestVersion) >= 0 {
		response := UpdateResponse{
			UpdateAnswer: "Обновление не требуется",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	err = performUpdate(downloadURL)
	if err != nil {
		response := UpdateResponse{
			UpdateAnswer: "Ошибка",
			Description:  fmt.Sprintf("Ошибка обновления: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := UpdateResponse{
		UpdateAnswer: "Успех",
		Version:      latestVersion,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// RollbackBackupOWASPHandler выполняет откат правил OWASP CRS к предыдущей версии из бэкапа
func RollbackBackupOWASPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Получает текущую версию для проверки необходимости отката
	currentVersion, err := getCurrentVersion(pathsOS.Path_Setup_OWASP_CRS)
	if err != nil {
		response := RollbackResponse{
			RollbackAnswer: "Ошибка",
			Description:    fmt.Sprintf("Ошибка чтения текущей версии: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	backupFile, err := findLatestBackup()
	if err != nil {
		response := RollbackResponse{
			RollbackAnswer: "Ошибка",
			Description:    "Бэкапа пока ещё нет, он появится после первого обновления.",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	if backupFile == "" {
		response := RollbackResponse{
			RollbackAnswer: "Ошибка",
			Description:    "Бэкапа пока ещё нет, он появится после первого обновления.",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Извлекает версию из имени файла бэкапа
	backupVersion := extractVersionFromBackupFilename(filepath.Base(backupFile))
	if backupVersion == "" {
		response := RollbackResponse{
			RollbackAnswer: "Ошибка",
			Description:    "Не удалось определить версию в бэкапе",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Проверяет, совпадает ли версия бэкапа с текущей версией
	if backupVersion == currentVersion {
		response := RollbackResponse{
			RollbackAnswer: "Ошибка",
			Description:    "Не требуется! Текущая версия совпадает с версией в бэкапе",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	err = restoreBackup(backupFile)
	if err != nil {
		response := RollbackResponse{
			RollbackAnswer: "Ошибка",
			Description:    fmt.Sprintf("Ошибка восстановления: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Получает версию после успешного восстановления
	version, err := getCurrentVersion(pathsOS.Path_Setup_OWASP_CRS)
	if err != nil {
		response := RollbackResponse{
			RollbackAnswer: "Ошибка",
			Description:    fmt.Sprintf("Ошибка чтения версии после восстановления: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response := RollbackResponse{
		RollbackAnswer:  "Успех",
		RollbackVersion: version,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// extractVersionFromBackupFilename извлекает версию из имени файла бэкапа в формате bak_дд.мм.гг(в_чч.мм.сс)_OWASP_CRS_ВЕРСИЯ.7z
func extractVersionFromBackupFilename(filename string) string {
	// Формат имени файла: bak_дд.мм.гг(в_чч.мм.сс)_OWASP_CRS_ВЕРСИЯ.7z
	parts := strings.Split(filename, "_")
	if len(parts) < 5 {
		return ""
	}

	// Извлекает последнюю часть (ВЕРСИЯ.7z)
	versionPart := parts[len(parts)-1]
	version := strings.TrimSuffix(versionPart, ".7z")

	return version
}

// performUpdate выполняет полную последовательность обновления правил OWASP CRS: бэкап, скачивание, распаковка, копирование и перезагрузка WAF
func performUpdate(downloadURL string) error {
	tmpDir := pathsOS.Path_Folder_tmp_OWASP_CRS
	if err := pathsOS.EnsureDir(tmpDir); err != nil {
		return fmt.Errorf("ошибка создания tmp: %v", err)
	}
	defer os.RemoveAll(tmpDir) // Очищает временную директорию после завершения работы

	// Получает текущую версию для именования бэкапа
	currentVersion, err := getCurrentVersion(pathsOS.Path_Setup_OWASP_CRS)
	if err != nil {
		return fmt.Errorf("ошибка чтения текущей версии: %v", err)
	}

	// Формирует имя файла нового бэкапа
	backupFile := filepath.Join(pathsOS.Path_Backup,
		fmt.Sprintf("bak_%s_OWASP_CRS_%s.7z",
			time.Now().Format("02.01.06(в_15.04.05)"), currentVersion))

	// Создаёт директорию для бэкапов, если она не существует
	if err := pathsOS.EnsureDir(pathsOS.Path_Backup); err != nil {
		return fmt.Errorf("ошибка создания директории бэкапов: %v", err)
	}

	// 1. Создает новый бэкап текущих правил
	if err := createBackup(backupFile); err != nil {
		return fmt.Errorf("ошибка создания нового бэкапа: %v", err)
	}
	LogSystem("OWASP CRS: Создан новый бэкап правил CRS: %s", backupFile)

	// 2. Удаляет предыдущие старые бэкапы, оставляя только один свежий
	entries, _ := os.ReadDir(pathsOS.Path_Backup)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "bak_") && strings.HasSuffix(name, ".7z") &&
			name != filepath.Base(backupFile) {

			oldPath := filepath.Join(pathsOS.Path_Backup, name)
			if err := os.Remove(oldPath); err != nil {
				LogError("OWASP CRS: Не удалось удалить старый бэкап %s: %v", oldPath, err)
			} else {
				LogSystem("OWASP CRS: Удалён старый бэкап правил: %s", oldPath)
			}
		}
	}

	// Скачивает архив с новой версией
	archivePath := filepath.Join(tmpDir, "archive.tar.gz")
	if err := downloadFile(downloadURL, archivePath); err != nil {
		restoreBackup(backupFile) // Откатывается к бэкапу в случае ошибки скачивания
		return fmt.Errorf("ошибка скачивания архива: %v", err)
	}

	// Распаковывает архив
	if err := extractTarGz(archivePath, tmpDir); err != nil {
		restoreBackup(backupFile) // Откатывается к бэкапу в случае ошибки распаковки
		return fmt.Errorf("ошибка распаковки архива: %v", err)
	}

	// Находит директорию, созданную после распаковки
	extractedDir, err := findExtractedDir(tmpDir)
	if err != nil {
		restoreBackup(backupFile) // Откатывается к бэкапу в случае ошибки поиска директории
		return fmt.Errorf("ошибка поиска распакованной директории: %v", err)
	}

	// Определяет полные пути для установки
	rulesPath := filepath.Join(pathsOS.Path_Config_Base, pathsOS.Path_Rules_Base)
	confPath := filepath.Join(pathsOS.Path_Config_Base, pathsOS.Path_Setup_Base)

	// Удаляет старые правила и конфигурацию перед установкой новых
	if err := os.RemoveAll(rulesPath); err != nil {
		restoreBackup(backupFile) // Откатывается к бэкапу в случае ошибки удаления
		return fmt.Errorf("ошибка удаления старых правил: %v", err)
	}

	if err := os.Remove(confPath); err != nil {
		restoreBackup(backupFile) // Откатывается к бэкапу в случае ошибки удаления
		return fmt.Errorf("ошибка удаления старой конфигурации: %v", err)
	}

	// Копирует новые правила
	if err := copyDir(filepath.Join(extractedDir, pathsOS.Path_Rules_Base), rulesPath); err != nil {
		restoreBackup(backupFile) // Откатывается к бэкапу в случае ошибки копирования
		return fmt.Errorf("ошибка копирования новых правил: %v", err)
	}
	// Копирует файл конфигурации
	if err := copyFile(filepath.Join(extractedDir, "crs-setup.conf.example"), confPath); err != nil {
		restoreBackup(backupFile) // Откатывается к бэкапу в случае ошибки копирования
		return fmt.Errorf("ошибка копирования новой конфигурации: %v", err)
	}

	// Копирует файл лицензии (некритическая операция)
	licenseSrc := filepath.Join(extractedDir, "LICENSE")
	licenseDst := filepath.Join(rulesPath, "LICENSE")
	if err := copyFile(licenseSrc, licenseDst); err != nil {
		fmt.Printf("Предупреждение: ошибка копирования LICENSE: %v\n", err)
	}

	// Перезагружает Coraza WAF для применения новых правил
	if err := reloadWAF(); err != nil {
		return fmt.Errorf("ошибка перезагрузки WAF: %v", err)
	}

	return nil
}

// normalizeOWASPCRSReleaseURL преобразует обычную GitHub-ссылку на страницу релиза OWASP CRS
func normalizeOWASPCRSReleaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}

	// Если ссылка уже API или явно указывает на api.github.com — возвращает как есть
	if strings.HasPrefix(raw, "https://api.github.com/") || strings.HasPrefix(raw, "http://api.github.com/") {
		return raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	if !strings.EqualFold(u.Host, "github.com") {
		return raw
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 {
		// Ожидает формат /owner/repo/...
		return raw
	}

	owner := parts[0]
	repo := parts[1]
	rest := strings.Join(parts[2:], "/")

	apiURL := url.URL{
		Scheme: u.Scheme,
		Host:   "api.github.com",
		Path:   "/repos/" + owner + "/" + repo + "/" + rest,
	}

	return apiURL.String()
}

// getLatestReleaseInfo получает информацию о последнем стабильном релизе OWASP CRS с GitHub
func getLatestReleaseInfo() (string, string, error) {
	// Получает ссылку из конфига "server.conf" и при необходимости преобразует её в GitHub API URL
	apiURL := normalizeOWASPCRSReleaseURL(pathsOS.URL_OWASP_CRS_LatestRelease)
	resp, err := http.Get(apiURL)

	if err != nil {
		return "", "", fmt.Errorf("не удалось получить данные о релизе: %v", err)
	}
	defer resp.Body.Close()

	// Декодирует JSON-ответ
	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", fmt.Errorf("ошибка декодирования ответа GitHub API: %v", err)
	}

	// Извлекает версию, удаляя префикс "v"
	version := strings.TrimPrefix(release.TagName, "v")

	// Ищет URL для скачивания .tar.gz архива, предпочитая "minimal" версию
	var downloadURL string
	var foundNonMinimal bool

	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, ".tar.gz") && !strings.HasSuffix(asset.Name, ".asc") {
			if strings.Contains(asset.Name, "minimal") {
				downloadURL = asset.DownloadURL
				break // Найдена предпочтительная "minimal" версия
			} else if !foundNonMinimal {
				// Запоминает первый найденный обычный .tar.gz как запасной вариант
				downloadURL = asset.DownloadURL
				foundNonMinimal = true
			}
		}
	}

	// Возвращает ошибку, если подходящий URL для скачивания не найден
	if downloadURL == "" {
		return "", "", fmt.Errorf("не найден .tar.gz архив (ни обычный, ни minimal)")
	}

	return version, downloadURL, nil
}

// getCurrentVersion читает текущую версию из файла конфигурации CRS
func getCurrentVersion(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	lines := 0
	var version string
	scanner := bufio.NewScanner(file)

	// Читает файл построчно, ищет вторую строку, которая содержит версию
	for scanner.Scan() {
		lines++
		if lines == 2 {
			line := scanner.Text()
			// Извлекает версию из строки комментария
			if after, ok := strings.CutPrefix(line, "# OWASP CRS ver."); ok {
				version = after
			}
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	// Возвращает ошибку, если версия не была найдена во второй строке
	if version == "" {
		return "", fmt.Errorf("версия не найдена в файле")
	}

	return version, nil
}

// compareVersions сравнивает две версии в формате "x.y.z" (major.minor.patch)
func compareVersions(v1, v2 string) int {
	// Возвращает -1 если v1 < v2, 1 если v1 > v2, 0 если равны
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	// Сравнивает компоненты версий как числа
	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		p1, _ := strconv.Atoi(parts1[i]) // Предполагает корректный числовой формат
		p2, _ := strconv.Atoi(parts2[i])
		if p1 < p2 {
			return -1
		} else if p1 > p2 {
			return 1
		}
	}

	// Обрабатывает случай, когда версии имеют разное количество компонентов
	if len(parts1) < len(parts2) {
		return -1
	} else if len(parts1) > len(parts2) {
		return 1
	}

	return 0
}

// downloadFile скачивает файл по URL и сохраняет по указанному пути
func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// extractTarGz распаковывает .tar.gz архив в указанную временную директорию
func extractTarGz(gzipFilePath, dest string) error {
	file, err := os.Open(gzipFilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Создает gzip-читатель
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	// Создает tar-читатель
	tarReader := tar.NewReader(gzReader)

	// Читает и распаковывает каждый элемент в архиве
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // Достигнут конец архива
		}
		if err != nil {
			return err
		}

		path := filepath.Join(dest, header.Name)

		// Обрабатывает файлы и директории
		switch header.Typeflag {
		case tar.TypeDir:
			// Создает директорию
			if err := pathsOS.EnsureDir(path); err != nil {
				return err
			}
		case tar.TypeReg:
			// Создает и копирует содержимое файла
			outFile, err := os.Create(path)
			if err != nil {
				return err
			}

			// Копирует содержимое файла из tar-потока
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}

// findExtractedDir находит первую директорию, созданную в процессе распаковки в tmpDir
func findExtractedDir(tmpDir string) (string, error) {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", err
	}

	// Ищет первую вложенную директорию
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(tmpDir, entry.Name()), nil
		}
	}

	return "", fmt.Errorf("распакованная директория не найдена")
}

// createBackup создает .7z архив текущих правил и конфигурации CRS
func createBackup(backupFile string) error {
	// Определяет директорию исполняемого файла
	exeDir, err := os.Executable()
	if err != nil {
		return fmt.Errorf("ошибка получения пути к исполняемому файлу: %w", err)
	}
	exeDir = filepath.Dir(exeDir)

	// Нормализует путь к директории для бэкапов, делая его абсолютным
	backupDir := pathsOS.Path_Backup
	if !filepath.IsAbs(backupDir) {
		backupDir = filepath.Join(exeDir, backupDir)
	}
	if err := pathsOS.EnsureDir(backupDir); err != nil {
		return fmt.Errorf("ошибка создания директории бэкапов %s: %v", backupDir, err)
	}

	// Формирует полный абсолютный путь к файлу бэкапа
	absBackupFile := filepath.Join(backupDir, filepath.Base(backupFile))

	// Определяет относительные пути к файлам и папкам, которые нужно добавить в архив, относительно рабочей директории
	relativeConfPath := pathsOS.Path_Setup_Base
	relativeRulesPath := pathsOS.Path_Rules_Base

	// Получает абсолютный путь к утилите 7-Zip
	absPath7z, err := pathsOS.Resolve7zip()
	if err != nil {
		return fmt.Errorf("не удалось найти 7-Zip: %v", err)
	}

	// Создает команду для 7-Zip (a: добавить, -t7z: тип, -mx=9: сжатие)
	cmd := exec.Command(absPath7z, "a", "-t7z", "-mx=9", absBackupFile, relativeConfPath, relativeRulesPath)

	// Устанавливает рабочую директорию, чтобы пути в архиве были относительными от Path_Config_Base
	cmd.Dir = pathsOS.Path_Config_Base

	// Запускает команду и получает ее объединенный вывод
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ошибка создания .7z архива: %v, вывод: %s", err, output)
	}

	return nil
}

// restoreBackup восстанавливает правила и конфигурацию из .7z архива
func restoreBackup(backupFile string) error {
	// Определяет полные пути для удаления старых данных
	rulesPath := filepath.Join(pathsOS.Path_Config_Base, pathsOS.Path_Rules_Base)
	confPath := filepath.Join(pathsOS.Path_Config_Base, pathsOS.Path_Setup_Base)

	// Удаляет существующие правила и конфигурацию, если они существуют
	if _, err := os.Stat(rulesPath); err == nil {
		if err := os.RemoveAll(rulesPath); err != nil {
			return fmt.Errorf("ошибка удаления '%s' перед восстановлением: %v", pathsOS.Path_Rules_Base, err)
		}
	}
	if _, err := os.Stat(confPath); err == nil {
		if err := os.Remove(confPath); err != nil {
			return fmt.Errorf("ошибка удаления '%s' перед восстановлением: %v", pathsOS.Path_Setup_Base, err)
		}
	}

	// Получает абсолютный путь к утилите 7-Zip
	abs7, err := pathsOS.Resolve7zip()
	if err != nil {
		return fmt.Errorf("не удалось найти 7-Zip: %v", err)
	}

	// Создает команду для распаковки (x: извлечь, -o: директория назначения, -y: подтвердить перезапись)
	cmd := exec.Command(abs7, "x", backupFile, "-o"+pathsOS.Path_Config_Base, "-y")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ошибка восстановления бэкапа: %v, вывод: %s", err, output)
	}

	// Перезагружает WAF для применения восстановленных правил
	if err := reloadWAF(); err != nil {
		return fmt.Errorf("ошибка перезагрузки WAF: %v", err)
	}

	return nil
}

// findLatestBackup находит путь к самому новому (единственному) .7z бэкапу правил CRS
func findLatestBackup() (string, error) {
	entries, err := os.ReadDir(pathsOS.Path_Backup)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения директории %s: %v", pathsOS.Path_Backup, err)
	}

	var backups []string

	// Собирает список файлов бэкапов CRS
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "bak_") && strings.HasSuffix(entry.Name(), ".7z") {
			backups = append(backups, entry.Name())
		}
	}

	// Возвращает пустую строку, если бэкапов нет
	if len(backups) == 0 {
		return "", nil
	}

	// Сортирует по имени файла в обратном лексикографическом порядке, так как имя содержит временную метку
	sort.Slice(backups, func(i, j int) bool {
		return backups[i] > backups[j] // Сравнение строк в обратном лексикографическом порядке
	})

	// Возвращает путь к самому новому бэкапу
	return filepath.Join(pathsOS.Path_Backup, backups[0]), nil
}

// copyFile копирует содержимое файла из src в dst
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

// copyDir рекурсивно копирует содержимое директории src в dst
func copyDir(src, dst string) error {
	// Использует filepath.Walk для рекурсивного обхода исходной директории
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Вычисляет относительный путь для сохранения структуры директории
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Формирует путь назначения
		destPath := filepath.Join(dst, relPath)

		// Создает директорию, если это директория
		if info.IsDir() {
			return pathsOS.EnsureDir(destPath)
		}

		// Копирует файл
		return copyFile(path, destPath)
	})
}

// reloadWAF перезагружает Coraza WAF с текущей конфигурацией
func reloadWAF() error {
	// Создает новую конфигурацию WAF, используя директиву Include из server.conf
	waf, err := coraza.NewWAF(coraza.NewWAFConfig().WithDirectives(fmt.Sprintf("Include %s", pathsOS.Path_Config_Coraza)))
	if err != nil {
		return err
	}

	wafMutex.Lock()
	defer wafMutex.Unlock()
	currentWAF = waf // Заменяет старый экземпляр новым
	return nil
}

// GetCurrentWAF возвращает текущий экземпляр Coraza WAF, обеспечивая безопасный доступ
func GetCurrentWAF() coraza.WAF {
	wafMutex.RLock()
	defer wafMutex.RUnlock()
	return currentWAF
}

// InitializeWAFWithRecovery инициализирует Coraza WAF, выполняя откат из бэкапа в случае неудачи
func InitializeWAFWithRecovery() error {
	// Первая попытка инициализации
	if err := reloadWAF(); err == nil {
		return nil
	} else {
		LogError("OWASP CRS: Первая попытка инициализации WAF не удалась: %v", err)
	}

	// Вторая попытка инициализации
	time.Sleep(1 * time.Second) // Дает краткое время для стабилизации системы
	if err := reloadWAF(); err == nil {
		return nil
	} else {
		LogError("OWASP CRS: Вторая попытка инициализации WAF не удалась: %v", err)
	}

	// Поиск последнего бэкапа для отката
	backupFile, err := findLatestBackup()
	if err != nil {
		return fmt.Errorf("ошибка поиска бэкапа: %v", err)
	}
	if backupFile == "" {
		return fmt.Errorf("бэкапы для отката не найдены")
	}

	LogSystem("OWASP CRS: Восстанавливаем правила из бэкапа: %s", backupFile)
	if err := restoreBackup(backupFile); err != nil {
		return fmt.Errorf("ошибка отката из бэкапа: %v", err)
	}

	// Последняя попытка инициализации после отката
	time.Sleep(1 * time.Second) // Дает время для применения восстановленных файлов
	return reloadWAF()
}
