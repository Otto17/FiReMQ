// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package logging

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"FiReMQ/pathsOS"    // Локальный пакет с путями для разных платформ
	"FiReMQ/protection" // Локальный пакет с функциями базовой защиты
)

const (
	logFileName = "FiReMQ_Logs.html"           // Имя файла для сохранения логов
	footerStr   = "</div></div></body></html>" // Закрывающее HTML-содержимое лог-файла

	consoleTimeFormat = "02.01.2006 15:04:05" // Формат времени для вывода в консоль
	logDateLayout     = "02.01.2006"          // Формат даты для парсинга и записи в HTML лог
)

var (
	logFileMu   sync.Mutex // Мьютекс для безопасного доступа к лог-файлу
	lastLogDate string     // Хранит дату последней записанной строки для вставки разделителя

	tempLogLinks   = make(map[string]tempLogData) // Карта для хранения временных ссылок на логи
	tempLogLinksMu sync.Mutex                     // Мьютекс для защиты tempLogLinks

	logRowRegex = regexp.MustCompile(`class="row type-([^"]+)" data-date="([^"]+)"`) // Регулярное выражение для парсинга строк лога при очистке
	cleanupOnce sync.Once                                                            // Гарантирует, что процедура очистки запускается только один раз
)

// tempLogData представляет данные о временной ссылке на лог-файл
type tempLogData struct {
	TempFilePath string    // Путь к временному файлу лога на диске
	Expires      time.Time // Время, когда ссылка становится недействительной
	SessionID    string    // Идентификатор сессии пользователя, создавшего ссылку
	Login        string    // Логин пользователя, создавшего ссылку
}

