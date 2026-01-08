// Copyright (c) 2025-2026 Otto
// Лицензия: MIT (см. LICENSE)

package update

// Текущая версия FiReMQ в формате: "дд.мм.гг"
const CurrentVersion = "08.01.26"

// Имя исполняемого файла FiReMQ
const exeName = "FiReMQ"

// Полное название утилиты для обновления
const updaterName = "ServerUpdater"

// Формат временной метки для имени файла бэкапа: "дд.мм.гг(в_ЧЧ.ММ.СС)"
const backupTimestampLayout = "02.01.06(в_15.04.05)"

// Шаблон для поиска релиза в репозитории (формат: FiReMQ-<дд.мм.гг>-linux-amd64.tar.gz)
const assetPattern = `^FiReMQ-([0-9]{2}\.[0-9]{2}\.[0-9]{2})-linux-amd64\.tar\.gz$`

// Список путей, исключаемых из бэкапа при обновлении FiReMQ
var ExcludedBackupKeys = []string{
	"Path_Backup", // Директория с самими бэкапами
	"Path_Info",   // Директория с файлами об информации о компьютерах
}
