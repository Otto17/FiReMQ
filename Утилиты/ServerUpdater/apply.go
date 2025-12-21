// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

//go:build linux

package main

import (
	"archive/tar"
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
	SrcInZip  string // Путь внутри архива FiReMQ/
	DestAbs   string // Абсолютный путь назначения
	ConfKey   string // Ключ конфигурации (если применимо)
	SkipApply bool   // Указывает, что эту операцию следует пропустить
}

// ApplyStats содержит сводку по выполненным операциям обновления
type ApplyStats struct {
	Updated       int // Кол-во обновлённых/созданных файлов
	Deleted       int // Кол-во удалённых файлов
	SkippedDelete int // Кол-во пропущенных удалений (файл уже отсутствовал)
}

// Archive является оберткой для доступа к tar.gz архиву
type Archive struct {
	Path string // Путь к .tar.gz
}

// OpenArchive открывает .tar.gz архив по указанному пути
func OpenArchive(p string) (*Archive, error) {
	low := strings.ToLower(p)
	if strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".tgz") {
		if _, err := os.Stat(p); err != nil {
			return nil, err
		}
		return &Archive{Path: p}, nil
	}
	return nil, fmt.Errorf("неподдерживаемый формат архива: %s (ожидался .tar.gz)", p)
}

// --- Чтение update.toml из архива ---

// parseManifestFromArchive извлекает и парсит update.toml из tar.gz архива
func parseManifestFromArchive(a *Archive) (*Manifest, error) {
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
			// Читает содержимое прямо из потока
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			return parseManifest(bytes.NewReader(data))
		}
	}
	return nil, fmt.Errorf("в архиве нет update.toml (ожидался файл в корне архива рядом с папкой FiReMQ)")
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
		// Убрана блокировка обновления самого апдейтера, теперь это обрабатывается в applyPlan

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
			// Если путь указывает на директорию, добавляем относительный путь из Src
			if info, err := os.Stat(dest); err == nil && info.IsDir() {
				parts := strings.SplitN(filepath.ToSlash(it.Src), "/", 2)
				if len(parts) == 2 {
					dest = filepath.Join(dest, filepath.FromSlash(parts[1]))
				} else {
					dest = filepath.Join(dest, filepath.Base(it.Src))
				}
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

// extractFromArchiveToTemp извлекает файл из .tar.gz архива по пути внутри директории FiReMQ/<srcInZip> во временный файл
func extractFromArchiveToTemp(a *Archive, srcInZip, tempDest string) (os.FileMode, error) {
	srcInZip = strings.ReplaceAll(srcInZip, "\\", "/")
	srcInZip = path.Clean(strings.TrimPrefix(srcInZip, "/"))
	// Ищет файл внутри подкаталога /firemq/ (без учета регистра)
	wantSuffix := "firemq/" + strings.ToLower(srcInZip)

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
			// Создаёт директорию с правильными правами доступа
			if err := ensureDirAllAndSetOwner(filepath.Dir(tempDest), 0755); err != nil {
				return 0, err
			}

			mode := hdr.FileInfo().Mode()
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
}

// atomicReplace атомарно заменяет файл назначения dest временным файлом tempFile
func atomicReplace(dest, tempFile string) error {
	_ = os.Remove(dest)
	return os.Rename(tempFile, dest)
}

// applyPlan выполняет список операций PlanOp и возвращает статистику
func applyPlan(a *Archive, ops []PlanOp) (ApplyStats, error) {
	myExePath, _ := os.Executable()
	stats := ApplyStats{}

	for _, op := range ops {
		if op.SkipApply {
			log.Printf("ПРОПУСК: %s %s -> %s", ruAction(op.Action), op.SrcInZip, op.DestAbs)
			continue
		}

		// Проверка на обновление самого апдейтера
		isSelfUpdate := false
		if myExePath != "" && filepath.Clean(op.DestAbs) == filepath.Clean(myExePath) {
			isSelfUpdate = true
		}

		switch op.Action {
		case ActDelete:
			if isSelfUpdate {
				log.Printf("ПРОПУСК удаления апдейтера: %s", op.DestAbs)
				continue
			}

			if err := os.Remove(op.DestAbs); err != nil {
				if os.IsNotExist(err) {
					stats.SkippedDelete++
					log.Printf("ПРОПУСК УДАЛЕНИЯ: %s (файл уже отсутствует)", op.DestAbs)
					continue
				}
				return stats, fmt.Errorf("delete %s: %w", op.DestAbs, err)
			}

			stats.Deleted++
			log.Printf("УДАЛЕНИЕ: %s", op.DestAbs)

		case ActUpdate:
			temp := op.DestAbs + ".tmp"

			mode, err := extractFromArchiveToTemp(a, op.SrcInZip, temp)
			if err != nil {
				return stats, err
			}

			// Создаёт родительскую директорию с правильными правами, если она не существует
			if err := ensureDirAllAndSetOwner(filepath.Dir(op.DestAbs), 0755); err != nil {
				_ = os.Remove(temp)
				return stats, err
			}

			if err := atomicReplace(op.DestAbs, temp); err != nil {
				_ = os.Remove(temp)
				return stats, fmt.Errorf("replace %s: %w", op.DestAbs, err)
			}

			// Нормализация прав Linux
			ext := strings.ToLower(filepath.Ext(op.DestAbs))

			// Исполняемые файлы (без расширения или .sh)
			if ext == "" || ext == ".sh" || ext == ".bin" {
				mode = 0755 // Права на исполняемые файлы (FiReMQ, ServerUpdater)
			} else {
				mode = 0644 // Права на обычные файлы (index.html, modal.js, конфиги .json, .conf, .pem и т.д.)
			}

			// Установка владельца и прав доступа на финальный файл
			setOwnerAndPerms(op.DestAbs, mode)

			if isSelfUpdate {
				log.Printf("САМООБНОВЛЕНИЕ: %s успешно заменён.", op.DestAbs)
			} else {
				log.Printf("ОБНОВЛЕНИЕ: %s (права=%o)", op.DestAbs, mode)
			}

			stats.Updated++
		}
	}

	return stats, nil
}