// Чистый HTML шаблон (без кнопки скачивания, только фильтры, дата и таблица)
const htmlHeader = `<!DOCTYPE html>
<html lang="ru">
<head>
    <meta charset="UTF-8">
    <title>FiReMQ Logs</title>
    <style>
        :root { --bg: #1e1e1e; --text: #e0e0e0; --accent: #5fbfff; --err: #ff4d4d; --warn: #ffca28; --sys: #4caf50; --sec: #ff6609; --panel: #2d2d2d; --border: #444; }
        body { background: var(--bg); color: var(--text); font-family: Consolas, monospace; margin: 0; padding-top: 140px; overflow-y: scroll; }
        
        #control-panel {
            position: fixed; top: 0; left: 0; right: 0; height: 130px;
            background: var(--panel); border-bottom: 2px solid var(--accent);
            z-index: 1000; padding: 10px; box-shadow: 0 4px 10px rgba(0,0,0,0.5);
            display: flex; flex-direction: column; gap: 10px;
        }
        
        /* Верхняя строка для фильтров и динамическая кнопки скачивания */
        .top-row { display: flex; align-items: center; justify-content: center; position: relative; }

        .filters { display: flex; gap: 10px; justify-content: center; }
        .filter-btn {
            background: #444; color: #fff; border: 1px solid #555; padding: 8px 16px; cursor: pointer; border-radius: 4px; font-weight: bold; transition: 0.2s;
        }
        .filter-btn:hover { background: #555; }
        .filter-btn.active { background: var(--accent); border-color: var(--accent); }
        
        .date-picker-container { display: flex; justify-content: center; align-items: center; gap: 10px; }
        
        input[type="date"] { 
            background: #333; color: white; border: 1px solid #555; padding: 5px; border-radius: 4px; 
            transition: background-color 0.3s; font-family: inherit;
        }
        input[type="date"].input-error {
            background-color: #522 !important; border-color: #f55 !important;
        }
        
		/* Заголовки таблицы */
        .headers { 
            display: grid; grid-template-columns: 120px 100px 1fr; 
            font-weight: bold; background: #333; padding: 5px 10px; 
            border-radius: 4px; text-align: left; align-items: center;
        }
        .msg-header { display: flex; align-items: center; }

		/* Кнопка сортировки */
        .sort-btn {
            background: transparent; border: none; color: #ff0000; cursor: pointer; 
            font-size: 16px; font-weight: bold; padding: 0;
            margin-left: 25px; transition: color 0.2s;
        }
        .sort-btn:hover { color: var(--accent); }

		/* Контейнер логов */
        #log-container { display: flex; flex-direction: column-reverse; padding: 10px; }
        #log-container.sort-asc { flex-direction: column; }

        .row { display: grid; grid-template-columns: 120px 100px 1fr; padding: 4px 10px; border-bottom: 1px solid #333; transition: background 0.2s; }
        .row:hover { background: #333; }
        
        .type-СИСТЕМА { color: var(--sys); }
        .type-ОШИБКА { color: var(--err); background: rgba(255, 77, 77, 0.1); }
        .type-ДЕЙСТВИЕ { color: var(--warn); }
        .type-БЕЗОПАСНОСТЬ { color: var(--sec); }
		.type-ОБНОВЛЕНИЕ { color: var(--accent); }

        .date-separator {
            text-align: center; background: #444; color: #fff; padding: 5px; margin: 10px 0; font-weight: bold; border-radius: 4px;
        }
        .hidden { display: none !important; }

		/* Toast уведомление (всплывашка снизу) */
        #toast {
            visibility: hidden; min-width: 250px; background-color: #333; color: #fff; text-align: center;
            border-radius: 4px; padding: 16px; position: fixed; z-index: 2000; left: 50%; bottom: 30px;
            transform: translateX(-50%); box-shadow: 0 4px 10px rgba(0,0,0,0.5); border: 1px solid #ff4d4d;
            font-size: 16px;
        }
        #toast.show { visibility: visible; animation: fadein 0.5s, fadeout 0.5s 2.5s; }
        @keyframes fadein { from {bottom: 0; opacity: 0;} to {bottom: 30px; opacity: 1;} }
        @keyframes fadeout { from {bottom: 30px; opacity: 1;} to {bottom: 0; opacity: 0;} }
    </style>
    <script>
        document.addEventListener('DOMContentLoaded', () => {
            const container = document.getElementById('log-container');
            const btns = document.querySelectorAll('.filter-btn');
            const sortBtn = document.getElementById('sortBtn');
            const dateInput = document.getElementById('dateJump');

            // Настройка Min/Max дат
            const allRows = container.querySelectorAll('.row');
            if (allRows.length > 0) {
				// Извлечение даты из первого и последнего элемента в файле (0-й элемент - самый старый, последний - самый новый)
                const oldestParts = allRows[0].getAttribute('data-date').split('.'); // ДД, ММ, ГГГГ
                const newestParts = allRows[allRows.length - 1].getAttribute('data-date').split('.');

				// Формат для input type=date: ГГГГ-ММ-ДД
                dateInput.min = oldestParts[2] + '-' + oldestParts[1] + '-' + oldestParts[0];
                dateInput.max = newestParts[2] + '-' + newestParts[1] + '-' + newestParts[0];
            }

			// Функция Toast уведомления
            function showToast(msg) {
                const t = document.getElementById('toast');
                t.textContent = msg;
                t.className = 'show';
                setTimeout(() => { t.className = t.className.replace('show', ''); }, 3000);
            }

            // Фильтрация
            btns.forEach(btn => {
                btn.addEventListener('click', () => {
                    const type = btn.dataset.type;
                    if (type === 'ALL') {
                        btns.forEach(b => b.classList.remove('active'));
                        btn.classList.add('active');
                        container.querySelectorAll('.row').forEach(r => r.classList.remove('hidden'));
                    } else {
                        if (btn.classList.contains('active')) {
                            document.querySelector('[data-type="ALL"]').click(); return;
                        }
                        btns.forEach(b => b.classList.remove('active'));
                        btn.classList.add('active');
                        container.querySelectorAll('.row').forEach(r => {
                            if (r.classList.contains('type-' + type)) r.classList.remove('hidden');
                            else r.classList.add('hidden');
                        });
                    }
                });
            });

            // Сортировка
            sortBtn.addEventListener('click', () => {
                const isAsc = container.classList.toggle('sort-asc');
                sortBtn.textContent = isAsc ? '▼' : '▲';
                sortBtn.title = isAsc ? 'Сначала старые' : 'Сначала новые';
            });

            // Выбор даты
            dateInput.addEventListener('change', () => {
                if (!dateInput.value) return;

                const dateParts = dateInput.value.split('-'); // ГГГГ, ММ, ДД
                const searchDate = dateParts[2] + '.' + dateParts[1] + '.' + dateParts[0]; // ДД.ММ.ГГГГ

                const selector = '.row[data-date="' + searchDate + '"]';
                const elements = container.querySelectorAll(selector);
                
                if (elements.length > 0) {
                    const isAsc = container.classList.contains('sort-asc');
                    const target = isAsc ? elements[0] : elements[elements.length - 1];

                    target.scrollIntoView({ behavior: 'smooth', block: 'center' });
                    target.style.background = '#555';
                    setTimeout(() => target.style.background = '', 2000);
                } else {
                    dateInput.classList.add('input-error');
                    setTimeout(() => { dateInput.classList.remove('input-error'); }, 1000);
                    showToast('Записей за ' + searchDate + ' не найдено');
                }
            });
        });
    </script>
</head>
<body>
<!-- Уведомление -->
    <div id="toast"></div>

    <div id="control-panel">
        <div class="top-row">
            <div class="filters">
                <button class="filter-btn active" data-type="ALL">ВСЕ</button>
                <button class="filter-btn" data-type="СИСТЕМА">СИСТЕМА</button>
                <button class="filter-btn" data-type="ОШИБКА">ОШИБКИ</button>
                <button class="filter-btn" data-type="ДЕЙСТВИЕ">ДЕЙСТВИЯ</button>
                <button class="filter-btn" data-type="БЕЗОПАСНОСТЬ">БЕЗОПАСНОСТЬ</button>
				<button class="filter-btn" data-type="ОБНОВЛЕНИЕ">ОБНОВЛЕНИЯ</button>
            </div>
            <!-- сюда JS добавит кнопку скачивания при онлайн-просмотре -->
        </div>
        
        <div class="date-picker-container">
            <label>Перейти к дате: </label>
            <input type="date" id="dateJump">
        </div>

        <div class="headers">
            <div>ДАТА</div>
            <div>ВРЕМЯ</div>
            <div class="msg-header">
                СООБЩЕНИЕ
                <button id="sortBtn" class="sort-btn" title="Сначала старые">▼</button>
            </div>
        </div>
    </div>
    <div id="main-wrapper">
        <div id="log-container" class="sort-asc">
`

