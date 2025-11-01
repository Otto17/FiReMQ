// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"FiReMQ/db"          // Локальный пакет с БД BadgerDB
	"FiReMQ/mqtt_client" // Локальный пакет MQTT клиента AutoPaho
	"FiReMQ/pathsOS"     // Локальный пакет с путями для разных платформ

	"github.com/dgraph-io/badger/v4"
	"github.com/quic-go/quic-go"
)

const (
	// Статусы ошибок протокола QUIC
	statusOK  byte = 0 // Статус 0 - ОК
	statusErr byte = 1 // Статус 1 - Ошибка

	// Коды ошибок протокола QUIC
	ErrInvalidToken    uint16 = 1 // Неверный или просроченный токен
	ErrSessionNotFound uint16 = 2 // Не найдена сессия по токену
	ErrEmptyFileName   uint16 = 3 // В сессии не указано имя файла
	ErrFileOpen        uint16 = 4 // Файл отсутствует или недоступен на сервере
	ErrFileStat        uint16 = 5 // Ошибка получения информации о файле
	ErrBadOffset       uint16 = 6 // Смещение превышает размер файла
)

// SessionInfo содержит информацию о сеансе QUIC-клиента
type SessionInfo struct {
	Token          string        // Уникальный токен сессии
	Created        time.Time     // Время создания сессии
	Active         bool          // Указывает на активную передачу файлов
	Cancel         chan struct{} // Канал для отмены удаления
	FileName       string
	DateOfCreation string
}

// Глобальное хранилище сеансов и мьютексов QUIC-клиентов
var (
	sessionStore = make(map[string]SessionInfo)
	sessionMutex sync.Mutex
)

// QuicAccessManager управляет состоянием QUIC-сервера (открытие/закрытие UDP порта)
type quicAccessManager struct {
	mu        sync.Mutex
	isOpen    bool
	tlsConfig *tls.Config
	addr      string
	udpConn   *net.UDPConn
	listener  *quic.Listener
	ctx       context.Context
	acceptWG  sync.WaitGroup

	// Управление отложенным закрытием
	closeTimer *time.Timer
	grace      time.Duration
}

var quicMgr *quicAccessManager // Глобальный менеджер QUIC-сервера

// Очередь отправки по клиенту
type clientSendQueue struct {
	mu       sync.Mutex
	running  bool
	lastSend time.Time
}

var quicSendQueues sync.Map // key: clientID -> *clientSendQueue

// Интервал между отправками запросов одному клиенту
const quicQueueInterval = 20 * time.Second

// Срок жизни одноразового, индивидуального токена
const TokenTTL = 10 * time.Second

// StartQUICServer запускает и держит QUIC-сервер до отмены ctx
func StartQUICServer(ctx context.Context) {
	clientCACert, err := os.ReadFile(filepath.Join(pathsOS.Path_Client_QUIC_CA))
	if err != nil {
		log.Printf("Не удалось прочитать клиентский CA сертифкат: %v", err)
		return
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(pathsOS.Path_Server_QUIC_Cert), filepath.Join(pathsOS.Path_Server_QUIC_Key))
	if err != nil {
		log.Printf("Не удалось загрузить серверный сертификат: %v", err)
		return
	}
	clientCAPool := x509.NewCertPool()
	if !clientCAPool.AppendCertsFromPEM(clientCACert) {
		log.Println("Не удалось добавить CA сертификат")
		return
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAPool,
		NextProtos:   []string{"quic-file-transfer"},
	}

	// Инициализируем менеджер доступа
	quicMgr = &quicAccessManager{
		tlsConfig: tlsConfig,
		addr:      pathsOS.QUIC_Host + ":" + pathsOS.QUIC_Port,
		grace:     5 * time.Second, // Задержка перед закрытием
	}

	// Блокирующий запуск менеджера (до отмены ctx)
	quicMgr.run(ctx)
}

