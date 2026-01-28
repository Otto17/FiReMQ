// Copyright (c) 2025-2026 Otto
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
	"FiReMQ/logging"    // Локальный пакет с логированием в HTML файл
	"FiReMQ/protection" // Локальный пакет с функциями базовой защиты

	"github.com/dgraph-io/badger/v4"
)

// SafeUser представляет безопасное представление учетной записи администратора без хеша пароля
type SafeUser struct {
	Auth_Name                   string   `json:"auth_name"`
	Auth_Login                  string   `json:"auth_login"`
	Auth_Date_Create            string   `json:"date_create"`
	Auth_Date_Change            string   `json:"date_change"`
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

// AddAdminHandler обрабатывает запросы на добавление новой учетной записи администратора
func AddAdminHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	var newUser struct {
		Auth_Name                   string   `json:"auth_name"`
		Auth_Login                  string   `json:"auth_login"`
		Auth_Password               string   `json:"auth_password"`
		Perm_Create                 *bool    `json:"perm_create"`                   // Права на создание новых учётных записей (указатель для проверки наличия)
		Perm_Update                 *bool    `json:"perm_update"`                   // Права на изменение действующих учётных записей
		Perm_Delete                 *bool    `json:"perm_delete"`                   // Права на удаление действующих учётных записей
		Perm_RenameClients          *bool    `json:"perm_rename_clients"`           // Права на переименовывание клиентов
		Perm_RenameClientsGroups    []string `json:"perm_rename_clients_groups"`    // Список групп для переименования (пустой = все группы)
		Perm_DeleteClients          *bool    `json:"perm_delete_clients"`           // Права на удаление клиентов
		Perm_DeleteClientsGroups    []string `json:"perm_delete_clients_groups"`    // Список групп для удаления (пустой = все группы)
		Perm_MoveClients            *bool    `json:"perm_move_clients"`             // Права на перемещение клиентов
		Perm_MoveClientsGroups      []string `json:"perm_move_clients_groups"`      // Список разрешённых групп (пустой = все группы)
		Perm_UninstallAgents        *bool    `json:"perm_uninstall_agents"`         // Права на полное удаление FiReAgent
		Perm_TerminalCommands       *bool    `json:"perm_terminal_commands"`        // Права на отправку cmd/PowerShell команд
		Perm_TerminalCommandsGroups []string `json:"perm_terminal_commands_groups"` // Список групп для cmd/PowerShell (пустой = все группы)
		Perm_InstallPrograms        *bool    `json:"perm_install_programs"`         // Права на установку ПО через QUIC
		Perm_InstallProgramsGroups  []string `json:"perm_install_programs_groups"`  // Список групп для установки ПО (пустой = все группы)
		Perm_SystemSettings         *bool    `json:"perm_system_settings"`          // Права на системные настройки (обновление/откат OWASP CRS и FiReMQ, MQTT авторизация)
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, errs := getAuthInfoFromRequest(r)
	if errs != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	// Проверяет права текущего админа на создание учётных записей
	currentAdmin, err := GetAdminByLogin(authInfo.Login)
	if err != nil {
		http.Error(w, "Ошибка получения данных текущего админа", http.StatusInternalServerError)
		return
	}

	if !currentAdmin.Perm_Create {
		http.Error(w, "У вас нет прав на создание новых учётных записей", http.StatusForbidden)
		return
	}

	if err := json.NewDecoder(r.Body).Decode(&newUser); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	// Проверяет обязательное наличие всех параметров разрешений
	if newUser.Perm_Create == nil || newUser.Perm_Update == nil || newUser.Perm_Delete == nil || newUser.Perm_RenameClients == nil || newUser.Perm_DeleteClients == nil || newUser.Perm_MoveClients == nil || newUser.Perm_UninstallAgents == nil || newUser.Perm_TerminalCommands == nil || newUser.Perm_InstallPrograms == nil || newUser.Perm_SystemSettings == nil {
		http.Error(w, "Необходимо указать все параметры разрешений", http.StatusBadRequest)
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
	_, err = GetAdminByLogin(newUser.Auth_Login)
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
		Auth_Name:                   newUser.Auth_Name,
		Auth_Login:                  newUser.Auth_Login,
		Auth_PasswordHash:           protection.HashPassword(newUser.Auth_Password),
		Auth_Date_Create:            dateCreate,
		Auth_Date_Change:            "--.--.--(--:--)",
		Perm_Create:                 *newUser.Perm_Create, // Разыменовывает указатель
		Perm_Update:                 *newUser.Perm_Update,
		Perm_Delete:                 *newUser.Perm_Delete,
		Perm_RenameClients:          *newUser.Perm_RenameClients,
		Perm_RenameClientsGroups:    newUser.Perm_RenameClientsGroups,
		Perm_DeleteClients:          *newUser.Perm_DeleteClients,
		Perm_DeleteClientsGroups:    newUser.Perm_DeleteClientsGroups,
		Perm_MoveClients:            *newUser.Perm_MoveClients,
		Perm_MoveClientsGroups:      newUser.Perm_MoveClientsGroups,
		Perm_UninstallAgents:        *newUser.Perm_UninstallAgents,
		Perm_TerminalCommands:       *newUser.Perm_TerminalCommands,
		Perm_TerminalCommandsGroups: newUser.Perm_TerminalCommandsGroups,
		Perm_InstallPrograms:        *newUser.Perm_InstallPrograms,
		Perm_InstallProgramsGroups:  newUser.Perm_InstallProgramsGroups,
		Perm_SystemSettings:         *newUser.Perm_SystemSettings,
	}

	if err := saveAdmin(user); err != nil {
		logging.LogError("Аккаунты: Ошибка сохранения нового админа %s: %v", user.Auth_Login, err)
		http.Error(w, "Ошибка сохранения админа", http.StatusInternalServerError)
		return
	}

	// Формирует строку с информацией о разрешённых группах для переименования клиентов
	var renameGroupsInfo string
	if user.Perm_RenameClients {
		if len(user.Perm_RenameClientsGroups) == 0 {
			renameGroupsInfo = "все группы"
		} else {
			renameGroupsInfo = fmt.Sprintf("группы: %v", user.Perm_RenameClientsGroups)
		}
	} else {
		renameGroupsInfo = "запрещено"
	}

	// Формирует строку с информацией о разрешённых группах для перемещения
	var moveGroupsInfo string
	if user.Perm_MoveClients {
		if len(user.Perm_MoveClientsGroups) == 0 {
			moveGroupsInfo = "все группы"
		} else {
			moveGroupsInfo = fmt.Sprintf("группы: %v", user.Perm_MoveClientsGroups)
		}
	} else {
		moveGroupsInfo = "запрещено"
	}

	// Формирует строку с информацией о разрешённых группах для удаления клиентов
	var deleteGroupsInfo string
	if user.Perm_DeleteClients {
		if len(user.Perm_DeleteClientsGroups) == 0 {
			deleteGroupsInfo = "все группы"
		} else {
			deleteGroupsInfo = fmt.Sprintf("группы: %v", user.Perm_DeleteClientsGroups)
		}
	} else {
		deleteGroupsInfo = "запрещено"
	}

	// Формирует строку с информацией о разрешённых группах для cmd/PowerShell команд
	var terminalGroupsInfo string
	if user.Perm_TerminalCommands {
		if len(user.Perm_TerminalCommandsGroups) == 0 {
			terminalGroupsInfo = "все группы"
		} else {
			terminalGroupsInfo = fmt.Sprintf("группы: %v", user.Perm_TerminalCommandsGroups)
		}
	} else {
		terminalGroupsInfo = "запрещено"
	}

	// Формирует строку с информацией о разрешённых группах для установки ПО
	var installGroupsInfo string
	if user.Perm_InstallPrograms {
		if len(user.Perm_InstallProgramsGroups) == 0 {
			installGroupsInfo = "все группы"
		} else {
			installGroupsInfo = fmt.Sprintf("группы: %v", user.Perm_InstallProgramsGroups)
		}
	} else {
		installGroupsInfo = "запрещено"
	}

	logging.LogAction("Аккаунты: Админ \"%s\" (с именем: %s) добавил новую учётную запись: \"%s\" (с именем: %s, ПРАВА: создание уч. записей=%s, обновление уч. записей=%s, удаление уч. записей=%s, переименование клиентов=%s, удаление клиентов=%s, перемещение клиентов=%s, самоудаление FiReAgent=%s, отправка терминальных команд=%s, установка ПО=%s, системные настройки=%s)",
		authInfo.Login, authInfo.Name, user.Auth_Login, user.Auth_Name, permText(user.Perm_Create), permText(user.Perm_Update), permText(user.Perm_Delete), renameGroupsInfo, deleteGroupsInfo, moveGroupsInfo, permText(user.Perm_UninstallAgents), terminalGroupsInfo, installGroupsInfo, permText(user.Perm_SystemSettings))
	w.Write([]byte("Админ добавлен"))
}

// permText преобразует булево право доступа в читаемый текст для логов
func permText(v bool) string {
	if v {
		return "разрешено"
	}
	return "запрещено"
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

	// Получение информации об инициаторе (текущем админе)
	authInfo, errs := getAuthInfoFromRequest(r)
	if errs != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	currentUserLogin := authInfo.Login
	currentUserName := authInfo.Name

	// Получает данные текущего админа для проверки прав
	currentAdmin, err := GetAdminByLogin(currentUserLogin)
	if err != nil {
		http.Error(w, "Ошибка получения данных текущего админа", http.StatusInternalServerError)
		return
	}

	// Проверяет права на удаление (самоудаление разрешено всегда)
	isSelfDelete := currentUserLogin == decodedLogin
	if !isSelfDelete && !currentAdmin.Perm_Delete {
		http.Error(w, "У вас нет прав на удаление учётных записей", http.StatusForbidden)
		return
	}

	// Загружает все учетные записи для проверки их количества
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

	// Получение данных удаляемого админа из карты
	targetUser, exists := usersMap[decodedLogin]
	if !exists {
		http.Error(w, "Пользователь не найден", http.StatusNotFound)
		return
	}
	targetUserName := targetUser.Auth_Name

	// Проверяет, не удаляется ли последняя учётка с полными правами
	if hasFullPermissions(targetUser) {
		fullPermCount, err := countFullPermissionAdmins()
		if err != nil {
			http.Error(w, "Ошибка проверки прав администраторов", http.StatusInternalServerError)
			return
		}
		if fullPermCount <= 1 {
			http.Error(w, "Нельзя удалить последнюю учётную запись с полными правами!", http.StatusForbidden)
			return
		}
	}

	// Удаляет запись администратора из БД
	key := []byte("auth:" + decodedLogin)
	err = db.DBInstance.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})

	if err != nil {
		logging.LogError("Аккаунты: Ошибка удаления админа %s: %v", decodedLogin, err)
		http.Error(w, "Ошибка удаления админа", http.StatusInternalServerError)
		return
	}

	logging.LogAction("Аккаунты: Админ \"%s\" (с именем: %s) удалил учётную запись: \"%s\" (с именем: %s)", currentUserLogin, currentUserName, decodedLogin, targetUserName)

	// Очищает куки, если был удалён текущий авторизованный пользователь (самоудаление)
	if isSelfDelete {
		clearAuthCookie(w)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Ваш аккаунт удалён!"))
		return
	}

	w.Write([]byte("Админ удалён"))
}

