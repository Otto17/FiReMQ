// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// PlanOp представляет одну операцию, которую необходимо выполнить во время обновления
type PlanOp struct {
	Section   string // "files" | "config"
	Action    Action // update | delete
	SrcInZip  string // путь внутри архива FiReMQ/
	DestAbs   string // абсолютный путь назначения
	ConfKey   string // ключ конфигурации (если применимо)
	SkipApply bool   // указывает, что эту операцию следует пропустить, например, при защите от удаления апдейтера
}

// BackupEntry описывает одну запись в манифесте бэкапа
type BackupEntry struct {
	Action  Action `json:"action"`   // что было сделано
	DestAbs string `json:"dest_abs"` // абсолютный путь назначения после операции
	Existed bool   `json:"existed"`  // существовал ли файл до операции
}

// BackupManifest описывает общую информацию о созданном бэкапе
type BackupManifest struct {
	CreatedAt   string        `json:"created_at"`
	FromVersion string        `json:"from_version"`
	ToVersion   string        `json:"to_version"`
	Entries     []BackupEntry `json:"entries"`
	Note        string        `json:"note,omitempty"`
}

// --- Универсальная обёртка над архивом (.zip | .tar.gz) ---

// ArchiveKind определяет тип архива
type ArchiveKind int

const (
	KindZIP ArchiveKind = iota
	KindTARGZ
)

// Archive является оберткой для доступа к ZIP или TAR.GZ архиву
type Archive struct {
	Kind ArchiveKind
	Zip  *zip.ReadCloser
	Path string // путь к .tar.gz или .zip
}

// OpenArchive открывает архив по указанному пути, определяя его тип
func OpenArchive(p string) (*Archive, error) {
	low := strings.ToLower(p)
	if strings.HasSuffix(low, ".zip") {
		zr, err := zip.OpenReader(p)
		if err != nil {
			return nil, err
		}
		return &Archive{Kind: KindZIP, Zip: zr, Path: p}, nil
	}
	if strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".tgz") {
		// Для tar.gz открывает файл на чтение по мере необходимости
		if _, err := os.Stat(p); err != nil {
			return nil, err
		}
		return &Archive{Kind: KindTARGZ, Path: p}, nil
	}
	return nil, fmt.Errorf("неподдерживаемый формат архива: %s (ожидался .zip или .tar.gz)", p)
}

// Close закрывает открытый ZIP-файл, если он существует
func (a *Archive) Close() error {
	if a.Kind == KindZIP && a.Zip != nil {
		return a.Zip.Close()
	}
	return nil
}

// --- Чтение update.toml из архива ---

// parseManifestFromArchive извлекает и парсит update.toml из архива
func parseManifestFromArchive(a *Archive) (*Manifest, error) {
	switch a.Kind {
	case KindZIP:
		var mf *zip.File
		for _, f := range a.Zip.File {
			n := strings.ReplaceAll(f.Name, "\\", "/")
			n = path.Clean(strings.TrimPrefix(n, "./"))
			if strings.EqualFold(path.Base(n), "update.toml") {
				mf = f
				break
			}
		}
		if mf == nil {
			return nil, fmt.Errorf("в архиве нет update.toml (ожидался файл в корне архива рядом с папкой FiReMQ)")
		}
		rc, err := mf.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return parseManifest(rc)

	case KindTARGZ:
		f, err := os.Open(a.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		gr, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		tr := tar.NewReader(gr)

		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			n := strings.ReplaceAll(hdr.Name, "\\", "/")
			n = path.Clean(strings.TrimPrefix(n, "./"))
			if strings.EqualFold(path.Base(n), "update.toml") {
				// Читает содержимое прямо из потока, чтобы избежать повторного открытия
				data, err := io.ReadAll(tr)
				if err != nil {
					return nil, err
				}
				return parseManifest(bytes.NewReader(data))
			}
		}
		return nil, fmt.Errorf("в архиве нет update.toml (ожидался файл в корне архива рядом с папкой FiReMQ)")

	default:
		return nil, fmt.Errorf("неизвестный тип архива")
	}
}

