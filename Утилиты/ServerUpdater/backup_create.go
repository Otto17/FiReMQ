// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

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
	"strings"
	"time"
)

// excludedBackupKeys перечисляет ключи, исключаемые из бэкапа
var excludedBackupKeys = map[string]struct{}{
	"Path_Backup": {},
	"Path_Info":   {},
}

// CreateFullBackup создает ZIP-бэкап на основе server.conf и возвращает путь к ZIP-файлу
func CreateFullBackup(exeDir, currentVersion string, conf map[string]string, serverConfPath string) (string, error) {
	backupDir := resolveBackupDir(exeDir)

	// Создает директорию бэкапов, обеспечивая правильного владельца/права доступа
	if err := ensureDirAllAndSetOwner(backupDir, 0755); err != nil {
		return "", fmt.Errorf("не удалось создать директорию бэкапов %q: %w", backupDir, err)
	}

	backupName := fmt.Sprintf("bak_%s_ver=%s_FiReMQ.zip", time.Now().Format(backupTimestampLayout), currentVersion)
	backupPath := filepath.Join(backupDir, backupName)

	// Использует 0644, поскольку это стандартные безопасные права доступа для файлов
	out, err := os.OpenFile(backupPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fmt.Errorf("не удалось создать файл бэкапа %q: %w", backupPath, err)
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	defer zw.Close()

	man := ManifestBackup{
		CreatedAt:      time.Now().Format(time.RFC3339),
		Version:        currentVersion,
		ExeDir:         exeDir,
		PathBackup:     backupDir,
		ServerConfPath: serverConfPath,
	}

	seenFiles := make(map[string]string) // abs -> zipRel
	usedZip := make(map[string]struct{})

	addFile := func(key, absPath string) error {
		absPath = filepath.Clean(absPath)

		// Пропускает, если путь указывает на саму директорию бэкапов
		if isPathWithin(absPath, backupDir) || samePath(absPath, backupDir) {
			return nil
		}
		info, err := os.Lstat(absPath)
		if err != nil {
			return nil // нет файла — пропустим
		}
		// Пропускает символические ссылки и джанкшены
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		if z, ok := seenFiles[absPath]; ok {
			// Использует ранее добавленный путь в ZIP-архиве для избежания дублирования файлов
			man.Entries = append(man.Entries, ManifestBackupItem{
				Key:      key,
				DestPath: absPath,
				ZipPath:  z,
			})
			return nil
		}

		ext := strings.ToLower(filepath.Ext(absPath))
		h := sha256.Sum256([]byte(normalizeCaseForOS(absPath)))
		name := hex.EncodeToString(h[:])
		zipRel := filepath.ToSlash(filepath.Join("payload", "f", name+ext))

		// Проверяет уникальность сгенерированного имени файла в ZIP-архиве
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
		man.Entries = append(man.Entries, ManifestBackupItem{
			Key:      key,
			DestPath: absPath,
			ZipPath:  zipRel,
		})
		return nil
	}

	// Добавляет исполняемый файл приложения
	_ = addFile("__exe__", filepath.Join(exeDir, exeName()))

	// Добавляет файл конфигурации сервера
	if strings.TrimSpace(serverConfPath) != "" {
		_ = addFile("__server_conf__", serverConfPath)
	}

	// Добавляет все файлы и папки, указанные в server.conf
	// Определяет структуру для получения и обработки путей из конфигурации
	type entry struct {
		Key   string
		Path  string
		IsDir bool
	}
	get := func(k string) string {
		return strings.TrimSpace(conf[k])
	}
	cfgEntries := []entry{
		{"Path_DB", get("Path_DB"), true},
		{"Path_Config_Coraza", get("Path_Config_Coraza"), false},

		{"Path_Folder_Rules_OWASP_CRS", get("Path_Folder_Rules_OWASP_CRS"), true},
		{"Path_Folder_tmp_OWASP_CRS", get("Path_Folder_tmp_OWASP_CRS"), true},
		{"Path_Config_Base", get("Path_Config_Base"), true},
		{"Path_Rules_Base", get("Path_Rules_Base"), true},
		{"Path_Setup_OWASP_CRS", get("Path_Setup_OWASP_CRS"), false},

		{"Path_7zip", get("Path_7zip"), false},
		{"Path_Info", get("Path_Info"), true},

		{"Path_Web_Cert", get("Path_Web_Cert"), false},
		{"Path_Web_Key", get("Path_Web_Key"), false},

		{"Path_Config_MQTT", get("Path_Config_MQTT"), false},
		{"Path_Server_MQTT_CA", get("Path_Server_MQTT_CA"), false},
		{"Path_Server_MQTT_Cert", get("Path_Server_MQTT_Cert"), false},
		{"Path_Server_MQTT_Key", get("Path_Server_MQTT_Key"), false},
		{"Path_Client_MQTT_CA", get("Path_Client_MQTT_CA"), false},
		{"Path_Client_MQTT_Cert", get("Path_Client_MQTT_Cert"), false},
		{"Path_Client_MQTT_Key", get("Path_Client_MQTT_Key"), false},

		{"Path_QUIC_Downloads", get("Path_QUIC_Downloads"), true},
		{"Path_Client_QUIC_CA", get("Path_Client_QUIC_CA"), false},
		{"Path_Server_QUIC_Cert", get("Path_Server_QUIC_Cert"), false},
		{"Path_Server_QUIC_Key", get("Path_Server_QUIC_Key"), false},

		{"Key_ChaCha20_Poly1305", get("Key_ChaCha20_Poly1305"), false},
	}

	for _, e := range cfgEntries {
		if _, skip := excludedBackupKeys[e.Key]; skip {
			continue
		}
		p := strings.TrimSpace(e.Path)
		if p == "" {
			continue
		}
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(exeDir, abs)
		}
		abs = filepath.Clean(abs)

		info, err := os.Lstat(abs)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		if info.IsDir() || e.IsDir {
			filepath.WalkDir(abs, func(path string, d os.DirEntry, werr error) error {
				if werr != nil {
					return nil
				}
				// Проверяет, что не включает сам каталог бэкапа или его подкаталоги
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

	// Добавляет файл манифеста в ZIP-архив
	if err := addManifestBackupToZip(zw, &man); err != nil {
		return "", fmt.Errorf("не удалось добавить manifestBackup.json: %w", err)
	}

	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("ошибка закрытия ZIP: %w", err)
	}

	if err := out.Close(); err != nil {
		return "", fmt.Errorf("ошибка закрытия файла бэкапа: %w", err)
	}

	// Устанавливает владельца и права доступа для созданного файла бэкапа
	setOwnerAndPerms(backupPath, 0644)

	// Удаляет старые бэкапы, оставляя только текущий свежий архив
	removeOldBackups(backupDir, filepath.Base(backupPath))

	return backupPath, nil
}

// addFileToZip добавляет файл, расположенный по srcPath, в ZIP-архив с указанным относительным именем zipRel
func addFileToZip(zw *zip.Writer, srcPath, zipRel string, modTime time.Time) error {
	zp := filepath.ToSlash(zipRel)
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = zp
	hdr.SetMode(info.Mode()) // Сохраняет Unix права доступа
	hdr.Method = zip.Deflate
	hdr.Modified = modTime

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

// addManifestBackupToZip маршалирует структуру ManifestBackup в JSON и добавляет ее в ZIP как manifestBackup.json
func addManifestBackupToZip(zw *zip.Writer, mf *ManifestBackup) error {
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

// removeOldBackups удаляет все файлы бэкапов в указанной директории, кроме keepName
func removeOldBackups(dir, keepName string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Удаляет только те файлы, которые соответствуют шаблону именования бэкапов и не являются текущим файлом
		if strings.HasPrefix(name, backupPatternPrefix) && strings.HasSuffix(name, backupPatternSuffix) && name != keepName {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// isPathWithin проверяет, находится ли дочерний путь child внутри базового пути base
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
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

// samePath проверяет, указывают ли два пути A и B на одну и ту же локацию файловой системы, игнорируя регистр на Windows
func samePath(a, b string) bool {
	return equalFoldOS(filepath.Clean(a), filepath.Clean(b))
}

// equalFoldOS сравнивает две строки, используя сравнение без учета регистра на Windows
func equalFoldOS(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// normalizeCaseForOS приводит строку к нижнему регистру только на Windows
func normalizeCaseForOS(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(s)
	}
	return s
}

// randSuffix генерирует суффикс, основанный на текущем Unix Nano времени
func randSuffix() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
