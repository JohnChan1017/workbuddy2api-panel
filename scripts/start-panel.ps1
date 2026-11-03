$ErrorActionPreference = 'Stop'
$projectDir = Split-Path -Parent $PSScriptRoot
$configPath = Join-Path $projectDir 'config.json'
$executable = Join-Path $projectDir 'wb2api.exe'

function Get-PanelBaseUrl {
    $listen = '127.0.0.1:7863'
    if (Test-Path -LiteralPath $configPath) {
        try {
            $config = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 | ConvertFrom-Json
            if ($config.listen) { $listen = [string]$config.listen }
        } catch {
            throw 'Cannot read config.json. Please check that it contains valid JSON.'
        }
    }
    if ($listen -notmatch '^(?:(?<host>[^:]*|\[[^\]]+\])):(?<port>\d+)$') {
        throw 'Invalid listen address in config.json. Expected host:port.'
    }
    $listenHost = $Matches['host'].Trim('[', ']')
    $listenPort = [int]$Matches['port']
    if ($listenPort -lt 1 -or $listenPort -gt 65535) {
        throw 'The configured port must be between 1 and 65535.'
    }
    if ($listenHost -in @('', '0.0.0.0', '::')) { $listenHost = '127.0.0.1' }
    $uri = New-Object System.UriBuilder('http', $listenHost, $listenPort)
    return $uri.Uri.AbsoluteUri.TrimEnd('/')
}

function Test-PanelReady([string]$baseUrl) {
    $response = $null
    try {
        $request = [System.Net.HttpWebRequest]::Create("$baseUrl/healthz")
        $request.Proxy = $null
        $request.AllowAutoRedirect = $false
        $request.Timeout = 1000
        $request.ReadWriteTimeout = 1000
        try {
            $response = $request.GetResponse()
        } catch [System.Net.WebException] {
            $response = $_.Exception.Response
        }
        if ($null -eq $response) { return $false }
        # A 503 with this identity also means the panel is ready (e.g. no accounts).
        return $response.Headers['X-Service'] -eq 'workbuddy2api'
    } catch {
        return $false
    } finally {
        if ($null -ne $response) { $response.Close() }
    }
}

$launcherMutex = $null
$hasLock = $false
try {
    $baseUrl = Get-PanelBaseUrl
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        $pathHash = [BitConverter]::ToString($sha.ComputeHash(
            [Text.Encoding]::UTF8.GetBytes($projectDir.ToLowerInvariant()))) -replace '-', ''
    } finally { $sha.Dispose() }
    $launcherMutex = New-Object System.Threading.Mutex($false, "Local\WorkBuddyPanel-$pathHash")
    try { $hasLock = $launcherMutex.WaitOne(45000) }
    catch [System.Threading.AbandonedMutexException] { $hasLock = $true }
    if (-not $hasLock) { throw 'Another launcher is still starting the service. Please try again shortly.' }

    if (-not (Test-PanelReady $baseUrl)) {
        if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) {
            throw 'wb2api.exe is missing. Please place the Windows executable in the project folder.'
        }
        $logDir = Join-Path $projectDir 'data'
        [IO.Directory]::CreateDirectory($logDir) | Out-Null
        $stdoutPath = Join-Path $logDir 'launcher.stdout.log'
        $stderrPath = Join-Path $logDir 'launcher.stderr.log'
        $serviceProcess = Start-Process -FilePath $executable -ArgumentList '-config config.json' `
            -WorkingDirectory $projectDir -WindowStyle Hidden -PassThru `
            -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
        $timer = [Diagnostics.Stopwatch]::StartNew()
        while (-not (Test-PanelReady $baseUrl)) {
            if ($serviceProcess.HasExited) {
                throw "Service exited before startup. See $stderrPath"
            }
            if ($timer.Elapsed.TotalSeconds -ge 30) {
                throw "Service is not ready after 30 seconds. See $stderrPath"
            }
            Start-Sleep -Milliseconds 300
        }
    }

    $panelUrl = "$baseUrl/panel/#accounts"
    Start-Process -FilePath $panelUrl
    Write-Host "Management panel: $panelUrl"
    exit 0
} catch {
    Write-Host "Startup failed: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
} finally {
    if ($hasLock) { $launcherMutex.ReleaseMutex() }
    if ($null -ne $launcherMutex) { $launcherMutex.Dispose() }
}