// HandleQUICConnection обрабатывает входящее QUIC-соединение
func handleQUICConnection(conn *quic.Conn) {
	var mqttID string
	var shouldDeleteSession bool = true
	defer func() {
		if mqttID != "" && shouldDeleteSession {
			sessionMutex.Lock()
			if session, exists := sessionStore[mqttID]; exists && session.Cancel != nil {
				close(session.Cancel)
			}
			delete(sessionStore, mqttID)
			sessionMutex.Unlock()
			log.Printf("Сессия для %s удалена (ошибка или отсутствие подтверждения)", mqttID)
		}
	}()

	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		log.Printf("Ошибка при открытии потока: %v", err)
		return
	}
	defer stream.Close()

	// Чтение токена
	var tokenLen uint16
	if err := binary.Read(stream, binary.BigEndian, &tokenLen); err != nil {
		log.Printf("Ошибка при чтении длины токена: %v", err)
		return
	}
	tokenBytes := make([]byte, tokenLen)
	if _, err := io.ReadFull(stream, tokenBytes); err != nil {
		log.Printf("Ошибка при чтении токена: %v", err)
		return
	}
	token := string(tokenBytes)
	log.Printf("Получен токен: %s для проверки mqttID: %s", token, mqttID) // ДЛЯ ОТЛАДКИ

	// Чтение mqttID
	var mqttIDLen uint16
	if err := binary.Read(stream, binary.BigEndian, &mqttIDLen); err != nil {
		log.Printf("Ошибка при чтении длины mqttID: %v", err)
		return
	}
	mqttIDBytes := make([]byte, mqttIDLen)
	if _, err := io.ReadFull(stream, mqttIDBytes); err != nil {
		log.Printf("Ошибка при чтении mqttID: %v", err)
		return
	}
	mqttID = string(mqttIDBytes)

	// Чтение смещения
	var resumeFrom uint64
	if err := binary.Read(stream, binary.BigEndian, &resumeFrom); err != nil {
		log.Printf("Ошибка при чтении смещения: %v", err)
		return
	}
	log.Printf("Получено смещение resumeFrom=%d для mqttID=%s", resumeFrom, mqttID)

	// Проверка токена
	if !validateQUICToken(token, mqttID) {
		_ = sendProtoError(stream, ErrInvalidToken, "Недопустимый токен или mqttID")
		return
	}

	// Поиск сессии по токену
	sessionMutex.Lock()
	sess, ok := sessionStore[mqttID]
	sessionMutex.Unlock()
	if !ok || sess.Token != token {
		_ = sendProtoError(stream, ErrSessionNotFound, "Сессия по токену не найдена")
		return
	}

	// Получение имени файла
	fileName := sess.FileName
	if strings.TrimSpace(fileName) == "" {
		_ = sendProtoError(stream, ErrEmptyFileName, "В сессии нет имени файла")
		return
	}

	// Передача файла через QUIC протокол
	filePath := filepath.Join(pathsOS.Path_QUIC_Downloads, fileName)
	file, err := os.Open(filePath)
	if err != nil {
		_ = sendProtoError(stream, ErrFileOpen, "Файл на сервере отсутствует или недоступен")
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		_ = sendProtoError(stream, ErrFileStat, "Ошибка получения информации о файле")
		return
	}
	fileSize := uint64(fileInfo.Size())

	// Проверка корректности resumeFrom
	if resumeFrom > fileSize {
		_ = sendProtoError(stream, ErrBadOffset, "Недопустимое смещение")
		return
	}

	// Перед метаданными шлём статус OK
	if err := binary.Write(stream, binary.BigEndian, statusOK); err != nil {
		log.Printf("Ошибка отправки статуса: %v", err)
		return
	}

	// Всегда отправляем метаданные файла (имя и размер)
	fileNameBytes := []byte(fileName)
	if err := binary.Write(stream, binary.BigEndian, uint16(len(fileNameBytes))); err != nil {
		log.Printf("Ошибка при отправке длины имени файла: %v", err)
		return
	}
	if _, err := stream.Write(fileNameBytes); err != nil {
		log.Printf("Ошибка при отправке имени файла: %v", err)
		return
	}
	if err := binary.Write(stream, binary.BigEndian, fileSize); err != nil {
		log.Printf("Ошибка при отправке размера файла: %v", err)
		return
	}

	// Перемещение к указанному смещению
	if _, err := file.Seek(int64(resumeFrom), 0); err != nil {
		log.Printf("Ошибка при установке смещения: %v", err)
		return
	}
	log.Printf("Отправка файла: %s, начиная с %d байт", fileName, resumeFrom)

	// Определение размера буфера
	bufSize := getBufferSize(fileSize, resumeFrom)
	buf := make([]byte, bufSize)
	log.Printf("Используется буфер %d КБ для файла %s", bufSize/1024, fileName)

	var sent uint64 = resumeFrom
	for sent < fileSize {
		n, err := file.Read(buf)
		if err != nil && err != io.EOF {
			log.Printf("Ошибка при чтении файла: %v", err)
			return
		}
		if n == 0 {
			break
		}
		if _, wErr := stream.Write(buf[:n]); wErr != nil {
			log.Printf("Ошибка при отправке данных: %v", wErr)
			return
		}
		sent += uint64(n)
	}
	log.Printf("Файл %s отправлен полностью: %d байт", fileName, sent)
	shouldDeleteSession = false // Ожидаем подтверждение от клиента
}

