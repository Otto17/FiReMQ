// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package db

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"FiReMQ/logging" // Локальный пакет с логированием в HTML файл
	"FiReMQ/pathsOS" // Локальный пакет с путями для разных платформ

	"github.com/dgraph-io/badger/v4"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

// User, содержит данные админов из БД
type User struct {
	Auth_Name         string `json:"auth_name"`
	Auth_Login        string `json:"auth_login"`
	Auth_PasswordHash string `json:"auth_password_hash"`
	Auth_Date_Create  string `json:"date_create"`
	Auth_Date_Change  string `json:"date_change"`
	Auth_Session_ID   string `json:"auth_session_id"`
}

// SimpleUser, содержит упрощенные данные для отображения
type SimpleUser struct {
	Login string
	Name  string
}

// PerformPasswordReset запускает интерактивный режим смены пароля
func PerformPasswordReset() {
	c := make(chan os.Signal, 1) // Канал для перехвата сигналов
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() { // Горутина для обработки Ctrl+C
		<-c
		fmt.Println("\nВыход.") // Сообщение при выходе

		// Принудительно закрывает БД при зависании
		if err := Close(); err != nil {
			// Игнорирует ошибку если БД закрывается штатно
		}

		// Восстанавливает права для Linux перед выходом
		if runtime.GOOS == "linux" {
			_ = pathsOS.VerifyAndFixPermissions()
		}
		os.Exit(0)
	}()

	enableANSI() // Включает поддержку ANSI цветов в Windows

	if runtime.GOOS == "linux" { // Проверяет права и состояние службы на Linux
		if os.Geteuid() != 0 {
			fmt.Printf("%sУтилита должна быть запущена от пользователя root!%s\n", ColorRed, ColorReset)
			os.Exit(1)
		}

		if isServiceRunning() {
			fmt.Println("Сначала остановите службу командой \"systemctl stop firemq\"!")
			os.Exit(1)
		}
	}

	fmt.Println("Запуск режима сброса пароля FiReMQ...")
	fmt.Printf("Путь к БД: %s\n", pathsOS.Path_DB)

	if err := InitDB(); err != nil { // Инициализирует БД
		if strings.Contains(err.Error(), "Another process is using this Badger database") {
			fmt.Printf("\n%sОШИБКА: База данных заблокирована!%s\n", ColorRed, ColorReset)
			fmt.Println("Остановите сервер FiReMQ перед сбросом пароля.")
			if runtime.GOOS == "linux" {
				fmt.Println("Выполните: systemctl stop firemq")
			}
			os.Exit(1)
		}
		logging.LogError("Сброс пароля БД (CLI): Не удалось открыть БД: %v", err)
	}

	// Закрытие БД при выходе
	defer func() {
		if err := Close(); err != nil {
			logging.LogError("Сброс пароля БД (CLI): Ошибка закрытия БД: %v", err)
		}
	}()

	// Загружает учётные записи админов
	users, err := loadUsers()
	if err != nil {
		logging.LogError("Сброс пароля (CLI): Ошибка чтения учётных записей: %v", err)
		os.Exit(1)
	}

	if len(users) == 0 {
		fmt.Println("В базе данных нет учётных записей.")
		return
	}

	selectedUser := selectUserInteractive(users)
	if selectedUser == nil {
		// defer Close() сработает при возврате nil
		return
	}

	newPass := promptNewPassword(selectedUser.Login) // Запрашивает новый пароль

	if err := updatePassword(selectedUser.Login, newPass); err != nil { // Обновляет пароль в БД
		logging.LogError("Сброс пароля БД (CLI): Ошибка обновления пароля для %s: %v", selectedUser.Login, err)
		os.Exit(1)
	}

	logging.LogAction("Сброс пароля (CLI): Пароль для учётной записи '%s' (с именем: %s) успешно изменён через консоль", selectedUser.Login, selectedUser.Name)
	fmt.Printf("\n%sПароль для учётной записи '%s' (%s) успешно изменён!%s\n", ColorGreen, selectedUser.Login, selectedUser.Name, ColorReset)

	if runtime.GOOS == "linux" { // Восстанавливает права доступа
		fmt.Println("Применение прав доступа...")
		if err := pathsOS.VerifyAndFixPermissions(); err != nil {
			logging.LogError("Сброс пароля БД (CLI): Сброс пароля: Ошибка при восстановлении прав доступа: %v", err)
			fmt.Printf("%sОшибка при восстановлении прав доступа: %v%s\n", ColorRed, err, ColorReset)
		} else {
			logging.LogSystem("Сброс пароля (CLI): Права доступа, владелец и группа успешно восстановлены")
			fmt.Println("Права доступа, владелец и группа успешно восстановлены.")
		}
	}
}

