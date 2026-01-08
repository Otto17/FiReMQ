// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	version = "08.01.26" // Текущая версия ServerUpdater в формате "дд.мм.гг"

	backupTimestampLayout = "02.01.06(в_15.04.05)" // Формат метки времени для резервной копии "дд.мм.гг(в_ЧЧ.ММ.СС)""
	backupPatternPrefix   = "bak_"                 // Префикс
	backupPatternSuffix   = "_FiReMQ.zip"          // Суффикс
	waitShutdownTimeout   = 30 * time.Second       // Максимальное время ожидания завершения FiReMQ
	waitShutdownInterval  = 500 * time.Millisecond // Интервал проверки завершения процесса
)

// UpdateChainManifest описывает цепочку обновлений (update_chain.json), формируемую FiReMQ
type UpdateChainManifest struct {
	CurrentVersion string            `json:"CurrentVersion"`
	Items          []UpdateChainItem `json:"Items"`
}

// UpdateChainItem описывает один релиз в цепочке
type UpdateChainItem struct {
	Version  string `json:"Version"`
	FileName string `json:"FileName"`
	Repo     string `json:"Repo"`
}

func main() {
	args := os.Args

	// Показывает версию утилиты ServerUpdater
	if len(args) >= 2 && strings.EqualFold(args[1], "--version") {
		fmt.Printf("Версия \"ServerUpdater\": %s\n", version)
		return
	}

	// Включает логирование
	log.SetFlags(log.LstdFlags)
	ServerUpdaterLogging()

	log.Printf("Аргументы: %v", args)

	// Откат
	if len(args) >= 2 && strings.EqualFold(args[1], "-rollback") {
		if err := RunRollback(); err != nil {
			log.Fatalf("Откат не выполнен: %v", err)
		}

		log.Println("Откат успешно завершён!")
		LogSpacer(1) // Один пустой абзац-разделитель
		return       // Завершает работу после успешного отката
	}

	// Обновление
	if len(args) >= 3 && strings.EqualFold(args[1], "-apply-zip") {
		archPath := strings.TrimSpace(args[2])
		curVer := ""

		if len(args) >= 4 {
			curVer = strings.TrimSpace(args[3]) // Читает опциональный аргумент текущей версии
		}

		pidStr := ""
		if len(args) >= 5 {
			pidStr = strings.TrimSpace(args[4]) // Читает опциональный аргумент PID процесса
		}

		log.Printf("Применение архива: %s (текущая версия=%s, pid=%s)", archPath, curVer, pidStr)
		if err := RunApplyFromZip(archPath, curVer, pidStr); err != nil {
			log.Fatalf("Ошибка применения архива: %v", err)
		}

		log.Println("Замена из локального архива успешно завершена!")
		LogSpacer(1) // Один пустой абзац-разделитель
		return       // Завершает работу после успешного обновления
	}

	log.Println("Использование:")
	log.Printf(" %s -apply-zip \"<путь_к_архиву(.tar.gz)>\" [\"<текущая_версия(дд.мм.гг)>\"] [<pid_FiReMQ>] — обновляет FiReMQ из указанного архива.", updaterName())
	log.Printf(" %s -rollback — откатывает FiReMQ к предыдущей версии из бэкапа.", updaterName())
	log.Printf(" %s --version — выводит версию утилиты.", updaterName())
	os.Exit(2) // Выход с кодом ошибки при неверном использовании
}

// RunApplyFromZip - если archPath указывает на архив (.tar.gz) — выполняет одиночное обновление, если archPath указывает на update_chain.json — по очереди применяет все обновления из цепочки.
func RunApplyFromZip(archPath, currentVersion, pidStr string) error {
	dir, err := exeDir()
	if err != nil {
		return fmt.Errorf("не удалось определить директорию апдейтера: %w", err)
	}
	archPath = normalizePath(archPath, dir)

	ext := strings.ToLower(filepath.Ext(archPath))
	if ext == ".json" {
		// update_chain.json
		return runApplyChainFromManifest(dir, archPath, currentVersion, pidStr)
	}

	// Одиночный архив (.tar.gz)
	return runApplySingleArchive(dir, archPath, currentVersion, pidStr)
}