// UpdateAdminHandler обрабатывает запросы на обновление данных учетной записи
func UpdateAdminHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, errs := getAuthInfoFromRequest(r)
	if errs != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
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

	// Декодирует логин, извлеченный из тела запроса
	decodedLogin, err := url.QueryUnescape(updateUser.Auth_Login)
	if err != nil {
		http.Error(w, "Ошибка декодирования логина", http.StatusBadRequest)
		return
	}

	// Получает данные текущего админа для проверки прав
	currentAdmin, err := GetAdminByLogin(authInfo.Login)
	if err != nil {
		http.Error(w, "Ошибка получения данных текущего админа", http.StatusInternalServerError)
		return
	}

	// Проверяет права на обновление (обновление себя разрешено всегда)
	isSelfUpdate := authInfo.Login == decodedLogin
	if !isSelfUpdate && !currentAdmin.Perm_Update {
		http.Error(w, "У вас нет прав на обновление других учётных записей", http.StatusForbidden)
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

	// Отслеживание изменений аккаунта
	nameNotChanged := false  // Имя не изменилось (совпадает с текущим)
	passwordChanged := false // Был ли изменен пароль
	nameChanged := false     // Было ли изменено имя

	var oldName string // Для хранения старого имени изменяемого админа

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

		oldName = user.Auth_Name // Получение старого имени админа

		// Пропускает обновление, если имя не изменилось, и пароль пуст
		if updateUser.Auth_NewName != "" && updateUser.Auth_NewName == user.Auth_Name && updateUser.Auth_NewPassword == "" {
			nameNotChanged = true
			return nil
		}

		// Обновляет хеш пароля, если предоставлен новый пароль
		if updateUser.Auth_NewPassword != "" {
			user.Auth_PasswordHash = protection.HashPassword(updateUser.Auth_NewPassword)
			passwordChanged = true
		}

		// Обновляет имя, если оно предоставлено и отличается
		if updateUser.Auth_NewName != "" && updateUser.Auth_NewName != user.Auth_Name {
			user.Auth_Name = updateUser.Auth_NewName
			nameChanged = true
		}

		// Если ничего по факту не изменилось (например, прислали старое имя и пустой пароль)
		if !passwordChanged && !nameChanged {
			// Если пароль пустой, а имя совпадает — это случай nameNotChanged
			if updateUser.Auth_NewPassword == "" {
				nameNotChanged = true
			}
			return nil
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
		logging.LogError("Аккаунты: Ошибка обновления админа %s: %v", decodedLogin, err)
		http.Error(w, "Ошибка обновления админа", http.StatusInternalServerError)
		return
	}

	if nameNotChanged {
		w.Write([]byte("Имя админа совпадает с текущим!"))
		return
	}

	// Формирование сообщения для лога
	var actionMsg string
	initiatorStr := fmt.Sprintf("Админ \"%s\" (с именем: %s)", authInfo.Login, authInfo.Name)

	if passwordChanged && nameChanged {
		actionMsg = fmt.Sprintf("%s обновил пароль и имя учётной записи \"%s\" с \"%s\" на: \"%s\"", initiatorStr, decodedLogin, oldName, updateUser.Auth_NewName)
	} else if nameChanged {
		actionMsg = fmt.Sprintf("%s обновил имя учётной записи \"%s\" с \"%s\" на: \"%s\"", initiatorStr, decodedLogin, oldName, updateUser.Auth_NewName)
	} else if passwordChanged {
		actionMsg = fmt.Sprintf("%s обновил пароль учётной записи \"%s\" (с именем: %s)", initiatorStr, decodedLogin, oldName)
	}

	if actionMsg != "" {
		logging.LogAction("Аккаунты: %s", actionMsg)
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
					Auth_Name:                   user.Auth_Name,
					Auth_Login:                  user.Auth_Login,
					Auth_Date_Create:            user.Auth_Date_Create,
					Auth_Date_Change:            user.Auth_Date_Change,
					Perm_Create:                 user.Perm_Create,
					Perm_Update:                 user.Perm_Update,
					Perm_Delete:                 user.Perm_Delete,
					Perm_RenameClients:          user.Perm_RenameClients,
					Perm_RenameClientsGroups:    user.Perm_RenameClientsGroups,
					Perm_DeleteClients:          user.Perm_DeleteClients,
					Perm_DeleteClientsGroups:    user.Perm_DeleteClientsGroups,
					Perm_MoveClients:            user.Perm_MoveClients,
					Perm_MoveClientsGroups:      user.Perm_MoveClientsGroups,
					Perm_UninstallAgents:        user.Perm_UninstallAgents,
					Perm_TerminalCommands:       user.Perm_TerminalCommands,
					Perm_TerminalCommandsGroups: user.Perm_TerminalCommandsGroups,
					Perm_InstallPrograms:        user.Perm_InstallPrograms,
					Perm_InstallProgramsGroups:  user.Perm_InstallProgramsGroups,
					Perm_SystemSettings:         user.Perm_SystemSettings,
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

// GetCurrentAdminPermissionsHandler возвращает права текущего авторизованного администратора
func GetCurrentAdminPermissionsHandler(w http.ResponseWriter, r *http.Request) {
	// Получение информации об авторизованном админе
	authInfo, err := getAuthInfoFromRequest(r)
	if err != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	// Получает полные данные текущего админа
	user, err := GetAdminByLogin(authInfo.Login)
	if err != nil {
		http.Error(w, "Админ не найден", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"login":                         user.Auth_Login,
		"perm_create":                   user.Perm_Create,
		"perm_update":                   user.Perm_Update,
		"perm_delete":                   user.Perm_Delete,
		"perm_rename_clients":           user.Perm_RenameClients,
		"perm_rename_clients_groups":    user.Perm_RenameClientsGroups,
		"perm_delete_clients":           user.Perm_DeleteClients,
		"perm_delete_clients_groups":    user.Perm_DeleteClientsGroups,
		"perm_move_clients":             user.Perm_MoveClients,
		"perm_move_clients_groups":      user.Perm_MoveClientsGroups,
		"perm_uninstall_agents":         user.Perm_UninstallAgents,
		"perm_terminal_commands":        user.Perm_TerminalCommands,
		"perm_terminal_commands_groups": user.Perm_TerminalCommandsGroups,
		"perm_install_programs":         user.Perm_InstallPrograms,
		"perm_install_programs_groups":  user.Perm_InstallProgramsGroups,
		"perm_system_settings":          user.Perm_SystemSettings,
	})
}

// ToggleAdminPermissionHandler обрабатывает запросы на изменение конкретного разрешения учётной записи
func ToggleAdminPermissionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, err := getAuthInfoFromRequest(r)
	if err != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	// Получает данные текущего админа для проверки прав
	currentAdmin, err := GetAdminByLogin(authInfo.Login)
	if err != nil {
		http.Error(w, "Ошибка получения данных текущего админа", http.StatusInternalServerError)
		return
	}

	// Проверяет, что текущий админ имеет полные права
	if !hasFullPermissions(currentAdmin) {
		http.Error(w, "Только администраторы с полными правами могут изменять разрешения", http.StatusForbidden)
		return
	}

	var request struct {
		Auth_Login     string `json:"auth_login"`      // Логин изменяемого админа
		PermissionType string `json:"permission_type"` // Тип разрешения: "create", "update", "delete"
		NewValue       bool   `json:"new_value"`       // Новое значение разрешения
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	// Декодирует логин
	decodedLogin, err := url.QueryUnescape(request.Auth_Login)
	if err != nil {
		http.Error(w, "Ошибка декодирования логина", http.StatusBadRequest)
		return
	}

	// Проверяет корректность типа разрешения
	validTypes := map[string]bool{"create": true, "update": true, "delete": true, "rename_clients": true, "delete_clients": true, "move_clients": true, "uninstall_agents": true, "terminal_commands": true, "install_programs": true, "system_settings": true}
	if !validTypes[request.PermissionType] {
		http.Error(w, "Неверный тип разрешения", http.StatusBadRequest)
		return
	}

	// Получает данные изменяемого админа
	targetAdmin, err := GetAdminByLogin(decodedLogin)
	if err != nil {
		http.Error(w, "Учётная запись не найдена", http.StatusNotFound)
		return
	}

	// Проверяет, не отбираются ли права у последнего админа с полными правами
	if hasFullPermissions(targetAdmin) && !request.NewValue {
		fullPermCount, err := countFullPermissionAdmins()
		if err != nil {
			http.Error(w, "Ошибка проверки прав администраторов", http.StatusInternalServerError)
			return
		}
		if fullPermCount <= 1 {
			http.Error(w, "Нельзя отобрать права у последней учётной записи с полными правами!", http.StatusForbidden)
			return
		}
	}

	// Определяет название разрешения для логирования
	var permissionName string
	var oldValue bool

	switch request.PermissionType {
	case "create":
		permissionName = "создание учётных записей"
		oldValue = targetAdmin.Perm_Create
		targetAdmin.Perm_Create = request.NewValue
	case "update":
		permissionName = "изменение учётных записей"
		oldValue = targetAdmin.Perm_Update
		targetAdmin.Perm_Update = request.NewValue
	case "delete":
		permissionName = "удаление учётных записей"
		oldValue = targetAdmin.Perm_Delete
		targetAdmin.Perm_Delete = request.NewValue
	case "rename_clients":
		permissionName = "переименование клиентов"
		oldValue = targetAdmin.Perm_RenameClients
		targetAdmin.Perm_RenameClients = request.NewValue
		// При отключении права на переименование клиентов очищает список групп
		if !request.NewValue {
			targetAdmin.Perm_RenameClientsGroups = []string{}
		}
	case "delete_clients":
		permissionName = "удаление клиентов"
		oldValue = targetAdmin.Perm_DeleteClients
		targetAdmin.Perm_DeleteClients = request.NewValue
		// При отключении права на удаление клиентов очищает список групп
		if !request.NewValue {
			targetAdmin.Perm_DeleteClientsGroups = []string{}
		}
	case "move_clients":
		permissionName = "перемещение клиентов"
		oldValue = targetAdmin.Perm_MoveClients
		targetAdmin.Perm_MoveClients = request.NewValue
		// При отключении права на перемещение очищает список групп
		if !request.NewValue {
			targetAdmin.Perm_MoveClientsGroups = []string{}
		}
	case "uninstall_agents":
		permissionName = "полное удаление FiReAgent"
		oldValue = targetAdmin.Perm_UninstallAgents
		targetAdmin.Perm_UninstallAgents = request.NewValue
	case "terminal_commands":
		permissionName = "отправка cmd/PowerShell команд"
		oldValue = targetAdmin.Perm_TerminalCommands
		targetAdmin.Perm_TerminalCommands = request.NewValue
		// При отключении права на отправку команд очищает список групп
		if !request.NewValue {
			targetAdmin.Perm_TerminalCommandsGroups = []string{}
		}
	case "install_programs":
		permissionName = "установка ПО через QUIC"
		oldValue = targetAdmin.Perm_InstallPrograms
		targetAdmin.Perm_InstallPrograms = request.NewValue
		// При отключении права на установку ПО очищает список групп
		if !request.NewValue {
			targetAdmin.Perm_InstallProgramsGroups = []string{}
		}
	case "system_settings":
		permissionName = "системные настройки (обновление/откат, MQTT авторизация)"
		oldValue = targetAdmin.Perm_SystemSettings
		targetAdmin.Perm_SystemSettings = request.NewValue
	}

	// Проверяет, изменилось ли значение
	if oldValue == request.NewValue {
		w.Write([]byte("Разрешение уже имеет это значение"))
		return
	}

	// Обновляет дату изменения
	now := time.Now()
	targetAdmin.Auth_Date_Change = fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)",
		now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())

	// Сохраняет изменения
	if err := saveAdmin(targetAdmin); err != nil {
		logging.LogError("Аккаунты: Ошибка сохранения разрешения для %s: %v", decodedLogin, err)
		http.Error(w, "Ошибка сохранения разрешения", http.StatusInternalServerError)
		return
	}

	// Формирует сообщение для ответа и лога
	var actionWord string
	if request.NewValue {
		actionWord = "разрешил"
	} else {
		actionWord = "запретил"
	}

	logging.LogAction("Аккаунты: Админ \"%s\" (с именем: %s) %s %s для учётной записи \"%s\" (с именем: %s)",
		authInfo.Login, authInfo.Name, actionWord, permissionName, decodedLogin, targetAdmin.Auth_Name)

	fmt.Fprintf(w, "Разрешение изменено: %s", permissionName)
}

