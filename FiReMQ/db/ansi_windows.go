// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

//go:build windows

package db

import (
	"os"
	"syscall"
	"unsafe"
)

// enableANSI включает поддержку виртуального терминала в Windows 10+
func enableANSI() {
	// Получение дескриптора стандартного вывода
	handle := syscall.Handle(os.Stdout.Fd())

	var mode uint32
	// Загружает динамическую библиотеку "kernel32.dll" для доступа к функциям API
	kernel32 := syscall.NewLazyDLL("kernel32.dll")

	// Получение адреса функций
	procGetConsoleMode := kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode := kernel32.NewProc("SetConsoleMode")

	// Чтение текущего режима
	r1, _, _ := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if r1 == 0 {
		// Выход, так как дальнейшие операции невозможны
		return
	}

	// Флаг для включения обработки виртуального терминала
	const enableVirtualTerminalProcessing = 0x0004
	mode |= enableVirtualTerminalProcessing

	// Установка нового режима
	procSetConsoleMode.Call(uintptr(handle), uintptr(mode))
}
