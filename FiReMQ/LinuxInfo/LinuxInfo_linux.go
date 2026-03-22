// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

//go:build linux

package LinuxInfo

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"FiReMQ/pathsOS" // Локальный пакет с путями для разных платформ
)

// GetServerInfo собирает полную информацию о Linux сервере
func GetServerInfo() ServerInfo {
	info := ServerInfo{
		Available: true,
	}

	info.Server = getServerSection()    // Информация о сервере
	info.Temperature = getTemperature() // Информация о температурах
	info.Load = getLoadInfo()           // Информация о нагрузке
	info.Memory = getMemoryInfo()       // Информация о памяти
	info.Disks = getDisksInfo()         // Информация о дисках
	info.Network = getNetworkInfo()     // Информация о сети
	info.FiReMQ = getFiReMQInfo()       // Информация о FiReMQ

	return info
}

// getDistroInfo возвращает строку с названием и версией дистрибутива
func getDistroInfo() string {
	// Использует lsb_release
	if out, err := exec.Command("lsb_release", "-dsir").Output(); err == nil {
		// Добавляет к out кодовое имя
		if code, errCode := exec.Command("lsb_release", "-c").Output(); errCode == nil {
			return string(out) + " (" + strings.TrimSpace(string(code)) + ")"
		}
		return strings.TrimSpace(string(out))
	}

	// Чтение информации о релизе
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		var prettyName string
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				prettyName = strings.Trim(strings.SplitN(line, "=", 2)[1], "\"")
				break
			}
		}
		if prettyName != "" {
			return prettyName
		}
	}
	return "Неизвестно"
}

// getHardwareInfo возвращает строку с моделью железа
func getHardwareInfo() string {
	var vendor, model string

	// Читает Product Name
	if data, err := os.ReadFile("/sys/class/dmi/id/product_name"); err == nil {
		model = strings.TrimSpace(string(data))
	}

	// Если модели нет или она "System Product Name", пытается использовать dmidecode
	if model == "" || model == "System Product Name" || model == "To Be Filled By O.E.M." {
		if out, err := exec.Command("dmidecode", "-s", "system-manufacturer").Output(); err == nil {
			vendor = strings.TrimSpace(string(out))
		}
		if out, err := exec.Command("dmidecode", "-s", "system-product-name").Output(); err == nil {
			model = strings.TrimSpace(string(out))
		}
	}

	if model != "" {
		if vendor != "" && !strings.Contains(model, vendor) {
			return vendor + " " + model
		}
		return model
	}
	return "Неизвестно"
}

// getServerSection собирает общую информацию о сервере
func getServerSection() *ServerSection {
	section := &ServerSection{}

	section.Distro = getDistroInfo()
	section.Kernel = getOSVersion()
	section.Hardware = getHardwareInfo()
	section.CPU = getCPUInfo()
	section.TotalRAM = getTotalRAM()

	// Серверное время с часовым поясом в одной строке: "дд.мм.гггг (ЧЧ:ММ:СС) (UTC +06)"
	now := time.Now()
	_, offset := now.Zone()
	hours := offset / 3600
	var utcStr string
	if hours >= 0 {
		utcStr = fmt.Sprintf("UTC +%02d", hours)
	} else {
		utcStr = fmt.Sprintf("UTC -%02d", -hours)
	}
	section.ServerTime = fmt.Sprintf("%s (%s)", now.Format("02.01.2006 (15:04:05)"), utcStr)

	section.Uptime = getUptime()

	return section
}

// getOSVersion возвращает версию ОС и архитектуру
func getOSVersion() string {
	// Получение версии ядра
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return "Linux x86_64"
	}

	// Извлечение версии ядра из строки "Linux version X.X.X-..."
	parts := strings.Fields(string(data))
	kernelVersion := ""
	if len(parts) >= 3 {
		kernelVersion = parts[2]
	}

	return fmt.Sprintf("Linux %s x86_64", kernelVersion)
}

// getCPUInfo возвращает информацию о процессоре
func getCPUInfo() string {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "Неизвестно"
	}
	defer file.Close()

	var modelName string
	var processorCount int

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			if modelName == "" {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					modelName = strings.TrimSpace(parts[1])
				}
			}
		}
		if strings.HasPrefix(line, "processor") {
			processorCount++
		}
	}

	if modelName == "" {
		modelName = "Неизвестно"
	}

	// Формирует строку с количеством потоков
	threadWord := getThreadWord(processorCount)
	return fmt.Sprintf("%s (%d %s)", modelName, processorCount, threadWord)
}

// getThreadWord возвращает правильную форму слова "поток" в зависимости от числа
func getThreadWord(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return "поток"
	}
	if n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20) {
		return "потока"
	}
	return "потоков"
}

// getTotalRAM возвращает общий объём оперативной памяти
func getTotalRAM() string {
	memInfo := parseMemInfo()
	if total, ok := memInfo["MemTotal"]; ok {
		return formatBytes(total * 1024) // /proc/meminfo в КБ
	}
	return "Неизвестно"
}

