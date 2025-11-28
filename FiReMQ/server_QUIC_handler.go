// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"FiReMQ/db"          // Локальный пакет с БД BadgerDB
	"FiReMQ/mqtt_client" // Локальный пакет MQTT клиента AutoPaho
	"FiReMQ/pathsOS"     // Локальный пакет с путями для разных платформ

	"github.com/dgraph-io/badger/v4"
	"github.com/zeebo/xxh3"
)

// HashResult структура для хранения хеша и канала отмены
type HashResult struct {
	hash   string        // Для хранения вычисленной хеш-суммы "XXH3"
	cancel chan struct{} // Для сигнала отмены
}

// Временный буфер для хранения хеш-суммы
var hashMap sync.Map

// Путь по умолчанию, используется только для наглядности, при GET ответе в функции "GetQUICReportHandler"
const defaultClientDownloadPath = "C:\\ProgramData\\FiReAgent\\Files"

// InstallProgramRequest структура конечного JSON для отправки конкретным клиентам
type InstallProgramRequest struct {
	ClientIDs                     []string `json:"client_ids"`
	OnlyDownload                  bool     `json:"OnlyDownload"`
	DownloadRunPath               string   `json:"DownloadRunPath"`
	ProgramRunArguments           string   `json:"ProgramRunArguments"`
	RunWhetherUserIsLoggedOnOrNot bool     `json:"RunWhetherUserIsLoggedOnOrNot"`
	UserName                      string   `json:"UserName"`
	UserPassword                  string   `json:"UserPassword"`
	RunWithHighestPrivileges      bool     `json:"RunWithHighestPrivileges"`
	NotDeleteAfterInstallation    bool     `json:"NotDeleteAfterInstallation"`
	XXH3                          string   `json:"XXH3,omitempty"`
}

// QUICPayload структура для формирования JSON с нужным порядком полей
type QUICPayload struct {
	DateOfCreation                string `json:"Date_Of_Creation"`
	OnlyDownload                  bool   `json:"OnlyDownload"`
	DownloadRunPath               string `json:"DownloadRunPath"`
	ProgramRunArguments           string `json:"ProgramRunArguments"`
	RunWhetherUserIsLoggedOnOrNot bool   `json:"RunWhetherUserIsLoggedOnOrNot"`
	UserName                      string `json:"UserName"`
	UserPassword                  string `json:"UserPassword"`
	RunWithHighestPrivileges      bool   `json:"RunWithHighestPrivileges"`
	NotDeleteAfterInstallation    bool   `json:"NotDeleteAfterInstallation"`
	XXH3                          string `json:"XXH3"`
	Token                         string `json:"Token"`
}

// UploadFileHandler обрабатывает POST-запрос для загрузки файла на сервер
func UploadFileHandler(w http.ResponseWriter, r *http.Request) {
	// Проверка метода запроса
	if r.Method != http.MethodPost {
		sendErrorResponse(w, http.StatusMethodNotAllowed, "Разрешены только POST запросы")
		return
	}

	// Создаём директорию для загрузки исполняемых файлов, если её нет
	if err := pathsOS.EnsureDir(pathsOS.Path_QUIC_Downloads); err != nil {
		sendErrorResponse(w, http.StatusInternalServerError, "Ошибка создания папки для загрузки исполняемых файлов QUIC-сервера")
		return
	}

	// Получение multipart reader для обработки файла
	reader, err := r.MultipartReader()
	if err != nil {
		sendErrorResponse(w, http.StatusBadRequest, "Ошибка получения multipart reader при загрузке на сервер")
		return
	}

	// Обработка частей multipart формы
	var fileName string
	var tempFile *os.File
	var tempFilePath string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			sendErrorResponse(w, http.StatusInternalServerError, "Ошибка чтения части файла при загрузке на сервер")
			return
		}
		if part.FormName() == "file" {
			fileName = baseNameAnyOS(part.FileName())
			tempFile, err = os.CreateTemp(pathsOS.Path_QUIC_Downloads, "upload-")
			if err != nil {
				sendErrorResponse(w, http.StatusInternalServerError, "Ошибка создания временного файла при загрузке на сервер")
				return
			}
			tempFilePath = tempFile.Name()
			// Инициализируем хеш-функцию методом "XXH3"
			hash := xxh3.New()
			// Создаем MultiWriter для одновременной записи в файл и хеш
			multiWriter := io.MultiWriter(tempFile, hash)
			// Копируем данные в MultiWriter
			if _, err := io.Copy(multiWriter, part); err != nil {
				tempFile.Close()
				os.Remove(tempFilePath)
				sendErrorResponse(w, http.StatusInternalServerError, "Ошибка копирования файла в MultiWriter")
				return
			}
			tempFile.Close()
			// Перемещаем файл в финальное место
			finalFilePath := filepath.Join(pathsOS.Path_QUIC_Downloads, fileName)
			if err := os.Rename(tempFilePath, finalFilePath); err != nil {
				os.Remove(tempFilePath)
				sendErrorResponse(w, http.StatusInternalServerError, "Ошибка перемещения загруженного на сервер файла")
				return
			}
			// Сохраняем хеш в hashMap
			hashSum := fmt.Sprintf("%016x", hash.Sum64())
			hr := &HashResult{
				hash:   hashSum,
				cancel: make(chan struct{}),
			}
			hashMap.Store(fileName, hr)
			fmt.Printf("Файл %s загружен, хеш: %s\n", fileName, hashSum)
		}
	}

	// Формирование ответа
	response := map[string]string{
		"status":   "Успех",
		"filePath": fileName, // Возвращаем только имя файла (без пути)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		sendErrorResponse(w, http.StatusInternalServerError, "Ошибка формирования ответа")
	}
}

