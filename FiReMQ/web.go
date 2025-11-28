// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"FiReMQ/mqtt_client" // Локальный пакет MQTT клиента AutoPaho
	"FiReMQ/mqtt_server" // Локальный пакет MQTT клиента Mocho-MQTT
	"FiReMQ/pathsOS"     // Локальный пакет с путями для разных платформ
	"FiReMQ/protection"  // Локальный пакет с функциями базовой защиты
	"FiReMQ/update"      // Локальный пакет для обновления FiReMQ

	"github.com/corazawaf/coraza/v3"
	"golang.org/x/time/rate"
)

// CorazaMiddleware Middleware для Coraza WAF
func CorazaMiddleware(getWAF func() coraza.WAF, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if ok, _, _, wasPrev := protection.ValidateCSRFForRequestDetailed(r); ok {
				// log.Printf("[CSRF][OK] %s %s (%s)", r.Method, r.URL.Path, dbg)

				// После успешной проверки — выдаётся новый токен.
				// Если запрос пришёл со старым (prev), не вращаем повторно, а отдаём текущий.
				if login, sid, err := protection.GetLoginAndSessionIDFromCookie(r); err == nil {
					var newTok string
					if wasPrev {
						newTok = protection.IssueOrGetCSRF(sid, login) // не крутим ещё раз
					} else {
						newTok = protection.RotateCSRF(sid, login) // крутим на каждый успешный POST
					}
					w.Header().Set("X-CSRF-Token", newTok)
				}

				next.ServeHTTP(w, r)
				return
			} else {
				// log.Printf("[CSRF][ОШИБКА] %s %s reason=%s", r.Method, r.URL.Path, reason)
				http.Error(w, "CSRF токен недействителен!", http.StatusForbidden)
				return
			}
		}

		// Иначе — стандартная проверка через WAF
		waf := getWAF() // Получение текущего экземпляра Coraza WAF

		// Если проверка не пройдена, выполняем стандартную проверку через WAF
		transaction := waf.NewTransaction()
		defer transaction.Close() // Очистка транзакции после завершения

		// Обработка соединения
		transaction.ProcessConnection(r.RemoteAddr, 0, r.Host, 0)

		// Обработка URI
		transaction.ProcessURI(r.URL.Path, r.Method, "HTTP/1.1")

		// Обработка заголовков запроса
		transaction.ProcessRequestHeaders()

		// Обработка тела запроса
		if _, err := transaction.ProcessRequestBody(); err != nil {
			log.Printf("Ошибка в теле запроса \"ProcessRequestBody\" на обработку: %v", err)
			http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
			return
		}

		// Проверка на прерывание (если запрос заблокирован)
		if interruption := transaction.Interruption(); interruption != nil {
			log.Printf("Прерывание Coraza WAF: %v", interruption)
			http.Error(w, "Запрещено!", http.StatusForbidden)
			return
		}

		// Если запрос не заблокирован, передаём его дальше
		next.ServeHTTP(w, r)
	})
}