// GetBufferSize адаптивное определение размера буфера
func getBufferSize(fileSize, resumeFrom uint64) int {
	remaining := fileSize - resumeFrom
	// Для файлов < 1 MB используем буфер 16 КБ
	if remaining < 1<<20 { // 1 MB
		return 16 << 10 // 16 КБ
	}
	// Для файлов > 100 MB используем буфер 256 КБ
	if remaining > 100<<20 { // 100 MB
		return 256 << 10 // 256 КБ
	}
	// По умолчанию 64 КБ
	return 64 << 10
}

// ValidateQUICToken проверяет одноразовый токен и устанавливает активный флаг
func validateQUICToken(token, mqttID string) bool {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	session, exists := sessionStore[mqttID]
	// Срок жизни одноразового, индивидуального токена
	if exists && session.Token == token && time.Since(session.Created) < TokenTTL {
		session.Active = true
		sessionStore[mqttID] = session
		return true
	}
	log.Printf("Недействительный токен %s для mqttID %s", token, mqttID)
	return false
}

// GenerateToken генерирует уникальный токен
func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Printf("Ошибка генерации токена: %v", err)
		return ""
	}
	return hex.EncodeToString(b)
}

// CheckAndResendQUIC запускает per-client очередь
func checkAndResendQUIC(clientID string) {
	// Ждем 3 секунды, чтобы клиент успел корректно запуститься
	time.Sleep(3 * time.Second)
	EnsureQUICOpen("фоновая повторная отправка для " + clientID)
	startQUICQueueForClient(clientID)
}

// HandleQUICAnswerMessage обрабатывает ответы клиентов и обновляет BadgerDB
func HandleQUICAnswerMessage(clientID, dateOfCreation, answer, quicExecution, attempts, description string) {
	dbKey := "FiReMQ_QUIC:" + dateOfCreation
	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(dbKey))
		if err != nil {
			return err
		}
		var record map[string]any
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &record)
		}); err != nil {
			return err
		}
		clientMapping, ok := record["ClientID_QUIC"].(map[string]any)
		if !ok {
			return nil
		}
		clientEntry, ok := clientMapping[clientID].(map[string]any)
		if !ok {
			clientEntry = make(map[string]any)
		}
		clientEntry["Answer"] = answer
		if strings.TrimSpace(quicExecution) != "" {
			clientEntry["QUIC_Execution"] = quicExecution
		}
		clientEntry["Attempts"] = attempts
		clientEntry["Description"] = description
		clientMapping[clientID] = clientEntry
		record["ClientID_QUIC"] = clientMapping
		updatedBytes, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return txn.Set([]byte(dbKey), updatedBytes)
	})
	if err != nil {
		log.Printf("Ошибка обновления QUIC-ответа для клиента %s: %v", clientID, err)
	}

	sessionMutex.Lock()
	if s, ok := sessionStore[clientID]; ok && s.DateOfCreation == dateOfCreation {
		if s.Cancel != nil {
			close(s.Cancel)
		}
		delete(sessionStore, clientID)
	}
	sessionMutex.Unlock()

	// После обновления ответа — пересчитываем доступ
	RecalculateQUICAccess("получен ответ от клиента " + clientID)
}

// Отменить отложенное закрытие (должно вызываться под m.mu)
func (m *quicAccessManager) cancelCloseTimerLocked() {
	if m.closeTimer != nil {
		m.closeTimer.Stop()
		m.closeTimer = nil
	}
}