// buildPlan строит список операций PlanOp на основе манифеста обновления
func buildPlan(man *Manifest, exeDir string, conf map[string]string, serverConfPath string) ([]PlanOp, bool, error) {
	configDir := filepath.Dir(serverConfPath)
	var ops []PlanOp
	needReplaceExe := false

	// files
	for _, it := range man.Files {
		var dest string
		if it.Dest != "" {
			dest = expandMacros(it.Dest, exeDir, configDir)
		} else {
			// Относительный путь, если Dest не указан, считается от каталога "EXE_DIR"
			dest = filepath.Join(exeDir, filepath.FromSlash(it.DestRel))
		}
		dest = filepath.Clean(dest)

		op := PlanOp{
			Section:  "files",
			Action:   it.Action,
			SrcInZip: strings.TrimPrefix(path.Clean(it.Src), "/"),
			DestAbs:  dest,
		}

		// Определяет, нужно ли заменять главный бинарный файл FiReMQ
		if strings.EqualFold(filepath.Clean(dest), filepath.Join(exeDir, exeName())) {
			if it.Action == ActUpdate {
				needReplaceExe = true
			}
		}

		// Обеспечивает защиту от самоуничтожения апдейтера
		if strings.EqualFold(filepath.Clean(dest), filepath.Join(exeDir, updaterName())) {
			log.Printf("Пропуск операции над %s (action=%s) — файл занят апдейтером", updaterName(), it.Action)
			op.SkipApply = true
		}

		ops = append(ops, op)
	}

	// config
	for _, it := range man.Configs {
		var dest string
		// Проверяет, существует ли путь в текущем "server.conf"
		if v, ok := conf[it.Key]; ok && strings.TrimSpace(v) != "" {
			dest = v
			if !filepath.IsAbs(dest) {
				dest = filepath.Join(exeDir, dest)
			}
			// Если путь не найден в server.conf, использует "DestDefault" из манифеста
		} else if it.DestDefault != "" {
			dRaw := it.DestDefault
			d := expandMacros(dRaw, exeDir, configDir)

			if !filepath.IsAbs(d) {
				// Если путь начинается с "config/", он должен быть относительно "CONFIG_DIR"
				norm := strings.TrimPrefix(strings.ReplaceAll(dRaw, "\\", "/"), "./")
				if after, ok0 := strings.CutPrefix(norm, "config/"); ok0 {
					d = filepath.Join(configDir, filepath.FromSlash(after))
				} else {
					// Иначе, путь считается относительным папке FiReMQ (exeDir)
					d = filepath.Join(exeDir, filepath.FromSlash(dRaw))
				}
			}
			dest = filepath.Clean(d)
		} else {
			// Для операции ActDelete нет необходимости продолжать, если ключ не найден
			if it.Action == ActDelete {
				log.Printf("config[%s]: ключ не найден и dest_default не задан — удалять нечего, пропуск", it.Key)
				continue
			}
			return nil, needReplaceExe, fmt.Errorf("config[%s]: не удалось определить путь назначения", it.Key)
		}

		op := PlanOp{
			Section:  "config",
			Action:   it.Action,
			SrcInZip: strings.TrimPrefix(path.Clean(it.Src), "/"),
			DestAbs:  filepath.Clean(dest),
			ConfKey:  it.Key,
		}

		// Защита от попыток трогать сам ServerUpdater
		if strings.EqualFold(op.DestAbs, filepath.Join(exeDir, updaterName())) {
			log.Printf("Пропуск операции над %s из config[%s]", updaterName(), it.Key)
			op.SkipApply = true
		}

		ops = append(ops, op)
	}

	return ops, needReplaceExe, nil
}

// dumpPlan выводит план операций в лог
func dumpPlan(ops []PlanOp) {
	for _, op := range ops {
		src := op.SrcInZip
		sec := ruSection(op.Section)
		act := ruAction(op.Action)
		if op.Action == ActDelete {
			log.Printf("ПЛАН: [%s] %s -> %s", sec, act, op.DestAbs)
		} else {
			log.Printf("ПЛАН: [%s] %s %s -> %s", sec, act, src, op.DestAbs)
		}
	}
}

// ruAction возвращает Русское описание действия (update/delete)
func ruAction(a Action) string {
	switch a {
	case ActUpdate:
		return "обновить"
	case ActDelete:
		return "удалить"
	default:
		return string(a)
	}
}

// ruSection возвращает Русское описание раздела (files/config)
func ruSection(s string) string {
	switch strings.ToLower(s) {
	case "files":
		return "файлы"
	case "config":
		return "конфиг"
	default:
		return s
	}
}