// getUptime возвращает время работы системы в читаемом формате
func getUptime() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "Неизвестно"
	}

	parts := strings.Fields(string(data))
	if len(parts) < 1 {
		return "Неизвестно"
	}

	uptimeSeconds, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return "Неизвестно"
	}

	return formatDuration(int64(uptimeSeconds))
}

// formatDuration форматирует продолжительность в читаемый вид
func formatDuration(seconds int64) string {
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	var parts []string

	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", days, getDayWord(int(days))))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", hours, getHourWord(int(hours))))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", minutes, getMinuteWord(int(minutes))))
	}
	if secs > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d %s", secs, getSecondWord(int(secs))))
	}

	return strings.Join(parts, ", ")
}

// getDayWord возвращает правильную форму слова "день"
func getDayWord(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return "день"
	}
	if n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20) {
		return "дня"
	}
	return "дней"
}

// getHourWord возвращает правильную форму слова "час"
func getHourWord(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return "час"
	}
	if n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20) {
		return "часа"
	}
	return "часов"
}

// getMinuteWord возвращает правильную форму слова "минута"
func getMinuteWord(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return "минута"
	}
	if n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20) {
		return "минуты"
	}
	return "минут"
}

// getSecondWord возвращает правильную форму слова "секунда"
func getSecondWord(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return "секунда"
	}
	if n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20) {
		return "секунды"
	}
	return "секунд"
}

// getTemperature собирает информацию о температуре с доступных датчиков
func getTemperature() []TemperatureInfo {
	var temps []TemperatureInfo
	hasCPUFromHwmon := false

	// Сбор данных из hwmon
	hwmonDirs, _ := filepath.Glob("/sys/class/hwmon/hwmon*")
	for _, hwmonDir := range hwmonDirs {
		deviceName := ""
		if nameData, err := os.ReadFile(filepath.Join(hwmonDir, "name")); err == nil {
			deviceName = strings.TrimSpace(string(nameData))
		}

		tempInputs, _ := filepath.Glob(filepath.Join(hwmonDir, "temp*_input"))
		for _, tempInput := range tempInputs {
			data, err := os.ReadFile(tempInput)
			if err != nil {
				continue
			}

			tempMilliC, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if err != nil || tempMilliC <= 0 {
				continue
			}

			tempC := float64(tempMilliC) / 1000.0

			// Пропускает некорректные значения
			if tempC < -40 || tempC > 150 {
				continue
			}

			sensorLabel := ""
			labelFile := strings.Replace(tempInput, "_input", "_label", 1)
			if labelData, err := os.ReadFile(labelFile); err == nil {
				sensorLabel = strings.TrimSpace(string(labelData))
			}

			label := translateTempSensor(deviceName, sensorLabel)

			if isCPUSensor(deviceName) {
				hasCPUFromHwmon = true
			}

			temps = append(temps, TemperatureInfo{
				Label:       label,
				Temperature: fmt.Sprintf("%.0f°C", tempC),
			})
		}
	}

	// Сбор данных из thermal_zone (дополнение к hwmon для датчиков, не представленных в hwmon)
	thermalZones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	for _, tempFile := range thermalZones {
		zoneDir := filepath.Dir(tempFile)
		rawType := ""
		if typeData, err := os.ReadFile(filepath.Join(zoneDir, "type")); err == nil {
			rawType = strings.TrimSpace(string(typeData))
		}

		// Пропускает датчики CPU из thermal_zone, если hwmon уже предоставил данные CPU
		if hasCPUFromHwmon && isCPUSensorByType(rawType) {
			continue
		}

		data, err := os.ReadFile(tempFile)
		if err != nil {
			continue
		}

		tempMilliC, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil || tempMilliC <= 0 {
			continue
		}

		tempC := float64(tempMilliC) / 1000.0
		if tempC < -40 || tempC > 150 {
			continue
		}

		label := translateTempSensor(rawType, "")

		temps = append(temps, TemperatureInfo{
			Label:       label,
			Temperature: fmt.Sprintf("%.0f°C", tempC),
		})
	}

	// Удаление точных дубликатов (одинаковые метка + температура)
	temps = deduplicateTemps(temps)

	// Нумерация повторяющихся меток (#1, #2, ...)
	temps = numberDuplicateLabels(temps)

	return temps
}