// Open инициализирует QUIC-сервер и начинает принимать соединения
func (m *quicAccessManager) open(why string) {
	// Открытие порта, только, если есть незавершённые задачи и хотя бы один клиент онлайн
	ready, err := hasReadyQUICTasks()
	if err != nil {
		log.Printf("QUIC: ошибка проверки готовности к открытию: %v", err)
	}
	if !ready {
		// log.Printf("QUIC: не открываем порт — нет онлайн клиентов с незавершёнными задачами (%s)", why)
		return
	}

	m.mu.Lock()
	if m.isOpen {
		// Уже открыт — просто отменим возможное отложенное закрытие
		m.cancelCloseTimerLocked()
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	udpAddr, err := net.ResolveUDPAddr("udp", m.addr)
	if err != nil {
		log.Printf("QUIC: не удалось резолвить адрес %s: %v", m.addr, err)
		return
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Printf("QUIC: не удалось слушать UDP на %s: %v", m.addr, err)
		return
	}
	listener, err := quic.Listen(udpConn, m.tlsConfig, &quic.Config{})
	if err != nil {
		log.Printf("QUIC: не удалось запустить listener: %v", err)
		udpConn.Close()
		return
	}

	m.mu.Lock()
	m.udpConn = udpConn
	m.listener = listener
	m.isOpen = true
	m.cancelCloseTimerLocked() // на всякий случай
	m.mu.Unlock()

	log.Printf("QUIC-сервер запущен на %s (доступ разрешён: %s)", m.addr, why)
	m.acceptWG.Add(1)
	go m.acceptLoop()
}

// AcceptLoop обрабатывает входящие QUIC-соединения в отдельной горутине
func (m *quicAccessManager) acceptLoop() {
	defer m.acceptWG.Done()
	for {
		conn, err := m.listener.Accept(m.ctx)
		if err != nil {
			if m.ctx.Err() != nil {
				log.Println("QUIC-сервер остановлен (ctx cancel)")
				return
			}
			// listener закрыт — выходим из acceptLoop
			return
		}
		go handleQUICConnection(conn)
	}
}

// Close безопасно останавливает QUIC-сервер и освобождает ресурсы
func (m *quicAccessManager) close(why string) {
	m.mu.Lock()
	if !m.isOpen {
		m.cancelCloseTimerLocked()
		m.mu.Unlock()
		return
	}
	listener := m.listener
	udpConn := m.udpConn
	m.listener = nil
	m.udpConn = nil
	m.isOpen = false
	m.cancelCloseTimerLocked()
	m.mu.Unlock()

	if listener != nil {
		_ = listener.Close()
	}
	if udpConn != nil {
		_ = udpConn.Close()
	}
	m.acceptWG.Wait()
	log.Printf("QUIC доступ запрещён (%s); порт не слушается", why)
}

// ScheduleClose отложенное закрытие с повторной проверкой готовности
func (m *quicAccessManager) scheduleClose(why string) {
	m.mu.Lock()
	if !m.isOpen {
		m.mu.Unlock()
		return
	}
	m.cancelCloseTimerLocked()
	d := m.grace
	m.closeTimer = time.AfterFunc(d, func() {
		// Повторная проверка — вдруг кто-то успел зайти онлайн
		ready, err := hasReadyQUICTasks()
		if err != nil {
			log.Printf("QUIC: ошибка перепроверки перед закрытием: %v", err)
			return
		}
		if ready {
			// Кто-то онлайн с незавершённой задачей — оставляем порт открытым
			return
		}
		// Проверяем, остались ли вообще невыполненные задания
		hasPending, err := hasPendingQUICTasks()
		if err != nil {
			log.Printf("QUIC: ошибка проверки активных задач перед закрытием: %v", err)
		}
		if hasPending {
			m.close("закрытие после grace-периода: " + why)
		} else {
			m.close("закрытие: нет активных задач")
		}
	})
	m.mu.Unlock()
}

// Run управляет жизненным циклом QUIC-сервера (запуск/остановка по контексту)
func (m *quicAccessManager) run(ctx context.Context) {
	m.ctx = ctx
	if ready, err := hasReadyQUICTasks(); err != nil {
		//log.Printf("QUIC: ошибка первичной проверки задач: %v", err)
	} else if ready {
		m.open("startup: есть невыполненные задачи и онлайн-клиенты")
	} else {
		if has, _ := hasPendingQUICTasks(); has {
			//log.Printf("QUIC: доступ закрыт (startup) — есть задачи, но все целевые клиенты офлайн")
		} else {
			//log.Printf("QUIC: доступ закрыт (startup) — нет активных задач")
		}
	}
	<-ctx.Done()
	m.close("shutdown")
}

// HasPendingQUICTasks ппроверяет, есть ли невыполненные задания (возвращает true, если хотя бы один клиент с пустым "Answer")
func hasPendingQUICTasks() (bool, error) {
	var found bool
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_QUIC:")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var record map[string]any
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			}); err != nil {
				continue
			}
			mapping, ok := record["ClientID_QUIC"].(map[string]any)
			if !ok {
				continue
			}
			for _, v := range mapping {
				ce, _ := v.(map[string]any)
				if ce == nil {
					continue
				}
				ans, _ := ce["Answer"].(string)
				if strings.TrimSpace(ans) == "" {
					found = true
					return nil
				}
			}
		}
		return nil
	})
	return found, err
}

