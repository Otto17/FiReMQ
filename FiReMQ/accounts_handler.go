// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"FiReMQ/db"         // Локальный пакет с БД BadgerDB
	"FiReMQ/protection" // Локальный пакет с функциями базовой защиты

	"github.com/dgraph-io/badger/v4"
)

// SafeUser представляет безопасное представление учетной записи администратора без хеша пароля
type SafeUser struct {
	Auth_Name        string `json:"auth_name"`
	Auth_Login       string `json:"auth_login"`
	Auth_Date_Create string `json:"date_create"`
	Auth_Date_Change string `json:"date_change"`
}

// AddAdminHandler обрабатывает запросы на добавление новой учетной записи администратора
func AddAdminHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	var newUser struct {
		Auth_Name     string `json:"auth_name"`
		Auth_Login    string `json:"auth_login"`
		Auth_Password string `json:"auth_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&newUser); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	// Подготавливает данные для валидации
	dataToValidate := map[string]string{
		"auth_name":     newUser.Auth_Name,
		"auth_login":    newUser.Auth_Login,
		"auth_password": newUser.Auth_Password,
	}

	// Определяет правила валидации для входных данных
	rules := map[string]protection.ValidationRule{
		// Правила для имени нового администратора
		"auth_name": {
			MinLength:   1, // От 1 до 40 символов
			MaxLength:   40,
			AllowSpaces: true,                // Разрешить пробелы
			FieldName:   "Имя нового админа", // Название поля для возврата сообщения об ошибке
		},
		// Правила для логина нового администратора
		"auth_login": {
			MinLength:   1, // От 1 до 30 символов
			MaxLength:   30,
			AllowSpaces: false,                 // Запретить пробелы
			FieldName:   "Логин нового админа", // Название поля для возврата сообщения об ошибке
		},
		// Правила для пароля нового администратора
		"auth_password": {
			MinLength:   1, // От 1 до 64 символов
			MaxLength:   64,
			AllowSpaces: false,                  // Запретить пробелы
			FieldName:   "Пароль нового админа", // Название поля для возврата сообщения об ошибке
		},
	}

	// Выполняет валидацию и санитизацию входных данных
	sanitized, err := protection.ValidateFields(dataToValidate, rules)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Обновляет данные санитизированными значениями
	newUser.Auth_Name = sanitized["auth_name"]
	newUser.Auth_Login = sanitized["auth_login"]
	newUser.Auth_Password = sanitized["auth_password"]

	// Проверяет, занят ли указанный логин
	_, err = getAdminByLogin(newUser.Auth_Login)
	if err == nil {
		// Возвращает ошибку, если логин уже существует
		http.Error(w, "Логин занят, используйте другой!", http.StatusConflict)
		return
	} else if err != badger.ErrKeyNotFound {
		// Обрабатывает ошибки базы данных, отличные от "ключ не найден"
		http.Error(w, "Ошибка при проверке админов", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	dateCreate := fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)", now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())

	user := User{
		Auth_Name:         newUser.Auth_Name,
		Auth_Login:        newUser.Auth_Login,
		Auth_PasswordHash: protection.HashPassword(newUser.Auth_Password),
		Auth_Date_Create:  dateCreate,
		Auth_Date_Change:  "--.--.--(--:--)",
	}

	if err := saveAdmin(user); err != nil {
		http.Error(w, "Ошибка сохранения админа", http.StatusInternalServerError)
		return
	}

	w.Write([]byte("Админ добавлен"))
}

// DeleteAdminHandler обрабатывает запросы на удаление учетной записи администратора
func DeleteAdminHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	var credentials struct {
		Auth_Login string `json:"auth_login"`
	}

	if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	decodedLogin, err := url.QueryUnescape(credentials.Auth_Login)
	if err != nil {
		http.Error(w, "Ошибка декодирования логина", http.StatusBadRequest)
		return
	}

	// Загружает все учетные записи для проверки количества
	usersMap, err := loadAdminsMap()
	if err != nil {
		http.Error(w, "Ошибка загрузки админов", http.StatusInternalServerError)
		return
	}

	// Предотвращает удаление единственной учетной записи
	if len(usersMap) <= 1 {
		http.Error(w, "Нельзя удалять единственный аккаунт!", http.StatusForbidden)
		return
	}

	// Проверяет, удаляет ли пользователь самого себя
	authInfo, err := getAuthInfoFromRequest(r)
	currentUserLogin := ""
	if err == nil {
		currentUserLogin = authInfo.Login
	}

	// Удаляет запись администратора из базы данных
	key := []byte("auth:" + decodedLogin)
	err = db.DBInstance.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})

	if err != nil {
		http.Error(w, "Ошибка удаления админа", http.StatusInternalServerError)
		return
	}

	// Очищает куки, если был удален текущий авторизованный пользователь
	if currentUserLogin == decodedLogin {
		clearAuthCookie(w)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Ваш аккаунт удалён!"))
		return
	}

	w.Write([]byte("Админ удалён"))
}

// UpdateAdminHandler обрабатывает запросы на изменение данных учетной записи
func UpdateAdminHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Структура содержит только обновляемые поля
	var updateUser struct {
		Auth_Login       string `json:"auth_login"`
		Auth_NewPassword string `json:"auth_new_password"`
		Auth_NewName     string `json:"auth_new_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&updateUser); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	// Подготавливает данные для валидации
	dataToValidate := map[string]string{
		"auth_new_name":     updateUser.Auth_NewName,
		"auth_new_password": updateUser.Auth_NewPassword,
	}

	// Определяет правила валидации для обновляемых полей
	rules := map[string]protection.ValidationRule{
		// Правила для имени администратора
		"auth_new_name": {
			MinLength:   0,                         // Указывает, что поле необязательно
			MaxLength:   40,                        // До 40 символов
			AllowSpaces: true,                      // Разрешить пробелы
			FieldName:   "Имя обновляемого админа", // Название поля для возврата сообщения об ошибке
		},
		// Правила для пароля
		"auth_new_password": {
			MinLength:   0,                            // Указывает, что поле необязательно
			MaxLength:   64,                           // До 64 символов
			AllowSpaces: false,                        // Запретить пробелы
			FieldName:   "Пароль обновляемого админа", // Название поля для возврата сообщения об ошибке
		},
	}

	// Выполняет валидацию и санитизацию входных данных
	sanitized, err := protection.ValidateFields(dataToValidate, rules)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Обновляет данные санитизированными значениями
	updateUser.Auth_NewName = sanitized["auth_new_name"]
	updateUser.Auth_NewPassword = sanitized["auth_new_password"]

	// Требует наличия хотя бы одного поля для обновления
	if updateUser.Auth_NewName == "" && updateUser.Auth_NewPassword == "" {
		http.Error(w, "Необходимо указать хотя бы одно поле для обновления", http.StatusBadRequest)
		return
	}

	// Декодирует логин, извлеченный из тела запроса
	decodedLogin, err := url.QueryUnescape(updateUser.Auth_Login)
	if err != nil {
		http.Error(w, "Ошибка декодирования логина", http.StatusBadRequest)
		return
	}

	// Флаг для отслеживания отсутствия фактических изменений
	nameNotChanged := false

	err = db.DBInstance.Update(func(txn *badger.Txn) error {
		key := []byte("auth:" + decodedLogin)
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		var user User
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &user)
		})
		if err != nil {
			return err
		}

		// Пропускает обновление, если имя не изменилось, и пароль пуст
		if updateUser.Auth_NewName != "" && updateUser.Auth_NewName == user.Auth_Name && updateUser.Auth_NewPassword == "" {
			// Имя точно такое же и пароль пустой — ничего менять
			nameNotChanged = true
			return nil
		}

		// Обновляет хеш пароля, если предоставлен новый пароль
		if updateUser.Auth_NewPassword != "" {
			user.Auth_PasswordHash = protection.HashPassword(updateUser.Auth_NewPassword)
		}

		// Обновляет имя, если оно предоставлено
		if updateUser.Auth_NewName != "" {
			user.Auth_Name = updateUser.Auth_NewName
		}

		// Устанавливает текущую дату и время изменения
		now := time.Now()
		user.Auth_Date_Change = fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)",
			now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())

		userData, err := json.Marshal(user)
		if err != nil {
			return err
		}

		return txn.Set(key, userData)
	})

	if err != nil {
		http.Error(w, "Ошибка обновления админа", http.StatusInternalServerError)
		return
	}

	if nameNotChanged {
		w.Write([]byte("Имя админа совпадает с текущим!"))
		return
	}

	w.Write([]byte("Админ обновлён"))
}

