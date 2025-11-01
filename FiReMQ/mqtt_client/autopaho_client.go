// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package mqtt_client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"FiReMQ/pathsOS" // Локальный пакет с путями для разных платформ

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/google/uuid"
)

// Константы для настройки производительности
const (
	fileTimeout     = 8 * time.Minute // Таймаут для неполностью полученных файлов
	cleanupInterval = 5 * time.Minute // Интервал для очистки старых буферов
)

var (
	// Default представляет глобальный синглтон AutoPaho клиента
	Default *MQTTService

	// FileBuffers хранит глобальный пул буферов для сборки файлов
	fileBuffers sync.Map // (map[string]*FileBuffer)
)

// ChunkTask содержит метаданные и данные части файла для обработки и сборки
type chunkTask struct {
	fileKey     string // Уникальный ключ файла (тип и ID клиента)
	chunkNum    uint64 // Номер текущей части
	totalChunks uint64 // Общее количество частей в файле
	chunkData   []byte // Данные части
	flags       uint16 // Флаги метаданных (например, признак последнего чанка)
}

// FileBuffer хранит части файла, их количество, счётчик полученных частей, время последнего обновления и мьютекс для синхронизации
type FileBuffer struct {
	TotalChunks   uint64     // Общее количество частей в файле
	Received      [][]byte   // Массив для хранения данных частей
	ReceivedCount int32      // Счётчик полученных частей
	LastUpdated   time.Time  // Время последнего обновления буфера
	mu            sync.Mutex // Защищает доступ к буферу
}

// MQTTService инкапсулирует клиент AutoPaho
type MQTTService struct {
	client *autopaho.ConnectionManager
}

// CreateTLSConfig собирает TLS конфигурацию и возвращает параметры подключения к брокеру
func createTLSConfig() (*tls.Config, string, string, string, string, string, error) {
	// Определяет параметры подключения к брокеру
	brokerHost := pathsOS.MQTT_Client_Host
	brokerPort := pathsOS.MQTT_Client_Port
	mqttID := "Client_FiReMQ_AutoPaho"

	// Чтение логина и пароля из MQTT конфига
	configFile, err := os.Open(pathsOS.Path_Config_MQTT)
	if err != nil {
		return nil, "", "", "", "", "", fmt.Errorf("ошибка открытия конфига MQTT: %v", err)
	}
	defer configFile.Close()

	var config struct {
		Hooks struct {
			Auth struct {
				Ledger struct {
					Auth []struct {
						Account  int    `json:"account"`
						Username string `json:"username"`
						Password string `json:"password"`
					} `json:"auth"`
				} `json:"ledger"`
			} `json:"auth"`
		} `json:"hooks"`
	}
	if err := json.NewDecoder(configFile).Decode(&config); err != nil {
		return nil, "", "", "", "", "", fmt.Errorf("ошибка парсинга конфига MQTT: %v", err)
	}

	// Поиск аккаунта с приоритетом 0, используемого для подключения
	var username, password string
	found := false
	for _, auth := range config.Hooks.Auth.Ledger.Auth {
		if auth.Account == 0 {
			username = auth.Username
			password = auth.Password
			found = true
			break
		}
	}
	if !found {
		return nil, "", "", "", "", "", fmt.Errorf("аккаунт с приоритетом 0 не найден в конфиге MQTT")
	}

	// Читает корневой сертификат сервера CA
	ServerCaPEM, err := os.ReadFile(pathsOS.Path_Server_MQTT_CA)
	if err != nil {
		return nil, "", "", "", "", "", fmt.Errorf("чтение серверного CA: %v", err)
	}

	// Читает сертификат клиента
	certPEM, err := os.ReadFile(pathsOS.Path_Client_MQTT_Cert)
	if err != nil {
		return nil, "", "", "", "", "", fmt.Errorf("чтение клиентского сертификата: %v", err)
	}

	// Читает ключ сертификата клиента
	keyPEM, err := os.ReadFile(pathsOS.Path_Client_MQTT_Key)
	if err != nil {
		return nil, "", "", "", "", "", fmt.Errorf("чтение клиентского ключа: %v", err)
	}

	// Формирует пул корневого серверного CA
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ServerCaPEM) {
		return nil, "", "", "", "", "", fmt.Errorf("недействительный серверный CA")
	}

	// Загружает пару клиентских ключей
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, "", "", "", "", "", fmt.Errorf("пара ключей x509: %v", err)
	}

	// Собирает tls.Config для взаимной TLS аутентификации
	tlsCfg := &tls.Config{
		RootCAs:            pool,
		Certificates:       []tls.Certificate{cert},
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: false, // Всегда должна быть false при наличии RootCAs
		ServerName:         brokerHost,
	}

	return tlsCfg, brokerHost, brokerPort, username, password, mqttID, nil
}

