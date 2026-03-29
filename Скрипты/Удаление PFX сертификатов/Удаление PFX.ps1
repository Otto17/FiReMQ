# Copyright (c) 2025-2026 Otto
# Лицензия: MIT (см. LICENSE)


# Скрипт для массового удаления PFX сертификатов из хранилища Windows через FiReMQ, с помощью PowerShell.

<#
  Инструкция:
  1) Через правый клик на скрипте "Install-Тест.ps1" выбрать "Изменить" (откроется в редакторе Windows PowerShell ISE), либо открыть в Notepad++, сверху в пункте "===== НАСТРОЙКИ =====" внести нужные правки для удаляемых сертификатов, выделить всё и скопировать (Ctrl+A / Ctrl+C).

  2) В FiReMQ выделить нужных клиентов, кликнуть на кнопку "Выполнить cmd / PowerShell", выбрать "Тип терминала: PowerShell", в поле "Команда:" вставить ранее скопированный скрипт и нажать "Отправить".

  3) Всё, можно наблюдать за результатом в "Отчёт -> По cmd / PowerShell".
#>


# ===== НАСТРОЙКИ =====

# Где искать сертификаты:
# CurrentUser  = текущий пользователь
# LocalMachine = весь компьютер (нужны права администратора)
# Можно использования сразу два хранилища: @('CurrentUser','LocalMachine')
$StoreLocations = @('CurrentUser')

# В каких хранилищах искать:
# My                = Личные
# Root              = Доверенные корневые центры сертификации
# Trust             = Доверительные отношения в предприятии
# CA                = Промежуточные центры сертификации
# TrustedPublisher  = Доверенные издатели
# AuthRoot          = Сторонние корневые центры сертификации
# TrustedPeople     = Доверенные лица
# AddressBook       = Другие пользователи
# Disallowed        = Запрещённые
$StoreNames = @('My')

# Искомые значения (любое количество):
# CN=Имя            → Поиск по Common Name (без CN=), пример: SUB-CA02
# HEX               → Поиск по серийному номеру (без пробелов или с пробелами), пример: 21db00cfe6c0cf36be6afc95dbe6c30000cfe7 или 21 db 00 cf e6 c0 cf 36 be 6a fc 95 db e6 c3 00 00 cf e7
# Текст             → Поиск по части Subject, пример: Федеральная налоговая служба

# Разделители - запятая или новая строка.
# Gример в одну строку: Федеральная налоговая служба, SUB-CA01, 47d5dde5c3c1cfe6c8caec95dbe6c10075dbe6
# Пример в несколько строк (каждая предыдущая строка должна заканчиваться на ','):
#  $SearchText = @'
#  SUB-CA02, SUB-CA01,
#  21 db 00 cf e6 c0 cf 36 be 6a fc 95 db e6 c3 00 00 cf e7,
#  47d5dde5c3c1cfe6c8caec95dbe6c10075dbe6,
#  Федеральная налоговая служба
#  '@
$SearchText = @'
SUB-CA02
'@

# Опционально (true или false), если нужно очистить: кэш CRL (отзыв сертификатов), кэш OCSP, эеш загрузки сертификатов.
# Это может потребоваться в редких случаях, например: Windows может восстановить сертификат или кэш цепочки останется.
$ClearCache = $false

# Любая ошибка сразу остановит скрипт
$ErrorActionPreference = 'Stop'



# Обход GUI-диалога для CurrentUser\Root
function Remove-CertFromStore {
    param(
        [System.Security.Cryptography.X509Certificates.X509Store]$Store,
        [System.Security.Cryptography.X509Certificates.X509Certificate2]$Certificate,
        [string]$LocationName,
        [string]$StoreName
    )

    if ($LocationName -eq 'CurrentUser' -and $StoreName -eq 'Root') {
        # Обход GUI: удаляет напрямую из реестра
        $regPath = "HKCU:\SOFTWARE\Microsoft\SystemCertificates\Root\Certificates\$($Certificate.Thumbprint)"

        if (Test-Path -LiteralPath $regPath) {
            Remove-Item -LiteralPath $regPath -Recurse -Force
            Write-Verbose "Реестр: удалён ключ $regPath"
        }
        else {
            Write-Warning "Ключ реестра не найден: $regPath"
        }
    }
    else {
        # В остальных хранилищах - стандартное удаление через API
        $Store.Remove($Certificate)
    }
}

# Разбивает строку поиска на массив условий (по , и переносам строк)
function Get-SearchTerms {
    param([string]$Text)

    $Text -split '[,`r`n;]+' |
        ForEach-Object { $_.Trim() } |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
}

# Проверяет, подходит ли сертификат под условие: CN=..., серийник или часть Subject
function Test-CertificateMatch {
    param(
        [System.Security.Cryptography.X509Certificates.X509Certificate2]$Certificate,
        [string]$Term
    )

    $subject  = $Certificate.Subject
    $serial   = ($Certificate.SerialNumber -replace '\s', '').ToUpperInvariant()
    $termTrim = $Term.Trim()

    if ($termTrim -match '^(?i)CN\s*=') {
        $cn = ($termTrim -replace '^(?i)CN\s*=\s*', '').Trim()
        return ($subject -like "*CN=$cn*")
    }

    if ($termTrim -match '^[0-9A-Fa-f\s]+$') {
        $needle = ($termTrim -replace '\s', '').ToUpperInvariant()
        return ($serial -eq $needle)
    }

    return ($subject -like "*$termTrim*")
}

# Преобразует строку (CurrentUser/LocalMachine) в enum для .NET
function Get-StoreLocationEnum {
    param([string]$LocationName)

    try {
        return [System.Security.Cryptography.X509Certificates.StoreLocation]::$LocationName
    }
    catch {
        throw "Недопустимый StoreLocation: $LocationName"
    }
}

$terms = @(Get-SearchTerms -Text $SearchText)
if ($terms.Count -eq 0) {
    throw "Не задано ни одного условия поиска."
}

$results = New-Object System.Collections.Generic.List[object]

foreach ($locName in $StoreLocations) {
    $location = Get-StoreLocationEnum -LocationName $locName

    foreach ($storeName in $StoreNames) {
        $store = New-Object System.Security.Cryptography.X509Certificates.X509Store($storeName, $location)

        try {
            $store.Open([System.Security.Cryptography.X509Certificates.OpenFlags]::ReadWrite)

            $snapshot = @($store.Certificates)

            $matches = foreach ($cert in $snapshot) {
                foreach ($term in $terms) {
                    if (Test-CertificateMatch -Certificate $cert -Term $term) {
                        $cert
                        break
                    }
                }
            }

            $matches = @($matches | Sort-Object Thumbprint -Unique)

            foreach ($cert in $matches) {
                $row = [pscustomobject]@{
                    Location   = $locName
                    Store      = $storeName
                    Subject    = $cert.Subject
                    Serial     = $cert.SerialNumber
                    Thumbprint = $cert.Thumbprint
                    Action     = 'Removed'
                }

                Remove-CertFromStore -Store $store `
                -Certificate $cert `
                -LocationName $locName `
                -StoreName $storeName

                $results.Add($row)
            }
        }
        finally {
            $store.Close()
        }
    }
}

if ($results.Count -eq 0) {
    Write-Host "Совпадений не найдено."
}
else {
    $results | Format-Table -AutoSize
}

if ($ClearCache) {
    try {
        certutil -urlcache * delete | Out-Null
    }
    catch {
        Write-Warning "Не удалось очистить кэш сертификатов"
    }
}
