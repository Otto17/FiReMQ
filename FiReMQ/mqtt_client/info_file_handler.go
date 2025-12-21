// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package mqtt_client

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"FiReMQ/db"         // Локальный пакет с БД BadgerDB
	"FiReMQ/logging"    // Локальный пакет с логированием в HTML файл
	"FiReMQ/pathsOS"    // Локальный пакет с путями для разных платформ
	"FiReMQ/protection" // Локальный пакет с функциями базовой защиты

	"github.com/dgraph-io/badger/v4"
)

// TempReport хранит путь к распакованному отчёту и метаданные
type tempReport struct {
	FilePath  string    // Путь к распакованному HTML-файлу
	TempDir   string    // Временная папка для распаковки (будет очищена)
	ClientID  string    // Идентификатор клиента для заголовка
	Prefix    string    // Префикс отчёта (Lite_ или Aida_)
	Expires   time.Time // Время истечения срока действия ссылки (TTL)
	SessionID string    // Идентификатор сессии для привязки
	Login     string    // Логин пользователя для привязки
}

// TempReports хранит метаданные одноразовых ссылок на отчёты
var (
	tempReports = make(map[string]tempReport) // Идентификатор к структуре отчёта
	// TempReportsMu используется для защиты доступа к tempReports
	tempReportsMu sync.Mutex
)

// GenUUID создаёт случайный 128-битный идентификатор для одноразовой ссылки
func genUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ReportTemplate определяет HTML-шаблон для страницы просмотра отчёта
const reportTemplate = `
<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="UTF-8">
<title>{{.Prefix}}{{.ClientID}}.html</title>
<link rel="stylesheet" href="/css/info-viewer.css">
</head>
<body data-client-id="{{.ClientID}}" data-prefix="{{.Prefix}}">
<button class="download-btn" id="downloadBtn">Скачать отчёт</button>
<div>
{{.HTMLContent}}
</div>
<script src="/js/csrf.js"></script>
<script src="/js/info-viewer.js"></script>
</body>
</html>`