// runApplySingleArchive обновление из одного архива
func runApplySingleArchive(dir, archPath, currentVersion, pidStr string) error {
	arch, err := OpenArchive(archPath)
	if err != nil {
		return fmt.Errorf("архив не найден или не открывается: %s (%v)", archPath, err)
	}

	man, err := parseManifestFromArchive(arch)
	if err != nil {
		return fmt.Errorf("ошибка чтения update.toml: %w", err)
	}

	// Шапка
	curVer := strings.TrimSpace(currentVersion)
	if curVer == "" {
		curVer = "00.00.00"
	}
	newVer := strings.TrimSpace(man.Version)

	log.Printf("Локальная версия: %s. Найдено обновлений: 1", curVer)
	if newVer != "" {
		log.Printf("1. Версия %s (из архива %s)", newVer, filepath.Base(archPath))
	} else {
		log.Printf("1. Версия не указана в манифесте (архив %s)", filepath.Base(archPath))
	}

	// Загружает главный конфиг server.conf
	confPath, confMap, _ := loadServerConfMap(dir)
	ops, needReplaceExe, err := buildPlan(man, dir, confMap, confPath)
	if err != nil {
		return err
	}
	dumpPlan(ops)

	// Полный путь к основному исполняемому файлу
	exeFull := filepath.Join(dir, exeName())

	// Ждёт полного завершения FiReMQ по PID
	if pidStr != "" {
		if err := waitPIDExit(pidStr, waitShutdownTimeout); err != nil {
			return fmt.Errorf("FiReMQ не завершился за %s: %w", waitShutdownTimeout, err)
		}
	} else {
		log.Printf("PID не передан — пропускаем точное ожидание процесса, продолжаем по тайм-аутам.")
	}

	// Пауза, чтобы БД и файлы успели закрыться
	time.Sleep(1 * time.Second) // Пауза для закрытия файлов и соединений с БД

	// Полный бэкап
	bakPath, err := CreateFullBackup(dir, curVer, confMap, confPath)
	if err != nil {
		return fmt.Errorf("не удалось создать полный бэкап перед обновлением: %w", err)
	}
	log.Printf("Полный бэкап создан: %s", bakPath)

	// Перед непосредственной заменой файлов - заголовок установки
	if newVer != "" {
		log.Printf(">>> Установка обновления 1 из 1: версия %s <<<", newVer)
	} else {
		log.Printf(">>> Установка обновления 1 из 1 (версия не указана в манифесте) <<<")
	}

	// Замена файлов
	if needReplaceExe {
		if err := waitFiReMQExit(exeFull); err != nil {
			return err
		}
	} else {
		time.Sleep(2 * time.Second) // Пауза если исполняемый файл не подлежит замене
	}

	// Применяет план
	stats, err := applyPlan(arch, ops)
	if err != nil {
		return fmt.Errorf("ошибка применения плана: %w", err)
	}

	log.Printf("Сводка: обновлено=%d, удалено=%d, пропущено удалений=%d",
		stats.Updated, stats.Deleted, stats.SkippedDelete)

	if newVer != "" {
		log.Printf("Версия %s успешно установлена.", newVer)
	} else {
		log.Printf("Обновление успешно установлено (версия не указана в манифесте).")
	}

	// Удаляет временную директорию
	tmpDir := filepath.Dir(archPath)
	if strings.EqualFold(filepath.Base(tmpDir), "tmp") {
		if err := os.RemoveAll(tmpDir); err != nil {
			log.Printf("Предупреждение: не удалось удалить временную директорию %s: %v", tmpDir, err)
		} else {
			log.Printf("Временная директория удалена: %s", tmpDir)
		}
	}

	// Запускает FiReMQ (на Linux предварительно устанавливаются права +x и владелец)
	startErr := startFiReMQ(exeFull)

	return startErr
}

