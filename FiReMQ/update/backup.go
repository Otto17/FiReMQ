// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package update

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"FiReMQ/logging" // Локальный пакет с логированием в HTML файл
	"FiReMQ/pathsOS" // Локальный пакет с путями для разных платформ
)

// manifestBackup представляет метаданные для полного бэкапа
type manifestBackup struct {
	CreatedAt      string                `json:"CreatedAt"`
	Version        string                `json:"Version"`
	ExeDir         string                `json:"ExeDir"`
	PathBackup     string                `json:"PathBackup"`
	ServerConfPath string                `json:"ServerConfPath"`
	Entries        []manifestBackupEntry `json:"Entries"`
}

// manifestBackupEntry описывает один зарезервированный файл/каталог в бэкапе
type manifestBackupEntry struct {
	// Key — имя ключа server.conf, из которого взят путь (или спец.значение "__exe__" / "__server_conf__")
	Key string `json:"Key"`
	// DestPath — конечный путь на диске (куда класть при восстановлении)
	DestPath string `json:"DestPath"`
	// ZipPath — путь файла внутри ZIP-архива
	ZipPath string `json:"ZipPath"`
}

// CreateFullBackup создает ZIP-архив, содержащий все файлы и папки, указанные в server.conf
func CreateFullBackup() (string, error) {
	dir, err := exeDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить директорию FiReMQ: %w", err)
	}

	// Определяет директорию бэкапа, используя Path_Backup или значение по умолчанию "Backup"
	backupDir := strings.TrimSpace(pathsOS.Path_Backup)
	if backupDir == "" {
		backupDir = "Backup"
	}
	// Преобразует относительный путь в абсолютный
	if !filepath.IsAbs(backupDir) {
		backupDir = filepath.Clean(filepath.Join(dir, backupDir))
	}

	if err := pathsOS.EnsureDir(backupDir); err != nil {
		return "", fmt.Errorf("не удалось создать директорию бэкапов %q: %w", backupDir, err)
	}

	backupName := fmt.Sprintf("bak_%s_ver=%s_FiReMQ.zip", time.Now().Format(backupTimestampLayout), CurrentVersion)
	backupPath := filepath.Join(backupDir, backupName)

	out, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("не удалось создать файл бэкапа %q: %w", backupPath, err)
	}
	// Закрывает файл только при возникновении ошибки до успешного закрытия ZIP-писателя
	defer func() {
		_ = out.Close()
	}()

	zw := zip.NewWriter(out)
	// Закрывает ZIP-писатель в defer, чтобы гарантировать запись центрального каталога
	defer func() {
		_ = zw.Close()
	}()

	// Инициализирует манифест бэкапа
	mf := manifestBackup{
		CreatedAt:      time.Now().Format(time.RFC3339),
		Version:        CurrentVersion,
		ExeDir:         dir,
		PathBackup:     backupDir,
		ServerConfPath: pathsOS.ServerConfPath,
	}

	seenFiles := make(map[string]string) // absPath -> zipPath (предотвращает дублирование файлов)
	usedZip := make(map[string]struct{})

	// addFile добавляет отдельный файл в ZIP-архив и записывает информацию в манифест
	addFile := func(key, absPath string) error {
		absPath = filepath.Clean(absPath)
		// Пропускает файлы, находящиеся внутри директории бэкапов
		if isPathWithin(absPath, backupDir) || samePath(absPath, backupDir) {
			return nil
		}
		info, err := os.Lstat(absPath)
		if err != nil {
			return nil // Игнорирует, если файл не существует
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// Игнорирует символические ссылки для сохранения переносимости
			return nil
		}
		if info.IsDir() {
			// Ожидает, что директории обрабатываются рекурсивно
			return nil
		}

		// Использует дедупликацию по абсолютному пути
		if z, ok := seenFiles[absPath]; ok {
			mf.Entries = append(mf.Entries, manifestBackupEntry{
				Key:      key,
				DestPath: absPath,
				ZipPath:  z,
			})
			return nil
		}

		// Генерирует детерминированное имя файла внутри ZIP на основе хеша абсолютного пути
		ext := strings.ToLower(filepath.Ext(absPath))
		h := sha256.Sum256([]byte(normalizeCaseForOS(absPath)))
		name := hex.EncodeToString(h[:])
		zipRel := filepath.ToSlash(filepath.Join("payload", "f", name+ext))

		// Гарантирует уникальность zipRel на случай маловероятных коллизий
		for {
			if _, busy := usedZip[zipRel]; !busy {
				break
			}
			zipRel = filepath.ToSlash(filepath.Join("payload", "f", name+"_"+randSuffix()+ext))
		}
		usedZip[zipRel] = struct{}{}

		if err := addFileToZip(zw, absPath, zipRel, info.ModTime()); err != nil {
			return err
		}

		seenFiles[absPath] = zipRel
		mf.Entries = append(mf.Entries, manifestBackupEntry{
			Key:      key,
			DestPath: absPath,
			ZipPath:  zipRel,
		})
		return nil
	}

	// Добавляет исполняемый файл FiReMQ
	exe := filepath.Join(dir, exeName)
	_ = addFile("__exe__", exe)

	// Добавляет конфигурационный файл server.conf
	if pathsOS.ServerConfPath != "" {
		_ = addFile("__server_conf__", pathsOS.ServerConfPath)
	}

	// Определяет список всех путей из server.conf, которые должны быть заархивированы
	type entry struct {
		Key   string
		Path  string
		IsDir bool // Указывает, ожидается ли, что это директория
	}
	cfgEntries := []entry{
		// БД, правила/конфиги Coraza/CRS
		{"Path_DB", pathsOS.Path_DB, true},
		{"Path_Config_Coraza", pathsOS.Path_Config_Coraza, false},
		{"Path_Folder_Rules_OWASP_CRS", pathsOS.Path_Folder_Rules_OWASP_CRS, true},
		{"Path_Folder_tmp_OWASP_CRS", pathsOS.Path_Folder_tmp_OWASP_CRS, true},
		{"Path_Config_Base", pathsOS.Path_Config_Base, true},
		{"Path_Rules_Base", pathsOS.Path_Rules_Base, true},
		{"Path_Setup_OWASP_CRS", pathsOS.Path_Setup_OWASP_CRS, false},

		// Утилиты/прочее
		{"Path_7zip", pathsOS.Path_7zip, false},
		{"Path_Info", pathsOS.Path_Info, true},

		// WEB
		{"Path_Web_Cert", pathsOS.Path_Web_Cert, false},
		{"Path_Web_Key", pathsOS.Path_Web_Key, false},

		// MQTT
		{"Path_Config_MQTT", pathsOS.Path_Config_MQTT, false},
		{"Path_Server_MQTT_CA", pathsOS.Path_Server_MQTT_CA, false},
		{"Path_Server_MQTT_Cert", pathsOS.Path_Server_MQTT_Cert, false},
		{"Path_Server_MQTT_Key", pathsOS.Path_Server_MQTT_Key, false},
		{"Path_Client_MQTT_CA", pathsOS.Path_Client_MQTT_CA, false},
		{"Path_Client_MQTT_Cert", pathsOS.Path_Client_MQTT_Cert, false},
		{"Path_Client_MQTT_Key", pathsOS.Path_Client_MQTT_Key, false},

		// QUIC
		{"Path_QUIC_Downloads", pathsOS.Path_QUIC_Downloads, true},
		{"Path_Client_QUIC_CA", pathsOS.Path_Client_QUIC_CA, false},
		{"Path_Server_QUIC_Cert", pathsOS.Path_Server_QUIC_Cert, false},
		{"Path_Server_QUIC_Key", pathsOS.Path_Server_QUIC_Key, false},

		// Ключ для куки
		{"Key_ChaCha20_Poly1305", pathsOS.Key_ChaCha20_Poly1305, false},
	}

	// Обходит все пути, указанные в конфигурации
	for _, e := range cfgEntries {
		// Проверяет, не исключен ли путь из бэкапа
		if slices.Contains(ExcludedBackupKeys, e.Key) {
			continue
		}

		p := strings.TrimSpace(e.Path)
		if p == "" {
			continue
		}
		abs := p
		// Преобразует относительные пути в абсолютные
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(dir, abs)
		}
		abs = filepath.Clean(abs)

		info, err := os.Lstat(abs)
		if err != nil {
			continue // Игнорирует отсутствующие пути
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue // Игнорирует символические ссылки
		}

		if info.IsDir() || e.IsDir {
			// Рекурсивно добавляет только файлы внутри директории
			filepath.WalkDir(abs, func(path string, d os.DirEntry, werr error) error {
				if werr != nil {
					return nil // Продолжает обход, если возникла ошибка чтения
				}
				// Пропускает директорию бэкапов, чтобы избежать бесконечного цикла
				if d.IsDir() && (samePath(path, backupDir) || isPathWithin(path, backupDir)) {
					return filepath.SkipDir
				}
				if d.Type()&os.ModeSymlink != 0 {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if d.IsDir() {
					return nil
				}
				_ = addFile(e.Key, path)
				return nil
			})
		} else {
			_ = addFile(e.Key, abs)
		}
	}

	// Сохраняет манифест в ZIP-архиве
	if err := addManifestBackupToZip(zw, &mf); err != nil {
		return "", fmt.Errorf("не удалось добавить manifestBackup.json в бэкап: %w", err)
	}

	// Явно закрывает ZIP-писатель и файл, чтобы проверить ошибки закрытия
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("ошибка закрытия ZIP: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("ошибка закрытия файла бэкапа: %w", err)
	}

	// Удаляет старые бэкапы, оставляя только самый свежий
	removeOldBackups(backupDir, filepath.Base(backupPath))

	logging.LogUpdate("Обновление FiReMQ: Полный бэкап создан: %s", backupPath)
	return backupPath, nil
}