// EnsureQUICOpen принудительно открывает UDP QUIC-порт
func EnsureQUICOpen(why string) {
	if quicMgr == nil {
		log.Printf("EnsureQUICOpen: менеджер не инициализирован (%s)", why)
		return
	}
	quicMgr.open(why)
}

// EnsureQUICClosed принудительно закрывает UDP QUIC-порт
func EnsureQUICClosed(why string) {
	if quicMgr == nil {
		return
	}
	quicMgr.close(why)
}

// RecalculateQUICAccess пересчитывает необходимость открытия UDP порта
func RecalculateQUICAccess(why string) {
	if quicMgr == nil {
		return
	}
	ready, err := hasReadyQUICTasks()
	if err != nil {
		log.Printf("RecalculateQUICAccess: ошибка проверки готовности: %v", err)
		return
	}
	if ready {
		quicMgr.open(why)
		return
	}
	has, err := hasPendingQUICTasks()
	if err != nil {
		log.Printf("RecalculateQUICAccess: ошибка проверки активных задач: %v", err)
		return
	}
	if has {
		// Есть задачи, но онлайн-клиентов под них нет — закрываем с задержкой
		quicMgr.scheduleClose("нет онлайн-клиентов для активных задач (" + why + ")")
	} else {
		// Задач нет — закрываем сразу
		quicMgr.close("нет активных задач (" + why + ")")
	}
}

// ExtractFileNameFromQUICRecord извлекает имя файла из поля "QUIC_Command" записи
func extractFileNameFromQUICRecord(record map[string]any) (string, error) {
	quicStr, ok := record["QUIC_Command"].(string)
	if !ok || quicStr == "" {
		return "", fmt.Errorf("QUIC_Command отсутствует")
	}
	var payload QUICPayload
	if err := json.Unmarshal([]byte(quicStr), &payload); err != nil {
		return "", err
	}
	if payload.DownloadRunPath == "" {
		return "", fmt.Errorf("DownloadRunPath пуст")
	}
	return baseNameAnyOS(payload.DownloadRunPath), nil
}

// IsQUICFileStillReferenced проверяет, остались ли записи QUIC, использующие файл из "Path_QUIC_Downloads"
func isQUICFileStillReferenced(fileName string) (bool, error) {
	var referenced bool
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_QUIC:")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var record map[string]any
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			}); err != nil {
				continue
			}
			otherName, err := extractFileNameFromQUICRecord(record)
			if err != nil {
				continue
			}
			if otherName == filepath.Base(fileName) {
				referenced = true
				return nil
			}
		}
		return nil
	})
	return referenced, err
}

// DeleteQUICFileIfUnreferenced удаляет файл из папки "Path_QUIC_Downloads", если он больше не используется никакими записями
func deleteQUICFileIfUnreferenced(fileName string) {
	fileName = filepath.Base(strings.TrimSpace(fileName))
	if fileName == "" {
		return
	}
	referenced, err := isQUICFileStillReferenced(fileName)
	if err != nil {
		log.Printf("Проверка ссылок на файл %s завершилась ошибкой: %v", fileName, err)
		return
	}
	if referenced {
		log.Printf("Файл %s не удалён — всё ещё используется другими запросами", fileName)
		return
	}
	downloadsDir := pathsOS.Path_QUIC_Downloads
	filePath := filepath.Join(downloadsDir, fileName)
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			log.Printf("Файл %s отсутствует — удаление не требуется", filePath)
			break
		}
		if err := os.Remove(filePath); err != nil {
			log.Printf("Попытка %d: ошибка удаления файла %s: %v", i+1, filePath, err)
			if i < maxRetries-1 {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// Последняя попытка — просто логируем
			log.Printf("Не удалось удалить файл %s окончательно: %v", filePath, err)
			return
		}
		log.Printf("Файл %s удалён (очистка после удаления запроса)", filePath)
		break
	}
}

