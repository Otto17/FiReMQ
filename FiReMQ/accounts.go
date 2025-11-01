// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"FiReMQ/db"         // Локальный пакет с БД BadgerDB
	"FiReMQ/protection" // Локальный пакет с функциями базовой защиты

	"github.com/dgraph-io/badger/v4"
)

// User представляет структуру учетной записи администратора
type User struct {
	Auth_Name         string `json:"auth_name"`
	Auth_Login        string `json:"auth_login"`
	Auth_PasswordHash string `json:"auth_password_hash"`
	Auth_Date_Create  string `json:"date_create"`
	Auth_Date_Change  string `json:"date_change"`
	Auth_Session_ID   string `json:"auth_session_id"`
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
func getAdminByLogin(login string) (User, error) {
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
	user, err := getAdminByLogin(login)
	if err != nil {
		return AuthInfo{}, err
	}

	return AuthInfo{
		Login: login,
		Name:  user.Auth_Name,
	}, nil
}
