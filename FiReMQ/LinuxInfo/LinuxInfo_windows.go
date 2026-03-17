// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

//go:build windows

package LinuxInfo

// GetServerInfo возвращает заглушку для Windows
func GetServerInfo() ServerInfo {
	return ServerInfo{
		Available: false,
		Message:   "Недоступно под Windows. Используйте Linux для полноценной работы с FiReMQ.",
	}
}