// GenerateQUICTokenForFile выполняет генерацию токена с привязкой к файлу
func generateQUICTokenForFile(mqttID, filePath, dateOfCreation string) string {
	token := generateToken()
	cancel := make(chan struct{})
	info := SessionInfo{
		Token:          token,
		Created:        time.Now(),
		Active:         false,
		Cancel:         cancel,
		FileName:       baseNameAnyOS(filePath),
		DateOfCreation: dateOfCreation,
	}
	sessionMutex.Lock()
	if old, exists := sessionStore[mqttID]; exists && old.Cancel != nil {
		close(old.Cancel)
	}
	sessionStore[mqttID] = info
	sessionMutex.Unlock()

	go func(tok string, c chan struct{}, createdAt string, client string) {
		select {
		// По истечению TTL токен удаляется из БД
		case <-time.After(TokenTTL):
			var snap SessionInfo
			var expired bool
			sessionMutex.Lock()
			if s, ok := sessionStore[client]; ok && !s.Active && s.Token == tok {
				snap = s
				delete(sessionStore, client)
				expired = true
			}
			sessionMutex.Unlock()
			if expired {
				log.Printf("Срок действия токена истек для %s", client)
				// Отмечаем флаг для будущей отправки
				if snap.DateOfCreation != "" {
					_ = setResendRequestedFor(client, snap.DateOfCreation)
				}
			}
		case <-c:
			// Отменено
		}
	}(token, cancel, dateOfCreation, mqttID)
	return token
}

// GetPendingQUICClientIDs собирает список клиентов, у которых есть незавершённые (Answer == "") QUIC-задачи
func getPendingQUICClientIDs() ([]string, error) {
	ids := make(map[string]struct{})
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_QUIC:")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var record map[string]any
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			}); err != nil {
				continue
			}
			mapping, ok := record["ClientID_QUIC"].(map[string]any)
			if !ok {
				continue
			}
			for cid, v := range mapping {
				ce, _ := v.(map[string]any)
				if ce == nil {
					continue
				}
				ans, _ := ce["Answer"].(string)
				if strings.TrimSpace(ans) == "" {
					ids[cid] = struct{}{}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out, nil
}

// IsAnyOfClientsOnline проверяет, есть ли среди переданных QUIC-клиентов хоть один онлайн
func isAnyOfClientsOnline(ids []string) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	var found bool
	err := db.DBInstance.View(func(txn *badger.Txn) error {
		for _, id := range ids {
			item, err := txn.Get([]byte("client:" + id))
			if err != nil {
				continue
			}
			var data map[string]string
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &data)
			}); err != nil {
				continue
			}
			if data["status"] == "On" {
				found = true
				return nil
			}
		}
		return nil
	})
	return found, err
}

// HasReadyQUICTasks проверяет, есть ли незавершённые задачи для онлайн QUIC-клиентов
func hasReadyQUICTasks() (bool, error) {
	ids, err := getPendingQUICClientIDs()
	if err != nil {
		return false, err
	}
	return isAnyOfClientsOnline(ids)
}

// SetResendRequestedFor выставляет флаг ResendRequested[clientID]=true для конкретной записи (если Answer пуст)
func setResendRequestedFor(clientID, dateOfCreation string) bool {
	dbKey := "FiReMQ_QUIC:" + dateOfCreation
	var changed bool
	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(dbKey))
		if err != nil {
			return nil
		}
		var record map[string]any
		if err := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &record)
		}); err != nil {
			return nil
		}
		mapping, ok := record["ClientID_QUIC"].(map[string]any)
		if !ok {
			return nil
		}
		ce, _ := mapping[clientID].(map[string]any)
		if ce == nil {
			return nil
		}
		if ans, _ := ce["Answer"].(string); strings.TrimSpace(ans) != "" {
			return nil
		}
		rr, _ := record["ResendRequested"].(map[string]any)
		if rr == nil {
			rr = make(map[string]any)
		}
		if rrb, _ := rr[clientID].(bool); !rrb {
			rr[clientID] = true
			record["ResendRequested"] = rr
			newBytes, err := json.Marshal(record)
			if err != nil {
				return nil
			}
			if err := txn.Set([]byte(dbKey), newBytes); err != nil {
				return err
			}
			changed = true
		}
		return nil
	})
	if err != nil {
		log.Printf("Ошибка установки ResendRequested для клиента %s (%s): %v", clientID, dateOfCreation, err)
	}
	return changed
}