// translateTempSensor переводит названия датчиков температуры на Русский язык
func translateTempSensor(deviceName, sensorLabel string) string {
	devLower := strings.ToLower(deviceName)
	lblLower := strings.ToLower(sensorLabel)

	// Процессор Intel (coretemp)
	if strings.Contains(devLower, "coretemp") {
		if strings.Contains(lblLower, "package") {
			return "Процессор (общая)"
		}
		if strings.Contains(lblLower, "core") {
			// Извлекает номер ядра из "Core 0", "Core 1" и т.д.
			for _, field := range strings.Fields(sensorLabel) {
				if num, err := strconv.Atoi(field); err == nil {
					return fmt.Sprintf("Процессор (ядро %d)", num)
				}
			}
			return "Процессор (ядро)"
		}
		return "Процессор"
	}

	// Датчики ACPI на системной плате
	if strings.Contains(devLower, "acpitz") {
		return "Датчик системной платы"
	}

	// Температура корпуса процессора (thermal_zone)
	if strings.Contains(devLower, "x86_pkg_temp") {
		return "Процессор"
	}

	// Процессор AMD
	if strings.Contains(devLower, "k10temp") || strings.Contains(devLower, "zenpower") {
		if strings.Contains(lblLower, "tctl") || strings.Contains(lblLower, "tdie") {
			return "Процессор AMD (общая)"
		}
		if strings.Contains(lblLower, "tccd") {
			for _, field := range strings.Fields(sensorLabel) {
				if num, err := strconv.Atoi(field); err == nil {
					return fmt.Sprintf("Процессор AMD (CCD %d)", num)
				}
			}
		}
		return "Процессор AMD"
	}

	// Чипсет (PCH)
	if strings.Contains(devLower, "pch") {
		return "Чипсет"
	}

	// Wi-Fi адаптер
	if strings.Contains(devLower, "iwlwifi") {
		return "Wi-Fi адаптер"
	}

	// NVMe накопитель
	if strings.Contains(devLower, "nvme") {
		if sensorLabel != "" {
			return fmt.Sprintf("NVMe накопитель (%s)", sensorLabel)
		}
		return "NVMe накопитель"
	}

	// Видеокарты
	if strings.Contains(devLower, "amdgpu") || strings.Contains(devLower, "radeon") {
		return "Видеокарта AMD"
	}
	if strings.Contains(devLower, "nouveau") || strings.Contains(devLower, "nvidia") {
		return "Видеокарта NVIDIA"
	}

	// Датчики на системной плате (IT87xx, NCT67xx, Winbond, Fintek)
	if strings.Contains(devLower, "nct6") || strings.Contains(devLower, "it87") ||
		strings.Contains(devLower, "w83") || strings.Contains(devLower, "f71") {
		if sensorLabel != "" {
			return fmt.Sprintf("Системная плата (%s)", sensorLabel)
		}
		return "Системная плата"
	}

	// По умолчанию: возвращает оригинальное название
	if sensorLabel != "" {
		return fmt.Sprintf("%s (%s)", deviceName, sensorLabel)
	}
	return deviceName
}

// isCPUSensor проверяет, является ли hwmon устройство датчиком процессора
func isCPUSensor(deviceName string) bool {
	dl := strings.ToLower(deviceName)
	return strings.Contains(dl, "coretemp") ||
		strings.Contains(dl, "k10temp") ||
		strings.Contains(dl, "zenpower")
}

// isCPUSensorByType проверяет, является ли тип thermal_zone датчиком процессора
func isCPUSensorByType(zoneType string) bool {
	tl := strings.ToLower(zoneType)
	return strings.Contains(tl, "x86_pkg_temp") ||
		strings.Contains(tl, "coretemp")
}

// deduplicateTemps удаляет точные дубликаты (одинаковые метка + температура)
func deduplicateTemps(temps []TemperatureInfo) []TemperatureInfo {
	seen := make(map[string]bool)
	result := make([]TemperatureInfo, 0, len(temps))
	for _, t := range temps {
		key := t.Label + "|" + t.Temperature
		if !seen[key] {
			seen[key] = true
			result = append(result, t)
		}
	}
	return result
}

// numberDuplicateLabels добавляет нумерацию (#1, #2, ...) к повторяющимся меткам
func numberDuplicateLabels(temps []TemperatureInfo) []TemperatureInfo {
	// Подсчитывает количество записей с одинаковыми метками
	counts := make(map[string]int)
	for _, t := range temps {
		counts[t.Label]++
	}

	// Нумерует только те метки, которые встречаются более одного раза
	seen := make(map[string]int)
	for i := range temps {
		label := temps[i].Label
		if counts[label] > 1 {
			seen[label]++
			temps[i].Label = fmt.Sprintf("%s #%d", label, seen[label])
		}
	}

	return temps
}

// getLoadInfo возвращает информацию о нагрузке системы
func getLoadInfo() *LoadInfo {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}

	parts := strings.Fields(string(data))
	if len(parts) < 3 {
		return nil
	}

	cpuCount := runtime.NumCPU()
	load := &LoadInfo{}

	avg1, _ := strconv.ParseFloat(parts[0], 64)
	avg5, _ := strconv.ParseFloat(parts[1], 64)
	avg15, _ := strconv.ParseFloat(parts[2], 64)

	// Рассчитывает проценты нагрузки относительно количества логических процессоров
	pct1, pct5, pct15 := float64(0), float64(0), float64(0)
	if cpuCount > 0 {
		cpu := float64(cpuCount)
		pct1 = clampPercent((avg1 / cpu) * 100)
		pct5 = clampPercent((avg5 / cpu) * 100)
		pct15 = clampPercent((avg15 / cpu) * 100)
	}

	// Формирует строки с нагрузкой
	load.Load1Min = fmt.Sprintf("%.1f%% (%.2f)", pct1, avg1)
	load.Load5Min = fmt.Sprintf("%.1f%% (%.2f)", pct5, avg5)
	load.Load15Min = fmt.Sprintf("%.1f%% (%.2f)", pct15, avg15)

	// Оценка нагрузки для каждого интервала
	load.CPUStatus1 = formatCPUStatus(avg1, cpuCount)
	load.CPUStatus5 = formatCPUStatus(avg5, cpuCount)
	load.CPUStatus15 = formatCPUStatus(avg15, cpuCount)

	return load
}