// RenderWebPage обработка HTML страницы
func renderWebPage(w http.ResponseWriter, r *http.Request) {
	// Устанавливаем заголовки безопасности
	protection.SetSecurityHeaders(w)

	if r.URL.Path == "/get-all-groups-and-sub-groups" {
		groups, err := GetAllGroupsAndSubgroups()
		if err != nil {
			http.Error(w, "Ошибка получения данных", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(groups)
		return
	}

	// Обработка HTML страницы
	http.ServeFile(w, r, filepath.Join(pathsOS.Path_Web_Data, "index.html"))
}

// StartWebServer запуск веб-сервера (маршруты)
func StartWebServer(getWAF func() coraza.WAF) {
	// Страница с авторизацией (1 запрос каждые 250 мс = 4 запросов в секунду)
	http.HandleFunc("/auth.html", protection.RateLimitMiddleware(4, 8)(AuthPageHandler))

	// Генерация капчи (1 запрос каждые 6 секунд = 10 запросов в минуту)
	http.HandleFunc("/captcha", protection.RateLimitMiddleware(rate.Every(time.Minute/10), 20)(func(w http.ResponseWriter, r *http.Request) {
		// Устанавливаем заголовки безопасности
		protection.SetSecurityHeaders(w)

		// Генерируем капчу
		id, b64s, err := protection.GenerateCaptcha()
		if err != nil {
			log.Printf("Ошибка генерации капчи: %v", err)
			// Отправлять в ответ будем будем другое сообщение, что бы не раскрывать деталей сервера
			http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":    id,
			"image": b64s,
		})
	}))

	// Проверка необходимости показа капчи для конкретного IP (1 запрос каждые 7,5 секунд = 8 запросов в минуту)
	http.HandleFunc("/check-captcha", protection.RateLimitMiddleware(rate.Every(time.Minute/8), 8)(func(w http.ResponseWriter, r *http.Request) {
		// Устанавливаем заголовки безопасности
		protection.SetSecurityHeaders(w)

		// Получение IP-адрес клиента
		ip := protection.GetClientIP(r)
		attempts := protection.GetLoginAttempts(ip)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{
			"captcha_required": attempts > 2,
		})
	}))

	// Авторизация (применяем Middleware для проверки авторизации и Coraza WAF)
	http.HandleFunc("/auth", protection.RateLimitMiddleware(rate.Every(6*time.Second), 10)(AuthHandler)) // POST команда для авторизации (1 запрос каждые 6 секунд = 10 запросов в минуту)
	http.HandleFunc("/logout", LogoutHandler)                                                            // GET команда для разлогинивания
	http.HandleFunc("/check-auth", CheckAuthHandler)                                                     // GET Проверка авторизации
	http.HandleFunc("/refresh-token", RefreshTokenHandler)                                               // GET Обновление токена

	// Публичный статический файл "auth.css" (доступен до авторизации)
	http.HandleFunc("/css/auth.css", func(w http.ResponseWriter, r *http.Request) {
		protection.SetSecurityHeaders(w)
		http.ServeFile(w, r, filepath.Join(pathsOS.Path_Web_Data, "css", "auth.css"))
	})

	// Публичный статический файл "auth.js" (доступен до авторизации)
	http.HandleFunc("/js/auth.js", func(w http.ResponseWriter, r *http.Request) {
		protection.SetSecurityHeaders(w)
		http.ServeFile(w, r, filepath.Join(pathsOS.Path_Web_Data, "js", "auth.js"))
	})

	// Публичный статический файл иконки WEB страницы
	http.Handle("/favicon.ico", protection.SecurityHeadersMiddleware(http.FileServer(http.Dir(pathsOS.Path_Web_Data))))

	// Защищённые CSS (доступные только после успешной авторизации)
	cssHandler := http.StripPrefix("/css/", http.FileServer(http.Dir(filepath.Join(pathsOS.Path_Web_Data, "css"))))
	http.Handle("/css/", protection.SecurityHeadersMiddleware(CorazaMiddleware(getWAF, AuthMiddleware(cssHandler))))

	// Защищённые JS (доступные только после успешной авторизации)
	jsHandler := http.StripPrefix("/js/", http.FileServer(http.Dir(filepath.Join(pathsOS.Path_Web_Data, "js"))))
	http.Handle("/js/", protection.SecurityHeadersMiddleware(CorazaMiddleware(getWAF, AuthMiddleware(jsHandler))))

	// Защищённые векторные иконки (доступные только после успешной авторизации)
	iconHandler := http.StripPrefix("/icon/", http.FileServer(http.Dir(filepath.Join(pathsOS.Path_Web_Data, "icon"))))
	http.Handle("/icon/", protection.SecurityHeadersMiddleware(CorazaMiddleware(getWAF, AuthMiddleware(iconHandler))))

	// Маршруты для работы с клиентами
	protectedMux := http.NewServeMux()
	protectedMux.HandleFunc("/", renderWebPage)                         // Путь для главной страницы
	protectedMux.HandleFunc("/csrf-token", protection.CSRFTokenHandler) // GET команда для выдачи CSRF токена в JSON

	protectedMux.HandleFunc("/set-name-client", SetNameHandler)                       // POST команда для изменения имени клиента
	protectedMux.HandleFunc("/delete-client", DeleteClientHandler)                    // POST команда для удаления клиента
	protectedMux.HandleFunc("/delete-selected-clients", DeleteSelectedClientsHandler) // POST команда для массового удаления клиентов
	protectedMux.HandleFunc("/move-client", MoveClientHandler)                        // POST команда для перемещения клиента в другую подгруппу
	protectedMux.HandleFunc("/move-selected-clients", MoveSelectedClientsHandler)     // POST команда для массового перемещения клиентов
	protectedMux.HandleFunc("/get-clients-by-group", FetchClientsByGroupHandler)      // GET команда для формирования сортировки отображаемых клиентов

	// Маршруты для "Учётные записи админов"
	protectedMux.HandleFunc("/add-admin", AddAdminHandler)             // POST команда для добавления новой учетной записи
	protectedMux.HandleFunc("/delete-admin", DeleteAdminHandler)       // POST команда для удаления учетной записи
	protectedMux.HandleFunc("/update-admin", UpdateAdminHandler)       // POST команда для обновления учетной записи
	protectedMux.HandleFunc("/get-admin-names", GetAdminsNamesHandler) // GET команда для получения списка имён
	protectedMux.HandleFunc("/get-authname", GetAuthNameHandler)       // GET команда для получения имени авторизованного админа в WEB админке

	// Маршруты MQTT сервера
	protectedMux.HandleFunc("/get-accounts-mqtt", mqtt_server.GetAccountsHandler)     // GET команда для получения данных учетных записей
	protectedMux.HandleFunc("/update-account-mqtt", mqtt_server.UpdateAccountHandler) // POST команда для обновления данных учетной записи
	protectedMux.HandleFunc("/update-allow-mqtt", mqtt_server.UpdateAllowHandler)     // POST команда разрешает или запрещает подключение через учётную запись в конфиге "mqtt_config.json" с низким приоритетом "1"

	// Маршрут для формирования и отправки команд в "cmd/PowerShell"
	protectedMux.HandleFunc("/send-terminal-command", SendCommandHandler) // POST команда для отправки cmd или PowerShell команды

	// Маршруты для отчёта по "cmd/PowerShell"
	protectedMux.HandleFunc("/get-terminal-report", GetCommandsHandler)                             // GET команда для получения всех записей команд
	protectedMux.HandleFunc("/resend-terminal-report", ResendCommandHandler)                        // POST команда для повторной отправки команды конкретному клиенту
	protectedMux.HandleFunc("/delete-by-date-terminal-report", DeleteCommandsByDateHandler)         // POST команда для удаления всех записей в БД по дате создания
	protectedMux.HandleFunc("/delete-client-terminal-report", DeleteClientFromCommandByDateHandler) // POST команда для удаления конкретной записи ClientID из БД по дате создания

	// Маршруты для получения информации о системе клиента
	protectedMux.HandleFunc("/getFile-info", protection.RateLimitMiddleware(rate.Every(1500*time.Millisecond), 1)(mqtt_client.HandleClientInfoFileRequest)) // POST команда для создания одноразовой ссылки на просмотр или скачивание файла отчёта (1 запрос каждые 1,5 секунды = 40 запросов в минуту)
	protectedMux.HandleFunc("/report-view/", mqtt_client.ReportViewHandler)                                                                                 // GET команда от открытия страницы отчёта по одноразовой ссылке

	// Маршруты для формирования и отправки команд и загрузки файла в "Установка ПО"
	protectedMux.HandleFunc("/upload-file-QUIC", UploadFileHandler)              // POST команда для загрузки исполняемого файла на сервер
	protectedMux.HandleFunc("/delete-file-QUIC", DeleteFileHandler)              // POST команда для удаления файла с сервера при отмене загрузки в WEB админке
	protectedMux.HandleFunc("/send-install-QUIC-program", InstallProgramHandler) // POST команда для отправки JSON команд QUIC-клиентам

	// Маршруты для отчёта по "Установка ПО"
	protectedMux.HandleFunc("/get-QUIC-report", GetQUICReportHandler)                        // GET команда для получения всех записей QUIC
	protectedMux.HandleFunc("/resend-QUIC-report", ResendQUICReportHandler)                  // POST команда для повторной отправки команды конкретному QUIC-клиенту
	protectedMux.HandleFunc("/delete-by-date-QUIC-report", DeleteQUICByDateHandler)          // POST команда для удаления всех QUIC записей по дате создания
	protectedMux.HandleFunc("/delete-client-QUIC-report", DeleteClientFromQUICByDateHandler) // POST команда для удаления конкретной QUIC записи ClientID по дате создания

	// Маршруты для обновления или отката правил OWASP CRS для Coraza WAF с GitHub
	protectedMux.HandleFunc("/check-OWASP-CRS", protection.CheckOWASPHandler)                    // GET команда проверяет наличие новой версии правил
	protectedMux.HandleFunc("/update-OWASP-CRS", protection.UpdateOWASPHandler)                  // POST команда обновляет правила
	protectedMux.HandleFunc("/rollback-backup-OWASP-CRS", protection.RollbackBackupOWASPHandler) // POST команда для отката правил из бэкапа

	// Маршруты для отправки команды самоудаления клиентам "FiReAgent"
	protectedMux.HandleFunc("/uninstall-fireagent", UninstallFiReAgentHandler)    // POST команда на запроса самоудаления конкретных клиентов по их ID
	protectedMux.HandleFunc("/uninstall-pending", GetPendingUninstallListHandler) // GET команда показывает список ID, находящихся в офлайне и ожидающих удаления
	protectedMux.HandleFunc("/uninstall-cancel", CancelPendingUninstallHandler)   // POST команда отменяет удаление конкретного офлайн ID (если клиент ещё не был в онлайне, при запросе удаления, удаление можно отменить)

	// Маршруты для обновления или отката серверной части FiReMQ с GitHub/GitFlic
	protectedMux.HandleFunc("/check-FiReMQ", update.CheckHandler)              // GET команда проверяет наличие новой версии FiReMQ
	protectedMux.HandleFunc("/update-FiReMQ", update.UpdateHandler)            // POST команда скачивает, проверяет, запускает утилиту "ServerUpdater" и корректно завершает работу FiReMQ
	protectedMux.HandleFunc("/rollback-backup-FiReMQ", update.RollbackHandler) // POST команда для отката версии FiReMQ на предыдущий релиз (через утилиту ServerUpdater)

	/* * * * * * * * * * * * * * * * * * * * * */
	// ДЛЯ ТЕСТА!!! Временный обход проверок Coraza WAF для тестирования запроса с пропуском CSRF
	// http.HandleFunc("/check-FiReMQ", update.CheckHandler)
	// http.HandleFunc("/update-FiReMQ", update.UpdateHandler)
	// http.HandleFunc("/rollback-backup-FiReMQ", update.RollbackHandler)
	/* * * * * * * * * * * * * * * * * * * * * */

	// Обработка всех маршрутов
	http.Handle("/", protection.SecurityHeadersMiddleware(protection.OriginCheckMiddleware(CorazaMiddleware(getWAF, AuthMiddleware(protectedMux)))))

	log.Fatal(http.ListenAndServeTLS(pathsOS.Web_Host+":"+pathsOS.Web_Port, pathsOS.Path_Web_Cert, pathsOS.Path_Web_Key, nil))
}
