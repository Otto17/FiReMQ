// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package pathsOS

import (
	"bufio"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Функции логирования (внедряются из main.go для избежания циклического импорта)
var (
	LogSystem func(format string, args ...any) = func(format string, args ...any) {
		log.Printf("[СИСТЕМА] "+format, args...)
	}
	LogError func(format string, args ...any) = func(format string, args ...any) {
		log.Printf("[ОШИБКА] "+format, args...)
	}
)

// Константы для прав доступа
const (
	DirPerm           os.FileMode = 0750 // Для директорий: rwxr-x--- (только владелец и группа)
	FilePerm          os.FileMode = 0640 // Для обычных файлов: rw-r----- (владелец rw, группа r)
	SensitiveFilePerm os.FileMode = 0600 // Для чувствительных файлов (ключи): rw------- (только владелец)
)

// Переменные с путями (загружаются из "server.conf")
var (
	Path_DB                     string // Путь к БД
	Path_Config_Coraza          string // Конфиг WAF
	Path_Folder_Rules_OWASP_CRS string // Правила OWASP CRS
	Path_Folder_tmp_OWASP_CRS   string // Временная папка OWASP CRS
	Path_Config_Base            string // Базовый путь конфигов
	Path_Rules_Base             string // Базовый путь правил
	Path_Setup_OWASP_CRS        string // Конфиг CRS
	Path_Setup_Base             string // Имя конфига CRS
	URL_OWASP_CRS_LatestRelease string // URL релиза OWASP CRS
	Path_7zip                   string // Путь к 7-Zip
	Path_Info                   string // Инфо файлы клиентов
	Web_Host                    string // Хост WEB
	Web_Port                    string // Порт WEB
	Path_Web_Data               string // Данные WEB
	Path_Web_Cert               string // SSL сертификат WEB
	Path_Web_Key                string // SSL ключ WEB
	MQTT_Host                   string // Хост MQTT сервера
	MQTT_Port                   string // Порт MQTT сервера
	Path_Config_MQTT            string // Конфиг MQTT
	Path_Server_MQTT_CA         string // CA MQTT сервера
	Path_Server_MQTT_Cert       string // Сертификат MQTT сервера
	Path_Server_MQTT_Key        string // Ключ MQTT сервера
	MQTT_Client_Host            string // Хост брокера для локального клиента AutoPaho
	MQTT_Client_Port            string // Порт TCP брокера MQTT для локального клиента AutoPaho
	Path_Client_MQTT_CA         string // CA MQTT клиента
	Path_Client_MQTT_Cert       string // Сертификат MQTT клиента
	Path_Client_MQTT_Key        string // Ключ MQTT клиента
	QUIC_Host                   string // Хост QUIC
	QUIC_Port                   string // Порт QUIC
	Path_QUIC_Downloads         string // Загрузки QUIC
	Path_Client_QUIC_CA         string // CA QUIC клиента
	Path_Server_QUIC_Cert       string // Сертификат QUIC сервера
	Path_Server_QUIC_Key        string // Ключ QUIC сервера
	Key_ChaCha20_Poly1305       string // Ключ шифрования
	Path_Backup                 string // Путь бэкапов
	DB_Backup_Interval          string // Интервал создания бэкапов БД
	DB_Backup_Retention_Count   string // Кол-во хранимых бэкапов БД
	Path_Logs                   string // Путь к директории логов (для обновления FiReMQ)
	Logs_Retention_Days         string // Период хранения логов в HTML, в днях
	Logs_Min_Count_Per_Type     string // Минимальное количество логов КАЖДОГО ТИПА, которое всегда должно оставаться в HTML
	Update_PrimaryRepo          string // Выбор основного репозитория: "github" или "gitflic"
	Update_GitHubReleasesURL    string // URL релизов GitHub
	Update_GitFlicReleasesURL   string // URL релизов GitFlic
	Update_GitFlicToken         string // Токен GitFlic

	// Фактический путь к server.conf (определяется в Init)
	ServerConfPath string
)

// configEntry описывает один параметр конфигурации: имя, комментарий, указатель на переменную и значение по умолчанию
type configEntry struct {
	Name    string
	Comment string
	Ptr     *string
	Default string
}

// getPlatformDefaults возвращает базовые директории, специфичные для текущей операционной системы
func getPlatformDefaults() (configDir, varDir, certsDir, backupDir, webDataDir, sevenZipDir, infoDir, downloadsDir, dbDir, logsDir string) {
	if runtime.GOOS == "linux" {
		// Стандартные пути для Linux (FHS)
		configDir = "/etc/firemq/config"                  // Главный конфиги здесь
		varDir = "/var/lib/firemq"                        // Для DB, Info_files, Downloads
		certsDir = "/etc/firemq/certs"                    // Сертификаты вне "config/"
		backupDir = "/var/backups/firemq/Backup"          // Бэкапы
		logsDir = "/var/log/firemq"                       // Логи
		webDataDir = "/usr/local/share/firemq/data"       // WEB контент
		sevenZipDir = "/usr/local/share/firemq/7z"        // Утилита 7-ZIP
		infoDir = filepath.Join(varDir, "Info_files")     // Файлы с информацией о компьютерах клиентов (Info_files)
		downloadsDir = filepath.Join(varDir, "Downloads") // Папка для загрузки файлов через QUIC
		dbDir = filepath.Join(varDir, "db")               // БД
	} else {
		// Для Windows (относительные пути)
		configDir = "config"       // Главный конфиги здесь
		varDir = "."               // Для DB, Info_files, Downloads
		certsDir = "certs"         // Сертификаты
		backupDir = "Backup"       // Бэкапы
		logsDir = "log"            // Логи
		webDataDir = "data"        // WEB контент
		sevenZipDir = "7z"         // Утилита 7-ZIP
		infoDir = "Info_Files"     // Файлы с информацией о компьютерах клиентов (Info_files)
		downloadsDir = "Downloads" // Папка для загрузки файлов через QUIC (Downloads)
		dbDir = "db"               // БД
	}
	return
}

// entries возвращает список всех параметров, которые должны присутствовать в "server.conf"
func entries() []configEntry {
	configDir, _, certsDir, backupDir, webDataDir, sevenZipDir, infoDir, downloadsDir, dbDir, logsDir := getPlatformDefaults()

	return []configEntry{
		{"Path_DB", "Путь до директории с БД", &Path_DB, filepath.Join(dbDir, "FiReMQ_DB")},
		{"Path_Config_Coraza", "Путь до конфига Coraza WAF", &Path_Config_Coraza, filepath.Join(configDir, "coraza.conf")},

		{"Path_Folder_Rules_OWASP_CRS", "Директории правил OWASP CRS", &Path_Folder_Rules_OWASP_CRS, filepath.Join(configDir, "rules")},
		{"Path_Folder_tmp_OWASP_CRS", "Временная директория для обновления OWASP CRS", &Path_Folder_tmp_OWASP_CRS, filepath.Join(configDir, "tmp")},
		{"Path_Config_Base", "Базовый каталог конфигов CRS", &Path_Config_Base, configDir}, // Для Linux это будет /etc/firemq
		{"Path_Rules_Base", "Базовый каталог правил CRS", &Path_Rules_Base, "rules"},
		{"Path_Setup_OWASP_CRS", "Полный путь до файла конфига \"crs-setup.conf\"", &Path_Setup_OWASP_CRS, filepath.Join(configDir, "crs-setup.conf")},
		{"Path_Setup_Base", "Имя файла \"crs-setup.conf\" конфига", &Path_Setup_Base, "crs-setup.conf"},
		{"URL_OWASP_CRS_LatestRelease", "Ссылка на последний релиз OWASP CRS из GitHub (автоматически преобразуется в API URL, используется для проверки и обновления правил для Coraza WAF)", &URL_OWASP_CRS_LatestRelease, "https://github.com/coreruleset/coreruleset/releases/latest"},

		{"Path_7zip", "Путь до ДИРЕКТОРИИ с консольной 7-Zip утилитой", &Path_7zip, sevenZipDir},
		{"Path_Info", "Путь до директории с архивами файлов с информацией о железе клиентов", &Path_Info, infoDir},

		{"Web_Host", "Хост WEB-сервера, 0.0.0.0 (для доступа извне) или конкретный IP (например, 192.168.1.100 для внутренней сети)", &Web_Host, "0.0.0.0"},
		{"Web_Port", "Порт TCP WEB-сервера", &Web_Port, "8443"},
		{"Path_Web_Data", "Путь до директории с файлами WEB-интерфейса (html, css, js)", &Path_Web_Data, webDataDir}, // !!! НОВЫЙ ПАРАМЕТР
		{"Path_Web_Cert", "SSL сертификат для WEB админки", &Path_Web_Cert, filepath.Join(certsDir, "server-cert.pem")},
		{"Path_Web_Key", "SSL ключ для WEB админки", &Path_Web_Key, filepath.Join(certsDir, "server-key.pem")},

		{"MQTT_Host", "Хост MQTT сервера, (0.0.0.0 для доступа из любой сети) или конкретный IP (например, 127.0.0.1) только для локальных подключений", &MQTT_Host, "0.0.0.0"},
		{"MQTT_Port", "Порт TCP MQTT сервера", &MQTT_Port, "8783"},
		{"Path_Config_MQTT", "Конфиг MQTT сервера", &Path_Config_MQTT, filepath.Join(configDir, "mqtt_config.json")},
		{"Path_Server_MQTT_CA", "MQTT CA сертификат", &Path_Server_MQTT_CA, filepath.Join(certsDir, "server-cacert.pem")},
		{"Path_Server_MQTT_Cert", "MQTT сертификат сервера", &Path_Server_MQTT_Cert, filepath.Join(certsDir, "server-cert.pem")},
		{"Path_Server_MQTT_Key", "MQTT ключ сервера", &Path_Server_MQTT_Key, filepath.Join(certsDir, "server-key.pem")},

		{"MQTT_Client_Host", "Хост брокера для локального клиента AutoPaho", &MQTT_Client_Host, "localhost"},
		{"MQTT_Client_Port", "Порт TCP брокера MQTT для локального клиента AutoPaho", &MQTT_Client_Port, "8783"},
		{"Path_Client_MQTT_CA", "MQTT CA клиент", &Path_Client_MQTT_CA, filepath.Join(certsDir, "client-cacert.pem")},
		{"Path_Client_MQTT_Cert", "MQTT сертификат клиента", &Path_Client_MQTT_Cert, filepath.Join(certsDir, "client-cert.pem")},
		{"Path_Client_MQTT_Key", "MQTT ключ клиента", &Path_Client_MQTT_Key, filepath.Join(certsDir, "client-key.pem")},

		{"QUIC_Host", "Хост QUIC сервера, (0.0.0.0 для доступа из любой сети) или конкретный IP (например, 127.0.0.1) для ограничения доступа", &QUIC_Host, "0.0.0.0"},
		{"QUIC_Port", "Порт UDP QUIC сервера", &QUIC_Port, "4242"},
		{"Path_QUIC_Downloads", "Путь до директории с исполняемыми файлами QUIC-сервера", &Path_QUIC_Downloads, downloadsDir},
		{"Path_Client_QUIC_CA", "CA для QUIC клиента", &Path_Client_QUIC_CA, filepath.Join(certsDir, "client-cacert.pem")},
		{"Path_Server_QUIC_Cert", "Сертификат QUIC сервера", &Path_Server_QUIC_Cert, filepath.Join(certsDir, "server-cert.pem")},
		{"Path_Server_QUIC_Key", "Ключ QUIC сервера", &Path_Server_QUIC_Key, filepath.Join(certsDir, "server-key.pem")},

		{"Key_ChaCha20_Poly1305", "Файл ключа ChaCha20-Poly1305, для шифрования/дешифрования логина авторизованного админа в куках браузера", &Key_ChaCha20_Poly1305, filepath.Join(configDir, "chacha20_key")},

		{"Path_Backup", "Путь до директории с бэкапами FiReMQ", &Path_Backup, backupDir},
		{"DB_Backup_Interval", "Интервал создания полных бэкапов БД в часах (0 - отключено)", &DB_Backup_Interval, "12"},
		{"DB_Backup_Retention_Count", "Количество хранимых бэкапов БД (при достижении лимита, новый бэкап заменяет самый старый)", &DB_Backup_Retention_Count, "15"},
		{"Path_Logs", "Путь до директории с логами (для обновления FiReMQ)", &Path_Logs, logsDir},
		{"Logs_Retention_Days", "Период хранения логов в HTML, в днях (0 — отключить автоматическую очистку)", &Logs_Retention_Days, "365"},
		{"Logs_Min_Count_Per_Type", "Минимальное количество логов КАЖДОГО ТИПА, которое всегда должно оставаться в HTML (0 — без ограничения)", &Logs_Min_Count_Per_Type, "500"},

		{"Update_PrimaryRepo", "Выбор основного репозитория: \"gitflic\" или \"github\" для обновления FiReMQ (резервный задействуется автоматически при проблемах с основным репозиторием)", &Update_PrimaryRepo, "gitflic"},
		{"Update_GitHubReleasesURL", "Ссылка на последний релиз FiReMQ из GitHub (автоматически преобразуется в API URL)", &Update_GitHubReleasesURL, "https://github.com/Otto17/FiReMQ/releases/latest"},
		{"Update_GitFlicReleasesURL", "Ссылка на релизы FiReMQ из GitFlic (автоматически преобразуется в API URL)", &Update_GitFlicReleasesURL, "https://gitflic.ru/project/otto/firemq/release"},
		{"Update_GitFlicToken", "Публичный токен доступа к GitFlic API для проверки и скачивания обновлений", &Update_GitFlicToken, "efed450c-d7b2-477e-8f8f-88d2a377b8ca"},
	}
}

// defaultConfPath возвращает путь к файлу server.conf, используя переменные окружения или пути по умолчанию
func defaultConfPath() string {
	if v := os.Getenv("FIREMQ_SERVER_CONF"); v != "" {
		return v
	}
	if runtime.GOOS == "linux" {
		return "/etc/firemq/config/server.conf"
	}
	// Для Windows использует директорию исполняемого файла
	exe, err := os.Executable()
	if err == nil {
		return filepath.Join(filepath.Dir(exe), "config", "server.conf")
	}
	// Запасной путь, если не удалось определить путь к exe
	return filepath.Join("config", "server.conf")
}

// normalizeIn приводит строку пути, прочитанную из конфига, к формату, соответствующему текущей ОС
func normalizeIn(key, s string) string {
	// Игнорирует нормализацию для URL
	if strings.HasPrefix(strings.ToLower(s), "http://") || strings.HasPrefix(strings.ToLower(s), "https://") {
		return s // URL не трогать
	}

	original := s
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// Удаляет обрамляющие кавычки
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
	}

	// Флаги для логирования изменений
	hadMultiple := strings.Contains(s, "//") || strings.Contains(s, `\\`)
	hadMixed := strings.Contains(s, "/") && strings.Contains(s, `\`)

	// Приводит все слеши к прямому формату (UNIX-стиль)
	s = strings.ReplaceAll(s, "\\", "/")

	// Удаляет повторяющиеся слеши
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}

	// Преобразует путь в формат ОС
	normalized := filepath.FromSlash(s)

	// Логирует только при обнаружении некорректных или смешанных слешей
	if hadMultiple || hadMixed {
		LogSystem("Главный конфиг: исправлена запись [%s]: \"%s\" → \"%s\"", key, original, normalized)
	}

	return normalized
}

// normalizeOut приводит строку пути к формату, подходящему для записи в server.conf
func normalizeOut(s string) string {
	// Игнорирует нормализацию для URL
	if strings.HasPrefix(strings.ToLower(s), "http://") || strings.HasPrefix(strings.ToLower(s), "https://") {
		return s // URL записывает как есть
	}

	// Сначала все обратные слеши в прямые
	s = strings.ReplaceAll(s, "\\", "/")

	// Удаляет повторяющиеся слеши
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}

	// Возвращает обратные слеши для Windows, если это требуется ОС
	if os.PathSeparator == '\\' {
		return strings.ReplaceAll(s, "/", "\\")
	}
	return s
}

// EnsureDir создаёт директорию, если она не существует, с корректными правами доступа для ОС
func EnsureDir(dir string) error {
	if dir == "" || dir == "." {
		return nil
	}

	var perm os.FileMode = 0755 // Права по умолчанию (для Windows)
	if runtime.GOOS == "linux" {
		perm = DirPerm // Использует безопасные права для Linux
	}
	return os.MkdirAll(dir, perm)
}

// writeConf создаёт или перезаписывает "server.conf" на основе текущих значений и шаблона
func writeConf(path string, es []configEntry, extras map[string]string) error {
	var b strings.Builder
	b.WriteString("# \"server.conf\" — автоматически сгенерирован: " + time.Now().Format("02-01-2006г в 15:04:05.") + "\n\n")
	b.WriteString("# Если требуется, меняйте значения справа от '=' и перезапустите сервер.\n")
	b.WriteString("# При синтаксических ошибках (нет '=', пустой ключ, дубликаты ключей или ошибка чтения), тогда FiReMQ переименовывает конфиг в \"СБОЙНЫЙ_server.conf_old\" и создаёт новый по шаблону.\n")
	b.WriteString("# Если конфиг корректен, но требует нормализации (неправильные слеши для текущей ОС, отсутствуют некоторые известные ключи), конфиг будет автоматически исправлен и перезаписан без переименования.\n")
	b.WriteString("# Можно подсовывать конфиг от Linux для Windows и на оборот, FiReMQ сам, автоматически нормализует слеши в конфиге под текущую платформу.\n\n\n\n")

	// Записывает основные ключи
	for _, e := range es {
		if e.Comment != "" {
			b.WriteString("# " + e.Comment + "\n")
		}
		val := *e.Ptr
		if val == "" {
			val = e.Default
		}
		// Использует normalizeOut для записи пути в правильном формате ОС
		b.WriteString(e.Name + "=" + normalizeOut(val) + "\n\n")
	}

	// Сохраняет неизвестные ключи (чтобы не потерялись)
	if len(extras) > 0 {
		b.WriteString("# Неизвестные ключи (сохранены как есть):\n")
		keys := make([]string, 0, len(extras))
		for k := range extras {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(k + "=" + normalizeOut(extras[k]) + "\n")
		}
	}

	// Использует WriteFile для установки правильных прав
	return WriteFile(path, []byte(b.String()), FilePerm)
}

// loadOrCreate загружает конфигурацию из файла или создаёт новый файл, обрабатывая ошибки синтаксиса
func loadOrCreate(path string) error {
	es := entries()

	// Устанавливает дефолтные значения в переменных до загрузки
	for i := range es {
		*es[i].Ptr = es[i].Default
	}
	_ = EnsureDir(filepath.Dir(path))

	// Создаёт файл по шаблону, если он отсутствует
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := writeConf(path, es, nil); err != nil {
			return err
		}
		LogSystem("Главный конфиг: создан новый конфиг по умолчанию: %s", path)
		return nil
	} else if err != nil {
		return fmt.Errorf("ошибка доступа к %s: %w", path, err)
	}

	// Читает файл в локальные структуры для проверки и обработки
	f, err := os.Open(path)
	if err != nil {
		return err
	}

	knownNames := make(map[string]struct{}, len(es))
	for i := range es {
		knownNames[es[i].Name] = struct{}{}
	}

	present := make(map[string]string, len(es)) // Собранные значения для известных ключей
	extras := make(map[string]string)           // Неизвестные ключи
	normalized := false                         // Флаг, указывающий на необходимость нормализации путей
	syntaxErr := false                          // Флаг синтаксической ошибки

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Удаляет комментарии, идущие после значений
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
			if line == "" {
				continue
			}
		}

		eq := strings.IndexRune(line, '=')
		if eq <= 0 {
			syntaxErr = true // Синтаксическая ошибка: отсутствует '=' или ключ пуст
			continue
		}
		key := strings.TrimSpace(line[:eq])
		valRaw := strings.TrimSpace(line[eq+1:])

		if key == "" {
			syntaxErr = true // Синтаксическая ошибка: ключ пуст
			continue
		}
		if _, dup := present[key]; dup {
			syntaxErr = true // Синтаксическая ошибка: дубликат ключа
			continue
		}

		norm := normalizeIn(key, valRaw)
		if _, isKnown := knownNames[key]; isKnown {
			// Проверяет, изменился ли путь после нормализации
			if valRaw != normalizeOut(norm) {
				normalized = true
			}
			present[key] = norm
		} else {
			extras[key] = norm
		}
	}
	if err := sc.Err(); err != nil {
		syntaxErr = true
	}
	_ = f.Close() // Закрывает файл перед возможным os.Rename (особенно важно для Windows)

	// Если найдена синтаксическая ошибка, создает бэкап и новый конфиг с дефолтными значениями
	if syntaxErr {
		bad := filepath.Join(filepath.Dir(path), "СБОЙНЫЙ_server.conf_old")
		if _, err := os.Stat(bad); err == nil {
			bad = bad + "_" + time.Now().Format("20060102_150405") // Добавляет временную метку, если бэкап уже существует
		}
		if err := os.Rename(path, bad); err != nil {
			LogError("Главный конфиг: Не удалось создать бэкап повреждённого конфига: %v", err)
		} else {
			LogError("Главный конфиг: Повреждённый конфиг переименован в: %s", bad)
		}
		if err := writeConf(path, es, nil); err != nil {
			return err
		}

		LogSystem("Главный конфиг: Создан новый конфиг по умолчанию: %s", path)
		// Переменные уже содержат дефолты
		return nil
	}

	// Применяет значения из файла к глобальным переменным
	for i := range es {
		if v, ok := present[es[i].Name]; ok {
			*es[i].Ptr = v
		}
	}

	// Проверяет необходимость перезаписи: нормализация, наличие неизвестных ключей или отсутствие известных
	needRewrite := normalized || len(extras) > 0 || len(present) != len(es)
	if needRewrite {
		if err := writeConf(path, es, extras); err != nil {
			return err
		}
		LogSystem("Главный конфиг: Конфиг перезаписан (нормализация/добавление ключей): %s", path)
	}
	return nil
}

// Init инициализирует пути, загружая или создавая server.conf
func Init() error {
	ServerConfPath = defaultConfPath()
	return loadOrCreate(ServerConfPath)
}

// Resolve7zip возвращает полный путь к исполняемому файлу 7-Zip, выполняя поиск и устанавливая права
func Resolve7zip() (string, error) {
	// Определяет правильное имя исполняемого файла для текущей ОС
	var targetFilename string
	switch runtime.GOOS {
	case "linux":
		targetFilename = "7zzs"
	case "windows":
		targetFilename = "7z.exe"
	}

	// Возвращает ошибку, если ОС не поддерживается
	if targetFilename == "" {
		return "", fmt.Errorf("не удалось определить имя утилиты 7-Zip для операционной системы: %s", runtime.GOOS)
	}

	// Определяет директорию поиска из конфига
	dirFromConfig := Path_7zip

	// Использует exeDir для корректного разрешения относительных путей
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("не удалось определить путь к исполняемому файлу: %w", err)
	}
	exeDir := filepath.Dir(exe)

	// Преобразует путь из конфига в абсолютный, если он относительный
	searchDir := dirFromConfig
	if !filepath.IsAbs(searchDir) {
		searchDir = filepath.Join(exeDir, searchDir)
	}

	// Ищет целевой файл
	fullPath := filepath.Join(searchDir, targetFilename)
	if _, err := os.Stat(fullPath); err == nil {
		// Файл найден
		if runtime.GOOS != "windows" {
			// Для Linux устанавливает права на исполнение
			if err := os.Chmod(fullPath, 0755); err != nil {
				LogError("Главный конфиг: Не удалось установить права на исполнение для %s: %v", fullPath, err)
			}
		}
		return fullPath, nil
	}

	// Если файл не найден
	return "", fmt.Errorf("утилита 7-Zip ('%s') не найдена в указанной в конфиге директории: '%s'", targetFilename, dirFromConfig)
}

// VerifyAndFixPermissions проверяет и исправляет права доступа и владельца для ключевых файлов и директорий на Linux
func VerifyAndFixPermissions() error {
	if runtime.GOOS != "linux" {
		return nil // Проверка прав актуальна только для Linux
	}

	var (
		uid                 int
		gid                 int
		ownerChangePossible bool // Флаг, определяющий возможность смены владельца
	)

	// Пытается найти пользователя 'firemq' для установки владельца
	firemqUser, err := user.Lookup("firemq")
	if err != nil {
		// Если пользователь не найден, пропускает chown, но продолжает chmod
		LogSystem("Главный конфиг: Пользователь 'firemq' не найден. Смена владельца (chown) будет пропущена. Проверяются только права доступа. Ошибка: %v", err)
		ownerChangePossible = false
	} else {
		// Устанавливает UID и GID для chown
		uid, _ = strconv.Atoi(firemqUser.Uid)
		gid, _ = strconv.Atoi(firemqUser.Gid)
		ownerChangePossible = true
	}

	// Структура для описания объекта проверки
	type checkItem struct {
		Path       string
		Perm       os.FileMode
		IsOptional bool
		IsDir      bool
		Recursive  bool // Флаг для рекурсивного обхода
	}

	// Собирает список всех объектов для проверки
	items := []checkItem{
		// Директории, где постоянно создаются новые файлы (требуют рекурсивной обработки)
		{Path: Path_DB, Perm: DirPerm, IsDir: true, Recursive: true},
		{Path: Path_Info, Perm: DirPerm, IsDir: true, Recursive: true},
		{Path: Path_Logs, Perm: DirPerm, IsDir: true, Recursive: true},
		{Path: Path_Backup, Perm: DirPerm, IsDir: true, Recursive: true},
		{Path: Path_QUIC_Downloads, Perm: DirPerm, IsDir: true, Recursive: true},
		{Path: Path_Folder_Rules_OWASP_CRS, Perm: DirPerm, IsDir: true, IsOptional: true, Recursive: true},

		// Директории, которые нужно обработать только на верхнем уровне
		{Path: filepath.Dir(ServerConfPath), Perm: DirPerm, IsDir: true},
		{Path: filepath.Dir(Path_Web_Key), Perm: DirPerm, IsDir: true},
		{Path: Path_Web_Data, Perm: DirPerm, IsDir: true},
		{Path: Path_7zip, Perm: DirPerm, IsDir: true},

		// Обычные файлы
		{Path: ServerConfPath, Perm: FilePerm},
		{Path: Path_Config_Coraza, Perm: FilePerm, IsOptional: true},
		{Path: Path_Config_MQTT, Perm: FilePerm, IsOptional: true},
		{Path: Path_Setup_OWASP_CRS, Perm: FilePerm, IsOptional: true},
		{Path: Path_Web_Cert, Perm: FilePerm, IsOptional: true},
		{Path: Path_Server_MQTT_CA, Perm: FilePerm, IsOptional: true},
		{Path: Path_Server_MQTT_Cert, Perm: FilePerm, IsOptional: true},
		{Path: Path_Client_MQTT_CA, Perm: FilePerm, IsOptional: true},
		{Path: Path_Client_MQTT_Cert, Perm: FilePerm, IsOptional: true},
		{Path: Path_Client_QUIC_CA, Perm: FilePerm, IsOptional: true},
		{Path: Path_Server_QUIC_Cert, Perm: FilePerm, IsOptional: true},

		// Чувствительные файлы (приватные ключи)
		{Path: Path_Web_Key, Perm: SensitiveFilePerm, IsOptional: true},
		{Path: Path_Server_MQTT_Key, Perm: SensitiveFilePerm, IsOptional: true},
		{Path: Path_Client_MQTT_Key, Perm: SensitiveFilePerm, IsOptional: true},
		{Path: Path_Server_QUIC_Key, Perm: SensitiveFilePerm, IsOptional: true},
		{Path: Key_ChaCha20_Poly1305, Perm: SensitiveFilePerm, IsOptional: true},
	}

	// fixItem — внутренняя функция-помощник для исправления прав и владельца
	fixItem := func(path string, perm os.FileMode) {
		info, err := os.Lstat(path) // Использует Lstat, чтобы не следовать по символическим ссылкам
		if err != nil {
			LogError("Главный конфиг: Не удалось получить информацию о '%s': %v", path, err)
			return
		}

		// 1. Исправляет права доступа (chmod)
		if info.Mode().Perm() != perm {
			if err := os.Chmod(path, perm); err != nil {
				LogError("Главный конфиг: Не удалось изменить права для '%s': %v.", path, err)
			}
		}

		// 2. Устанавливает владельца (chown), если пользователь 'firemq' был найден
		if ownerChangePossible {
			if err := os.Chown(path, uid, gid); err != nil {
				LogError("Главный конфиг: Не удалось изменить владельца для '%s' на %d:%d: %v.", path, uid, gid, err)
			}
		}
	}

	// Основной цикл обработки всех объектов
	for _, item := range items {
		if _, err := os.Stat(item.Path); os.IsNotExist(err) {
			// Создает обязательные директории, если они отсутствуют
			if item.IsDir && !item.IsOptional {
				if err := EnsureDir(item.Path); err != nil {
					LogError("Главный конфиг: Не удалось создать обязательную директорию '%s': %v", item.Path, err)
					continue
				}
				LogSystem("Главный конфиг: Создана директория: %s", item.Path)
				fixItem(item.Path, item.Perm)
			}
			continue
		}

		if item.IsDir && item.Recursive {
			// Рекурсивно обходит и исправляет права и владельца для всех вложенных файлов и директорий
			err := filepath.WalkDir(item.Path, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				perm := FilePerm
				if d.IsDir() {
					perm = DirPerm
				}
				fixItem(path, perm)
				return nil
			})
			if err != nil {
				LogError("Главный конфиг: Ошибка при рекурсивном обходе '%s': %v", item.Path, err)
			}
		} else {
			// Исправляет права для отдельного файла или директории
			fixItem(item.Path, item.Perm)
		}
	}

	return nil
}

// WriteFile записывает данные в файл с корректными правами для текущей ОС
func WriteFile(path string, data []byte, defaultPerm os.FileMode) error {
	perm := os.FileMode(0644) // Права по умолчанию для Windows
	if runtime.GOOS == "linux" {
		perm = defaultPerm // Использует заданные безопасные права для Linux
	}
	return os.WriteFile(path, data, perm)
}

// VerifyExecutableFilesRights проверяет и, при необходимости, исправляет права на выполнение для 7zzs и ServerUpdater под Linux
func VerifyExecutableFilesRights() {
	if runtime.GOOS != "linux" {
		return
	}

	LogSystem("Главный конфиг: Проверка прав доступа исполняемых файлов (7zzs, ServerUpdater)...")

	type execFile struct {
		Path string
		Name string
	}

	var execs []execFile

	// 1) Путь к 7zzs
	zzsPath := filepath.Join(Path_7zip, "7zzs")
	execs = append(execs, execFile{Path: zzsPath, Name: "7zzs"})

	// 2) Путь к ServerUpdater (расположен рядом с бинарём FiReMQ)
	exePath, err := os.Executable()
	if err != nil {
		LogError("Главный конфиг: Не удалось определить путь к FiReMQ: %v", err)
	} else {
		updPath := filepath.Join(filepath.Dir(exePath), "ServerUpdater")
		execs = append(execs, execFile{Path: updPath, Name: "ServerUpdater"})
	}

	for _, e := range execs {
		info, err := os.Stat(e.Path)
		if err != nil {
			if os.IsNotExist(err) {
				LogSystem("Главный конфиг: %s отсутствует (%s) — пропуск проверки.", e.Name, e.Path)
				continue
			}
			LogError("Главный конфиг: Не удалось получить информацию о %s (%s): %v", e.Name, e.Path, err)
			continue
		}

		// Проверяет, что это файл
		if info.IsDir() {
			LogSystem("Главный конфиг: %s (%s) является директорией, ожидался исполняемый файл", e.Name, e.Path)
			continue
		}

		current := info.Mode().Perm()
		const minRights = 0755 // rwxr-xr-x
		// Проверяет, установлены ли необходимые права на выполнение (0755)
		if current != minRights {
			LogSystem("Главный конфиг: Некорректные права для '%s'. Текущие: %o, требуемые: %o. Исправляю...", e.Path, current, minRights)
			if err := os.Chmod(e.Path, minRights); err != nil {
				LogError("Главный конфиг: Не удалось изменить права для '%s': %v", e.Path, err)
			} else {
				LogSystem("Главный конфиг: Исправлены права для '%s' (%s) → %o", e.Name, e.Path, minRights)
			}
		}
	}
}
