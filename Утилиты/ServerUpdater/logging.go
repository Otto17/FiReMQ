// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

//go:build linux

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"html"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

	// TEMP (на 1 релиз): старые текстовые логи для переноса в HTML
	baseLogName = "ServerUpdater.log"
	maxLogFiles = 4
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

	// htmlRowRe парсит существующие row из HTML при миграции,	берёт type, дату и время, сообщение оставляет как есть в RawLine
	htmlRowRe = regexp.MustCompile(`<div class="row type-([^"]+)" data-date="(\d{2}\.\d{2}\.\d{4})"><div>\d{2}\.\d{2}\.\d{4}</div><div>(\d{2}:\d{2}:\d{2})</div><div>`)
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

	// --- TEMP (на 1 релиз): перенос старого ServerUpdater.log в FiReMQ_Logs.html и его удаление ---
	// Важно: делает ДО переключения log.SetOutput, чтобы миграция могла пересобрать HTML без риска потери свежих записей
	if err := migrateLegacyTextLogsToHTML(htmlLogPath, logDir); err != nil {
		log.Printf("ПРЕДУПРЕЖДЕНИЕ: миграция старых логов не выполнена: %v", err)
	}

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

// --- TEMP миграция: ServerUpdater.log -> FiReMQ_Logs.html (вкладка ОБНОВЛЕНИЯ) ---

// rowEntry представляет одну строку лога для миграции
type rowEntry struct {
	T     time.Time // Временная метка записи
	Raw   string    // Готовая HTML-строка .row
	Kind  string    // Тип записи
	Valid bool      // Валидность записи
}

