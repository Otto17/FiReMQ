// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

//go:build windows

package update

// Имя исполняемого файла FiReMQ под Windows
const exeName = "FiReMQ.exe"

// Полное название утилиты для обновления
const updaterName = "ServerUpdater.exe"

// Шаблон для поиска релиза в репозитории для Windows (формат: FiReMQ-<дд.мм.гг>-windows-amd64.zip)
const assetPattern = `^FiReMQ-([0-9]{2}\.[0-9]{2}\.[0-9]{2})-windows-amd64\.zip$`
