// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

//go:build linux

package main

import (
	"log"
	"os"
	"os/user"
	"strconv"
)

var (
	firemqUID = -1 // UID пользователя firemq для установки прав
	firemqGID = -1 // GID пользователя firemq для установки прав
)

func init() {
	// Добавляет логирование для отслеживания инициализации
	log.Println("DEBUG: permissions_linux.go init() стартовал.")

	u, err := user.Lookup("firemq")
	if err != nil {
		log.Printf("ПРЕДУПРЕЖДЕНИЕ: Не удалось найти пользователя 'firemq': %v. Владелец файлов не будет изменен.", err)
		return
	}

	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		log.Printf("ПРЕДУПРЕЖДЕНИЕ: Некорректный UID для пользователя 'firemq': %s", u.Uid)
		return
	}

	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		log.Printf("ПРЕДУПРЕЖДЕНИЕ: Некорректный GID для группы 'firemq': %s", u.Gid)
		return
	}

	firemqUID = uid
	firemqGID = gid
	log.Printf("Пользователь 'firemq' найден (uid=%d, gid=%d). Права будут применены.", firemqUID, firemqGID)
	// Добавляет логирование для отслеживания завершения инициализации
	log.Println("DEBUG: permissions_linux.go init() завершен.")
}

// SetOwnerAndPerms устанавливает владельца firemq:firemq и права доступа для пути
func setOwnerAndPerms(path string, perm os.FileMode) {
	// Добавляет логирование для отслеживания установки прав
	log.Printf("DEBUG: Установка прав %o для %s", perm, path)
	if err := os.Chmod(path, perm); err != nil {
		log.Printf("ПРЕДУПРЕЖДЕНИЕ: Не удалось изменить права доступа для %s на %o: %v", path, perm, err)
	}

	if firemqUID != -1 && firemqGID != -1 {
		// Добавляет логирование для отслеживания установки владельца
		log.Printf("DEBUG: Установка владельца %d:%d для %s", firemqUID, firemqGID, path)
		if err := os.Chown(path, firemqUID, firemqGID); err != nil {
			log.Printf("ПРЕДУПРЕЖДЕНИЕ: Не удалось изменить владельца для %s на firemq:firemq: %v", path, err)
		}
	} else {
		// Пропускает смену владельца, если firemq не определен
		log.Printf("DEBUG: Пропуск смены владельца, т.к. UID/GID для firemq не определены.")
	}
}

// EnsureDirAllAndSetOwner создает директорию и устанавливает для нее владельца и права
func ensureDirAllAndSetOwner(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}
	setOwnerAndPerms(path, perm)
	return nil
}