// formatCPUStatus формирует строку с оценкой нагрузки относительно количества потоков.
// Пороги:
//   - LA < 70% потоков  → Низкая (запас ресурсов)
//   - LA < 100% потоков → Нормальная (система нагружена, но справляется)
//   - LA < 200% потоков → Высокая (процессы начинают ожидать в очереди)
//   - LA >= 200% потоков → Критическая (серьёзная перегрузка)
func formatCPUStatus(loadAvg float64, cpuCount int) string {
	threshold := float64(cpuCount) // Предельное значение LA (равно количеству потоков)

	var level string
	switch {
	case loadAvg < threshold*0.7:
		level = "Низкая"
	case loadAvg < threshold:
		level = "Нормальная"
	case loadAvg < threshold*2:
		level = "Высокая"
	default:
		level = "Критическая"
	}

	threadWord := getThreadWord(cpuCount)
	return fmt.Sprintf("%s — %d %s (предел: %.2f)", level, cpuCount, threadWord, threshold)
}

// clampPercent ограничивает значение процента максимумом 100%
func clampPercent(v float64) float64 {
	if v > 100 {
		return 100
	}
	return v
}

// parseMemInfo читает и парсит /proc/meminfo
func parseMemInfo() map[string]uint64 {
	result := make(map[string]uint64)

	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return result
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		key := strings.TrimSuffix(parts[0], ":")
		value, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}

		result[key] = value
	}

	return result
}

// getMemoryInfo возвращает информацию об RAM И SWAP
func getMemoryInfo() *MemoryInfo {
	memInfo := parseMemInfo()

	mem := &MemoryInfo{}

	// RAM
	if total, ok := memInfo["MemTotal"]; ok {
		totalBytes := total * 1024
		mem.RAMTotal = formatBytes(totalBytes)

		// Вычисляет используемую память
		free := memInfo["MemFree"] * 1024
		buffers := memInfo["Buffers"] * 1024
		cached := memInfo["Cached"] * 1024
		sReclaimable := memInfo["SReclaimable"] * 1024

		// Доступная память
		available := free + buffers + cached + sReclaimable
		if memAvail, ok := memInfo["MemAvailable"]; ok {
			available = memAvail * 1024
		}

		used := totalBytes - available
		mem.RAMUsed = formatBytes(used)
		mem.RAMFree = formatBytes(available)
	}

	// SWAP
	if swapTotal, ok := memInfo["SwapTotal"]; ok {
		swapTotalBytes := swapTotal * 1024
		mem.SwapTotal = formatBytes(swapTotalBytes)

		swapFreeBytes := memInfo["SwapFree"] * 1024
		swapUsedBytes := swapTotalBytes - swapFreeBytes

		mem.SwapUsed = formatBytes(swapUsedBytes)
		mem.SwapFree = formatBytes(swapFreeBytes)
	}

	return mem
}

// formatBytes форматирует байты в человекочитаемый формат
func formatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f ТБ", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.2f ГБ", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f МБ", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f КБ", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d Б", bytes)
	}
}

