// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

//go:build linux

package main

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	rawLogOutput io.Writer // Для LogSpacer: пишет только в stderr
	htmlLogPath  string    // Путь к файлу HTML лога

	htmlMu       sync.Mutex
	htmlLastDate string // ДД.ММ.ГГГГ последней записи в HTML для вставки в date-separator
)

const (
	htmlLogFileName = "FiReMQ_Logs.html" // Имя файла HTML лога

	footerStr     = "</div></div></body></html>" // Должно совпадать с FiReMQ (log.go)
	logDateLayout = "02.01.2006"                 // Формат даты для логов
)

// defaultHTMLHeader содержит минимальный заголовок если "FiReMQ_Logs.html" отсутствует, Если файл уже создан FiReMQ — сохраняет его заголовок при миграции
const defaultHTMLHeader = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="UTF-8">
<title>FiReMQ Logs</title>
<style>
body { background:#1e1e1e; color:#e0e0e0; font-family:Consolas, monospace; margin:0; padding:10px; }
.row { display:grid; grid-template-columns:120px 100px 1fr; padding:4px 10px; border-bottom:1px solid #333; }
.type-ОБНОВЛЕНИЕ { color:#007acc; }
.date-separator { text-align:center; background:#444; color:#fff; padding:5px; margin:10px 0; font-weight:bold; border-radius:4px; }
</style>
</head>
<body>
<div id="main-wrapper">
<div id="log-container">
`

var (
	// Парсит строку стандартного log.LstdFlags: "2006/01/02 15:04:05 message"
	stdLogLineRe = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})\s+(.*)$`)

	// Извлекает последнюю дату из HTML
	htmlDateAttrRe = regexp.MustCompile(`data-date="(\d{2}\.\d{2}\.\d{4})"`)
)

// htmlUpdateWriter пишет логи обновлений в HTML файл
type htmlUpdateWriter struct {
	path string
}

// ServerUpdaterLogging настраивает стандартный log.Logger для записи в stderr и FiReMQ_Logs.html (вкладка ОБНОВЛЕНИЯ)
func ServerUpdaterLogging() {
	rawLogOutput = os.Stderr // Всегда оставляет stderr

	// Определяет путь к HTML логу используя ту же логику каталогов что и для ServerUpdater.log
	logDir, htmlPath := detectHTMLLogPath()

	if err := ensureLogDir(logDir); err != nil {
		// Не фатально: пусть хотя бы stderr работает
		log.Printf("ПРЕДУПРЕЖДЕНИЕ: не удалось подготовить каталог логов %s: %v (лог только в stderr)", logDir, err)
		return
	}

	// Убеждается что HTML лог существует и содержит footerStr
	if err := createHTMLLogFileIfNeeded(htmlPath); err != nil {
		log.Printf("ПРЕДУПРЕЖДЕНИЕ: не удалось подготовить HTML лог %s: %v (лог только в stderr)", htmlPath, err)
		return
	}

	htmlLogPath = htmlPath

	// Инициализирует htmlLastDate по текущему хвосту файла
	htmlLastDate = loadLastDateFromHTML(htmlLogPath)

	// Теперь весь log.Printf автоматически идёт в HTML как ОБНОВЛЕНИЕ
	w := &htmlUpdateWriter{path: htmlLogPath}
	log.SetOutput(io.MultiWriter(os.Stderr, w))

	log.Printf("HTML лог обновлений инициализирован: %s", htmlLogPath)
}

// LogUpdate пишет в вкладку ОБНОВЛЕНИЯ как в FiReMQ (использует log.Printf так как вывод уже направлен в HTML)
func LogUpdate(format string, args ...any) {
	log.Printf(format, args...)
}

// WriteToLogFile оставляет для совместимости с текущим кодом
func WriteToLogFile(format string, args ...any) {
	log.Printf(format, args...)
}

// LogSpacer пишет N пустых строк только в stderr (в HTML пустые строки не нужны)
func LogSpacer(n int) {
	if n <= 0 || rawLogOutput == nil {
		return
	}
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = '\n'
	}
	_, _ = rawLogOutput.Write(buf)
}

// Write получает сформированную строку стандартного logger и парсит timestamp из префикса для записи в HTML как type-ОБНОВЛЕНИЕ
func (w *htmlUpdateWriter) Write(p []byte) (int, error) {
	// log может передать сразу несколько строк
	s := strings.ReplaceAll(string(p), "\r\n", "\n")
	lines := strings.Split(s, "\n")

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		t := time.Now()
		msg := line

		if m := stdLogLineRe.FindStringSubmatch(line); len(m) == 3 {
			// m[1] = "гггг/мм/дд ЧЧ:ММ:СС"
			if tt, err := time.ParseInLocation("2006/01/02 15:04:05", m[1], time.Local); err == nil {
				t = tt
			}
			msg = m[2]
		}

		_ = appendUpdateRowHTML(w.path, t, msg)
	}

	return len(p), nil
}

