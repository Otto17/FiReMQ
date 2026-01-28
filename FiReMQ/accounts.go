// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"FiReMQ/db"         // Локальный пакет с БД BadgerDB
	"FiReMQ/protection" // Локальный пакет с функциями базовой защиты

	"github.com/dgraph-io/badger/v4"
)

// User представляет структуру учетной записи администратора
type User struct {
	Auth_Name                   string   `json:"auth_name"`
	Auth_Login                  string   `json:"auth_login"`
	Auth_PasswordHash           string   `json:"auth_password_hash"`
	Auth_Date_Create            string   `json:"date_create"`
	Auth_Date_Change            string   `json:"date_change"`
	Auth_Session_ID             string   `json:"auth_session_id"`
	Perm_Create                 bool     `json:"perm_create"`                   // Права на создание новых учётных записей
	Perm_Update                 bool     `json:"perm_update"`                   // Права на изменение действующих учётных записей
	Perm_Delete                 bool     `json:"perm_delete"`                   // Права на удаление действующих учётных записей
	Perm_RenameClients          bool     `json:"perm_rename_clients"`           // Права на переименовывание клиентов
	Perm_RenameClientsGroups    []string `json:"perm_rename_clients_groups"`    // Список групп для переименования (пустой = все группы)
	Perm_DeleteClients          bool     `json:"perm_delete_clients"`           // Права на удаление клиентов
	Perm_DeleteClientsGroups    []string `json:"perm_delete_clients_groups"`    // Список групп для удаления (пустой = все группы)
	Perm_MoveClients            bool     `json:"perm_move_clients"`             // Права на перемещение клиентов
	Perm_MoveClientsGroups      []string `json:"perm_move_clients_groups"`      // Список разрешённых групп (пустой = все группы)
	Perm_UninstallAgents        bool     `json:"perm_uninstall_agents"`         // Права на полное удаление FiReAgent
	Perm_TerminalCommands       bool     `json:"perm_terminal_commands"`        // Права на отправку cmd/PowerShell команд
	Perm_TerminalCommandsGroups []string `json:"perm_terminal_commands_groups"` // Список групп для cmd/PowerShell (пустой = все группы)
	Perm_InstallPrograms        bool     `json:"perm_install_programs"`         // Права на установку ПО через QUIC
	Perm_InstallProgramsGroups  []string `json:"perm_install_programs_groups"`  // Список групп для установки ПО (пустой = все группы)
	Perm_SystemSettings         bool     `json:"perm_system_settings"`          // Права на системные настройки (обновление/откат OWASP CRS и FiReMQ, MQTT авторизация)
}

// AuthInfo содержит информацию об авторизованном администраторе
type AuthInfo struct {
	Login string
	Name  string
}

// loadAdmins загружает все учетные записи администраторов из базы данных
func loadAdmins() ([]User, error) {
	var users []User

	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("auth:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var user User
				if err := json.Unmarshal(val, &user); err != nil {
					return err
				}
				users = append(users, user)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	return users, err
}

// saveAdmin сохраняет или обновляет учетную запись администратора в базе данных
func saveAdmin(user User) error {
	userData, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return db.DBInstance.Update(func(txn *badger.Txn) error {
		key := []byte("auth:" + user.Auth_Login)
		// Обновляет запись, если администратор уже существует
		return txn.Set(key, userData)
	})
}

// getAdminByLogin возвращает учетную запись администратора по логину
func GetAdminByLogin(login string) (User, error) {
	var user User
	key := []byte("auth:" + login)
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &user)
		})
	})
	return user, err
}

// loadAdminsMap загружает все учетные записи администраторов в карту, где ключ — логин
func loadAdminsMap() (map[string]User, error) {
	usersMap := make(map[string]User)
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("auth:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			var user User
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &user)
			})
			if err != nil {
				return err
			}
			usersMap[user.Auth_Login] = user
		}
		return nil
	})
	return usersMap, err
}

// getAuthInfoFromRequest извлекает информацию об авторизованном администраторе из запроса
func getAuthInfoFromRequest(r *http.Request) (AuthInfo, error) {
	sessionCookie, err := r.Cookie("session_id")
	if err != nil {
		return AuthInfo{}, err
	}

	parts := strings.Split(sessionCookie.Value, "|")
	if len(parts) != 2 {
		return AuthInfo{}, fmt.Errorf("недопустимый формат файлов cookie")
	}

	encryptedLogin := parts[0]
	login, err := protection.DecryptLogin(encryptedLogin)
	if err != nil {
		return AuthInfo{}, err
	}

	// Извлекает полную запись пользователя по логину
	user, err := GetAdminByLogin(login)
	if err != nil {
		return AuthInfo{}, err
	}

	return AuthInfo{
		Login: login,
		Name:  user.Auth_Name,
	}, nil
}