// HandleClientInfoFileRequest обрабатывает запросы на просмотр (view) или скачивание (download) информационных отчётов
func HandleClientInfoFileRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "Неподдерживаемый Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	// Парсинг входящего JSON, запрещает неизвестные поля
	var req struct {
		ClientID string `json:"clientID"`
		Prefix   string `json:"prefix"`
		Action   string `json:"action"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // Обеспечивает строгий парсинг без неизвестных полей
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "Некорректный JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Получает логин и SID из куки, так как они обязательны для авторизации
	login, sid, err := getLoginAndSessionIDFromCookie(r)
	if err != nil {
		http.Error(w, "Не авторизованы", http.StatusUnauthorized)
		return
	}

	// Проверяет обязательные поля запроса
	if req.ClientID == "" || req.Prefix == "" {
		http.Error(w, "Отсутствует clientID или prefix", http.StatusBadRequest)
		return
	}
	if req.Prefix != "Lite_" && req.Prefix != "Aida_" {
		http.Error(w, "Некорректный prefix: "+req.Prefix, http.StatusBadRequest)
		return
	}
	if !clientExists(req.ClientID) {
		http.Error(w, "Некорректный clientID", http.StatusBadRequest)
		return
	}

	switch req.Action {
	case "view":
		// Распаковывает архив, чтобы получить доступ к HTML-файлу
		unpackedFile, tempDir, err := unpackReportArchive(req.ClientID, req.Prefix)
		if err != nil {
			logging.LogError("MQTT Info_Files: Ошибка обработки архива отчёта клиента %s: %v", req.ClientID, err)
			if strings.Contains(err.Error(), "не найден") {
				http.Error(w, err.Error(), http.StatusNotFound)
			} else {
				http.Error(w, "Внутренняя ошибка сервера при обработке файла", http.StatusInternalServerError)
			}
			return
		}

		// Регистрирует ссылку в памяти для одноразового доступа
		id := genUUID()
		tr := tempReport{
			FilePath:  unpackedFile,
			TempDir:   tempDir,
			ClientID:  req.ClientID,
			Prefix:    req.Prefix,
			Expires:   time.Now().Add(5 * time.Second), // Короткий TTL
			SessionID: sid,
			Login:     login,
		}

		tempReportsMu.Lock()
		tempReports[id] = tr
		tempReportsMu.Unlock()

		// Очищает временные файлы и запись, если ссылка не была использована в течение 5 секунд
		go func(id string, dir string) {
			time.Sleep(5 * time.Second)
			tempReportsMu.Lock()
			_, still := tempReports[id]
			if still {
				delete(tempReports, id)
			}
			tempReportsMu.Unlock()
			if still {
				_ = os.RemoveAll(dir)
			}
		}(id, tempDir)

		// Возвращает временный URL клиенту
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"reportURL": "/report-view/" + id,
		})
		return

	case "download":
		// Обрабатывает запрос на прямое скачивание
		unpackedFile, tempDir, err := unpackReportArchive(req.ClientID, req.Prefix)
		if err != nil {
			logging.LogError("MQTT Info_Files: Ошибка обработки архива отчёта клиента %s: %v", req.ClientID, err)
			if strings.Contains(err.Error(), "не найден") {
				http.Error(w, err.Error(), http.StatusNotFound)
			} else {
				http.Error(w, "Внутренняя ошибка сервера при обработке файла", http.StatusInternalServerError)
			}
			return
		}
		defer os.RemoveAll(tempDir) // Временная папка должна быть удалена сразу после скачивания

		fileName := req.Prefix + req.ClientID + ".html"
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, unpackedFile)
		return

	default:
		http.Error(w, "Неизвестное действие: "+req.Action, http.StatusBadRequest)
		return
	}
}

// ReportViewHandler обрабатывает запрос GET по одноразовой ссылке, показывает отчёт и удаляет временные файлы
func ReportViewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/report-view/")
	if id == "" {
		http.Error(w, "Некорректная ссылка", http.StatusBadRequest)
		return
	}

	// Извлекает запись для проверки TTL до блокировки сессии
	tempReportsMu.Lock()
	tr, ok := tempReports[id]
	if ok && time.Now().After(tr.Expires) {
		delete(tempReports, id)
		tempReportsMu.Unlock()
		_ = os.RemoveAll(tr.TempDir)
		http.Error(w, "Ссылка устарела", http.StatusNotFound)
		return
	}
	tempReportsMu.Unlock()

	if !ok {
		http.Error(w, "Ссылка устарела", http.StatusNotFound)
		return
	}

	// Гарантирует, что ссылку использует только её владелец
	login, sid, err := getLoginAndSessionIDFromCookie(r)
	if err != nil || sid == "" || login == "" {
		http.Error(w, "Не авторизованы", http.StatusUnauthorized)
		return
	}
	if sid != tr.SessionID || login != tr.Login {
		http.Error(w, "Ссылка недействительна для этой сессии", http.StatusForbidden)
		return
	}

	// Удаляет запись из хранилища, так как она одноразовая
	tempReportsMu.Lock()
	delete(tempReports, id)
	tempReportsMu.Unlock()

	// Читает файл перед ответом
	htmlContent, err := os.ReadFile(tr.FilePath)
	if err != nil {
		logging.LogError("MQTT Info_Files: Ошибка чтения файла отчёта %s: %v", tr.FilePath, err)
		_ = os.RemoveAll(tr.TempDir)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tr.TempDir) // Временная папка должна быть удалена сразу после использования

	// Устанавливает строгую политику CSP для защиты страницы
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; "+
			"script-src 'self'; "+
			"script-src-attr 'none'; "+
			"style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data:; "+
			"font-src 'self' data:; "+
			"connect-src 'self'; "+
			"object-src 'none'; "+
			"base-uri 'none'; "+
			"form-action 'none'; "+
			"frame-src 'none'; "+
			"frame-ancestors 'none'; "+
			"block-all-mixed-content;")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Вставляет сырой HTML, используя template.HTML, так как CSP обеспечивает защиту
	data := struct {
		ClientID    string
		Prefix      string
		HTMLContent template.HTML
	}{
		ClientID:    tr.ClientID,
		Prefix:      tr.Prefix,
		HTMLContent: template.HTML(htmlContent),
	}

	tmpl, err := template.New("report").Parse(reportTemplate)
	if err != nil {
		http.Error(w, "Ошибка создания шаблона", http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "Ошибка выполнения шаблона", http.StatusInternalServerError)
		return
	}
}

// UnpackReportArchive распаковывает архив `<prefix><clientID>.html.xz` во временную директорию и возвращает путь к HTML-файлу
func unpackReportArchive(clientID, prefix string) (unpackedFilePath, tempDir string, err error) {
	fileKey := prefix + clientID
	archivePath := filepath.Join(pathsOS.Path_Info, fileKey+".html.xz")

	// Проверяет наличие архива перед началом работы
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("архив %s не найден", fileKey)
	}

	// Определяет путь к исполняемому файлу 7z
	sevenZipPath, err := pathsOS.Resolve7zip()
	if err != nil {
		return "", "", err
	}

	// Создает базовую директорию для временных файлов
	unpackBaseDir := filepath.Join(pathsOS.Path_Info, "tmp_unpack")
	if err := pathsOS.EnsureDir(unpackBaseDir); err != nil {
		return "", "", fmt.Errorf("не удалось создать базовую директорию для распаковки: %w", err)
	}

	tempDir, err = os.MkdirTemp(unpackBaseDir, "report-")
	if err != nil {
		return "", "", fmt.Errorf("ошибка создания временной папки: %w", err)
	}
	cmd := exec.Command(sevenZipPath, "x", archivePath, fmt.Sprintf("-o%s", tempDir), "-y")
	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tempDir)
		return "", "", fmt.Errorf("ошибка при распаковке архива %s: %w, вывод: %s", archivePath, err, string(output))
	}

	// Проверяет, что распаковка прошла успешно и файл существует
	unpackedFileName := strings.TrimSuffix(filepath.Base(archivePath), ".xz")
	unpackedFilePath = filepath.Join(tempDir, unpackedFileName)

	if _, err := os.Stat(unpackedFilePath); os.IsNotExist(err) {
		os.RemoveAll(tempDir)
		return "", "", fmt.Errorf("распакованный файл %s не найден после распаковки", unpackedFilePath)
	}

	return unpackedFilePath, tempDir, nil
}

// ClientExists проверяет наличие заданного clientID в базе данных BadgerDB
func clientExists(clientID string) bool {
	if clientID == "" {
		return false
	}

	var exists bool
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("client:" + clientID))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				exists = false
				return nil
			}
			return err
		}

		// Проверяет, что ключ существует и данные внутри соответствуют ClientID
		return item.Value(func(val []byte) error {
			var data map[string]string
			if json.Unmarshal(val, &data) == nil && data["client_id"] == clientID {
				exists = true
			}
			return nil
		})
	})

	if err != nil {
		logging.LogError("Ошибка проверки существования клиента %s в БД: %v", clientID, err)
		return false
	}
	return exists
}

// GetLoginAndSessionIDFromCookie извлекает логин и ID сессии из куки `session_id`, расшифровывая логин
func getLoginAndSessionIDFromCookie(r *http.Request) (login string, sessionID string, err error) {
	// Извлекает куку
	c, err := r.Cookie("session_id")
	if err != nil {
		return "", "", err
	}

	// Разделяет зашифрованный логин и токен сессии
	parts := strings.Split(c.Value, "|")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("неверный формат куки")
	}

	// Расшифровывает логин для проверки прав доступа
	encLogin := parts[0]
	login, err = protection.DecryptLogin(encLogin)
	if err != nil {
		return "", "", err
	}

	return login, parts[1], nil
}
