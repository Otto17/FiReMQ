// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package LinuxInfo

import (
	"encoding/json"
	"net/http"

	"FiReMQ/protection"
)

// ServerInfo основная структура с информацией о сервере
type ServerInfo struct {
	Available   bool              `json:"available"`
	Message     string            `json:"message,omitempty"`
	Server      *ServerSection    `json:"server,omitempty"`
	Temperature []TemperatureInfo `json:"temperature,omitempty"`
	Load        *LoadInfo         `json:"load,omitempty"`
	Memory      *MemoryInfo       `json:"memory,omitempty"`
	Disks       []DiskInfo        `json:"disks,omitempty"`
	Network     *NetworkInfo      `json:"network,omitempty"`
	FiReMQ      *FiReMQInfo       `json:"firemq,omitempty"`
}

// ServerSection содержит общую информацию о сервере
type ServerSection struct {
	Distro     string `json:"os"`          // Название и версия дистрибутива
	Kernel     string `json:"kernel"`      // Версия и архитектура ядра
	Hardware   string `json:"hardware"`    // Модель сервера (самого железа)
	CPU        string `json:"cpu"`         // Процессор
	TotalRAM   string `json:"total_ram"`   // ОЗУ
	ServerTime string `json:"server_time"` // Серверное время, формат: "дд.мм.гггг (ЧЧ:ММ:СС) (UTC +06)"
	Uptime     string `json:"uptime"`      // Время работы сервера
}

// TemperatureInfo содержит данные о температуре сервера
type TemperatureInfo struct {
	Label       string `json:"label"`       // Название датчика
	Temperature string `json:"temperature"` // Значение температуры
}

// LoadInfo содержит информацию о нагрузке системы
type LoadInfo struct {
	Load1Min    string `json:"load_1min"`    // Нагрузка за последнюю минуту, формат: "22.0% (0.44)"
	Load5Min    string `json:"load_5min"`    // Нагрузка за последние 5 минут
	Load15Min   string `json:"load_15min"`   // Нагрузка за последние 15 минут
	CPUStatus1  string `json:"cpu_status1"`  // Кратковременная оценка нагрузки за 1 минуту
	CPUStatus5  string `json:"cpu_status5"`  // Среднесрочная оценка нагрузки за 5 минут
	CPUStatus15 string `json:"cpu_status15"` // Долговременная оценка нагрузки за 15 минут
}

// MemoryInfo содержит информацию об RAM И SWAP
type MemoryInfo struct {
	RAMTotal  string `json:"ram_total"`  // Всего ОЗУ
	RAMUsed   string `json:"ram_used"`   // Использовано ОЗУ
	RAMFree   string `json:"ram_free"`   // Свободно ОЗУ
	SwapTotal string `json:"swap_total"` // Всего SWAP
	SwapUsed  string `json:"swap_used"`  // Использовано SWAP
	SwapFree  string `json:"swap_free"`  // Свободно SWAP
}

// DiskInfo содержит информацию о дисках/разделах
type DiskInfo struct {
	Device      string `json:"device"`       // Диск
	MountPoint  string `json:"mount_point"`  // Точка монтирования
	FSType      string `json:"fs_type"`      // Файловая система
	Total       string `json:"total"`        // Размер
	Available   string `json:"available"`    // Доступно
	Used        string `json:"used"`         // Используется
	UsedPercent string `json:"used_percent"` // % занятого места на диске
}

// NetworkInfo содержит информацию о сети
type NetworkInfo struct {
	Hostname   string          `json:"hostname"`      // Хост
	Gateway    string          `json:"gateway"`       // Шлюз
	DNS        []string        `json:"dns,omitempty"` // Список DNS-серверов
	Interfaces []InterfaceInfo `json:"interfaces"`    // Интерфейсы
}

// InterfaceInfo содержит информацию о сетевом интерфейсе
type InterfaceInfo struct {
	Name   string `json:"name"`           // Интерфейс
	Status string `json:"status"`         // Статус интерфейса
	Speed  string `json:"speed"`          // Скорость интерфейса
	Duplex string `json:"duplex"`         // Дуплекс
	MAC    string `json:"mac,omitempty"`  // MAC-адрес
	IPv4   string `json:"ipv4,omitempty"` // IPv4-адрес
	IPv6   string `json:"ipv6,omitempty"` // IPv6-адрес
}

// FiReMQInfo содержит информацию о директориях и процессе FiReMQ
type FiReMQInfo struct {
	Process     *ProcessInfo    `json:"process_FiReMQ,omitempty"` // Процесс FiReMQ
	Directories []DirectoryInfo `json:"directories"`              // Директории с файлами FiReMQ
}

// ProcessInfo содержит информацию о работающем процессе FiReMQ
type ProcessInfo struct {
	PID      int    `json:"pid"`      // Идентификатор процесса
	Owner    string `json:"owner"`    // Владелец процесса: "имя [UID]"
	Group    string `json:"group"`    // Группа процесса: "имя [GID]"
	Memory   string `json:"memory"`   // Потребление ОЗУ (RSS — размер резидентного набора)
	CPU      string `json:"cpu"`      // Нагрузка на процессор в %
	Priority string `json:"priority"` // Приоритет процесса: "Нормальный [0]"
	Uptime   string `json:"uptime"`   // Время работы процесса
}

// DirectoryInfo содержит информацию о директориях FiReMQ
type DirectoryInfo struct {
	Name string `json:"name"` // Название
	Path string `json:"path"` // Размер
	Size string `json:"size"` // Путь
}

// LinuxInfoHandler обрабатывает POST запрос на получение информации о сервере Linux
func LinuxInfoHandler(w http.ResponseWriter, r *http.Request) {
	// Устанавливает заголовки безопасности
	protection.SetSecurityHeaders(w)

	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Собирает информацию о сервере
	info := GetServerInfo()

	// Отправляет ответ в формате JSON
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		http.Error(w, "Ошибка формирования ответа", http.StatusInternalServerError)
		return
	}
}