// MarkQUICResendOnOffline — при переходе клиента в офлайн, отмечает ResendRequested для всех его незавершённых задач
func markQUICResendOnOffline(clientID string) {
	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_QUIC:")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var record map[string]any
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			}); err != nil {
				continue
			}
			mapping, ok := record["ClientID_QUIC"].(map[string]any)
			if !ok {
				continue
			}
			ce, _ := mapping[clientID].(map[string]any)
			if ce == nil {
				continue
			}
			if ans, _ := ce["Answer"].(string); strings.TrimSpace(ans) != "" {
				continue
			}
			rr, _ := record["ResendRequested"].(map[string]any)
			if rr == nil {
				rr = make(map[string]any)
			}
			rr[clientID] = true
			record["ResendRequested"] = rr
			newBytes, err := json.Marshal(record)
			if err != nil {
				continue
			}
			if err := txn.Set(item.KeyCopy(nil), newBytes); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("Ошибка отметки ResendRequested при оффлайне для %s: %v", clientID, err)
	}
}

// IsQUICActive проверяет, активна ли сейчас передача по QUIC у клиента
func isQUICActive(clientID string) bool {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	s, ok := sessionStore[clientID]
	return ok && s.Active
}

// IsQUICActiveFor проверяет активен ли конкретный запрос
func isQUICActiveFor(clientID, dateOfCreation string) bool {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	s, ok := sessionStore[clientID]
	return ok && s.Active && s.DateOfCreation == dateOfCreation
}

// ParseQUICDate парсер даты
func parseQUICDate(s string) time.Time {
	idx := strings.LastIndex(s, ":")
	if idx == -1 {
		if t, err := time.Parse("02.01.06(15:04:05)", s); err == nil {
			return t
		}
		return time.Time{}
	}
	base := s[:idx]
	msStr := s[idx+1:]
	t, err := time.Parse("02.01.06(15:04:05)", base)
	if err != nil {
		return time.Time{}
	}
	ms, _ := strconv.Atoi(msStr)
	return t.Add(time.Duration(ms) * time.Millisecond)
}