// UpdateMoveClientsGroupsHandler обрабатывает запросы на изменение списка разрешённых групп для перемещения
func UpdateMoveClientsGroupsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, err := getAuthInfoFromRequest(r)
	if err != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	// Получает данные текущего админа для проверки прав
	currentAdmin, err := GetAdminByLogin(authInfo.Login)
	if err != nil {
		http.Error(w, "Ошибка получения данных текущего админа", http.StatusInternalServerError)
		return
	}

	// Проверяет, что текущий админ имеет полные права
	if !hasFullPermissions(currentAdmin) {
		http.Error(w, "Только администраторы с полными правами могут изменять разрешения", http.StatusForbidden)
		return
	}

	var request struct {
		Auth_Login     string   `json:"auth_login"`       // Логин изменяемого админа
		AllowAllGroups bool     `json:"allow_all_groups"` // Разрешить все группы (true = полные права на перемещение)
		AllowedGroups  []string `json:"allowed_groups"`   // Список разрешённых групп (если не все)
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	// Декодирует логин
	decodedLogin, err := url.QueryUnescape(request.Auth_Login)
	if err != nil {
		http.Error(w, "Ошибка декодирования логина", http.StatusBadRequest)
		return
	}

	// Получает данные изменяемого админа
	targetAdmin, err := GetAdminByLogin(decodedLogin)
	if err != nil {
		http.Error(w, "Учётная запись не найдена", http.StatusNotFound)
		return
	}

	// Проверяет, не отбираются ли права у последнего админа с полными правами
	wasFullPerm := hasFullPermissions(targetAdmin)

	// Определяет, какие группы будут установлены
	var newGroups []string
	if request.AllowAllGroups {
		newGroups = []string{} // Пустой список = все группы
	} else {
		newGroups = request.AllowedGroups
	}

	// Устанавливает новые значения для проверки
	targetAdmin.Perm_MoveClientsGroups = newGroups

	// Устанавливает флаг перемещения в зависимости от выбора
	if len(newGroups) > 0 || request.AllowAllGroups {
		targetAdmin.Perm_MoveClients = true
	} else {
		// Если список пуст и не все группы — запрещает перемещение полностью
		targetAdmin.Perm_MoveClients = false
	}

	// Проверяет, будут ли полные права после изменения
	willHaveFullPerm := hasFullPermissions(targetAdmin)

	// Если был с полными правами, а станет без — проверяет, не последний ли он
	if wasFullPerm && !willHaveFullPerm {
		fullPermCount, err := countFullPermissionAdmins()
		if err != nil {
			http.Error(w, "Ошибка проверки прав администраторов", http.StatusInternalServerError)
			return
		}
		if fullPermCount <= 1 {
			http.Error(w, "Нельзя ограничить права последней учётной записи с полными правами!", http.StatusForbidden)
			return
		}
	}

	// Обновляет дату изменения
	now := time.Now()
	targetAdmin.Auth_Date_Change = fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)",
		now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())

	// Сохраняет изменения
	if err := saveAdmin(targetAdmin); err != nil {
		logging.LogError("Аккаунты: Ошибка сохранения списка групп для %s: %v", decodedLogin, err)
		http.Error(w, "Ошибка сохранения разрешений", http.StatusInternalServerError)
		return
	}

	// Формирует сообщение для лога
	var groupsInfo string
	if request.AllowAllGroups || len(newGroups) == 0 {
		groupsInfo = "все группы"
	} else {
		groupsInfo = fmt.Sprintf("группы: %v", newGroups)
	}

	logging.LogAction("Аккаунты: Админ \"%s\" (с именем: %s) изменил разрешённые группы для перемещения учётной записи \"%s\" (с именем: %s) на: %s",
		authInfo.Login, authInfo.Name, decodedLogin, targetAdmin.Auth_Name, groupsInfo)

	w.Write([]byte("Разрешённые группы обновлены"))
}