// GetAdminsNamesHandler возвращает безопасный список имен и логинов всех администраторов
func GetAdminsNamesHandler(w http.ResponseWriter, r *http.Request) {
	var safeUsers []SafeUser

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
				safeUsers = append(safeUsers, SafeUser{
					Auth_Name:        user.Auth_Name,
					Auth_Login:       user.Auth_Login,
					Auth_Date_Create: user.Auth_Date_Create,
					Auth_Date_Change: user.Auth_Date_Change,
				})
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		http.Error(w, "Ошибка получения имён", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(safeUsers)
}

// GetAuthNameHandler возвращает имя авторизованного администратора для отображения в веб-интерфейсе
func GetAuthNameHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяет наличие авторизационной куки
	authCookie, err := r.Cookie("auth")
	if err != nil {
		http.Error(w, "Не авторизован", http.StatusUnauthorized)
		return
	}

	// Парсит токен и проверяет его срок действия
	expiration, valid := protection.ParseAuthToken(authCookie.Value)
	if !valid || time.Now().Unix() > expiration {
		http.Error(w, "Не авторизован", http.StatusUnauthorized)
		return
	}

	// Извлекает куку session_id
	sessionCookie, err := r.Cookie("session_id")
	if err != nil {
		http.Error(w, "Не авторизован", http.StatusUnauthorized)
		return
	}

	// Разделяет зашифрованный логин и токен сессии
	parts := strings.Split(sessionCookie.Value, "|")
	if len(parts) != 2 {
		http.Error(w, "Неверный формат куки", http.StatusBadRequest)
		return
	}

	// Расшифровывает логин администратора
	encryptedLogin := parts[0]
	login, err := protection.DecryptLogin(encryptedLogin)
	if err != nil {
		http.Error(w, "Ошибка при расшифровке логина", http.StatusInternalServerError)
		return
	}

	// Загружает все учетные записи администраторов
	users, err := loadAdmins()
	if err != nil {
		http.Error(w, "Ошибка загрузки админов", http.StatusInternalServerError)
		return
	}

	// Ищет администратора по расшифрованному логину
	for _, user := range users {
		if user.Auth_Login == login {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"auth_name": user.Auth_Name,
			})
			return
		}
	}

	http.Error(w, "Админ не найден", http.StatusNotFound)
}
