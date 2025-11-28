// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

/*
	Генератор тестовых рандомных клиентов для БД "FiReMQ_DB" (BadgerDB).
*/

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

const version = "26.11.25" // Текущая версия AddClient в формате "дд.мм.гг"

var (
	DBInstance *badger.DB // Объект, предоставляет доступ к базе данных
	r          *rand.Rand // Локальный генератор случайных чисел
)

// InitDB инициализирует базу данных
func InitDB() error {
	opts := badger.DefaultOptions("./FiReMQ_DB").
		// WithLoggingLevel(badger.DEBUG).
		WithValueLogFileSize(64 << 20). // Устанавливает максимальный размер файла журнала значений в 64MB
		WithMemTableSize(1 << 30).      // Устанавливает размер memtable в 1GB для быстрых операций
		WithNumGoroutines(4)            // Использует 4 потока для фоновых задач
	db, err := badger.Open(opts)
	if err != nil {
		return err
	}
	DBInstance = db
	return nil
}

// CloseDB закрывает соединение с базой данных
func CloseDB() {
	_ = DBInstance.Close()
}

// SaveClientBatch сохраняет список клиентов в базе данных используя батч-транзакцию
func SaveClientBatch(clients []map[string]string) error {
	return DBInstance.Update(func(txn *badger.Txn) error {
		for _, data := range clients {
			jsonData, err := json.Marshal(data)
			if err != nil {
				return err
			}
			key := []byte("client:" + data["client_id"])
			if err := txn.Set(key, jsonData); err != nil {
				return err
			}
		}
		return nil
	})
}

// generateRandomName генерирует случайное имя или фразу
func generateRandomName() string {
	names := []string{
		"Вася", "Петя", "Иван", "Сидор", "Кузьма",
		"Гриша", "Ваня", "Коля", "Федя", "Жора",
		"Вова", "Сеня", "Лёха", "Витёк", "Слава",
		"Дима", "Толя", "Саня", "Женя", "Миша",
		"Серёга", "Влад", "Артем", "Гена", "Боря",
	}

	lastNames := []string{
		"Пупкин", "Васечкин", "Дураков", "Козлов", "Рыбкин",
		"Шариков", "Кузнецов", "Малышев", "Пивной", "Дырявый",
		"Пьяный", "Бухой", "Кривой", "Жопкин", "Лопухов",
		"Растяпин", "Балбесов", "Тормозов", "Сонев", "Забывалкин",
		"Неумехов", "Разиняев", "Врунишкин", "Лентяев", "Чудаков",
	}

	words := []string{
		"Борщ", "Пельмень", "Ватрушка", "Котлета", "Селёдка",
		"Огурец", "Помидор", "Картошка", "Морковка", "Свёкла",
		"Пирожок", "Блинчик", "Сосиска", "Торт", "Пончик",
		"Вафля", "Мороженое", "Конфета", "Пирог", "Булка",
		"Чайник", "Самовар", "Ложка", "Вилка", "Тарелка",
		"Чашка", "Кастрюля", "Сковорода", "Подушка", "Одеяло",
		"Ковёр", "Диван", "Стул", "Стол", "Шкаф",
		"Лампа", "Зеркало", "Телефон", "Ноутбук", "Монитор",
		"Мышка", "Кошелёк", "Часы", "Очки", "Зубная щётка",
		"Игрушка", "Кукла", "Мячик", "Кубики", "Цветок",
		"Дерево", "Камень", "Песок", "Вода", "Огонь",
		"Небо", "Облако", "Дождь", "Кошка", "Собака",
		"Рыбка", "Хомяк", "Лиса", "Волк", "Медведь",
		"Заяц", "Ёжик", "Гриб", "Река", "Озеро",
		"Лес", "Гора", "Яма", "Болото", "Трясина",
	}

	choice := r.Intn(5) // Выбирает один из 5 возможных вариантов комбинаций
	switch choice {
	case 0:
		// Обычное имя + фамилия
		return fmt.Sprintf("%s %s", names[r.Intn(len(names))], lastNames[r.Intn(len(lastNames))])
	case 1:
		// Слово + имя + фамилия
		return fmt.Sprintf("%s %s %s", words[r.Intn(len(words))], names[r.Intn(len(names))], lastNames[r.Intn(len(lastNames))])
	case 2:
		// Имя + слово + фамилия
		return fmt.Sprintf("%s %s %s", names[r.Intn(len(names))], words[r.Intn(len(words))], lastNames[r.Intn(len(lastNames))])
	case 3:
		// Имя + фамилия + слово
		return fmt.Sprintf("%s %s %s", names[r.Intn(len(names))], lastNames[r.Intn(len(lastNames))], words[r.Intn(len(words))])
	case 4:
		// Только слово (как было раньше)
		return words[r.Intn(len(words))]
	default:
		return fmt.Sprintf("%s %s", names[r.Intn(len(names))], lastNames[r.Intn(len(lastNames))])
	}
}