// getDisksInfo возвращает информацию только о реальных физических дисках/разделах
func getDisksInfo() []DiskInfo {
	var disks []DiskInfo

	file, err := os.Open("/proc/mounts")
	if err != nil {
		return disks
	}
	defer file.Close()

	// Список допустимых файловых систем
	validFS := map[string]bool{
		"ext4": true, "ext3": true, "ext2": true,
		"xfs": true, "btrfs": true, "zfs": true,
		"ntfs": true, "vfat": true, "exfat": true, "f2fs": true,
	}

	// Структура для хранения записи о точке монтирования
	type mountEntry struct {
		realDevice string
		mountPoint string
		fsType     string
	}

	// Собирает записи, оставляя для каждого устройства только основную (самую короткую) точку монтирования
	bestMount := make(map[string]mountEntry)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		device := fields[0]
		mountPoint := fields[1]
		fsType := fields[2]

		// Пропускает виртуальные файловые системы
		if !validFS[fsType] {
			continue
		}

		// Только реальные блочные устройства (/dev/...)
		if !strings.HasPrefix(device, "/dev/") {
			continue
		}

		// Разрешает симлинки для определения реального устройства (например, /dev/mapper/... → /dev/dm-X)
		realDevice := device
		if resolved, err := filepath.EvalSymlinks(device); err == nil {
			realDevice = resolved
		}

		// Сохраняет запись с самой короткой точкой монтирования (основной раздел, а не bind-mount)
		existing, ok := bestMount[realDevice]
		if !ok || len(mountPoint) < len(existing.mountPoint) {
			bestMount[realDevice] = mountEntry{
				realDevice: realDevice,
				mountPoint: mountPoint,
				fsType:     fsType,
			}
		}
	}

	// Преобразует собранные записи в DiskInfo
	for _, entry := range bestMount {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(entry.mountPoint, &stat); err != nil {
			continue
		}

		totalBytes := stat.Blocks * uint64(stat.Bsize)
		freeBytes := stat.Bavail * uint64(stat.Bsize)
		usedBytes := totalBytes - (stat.Bfree * uint64(stat.Bsize))

		usedPercent := float64(0)
		if totalBytes > 0 {
			usedPercent = (float64(usedBytes) / float64(totalBytes)) * 100
		}

		deviceName := filepath.Base(entry.realDevice)

		disks = append(disks, DiskInfo{
			Device:      deviceName,
			MountPoint:  entry.mountPoint,
			FSType:      entry.fsType,
			Total:       formatBytes(totalBytes),
			Available:   formatBytes(freeBytes),
			Used:        formatBytes(usedBytes),
			UsedPercent: fmt.Sprintf("%.1f%%", usedPercent),
		})
	}

	// Сортировка по точке монтирования для стабильного представления
	sort.Slice(disks, func(i, j int) bool {
		return disks[i].MountPoint < disks[j].MountPoint
	})

	return disks
}

// getNetworkInfo возвращает информацию о сети
func getNetworkInfo() *NetworkInfo {
	netInfo := &NetworkInfo{}

	// Хост
	hostname, err := os.Hostname()
	if err == nil {
		netInfo.Hostname = hostname
	}

	// Шлюз (из /proc/net/route)
	netInfo.Gateway = getDefaultGateway()

	// DNS серверы (из /etc/resolv.conf)
	netInfo.DNS = getDNSServers()

	// Интерфейсы
	netInfo.Interfaces = getNetworkInterfaces()

	return netInfo
}

// getDefaultGateway возвращает IP шлюза по умолчанию
func getDefaultGateway() string {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return "Неизвестно"
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Пропускает заголовок
	scanner.Scan()

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}

		// Ищет маршрут по умолчанию (Destination = 00000000)
		if fields[1] == "00000000" {
			// Шлюз в шестнадцатеричном формате (little-endian)
			gatewayHex := fields[2]
			return parseHexIP(gatewayHex)
		}
	}

	return "Неизвестно"
}

// getDNSServers возвращает список DNS-серверов из /etc/resolv.conf
func getDNSServers() []string {
	var servers []string

	file, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return servers
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Пропускает комментарии и пустые строки
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "nameserver") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				servers = append(servers, fields[1])
			}
		}
	}

	return servers
}

// parseHexIP преобразует шестнадцатеричный IP (little-endian) в строку
func parseHexIP(hexIP string) string {
	if len(hexIP) != 8 {
		return "Неизвестно"
	}

	// Преобразует каждый байт
	var octets [4]uint64
	for i := 0; i < 4; i++ {
		octet, err := strconv.ParseUint(hexIP[i*2:i*2+2], 16, 8)
		if err != nil {
			return "Неизвестно"
		}
		octets[i] = octet
	}

	// Little-endian: байты идут в обратном порядке
	return fmt.Sprintf("%d.%d.%d.%d", octets[3], octets[2], octets[1], octets[0])
}

// getNetworkInterfaces возвращает список сетевых интерфейсов, читая данные из /sys и /proc
func getNetworkInterfaces() []InterfaceInfo {
	// Инициализация пустым срезом (не nil), чтобы JSON отдавал [] вместо null
	interfaces := make([]InterfaceInfo, 0)

	// Получает список интерфейсов из /sys/class/net/
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return interfaces
	}

	// Предварительно загружает таблицу IPv6 адресов из /proc/net/if_inet6
	ipv6Map := loadIPv6Addresses()

	for _, entry := range entries {
		ifaceName := entry.Name()
		basePath := "/sys/class/net/" + ifaceName

		info := InterfaceInfo{
			Name: ifaceName,
		}

		// Статус интерфейса из /sys/class/net/<iface>/operstate и flags
		info.Status = readInterfaceStatus(basePath)

		// MAC-адрес из /sys/class/net/<iface>/address (пропускает нулевой MAC loopback)
		if data, err := os.ReadFile(basePath + "/address"); err == nil {
			mac := strings.TrimSpace(string(data))
			if mac != "" && mac != "00:00:00:00:00:00" {
				info.MAC = mac
			}
		}

		// Скорость и дуплекс
		info.Speed, info.Duplex = getInterfaceSpeedDuplex(ifaceName)

		// IPv4 адрес через ioctl SIOCGIFADDR
		info.IPv4 = getIPv4ViaIoctl(ifaceName)

		// IPv6 адрес из /proc/net/if_inet6
		if addrs, ok := ipv6Map[ifaceName]; ok && len(addrs) > 0 {
			info.IPv6 = addrs[0]
		}

		interfaces = append(interfaces, info)
	}

	// Сортировка: реальные интерфейсы с IP/MAC первыми, loopback последним
	sort.Slice(interfaces, func(i, j int) bool {
		a := interfaces[i]
		b := interfaces[j]

		aLoop := a.Name == "lo"
		bLoop := b.Name == "lo"

		// Loopback всегда в конце
		if aLoop != bLoop {
			return !aLoop
		}

		// Интерфейсы с IP или MAC первыми
		aHasIP := a.IPv4 != "" || a.MAC != ""
		bHasIP := b.IPv4 != "" || b.MAC != ""

		if aHasIP != bHasIP {
			return aHasIP
		}

		return a.Name < b.Name
	})

	return interfaces
}