// PrepareNextQUICMessage подготавливает следующие к отправке записи (начиная с самой старой)
func prepareNextQUICMessage(clientID string) (topic string, payload []byte, ok bool) {
	var (
		chosenKey    []byte
		chosenRecord map[string]any
		chosenDate   string
		chosenTime   time.Time
		choose       bool
		outPayload   []byte
		outTopic     string
	)

	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_QUIC:")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var record map[string]any
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			}); err != nil {
				continue
			}
			mapping, ok := record["ClientID_QUIC"].(map[string]any)
			if !ok {
				continue
			}
			ce, _ := mapping[clientID].(map[string]any)
			if ce == nil {
				continue
			}
			ans, _ := ce["Answer"].(string)
			if strings.TrimSpace(ans) != "" {
				continue
			}
			// eligible: (ещё не отправляли) ИЛИ (стоит флаг ResendRequested)
			alreadySent := false
			if sentForAny, ok := record["SentFor"].([]any); ok {
				for _, v := range sentForAny {
					if s, ok := v.(string); ok && s == clientID {
						alreadySent = true
						break
					}
				}
			}
			rr := false
			if rrMap, ok := record["ResendRequested"].(map[string]any); ok {
				if b, ok := rrMap[clientID].(bool); ok && b {
					rr = true
				}
			}
			if alreadySent && !rr {
				continue
			}

			dateStr, _ := record["Date_Of_Creation"].(string)
			t := parseQUICDate(dateStr)
			if !choose || (!t.IsZero() && t.Before(chosenTime)) || (chosenTime.IsZero() && !t.IsZero()) {
				choose = true
				chosenKey = item.KeyCopy(nil)
				chosenRecord = record
				chosenDate = dateStr
				chosenTime = t
			}
		}
		if !choose {
			return nil
		}

		// Готовим payload с новым токеном
		payloadStr, okk := chosenRecord["QUIC_Command"].(string)
		if !okk || payloadStr == "" {
			return nil
		}
		var p QUICPayload
		if err := json.Unmarshal([]byte(payloadStr), &p); err != nil {
			return nil
		}
		p.Token = generateQUICTokenForFile(clientID, p.DownloadRunPath, chosenDate)
		buf, err := json.Marshal(p)
		if err != nil {
			return err
		}

		// Обновим SentFor
		var sentFor []string
		if s, ok := chosenRecord["SentFor"]; ok {
			if arr, ok := s.([]any); ok {
				for _, vv := range arr {
					if ss, ok := vv.(string); ok {
						sentFor = append(sentFor, ss)
					}
				}
			}
		}
		found := false
		for _, s := range sentFor {
			if s == clientID {
				found = true
				break
			}
		}
		if !found {
			sentFor = append(sentFor, clientID)
			chosenRecord["SentFor"] = sentFor
		}

		// Снимем флаг ResendRequested для этого клиента
		if rrMap, ok := chosenRecord["ResendRequested"].(map[string]any); ok {
			if _, ex := rrMap[clientID]; ex {
				delete(rrMap, clientID)
				chosenRecord["ResendRequested"] = rrMap
			}
		}

		// Сохраняем изменения
		newBytes, err := json.Marshal(chosenRecord)
		if err != nil {
			return err
		}
		if err := txn.Set(chosenKey, newBytes); err != nil {
			return err
		}

		outPayload = buf
		outTopic = "Client/" + clientID + "/ModuleQUIC"
		return nil
	})
	if err != nil || !choose {
		return "", nil, false
	}
	return outTopic, outPayload, true
}

// StartQUICQueueForClient производит запуск очереди для клиента
func startQUICQueueForClient(clientID string) {
	val, _ := quicSendQueues.LoadOrStore(clientID, &clientSendQueue{})
	q := val.(*clientSendQueue)
	q.mu.Lock()
	if q.running {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.mu.Unlock()

	go func() {
		defer func() {
			q.mu.Lock()
			q.running = false
			q.mu.Unlock()
		}()
		for {
			// Если клиент ушёл оффлайн, завершаем (перезапустится при следующем Online)
			online, _ := isClientOnline(clientID)
			if !online {
				return
			}
			// Не шлём, пока идёт активная передача у клиента
			for isQUICActive(clientID) {
				time.Sleep(1 * time.Second)
				if o, _ := isClientOnline(clientID); !o {
					return
				}
			}
			// Интервал между отправками
			q.mu.Lock()
			now := time.Now()
			wait := quicQueueInterval - now.Sub(q.lastSend)
			q.mu.Unlock()
			if wait > 0 {
				time.Sleep(wait)
			}
			// Готовим следующую подходящую запись (самую старую)
			topic, payload, ok := prepareNextQUICMessage(clientID)
			if !ok {
				return // Нечего слать
			}
			EnsureQUICOpen("очередь QUIC — отправка клиенту " + clientID)
			if err := mqtt_client.Publish(topic, payload, 2); err != nil {
				log.Printf("QUIC очередь: ошибка публикации для %s: %v", clientID, err)
				time.Sleep(3 * time.Second)
				continue
			}
			// Отметим момент отправки
			q.mu.Lock()
			q.lastSend = time.Now()
			q.mu.Unlock()
		}
	}()
}

// SendProtoError отправляет клиенту статус "ошибка" (statusErr), затем код ошибки и текстовое сообщение, закрывая поток для гарантированной доставки.
func sendProtoError(stream *quic.Stream, code uint16, msg string) error {
	if err := binary.Write(stream, binary.BigEndian, statusErr); err != nil {
		return err
	}
	if err := binary.Write(stream, binary.BigEndian, code); err != nil {
		return err
	}
	if err := binary.Write(stream, binary.BigEndian, uint16(len(msg))); err != nil {
		return err
	}
	_, err := stream.Write([]byte(msg))
	_ = stream.Close() // Полузакрыть запись, чтобы клиент гарантированно получил ошибку
	return err
}