// UpdateDeleteClientsGroupsHandler обрабатывает запросы на изменение списка разрешённых групп для удаления клиентов
func UpdateDeleteClientsGroupsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, err := getAuthInfoFromRequest(r)
	if err != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	// Получает данные текущего админа для проверки прав
	currentAdmin, err := GetAdminByLogin(authInfo.Login)
	if err != nil {
		http.Error(w, "Ошибка получения данных текущего админа", http.StatusInternalServerError)
		return
	}

	// Проверяет, что текущий админ имеет полные права
	if !hasFullPermissions(currentAdmin) {
		http.Error(w, "Только администраторы с полными правами могут изменять разрешения", http.StatusForbidden)
		return
	}

	var request struct {
		Auth_Login     string   `json:"auth_login"`       // Логин изменяемого админа
		AllowAllGroups bool     `json:"allow_all_groups"` // Разрешить все группы (true = полные права на удаление)
		AllowedGroups  []string `json:"allowed_groups"`   // Список разрешённых групп (если не все)
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	// Декодирует логин
	decodedLogin, err := url.QueryUnescape(request.Auth_Login)
	if err != nil {
		http.Error(w, "Ошибка декодирования логина", http.StatusBadRequest)
		return
	}

	// Получает данные изменяемого админа
	targetAdmin, err := GetAdminByLogin(decodedLogin)
	if err != nil {
		http.Error(w, "Учётная запись не найдена", http.StatusNotFound)
		return
	}

	// Проверяет, не отбираются ли права у последнего админа с полными правами
	wasFullPerm := hasFullPermissions(targetAdmin)

	// Определяет, какие группы будут установлены
	var newGroups []string
	if request.AllowAllGroups {
		newGroups = []string{} // Пустой список = все группы
	} else {
		newGroups = request.AllowedGroups
	}

	// Устанавливает новые значения для проверки
	targetAdmin.Perm_DeleteClientsGroups = newGroups

	// Устанавливает флаг удаления в зависимости от выбора
	if len(newGroups) > 0 || request.AllowAllGroups {
		targetAdmin.Perm_DeleteClients = true
	} else {
		// Если список пуст и не все группы — запрещает удаление полностью
		targetAdmin.Perm_DeleteClients = false
	}

	// Проверяет, будут ли полные права после изменения
	willHaveFullPerm := hasFullPermissions(targetAdmin)

	// Если был с полными правами, а станет без — проверяет, не последний ли он
	if wasFullPerm && !willHaveFullPerm {
		fullPermCount, err := countFullPermissionAdmins()
		if err != nil {
			http.Error(w, "Ошибка проверки прав администраторов", http.StatusInternalServerError)
			return
		}
		if fullPermCount <= 1 {
			http.Error(w, "Нельзя ограничить права последней учётной записи с полными правами!", http.StatusForbidden)
			return
		}
	}

	// Обновляет дату изменения
	now := time.Now()
	targetAdmin.Auth_Date_Change = fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)",
		now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())

	// Сохраняет изменения
	if err := saveAdmin(targetAdmin); err != nil {
		logging.LogError("Аккаунты: Ошибка сохранения списка групп для удаления %s: %v", decodedLogin, err)
		http.Error(w, "Ошибка сохранения разрешений", http.StatusInternalServerError)
		return
	}

	// Формирует сообщение для лога
	var groupsInfo string
	if request.AllowAllGroups || len(newGroups) == 0 {
		groupsInfo = "все группы"
	} else {
		groupsInfo = fmt.Sprintf("группы: %v", newGroups)
	}

	logging.LogAction("Аккаунты: Админ \"%s\" (с именем: %s) изменил разрешённые группы для удаления клиентов учётной записи \"%s\" (с именем: %s) на: %s",
		authInfo.Login, authInfo.Name, decodedLogin, targetAdmin.Auth_Name, groupsInfo)

	w.Write([]byte("Разрешённые группы для удаления обновлены"))
}