// InitLog инициализирует систему логирования
func InitLog() {
	createLogFileIfNeeded()
	startLogCleanup()
}

// createLogFileIfNeeded создает лог-файл, если он не существует, и добавляет в него базовый HTML
func createLogFileIfNeeded() {
	logPath := filepath.Join(pathsOS.Path_Logs, logFileName)
	if err := pathsOS.EnsureDir(pathsOS.Path_Logs); err != nil {
		// Использует fmt.Printf, так как система логирования может быть еще не готова
		fmt.Printf("Ошибка создания директории логов: %v\n", err)
		return
	}
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		f, err := os.Create(logPath)
		if err != nil {
			fmt.Printf("Ошибка создания HTML лог-файла: %v\n", err)
			return
		}
		defer f.Close()
		// Инициализирует файл базовым HTML
		f.WriteString(htmlHeader + footerStr)
	}
}

// writeLogEntry записывает новую строку лога в HTML файл
func writeLogEntry(level, msg string) {
	logFileMu.Lock()
	defer logFileMu.Unlock()

	logPath := filepath.Join(pathsOS.Path_Logs, logFileName)
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		// Пересоздает файл, если он был удалён вручную
		createLogFileIfNeeded()
		lastLogDate = ""
	}

	f, err := os.OpenFile(logPath, os.O_RDWR, 0644)
	if err != nil {
		fmt.Printf("Ошибка записи в лог: %v\n", err)
		return
	}
	defer f.Close()

	now := time.Now()
	dateStr := now.Format(logDateLayout)
	timeStr := now.Format("15:04:05")

	// Удаляет переносы строк, чтобы строка лога оставалась в одной HTML строке
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", "")

	stat, err := f.Stat()
	if err != nil {
		return
	}
	fileSize := stat.Size()
	offset := int64(len(footerStr))

	if fileSize < offset {
		// Восстанавливает базовую структуру, если файл повреждён
		f.Truncate(0)
		f.WriteString(htmlHeader + footerStr)
		fileSize = int64(len(htmlHeader) + len(footerStr))
	}

	// Перемещает курсор перед footerStr
	_, err = f.Seek(-offset, io.SeekEnd)
	if err != nil {
		return
	}

	// Вставляет разделитель даты, если дата изменилась
	if lastLogDate != "" && lastLogDate != dateStr {
		sep := fmt.Sprintf(`<div class="date-separator">--- %s ---</div>`, dateStr)
		f.WriteString(sep + "\n")
	}
	lastLogDate = dateStr

	rowHTML := fmt.Sprintf(
		`<div class="row type-%s" data-date="%s"><div>%s</div><div>%s</div><div>%s</div></div>`+"\n",
		level, dateStr, dateStr, timeStr, msg,
	)

	if _, err := f.WriteString(rowHTML); err != nil {
		return
	}

	// Завершает запись, добавляя footer обратно
	f.WriteString(footerStr)
}

