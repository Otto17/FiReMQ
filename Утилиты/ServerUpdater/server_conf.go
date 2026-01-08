// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

//go:build linux

package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// defaultServerConfPath определяет путь к файлу server.conf
func defaultServerConfPath(string) string {
	if v := os.Getenv("FIREMQ_SERVER_CONF"); v != "" {
		return v
	}
	return "/etc/firemq/config/server.conf"
}

// loadServerConfMap загружает ключи и значения из server.conf в виде карты
func loadServerConfMap(exeDir string) (pathToConf string, values map[string]string, err error) {
	pathToConf = defaultServerConfPath(exeDir)
	f, err := os.Open(pathToConf)
	if err != nil {
		return pathToConf, map[string]string{}, nil // Файл может отсутствовать, что не является ошибкой
	}
	defer f.Close()

	values = make(map[string]string, 64)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Обрезает комментарии в конце строки
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
			if line == "" {
				continue
			}
		}
		eq := strings.IndexRune(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}
		// Нормализация путей: не трогает URL, преобразует слеши в платформенный формат для остального
		if strings.HasPrefix(strings.ToLower(val), "http://") || strings.HasPrefix(strings.ToLower(val), "https://") {
			// Оставляет как есть
		} else {
			// Заменяет обратные слеши на прямые, удаляет дублирующие слеши
			val = strings.ReplaceAll(val, "\\", "/")
			for strings.Contains(val, "//") {
				val = strings.ReplaceAll(val, "//", "/")
			}
			val = filepath.FromSlash(val)
		}
		values[key] = val
	}
	return pathToConf, values, sc.Err()
}
