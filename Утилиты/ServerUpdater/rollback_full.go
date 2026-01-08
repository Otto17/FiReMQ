// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

//go:build linux

package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ManifestBackup описывает структуру файла манифеста бэкапа (manifestBackup.json)
type ManifestBackup struct {
	CreatedAt      string               `json:"CreatedAt"`
	Version        string               `json:"Version"`
	ExeDir         string               `json:"ExeDir"`
	PathBackup     string               `json:"PathBackup"`
	ServerConfPath string               `json:"ServerConfPath"`
	Entries        []ManifestBackupItem `json:"Entries"`
}

// ManifestBackupItem описывает запись о файле, включенном в бэкап
type ManifestBackupItem struct {
	Key      string `json:"Key"`      // имя ключа server.conf, или "__exe__"/"__server_conf__"
	DestPath string `json:"DestPath"` // абсолютный путь назначения
	ZipPath  string `json:"ZipPath"`  // путь файла внутри zip (payload/f/<hash><ext>)
}

// trimQuotes удаляет обрамляющие кавычки (", ') из строки
func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// resolveBackupDir определяет абсолютный путь к директории бэкапов
func resolveBackupDir(exeDir string) string {
	_, conf, _ := loadServerConfMap(exeDir)
	bdir := strings.TrimSpace(conf["Path_Backup"])
	if bdir == "" {
		bdir = "Backup"
	}
	bdir = trimQuotes(bdir)
	if !filepath.IsAbs(bdir) {
		// Нормализует относительный путь относительно exeDir
		bdir = filepath.Clean(filepath.Join(exeDir, bdir))
	}
	return bdir
}

// findLatestBackupInDir ищет самый свежий бэкап в каталоге dir по шаблону имени
func findLatestBackupInDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	var latestPath string
	var latestTime time.Time

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, backupPatternPrefix) || !strings.HasSuffix(name, backupPatternSuffix) {
			continue
		}

		// Обрезает префиксы и суффиксы для извлечения временной метки
		trimmed := strings.TrimPrefix(name, backupPatternPrefix)
		trimmed = strings.TrimSuffix(trimmed, backupPatternSuffix)

		// Ожидает формат "ДД.ММ.ГГ(в_ЧЧ.ММ.СС)_ver=ДД.ММ.ГГ"
		parts := strings.SplitN(trimmed, "_ver=", 2)
		if len(parts) != 2 {
			continue
		}

		tsStr := parts[0] // "ДД.ММ.ГГ(в_ЧЧ.ММ.СС)"
		ts, err := time.Parse(backupTimestampLayout, tsStr)
		if err != nil {
			continue
		}

		full := filepath.Join(dir, name)

		// Обновляет самый свежий бэкап
		if latestPath == "" || ts.After(latestTime) {
			latestPath = full
			latestTime = ts
		}
	}
	return latestPath, nil
}

// findZipMember ищет файл в zip-архиве по пути (с нормализацией слешей)
func findZipMember(r *zip.Reader, name string) *zip.File {
	want := path.Clean(strings.TrimPrefix(name, "/"))
	for _, f := range r.File {
		if path.Clean(f.Name) == want {
			return f
		}
	}
	return nil
}

// restoreFromManifestBackup восстанавливает файлы из бэкапа согласно manifestBackup.json
func restoreFromManifestBackup(zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	var man ManifestBackup
	mf := findZipMember(&zr.Reader, "manifestBackup.json")
	if mf == nil {
		return fmt.Errorf("в бэкапе отсутствует manifestBackup.json")
	}
	rc, err := mf.Open()
	if err != nil {
		return err
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return fmt.Errorf("не удалось прочитать manifestBackup.json: %w", err)
	}
	if err := json.Unmarshal(data, &man); err != nil {
		return fmt.Errorf("не удалось разобрать manifestBackup.json: %w", err)
	}

	// Восстанавливает все файлы, перечисленные в Entries
	for _, e := range man.Entries {
		if strings.TrimSpace(e.DestPath) == "" || strings.TrimSpace(e.ZipPath) == "" {
			continue
		}

		zf := findZipMember(&zr.Reader, e.ZipPath)
		if zf == nil {
			// На всякий случай пытается найти с префиксом "FiReMQ/"
			zf = findZipMember(&zr.Reader, path.Join("FiReMQ", e.ZipPath))
		}
		if zf == nil {
			log.Printf("Предупреждение: в бэкапе не найден ZipPath=%s (для %s)", e.ZipPath, e.DestPath)
			continue
		}

		// Обеспечивает существование родительской директории с правильными правами
		if err := ensureDirAllAndSetOwner(filepath.Dir(e.DestPath), 0755); err != nil {
			return fmt.Errorf("mkdir для %s: %w", e.DestPath, err)
		}

		rc, err := zf.Open()
		if err != nil {
			return err
		}
		tmp := e.DestPath + ".tmp"
		out, err := os.Create(tmp)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeOutErr := out.Close()
		rc.Close()

		if copyErr != nil {
			_ = os.Remove(tmp)
			return copyErr
		}
		if closeOutErr != nil {
			_ = os.Remove(tmp)
			return closeOutErr
		}

		// Определение размера восстановленного файла
		var sizeStr string
		if info, err := os.Stat(tmp); err == nil {
			sizeStr = formatSize(info.Size())
		} else {
			sizeStr = "неизвестно"
		}

		// Атомарно заменяет файл назначения временным файлом
		_ = os.Remove(e.DestPath)
		if err := os.Rename(tmp, e.DestPath); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("rename %s: %w", e.DestPath, err)
		}

		// Устанавливает владельца и права доступа на восстановленный файл
		setOwnerAndPerms(e.DestPath, zf.Mode())

		log.Printf("ВОССТАНОВЛЕНИЕ: %s (размер=%s, права=%o)", e.DestPath, sizeStr, zf.Mode())
	}
	return nil
}

// RunRollback ожидает завершения работы FiReMQ, восстанавливает из самого свежего бэкапа и запускает FiReMQ
func RunRollback() error {
	exeDir, err := exeDir()
	if err != nil {
		return fmt.Errorf("не удалось определить директорию апдейтера: %w", err)
	}
	exeFull := filepath.Join(exeDir, exeName())

	// Ищет самый свежий бэкап в директории Path_Backup
	backupDir := resolveBackupDir(exeDir)
	if latestNew, _ := findLatestBackupInDir(backupDir); latestNew != "" {
		log.Printf("Откат (manifestBackup.json): %s", latestNew)
		if err := waitFiReMQExit(exeFull); err != nil {
			return err
		}

		if err := restoreFromManifestBackup(latestNew); err != nil {
			return fmt.Errorf("ошибка восстановления из бэкапа: %w", err)
		}

		return startFiReMQ(exeFull)
	}

	return fmt.Errorf("не найден ни один бэкап в %s", backupDir)
}