// startLogCleanup запускает горутину для периодической очистки логов
func startLogCleanup() {
	cleanupOnce.Do(func() {
		go func() {
			performCleanup()
			ticker := time.NewTicker(24 * time.Hour)
			for range ticker.C {
				performCleanup()
			}
		}()
	})
}

// performCleanup выполняет логику очистки лог-файла на основе настроек хранения
func performCleanup() {
	days, err := strconv.Atoi(pathsOS.Logs_Retention_Days)
	if err != nil || days <= 0 {
		// Выход, если настройки хранения некорректны
		return
	}
	minPerType, _ := strconv.Atoi(pathsOS.Logs_Min_Count_Per_Type)

	logPath := filepath.Join(pathsOS.Path_Logs, logFileName)
	logFileMu.Lock()
	defer logFileMu.Unlock()

	f, err := os.Open(logPath)
	if err != nil {
		return
	}

	var lines []string
	scanner := bufio.NewScanner(f)

	// Увеличивает буфер сканера для обработки больших файлов
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	f.Close()

	if len(lines) == 0 {
		return
	}

	// Определяет дату, раньше которой записи считаются устаревшими
	cutoff := time.Now().AddDate(0, 0, -days)

	type parsedRow struct {
		RawLine string
		Type    string
		Date    time.Time
	}
	var logRows []parsedRow

	// Парсит строки, извлекая тип и дату
	for _, line := range lines {
		matches := logRowRegex.FindStringSubmatch(line)
		if len(matches) == 3 {
			logType := matches[1]
			dateStr := matches[2]
			t, err := time.Parse(logDateLayout, dateStr)
			if err != nil {
				// Использует текущее время, если парсинг даты не удался
				t = time.Now()
			}
			logRows = append(logRows, parsedRow{line, logType, t})
		}
	}

	typeCounts := make(map[string]int)

	// Подсчитывает общее количество записей по типам
	for _, row := range logRows {
		typeCounts[row.Type]++
	}

	currentDate := ""
	var buffer bytes.Buffer
	buffer.WriteString(htmlHeader)
	deletedCount := make(map[string]int)

	// Пересобирает файл, исключая устаревшие записи
	for _, row := range logRows {
		keep := false
		if !row.Date.Before(cutoff) {
			// Оставляет запись, если она моложе cutoff
			keep = true
		} else {
			// Проверяет, не достигнет ли счетчик минимально допустимого количества
			remaining := typeCounts[row.Type] - deletedCount[row.Type]
			if remaining <= minPerType {
				keep = true
			}
		}

		if keep {
			rowDateStr := row.Date.Format(logDateLayout)
			if currentDate != "" && currentDate != rowDateStr {
				// Вставляет разделитель даты
				sep := fmt.Sprintf(`<div class="date-separator">--- %s ---</div>`, rowDateStr)
				buffer.WriteString(sep + "\n")
			}
			currentDate = rowDateStr
			buffer.WriteString(row.RawLine + "\n")
		} else {
			deletedCount[row.Type]++
		}
	}
	buffer.WriteString(footerStr)

	// Обновляет последнюю дату после очистки
	lastLogDate = currentDate

	if err := os.WriteFile(logPath, buffer.Bytes(), 0644); err != nil {
		fmt.Printf("Ошибка при перезаписи логов: %v\n", err)
	}
}