// readInterfaceStatus определяет статус интерфейса из /sys/class/net/<iface>/operstate и flags
func readInterfaceStatus(basePath string) string {
	if data, err := os.ReadFile(basePath + "/operstate"); err == nil {
		state := strings.TrimSpace(string(data))
		switch state {
		case "up":
			return "Включён"
		case "down":
			return "Отключён"
		case "unknown":
			// "unknown" означает, что интерфейс поднят, но состояние линка не определяется
			// Проверяет флаг IFF_UP
			if flagsData, err := os.ReadFile(basePath + "/flags"); err == nil {
				flagStr := strings.TrimSpace(string(flagsData))
				flagStr = strings.TrimPrefix(flagStr, "0x")
				if flags, err := strconv.ParseUint(flagStr, 16, 32); err == nil {
					if flags&0x1 != 0 { // IFF_UP
						return "Включён"
					}
				}
			}
			return "Отключён"
		default:
			return state
		}
	}
	return "Неизвестно"
}

// getIPv4ViaIoctl получает IPv4 адрес интерфейса через ioctl SIOCGIFADDR
func getIPv4ViaIoctl(ifaceName string) string {
	const siocgifaddr = 0x8915 // SIOCGIFADDR

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return ""
	}
	defer syscall.Close(fd)

	// struct ifreq: ifr_name[16] + union{sockaddr ifr_addr, ...}[24] = 40 байт
	var req [40]byte
	copy(req[:16], ifaceName)

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(siocgifaddr),
		uintptr(unsafe.Pointer(&req[0])),
	)
	if errno != 0 {
		return ""
	}

	// struct sockaddr_in начинается с offset 16:
	// [16:18] = sa_family (AF_INET)
	// [18:20] = sin_port
	// [20:24] = sin_addr (4 байта IPv4 адреса)
	return fmt.Sprintf("%d.%d.%d.%d", req[20], req[21], req[22], req[23])
}

// loadIPv6Addresses загружает IPv6 адреса всех интерфейсов из /proc/net/if_inet6
func loadIPv6Addresses() map[string][]string {
	result := make(map[string][]string)

	file, err := os.Open("/proc/net/if_inet6")
	if err != nil {
		return result
	}
	defer file.Close()

	// Формат строки: hex_addr ifindex prefix_len scope flags iface_name
	// Пример: fe800000000000000000000000000001 01 80 20 80 lo
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 {
			continue
		}

		hexAddr := fields[0]
		ifaceName := fields[5]

		if len(hexAddr) != 32 {
			continue
		}

		// Преобразует 32 hex-символа в полный IPv6 адрес (8 групп по 4 символа)
		var groups []string
		for i := 0; i < 32; i += 4 {
			groups = append(groups, hexAddr[i:i+4])
		}
		fullIPv6 := strings.Join(groups, ":")

		ip := net.ParseIP(fullIPv6)
		if ip == nil {
			continue
		}

		formatted := ip.String() // Сокращённая форма (::1 вместо 0000:0000:...:0001)

		// Глобальные адреса размещает первыми, link-local — после
		if ip.IsLinkLocalUnicast() {
			result[ifaceName] = append(result[ifaceName], formatted)
		} else {
			result[ifaceName] = append([]string{formatted}, result[ifaceName]...)
		}
	}

	return result
}

