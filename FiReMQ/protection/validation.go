// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package protection

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// ValidationRule описывает правило валидации для конкретного поля
type ValidationRule struct {
	MinLength   int    // Минимальное кол-во символов
	MaxLength   int    // Максимальное кол-во символов
	AllowSpaces bool   // Разрешить или запретить пробелы
	FieldName   string // Название поля для сообщений об ошибках
}

// Паттерны для валидации
const (
	patternAlphaNumRuEn         = `^[a-zA-Z0-9а-яА-ЯёЁ_!@#$%.\/?\-]+$`                // Без пробелов
	patternAlphaNumRuEnWithSpec = `^[a-zA-Z0-9а-яА-ЯёЁ_!@#$%.\/?\-\*+=,:|()"'@–— ]+$` // С доп. символами и буквальным пробелом
)

// Регулярные выражения на уровне пакета, для повышения производительности
var (
	regexAlphaNumRuEn           = regexp.MustCompile(patternAlphaNumRuEn)
	regexAlphaNumRuEnWithSpaces = regexp.MustCompile(patternAlphaNumRuEnWithSpec)
)

// ValidateFields выполняет санитизацию и валидацию данных, полученных из полей ввода веб-страниц
func ValidateFields(data map[string]string, rules map[string]ValidationRule) (map[string]string, error) {
	sanitized := make(map[string]string, len(data))

	// Применяет правила ко всем полям, для которых они определены
	for field, value := range data {
		rule, exists := rules[field]
		if !exists {
			continue // Поле без правила — пропускается
		}

		// Санитизация: удаление пробелов по краям
		sv := strings.TrimSpace(value)

		// fmt.Println(sv) // ДЛЯ ОТЛАДКИ

		// Пропускает, если поле пустое и MinLength установлен в 0 (поле необязательное)
		if sv == "" && rule.MinLength == 0 {
			sanitized[field] = sv
			continue
		}

		// Проверяет длину в рунах (символах)
		runeCount := utf8.RuneCountInString(sv)
		if runeCount < rule.MinLength || runeCount > rule.MaxLength {
			return nil, fmt.Errorf("%s должен быть от %d до %d символов", rule.FieldName, rule.MinLength, rule.MaxLength)
		}

		// Проверяет наличие только разрешенных символов
		var ok bool
		if rule.AllowSpaces {
			// Использует шаблон, разрешающий пробелы и специальные символы
			ok = regexAlphaNumRuEnWithSpaces.MatchString(sv)
		} else {
			// Использует шаблон, не разрешающий пробелы
			ok = regexAlphaNumRuEn.MatchString(sv)
		}
		if !ok {
			return nil, fmt.Errorf("%s содержит запрещённые символы", rule.FieldName)
		}

		sanitized[field] = sv
	}

	return sanitized, nil
}

// HashPassword хеширует пароль с использованием bcrypt со стоимостью 12
func HashPassword(password string) string {
	// Игнорирует возможную ошибку, поскольку входные данные должны быть корректны
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash)
}

// CompareHash проверяет соответствие хеша и исходной строки, предотвращая timing-атаки
func CompareHash(hashedValue, plainValue string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashedValue), []byte(plainValue)) == nil
}