// logToConsole выводит сообщение в стандартный вывод с меткой времени
func logToConsole(level, msg string) {
	ts := time.Now().Format(consoleTimeFormat)
	fmt.Printf("%s [%s]: %s\n", ts, level, msg)
}

// LogSystem для событий жизненного цикла сервера (запуск, остановка, конфиг...)
func LogSystem(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	logToConsole("СИСТЕМА", msg)
	writeLogEntry("СИСТЕМА", msg)
}

// LogError для ошибок, сбоев и предупреждений
func LogError(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	logToConsole("ОШИБКА", msg)
	writeLogEntry("ОШИБКА", msg)
}

// LogAction для записи действий админов и операций с клиентами
func LogAction(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	logToConsole("ДЕЙСТВИЕ", msg)
	writeLogEntry("ДЕЙСТВИЕ", msg)
}

// LogSecurity для аудита безопасности (вход, атаки, блокировки WAF)
func LogSecurity(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	logToConsole("БЕЗОПАСНОСТЬ", msg)
	writeLogEntry("БЕЗОПАСНОСТЬ", msg)
}

// LogUpdate для логирования процесса обновлений (FiReMQ и ServerUpdater)
func LogUpdate(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	logToConsole("ОБНОВЛЕНИЕ", msg)
	writeLogEntry("ОБНОВЛЕНИЕ", msg)
}

// --- HTTP ОБРАБОТЧИКИ ---