// extractFromArchiveToTemp извлекает файл из архива (ZIP/TAR.GZ) по пути внутри папки FiReMQ/<srcInZip> во временный файл
func extractFromArchiveToTemp(a *Archive, srcInZip, tempDest string) (os.FileMode, error) {
	srcInZip = strings.ReplaceAll(srcInZip, "\\", "/")
	srcInZip = path.Clean(strings.TrimPrefix(srcInZip, "/"))
	// Ищет файл внутри подпапки /firemq/ (без учета регистра)
	wantSuffix := "firemq/" + strings.ToLower(srcInZip)

	switch a.Kind {
	case KindZIP:
		for _, f := range a.Zip.File {
			n := strings.ReplaceAll(f.Name, "\\", "/")
			n = strings.TrimPrefix(n, "./")
			ln := strings.ToLower(n)
			if strings.HasSuffix(ln, wantSuffix) || strings.HasSuffix(ln, "/"+wantSuffix) {
				rc, err := f.Open()
				if err != nil {
					return 0, err
				}
				defer rc.Close()

				// Использует отдельную функцию для создания директории с правильными правами доступа
				if err := ensureDirAllAndSetOwner(filepath.Dir(tempDest), 0755); err != nil {
					return 0, err
				}

				mode := f.FileInfo().Mode()
				out, err := os.OpenFile(tempDest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
				if err != nil {
					return 0, err
				}

				if _, err := io.Copy(out, rc); err != nil {
					out.Close()
					_ = os.Remove(tempDest)
					return 0, err
				}
				if err := out.Close(); err != nil {
					_ = os.Remove(tempDest)
					return 0, err
				}

				return mode, nil
			}
		}
		return 0, fmt.Errorf("в архиве не найден файл: .../FiReMQ/%s", srcInZip)

	case KindTARGZ:
		f, err := os.Open(a.Path)
		if err != nil {
			return 0, err
		}
		defer f.Close()

		gr, err := gzip.NewReader(f)
		if err != nil {
			return 0, err
		}
		defer gr.Close()

		tr := tar.NewReader(gr)

		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return 0, err
			}
			// Ищет нужный файл
			n := strings.ReplaceAll(hdr.Name, "\\", "/")
			n = strings.TrimPrefix(n, "./")
			ln := strings.ToLower(n)
			if strings.HasSuffix(ln, wantSuffix) || strings.HasSuffix(ln, "/"+wantSuffix) {
				// Использует отдельную функцию для создания директории с правильными правами доступа
				if err := ensureDirAllAndSetOwner(filepath.Dir(tempDest), 0755); err != nil {
					return 0, err
				}

				mode := os.FileMode(hdr.Mode)
				out, err := os.OpenFile(tempDest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
				if err != nil {
					return 0, err
				}

				if _, err := io.Copy(out, tr); err != nil {
					out.Close()
					_ = os.Remove(tempDest)
					return 0, err
				}
				if err := out.Close(); err != nil {
					_ = os.Remove(tempDest)
					return 0, err
				}
				return mode, nil
			}
		}
		return 0, fmt.Errorf("в архиве не найден файл: .../FiReMQ/%s", srcInZip)

	default:
		return 0, fmt.Errorf("неизвестный тип архива")
	}
}

// atomicReplace атомарно заменяет файл назначения dest временным файлом tempFile
func atomicReplace(dest, tempFile string) error {
	_ = os.Remove(dest)
	return os.Rename(tempFile, dest)
}

// applyPlan выполняет список операций PlanOp
func applyPlan(a *Archive, ops []PlanOp) error {
	for _, op := range ops {
		if op.SkipApply {
			log.Printf("ПРОПУСК: %s %s -> %s", ruAction(op.Action), op.SrcInZip, op.DestAbs)
			continue
		}
		switch op.Action {
		case ActDelete:
			// Игнорирует ошибку, если файл уже не существует
			if err := os.Remove(op.DestAbs); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete %s: %w", op.DestAbs, err)
			}
			log.Printf("УДАЛЕНИЕ: %s", op.DestAbs)

		case ActUpdate:
			temp := op.DestAbs + ".tmp"

			// Извлекает файл из архива во временную директорию
			mode, err := extractFromArchiveToTemp(a, op.SrcInZip, temp)
			if err != nil {
				return err
			}

			// Создает родительскую директорию с правильными правами, если она не существует
			if err := ensureDirAllAndSetOwner(filepath.Dir(op.DestAbs), 0755); err != nil {
				_ = os.Remove(temp)
				return err
			}

			if err := atomicReplace(op.DestAbs, temp); err != nil {
				_ = os.Remove(temp)
				return fmt.Errorf("replace %s: %w", op.DestAbs, err)
			}

			// Устанавливает владельца и права доступа на финальный файл
			setOwnerAndPerms(op.DestAbs, mode)

			log.Printf("ОБНОВЛЕНИЕ: %s (права=%o)", op.DestAbs, mode)
		}
	}
	return nil
}
