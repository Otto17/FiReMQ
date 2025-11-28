// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	version = "26.11.25" // Текущая версия ServerUpdater в формате "дд.мм.гг"

	backupTimestampLayout = "02.01.06(в_15.04.05)" // Формат метки времени для резервной копии "дд.мм.гг(в_ЧЧ.ММ.СС)""
	backupPatternPrefix   = "bak_"                 // Префикс
	backupPatternSuffix   = "_FiReMQ.zip"          // Суффикс
	waitShutdownTimeout   = 30 * time.Second       // Максимальное время ожидания завершения FiReMQ
	waitShutdownInterval  = 500 * time.Millisecond // Интервал проверки завершения процесса
)

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

		log.Println("Откат завершён успешно.")
		LogSpacer(2) // Два пустых абзаца-разделителя
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

		log.Printf("Применение архива: %s (current=%s, pid=%s)", archPath, curVer, pidStr)
		if err := RunApplyFromZip(archPath, curVer, pidStr); err != nil {
			log.Fatalf("Ошибка применения архива: %v", err)
		}

		log.Println("Замена из локального архива завершена успешно.")
		LogSpacer(2) // Два пустых абзаца-разделителя
		return       // Завершает работу после успешного обновления
	}

	log.Println("Использование:")
	log.Printf(" %s -apply-zip \"<путь_к_архиву(.zip|.tar.gz)>\" [\"<текущая_версия(дд.мм.гг)>\"] [<pid_FiReMQ>] — обновляет FiReMQ из указанного архива.", updaterName())
	log.Printf(" %s -rollback — откатывает FiReMQ к предыдущей версии из бэкапа.", updaterName())
	log.Printf(" %s --version — выводит версию утилиты.", updaterName())
	os.Exit(2) // Выход с кодом ошибки при неверном использовании
}

// RunApplyFromZip ждёт завершения FiReMQ, делает бэкап, затем применяет обновление и запускает FiReMQ
func RunApplyFromZip(archPath, currentVersion, pidStr string) error {
	dir, err := exeDir()
	if err != nil {
		return fmt.Errorf("не удалось определить директорию апдейтера: %w", err)
	}
	archPath = normalizePath(archPath, dir)

	arch, err := OpenArchive(archPath)
	if err != nil {
		return fmt.Errorf("архив не найден или не открывается: %s (%v)", archPath, err)
	}
	defer arch.Close()

	man, err := parseManifestFromArchive(arch)
	if err != nil {
		return fmt.Errorf("ошибка чтения update.toml: %w", err)
	}

	// Загружает конфигурацию server.conf
	confPath, confMap, _ := loadServerConfMap(dir)
	ops, needReplaceExe, err := buildPlan(man, dir, confMap, confPath)
	if err != nil {
		return err
	}
	dumpPlan(ops)

	// Полный путь к основному исполняемому файлу
	exeFull := filepath.Join(dir, exeName())

	// Ждёт полного завершения FiReMQ по PID (если PID передан)
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
	bakPath, err := CreateFullBackup(dir, currentVersion, confMap, confPath)
	if err != nil {
		return fmt.Errorf("не удалось создать полный бэкап перед обновлением: %w", err)
	}
	log.Printf("Полный бэкап создан: %s", bakPath)

	// Замена файлов
	if needReplaceExe {
		if err := waitFiReMQExit(exeFull); err != nil {
			return err
		}
	} else {
		time.Sleep(2 * time.Second) // Пауза если исполняемый файл не подлежит замене
	}

	// Применяет план, возвращает true, если на Windows был отложен апдейт самого себя
	selfUpdatePending, err := applyPlan(arch, ops)
	if err != nil {
		return fmt.Errorf("ошибка применения плана: %w", err)
	}

	// Закрывает архив перед удалением временной директории
	if err := arch.Close(); err != nil {
		log.Printf("Предупреждение: не удалось закрыть архив: %v", err)
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

	// Если было запланировано самообновление (Windows)
	if selfUpdatePending {
		myExe, _ := os.Executable()
		newExe := strings.TrimSuffix(myExe, ".exe") + "_new.exe"
		log.Println("Запуск планировщика самообновления (замена ServerUpdater.exe после выхода)...")
		scheduleSelfUpdate(newExe, myExe)
	}

	return startErr
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

// waitFiReMQExit гарантированно ожидает завершения процесса FiReMQ
// На Linux использует /proc/<pid>/exe, на Windows пытается удалить исполняемый файл
func waitFiReMQExit(destExe string) error {
	deadline := time.Now().Add(waitShutdownTimeout)

	if runtime.GOOS == "linux" {
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

	// Логика для Windows: попытка удалить файл подтверждает завершение процесса
	for {
		err := os.Remove(destExe)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("не удалось удалить %s — процесс не завершился за %s: %v", destExe, waitShutdownTimeout, err)
		}
		time.Sleep(waitShutdownInterval)
	}
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

	if runtime.GOOS == "linux" {
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
		} else if _, err := os.Stat("/etc/systemd/system/firemq.service"); err == nil {
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

	// Windows
	cmd := exec.Command(destExe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось запустить %s: %w", name, err)
	}
	log.Printf("Запущен %s (PID=%d).", name, cmd.Process.Pid)
	return nil
}

// exeName возвращает имя исполняемого файла FiReMQ в зависимости от ОС
func exeName() string {
	if runtime.GOOS == "windows" {
		return "FiReMQ.exe"
	}
	return "FiReMQ"
}

// updaterName возвращает имя исполняемого файла апдейтера в зависимости от ОС
func updaterName() string {
	if runtime.GOOS == "windows" {
		return "ServerUpdater.exe"
	}
	return "ServerUpdater"
}