// HandleLogFileRequest обрабатывает POST-запросы на просмотр или скачивание лог-файла
func HandleLogFileRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	login, sid, err := getLoginAndSessionID(r)
	if err != nil {
		http.Error(w, "Не авторизованы", http.StatusUnauthorized)
		return
	}

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	// СКАЧИВАНИЕ (Download)
	// Отдаёт "чистый" файл сразу, без временных ссылок
	if req.Action == "download" {
		id := genUUID()
		tmpDir := os.TempDir()
		tmpFile := filepath.Join(tmpDir, fmt.Sprintf("firemq_dl_%s.html", id))

		// Копирует текущий лог во временный, чтобы не блокировать основной файл во время передачи
		if err := copyLogFileSafely(tmpFile); err != nil {
			LogError("Ошибка подготовки лога для скачивания: %v", err)
			http.Error(w, "Ошибка подготовки лога для скачивания", http.StatusInternalServerError)
			return
		}
		defer os.Remove(tmpFile)

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="FiReMQ_Logs.html"`)
		http.ServeFile(w, r, tmpFile)
		return
	}

	// ПРОСМОТР (View)
	// Создаёт временную ссылку
	id := genUUID()
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("firemq_view_%s.html", id))

	if err := copyLogFileSafely(tmpFile); err != nil {
		LogError("Ошибка копирования лога для просмотра: %v", err)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}

	// Сохраняет метаданные временной ссылки
	tempLogLinksMu.Lock()
	tempLogLinks[id] = tempLogData{
		TempFilePath: tmpFile,
		Expires:      time.Now().Add(30 * time.Second),
		SessionID:    sid,
		Login:        login,
	}
	tempLogLinksMu.Unlock()

	// Запускает таймер для удаления временного файла и ссылки
	go func(linkID, fPath string) {
		time.Sleep(35 * time.Second)
		tempLogLinksMu.Lock()
		delete(tempLogLinks, linkID)
		tempLogLinksMu.Unlock()
		os.Remove(fPath)
	}(id, tmpFile)

	w.Header().Set("Content-Type", "application/json")

	// Возвращает сгенерированную временную ссылку
	json.NewEncoder(w).Encode(map[string]string{
		"url": "/log-view/" + id,
	})
}

// LogViewHandler обслуживает временные ссылки на лог-файл для просмотра в браузере
func LogViewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/log-view/")

	tempLogLinksMu.Lock()
	data, ok := tempLogLinks[id]
	tempLogLinksMu.Unlock()

	if !ok || time.Now().After(data.Expires) {
		http.Error(w, "Ссылка устарела", http.StatusNotFound)
		return
	}

	login, sid, err := getLoginAndSessionID(r)
	if err != nil || login != data.Login || sid != data.SessionID {
		// Проверяет, соответствует ли текущая сессия сессии, создавшей ссылку
		http.Error(w, "Ссылка недействительна для этой сессии", http.StatusForbidden)
		return
	}

	// Читает временный файл в память
	content, err := os.ReadFile(data.TempFilePath)
	if err != nil {
		http.Error(w, "Ошибка при чтении файла", http.StatusInternalServerError)
		return
	}

	// Удаляет запись из мапы (одноразовая ссылка)
	tempLogLinksMu.Lock()
	delete(tempLogLinks, id)
	tempLogLinksMu.Unlock()
	// Файл удалится сам по таймеру

	// --- ИНЪЕКЦИЯ СКРИПТА ---
	// Вставляет ссылку на скрипт перед закрывающим </body> для динамической работы
	htmlStr := string(content)
	injector := `<script src="/js/log-viewer.js"></script></body>`
	htmlStr = strings.Replace(htmlStr, "</body>", injector, 1)

	// --- ПЕРЕОПРЕДЕЛЕНИЕ CSP ---
	// Разрешает 'unsafe-inline' для стилей и скриптов, поскольку HTML-лог использует встроенный JS/CSS
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; "+
			"script-src 'self' 'unsafe-inline'; "+
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
	w.Write([]byte(htmlStr))
}

// copyLogFileSafely безопасно копирует основной лог-файл во временное место
func copyLogFileSafely(destPath string) error {
	logFileMu.Lock()
	defer logFileMu.Unlock()

	srcPath := filepath.Join(pathsOS.Path_Logs, logFileName)
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("лог-файл ещё не создан")
	}

	source, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer destination.Close()

	// Использует io.Copy для эффективного копирования
	_, err = io.Copy(destination, source)
	return err
}

// genUUID генерирует случайный UUID-подобный идентификатор
func genUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// getLoginAndSessionID извлекает логин и ID сессии из куки
func getLoginAndSessionID(r *http.Request) (string, string, error) {
	c, err := r.Cookie("session_id")
	if err != nil {
		return "", "", err
	}

	parts := strings.Split(c.Value, "|")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("неверный формат куки")
	}

	// Дешифрует логин
	login, err := protection.DecryptLogin(parts[0])
	if err != nil {
		return "", "", err
	}

	return login, parts[1], nil
}
