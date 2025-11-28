// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// SetOwnerAndPerms на Windows выполняет только os.Chmod
func setOwnerAndPerms(path string, perm os.FileMode) {
	_ = os.Chmod(path, perm)
}

// EnsureDirAllAndSetOwner является заглушкой для Windows
func ensureDirAllAndSetOwner(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// scheduleSelfUpdate запускает скрытый CMD процесс, который ждет завершения текущего PID, а затем заменяет oldExe на newExe (move /y)
func scheduleSelfUpdate(newExe, oldExe string) {
	pid := os.Getpid()

	// Формирование команды: ждёт PID, если PID нет -> перемещает new поверх old, "move /y" перезаписывает целевой файл
	cleanCmd := fmt.Sprintf(`move /y "%s" "%s"`, newExe, oldExe)

	// Ждёт завершения процесса (PID) в цикле (раз в секунду), затем выполняет подмену
	cmdLine := fmt.Sprintf(
		`cmd /C "for /l %%i in (0,0,1) do (timeout /t 1 /nobreak >nul & tasklist /fi "PID eq %d" | findstr %d >nul || (%s & exit))"`,
		pid, pid, cleanCmd,
	)

	// Настраивает параметры для скрытого запуска процесса через WinAPI
	si := &syscall.StartupInfo{Cb: uint32(unsafe.Sizeof(syscall.StartupInfo{})), Flags: 0x1, ShowWindow: 0}
	pi := &syscall.ProcessInformation{}
	cmdLinePtr, _ := syscall.UTF16PtrFromString(cmdLine)

	const CREATE_NO_WINDOW = 0x08000000 // Запускает процесс без создания окна
	syscall.CreateProcess(nil, cmdLinePtr, nil, nil, false, CREATE_NO_WINDOW, nil, nil, si, pi)
	syscall.CloseHandle(pi.Process) // Освобождает ресурсы, не дожидаясь завершения
	syscall.CloseHandle(pi.Thread)
}
