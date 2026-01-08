// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package protection

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"

	"FiReMQ/pathsOS" // Локальный пакет с путями для разных платформ

	"golang.org/x/crypto/chacha20poly1305"
)

// generateKey генерирует ключ для ChaCha20-Poly1305 и сохраняет его в файл с ограниченными правами доступа
func generateKey() ([]byte, error) {
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	// Устанавливает права 0600 (только чтение/запись владельцем)
	return key, os.WriteFile(pathsOS.Key_ChaCha20_Poly1305, key, 0600)
}

// loadKey загружает ключ из файла или генерирует новый, если файл отсутствует или имеет неправильный размер
func loadKey() ([]byte, error) {
	if _, err := os.Stat(pathsOS.Key_ChaCha20_Poly1305); os.IsNotExist(err) {
		return generateKey()
	}

	key, err := os.ReadFile(pathsOS.Key_ChaCha20_Poly1305)
	if err != nil {
		return nil, err
	}

	// Перегенерирует ключ, если его размер не соответствует стандарту
	if len(key) != chacha20poly1305.KeySize {
		return generateKey()
	}

	return key, nil
}

// EncryptLogin шифрует логин администратора, используя ChaCha20-Poly1305 с рандомным nonce
func EncryptLogin(login string) (string, error) {
	key, err := loadKey()
	if err != nil {
		return "", err
	}

	// Использует NewX для XChaCha20-Poly1305, который обеспечивает больший nonce для лучшей защиты
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}

	// Генерирует уникальный nonce для каждого шифрования
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	// Шифрует логин и добавляет nonce в начало шифротекста
	ciphertext := aead.Seal(nonce, nonce, []byte(login), nil)
	// Кодирует результат в URL-безопасный base64
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// DecryptLogin расшифровывает логин администратора из строки, содержащей nonce и зашифрованный логин
func DecryptLogin(encryptedLogin string) (string, error) {
	key, err := loadKey()
	if err != nil {
		return "", err
	}

	ciphertext, err := base64.RawURLEncoding.DecodeString(encryptedLogin)
	if err != nil {
		return "", err
	}

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}

	// Проверяет минимальную длину шифротекста (nonce + тег)
	if len(ciphertext) < aead.NonceSize() {
		return "", errors.New("слишком короткий зашифрованный текст")
	}

	// Извлекает nonce из начала шифротекста
	nonce := ciphertext[:aead.NonceSize()]
	// Выполняет расшифровку и проверку аутентификационного тега
	plaintext, err := aead.Open(nil, nonce, ciphertext[aead.NonceSize():], nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