// detectHTMLLogPath возвращает директорию логов и полный путь к HTML файлу
func detectHTMLLogPath() (dir, full string) {
	dir = "/var/log/firemq"
	return dir, filepath.Join(dir, htmlLogFileName)
}

// ensureLogDir создаёт каталог логов если его нет
func ensureLogDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("пустой каталог логов")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o750)
	return nil
}

// createHTMLLogFileIfNeeded создаёт HTML файл лога если его нет или проверяет наличие footerStr
func createHTMLLogFileIfNeeded(path string) error {
	// Если нет файла — создаёт минимальный валидный HTML с правильным footerStr
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(defaultHTMLHeader+footerStr), 0o644); err != nil {
			return err
		}
		return nil
	}

	// Если файл есть — убеждается что footerStr присутствует в конце или хотя бы в хвосте
	// Если footerStr не найден — дописывает его в конец
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := ensureFooterAndTrimJunk(f); err != nil {
		return err
	}
	return nil
}

// ensureFooterAndTrimJunk проверяет наличие footer и удаляет мусор после него
func ensureFooterAndTrimJunk(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	if size <= 0 {
		// Файл пустой или битый
		if err := f.Truncate(0); err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		_, err = f.WriteString(defaultHTMLHeader + footerStr)
		return err
	}

	// Читает хвост и ищет footerStr
	const tail = 16 * 1024
	start := int64(0)
	if size > tail {
		start = size - tail
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return err
	}
	b, _ := io.ReadAll(f)

	idx := bytes.LastIndex(b, []byte(footerStr))
	if idx >= 0 {
		// Если после footerStr есть мусор — отрезает его
		absEnd := start + int64(idx) + int64(len(footerStr))
		if absEnd != size {
			return f.Truncate(absEnd)
		}
		return nil
	}

	// footerStr не найден — просто дописывает в конец
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	_, err = f.WriteString(footerStr)
	return err
}

// loadLastDateFromHTML извлекает последнюю дату из HTML файла
func loadLastDateFromHTML(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}

	size := info.Size()
	const tail = 256 * 1024
	start := int64(0)
	if size > tail {
		start = size - tail
	}

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	b, _ := io.ReadAll(f)

	ms := htmlDateAttrRe.FindAllSubmatch(b, -1)
	if len(ms) == 0 {
		return ""
	}
	return string(ms[len(ms)-1][1])
}

// sanitizeMsg экранирует HTML и удаляет переводы строк
func sanitizeMsg(msg string) string {
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", "")
	return html.EscapeString(msg)
}

// appendUpdateRowHTML добавляет новую строку лога в HTML файл
func appendUpdateRowHTML(path string, t time.Time, msg string) error {
	htmlMu.Lock()
	defer htmlMu.Unlock()

	// На всякий случай проверяет что путь задан
	if strings.TrimSpace(path) == "" {
		return nil
	}

	// Гарантирует наличие файла и footer
	if err := createHTMLLogFileIfNeeded(path); err != nil {
		// Внутри writer избегает log.Printf чтобы не зациклиться
		return err
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := ensureFooterAndTrimJunk(f); err != nil {
		return err
	}

	// Ищет позицию перед footerStr (в конце файла footerStr гарантирован)
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	off := int64(len(footerStr))
	if size < off {
		// Файл совсем битый
		if err := f.Truncate(0); err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if _, err := f.WriteString(defaultHTMLHeader + footerStr); err != nil {
			return err
		}
		size = int64(len(defaultHTMLHeader) + len(footerStr))
	}

	// Встаёт перед footer
	if _, err := f.Seek(-off, io.SeekEnd); err != nil {
		return err
	}

	dateStr := t.Format(logDateLayout)
	timeStr := t.Format("15:04:05")

	// Вставляет разделитель дат если нужно
	if htmlLastDate != "" && htmlLastDate != dateStr {
		sep := fmt.Sprintf(`<div class="date-separator">--- %s ---</div>`+"\n", dateStr)
		if _, err := f.WriteString(sep); err != nil {
			return err
		}
	}
	if htmlLastDate == "" {
		// Если не было инициализации просто устанавливает
	}
	htmlLastDate = dateStr

	row := fmt.Sprintf(
		`<div class="row type-%s" data-date="%s"><div>%s</div><div>%s</div><div>%s</div></div>`+"\n",
		"ОБНОВЛЕНИЕ", dateStr, dateStr, timeStr, sanitizeMsg(msg),
	)
	if _, err := f.WriteString(row); err != nil {
		return err
	}

	// Возвращает footer обратно
	_, err = f.WriteString(footerStr)
	return err
}