// generateRandomClientID генерирует случайный идентификатор клиента
func generateRandomClientID() string {
	return fmt.Sprintf("%X", r.Int63())
}

// generateRandomIP генерирует случайный глобальный IP-адрес
func generateRandomIP() string {
	return fmt.Sprintf("%d.%d.%d.%d", r.Intn(256), r.Intn(256), r.Intn(256), r.Intn(256))
}

// generateRandomLocalIP генерирует случайный локальный IP-адрес
func generateRandomLocalIP() string {
	return fmt.Sprintf("192.168.%d.%d", r.Intn(256), r.Intn(256))
}

// generateRandomTimestamp генерирует случайную дату и время
func generateRandomTimestamp() string {
	randomTime := time.Now().Add(time.Duration(r.Intn(1000)-500) * time.Hour) // Добавляет смещение до 500 часов вперед или назад относительно текущего времени
	return randomTime.Format("02.01.06(15:04)")
}

func main() {
	args := os.Args

	// Показывает версию утилиты ServerUpdater
	if len(args) >= 2 && strings.EqualFold(args[1], "--version") {
		fmt.Printf("Версия \"AddClient\": %s\n", version)
		return
	}

	// Инициализирует локальный генератор случайных чисел
	r = rand.New(rand.NewSource(time.Now().UnixNano()))

	// Инициализирует базу данных
	if err := InitDB(); err != nil {
		log.Fatalf("Ошибка инициализации БД: %v", err)
	}
	defer CloseDB()

	// Запрашивает у пользователя количество клиентов для генерации
	var count int
	fmt.Print("Введите количество клиентов: ")
	if _, err := fmt.Scan(&count); err != nil || count <= 0 {
		log.Fatal("Неверное количество")
	}

	// Автоматически определяет размер буфера для батча, чтобы избежать слишком больших транзакций
	batchSize := min(count, 100000)

	// Запускает таймер для измерения производительности
	startTime := time.Now()

	batch := make([]map[string]string, 0, batchSize)

	// Начинает цикл добавления клиентов и генерации данных
	for range count {
		status := []string{"On", "Off"}[r.Intn(2)]
		name := generateRandomName()
		ip := generateRandomIP()
		localIP := generateRandomLocalIP()
		clientID := generateRandomClientID()
		timestamp := generateRandomTimestamp()

		// Формирует структуру данных клиента
		data := map[string]string{
			"status":     status,
			"name":       name,
			"ip":         ip,
			"local_ip":   localIP,
			"client_id":  clientID,
			"time_stamp": timestamp,
			"group":      "Новые клиенты",
			"subgroup":   "Нераспределённые",
		}

		batch = append(batch, data)

		// Отправляет батч на запись, когда достигнут установленный размер
		if len(batch) >= batchSize {
			if err := SaveClientBatch(batch); err != nil {
				log.Fatalf("Ошибка записи батча: %v", err)
			}
			batch = batch[:0] // Очищает батч для повторного использования нижележащего массива
		}
	}

	// Записывает оставшиеся данные, если финальный батч не пуст
	if len(batch) > 0 {
		if err := SaveClientBatch(batch); err != nil {
			log.Fatalf("Ошибка записи финального батча: %v", err)
		}
	}

	// Вычисляет затраченное время
	duration := time.Since(startTime)
	microseconds := duration.Microseconds()
	milliseconds := duration.Milliseconds()
	seconds := duration.Seconds()

	fmt.Printf("Клиенты успешно добавлены за:\n")
	if microseconds < 1000 {
		fmt.Printf("- %d мкс\n", microseconds)
	} else {
		fmt.Printf("- %d мкс (%d мс)\n", microseconds, milliseconds)
		if milliseconds >= 1000 {
			fmt.Printf("- %.3f с\n", seconds)
		}
	}

	fmt.Println("Нажмите Enter для выхода.")
	fmt.Scanln() // Ожидание нажатия Enter
	fmt.Scanln() // Повторный вызов для уверенного ожидания
}
