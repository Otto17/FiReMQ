// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"FiReMQ/LinuxInfo"     // Локальный пакет с информацией о Linux сервере
	"FiReMQ/logging"       // Локальный пакет с логированием в HTML файл
	"FiReMQ/mqtt_client"   // Локальный пакет MQTT клиента AutoPaho
	"FiReMQ/mqtt_server"   // Локальный пакет MQTT клиента Mocho-MQTT
	"FiReMQ/pathsOS"       // Локальный пакет с путями для разных платформ
	"FiReMQ/protection"    // Локальный пакет с функциями базовой защиты
	"FiReMQ/update"        // Локальный пакет для обновления FiReMQ
	"FiReMQ/update_client" // Локальный пакет для обновлений клиентов

	"github.com/corazawaf/coraza/v3"
	"golang.org/x/time/rate"
)

// CorazaMiddleware Middleware для Coraza WAF
func CorazaMiddleware(getWAF func() coraza.WAF, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Сначала проверяется CSRF для POST
		if r.Method == http.MethodPost {
			if ok, _, _, wasPrev := protection.ValidateCSRFForRequestDetailed(r); ok {
				// log.Printf("[CSRF][OK] %s %s (%s)", r.Method, r.URL.Path, dbg)

				// После успешной проверки — выдаётся новый токен.
				// Если запрос пришёл со старым (prev), не вращает повторно, а отдаёт текущий.
				if login, sid, err := protection.GetLoginAndSessionIDFromCookie(r); err == nil {
					var newTok string
					if wasPrev {
						newTok = protection.IssueOrGetCSRF(sid, login) // не крутим ещё раз
					} else {
						newTok = protection.RotateCSRF(sid, login) // крутим на каждый успешный POST
					}
					w.Header().Set("X-CSRF-Token", newTok)
				}
			} else {
				// log.Printf("[CSRF][ОШИБКА] %s %s reason=%s", r.Method, r.URL.Path, reason)
				http.Error(w, "CSRF токен недействителен!", http.StatusForbidden)
				return
			}
		}

		// Иначе — стандартная проверка через WAF
		waf := getWAF() // Получение текущего экземпляра Coraza WAF

		// Если проверка не пройдена, выполняет стандартную проверку через WAF
		transaction := waf.NewTransaction()
		defer transaction.Close() // Очистка транзакции после завершения

		// Разделение IP-адреса и порта
		clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			clientIP = r.RemoteAddr
		}

		// Обработка соединения
		transaction.ProcessConnection(clientIP, 0, r.Host, 0)

		// Обработка URI (используется реальный протокол из запроса, например, "HTTP/2.0")
		transaction.ProcessURI(r.URL.Path, r.Method, r.Proto)

		// Добавляет в транзакцию реальные HTTP-заголовки запроса
		if r.Host != "" {
			transaction.AddRequestHeader("Host", r.Host)
		}

		// Остальные заголовки берутся из r.Header, в контекст WAF для анализа
		for k, vv := range r.Header {
			for _, v := range vv {
				transaction.AddRequestHeader(k, v)
			}
		}

		// Обработка заголовков запроса
		transaction.ProcessRequestHeaders()

		// Обработка тела запроса
		if _, err := transaction.ProcessRequestBody(); err != nil {
			logging.LogError("WAF: Ошибка в теле запроса \"ProcessRequestBody\" на обработку: %v", err)
			http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
			return
		}

		// Проверка на прерывание (если запрос заблокирован)
		if interruption := transaction.Interruption(); interruption != nil {
			// Получает IP для лога
			clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
			logging.LogSecurity("WAF: заблокировал запрос от %s. Причина: %v", clientIP, interruption)
			http.Error(w, "Запрещено!", http.StatusForbidden)
			return
		}

		/* * * * * * * * * * * * * * * * * * * * * */
		// ДЛЯ ОТЛАДКИ. Проверка на прерывание (если запрос заблокирован)
		// if interruption := transaction.Interruption(); interruption != nil {
		// 	log.Printf("Прерывание Coraza WAF: %v", interruption)

		// 	// Вывод причины блокировки
		// 	for _, mr := range transaction.MatchedRules() {
		// 		r := mr.Rule()
		// 		log.Printf("Сработало правило ID: %d | Tag: %s | Msg: %s", r.ID(), r.Tags(), mr.Message())
		// 		log.Printf("Данные: %s", mr.Data()) // Покажет, какая часть запроса вызвала сработку
		// 	}

		// 	http.Error(w, "Запрещено!", http.StatusForbidden)
		// 	return
		// }
		/* * * * * * * * * * * * * * * * * * * * * */

		// Если запрос не заблокирован, передаёт его дальше
		next.ServeHTTP(w, r)
	})
}