// runApplyChainFromManifest применяет цепочку обновлений из update_chain.json (FiReMQ перезапускается только один раз - после установки последнего обновления)
func runApplyChainFromManifest(dir, manifestPath, currentVersion, pidStr string) error {
	exeFull := filepath.Join(dir, exeName())

	// Чтение манифеста цепочки
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("не удалось прочитать манифест цепочки обновлений %s: %w", manifestPath, err)
	}

	var chain UpdateChainManifest
	if err := json.Unmarshal(data, &chain); err != nil {
		return fmt.Errorf("некорректный формат update_chain.json: %w", err)
	}

	if len(chain.Items) == 0 {
		log.Printf("Цепочка обновлений пуста. Запуск FiReMQ без изменений.")
		return startFiReMQ(exeFull)
	}

	// Текущая версия из параметра
	curVer := strings.TrimSpace(currentVersion)
	if curVer == "" {
		curVer = "00.00.00"
	}

	// Шапка
	log.Printf("Локальная версия: %s. Найдено обновлений: %d", curVer, len(chain.Items))
	for i, it := range chain.Items {
		src := strings.TrimSpace(it.Repo)
		if src == "" {
			src = "репозиторий не указан"
		}
		log.Printf("%d. Версия %s (от %s)", i+1, strings.TrimSpace(it.Version), src)
	}

	// Ждёт завершения FiReMQ по PID (если передан)
	if pidStr != "" {
		if err := waitPIDExit(pidStr, waitShutdownTimeout); err != nil {
			return fmt.Errorf("FiReMQ не завершился за %s: %w", waitShutdownTimeout, err)
		}
	} else {
		log.Printf("PID не передан — пропускаем точное ожидание процесса, продолжаем по тайм-аутам.")
	}

	// Пауза для закрытия файлов / БД
	time.Sleep(1 * time.Second)

	// Загружает конфиг для бэкапа
	confPath, confMap, _ := loadServerConfMap(dir)

	// Полный бэкап перед всей цепочкой
	bakPath, err := CreateFullBackup(dir, curVer, confMap, confPath)
	if err != nil {
		return fmt.Errorf("не удалось создать полный бэкап перед обновлением: %w", err)
	}
	log.Printf("Полный бэкап создан: %s", bakPath)

	// Убеждается, что бинарник FiReMQ больше не используется
	if err := waitFiReMQExit(exeFull); err != nil {
		return err
	}

	// Последовательно применяет каждое обновление
	for idx, it := range chain.Items {
		ver := strings.TrimSpace(it.Version)
		total := len(chain.Items)

		if ver != "" {
			log.Printf(">>> Установка обновления %d из %d: версия %s <<<", idx+1, total, ver)
		} else {
			log.Printf(">>> Установка обновления %d из %d (версия не указана в цепочке) <<<", idx+1, total)
		}

		// Путь к архиву (относительно манифеста)
		itemArchPath := filepath.Join(filepath.Dir(manifestPath), it.FileName)

		arch, err := OpenArchive(itemArchPath)
		if err != nil {
			return fmt.Errorf("архив не найден или не открывается: %s (%v)", itemArchPath, err)
		}

		man, err := parseManifestFromArchive(arch)
		if err != nil {
			return fmt.Errorf("ошибка чтения update.toml: %w", err)
		}

		// Перед каждым шагом перечитывает server.conf
		confPath, confMap, _ = loadServerConfMap(dir)
		ops, _, err := buildPlan(man, dir, confMap, confPath)
		if err != nil {
			return err
		}
		dumpPlan(ops)

		stats, err := applyPlan(arch, ops)
		if err != nil {
			return fmt.Errorf("ошибка применения плана для версии %s: %w", ver, err)
		}

		log.Printf("Сводка: обновлено=%d, удалено=%d, пропущено удалений=%d",
			stats.Updated, stats.Deleted, stats.SkippedDelete)

		// Логирует установленную версию (приоритет версии из манифеста архива)
		manVer := strings.TrimSpace(man.Version)
		if manVer != "" {
			log.Printf("Версия %s успешно установлена.", manVer)
			curVer = manVer
		} else if ver != "" {
			log.Printf("Обновление %d из %d успешно установлено (целевой версии в манифесте нет, ожидаемая по цепочке: %s).", idx+1, total, ver)
		} else {
			log.Printf("Обновление %d из %d успешно установлено (версия не указана).", idx+1, total)
		}
	}

	// В конце удаляет временную директорию с цепочкой, если это .../Backup/tmp
	tmpDir := filepath.Dir(manifestPath)
	if strings.EqualFold(filepath.Base(tmpDir), "tmp") {
		if err := os.RemoveAll(tmpDir); err != nil {
			log.Printf("Предупреждение: не удалось удалить временную директорию %s: %v", tmpDir, err)
		} else {
			log.Printf("Временная директория удалена: %s", tmpDir)
		}
	}

	// Запускает FiReMQ один раз после окончания всей цепочки
	return startFiReMQ(exeFull)
}

