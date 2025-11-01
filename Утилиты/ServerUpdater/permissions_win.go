// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

//go:build windows

package main

import "os"

// SetOwnerAndPerms на Windows выполняет только os.Chmod
func setOwnerAndPerms(path string, perm os.FileMode) {
	_ = os.Chmod(path, perm)
}

// EnsureDirAllAndSetOwner является заглушкой для Windows
func ensureDirAllAndSetOwner(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