// RenderWebPage обработка HTML страницы
func renderWebPage(w http.ResponseWriter, r *http.Request) {
	// Устанавливает заголовки безопасности
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
	// Страница с авторизацией (1 запрос каждые 250 мс = 4 запросов в секунду), DoS логи в данном случае пишутся ТОЛЬКО в консоль
	http.HandleFunc("/auth.html", protection.RateLimitMiddleware(4, 8, protection.DoSLogConsoleOnly)(AuthPageHandler))

	// Генерация капчи (1 запрос каждые 6 секунд = 10 запросов в минуту)
	http.HandleFunc("/captcha", protection.RateLimitMiddleware(rate.Every(time.Minute/10), 20)(func(w http.ResponseWriter, r *http.Request) {
		// Устанавливает заголовки безопасности
		protection.SetSecurityHeaders(w)

		// Генерирует капчу
		id, b64s, err := protection.GenerateCaptcha()
		if err != nil {
			logging.LogError("Ошибка генерации капчи: %v", err)
			// Отправлять в ответ будет другое сообщение, что бы не раскрывать деталей сервера
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
		// Устанавливает заголовки безопасности
		protection.SetSecurityHeaders(w)

		// Получение IP-адрес клиента
		ip := protection.GetClientIP(r)
		attempts := protection.GetLoginAttempts(ip)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{
			"captcha_required": attempts > 2,
		})
	}))

	// Авторизация (применяет Middleware для проверки авторизации и Coraza WAF), DoS логи в данном случае пишутся ТОЛЬКО в консоль
	http.HandleFunc("/auth", protection.RateLimitMiddleware(rate.Every(6*time.Second), 10, protection.DoSLogConsoleOnly)(AuthHandler)) // POST команда для авторизации (1 запрос каждые 6 секунд = 10 запросов в минуту)
	http.HandleFunc("/logout", LogoutHandler)                                                                                          // GET команда для разлогинивания
	http.HandleFunc("/check-auth", CheckAuthHandler)                                                                                   // GET Проверка авторизации
	http.HandleFunc("/refresh-token", RefreshTokenHandler)                                                                             // GET Обновление токена

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

	protectedMux.HandleFunc("/get-clients-by-group", FetchClientsByGroupHandler)                                                                    // GET команда для формирования сортировки отображаемых клиентов
	protectedMux.HandleFunc("/set-name-client", protection.RateLimitMiddleware(rate.Every(1*time.Second), 5)(SetNameHandler))                       // POST команда для изменения имени клиента (1 запрос каждую секунду = 60 запросов в минуту, до 5 подряд)
	protectedMux.HandleFunc("/delete-client", protection.RateLimitMiddleware(rate.Every(1*time.Second), 5)(DeleteClientHandler))                    // POST команда для удаления клиента (1 запрос каждую секунду = 60 запросов в минуту, до 5 подряд)
	protectedMux.HandleFunc("/move-client", protection.RateLimitMiddleware(rate.Every(1*time.Second), 5)(MoveClientHandler))                        // POST команда для перемещения клиента в другую подгруппу (1 запрос каждую секунду = 60 запросов в минуту, до 5 подряд)
	protectedMux.HandleFunc("/delete-selected-clients", protection.RateLimitMiddleware(rate.Every(3*time.Second), 2)(DeleteSelectedClientsHandler)) // POST команда для массового удаления клиентов (1 запрос каждые 3 секунды = 20 запросов в минуту, до 2 подряд)
	protectedMux.HandleFunc("/move-selected-clients", protection.RateLimitMiddleware(rate.Every(3*time.Second), 2)(MoveSelectedClientsHandler))     // POST команда для массового перемещения клиентов (1 запрос каждые 3 секунды = 20 запросов в минуту, до 2 подряд)

	// Маршруты для "Учётные записи админов"
	protectedMux.HandleFunc("/get-admin-names", GetAdminsNamesHandler)                                                                                             // GET команда для получения списка имён
	protectedMux.HandleFunc("/get-authname", GetAuthNameHandler)                                                                                                   // GET команда для получения имени авторизованного админа в WEB админке
	protectedMux.HandleFunc("/get-current-permissions", GetCurrentAdminPermissionsHandler)                                                                         // GET команда для получения прав текущего авторизованного админа
	protectedMux.HandleFunc("/add-admin", protection.RateLimitMiddleware(rate.Every(5*time.Second), 2)(AddAdminHandler))                                           // POST команда для добавления новой учетной записи (1 запрос каждые 5 секунд = 12 запросов в минуту, до 2 подряд)
	protectedMux.HandleFunc("/delete-admin", protection.RateLimitMiddleware(rate.Every(5*time.Second), 2)(DeleteAdminHandler))                                     // POST команда для удаления учетной записи (1 запрос каждые 5 секунд = 12 запросов в минуту, до 2 подряд)
	protectedMux.HandleFunc("/update-admin", protection.RateLimitMiddleware(rate.Every(5*time.Second), 2)(UpdateAdminHandler))                                     // POST команда для обновления учетной записи (1 запрос каждые 5 секунд = 12 запросов в минуту, до 2 подряд)
	protectedMux.HandleFunc("/toggle-admin-permission", protection.RateLimitMiddleware(rate.Every(1*time.Second), 5)(ToggleAdminPermissionHandler))                // POST команда для изменения конкретного разрешения учётной записи (1 запрос каждую секунду = 60 запросов в минуту, до 5 подряд)
	protectedMux.HandleFunc("/update-rename-clients-groups", protection.RateLimitMiddleware(rate.Every(1*time.Second), 5)(UpdateRenameClientsGroupsHandler))       // POST команда для изменения списка разрешённых групп для переименования клиентов (1 запрос каждую секунду = 60 запросов в минуту, до 5 подряд)
	protectedMux.HandleFunc("/update-delete-clients-groups", protection.RateLimitMiddleware(rate.Every(1*time.Second), 5)(UpdateDeleteClientsGroupsHandler))       // POST команда для изменения списка разрешённых групп для удаления клиентов (1 запрос каждую секунду = 60 запросов в минуту, до 5 подряд)
	protectedMux.HandleFunc("/update-move-clients-groups", protection.RateLimitMiddleware(rate.Every(1*time.Second), 5)(UpdateMoveClientsGroupsHandler))           // POST команда для изменения списка разрешённых групп для перемещения (1 запрос каждую секунду = 60 запросов в минуту, до 5 подряд)
	protectedMux.HandleFunc("/update-terminal-commands-groups", protection.RateLimitMiddleware(rate.Every(1*time.Second), 5)(UpdateTerminalCommandsGroupsHandler)) // POST команда для изменения списка разрешённых групп для cmd/PowerShell команд (1 запрос каждую секунду = 60 запросов в минуту, до 5 подряд)
	protectedMux.HandleFunc("/update-install-programs-groups", protection.RateLimitMiddleware(rate.Every(1*time.Second), 5)(UpdateInstallProgramsGroupsHandler))   // POST команда для изменения списка разрешённых групп для установки ПО через QUIC (1 запрос каждую секунду = 60 запросов в минуту, до 5 подряд)

	// Маршруты MQTT сервера
	protectedMux.HandleFunc("/get-accounts-mqtt", mqtt_server.GetAccountsHandler)                                                                      // GET команда для получения данных учетных записей
	protectedMux.HandleFunc("/mqtt-auth-status", mqtt_server.GetMQTTAuthStatusHandler)                                                                 // GET команда для получения статуса смены MQTT авторизации клиентов
	protectedMux.HandleFunc("/update-account-mqtt", protection.RateLimitMiddleware(rate.Every(5*time.Second), 2)(mqtt_server.UpdateAccountHandler))    // POST команда для обновления данных учетной записи (1 запрос каждые 5 секунд = 12 запросов в минуту, до 2 подряд)
	protectedMux.HandleFunc("/update-allow-mqtt", protection.RateLimitMiddleware(rate.Every(5*time.Second), 2)(mqtt_server.UpdateAllowHandler))        // POST команда разрешает или запрещает подключение через учётную запись в конфиге "mqtt_config.json" с низким приоритетом "1" (1 запрос каждые 5 секунд = 12 запросов в минуту, до 2 подряд)
	protectedMux.HandleFunc("/mqtt-auth-resend", protection.RateLimitMiddleware(rate.Every(5*time.Second), 2)(mqtt_server.ResendMQTTAuthHandler))      // POST команда для повторной отправки запроса клиентам с ошибками смены пароля (1 запрос каждые 5 секунд = 12 запросов в минуту, до 2 подряд)
	protectedMux.HandleFunc("/mqtt-auth-clear", protection.RateLimitMiddleware(rate.Every(5*time.Second), 2)(mqtt_server.ClearMQTTAuthSessionHandler)) // POST команда для очистки сессии смены авторизации (1 запрос каждые 5 секунд = 12 запросов в минуту, до 2 подряд)

	// Маршрут для формирования и отправки команд в "cmd/PowerShell"
	protectedMux.HandleFunc("/send-terminal-command", protection.RateLimitMiddleware(rate.Every(3*time.Second), 2)(SendCommandHandler)) // POST команда для отправки cmd или PowerShell команды (1 запрос каждые 3 секунды = 20 запросов в минуту, до 2 подряд)

	// Маршруты для отчёта по "cmd/PowerShell"
	protectedMux.HandleFunc("/get-terminal-report", GetCommandsHandler)                                                                                                   // GET команда для получения списка записей (без полного вывода скриптов)
	protectedMux.HandleFunc("/get-terminal-client-info", GetTerminalClientInfoHandler)                                                                                    // GET команда с детальной информацией по клиенту (для открытия отдельного окна)
	protectedMux.HandleFunc("/resend-terminal-report", protection.RateLimitMiddleware(rate.Every(500*time.Millisecond), 10)(ResendCommandHandler))                        // POST команда для повторной отправки команды конкретному клиенту (1 запрос каждые 0,5 секунды = 120 запросов в минуту, до 10 подряд)
	protectedMux.HandleFunc("/delete-client-terminal-report", protection.RateLimitMiddleware(rate.Every(500*time.Millisecond), 10)(DeleteClientFromCommandByDateHandler)) // POST команда для удаления конкретной записи ClientID из БД по дате создания (1 запрос каждые 0,5 секунды = 120 запросов в минуту, до 10 подряд)
	protectedMux.HandleFunc("/delete-by-date-terminal-report", protection.RateLimitMiddleware(rate.Every(3*time.Second), 2)(DeleteCommandsByDateHandler))                 // POST команда для удаления всех записей в БД по дате создания (1 запрос каждые 3 секунды = 20 запросов в минуту, до 2 подряд)

	// Маршруты для формирования и отправки команд и загрузки файла в "Установка ПО"
	protectedMux.HandleFunc("/upload-file-QUIC", protection.RateLimitMiddleware(rate.Every(6*time.Second), 1)(UploadFileHandler))              // POST команда для загрузки исполняемого файла на сервер (1 запрос каждые 6 секунд = 10 запросов в минуту)
	protectedMux.HandleFunc("/delete-file-QUIC", protection.RateLimitMiddleware(rate.Every(6*time.Second), 1)(DeleteFileHandler))              // POST команда для удаления файла с сервера при отмене загрузки в WEB админке (1 запрос каждые 6 секунд = 10 запросов в минуту)
	protectedMux.HandleFunc("/send-install-QUIC-program", protection.RateLimitMiddleware(rate.Every(6*time.Second), 1)(InstallProgramHandler)) // POST команда для отправки JSON команд QUIC-клиентам (1 запрос каждые 6 секунд = 10 запросов в минуту)

	// Маршруты для отчёта по "Установка ПО"
	protectedMux.HandleFunc("/get-QUIC-report", GetQUICReportHandler)                                                                                              // GET команда для получения всех записей QUIC
	protectedMux.HandleFunc("/resend-QUIC-report", protection.RateLimitMiddleware(rate.Every(500*time.Millisecond), 10)(ResendQUICReportHandler))                  // POST команда для повторной отправки команды конкретному QUIC-клиенту (1 запрос каждые 0,5 секунды = 120 запросов в минуту, до 10 подряд)
	protectedMux.HandleFunc("/delete-client-QUIC-report", protection.RateLimitMiddleware(rate.Every(500*time.Millisecond), 10)(DeleteClientFromQUICByDateHandler)) // POST команда для удаления конкретной QUIC записи ClientID по дате создания (1 запрос каждые 0,5 секунды = 120 запросов в минуту, до 10 подряд)
	protectedMux.HandleFunc("/delete-by-date-QUIC-report", protection.RateLimitMiddleware(rate.Every(3*time.Second), 2)(DeleteQUICByDateHandler))                  // POST команда для удаления всех QUIC записей по дате создания (1 запрос каждые 3 секунды = 20 запросов в минуту, до 2 подряд)

	// Маршруты для получения информации о системе клиента
	protectedMux.HandleFunc("/getFile-info", protection.RateLimitMiddleware(rate.Every(1500*time.Millisecond), 1)(mqtt_client.HandleClientInfoFileRequest)) // POST команда для создания одноразовой ссылки на просмотр или скачивание файла отчёта (1 запрос каждые 1,5 секунды = 40 запросов в минуту)
	protectedMux.HandleFunc("/report-view/", mqtt_client.ReportViewHandler)                                                                                 // GET команда от открытия страницы отчёта по одноразовой ссылке

	// Маршруты для обновления или отката правил OWASP CRS для Coraza WAF с GitHub (О проекте)
	protectedMux.HandleFunc("/check-OWASP-CRS", protection.CheckOWASPHandler)                                                                                   // GET команда проверяет наличие новой версии правил
	protectedMux.HandleFunc("/update-OWASP-CRS", protection.RateLimitMiddleware(rate.Every(10*time.Second), 1)(protection.UpdateOWASPHandler))                  // POST команда обновляет правила (1 запрос каждые 10 секунд = 6 запросов в минуту)
	protectedMux.HandleFunc("/rollback-backup-OWASP-CRS", protection.RateLimitMiddleware(rate.Every(10*time.Second), 1)(protection.RollbackBackupOWASPHandler)) // POST команда для отката правил из бэкапа (1 запрос каждые 10 секунд = 6 запросов в минуту)

	// Маршруты для обновления или отката серверной части FiReMQ с GitHub/GitFlic (О проекте)
	protectedMux.HandleFunc("/check-FiReMQ", update.CheckHandler)                                                                             // GET команда проверяет наличие новой версии FiReMQ
	protectedMux.HandleFunc("/update-FiReMQ", protection.RateLimitMiddleware(rate.Every(10*time.Second), 1)(update.UpdateHandler))            // POST команда скачивает, проверяет, запускает утилиту "ServerUpdater" и корректно завершает работу FiReMQ (1 запрос каждые 10 секунд = 6 запросов в минуту)
	protectedMux.HandleFunc("/rollback-backup-FiReMQ", protection.RateLimitMiddleware(rate.Every(10*time.Second), 1)(update.RollbackHandler)) // POST команда для отката версии FiReMQ на предыдущий релиз через утилиту ServerUpdater (1 запрос каждые 10 секунд = 6 запросов в минуту)

	// Маршруты для отправки команды самоудаления клиентам "FiReAgent"
	protectedMux.HandleFunc("/uninstall-pending", GetPendingUninstallListHandler)                                                                     // GET команда показывает список ID, находящихся в офлайне и ожидающих удаления
	protectedMux.HandleFunc("/uninstall-fireagent", protection.RateLimitMiddleware(rate.Every(5*time.Second), 2)(UninstallFiReAgentHandler))          // POST команда на запроса самоудаления конкретных клиентов по их ID (1 запрос каждые 5 секунд = 12 запросов в минуту, до 2 подряд)
	protectedMux.HandleFunc("/uninstall-cancel", protection.RateLimitMiddleware(rate.Every(500*time.Millisecond), 10)(CancelPendingUninstallHandler)) // POST команда отменяет удаление конкретного офлайн ID (1 запрос каждые 0,5 секунды = 120 запросов в минуту, до 10 подряд)

	// Маршруты для просмотра и/или скачивания HTML лога сервера
	protectedMux.HandleFunc("/getServer-log", protection.RateLimitMiddleware(rate.Every(1500*time.Millisecond), 1)(logging.HandleLogFileRequest)) // POST команда для создания одноразовой ссылки на просмотр или скачивание файла лога (1 запрос каждые 1,5 секунды = 40 запросов в минуту)
	protectedMux.HandleFunc("/log-view/", logging.LogViewHandler)                                                                                 // GET команда от открытия страницы лога по одноразовой ссылке

	// Маршрут для получения информации о Linux сервере
	protectedMux.HandleFunc("/get-linux-info", protection.RateLimitMiddleware(rate.Every(2*time.Second), 2)(LinuxInfo.LinuxInfoHandler)) // POST команда для получения JSON информации о Linux сервере (1 запрос каждые 2 секунды = 30 запросов в минуту, до 2 подряд)

	// Маршруты для проверки обновлений клиентов (FiReAgent)
	protectedMux.HandleFunc("/get-update-clients", update_client.GetUpdateClientsHandler)                                                             // GET команда для получения списка всех клиентов с версиями модулей и датой последней проверки обновлений
	protectedMux.HandleFunc("/send-update-check", protection.RateLimitMiddleware(rate.Every(8*time.Second), 1)(update_client.SendCheckUpdateHandler)) // POST команда для отправки принудительной проверки обновлений всем онлайн-клиентам (1 запрос каждые 8 секунд = 7 запросов в минуту)

	/* * * * * * * * * * * * * * * * * * * * * */
	// ДЛЯ ТЕСТА!!! Временный обход проверок Coraza WAF для тестирования запроса с пропуском CSRF
	//http.HandleFunc("/getServer-log", logging.HandleLogFileRequest)
	//http.HandleFunc("/log-view/", logging.LogViewHandler)
	//http.HandleFunc("/rollback-backup-FiReMQ", update.RollbackHandler)
	/* * * * * * * * * * * * * * * * * * * * * */

	// Обработка всех маршрутов
	http.Handle("/", protection.SecurityHeadersMiddleware(protection.OriginCheckMiddleware(CorazaMiddleware(getWAF, AuthMiddleware(protectedMux)))))

	if err := http.ListenAndServeTLS(pathsOS.Web_Host+":"+pathsOS.Web_Port, pathsOS.Path_Web_Cert, pathsOS.Path_Web_Key, nil); err != nil {
		logging.LogError("WEB: Критическая ошибка WEB-сервера: %v", err)
		time.Sleep(100 * time.Millisecond) // Небольшая пауза для надёжности записи лога
		log.Fatal(err)                     // Дублирование в stderr и выход с кодом 1
	}
}
