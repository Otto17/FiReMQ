// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var rawLogOutput io.Writer // MultiWriter(os.Stderr, rotatingWriter) для прямой записи без префиксов

const (
	baseLogName = "ServerUpdater.log" // Имя лог‑файла
	maxLogSize  = 1_000_000           // Максимальный размер лог-файла в байтах для ротации (Установлено 1 Мбайт)
	maxLogFiles = 4                   // Максимальное количество архивных лог-файлов для хранения (с префиксами: _0, _1, _2, _3)
)

// rotatingWriter является потокобезопасным писателем с автоматической ротацией лог-файлов
type rotatingWriter struct {
	path string     // Путь к текущему log-файлу
	mu   sync.Mutex // Блокировка для обеспечения атомарности записи и ротации
}

// ServerUpdaterLogging настраивает стандартный логгер на вывод в stderr и файл (с ротацией)
func ServerUpdaterLogging() {
	logDir, logPath := detectLogPath()

	// Создаёт каталог логов (с корректными правами под Linux)
	if err := ensureLogDir(logDir); err != nil {
		// Продолжает работу, используя только stderr, если не удалось создать нужный каталог
		log.Printf("ПРЕДУПРЕЖДЕНИЕ: не удалось подготовить каталог логов %s: %v (лог только в stderr)", logDir, err)
		return
	}

	// Настраивает MultiWriter: stderr + ротационный писатель
	w := newRotatingWriter(logPath)
	rawLogOutput = io.MultiWriter(os.Stderr, w)
	log.SetOutput(rawLogOutput)

	// Фиксирует путь лога
	log.Printf("Лог инициализирован: %s", logPath)
}

// WriteToLogFile является универсальной обёрткой для записи сообщения в лог
func WriteToLogFile(format string, args ...any) {
	log.Printf(format, args...)
}

// newRotatingWriter создаёт новый писатель с ротацией
func newRotatingWriter(path string) *rotatingWriter {
	return &rotatingWriter{path: path}
}

// Write записывает данные в файл и выполняет ротацию при превышении лимита
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	dir := filepath.Dir(w.path)
	_ = ensureLogDir(dir) // Пытается гарантировать существование каталога перед записью

	// Ротация при необходимости
	if needRotate(w.path) {
		_ = rotateLogs(w.path)
	}

	f, err := openLogForAppend(w.path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.Write(p)
}

// detectLogPath определяет каталог и полный путь к лог-файлу (зависит от ОС)
func detectLogPath() (dir, full string) {
	if runtime.GOOS == "linux" {
		dir = "/var/log/firemq" // Стандартное местоположение логов на Linux
		return dir, filepath.Join(dir, baseLogName)
	}

	// Для Windows лог хранится в папке "log", рядом с исполняемым файлом
	exeD, err := exeDir()
	if err != nil {
		// Использует текущую директорию как фоллбек
		exeD = "."
	}
	dir = filepath.Join(exeD, "log")
	return dir, filepath.Join(dir, baseLogName)
}

// ensureLogDir создаёт каталог логов с корректными правами
func ensureLogDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("пустой каталог логов")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Устанавливает минимально безопасные права под Linux для логов
	if runtime.GOOS == "linux" {
		if err := os.Chmod(dir, 0o750); err != nil {
			// Ошибка не фатальна, просто информирует
			log.Printf("ПРЕДУПРЕЖДЕНИЕ: не удалось выставить права 0750 для %s: %v", dir, err)
		}
	}
	return nil
}

// openLogForAppend открывает лог-файл для дозаписи, создаёт его при необходимости
func openLogForAppend(path string) (*os.File, error) {
	if runtime.GOOS == "linux" {
		// Использует права 0640 для безопасности лог-файлов на Linux
		return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

// needRotate проверяет, превышен ли допустимый размер лог-файла
func needRotate(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() >= maxLogSize
}

// rotateLogs выполняет циклическую ротацию лог-файлов
func rotateLogs(basePath string) error {
	dir := filepath.Dir(basePath)
	base := filepath.Base(basePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	// Удаляет самый старый архивный файл (чтобы количество файлов не превышало maxLogFiles)
	oldest := filepath.Join(dir, fmt.Sprintf("%s_%d%s", name, maxLogFiles-1, ext))
	_ = os.Remove(oldest)

	// Сдвигает ServerUpdater_(i-1).log -> ServerUpdater_i.log
	for i := maxLogFiles - 1; i > 0; i-- {
		src := filepath.Join(dir, fmt.Sprintf("%s_%d%s", name, i-1, ext))
		dst := filepath.Join(dir, fmt.Sprintf("%s_%d%s", name, i, ext))
		if _, err := os.Stat(src); err == nil {
			_ = os.Rename(src, dst)
		}
	}

	// Текущий файл становится первым архивом -> _0
	cur := basePath
	dst := filepath.Join(dir, fmt.Sprintf("%s_0%s", name, ext))
	if _, err := os.Stat(cur); err == nil {
		_ = os.Rename(cur, dst)
		// Новый основной файл будет создан при следующей записи
	}
	return nil
}

// LogSpacer записывает N пустых строк без таймстемпа/префикса
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