// getInterfaceSpeedDuplex возвращает скорость и режим дуплекса интерфейса
func getInterfaceSpeedDuplex(ifaceName string) (speed, duplex string) {
	speed = "неизвестно"
	duplex = "неизвестно"

	// Читает скорость
	speedFile := fmt.Sprintf("/sys/class/net/%s/speed", ifaceName)
	if data, err := os.ReadFile(speedFile); err == nil {
		speedMbps := strings.TrimSpace(string(data))
		if speedMbps != "-1" && speedMbps != "" {
			mbps, err := strconv.Atoi(speedMbps)
			if err == nil {
				if mbps >= 1000 {
					speed = fmt.Sprintf("%d Гбит/с", mbps/1000)
				} else {
					speed = fmt.Sprintf("%d Мбит/с", mbps)
				}
			}
		}
	}

	// Читает дуплекс
	duplexFile := fmt.Sprintf("/sys/class/net/%s/duplex", ifaceName)
	if data, err := os.ReadFile(duplexFile); err == nil {
		duplexValue := strings.TrimSpace(string(data))
		switch duplexValue {
		case "full":
			duplex = "полный"
		case "half":
			duplex = "полудуплекс"
		}
	}

	return speed, duplex
}

// getFiReMQInfo возвращает информацию о директориях и процессе FiReMQ
func getFiReMQInfo() *FiReMQInfo {
	info := &FiReMQInfo{}

	// Информация о работающем процессе FiReMQ
	info.Process = getProcessInfo()

	// Список директорий для отображения, собирает имя и путь из главного конфига "server.conf"
	directories := []struct {
		Name string
		Path string
	}{
		{"Конфиги", filepath.Dir(pathsOS.ServerConfPath)},
		{"Сертификаты", filepath.Dir(pathsOS.Path_Web_Key)},
		{"Исполняемые файлы", getExecutablePath()},
		{"7-ZIP", pathsOS.Path_7zip},
		{"WEB", pathsOS.Path_Web_Data},
		{"Бэкапы", pathsOS.Path_Backup},
		{"БД", pathsOS.Path_DB},
		{"Загрузки", pathsOS.Path_QUIC_Downloads},
		{"Отчёты по клиентам", pathsOS.Path_Info},
		{"Логи", pathsOS.Path_Logs},
	}

	for _, dir := range directories {
		if dir.Path == "" {
			continue
		}

		size := getDirSize(dir.Path)
		info.Directories = append(info.Directories, DirectoryInfo{
			Name: dir.Name,
			Path: dir.Path,
			Size: formatBytes(size),
		})
	}

	return info
}

// getExecutablePath возвращает путь к директории исполняемого файла
func getExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// getDirSize рекурсивно вычисляет размер директории
func getDirSize(path string) uint64 {
	var size uint64

	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Пропускает файлы с ошибками доступа
		}
		if !info.IsDir() {
			size += uint64(info.Size())
		}
		return nil
	})

	return size
}

// getProcessInfo возвращает информацию о текущем процессе FiReMQ
func getProcessInfo() *ProcessInfo {
	pid := os.Getpid()
	pidStr := strconv.Itoa(pid)
	procPath := "/proc/" + pidStr

	proc := &ProcessInfo{
		PID: pid,
	}

	// UID и GID пользователя из /proc/<pid>/status
	var uid, gid int
	if file, err := os.Open(procPath + "/status"); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "Uid:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					uid, _ = strconv.Atoi(fields[1])
				}
			}
			if strings.HasPrefix(line, "Gid:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					gid, _ = strconv.Atoi(fields[1])
				}
			}
		}
		file.Close()
	}

	// Формирует строки владельца и группы: "имя [ID]"
	userName := getUserNameByUID(uid)
	groupName := getGroupNameByGID(gid)
	proc.Owner = fmt.Sprintf("%s [%d]", userName, uid)
	proc.Group = fmt.Sprintf("%s [%d]", groupName, gid)

	// Потребление оперативной памяти (RSS — размер резидентного набора) из /proc/<pid>/status
	if file, err := os.Open(procPath + "/status"); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "VmRSS:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if rssKB, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
						proc.Memory = formatBytes(rssKB * 1024)
					}
				}
				break
			}
		}
		file.Close()
	}

	// Приоритет (nice) из /proc/<pid>/stat с текстовым описанием
	if data, err := os.ReadFile(procPath + "/stat"); err == nil {
		nice := parseNiceFromStat(string(data))
		proc.Priority = formatPriority(nice)
	}

	// Нагрузка на CPU и время работы процесса из /proc/<pid>/stat и /proc/uptime
	proc.CPU, proc.Uptime = getProcessCPUAndUptime(procPath)

	return proc
}

// getUserNameByUID определяет имя пользователя по UID из /etc/passwd
func getUserNameByUID(uid int) string {
	file, err := os.Open("/etc/passwd")
	if err != nil {
		return strconv.Itoa(uid)
	}
	defer file.Close()

	uidStr := strconv.Itoa(uid)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// Формат: name:x:uid:gid:...
		parts := strings.SplitN(scanner.Text(), ":", 4)
		if len(parts) >= 3 && parts[2] == uidStr {
			return parts[0]
		}
	}

	return strconv.Itoa(uid)
}

// getGroupNameByGID определяет имя группы по GID из /etc/group
func getGroupNameByGID(gid int) string {
	file, err := os.Open("/etc/group")
	if err != nil {
		return strconv.Itoa(gid)
	}
	defer file.Close()

	gidStr := strconv.Itoa(gid)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// Формат: name:x:gid:members
		parts := strings.SplitN(scanner.Text(), ":", 4)
		if len(parts) >= 3 && parts[2] == gidStr {
			return parts[0]
		}
	}

	return strconv.Itoa(gid)
}