// exeDir возвращает абсолютный путь к директории, где расположен апдейтер
func exeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// normalizePath приводит путь к чистому абсолютному или относительному пути относительно base
func normalizePath(p, base string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

// waitFiReMQExit гарантированно ожидает завершения процесса FiReMQ через /proc/<pid>/exe
func waitFiReMQExit(destExe string) error {
	deadline := time.Now().Add(waitShutdownTimeout)

	exe := filepath.Clean(destExe)
	for time.Now().Before(deadline) {
		running, _ := isExeRunningLinux(exe)
		if !running {
			return nil
		}
		time.Sleep(waitShutdownInterval)
	}
	return fmt.Errorf("процесс %s не завершился за %s", destExe, waitShutdownTimeout)
}

// isExeRunningLinux проверяет, существует ли процесс, чей исполняемый файл совпадает с destExe (учитывает " (deleted)")
func isExeRunningLinux(destExe string) (bool, error) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !allDigits(name) {
			continue
		}
		link := filepath.Join("/proc", name, "exe")
		target, err := os.Readlink(link)
		if err != nil {
			continue // Процесс мог завершиться в момент чтения
		}
		// Убирает суффикс " (deleted)", который добавляется ядром, когда бинарный файл удален
		t := strings.TrimSuffix(target, " (deleted)")
		if filepath.Clean(t) == destExe {
			return true, nil
		}
	}
	return false, nil
}

// allDigits проверяет, состоит ли строка полностью из цифр
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// startFiReMQ запускает FiReMQ как службу (при наличии systemd) или напрямую
func startFiReMQ(destExe string) error {
	name := exeName()

	setOwnerAndPerms(destExe, 0755) // Устанавливает права и владельца для исполняемого файла

	// Проверяет, существует ли systemd unit для FiReMQ
	// Ищет в /lib/systemd/system (из deb пакета) или /etc/systemd/system (пользовательский)
	if _, err := os.Stat("/lib/systemd/system/firemq.service"); err == nil {
		log.Printf("Обнаружен systemd сервис firemq.service — запуск через systemctl...")
		cmd := exec.Command("systemctl", "restart", "firemq.service")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("не удалось запустить %s через systemctl: %w", name, err)
		}
		log.Printf("%s успешно запущен через systemctl.", name)
		return nil
	}

	if _, err := os.Stat("/etc/systemd/system/firemq.service"); err == nil {
		log.Printf("Обнаружен systemd сервис firemq.service — запуск через systemctl...")
		cmd := exec.Command("systemctl", "restart", "firemq.service")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("не удалось запустить %s через systemctl: %w", name, err)
		}
		log.Printf("%s успешно запущен через systemctl.", name)
		return nil
	}

	// systemd unit отсутствует — fallback на прямой запуск
	log.Printf("systemd сервис не найден. Запуск напрямую...")

	cmd := exec.Command(destExe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось запустить %s: %w", name, err)
	}

	log.Printf("Запущен %s (PID=%d).", name, cmd.Process.Pid)
	return nil
}

// exeName возвращает имя исполняемого файла FiReMQ
func exeName() string {
	return "FiReMQ"
}

// updaterName возвращает имя исполняемого файла апдейтера
func updaterName() string {
	return "ServerUpdater"
}

// formatSize преобразует размер в удобночитаемые величины (Байт, КБ, МБ)
func formatSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d Байт", size)
	}
	fSize := float64(size) / 1024.0
	if fSize < 1024.0 {
		return fmt.Sprintf("%.2f КБ", fSize)
	}
	fSize /= 1024.0
	return fmt.Sprintf("%.2f МБ", fSize)
}