// StartMQTTClient создаёт, настраивает и запускает MQTT-клиент
func StartMQTTClient() *MQTTService {
	ctx := context.Background()
	tlsConfig, brokerHost, brokerPort, login, password, clientID, err := createTLSConfig()
	if err != nil {
		log.Fatalf("Ошибка создания TLS: %v", err)
	}

	// Формирует URL для подключения к брокеру
	brokerURL, err := url.Parse(fmt.Sprintf("tls://%s:%s", brokerHost, brokerPort))
	if err != nil {
		log.Fatalf("Ошибка парсинга брокера: %v", err)
	}

	cfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{brokerURL},
		TlsCfg:                        tlsConfig,
		KeepAlive:                     20,
		CleanStartOnInitialConnection: true,
		SessionExpiryInterval:         0,
		ConnectUsername:               login,
		ConnectPassword:               []byte(password),
		ClientConfig: paho.ClientConfig{
			ClientID: clientID,
			OnClientError: func(err error) {
				log.Printf("Клиентская ошибка MQTT: %v", err)
			},
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				handleIncoming,
			},
		},
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
			// log.Println("Локальный AutoPaho клиент подключен к брокеру MQTT")
			subs := []paho.SubscribeOptions{
				{Topic: "Client/ModuleInfo/#", QoS: 2},
			}
			if _, err := cm.Subscribe(context.Background(), &paho.Subscribe{Subscriptions: subs}); err != nil {
				log.Printf("Ошибка подписки: %v", err)
			}
		},
		OnConnectError: func(err error) {
			log.Printf("Ошибка подключения: %v", err)
		},
	}

	conn, err := autopaho.NewConnection(ctx, cfg)
	if err != nil {
		log.Fatalf("Начальное подключение не удалось: %v", err)
	}

	svc := &MQTTService{client: conn}

	// Запускает горутину для периодической очистки устаревших буферов
	go cleanupOldBuffers()

	Default = svc
	return svc
}

// ProcessChunk обрабатывает часть данных, сохраняет её в буфер и инициирует сборку файла при завершении
func processChunk(task chunkTask) {
	// Получает существующий буфер или создаёт новый
	val, _ := fileBuffers.LoadOrStore(task.fileKey, &FileBuffer{
		TotalChunks:   task.totalChunks,
		Received:      make([][]byte, task.totalChunks),
		ReceivedCount: 0,
		LastUpdated:   time.Now(),
	})

	fb := val.(*FileBuffer)
	fb.mu.Lock()
	defer fb.mu.Unlock()

	// Сбрасывает буфер, если метаданные файла изменились
	if fb.TotalChunks != task.totalChunks {
		log.Printf("Предупреждение: изменилось количество чанков для клиента %s: было %d, стало %d",
			task.fileKey, fb.TotalChunks, task.totalChunks)
		fb.TotalChunks = task.totalChunks
		fb.Received = make([][]byte, task.totalChunks)
		fb.ReceivedCount = 0
	}

	// Обновляет время, чтобы предотвратить преждевременное удаление по таймауту
	fb.LastUpdated = time.Now()

	// Игнорирует, если эта часть уже была получена
	if fb.Received[task.chunkNum] != nil {
		return
	}

	// ДЛЯ ОТЛАДКИ (проверка хэша каждой чанки)
	// hash := fmt.Sprintf("%x", md5.Sum(task.chunkData))
	// log.Printf("Чанк %d хеш: %s", task.chunkNum, hash)

	// Сохраняет полученную часть
	fb.Received[task.chunkNum] = task.chunkData
	fb.ReceivedCount++

	// Собирает файл, если все части получены
	if uint64(fb.ReceivedCount) == fb.TotalChunks {
		// log.Printf("Получено %d/%d чанков для %s", fb.ReceivedCount, fb.TotalChunks, task.fileKey)
		assembleFile(task.fileKey, fb)
		fileBuffers.Delete(task.fileKey) // Удаляет буфер сразу после успешной сборки
	}
}

// AssembleFile собирает все части из буфера в единый файл и сохраняет его на диск
func assembleFile(fileKey string, fb *FileBuffer) {
	// Объединяет все полученные части в один байтовый срез
	var fullFile []byte
	for _, chunk := range fb.Received {
		if chunk != nil {
			fullFile = append(fullFile, chunk...)
		}
	}

	// Формирует полный путь сохранения файла
	fileDir := pathsOS.Path_Info
	filePath := filepath.Join(fileDir, fileKey+".html.xz")

	// Создает директорию, если она ещё не существует
	if err := pathsOS.EnsureDir(fileDir); err != nil {
		log.Printf("Ошибка создания директории %s: %v", fileDir, err)
		return
	}

	// ДЛЯ ОТЛАДКИ (проверка хэша перед записью в файл)
	// hash := fmt.Sprintf("%x", md5.Sum(fullFile))
	// log.Printf("Хеш собранного файла перед записью: %s", hash)

	// Записывает собранный файл на диск
	if err := pathsOS.WriteFile(filePath, fullFile, pathsOS.FilePerm); err != nil {
		log.Printf("Ошибка сохранения файла %s: %v", filePath, err)
	} else {
		// log.Printf("Файл для клиента %s успешно собран и сохранён в %s (%d чанков, %d байт)", fileKey, filePath, fb.TotalChunks, len(fullFile))
	}

	// Очищает буферы памяти
	for i := range fb.Received {
		fb.Received[i] = nil
	}
}

