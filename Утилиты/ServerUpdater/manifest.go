// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Action представляет тип операции с файлом
type Action string

const (
	ActUpdate Action = "update"
	ActDelete Action = "delete"
)

// FileItem описывает файл или директорию для обновления
type FileItem struct {
	Src     string // Путь внутри архива относительно FiReMQ/
	DestRel string // Относительный путь к директории EXE_DIR
	Dest    string // Опциональный абсолютный путь с поддержкой макросов
	Action  Action
}

// ConfigItem описывает ключ конфигурации для обновления
type ConfigItem struct {
	Key         string // Имя ключа в server.conf
	Src         string // Путь внутри архива
	DestDefault string // Путь по умолчанию с макросами
	Action      Action
}

// Manifest содержит план обновления
type Manifest struct {
	Version    string
	MinUpdater string
	Files      []FileItem
	Configs    []ConfigItem
}

// parseManifest читает файл update.toml и преобразует его в структуру Manifest
func parseManifest(r io.Reader) (*Manifest, error) {
	dec := toml.NewDecoder(r)

	// Сначала пробует распарсить "как есть" в структуру
	var m Manifest
	if err := dec.Decode(&m); err == nil {
		return &m, nil
	}

	// Если не получилось (например, используется сокращённый синтаксис),
	// пробует декодировать в map[string]any для ручной сборки Manifest
	var raw map[string]any
	dec = toml.NewDecoder(r)
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}

	// Выполняет ручную сборку
	b, _ := json.Marshal(raw)
	var m2 Manifest
	if err := json.Unmarshal(b, &m2); err != nil {
		return nil, err
	}
	return &m2, nil
}

// expandMacros выполняет простую подстановку макросов ${EXE_DIR} и ${CONFIG_DIR}
func expandMacros(s string, exeDir, configDir string) string {
	s = strings.ReplaceAll(s, "${EXE_DIR}", exeDir)
	s = strings.ReplaceAll(s, "${CONFIG_DIR}", configDir)
	return s
}