// UpdateRenameClientsGroupsHandler обрабатывает запросы на изменение списка разрешённых групп для переименования клиентов
func UpdateRenameClientsGroupsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, err := getAuthInfoFromRequest(r)
	if err != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	// Получает данные текущего админа для проверки прав
	currentAdmin, err := GetAdminByLogin(authInfo.Login)
	if err != nil {
		http.Error(w, "Ошибка получения данных текущего админа", http.StatusInternalServerError)
		return
	}

	// Проверяет, что текущий админ имеет полные права
	if !hasFullPermissions(currentAdmin) {
		http.Error(w, "Только администраторы с полными правами могут изменять разрешения", http.StatusForbidden)
		return
	}

	var request struct {
		Auth_Login     string   `json:"auth_login"`       // Логин изменяемого админа
		AllowAllGroups bool     `json:"allow_all_groups"` // Разрешить все группы (true = полные права на переименование)
		AllowedGroups  []string `json:"allowed_groups"`   // Список разрешённых групп (если не все)
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	// Декодирует логин
	decodedLogin, err := url.QueryUnescape(request.Auth_Login)
	if err != nil {
		http.Error(w, "Ошибка декодирования логина", http.StatusBadRequest)
		return
	}

	// Получает данные изменяемого админа
	targetAdmin, err := GetAdminByLogin(decodedLogin)
	if err != nil {
		http.Error(w, "Учётная запись не найдена", http.StatusNotFound)
		return
	}

	// Проверяет, не отбираются ли права у последнего админа с полными правами
	wasFullPerm := hasFullPermissions(targetAdmin)

	// Определяет, какие группы будут установлены
	var newGroups []string
	if request.AllowAllGroups {
		newGroups = []string{} // Пустой список = все группы
	} else {
		newGroups = request.AllowedGroups
	}

	// Устанавливает новые значения для проверки
	targetAdmin.Perm_RenameClientsGroups = newGroups

	// Устанавливает флаг переименования в зависимости от выбора
	if len(newGroups) > 0 || request.AllowAllGroups {
		targetAdmin.Perm_RenameClients = true
	} else {
		// Если список пуст и не все группы — запрещает переименование полностью
		targetAdmin.Perm_RenameClients = false
	}

	// Проверяет, будут ли полные права после изменения
	willHaveFullPerm := hasFullPermissions(targetAdmin)

	// Если был с полными правами, а станет без — проверяет, не последний ли он
	if wasFullPerm && !willHaveFullPerm {
		fullPermCount, err := countFullPermissionAdmins()
		if err != nil {
			http.Error(w, "Ошибка проверки прав администраторов", http.StatusInternalServerError)
			return
		}
		if fullPermCount <= 1 {
			http.Error(w, "Нельзя ограничить права последней учётной записи с полными правами!", http.StatusForbidden)
			return
		}
	}

	// Обновляет дату изменения
	now := time.Now()
	targetAdmin.Auth_Date_Change = fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)",
		now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())

	// Сохраняет изменения
	if err := saveAdmin(targetAdmin); err != nil {
		logging.LogError("Аккаунты: Ошибка сохранения списка групп для переименования %s: %v", decodedLogin, err)
		http.Error(w, "Ошибка сохранения разрешений", http.StatusInternalServerError)
		return
	}

	// Формирует сообщение для лога
	var groupsInfo string
	if request.AllowAllGroups || len(newGroups) == 0 {
		groupsInfo = "все группы"
	} else {
		groupsInfo = fmt.Sprintf("группы: %v", newGroups)
	}

	logging.LogAction("Аккаунты: Админ \"%s\" (с именем: %s) изменил разрешённые группы для переименования клиентов учётной записи \"%s\" (с именем: %s) на: %s",
		authInfo.Login, authInfo.Name, decodedLogin, targetAdmin.Auth_Name, groupsInfo)

	w.Write([]byte("Разрешённые группы для переименования обновлены"))
}