// countFullPermissionAdmins подсчитывает количество учётных записей с полными правами (все разрешения)
func countFullPermissionAdmins() (int, error) {
	count := 0
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("auth:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(opts.Prefix); it.ValidForPrefix(opts.Prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var user User
				if err := json.Unmarshal(val, &user); err != nil {
					return err
				}

				// Полные права на переименование = флаг true И пустой список групп (все группы разрешены)
				hasRenameFullPerm := user.Perm_RenameClients && len(user.Perm_RenameClientsGroups) == 0

				// Полные права на перемещение = флаг true И пустой список групп (все группы разрешены)
				hasMoveFullPerm := user.Perm_MoveClients && len(user.Perm_MoveClientsGroups) == 0

				// Полные права на удаление клиентов = флаг true И пустой список групп
				hasDeleteFullPerm := user.Perm_DeleteClients && len(user.Perm_DeleteClientsGroups) == 0

				// Полные права на cmd/PowerShell = флаг true И пустой список групп
				hasTerminalFullPerm := user.Perm_TerminalCommands && len(user.Perm_TerminalCommandsGroups) == 0

				// Полные права на установку ПО = флаг true И пустой список групп
				hasInstallFullPerm := user.Perm_InstallPrograms && len(user.Perm_InstallProgramsGroups) == 0

				// Считает только учётки с полными правами
				if user.Perm_Create && user.Perm_Update && user.Perm_Delete && hasRenameFullPerm && hasDeleteFullPerm && hasMoveFullPerm && user.Perm_UninstallAgents && hasTerminalFullPerm && hasInstallFullPerm && user.Perm_SystemSettings {
					count++
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return count, err
}

// hasFullPermissions проверяет, имеет ли пользователь полные права (все разрешения включены)
func hasFullPermissions(user User) bool {
	// Полные права на переименование = флаг true И пустой список групп (все группы разрешены)
	hasRenameFullPerm := user.Perm_RenameClients && len(user.Perm_RenameClientsGroups) == 0

	// Полные права на перемещение = флаг true И пустой список групп
	hasMoveFullPerm := user.Perm_MoveClients && len(user.Perm_MoveClientsGroups) == 0

	// Полные права на удаление клиентов = флаг true И пустой список групп
	hasDeleteFullPerm := user.Perm_DeleteClients && len(user.Perm_DeleteClientsGroups) == 0

	// Полные права на cmd/PowerShell = флаг true И пустой список групп
	hasTerminalFullPerm := user.Perm_TerminalCommands && len(user.Perm_TerminalCommandsGroups) == 0

	// Полные права на установку ПО = флаг true И пустой список групп
	hasInstallFullPerm := user.Perm_InstallPrograms && len(user.Perm_InstallProgramsGroups) == 0
	return user.Perm_Create && user.Perm_Update && user.Perm_Delete && hasRenameFullPerm && hasDeleteFullPerm && hasMoveFullPerm && user.Perm_UninstallAgents && hasTerminalFullPerm && hasInstallFullPerm && user.Perm_SystemSettings
}

// CanMoveToGroup проверяет, может ли пользователь перемещать клиентов в/из указанной группы
func CanMoveToGroup(user User, group string) bool {
	// Если перемещение полностью запрещено
	if !user.Perm_MoveClients {
		return false
	}
	// Если список групп пуст — разрешены все группы
	if len(user.Perm_MoveClientsGroups) == 0 {
		return true
	}
	// Проверяет, есть ли группа в списке разрешённых
	return slices.Contains(user.Perm_MoveClientsGroups, group)
}

// CanMoveBetweenGroups проверяет, может ли пользователь перемещать клиентов между двумя группами
func CanMoveBetweenGroups(user User, fromGroup, toGroup string) bool {
	return CanMoveToGroup(user, fromGroup) && CanMoveToGroup(user, toGroup)
}

// CanDeleteInGroup проверяет, может ли пользователь удалять клиентов в указанной группе
func CanDeleteInGroup(user User, group string) bool {
	// Если удаление полностью запрещено
	if !user.Perm_DeleteClients {
		return false
	}
	// Если список групп пуст - разрешены все группы
	if len(user.Perm_DeleteClientsGroups) == 0 {
		return true
	}
	// Проверяет, есть ли группа в списке разрешённых
	return slices.Contains(user.Perm_DeleteClientsGroups, group)
}

// CanRenameInGroup проверяет, может ли пользователь переименовывать клиентов в указанной группе
func CanRenameInGroup(user User, group string) bool {
	// Если переименование полностью запрещено
	if !user.Perm_RenameClients {
		return false
	}
	// Если список групп пуст — разрешены все группы
	if len(user.Perm_RenameClientsGroups) == 0 {
		return true
	}
	// Проверяет, есть ли группа в списке разрешённых
	return slices.Contains(user.Perm_RenameClientsGroups, group)
}

// CanTerminalCommandInGroup проверяет, может ли пользователь отправлять cmd/PowerShell команды клиентам в указанной группе
func CanTerminalCommandInGroup(user User, group string) bool {
	// Если отправка команд полностью запрещена
	if !user.Perm_TerminalCommands {
		return false
	}
	// Если список групп пуст — разрешены все группы
	if len(user.Perm_TerminalCommandsGroups) == 0 {
		return true
	}
	// Проверяет, есть ли группа в списке разрешённых
	return slices.Contains(user.Perm_TerminalCommandsGroups, group)
}


// CanInstallProgramInGroup проверяет, может ли пользователь устанавливать ПО клиентам в указанной группе
func CanInstallProgramInGroup(user User, group string) bool {
	// Если установка ПО полностью запрещена
	if !user.Perm_InstallPrograms {
		return false
	}
	// Если список групп пуст — разрешены все группы
	if len(user.Perm_InstallProgramsGroups) == 0 {
		return true
	}
	// Проверяет, есть ли группа в списке разрешённых
	return slices.Contains(user.Perm_InstallProgramsGroups, group)
}