// migrateLegacyTextLogsToHTML переносит старые текстовые логи в HTML формат
// Читает текущий FiReMQ_Logs.html и старые ServerUpdater*.log файлы
// Объединяет и сортирует по времени затем пересобирает HTML с корректными date-separator
// После миграции удаляет старые текстовые логи
func migrateLegacyTextLogsToHTML(htmlPath, logDir string) error {
	legacy := legacyLogFiles(logDir)
	hasAny := false
	for _, p := range legacy {
		if _, err := os.Stat(p); err == nil {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return nil
	}

	// Читает текущий HTML (если есть)
	existingHeader := []byte(defaultHTMLHeader)
	existingRows := make([]rowEntry, 0, 4096)

	if data, err := os.ReadFile(htmlPath); err == nil && len(data) > 0 {
		// Сохраняет всё до строки с <div id="log-container">
		if hdr := extractHeaderPrefix(data); len(hdr) > 0 {
			existingHeader = hdr
		}
		existingRows = append(existingRows, extractRowsFromHTML(data)...)
	}

	// Парсит legacy текстовые логи
	importedRows := make([]rowEntry, 0, 4096)
	for _, lp := range legacy {
		part, err := extractRowsFromLegacyTextLog(lp)
		if err != nil {
			// Не фатально: просто пропускает проблемный файл
			continue
		}
		importedRows = append(importedRows, part...)
	}

	if len(importedRows) == 0 {
		// Если нечего импортировать всё равно удаляет старые файлы
		for _, lp := range legacy {
			_ = os.Remove(lp)
		}
		return nil
	}

	all := make([]rowEntry, 0, len(existingRows)+len(importedRows))
	all = append(all, existingRows...)
	all = append(all, importedRows...)

	// Фильтрует битые записи
	tmp := all[:0]
	for _, e := range all {
		if e.Valid && !e.T.IsZero() && strings.Contains(e.Raw, `class="row`) {
			tmp = append(tmp, e)
		}
	}
	all = tmp

	sort.SliceStable(all, func(i, j int) bool {
		return all[i].T.Before(all[j].T)
	})

	// Пересборка HTML: header + separators + rows + footer
	var buf bytes.Buffer
	buf.Write(existingHeader)

	curDate := ""
	for _, e := range all {
		ds := e.T.Format(logDateLayout)
		if curDate != "" && curDate != ds {
			buf.WriteString(fmt.Sprintf(`<div class="date-separator">--- %s ---</div>`+"\n", ds))
		}
		curDate = ds
		buf.WriteString(strings.TrimRight(e.Raw, "\r\n"))
		buf.WriteByte('\n')
	}

	buf.WriteString(footerStr)

	// Пишет через tmp и atomic replace
	tmpPath := htmlPath + ".tmp_migrate"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := atomicReplace(htmlPath, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	htmlLastDate = curDate // Обновляет lastDate для последующих append

	// Удаляет legacy текстовые логи
	for _, lp := range legacy {
		_ = os.Remove(lp)
	}
	return nil
}

// legacyLogFiles возвращает список старых текстовых файлов логов в порядке от старого к новому
func legacyLogFiles(dir string) []string {
	// Самый старый: _3 затем _2 _1 _0 затем основной ServerUpdater.log (самый свежий)
	// Корректнее при объединении но всё равно сортирует по времени
	files := make([]string, 0, maxLogFiles+1)

	base := filepath.Join(dir, baseLogName)
	ext := filepath.Ext(baseLogName) // ".log"
	name := strings.TrimSuffix(baseLogName, ext)

	for i := maxLogFiles - 1; i >= 0; i-- {
		files = append(files, filepath.Join(dir, fmt.Sprintf("%s_%d%s", name, i, ext)))
	}
	files = append(files, base)
	return files
}

// extractHeaderPrefix извлекает заголовок HTML до log-container
func extractHeaderPrefix(htmlData []byte) []byte {
	marker := []byte(`<div id="log-container">`)
	idx := bytes.Index(htmlData, marker)
	if idx < 0 {
		return nil
	}
	after := htmlData[idx:]
	nl := bytes.IndexByte(after, '\n')
	if nl < 0 {
		// Нет перевода строки — возвращает до конца marker
		return htmlData[:idx+len(marker)]
	}
	return htmlData[:idx+nl+1]
}

// extractRowsFromHTML извлекает существующие строки логов из HTML
func extractRowsFromHTML(htmlData []byte) []rowEntry {
	// Ищет строки которые содержат .row (как в FiReMQ: каждая строка лога — отдельная строка файла)
	var out []rowEntry
	sc := bufio.NewScanner(bytes.NewReader(htmlData))
	// На больших файлах стандартного буфера не хватит
	buf := make([]byte, 0, 128*1024)
	sc.Buffer(buf, 2*1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.Contains(line, `class="row type-`) {
			continue
		}

		m := htmlRowRe.FindStringSubmatch(line)
		if len(m) != 4 {
			continue
		}

		// m[2] = дд.мм.гггг, m[3] = ЧЧ:ММ:СС
		t, err := time.ParseInLocation("02.01.2006 15:04:05", m[2]+" "+m[3], time.Local)
		if err != nil {
			continue
		}

		out = append(out, rowEntry{
			T:     t,
			Raw:   line,
			Kind:  m[1],
			Valid: true,
		})
	}
	return out
}

// extractRowsFromLegacyTextLog парсит старый текстовый лог и конвертирует в HTML формат
func extractRowsFromLegacyTextLog(path string) ([]rowEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []rowEntry
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 128*1024)
	sc.Buffer(buf, 2*1024*1024)

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		t := time.Now()
		msg := line

		if m := stdLogLineRe.FindStringSubmatch(line); len(m) == 3 {
			if tt, err := time.ParseInLocation("2006/01/02 15:04:05", m[1], time.Local); err == nil {
				t = tt
			}
			msg = m[2]
		}

		dateStr := t.Format(logDateLayout)
		timeStr := t.Format("15:04:05")

		row := fmt.Sprintf(
			`<div class="row type-%s" data-date="%s"><div>%s</div><div>%s</div><div>%s</div></div>`,
			"ОБНОВЛЕНИЕ", dateStr, dateStr, timeStr, sanitizeMsg(msg),
		)

		out = append(out, rowEntry{
			T:     t,
			Raw:   row,
			Kind:  "ОБНОВЛЕНИЕ",
			Valid: true,
		})
	}

	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}