// InstallProgramHandler обрабатывает POST-запрос с JSON-данными и отправляет в динамические топики по MQTT
func InstallProgramHandler(w http.ResponseWriter, r *http.Request) {
	// Проверка метода запроса
	if r.Method != http.MethodPost {
		sendErrorResponse(w, http.StatusMethodNotAllowed, "Разрешены только POST запросы")
		return
	}

	// Чтение тела запроса
	body, err := io.ReadAll(r.Body)
	if err != nil {
		sendErrorResponse(w, http.StatusBadRequest, "Ошибка чтения тела запроса")
		return
	}
	defer r.Body.Close()

	// Парсим полученный JSON
	var data InstallProgramRequest
	if err := json.Unmarshal(body, &data); err != nil {
		sendErrorResponse(w, http.StatusBadRequest, "Ошибка декодирования JSON")
		return
	}

	// Извлечение имени файла с расширением из полного пути
	fileName := baseNameAnyOS(data.DownloadRunPath)

	// Получение хеша из hashMap
	hrInterface, ok := hashMap.Load(fileName)
	if !ok {
		sendErrorResponse(w, http.StatusBadRequest, "Файл не загружен или хеш не вычислен")
		return
	}
	hr := hrInterface.(*HashResult)
	select {
	case <-hr.cancel:
		// fmt.Printf("Операция для %s отменена\n", fileName)
		sendErrorResponse(w, http.StatusBadRequest, "Загрузка файла была отменена")
		return
	default:
		data.XXH3 = hr.hash
		// fmt.Printf("Используем хеш для %s: %s\n", fileName, hr.hash)
	}

	// Если имя пользователя не указано, ставим значение по умолчанию "СИСТЕМА"
	if data.UserName == "" {
		data.UserName = "СИСТЕМА"
	}

	// Если флаг "Только скачать" true, то и флаг "Не удалять после установки" тоже будет true
	if data.OnlyDownload {
		data.NotDeleteAfterInstallation = true
	}

	// Генерируем "Date_Of_Creation" перед формированием ответа
	now := time.Now()
	dateOfCreation := getTimestampWithMs(now)

	// Формируем payload без токена (токен формируется и добавляется при отправке)
	payloadData := QUICPayload{
		DateOfCreation:                dateOfCreation,
		OnlyDownload:                  data.OnlyDownload,
		DownloadRunPath:               data.DownloadRunPath,
		ProgramRunArguments:           data.ProgramRunArguments,
		RunWhetherUserIsLoggedOnOrNot: data.RunWhetherUserIsLoggedOnOrNot,
		UserName:                      data.UserName,
		UserPassword:                  data.UserPassword,
		RunWithHighestPrivileges:      data.RunWithHighestPrivileges,
		NotDeleteAfterInstallation:    data.NotDeleteAfterInstallation,
		XXH3:                          data.XXH3,
		Token:                         "", // Будет заменено для каждого клиента
	}
	payload, err := json.Marshal(payloadData)
	if err != nil {
		sendErrorResponse(w, http.StatusInternalServerError, "Ошибка формирования QUIC_Command")
		return
	}

	// Получаем информацию о админе
	authInfo, err := getAuthInfoFromRequest(r)
	if err != nil {
		log.Printf("Ошибка получения информации о админе: %v", err)
		sendErrorResponse(w, http.StatusUnauthorized, "Ошибка авторизации")
		return
	}

	// Формируем clientMapping
	clientMapping := make(map[string]map[string]string)
	for _, cid := range data.ClientIDs {
		name, err := getClientName(cid)
		if err != nil {
			log.Printf("Ошибка получения имени для клиента %s: %v", cid, err)
			name = ""
		}
		clientMapping[cid] = map[string]string{
			"ClientName":     name,
			"Answer":         "",
			"QUIC_Execution": "",
			"Attempts":       "",
			"Description":    "",
		}
	}

	// Подготавливаем запись для BadgerDB
	entry := map[string]any{
		"Date_Of_Creation": dateOfCreation,
		"QUIC_Command":     string(payload),
		"ClientID_QUIC":    clientMapping,
		"SentFor":          []string{},
		"ResendRequested":  map[string]bool{},
		"Created_By":       authInfo.Name,  // Имя админа, создавшего запрос
		"Created_By_Login": authInfo.Login, // Логин админа, создавшего запрос
	}
	entryBytes, err := json.Marshal(entry)
	if err != nil {
		sendErrorResponse(w, http.StatusInternalServerError, "Ошибка подготовки данных для БД")
		return
	}

	// Формируем ключ и подготавливаем запись
	dbKey := "FiReMQ_QUIC:" + dateOfCreation

	// Запись в BadgerDB с использованием батчи
	wb := db.DBInstance.NewWriteBatch()
	defer wb.Cancel()
	if err := wb.Set([]byte(dbKey), entryBytes); err != nil {
		sendErrorResponse(w, http.StatusInternalServerError, "Ошибка сохранения в БД")
		return
	}
	if err := wb.Flush(); err != nil {
		sendErrorResponse(w, http.StatusInternalServerError, "Ошибка записи в БД")
		return
	}

	// Разрешаем доступ к QUIC, чтобы клиенты могли подключаться
	EnsureQUICOpen("создан новый запрос установки ПО")

	// Отправляем онлайн клиентам с индивидуальным токеном
	var sentTo []string
	for _, clientID := range data.ClientIDs {
		online, err := isClientOnline(clientID)
		if err != nil {
			log.Printf("Ошибка проверки статуса клиента %s: %v", clientID, err)
			continue
		}
		if online {
			// Перед немедленной отправкой онлайн-клиенту — проверим активную загрузку
			if isQUICActive(clientID) {
				log.Printf("Клиент %s уже выполняет загрузку — откладываем немедленную отправку и добавляем в очередь", clientID)
				// Не публикуем сейчас: просто оставим запись в БД (SentFor не пополнится), очередь подхватит и отправит позже
				go checkAndResendQUIC(clientID)
				continue
			}
			// Генерируем токен с привязкой к файлу
			token := generateQUICTokenForFile(clientID, payloadData.DownloadRunPath, dateOfCreation)
			clientPayload := payloadData                                        // Создаем копию payload для клиента с его индивидуальным токеном
			clientPayload.Token = token                                         // Устанавливаем токен
			log.Printf("Сгенерирован токен %s для клиента %s", token, clientID) // ДЛЯ ОТЛАДКИ
			// Сериализуем с токеном
			clientPayloadBytes, err := json.Marshal(clientPayload)
			if err != nil {
				log.Printf("Ошибка сериализации QUIC_Command для клиента %s: %v", clientID, err)
				continue
			}
			topic := "Client/" + clientID + "/ModuleQUIC"
			if err := mqtt_client.Publish(topic, clientPayloadBytes, 2); err == nil {
				sentTo = append(sentTo, clientID)
			} else {
				log.Printf("Ошибка публикации в топик %s: %v", topic, err)
			}
		}
	}

	// Обновляем SentFor
	if len(sentTo) > 0 {
		entry["SentFor"] = sentTo
		updatedEntryBytes, err := json.Marshal(entry)
		if err == nil {
			if err := db.DBInstance.Update(func(txn *badger.Txn) error {
				return txn.Set([]byte(dbKey), updatedEntryBytes)
			}); err != nil {
				log.Printf("Ошибка обновления SentFor в БД: %v", err)
			}
		}
	}

	// Формируем ответ
	response := map[string]string{
		"status":  "Успех",
		"message": "Запрос сохранён и отправлен онлайн клиентам",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		sendErrorResponse(w, http.StatusInternalServerError, "Ошибка формирования ответа")
	}
	hashMap.Delete(fileName)
}