// CleanupOldBuffers периодически удаляет буферы, которые не обновлялись дольше fileTimeout
func cleanupOldBuffers() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		fileBuffers.Range(func(key, value any) bool {
			fb := value.(*FileBuffer)
			if now.Sub(fb.LastUpdated) > fileTimeout {
				fileBuffers.Delete(key)
				log.Printf("Удалён буфер %v (таймаут)", key)
			}
			return true
		})
	}
}

// HandleIncoming обрабатывает входящие MQTT-сообщения, извлекая метаданные и передавая части файла для сборки
func handleIncoming(pr paho.PublishReceived) (bool, error) {
	packet := pr.Packet

	// Обрабатывает только сообщения, касающиеся передачи информации о модулях
	if !strings.HasPrefix(packet.Topic, "Client/ModuleInfo/") {
		return true, nil
	}

	// Разделяет топик для извлечения типа отчёта и clientID
	topicParts := strings.Split(packet.Topic, "/")
	if len(topicParts) != 4 || (topicParts[2] != "Lite" && topicParts[2] != "Aida") {
		log.Printf("Некорректный топик: %s", packet.Topic)
		return true, nil
	}
	typeStr := topicParts[2]
	clientID := topicParts[3]
	fileKey := typeStr + "_" + clientID

	// Проверяет минимальный размер, так как метаданные занимают 34 байта
	if len(packet.Payload) < 34 {
		log.Println("Некорректный размер чанка")
		return true, nil
	}

	// Парсит метаданные из начала payload
	flags := binary.LittleEndian.Uint16(packet.Payload[0:2])
	fileIDBytes := packet.Payload[2:18]
	_, err := uuid.FromBytes(fileIDBytes) // Проверяет корректность UUID
	if err != nil {
		log.Printf("Ошибка парсинга UUID: %v", err)
		return true, nil
	}

	// Извлекает номера частей и общее количество
	chunkNum := binary.LittleEndian.Uint64(packet.Payload[18:26])
	totalChunks := binary.LittleEndian.Uint64(packet.Payload[26:34])
	chunkData := packet.Payload[34:]

	// Отклоняет чанки с невалидными номерами или размером
	if totalChunks == 0 || totalChunks > 1e6 || chunkNum >= totalChunks || len(chunkData) == 0 {
		log.Printf("Некорректные данные чанка: fileKey=%v, chunk=%d/%d, size=%d",
			fileKey, chunkNum, totalChunks, len(chunkData))
		return true, nil
	}

	// Создает копию данных, чтобы избежать проблем с параллельной обработкой
	chunkDataCopy := make([]byte, len(chunkData))
	copy(chunkDataCopy, chunkData)

	// Передаёт задачу для асинхронной обработки и сохранения в буфер
	processChunk(chunkTask{
		fileKey:     fileKey,
		chunkNum:    chunkNum,
		totalChunks: totalChunks,
		chunkData:   chunkDataCopy,
		flags:       flags,
	})

	return true, nil
}

// Publish отправляет сообщение в указанный топик с заданным QoS (пакет-уровневая обёртка над Default клиентом)
func Publish(topic string, payload []byte, qos byte) error {
	if Default == nil {
		return fmt.Errorf("autopaho client not initialized")
	}
	_, err := Default.client.Publish(context.Background(), &paho.Publish{
		Topic:   topic,
		Payload: payload,
		QoS:     qos,
	})
	return err
}

// Publish отправляет сообщение в указанный топик с заданным QoS
func (svc *MQTTService) Publish(topic string, payload []byte, qos byte) error {
	_, err := svc.client.Publish(context.Background(), &paho.Publish{
		Topic:   topic,
		Payload: payload,
		QoS:     qos,
	})
	return err
}

// StopMQTTClient завершает соединение Default клиента и очищает все буферы
func StopMQTTClient() {
	if Default != nil {
		Default.StopMQTTClient()
	}

	// Очищает все оставшиеся буферы, чтобы освободить память
	fileBuffers.Range(func(key, value any) bool {
		fileBuffers.Delete(key)
		return true
	})
}

// StopMQTTClient завершает MQTT-соединение
func (svc *MQTTService) StopMQTTClient() {
	if svc.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()
		if err := svc.client.Disconnect(ctx); err != nil {
			log.Printf("Ошибка отключения MQTT: %v", err)
		} else {
			// log.Println("Локальный MQTT клиент AutoPaho отключён")
		}
	}
}
