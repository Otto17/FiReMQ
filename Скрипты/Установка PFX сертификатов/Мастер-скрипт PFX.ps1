# Copyright (c) 2025-2026 Otto
# Лицензия: MIT (см. LICENSE)


# Мастер-скрипт PFX — создаёт инсталляционный скрипт для установки PFX сертификатов через FiReMQ, с помощью PowerShell.

<#
  Инструкция:
  1) Поместить PFX сертификат (название может быть любым) рядом со скриптом "Мастер-скрипт PFX.ps1" (PFX сертификатов может быть любое количество в папке, скрипт сам спросит какой из них выбрать), через правый клик мыши на скрипте выбрать "Выполнить с помощью PowerShell" и следовать интерактивным подсказкам, в конце создастся новый скрипт "Install-*.ps1" (к примеру: Install-Тест.ps1).

  2) Через правый клик на скрипте "Install-Тест.ps1" выбрать "Изменить" (откроется в редакторе Windows PowerShell ISE), либо открыть в Notepad++, выделить всё и скопировать (Ctrl+A / Ctrl+C).

  3.1) В FiReMQ выделить нужных клиентов, кликнуть на кнопку "Выполнить cmd / PowerShell", выбрать "Тип терминала: PowerShell", в поле "Команда:" вставить ранее скопированный скрипт, указать имя пользователя и пароль и нажать "Отправить".

  3.2) ВАЖНО, если установка производится в хранилище для текущего пользователя (CurrentUser) и у данного пользователя ОГРАНИЧЕННЫЕ ПРАВА и/или выбрана установка в "Доверенные корневые центры сертификации (Root)", тогда в FiReMQ после указания имени пользователя и пароля пользователю, нужно обязательно выбрать "Выполнять только для пользователей, вошедших в систему", при этом пользователь должен быть авторизован - это ограничения самой Windows!

  4) Всё, можно наблюдать за результатом в "Отчёт -> По cmd / PowerShell".
#>


# Любая ошибка сразу остановит скрипт
$ErrorActionPreference = 'Stop'

function Get-ScriptDirectory {
    if ($PSScriptRoot) { return $PSScriptRoot }
    return Split-Path -Parent $MyInvocation.MyCommand.Path
}

function Read-Choice {
    param(
        [string]$Title,
        [string[]]$Options
    )

    while ($true) {
        Write-Host ""
        Write-Host $Title
        for ($i = 0; $i -lt $Options.Count; $i++) {
            Write-Host ("  {0}. {1}" -f ($i + 1), $Options[$i])
        }

        $raw = Read-Host "Выберите номер"
        $n = 0
        if ([int]::TryParse($raw, [ref]$n)) {
            if ($n -ge 1 -and $n -le $Options.Count) {
                return $n
            }
        }

        Write-Host "Неверный выбор, попробуйте ещё раз." -ForegroundColor Yellow
    }
}

function Ask-YesNo {
    param(
        [string]$Question,
        [bool]$DefaultYes = $true
    )

    $suffix = if ($DefaultYes) { " [Д/н]:" } else { " [д/Н]:" }

    while ($true) {
        $answer = (Read-Host ($Question + $suffix)).Trim().ToLowerInvariant()

        if ([string]::IsNullOrWhiteSpace($answer)) {
            return $DefaultYes
        }

        if ($answer -in @('y', 'yes', 'д', 'да')) { return $true }
        if ($answer -in @('n', 'no', 'н', 'нет')) { return $false }

        Write-Host "Введите Д или Н." -ForegroundColor Yellow
    }
}

function Get-PfxFiles {
    $dir = Get-ScriptDirectory
    @(Get-ChildItem -Path $dir -Filter *.pfx -File | Sort-Object Name)
}