// DeleteFileHandler обрабатывает POST-запрос для удаления файла, загруженного на сервер при отмене на WEB
func DeleteFileHandler(w http.ResponseWriter, r *http.Request) {
	// Проверка метода запроса
	if r.Method != http.MethodPost {
		sendErrorResponse(w, http.StatusMethodNotAllowed, "Разрешены только POST запросы")
		return
	}

	// Читаем тело запроса
	var requestData struct {
		Filename string `json:"filename"`
	}
	err := json.NewDecoder(r.Body).Decode(&requestData)
	if err != nil {
		sendErrorResponse(w, http.StatusBadRequest, "Ошибка декодирования JSON")
		return
	}

	// Проверяем безопасность имени файла
	if requestData.Filename == "" || strings.Contains(requestData.Filename, "..") {
		sendErrorResponse(w, http.StatusBadRequest, "Недопустимое имя файла")
		return
	}

	// Формируем путь к файлу
	filePath := filepath.Join(pathsOS.Path_QUIC_Downloads, requestData.Filename)
	// fmt.Printf("Попытка удаления файла: %s\n", filePath)

	// Проверяем hashMap и сигнализируем об отмене
	if hrInterface, ok := hashMap.Load(requestData.Filename); ok {
		hr := hrInterface.(*HashResult)
		// fmt.Printf("Сигнализируем об отмене для файла: %s\n", requestData.Filename)
		close(hr.cancel)
		hashMap.Delete(requestData.Filename)
		// fmt.Printf("Файл %s удален из hashMap\n", requestData.Filename)
	}

	// Удаление файла (до 3-х попыток при неудачах)
	maxRetries := 3
	for i := range 3 {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			// fmt.Printf("Файл %s не существует, удаление не требуется\n", filePath)
			break
		}
		if err := os.Remove(filePath); err != nil {
			fmt.Printf("Попытка %d: Ошибка удаления файла %s: %v\n", i+1, filePath, err)
			if i < maxRetries-1 {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			sendErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Не удалось удалить файл: %v", err))
			return
		}
		fmt.Printf("Файл %s успешно удален\n", filePath)
		break
	}

	// Формируем ответ
	response := map[string]string{
		"status":  "Успех",
		"message": "Файл успешно удалён",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetQUICReportHandler возвращает все записи QUIC из БД методом GET
func GetQUICReportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Только GET запросы поддерживаются", http.StatusMethodNotAllowed)
		return
	}

	// Загружаем текущих админов
	usersMap, err := loadAdminsMap()
	if err != nil {
		log.Printf("Ошибка загрузки админов: %v", err)
	}

	downloadsDir := pathsOS.Path_QUIC_Downloads
	var results []map[string]any
	err = db.DBInstance.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_QUIC:")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)
			var record map[string]any
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			})
			if err != nil {
				continue
			}

			// Проверяем обновление имени админа
			if login, ok := record["Created_By_Login"].(string); ok && usersMap != nil {
				if user, exists := usersMap[login]; exists {
					currentName := user.Auth_Name
					savedName, nameExists := record["Created_By"].(string)
					// Если имя изменилось - обновляем запись
					if nameExists && savedName != currentName {
						record["Created_By"] = currentName
						newData, err := json.Marshal(record)
						if err == nil {
							// NB: badger.View обычно ReadOnly; предполагается текущая логика проекта
							txn.Set(key, newData)
						}
					}
				}
			}

			// Размер файла
			var fileSize int64 = 0
			// Удаляем "UserPassword", "Token" и ненужный дублирующий "Date_Of_Creation"
			if quicStr, ok := record["QUIC_Command"].(string); ok {
				var quicMap map[string]any
				if err := json.Unmarshal([]byte(quicStr), &quicMap); err == nil {
					delete(quicMap, "UserPassword")
					delete(quicMap, "Token")
					delete(quicMap, "Date_Of_Creation")
					// Размер файла + дополнение пути (только для ответа)
					if drp, ok := quicMap["DownloadRunPath"].(string); ok {
						orig := strings.TrimSpace(drp)
						base := baseNameAnyOS(orig)
						if base != "" && base != "." {
							fpath := filepath.Join(downloadsDir, base)
							if info, err := os.Stat(fpath); err == nil {
								fileSize = info.Size()
							}
							if isBareFileName(orig) {
								quicMap["DownloadRunPath"] = defaultClientDownloadPath + `\` + base
							}
						}
					}
					if updatedQuic, err := json.Marshal(quicMap); err == nil {
						record["QUIC_Command"] = string(updatedQuic)
					}
				}
			}

			itemResponse := map[string]any{
				"Date_Of_Creation": record["Date_Of_Creation"],
				"QUIC_Command":     record["QUIC_Command"],
				"ClientID_QUIC":    record["ClientID_QUIC"],
				"Created_By":       record["Created_By"], // Имя админа, создавшего запрос
				"File_Size_Bytes":  fileSize,             // Размер загруженного на сервер файла
			}
			results = append(results, itemResponse)
		}
		return nil
	})
	if err != nil {
		http.Error(w, "Ошибка чтения из БД", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// ResendQUICReportHandler обрабатывает POST запрос для повторной отправки QUIC команды конкретному клиенту
func ResendQUICReportHandler(w http.ResponseWriter, r *http.Request) {
	// Если клиент онлайн – команда отправляется сразу (не чаще 1 раза в 10 секунд на клиента)
	// Если клиент офлайн – выставляется флаг, чтобы при переходе в онлайн команда была отправлена один раз (независимо от кол-ва запросов)
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ClientID         string `json:"client_id"`
		Date_Of_Creation string `json:"Date_Of_Creation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClientID == "" || req.Date_Of_Creation == "" {
		http.Error(w, "Ошибка парсинга данных или отсутствует client_id/Date_Of_Creation", http.StatusBadRequest)
		return
	}

	// Запрет только для того запроса, который прямо сейчас скачивается
	if isQUICActiveFor(req.ClientID, req.Date_Of_Creation) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Отклонено",
			"message": "Скачивание ещё не завершено, повторная отправка невозможна.",
		})
		return
	}

	var (
		processed        bool // Были ли изменения в записи
		alreadyRequested bool // Флаг уже установлен
		commandSent      bool // Была ли отправлена команда
		throttled        bool // Флаг ограничения лимита запросов
		waitSeconds      int  // Время ожидания истичения лимита
		// Подготовка публикации и открытие порта после коммита транзакции
		topic            string
		payloadToPublish []byte
		needOpen         bool
	)

	dbKey := "FiReMQ_QUIC:" + req.Date_Of_Creation
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
		if !ok || mapping[req.ClientID] == nil {
			return nil
		}
		clientEntry, ok := mapping[req.ClientID].(map[string]any)
		if !ok {
			return nil
		}

		online, err := isClientOnline(req.ClientID)
		if err != nil {
			return nil
		}

		// Клиент онлайн
		if online {
			// Ограничение частоты, не чаще 1 раза в 10 секунд для онлайн-клиента
			allowed, wait := allowResend(req.ClientID, 10*time.Second)
			if !allowed {
				throttled = true
				waitSeconds = int(wait.Seconds()) + 1
				return nil
			}

			// Очистка Answer, только если реальная отправка
			cleared := false
			if s, _ := clientEntry["Answer"].(string); s != "" {
				clientEntry["Answer"] = ""
				cleared = true
			}
			if s, _ := clientEntry["QUIC_Execution"].(string); s != "" {
				clientEntry["QUIC_Execution"] = ""
				cleared = true
			}
			if s, _ := clientEntry["Attempts"].(string); s != "" {
				clientEntry["Attempts"] = ""
				cleared = true
			}
			if s, _ := clientEntry["Description"].(string); s != "" {
				clientEntry["Description"] = ""
				cleared = true
			}
			if cleared {
				mapping[req.ClientID] = clientEntry
				record["ClientID_QUIC"] = mapping
				processed = true
			}

			// Подготовка команды
			payloadStr, ok := record["QUIC_Command"].(string)
			if !ok {
				return nil
			}
			var payload QUICPayload
			if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
				return nil
			}

			// Генерация нового токена
			payload.Token = generateQUICTokenForFile(req.ClientID, payload.DownloadRunPath, req.Date_Of_Creation)
			buf, err := json.Marshal(payload)
			if err != nil {
				return nil
			}

			// Сохраняем для публикации после коммита
			payloadToPublish = buf
			topic = "Client/" + req.ClientID + "/ModuleQUIC"
			needOpen = true

			// Обновление SentFor
			var sentFor []string
			if s, exists := record["SentFor"]; exists {
				for _, v := range s.([]any) {
					if str, ok := v.(string); ok {
						sentFor = append(sentFor, str)
					}
				}
			}
			if !slices.Contains(sentFor, req.ClientID) {
				sentFor = append(sentFor, req.ClientID)
				record["SentFor"] = sentFor
				processed = true
			}
		} else {
			// Оффлайн: установка флага на будущую отправку, если ещё не стоял
			var rr map[string]any
			if v, exists := record["ResendRequested"]; exists {
				rr, _ = v.(map[string]any)
			} else {
				rr = make(map[string]any)
			}
			if _, exists := rr[req.ClientID]; exists {
				alreadyRequested = true
			} else {
				rr[req.ClientID] = true
				record["ResendRequested"] = rr
				log.Printf("Установлен флаг повторной отправки для клиента %s", req.ClientID)
				processed = true
				// Очистка Answer, только при первом выставлении флага
				if s, _ := clientEntry["Answer"].(string); s != "" {
					clientEntry["Answer"] = ""
				}
				if s, _ := clientEntry["QUIC_Execution"].(string); s != "" {
					clientEntry["QUIC_Execution"] = ""
				}
				if s, _ := clientEntry["Attempts"].(string); s != "" {
					clientEntry["Attempts"] = ""
				}
				if s, _ := clientEntry["Description"].(string); s != "" {
					clientEntry["Description"] = ""
				}
				mapping[req.ClientID] = clientEntry
				record["ClientID_QUIC"] = mapping
			}
		}

		// Сохранение записи, только если были изменения
		if processed {
			newBytes, err := json.Marshal(record)
			if err != nil {
				return err
			}
			return txn.Set([]byte(dbKey), newBytes)
		}
		return nil
	})
	if err != nil {
		http.Error(w, "Ошибка обработки запроса: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Определение ответа сервера
	w.Header().Set("Content-Type", "application/json")
	// При частых, повторных попытках
	if throttled {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Ограничение",
			"message": fmt.Sprintf("Повторная отправка не доступна. Подождите ещё %d сек.", waitSeconds),
		})
		return
	}

	// Если есть что публиковать — обеспечим открытый порт и отправим команду
	if needOpen && len(payloadToPublish) > 0 {
		// ВАЖНО: теперь в БД Answer уже очищен -> hasReadyQUICTasks() вернёт true
		EnsureQUICOpen("повторная отправка для клиента " + req.ClientID)
		if err := mqtt_client.Publish(topic, payloadToPublish, 2); err != nil {
			log.Printf("Ошибка повторной публикации QUIC команды в топик %s: %v", topic, err)
		} else {
			log.Printf("Повторная отправка QUIC команды клиенту %s выполнена", req.ClientID)
			commandSent = true
		}
	}

	if commandSent || processed {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Успех",
			"message": "Запрос на повторную отправку команды отправлен",
		})
	} else if alreadyRequested {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Уже запрошено",
			"message": "Повторная отправка уже запрошена ранее!",
		})
	} else {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Не найдено",
			"message": "Команда или клиент не найдены",
		})
	}
}