// addFileToZip добавляет один файл в ZIP-архив с указанным именем и временем модификации
func addFileToZip(zw *zip.Writer, srcPath, zipRel string, modTime time.Time) error {
	zp := filepath.ToSlash(zipRel)
	hdr, err := zip.FileInfoHeader(mustStat(srcPath))
	if err != nil {
		return err
	}
	hdr.Name = zp
	hdr.Method = zip.Deflate
	hdr.Modified = modTime // Сохраняет оригинальное время модификации

	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	return err
}

// addManifestBackupToZip записывает структуру manifestBackup в виде JSON в ZIP-архив
func addManifestBackupToZip(zw *zip.Writer, mf *manifestBackup) error {
	data, err := json.MarshalIndent(mf, "", " ")
	if err != nil {
		return err
	}
	w, err := zw.Create("manifestBackup.json")
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// removeOldBackups удаляет все ZIP-архивы бэкапов в указанной директории, кроме самого свежего
func removeOldBackups(dir string, keepName string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Идентифицирует старые бэкапы по имени, исключая файл, который нужно сохранить
		if strings.HasPrefix(name, "bak_") && strings.HasSuffix(name, "_FiReMQ.zip") && name != keepName {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// isPathWithin проверяет, находится ли дочерний путь внутри базового пути (включая равенство)
func isPathWithin(child, base string) bool {
	c := filepath.Clean(child)
	b := filepath.Clean(base)
	if equalFoldOS(c, b) {
		return true
	}
	rel, err := filepath.Rel(b, c)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	// Убеждается, что относительный путь не начинается с ".."
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

// samePath проверяет, указывают ли два пути на одно и то же место на файловой системе
func samePath(a, b string) bool {
	return equalFoldOS(filepath.Clean(a), filepath.Clean(b))
}

// equalFoldOS сравнивает пути с учетом специфики операционной системы (без учета регистра на Windows)
func equalFoldOS(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// normalizeCaseForOS возвращает путь в нижнем регистре для Windows, сохраняя оригинальный регистр для других ОС
func normalizeCaseForOS(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(s)
	}
	return s
}

// mustStat возвращает os.FileInfo для пути, игнорируя ошибки
func mustStat(p string) os.FileInfo {
	info, _ := os.Stat(p)
	return info
}

// randSuffix генерирует небольшой случайный суффикс, основанный на текущем времени, для уникализации путей в ZIP-архиве
func randSuffix() string {
	// Использует время в наносекундах для быстрой и простой уникализации
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