function Get-PfxCertInfo {
    param(
        [byte[]]$Bytes,
        [string]$Password
    )

    $flags = [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::Exportable
    $col = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2Collection

    if ([string]::IsNullOrEmpty($Password)) {
        $col.Import($Bytes, '', $flags)
    }
    else {
        $col.Import($Bytes, $Password, $flags)
    }

    if ($col.Count -eq 0) {
        throw "PFX не содержит сертификатов."
    }

    # Поиск основного сертификата (с приватным ключом)
    $mainCert = $null
    $chainCerts = @()

    foreach ($c in $col) {
        if ($c.HasPrivateKey -and $null -eq $mainCert) {
            $mainCert = $c
        }
        else {
            $chainCerts += $c
        }
    }

    if ($null -eq $mainCert) {
        $mainCert = $col[0]
        $chainCerts = @()
        for ($i = 1; $i -lt $col.Count; $i++) { $chainCerts += $col[$i] }
    }

    # Анализ основного сертификата
    $basic = $null
    foreach ($ext in $mainCert.Extensions) {
        if ($ext -is [System.Security.Cryptography.X509Certificates.X509BasicConstraintsExtension]) {
            $basic = $ext; break
        }
    }

    $isCA = [bool]($basic -and $basic.CertificateAuthority)
    $isSelf = ($mainCert.Subject -eq $mainCert.Issuer)

    $mainAutoStore = if ($isCA -and $isSelf) { 'Root' }
                     elseif ($isCA)          { 'CA' }
                     else                    { 'My' }

    # Анализ цепочки
    $chainInfo = @()
    foreach ($c in $chainCerts) {
        $cb = $null
        foreach ($ext in $c.Extensions) {
            if ($ext -is [System.Security.Cryptography.X509Certificates.X509BasicConstraintsExtension]) {
                $cb = $ext; break
            }
        }

        $cIsCA = [bool]($cb -and $cb.CertificateAuthority)
        $cIsSelf = ($c.Subject -eq $c.Issuer)

        $autoStore = if ($cIsCA -and $cIsSelf) { 'Root' }
                     elseif ($cIsCA)            { 'CA' }
                     else                       { 'My' }

        $chainInfo += [pscustomobject]@{
            Subject   = $c.Subject
            AutoStore = $autoStore
        }
    }

    [pscustomobject]@{
        Subject      = $mainCert.Subject
        Issuer       = $mainCert.Issuer
        IsSelfSigned = $isSelf
        IsCA         = $isCA
        AutoStore    = $mainAutoStore
        ChainCerts   = $chainInfo
        TotalCount   = $col.Count
    }
}

function Escape-SingleQuotes {
    param([string]$Text)
    return $Text -replace "'", "''"
}

function New-InstallerScript {
    param(
        [string]$OutputPath,
        [string]$PfxB64,
        [string]$PasswordB64,
        [string]$Scope,
        [string]$StoreName,
        [bool]$Exportable,
        [bool]$InstallChainCerts,
        [bool]$AutoDetectStores
    )

    # C# код для автоподтверждения диалогов безопасности при установке в Root
    $csharpAutoConfirm = @'
using System;
using System.Runtime.InteropServices;
using System.Text;
using System.Threading;

public class DialogAutoConfirm
{
    [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern IntPtr FindWindow(string lpClassName, string lpWindowName);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    private static extern int GetWindowText(IntPtr hWnd, StringBuilder lpString, int nMaxCount);

    [DllImport("user32.dll")]
    private static extern bool EnumChildWindows(IntPtr hWndParent, EnumWindowsProc lpEnumFunc, IntPtr lParam);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    private static extern int GetClassName(IntPtr hWnd, StringBuilder lpClassName, int nMaxCount);

    [DllImport("user32.dll")]
    private static extern IntPtr SendMessage(IntPtr hWnd, uint Msg, IntPtr wParam, IntPtr lParam);

    [DllImport("user32.dll")]
    private static extern bool IsWindowVisible(IntPtr hWnd);

    [DllImport("user32.dll")]
    private static extern uint GetWindowThreadProcessId(IntPtr hWnd, out uint lpdwProcessId);

    [DllImport("kernel32.dll")]
    private static extern uint GetCurrentProcessId();

    [DllImport("kernel32.dll")]
    private static extern IntPtr GetConsoleWindow();

    [DllImport("user32.dll")]
    private static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);

    public static void HideConsole()
    {
        IntPtr hWnd = GetConsoleWindow();
        if (hWnd != IntPtr.Zero)
            ShowWindow(hWnd, 0);
    }

    public delegate bool EnumWindowsProc(IntPtr hWnd, IntPtr lParam);

    private const uint BM_CLICK = 0x00F5;
    private static Timer _timer;
    private static int _confirmed;
    private static readonly uint _pid = GetCurrentProcessId();

    public static int Confirmed { get { return _confirmed; } }

    // Заголовки окна: RU и EN локали
    private static readonly string[] _titles = new string[] {
        "\u041F\u0440\u0435\u0434\u0443\u043F\u0440\u0435\u0436\u0434\u0435\u043D\u0438\u0435 \u0441\u0438\u0441\u0442\u0435\u043C\u044B \u0431\u0435\u0437\u043E\u043F\u0430\u0441\u043D\u043E\u0441\u0442\u0438",
        "Security Warning"
    };

    // Текст кнопки "Да" / "Yes" (без &-акселератора)
    private static readonly string[] _yesTexts = new string[] { "\u0414\u0430", "Yes" };

    public static void StartMonitoring(int intervalMs)
    {
        _confirmed = 0;
        _timer = new Timer(delegate(object o) {
            try { if (TryConfirm()) Interlocked.Increment(ref _confirmed); } catch { }
        }, null, 0, intervalMs);
    }

    public static void StopMonitoring()
    {
        Timer t = _timer;
        _timer = null;
        if (t != null) t.Dispose();
    }

    public static bool TryConfirm()
    {
        foreach (string title in _titles)
        {
            IntPtr hWnd = FindWindow(null, title);
            if (hWnd == IntPtr.Zero || !IsWindowVisible(hWnd))
                continue;

            // Проверяем, что окно принадлежит нашему процессу
            uint wPid;
            GetWindowThreadProcessId(hWnd, out wPid);
            if (wPid != _pid)
                continue;

            IntPtr found = IntPtr.Zero;
            EnumWindowsProc cb = delegate(IntPtr child, IntPtr lp) {
                StringBuilder cn = new StringBuilder(256);
                GetClassName(child, cn, 256);
                if (cn.ToString() != "Button")
                    return true;

                StringBuilder bt = new StringBuilder(256);
                GetWindowText(child, bt, 256);
                string btnText = bt.ToString().Replace("&", "");

                foreach (string y in _yesTexts)
                {
                    if (string.Equals(btnText, y, StringComparison.OrdinalIgnoreCase))
                    {
                        found = child;
                        return false;
                    }
                }
                return true;
            };

            EnumChildWindows(hWnd, cb, IntPtr.Zero);
            GC.KeepAlive(cb);

            if (found != IntPtr.Zero)
            {
                SendMessage(found, BM_CLICK, IntPtr.Zero, IntPtr.Zero);
                return true;
            }
        }
        return false;
    }
}
'@

    $autoConfirmB64 = [Convert]::ToBase64String(
        [System.Text.Encoding]::UTF8.GetBytes($csharpAutoConfirm)
    )

    $template = @'
$ErrorActionPreference = 'Stop'

$pfxTextB64 = '__PFX_B64__'
$passwordB64 = '__PASSWORD_B64__'
$scope = '__SCOPE__'
$storeName = '__STORENAME__'
$exportable = __EXPORTABLE__
$installChainCerts = __CHAIN__
$autoDetectStores = __AUTODETECT__
$autoConfirmB64 = '__AUTOCONFIRM_B64__'

# Загрузка модуля автоподтверждения диалогов безопасности
$acLoaded = $false
try { $null = [DialogAutoConfirm]; $acLoaded = $true } catch { }
if (-not $acLoaded) {
    try {
        $acCode = [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($autoConfirmB64))
        Add-Type -TypeDefinition $acCode
        $acLoaded = $true
    }
    catch {
        Write-Host ("Предупреждение: автоподтверждение недоступно: {0}" -f $_.Exception.Message) -ForegroundColor Yellow
    }
}

if ($acLoaded) {
    [DialogAutoConfirm]::HideConsole()
}

function Decode-Base64Utf8 {
    param([string]$TextB64)
    if ([string]::IsNullOrWhiteSpace($TextB64)) { return '' }
    return [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($TextB64))
}

function Decode-PfxBytes {
    param([string]$Text)
    $clean = ($Text -replace '\s', '').Trim()
    if ($clean.StartsWith('B64:')) { $clean = $clean.Substring(4) }
    return [Convert]::FromBase64String($clean)
}

function Get-AutoStoreName {
    param([System.Security.Cryptography.X509Certificates.X509Certificate2]$Certificate)

    $bce = $null
    foreach ($e in $Certificate.Extensions) {
        if ($e -is [System.Security.Cryptography.X509Certificates.X509BasicConstraintsExtension]) {
            $bce = $e; break
        }
    }

    $isCA   = [bool]($bce -and $bce.CertificateAuthority)
    $isSelf = ($Certificate.Subject -eq $Certificate.Issuer)

    if ($isCA -and $isSelf) { return 'Root' }
    elseif ($isCA)          { return 'CA' }
    else                    { return 'My' }
}

function Add-CertToStore {
    param(
        [System.Security.Cryptography.X509Certificates.X509Certificate2]$Certificate,
        [string]$TargetScope,
        [string]$TargetStore
    )

    $s = New-Object System.Security.Cryptography.X509Certificates.X509Store(
        $TargetStore,
        [System.Security.Cryptography.X509Certificates.StoreLocation]::$TargetScope
    )
    $s.Open([System.Security.Cryptography.X509Certificates.OpenFlags]::ReadWrite)
    $s.Add($Certificate)
    $s.Close()
}

$bytes = Decode-PfxBytes -Text $pfxTextB64
$tmpPfx = Join-Path $env:TEMP ("pfx_{0}.pfx" -f ([guid]::NewGuid().ToString('N')))
[System.IO.File]::WriteAllBytes($tmpPfx, $bytes)

$needAutoConfirm = $false

try {
    $password = Decode-Base64Utf8 -TextB64 $passwordB64

    $importFlags = [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::PersistKeySet

    if ($exportable) {
        $importFlags = $importFlags -bor [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::Exportable
    }

    if ($scope -eq 'LocalMachine') {
        $importFlags = $importFlags -bor [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::MachineKeySet
    }

    $pfxCol = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2Collection

    if ([string]::IsNullOrEmpty($password)) {
        $pfxCol.Import($tmpPfx, '', $importFlags)
    }
    else {
        $pfxCol.Import($tmpPfx, $password, $importFlags)
    }

    # Поиск основного сертификата
    $mainCert = $null
    foreach ($c in $pfxCol) {
        if ($c.HasPrivateKey) { $mainCert = $c; break }
    }
    if ($null -eq $mainCert -and $pfxCol.Count -gt 0) { $mainCert = $pfxCol[0] }

    if ($autoDetectStores) {
        $mainTargetStore = Get-AutoStoreName -Certificate $mainCert
    }
    else {
        $mainTargetStore = $storeName
    }

    # Определение необходимости автоподтверждения
    if ($acLoaded) {
        if ($mainTargetStore -eq 'Root') { $needAutoConfirm = $true }

        if ($installChainCerts -and (-not $needAutoConfirm)) {
            foreach ($c in $pfxCol) {
                if ($c.Thumbprint -eq $mainCert.Thumbprint) { continue }
                if ($autoDetectStores) {
                    $ts = Get-AutoStoreName -Certificate $c
                }
                else {
                    $ts = $storeName
                }
                if ($ts -eq 'Root') { $needAutoConfirm = $true; break }
            }
        }
    }

    if ($needAutoConfirm) {
        [DialogAutoConfirm]::StartMonitoring(200)
    }

    # Установка основного сертификата
    Add-CertToStore -Certificate $mainCert -TargetScope $scope -TargetStore $mainTargetStore
    Write-Host ("Установлен: {0} -> Cert:\{1}\{2}" -f $mainCert.Subject, $scope, $mainTargetStore) -ForegroundColor Green

    if ($mainCert.HasPrivateKey -and $mainTargetStore -eq 'Root') {
        Write-Host "  Примечание: приватный ключ не сохраняется в Root." -ForegroundColor Yellow
    }

    # Установка цепочки
    if ($installChainCerts) {
        foreach ($c in $pfxCol) {
            if ($c.Thumbprint -eq $mainCert.Thumbprint) { continue }

            if ($autoDetectStores) {
                $cTargetStore = Get-AutoStoreName -Certificate $c
            }
            else {
                $cTargetStore = $storeName
            }

            try {
                Add-CertToStore -Certificate $c -TargetScope $scope -TargetStore $cTargetStore
                Write-Host ("Установлен: {0} -> Cert:\{1}\{2}" -f $c.Subject, $scope, $cTargetStore) -ForegroundColor Green
            }
            catch {
                Write-Host ("Ошибка: {0} -> {1}" -f $c.Subject, $_.Exception.Message) -ForegroundColor Red
            }
        }
    }

    # Остановка автоподтверждения и отчёт
    if ($needAutoConfirm) {
        Start-Sleep -Milliseconds 500
        [DialogAutoConfirm]::StopMonitoring()
        $acCount = [DialogAutoConfirm]::Confirmed
        if ($acCount -gt 0) {
            Write-Host ("Автоподтверждено диалогов безопасности: {0}" -f $acCount) -ForegroundColor DarkYellow
        }
    }
}
finally {
    Remove-Item $tmpPfx -Force -ErrorAction SilentlyContinue
    try { [DialogAutoConfirm]::StopMonitoring() } catch { }
}
'@

    $script = $template.
        Replace('__PFX_B64__',         (Escape-SingleQuotes $PfxB64)).
        Replace('__PASSWORD_B64__',    (Escape-SingleQuotes $PasswordB64)).
        Replace('__SCOPE__',           (Escape-SingleQuotes $Scope)).
        Replace('__STORENAME__',       (Escape-SingleQuotes $StoreName)).
        Replace('__EXPORTABLE__',      ($(if ($Exportable) { '$true' } else { '$false' }))).
        Replace('__CHAIN__',           ($(if ($InstallChainCerts) { '$true' } else { '$false' }))).
        Replace('__AUTODETECT__',      ($(if ($AutoDetectStores) { '$true' } else { '$false' }))).
        Replace('__AUTOCONFIRM_B64__', $autoConfirmB64)

    $enc1251 = [System.Text.Encoding]::GetEncoding(1251)
    [System.IO.File]::WriteAllText($OutputPath, $script, $enc1251)
}

function Get-SafeFileName {
    param([string]$Name)
    $invalid = [System.IO.Path]::GetInvalidFileNameChars()
    $safe = $Name
    foreach ($ch in $invalid) {
        $safe = $safe.Replace($ch, '_')
    }
    return $safe
}

# ════════════════════════════════════════════════════════
# ОСНОВНОЙ БЛОК
# ════════════════════════════════════════════════════════
try {
    $pfxFiles = Get-PfxFiles
    if (@($pfxFiles).Count -eq 0) {
        throw "В папке со скриптом не найдено ни одного *.pfx"
    }

    if (@($pfxFiles).Count -eq 1) {
        $selected = $pfxFiles[0]
        Write-Host "Выбран: $($selected.Name)"
    }
    else {
        Write-Host "Найдено несколько PFX:"
        for ($i = 0; $i -lt $pfxFiles.Count; $i++) {
            Write-Host ("  {0}. {1}" -f ($i + 1), $pfxFiles[$i].Name)
        }

        while ($true) {
            $raw = Read-Host "Выберите номер PFX"
            $n = 0
            if ([int]::TryParse($raw, [ref]$n)) {
                if ($n -ge 1 -and $n -le $pfxFiles.Count) {
                    $selected = $pfxFiles[$n - 1]
                    break
                }
            }
            Write-Host "Неверный выбор." -ForegroundColor Yellow
        }
    }

    $pfxBytes = [System.IO.File]::ReadAllBytes($selected.FullName)

    while ($true) {
        $password = Read-Host "Введите пароль PFX (можно оставить пустым)"
        try {
            $certInfo = Get-PfxCertInfo -Bytes $pfxBytes -Password $password
            break
        }
        catch {
            Write-Host "Неверный пароль или файл PFX повреждён. Попробуйте ещё раз." -ForegroundColor Yellow
        }
    }

    # Вывод содержимого PFX
    Write-Host ""
    Write-Host ("PFX содержит {0} сертификат(ов):" -f $certInfo.TotalCount) -ForegroundColor Cyan
    Write-Host ("  Основной: {0} (авто -> {1})" -f $certInfo.Subject, $certInfo.AutoStore)
    foreach ($cc in $certInfo.ChainCerts) {
        Write-Host ("  Цепочка:  {0} (авто -> {1})" -f $cc.Subject, $cc.AutoStore)
    }

    $scopeChoice = Read-Choice `
        -Title "Куда устанавливать сертификат?" `
        -Options @(
            'Текущий пользователь (CurrentUser)',
            'Локальный компьютер (LocalMachine)'
        )

    $scope = if ($scopeChoice -eq 1) { 'CurrentUser' } else { 'LocalMachine' }

    $exportable = Ask-YesNo -Question "Пометить как экспортируемый?" -DefaultYes $false

    $storeChoice = Read-Choice `
        -Title "Куда именно установить сертификат(ы)?" `
        -Options @(
            'Автоматический выбор хранилища на основе типа сертификата',
            'Личное (My)',
            'Доверенные корневые центры сертификации (Root)',
            'Промежуточные центры сертификации (CA)',
            'Доверенные издатели (TrustedPublisher)',
            'Недоверенные (Disallowed)',
            'WebHosting',
            'Указать своё хранилище вручную'
        )

    $autoDetectStores = $false

    switch ($storeChoice) {
        1 {
            $autoDetectStores = $true
            $storeName = 'Auto'
            Write-Host "Режим: автоматический выбор хранилища для каждого сертификата" -ForegroundColor Cyan
        }
        2 { $storeName = 'My' }
        3 { $storeName = 'Root' }
        4 { $storeName = 'CA' }
        5 { $storeName = 'TrustedPublisher' }
        6 { $storeName = 'Disallowed' }
        7 { $storeName = 'WebHosting' }
        8 {
            $storeName = Read-Host "Введите имя хранилища (например My, Root, CA, TrustedPublisher)"
            if ([string]::IsNullOrWhiteSpace($storeName)) {
                throw "Имя хранилища не задано."
            }
        }
    }

    # Вопрос о цепочке
    $installChainCerts = $false
    if ($certInfo.ChainCerts.Count -gt 0) {
        $installChainCerts = Ask-YesNo `
            -Question ("PFX содержит {0} доп. сертификат(ов) цепочки. Установить их тоже?" -f $certInfo.ChainCerts.Count) `
            -DefaultYes $true
    }

    # Определение автоподтверждения
    $willUseAutoConfirm = $false
    if ($autoDetectStores) {
        if ($certInfo.AutoStore -eq 'Root') { $willUseAutoConfirm = $true }
        if ($installChainCerts -and (-not $willUseAutoConfirm)) {
            foreach ($cc in $certInfo.ChainCerts) {
                if ($cc.AutoStore -eq 'Root') { $willUseAutoConfirm = $true; break }
            }
        }
    }
    elseif ($storeName -eq 'Root') {
        $willUseAutoConfirm = $true
    }

    # Сводка
    Write-Host ""
    Write-Host "Проверка настроек:" -ForegroundColor Cyan
    Write-Host ("  PFX:            {0}" -f $selected.Name)
    Write-Host ("  Субъект:        {0}" -f $certInfo.Subject)
    Write-Host ("  Расположение:   {0}" -f $scope)

    if ($autoDetectStores) {
        Write-Host "  Хранилище:      Автоматический выбор"
        Write-Host ("    Основной -> {0}\{1}" -f $scope, $certInfo.AutoStore)
        if ($installChainCerts) {
            foreach ($cc in $certInfo.ChainCerts) {
                Write-Host ("    Цепочка  -> {0}\{1}" -f $scope, $cc.AutoStore)
            }
        }
    }
    else {
        Write-Host ("  Хранилище:      {0}" -f $storeName)
    }

    Write-Host ("  Экспортируемый: {0}" -f ($(if ($exportable) { 'Да' } else { 'Нет' })))
    Write-Host ("  Пароль:         {0}" -f ($(if ([string]::IsNullOrEmpty($password)) { '<пустой>' } else { '<задан>' })))

    if ($certInfo.ChainCerts.Count -gt 0) {
        Write-Host ("  Цепочка:        {0}" -f ($(if ($installChainCerts) { 'Да' } else { 'Нет' })))
    }

    if ($willUseAutoConfirm) {
        Write-Host "  Автоподтверждение: Да (установка в Root)" -ForegroundColor DarkYellow
    }

    if (Ask-YesNo -Question "Создать инсталляционный .ps1 файл?" -DefaultYes $true) {
        $baseName = [System.IO.Path]::GetFileNameWithoutExtension($selected.Name)
        $safeBaseName = Get-SafeFileName -Name $baseName
        $installerName = "Install-$safeBaseName.ps1"
        $installerPath = Join-Path $selected.DirectoryName $installerName

        $pfxB64 = 'B64:' + [Convert]::ToBase64String($pfxBytes)
        $passwordB64 = [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($password))

        New-InstallerScript `
            -OutputPath        $installerPath `
            -PfxB64            $pfxB64 `
            -PasswordB64       $passwordB64 `
            -Scope             $scope `
            -StoreName         $storeName `
            -Exportable:       $exportable `
            -InstallChainCerts:$installChainCerts `
            -AutoDetectStores: $autoDetectStores

        Write-Host ""
        Write-Host ("Готово: {0}" -f $installerPath) -ForegroundColor Green
    }
    else {
        Write-Host "Отменено."
    }
}
catch {
    Write-Host ""
    Write-Host ("Ошибка: {0}" -f $_.Exception.Message) -ForegroundColor Red
}
finally {
    [void](Read-Host "Нажмите Enter для выхода")
}