// loadUsers загружает список учётных записей админов
func loadUsers() ([]SimpleUser, error) {
	var users []SimpleUser

	err := DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("auth:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(opts.Prefix); it.ValidForPrefix(opts.Prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var u User
				if err := json.Unmarshal(val, &u); err != nil {
					return err
				}
				users = append(users, SimpleUser{
					Login: u.Auth_Login,
					Name:  u.Auth_Name,
				})
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	sort.Slice(users, func(i, j int) bool { // Сортирует учётные записи по логину
		return users[i].Login < users[j].Login
	})

	return users, err
}

// selectUserInteractive реализует пагинацию и выбор админа
func selectUserInteractive(users []SimpleUser) *SimpleUser {
	reader := bufio.NewReader(os.Stdin)
	pageSize := 10 // Размер страницы
	totalPages := (len(users) + pageSize - 1) / pageSize
	currentPage := 0

	for {
		startIndex := currentPage * pageSize
		endIndex := min(startIndex+pageSize, len(users))

		fmt.Println("\n----------------------------------------")
		fmt.Println("Список учётных записей админов:")
		fmt.Println("")

		currentList := users[startIndex:endIndex]
		for i, u := range currentList {
			globalIndex := startIndex + i + 1
			fmt.Printf("%d > Логин: %s%s%s (Имя: %s)\n",
				globalIndex, ColorCyan, u.Login, ColorReset, u.Name)
		}
		fmt.Println("")

		prompt := fmt.Sprintf("Введите номер (%sa=Отмена%s", ColorGreen, ColorReset)
		if len(users) > pageSize {
			prompt += fmt.Sprintf(", %sp=Предыдущий%s, %sn=Следующий%s", ColorYellow, ColorReset, ColorCyan, ColorReset)
		}
		prompt += "): "
		fmt.Print(prompt)

		input, err := reader.ReadString('\n')
		if err != nil {
			// Возвращает nil для выхода, т.к. Ctrl+C обрабатывается в PerformPasswordReset
			return nil
		}

		input = strings.TrimSpace(strings.ToLower(input))

		switch input {
		case "a", "c", "ф", "с":
			fmt.Println("Операция отменена.")
			return nil
		case "n", "т":
			if len(users) > pageSize && currentPage < totalPages-1 {
				currentPage++
			} else {
				fmt.Println(">> Это последняя страница.")
			}
		case "p", "з":
			if len(users) > pageSize && currentPage > 0 {
				currentPage--
			} else {
				fmt.Println(">> Это первая страница.")
			}
		default:
			idx, err := strconv.Atoi(input)
			if err != nil || idx < 1 || idx > len(users) {
				fmt.Println(">> Неверный ввод.")
				continue
			}
			return &users[idx-1]
		}
	}
}

// promptNewPassword запрашивает и подтверждает пароль
func promptNewPassword(login string) string {
	fmt.Printf("\nВведите новый пароль для '%s': ", login)

	bytePass, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Println("\nВыход.")
		Close()
		os.Exit(0)
	}
	pass := string(bytePass)
	fmt.Println()

	fmt.Print("Подтвердите пароль: ")
	bytePassConfirm, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Println("\nВыход.")
		Close()
		os.Exit(0)
	}
	passConfirm := string(bytePassConfirm)
	fmt.Println()

	if pass != passConfirm {
		fmt.Printf("%sПароли не совпадают! Попробуйте снова.%s\n", ColorRed, ColorReset)
		return promptNewPassword(login)
	}

	if strings.TrimSpace(pass) == "" {
		fmt.Printf("%sПароль не может быть пустым!%s\n", ColorRed, ColorReset)
		return promptNewPassword(login)
	}

	return pass
}

// updatePassword хеширует пароль и обновляет запись в БД
func updatePassword(login, rawPassword string) error {
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(rawPassword), 12) // Хеширует пароль с cost 12
	if err != nil {
		return fmt.Errorf("ошибка хеширования: %w", err)
	}
	newHash := string(hashBytes)

	return DBInstance.Update(func(txn *badger.Txn) error {
		key := []byte("auth:" + login)
		item, err := txn.Get(key)
		if err != nil {
			return fmt.Errorf("админ не найден при обновлении: %w", err)
		}

		var user User
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &user)
		})
		if err != nil {
			return err
		}

		// Обновляет поля учётной записи
		user.Auth_PasswordHash = newHash
		now := time.Now()
		user.Auth_Date_Change = fmt.Sprintf("%02d.%02d.%02d(%02d:%02d)",
			now.Day(), now.Month(), now.Year()%100, now.Hour(), now.Minute())

		userData, err := json.Marshal(user) // Сериализует данные в JSON
		if err != nil {
			return err
		}

		return txn.Set(key, userData)
	})
}
