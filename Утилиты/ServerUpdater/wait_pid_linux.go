// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

//go:build linux

package main

import (
	"fmt"
	"strconv"
	"syscall"
	"time"
)

// waitPIDExit ожидает завершения процесса по его PID
func waitPIDExit(pidStr string, timeout time.Duration) error {
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return fmt.Errorf("некорректный pid: %q", pidStr)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Использует сигнал 0 для проверки существования процесса
		if err := syscall.Kill(pid, 0); err != nil {
			if err == syscall.ESRCH {
				return nil // Процесса больше нет
			}
			// EPERM означает, что процесс жив, но прав нет
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("ожидание завершения процесса %d превысило %s", pid, timeout)
}