// UpdateTerminalCommandsGroupsHandler обрабатывает запросы на изменение списка разрешённых групп для cmd/PowerShell команд
func UpdateTerminalCommandsGroupsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, err := getAuthInfoFromRequest(r)
	if err != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	// Получает данные текущего админа для проверки прав
	currentAdmin, err := GetAdminByLogin(authInfo.Login)
	if err != nil {
		http.Error(w, "Ошибка получения данных текущего админа", http.StatusInternalServerError)
		return
	}

	// Проверяет, что текущий админ имеет полные права
	if !hasFullPermissions(currentAdmin) {
		http.Error(w, "Только администраторы с полными правами могут изменять разрешения", http.StatusForbidden)
		return
	}

	var request struct {
		Auth_Login     string   `json:"auth_login"`       // Логин изменяемого админа
		AllowAllGroups bool     `json:"allow_all_groups"` // Разрешить все группы (true = полные права на команды)
		AllowedGroups  []string `json:"allowed_groups"`   // Список разрешённых групп (если не все)
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	// Декодирует логин
	decodedLogin, err := url.QueryUnescape(request.Auth_Login)
	if err != nil {
		http.Error(w, "Ошибка декодирования логина", http.StatusBadRequest)
		return
	}

	// Получает данные изменяемого админа
	targetAdmin, err := GetAdminByLogin(decodedLogin)
	if err != nil {
		http.Error(w, "Учётная запись не найдена", http.StatusNotFound)
		return
	}

	// Проверяет, не отбираются ли права у последнего админа с полными правами
	wasFullPerm := hasFullPermissions(targetAdmin)

	// Определяет, какие группы будут установлены
	var newGroups []string
	if request.AllowAllGroups {
		newGroups = []string{} // Пустой список = все группы
	} else {
		newGroups = request.AllowedGroups
	}

	// Устанавливает новые значения для проверки
	targetAdmin.Perm_TerminalCommandsGroups = newGroups

	// Устанавливает флаг команд в зависимости от выбора
	if len(newGroups) > 0 || request.AllowAllGroups {
		targetAdmin.Perm_TerminalCommands = true
	} else {
		// Если список пуст и не все группы — запрещает команды полностью
		targetAdmin.Perm_TerminalCommands = false
	}

	// Проверяет, будут ли полные права после изменения
	willHaveFullPerm := hasFullPermissions(targetAdmin)

	// Если был с полными правами, а станет без — проверяет, не последний ли он
	if wasFullPerm && !willHaveFullPerm {
		fullPermCount, err := countFullPermissionAdmins()
		if err != nil {
			http.Error(w, "Ошибка проверки прав администраторов", http.StatusInternalServerError)
			return
		}
		if fullPermCount <= 1 {
			http.Error(w, "Нельзя ограничить права последней учётной записи с полными правами!", http.StatusForbidden)
			return
		}
	}

	// Обновляет дату изменения
	now := time.Now()
	targetAdmin.Auth_Date_Change = fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)",
		now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())

	// Сохраняет изменения
	if err := saveAdmin(targetAdmin); err != nil {
		logging.LogError("Ак��аунты: Ошибка сохранения списка групп для cmd/PowerShell %s: %v", decodedLogin, err)
		http.Error(w, "Ошибка сохранения разрешений", http.StatusInternalServerError)
		return
	}

	// Формирует сообщение для лога
	var groupsInfo string
	if request.AllowAllGroups || len(newGroups) == 0 {
		groupsInfo = "все группы"
	} else {
		groupsInfo = fmt.Sprintf("группы: %v", newGroups)
	}

	logging.LogAction("Аккаунты: Админ \"%s\" (с именем: %s) изменил разрешённые группы для cmd/PowerShell команд учётной записи \"%s\" (с именем: %s) на: %s",
		authInfo.Login, authInfo.Name, decodedLogin, targetAdmin.Auth_Name, groupsInfo)

	w.Write([]byte("Разрешённые группы для cmd/PowerShell обновлены"))
}