// formatPriority преобразует значение nice в читаемое описание с числовым значением
func formatPriority(nice int) string {
	var label string
	switch {
	case nice < 0:
		label = "Высокий"
	case nice == 0:
		label = "Нормальный"
	default:
		label = "Низкий"
	}
	return fmt.Sprintf("%s [%d]", label, nice)
}

// parseNiceFromStat извлекает значение nice из строки /proc/<pid>/stat
func parseNiceFromStat(statLine string) int {
	// Находит конец имени процесса (после последней закрывающей скобки)
	closeIdx := strings.LastIndex(statLine, ")")
	if closeIdx < 0 || closeIdx+2 >= len(statLine) {
		return 0
	}

	// После ") " идут поля, начиная с 3-го (state)
	// nice — это поле 19, то есть индекс 16 от начала оставшихся полей (19 - 3 = 16)
	fields := strings.Fields(statLine[closeIdx+2:])
	if len(fields) < 17 {
		return 0
	}

	nice, err := strconv.Atoi(fields[16])
	if err != nil {
		return 0
	}
	return nice
}

// getProcessCPUAndUptime возвращает нагрузку на CPU в % и время работы процесса
func getProcessCPUAndUptime(procPath string) (cpuPercent, uptimeStr string) {
	cpuPercent = "0.0%"
	uptimeStr = "Неизвестно"

	// Читает /proc/<pid>/stat
	statData, err := os.ReadFile(procPath + "/stat")
	if err != nil {
		return
	}

	// Парсит utime, stime и starttime из /proc/<pid>/stat
	closeIdx := strings.LastIndex(string(statData), ")")
	if closeIdx < 0 || closeIdx+2 >= len(statData) {
		return
	}

	fields := strings.Fields(string(statData)[closeIdx+2:])
	// Поля (нумерация от 3-го): state(0), ppid(1), pgrp(2), session(3), tty(4),
	// tpgid(5), flags(6), minflt(7), cminflt(8), majflt(9), cmajflt(10),
	// utime(11), stime(12), cutime(13), cstime(14), priority(15), nice(16),
	// num_threads(17), itrealvalue(18), starttime(19)
	if len(fields) < 20 {
		return
	}

	utime, _ := strconv.ParseUint(fields[11], 10, 64)     // Время CPU в пользовательском режиме
	stime, _ := strconv.ParseUint(fields[12], 10, 64)     // Время CPU в режиме ядра
	starttime, _ := strconv.ParseUint(fields[19], 10, 64) // Время запуска процесса (в тиках от загрузки системы)

	// Получает количество тиков в секунду
	clkTck := uint64(100) // Значение по умолчанию для Linux
	if data, err := os.ReadFile("/proc/self/auxv"); err == nil {
		clkTck = parseClkTckFromAuxv(data)
	}

	// Время работы процесса
	uptimeData, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return
	}
	uptimeFields := strings.Fields(string(uptimeData))
	if len(uptimeFields) < 1 {
		return
	}
	systemUptimeSec, err := strconv.ParseFloat(uptimeFields[0], 64)
	if err != nil {
		return
	}

	// Время работы процесса = uptime системы - (starttime / CLK_TCK)
	processStartSec := float64(starttime) / float64(clkTck)
	processUptimeSec := systemUptimeSec - processStartSec
	if processUptimeSec < 0 {
		processUptimeSec = 0
	}

	uptimeStr = formatDuration(int64(processUptimeSec))

	// Нагрузка на CPU = (utime + stime) / CLK_TCK / processUptime * 100
	totalCPUTime := float64(utime+stime) / float64(clkTck)
	if processUptimeSec > 0 {
		cpu := (totalCPUTime / processUptimeSec) * 100
		cpuPercent = fmt.Sprintf("%.1f%%", cpu)
	}

	return cpuPercent, uptimeStr
}

// parseClkTckFromAuxv извлекает значение CLK_TCK (AT_CLKTCK) из /proc/self/auxv
func parseClkTckFromAuxv(data []byte) uint64 {
	const atClkTck = 17 // AT_CLKTCK
	wordSize := 8       // 64-bit

	for i := 0; i+2*wordSize <= len(data); i += 2 * wordSize {
		// Читает тип (первые 8 байт) и значение (вторые 8 байт) в little-endian
		aType := uint64(0)
		aVal := uint64(0)
		for b := 0; b < wordSize; b++ {
			aType |= uint64(data[i+b]) << (uint(b) * 8)
			aVal |= uint64(data[i+wordSize+b]) << (uint(b) * 8)
		}

		if aType == atClkTck {
			return aVal
		}
		if aType == 0 { // AT_NULL — конец таблицы
			break
		}
	}

	return 100 // Значение по умолчанию для Linux
}
