// Copyright (c) 2025 Otto
// Лицензия: MIT (см. LICENSE)

//go:build linux

package update

// Имя исполняемого файла FiReMQ под Linux
const exeName = "FiReMQ"

// Полное название утилиты для обновления
const updaterName = "ServerUpdater"

// Шаблон для поиска релиза в репозитории для Linux (формат: FiReMQ-<дд.мм.гг>-linux-amd64.tar.gz)
const assetPattern = `^FiReMQ-([0-9]{2}\.[0-9]{2}\.[0-9]{2})-linux-amd64\.tar\.gz$`
