// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

package update

// Текущая версия FiReMQ в формате: "дд.мм.гг"
const CurrentVersion = "01.11.25"

// Формат версии для time.Parse ("дд.мм.гг")
const versionLayout = "02.01.06"

// Формат временной метки для имени файла бэкапа: "дд.мм.гг(в_ЧЧ.ММ.СС)"
const backupTimestampLayout = "02.01.06(в_15.04.05)"

// Список путей, исключаемых из бэкапа при обновлении FiReMQ
var ExcludedBackupKeys = []string{
	"Path_Backup", // Директория с самими бэкапами
	"Path_Info",   // Директория с файлами об информации о компьютерах
}
