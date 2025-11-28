// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

//go:build windows

package main

import (
	"fmt"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
)

// waitPIDExit ожидает завершения процесса по его PID
func waitPIDExit(pidStr string, timeout time.Duration) error {
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return fmt.Errorf("некорректный pid: %q", pidStr)
	}

	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// Считает успехом, если процесс уже завершён или нет доступа
		return nil
	}
	defer windows.CloseHandle(h)

	ms := uint32(timeout / time.Millisecond)
	s, err := windows.WaitForSingleObject(h, ms)
	if err != nil {
		return err
	}

	switch s {
	case uint32(windows.WAIT_OBJECT_0):
		// Процесс завершился
		return nil
	case uint32(windows.WAIT_TIMEOUT):
		return fmt.Errorf("ожидание завершения процесса %d превысило %s", pid, timeout)
	default:
		return fmt.Errorf("WaitForSingleObject вернул неожиданный код %d", s)
	}
}