// UpdateInstallProgramsGroupsHandler обрабатывает запросы на изменение списка разрешённых групп для установки ПО
func UpdateInstallProgramsGroupsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	// Получение информации об инициаторе (текущем админе)
	authInfo, err := getAuthInfoFromRequest(r)
	if err != nil {
		http.Error(w, "Ошибка авторизации", http.StatusUnauthorized)
		return
	}

	// Получает данные текущего админа для проверки прав
	currentAdmin, err := GetAdminByLogin(authInfo.Login)
	if err != nil {
		http.Error(w, "Ошибка получения данных текущего админа", http.StatusInternalServerError)
		return
	}

	// Проверяет, что текущий админ имеет полные права
	if !hasFullPermissions(currentAdmin) {
		http.Error(w, "Только администраторы с полными правами могут изменять разрешения", http.StatusForbidden)
		return
	}

	var request struct {
		Auth_Login     string   `json:"auth_login"`       // Логин изменяемого админа
		AllowAllGroups bool     `json:"allow_all_groups"` // Разрешить все группы (true = полные права на установку)
		AllowedGroups  []string `json:"allowed_groups"`   // Список разрешённых групп (если не все)
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Ошибка парсинга данных", http.StatusBadRequest)
		return
	}

	// Декодирует логин
	decodedLogin, err := url.QueryUnescape(request.Auth_Login)
	if err != nil {
		http.Error(w, "Ошибка декодирования логина", http.StatusBadRequest)
		return
	}

	// Получает данные изменяемого админа
	targetAdmin, err := GetAdminByLogin(decodedLogin)
	if err != nil {
		http.Error(w, "Учётная запись не найдена", http.StatusNotFound)
		return
	}

	// Проверяет, не отбираются ли права у последнего админа с полными правами
	wasFullPerm := hasFullPermissions(targetAdmin)

	// Определяет, какие группы будут установлены
	var newGroups []string
	if request.AllowAllGroups {
		newGroups = []string{} // Пустой список = все группы
	} else {
		newGroups = request.AllowedGroups
	}

	// Устанавливает новые значения для проверки
	targetAdmin.Perm_InstallProgramsGroups = newGroups

	// Устанавливает флаг установки ПО в зависимости от выбора
	if len(newGroups) > 0 || request.AllowAllGroups {
		targetAdmin.Perm_InstallPrograms = true
	} else {
		// Если список пуст и не все группы — запрещает установку ПО полностью
		targetAdmin.Perm_InstallPrograms = false
	}

	// Проверяет, будут ли полные права после изменения
	willHaveFullPerm := hasFullPermissions(targetAdmin)

	// Если был с полными правами, а станет без — проверяет, не последний ли он
	if wasFullPerm && !willHaveFullPerm {
		fullPermCount, err := countFullPermissionAdmins()
		if err != nil {
			http.Error(w, "Ошибка проверки прав администраторов", http.StatusInternalServerError)
			return
		}
		if fullPermCount <= 1 {
			http.Error(w, "Нельзя ограничить права последней учётной записи с полными правами!", http.StatusForbidden)
			return
		}
	}

	// Обновляет дату изменения
	now := time.Now()
	targetAdmin.Auth_Date_Change = fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)",
		now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())

	// Сохраняет изменения
	if err := saveAdmin(targetAdmin); err != nil {
		logging.LogError("Аккаунты: Ошибка сохранения списка групп для установки ПО %s: %v", decodedLogin, err)
		http.Error(w, "Ошибка сохранения разрешений", http.StatusInternalServerError)
		return
	}

	// Формирует сообщение для лога
	var groupsInfo string
	if request.AllowAllGroups || len(newGroups) == 0 {
		groupsInfo = "все группы"
	} else {
		groupsInfo = fmt.Sprintf("группы: %v", newGroups)
	}

	logging.LogAction("Аккаунты: Админ \"%s\" (с именем: %s) изменил разрешённые группы для установки ПО учётной записи \"%s\" (с именем: %s) на: %s",
		authInfo.Login, authInfo.Name, decodedLogin, targetAdmin.Auth_Name, groupsInfo)

	w.Write([]byte("Разрешённые группы для установки ПО обновлены"))
}