// DeleteQUICByDateHandler удаляет все QUIC записи по дате создания
func DeleteQUICByDateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Date_Of_Creation string `json:"Date_Of_Creation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Ошибка парсинга данных: "+err.Error(), http.StatusBadRequest)
		return
	}

	found := false
	var filesToMaybeDelete []string // Файл, подлежащий удалению
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
			if record["Date_Of_Creation"] == req.Date_Of_Creation {
				found = true
				// Сохраним имя файла, чтобы удалить его после коммита транзакции
				if fn, err := extractFileNameFromQUICRecord(record); err == nil && fn != "" {
					filesToMaybeDelete = append(filesToMaybeDelete, fn)
				} else if err != nil {
					log.Printf("Не удалось извлечь имя файла из записи для удаления: %v", err)
				}
				if err := txn.Delete(item.Key()); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		http.Error(w, "Ошибка удаления запроса: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !found {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Ошибка",
			"message": "Запрос с указанной датой и временем не найден",
		})
		return
	}

	// Удаляем связанные файлы, которые больше не используются другими запросами
	if len(filesToMaybeDelete) > 0 {
		uniq := make(map[string]struct{}, len(filesToMaybeDelete))
		for _, f := range filesToMaybeDelete {
			uniq[filepath.Base(f)] = struct{}{}
		}
		for f := range uniq {
			deleteQUICFileIfUnreferenced(f)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "Успех",
		"message": "Запрос удалён",
	})

	// После удаления — пересчитываем доступ
	RecalculateQUICAccess("удаление запроса по дате")
}

// DeleteClientFromQUICByDateHandler удаляет конкретного клиента из QUIC записи по дате создания
func DeleteClientFromQUICByDateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Разрешены только POST запросы", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Date_Of_Creation string `json:"Date_Of_Creation"`
		ClientID         string `json:"client_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Ошибка парсинга данных: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.ClientID == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Ошибка",
			"message": "Не указан ID клиента для удаления",
		})
		return
	}

	var deletedCount int
	var filesToMaybeDelete []string // Файл, подлежащий удалению
	err := db.DBInstance.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("FiReMQ_QUIC:")
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			key := it.Item().KeyCopy(nil)
			var record map[string]any
			err := it.Item().Value(func(val []byte) error {
				return json.Unmarshal(val, &record)
			})
			if err != nil {
				continue
			}
			if record["Date_Of_Creation"] == req.Date_Of_Creation {
				mapping, ok := record["ClientID_QUIC"].(map[string]any)
				if !ok {
					continue
				}
				if _, exists := mapping[req.ClientID]; exists {
					delete(mapping, req.ClientID)
					deletedCount++
				}
				if len(mapping) == 0 {
					// Запись будет удалена целиком — сохраним имя файла для последующего удаления
					if fn, err := extractFileNameFromQUICRecord(record); err == nil && fn != "" {
						filesToMaybeDelete = append(filesToMaybeDelete, fn)
					} else if err != nil {
						log.Printf("Не удалось извлечь имя файла из записи при удалении последнего клиента: %v", err)
					}
					if err := txn.Delete(key); err != nil {
						return err
					}
				} else {
					record["ClientID_QUIC"] = mapping
					newBytes, err := json.Marshal(record)
					if err != nil {
						return err
					}
					if err := txn.Set(key, newBytes); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		http.Error(w, "Ошибка обновления запросов: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if deletedCount == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "Ошибка",
			"message": "Клиент не найден или не удалён из запроса",
		})
		return
	}

	// Если запись была удалена (последний клиент), удалим файл, если он больше не используется
	if len(filesToMaybeDelete) > 0 {
		uniq := make(map[string]struct{}, len(filesToMaybeDelete))
		for _, f := range filesToMaybeDelete {
			uniq[filepath.Base(f)] = struct{}{}
		}
		for f := range uniq {
			deleteQUICFileIfUnreferenced(f)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "Успех",
		"message": "Клиент удалён из запроса",
	})

	// После удаления клиента — пересчитываем доступ
	RecalculateQUICAccess("удаление клиента из запроса")
}

// SendErrorResponse отправляет JSON-ответ с ошибкой обратно в WEB админку
func sendErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	response := map[string]string{
		"status":  "Ошибка",
		"message": message,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

func isBareFileName(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	// Если нет ни слешей, ни двоеточия для диска — значит это только имя файла
	if strings.ContainsAny(p, `\/`) {
		return false
	}
	if len(p) >= 2 && p[1] == ':' { // "C:\..."
		return false
	}
	return true
}

// BaseNameAnyOS возвращает имя файла из пути Windows/Linux независимо от ОС сервера
func baseNameAnyOS(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	// Уберём возможные кавычки, если путь вставлен в кавычках
	p = strings.Trim(p, `"'`)
	// Нормализуем разделители
	p = strings.ReplaceAll(p, "\\", "/")
	return filepath.Base(p)
}
