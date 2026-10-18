# ==============================================================================
# Backend Startup Script (Windows PowerShell)
# Performs pre-flight checks, dependency downloads, .env verification,
# starts Redis, checks PostgreSQL, applies pending migrations ($env:SKIP_MIGRATE = "1" to skip),
# checks the rules engine and its rules_version, and runs: go run ./cmd/api
# ==============================================================================

[CmdletBinding()]
param()

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

Write-Host "=======================================================" -ForegroundColor Cyan
Write-Host "            BACKEND PRE-FLIGHT CHECKS                  " -ForegroundColor Cyan
Write-Host "=======================================================" -ForegroundColor Cyan

$FailedItems = @()
$WhatToInstall = @()
$CheckSummary = @()

# Helper function to test TCP port connectivity
function Test-TcpPort {
    param(
        [string]$ComputerName = "127.0.0.1",
        [int]$Port = 8080,
        [int]$TimeoutMs = 1000
    )
    try {
        $tcpClient = New-Object System.Net.Sockets.TcpClient
        $connectResult = $tcpClient.BeginConnect($ComputerName, $Port, $null, $null)
        $waitResult = $connectResult.AsyncWaitHandle.WaitOne($TimeoutMs, $false)
        if ($waitResult -and $tcpClient.Connected) {
            $tcpClient.EndConnect($connectResult)
            $tcpClient.Close()
            return $true
        }
        $tcpClient.Close()
        return $false
    } catch {
        return $false
    }
}

# ------------------------------------------------------------------------------
# 1. Check Go runtime
# ------------------------------------------------------------------------------
$GoCmd = Get-Command go -ErrorAction SilentlyContinue
if (-not $GoCmd) {
    $CheckSummary += @{ Status = "FAIL"; Message = "Go Runtime: Not found in PATH" }
    $FailedItems += "Go compiler is missing"
    $WhatToInstall += "Install Go 1.25+ (Run: winget install GoLang.Go or download from https://go.dev/dl/)"
} else {
    try {
        $GoVer = (go version).Split(' ')[2]
        if ($GoVer -match '^go1\.(\d+)' -and [int]$Matches[1] -lt 25) {
            $CheckSummary += @{ Status = "FAIL"; Message = "Go Runtime: $GoVer is too old (go.mod needs 1.25+)" }
            $FailedItems += "Go 1.25+ is required"
            $WhatToInstall += "Upgrade Go to 1.25+ (winget upgrade GoLang.Go or https://go.dev/dl/)"
        } else {
            $CheckSummary += @{ Status = "PASS"; Message = "Go Runtime: $GoVer" }
        }
    } catch {
        $CheckSummary += @{ Status = "PASS"; Message = "Go Runtime: Available" }
    }
}

# ------------------------------------------------------------------------------
# 2. Check .env configuration file
# ------------------------------------------------------------------------------
if (Test-Path ".env") {
    $CheckSummary += @{ Status = "PASS"; Message = "Environment File: backend/.env is present" }
} else {
    if (Test-Path ".env.example") {
        Copy-Item -Path ".env.example" -Destination ".env"
        $CheckSummary += @{ Status = "PASS"; Message = "Environment File: Created backend/.env from .env.example" }
    } else {
        $CheckSummary += @{ Status = "FAIL"; Message = "Environment File: Neither .env nor .env.example found" }
        $FailedItems += "Missing .env configuration"
        $WhatToInstall += "Create backend/.env with DATABASE_URL, REDIS_URL, PORT, etc."
    }
}

# Parse .env if present
$Port = 8080
$DatabaseUrl = ""
$RedisUrl = "redis://localhost:6379/0"
$RulesEngineUrl = "http://localhost:8000"
$AuthMode = "off"
$AutoMigrate = "false"
$WorkerEnabled = "true"
$RateLimitPerMinute = "600"

if (Test-Path ".env") {
    Get-Content ".env" | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith("#")) {
            $parts = $line -split "=", 2
            if ($parts.Length -eq 2) {
                $key = $parts[0].Trim()
                $val = $parts[1].Trim()
                switch ($key) {
                    "PORT" { $Port = [int]$val }
                    "DATABASE_URL" { $DatabaseUrl = $val }
                    "REDIS_URL" { $RedisUrl = $val }
                    "RULES_ENGINE_URL" { $RulesEngineUrl = $val }
                    "AUTH_MODE" { $AuthMode = $val.ToLower() }
                    "AUTO_MIGRATE" { $AutoMigrate = $val }
                    "WORKER_ENABLED" { $WorkerEnabled = $val }
                    "RATE_LIMIT_PER_MINUTE" { $RateLimitPerMinute = $val }
                }
            }
        }
    }

    if ($DatabaseUrl -like "*CHANGE_ME*") {
        $CheckSummary += @{ Status = "WARN"; Message = "Database Config: DATABASE_URL contains placeholder 'CHANGE_ME'. Ensure your database user is configured." }
    }
}

# ------------------------------------------------------------------------------
# 3. Check & Download Go dependencies
# ------------------------------------------------------------------------------
if ($GoCmd) {
    if (Test-Path "go.mod") {
        Write-Host "[INFO] Verifying and downloading Go modules ..." -ForegroundColor Blue
        $modOutput = go mod download 2>&1
        if ($LASTEXITCODE -eq 0) {
            $CheckSummary += @{ Status = "PASS"; Message = "Dependencies: Go modules verified and downloaded" }
        } else {
            $CheckSummary += @{ Status = "FAIL"; Message = "Dependencies: go mod download failed" }
            $FailedItems += "Go modules download failed"
            $WhatToInstall += "Run: go mod download (verify network or proxy settings)"
        }
    } else {
        $CheckSummary += @{ Status = "FAIL"; Message = "Dependencies: go.mod not found in backend/" }
        $FailedItems += "backend/go.mod is missing"
    }
}

# ------------------------------------------------------------------------------
# 4. Check & Start Redis
# ------------------------------------------------------------------------------
$RedisHost = "127.0.0.1"
$RedisPort = 6379

$IsLocalRedis = $false
if ($RedisUrl -match "(localhost|127\.0\.0\.1):?(\d*)") {
    $IsLocalRedis = $true
    if ($Matches[2]) {
        $RedisPort = [int]$Matches[2]
    }
}

if ($IsLocalRedis) {
    $RedisRunning = Test-TcpPort -ComputerName $RedisHost -Port $RedisPort

    if (-not $RedisRunning) {
        Write-Host "[INFO] Redis is not running. Attempting to start Redis server ..." -ForegroundColor Blue

        # Attempt 1: Start Windows Redis / Memurai service
        $redisService = Get-Service -Name "redis", "Redis", "memurai" -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($redisService -and $redisService.Status -ne "Running") {
            try {
                Start-Service -Name $redisService.Name -ErrorAction SilentlyContinue
                Start-Sleep -Seconds 1
            } catch {}
        }

        # Attempt 2: Start redis-server process if in PATH
        if (-not (Test-TcpPort -ComputerName $RedisHost -Port $RedisPort)) {
            $redisServerCmd = Get-Command redis-server -ErrorAction SilentlyContinue
            if ($redisServerCmd) {
                Start-Process -FilePath "redis-server" -ArgumentList "--port $RedisPort" -WindowStyle Hidden -ErrorAction SilentlyContinue
                Start-Sleep -Seconds 1
            }
        }

        # Attempt 3: Docker container
        if (-not (Test-TcpPort -ComputerName $RedisHost -Port $RedisPort)) {
            if (Get-Command docker -ErrorAction SilentlyContinue) {
                docker start redis 2>$null
                Start-Sleep -Seconds 1
            }
        }

        # Verify
        $RedisRunning = Test-TcpPort -ComputerName $RedisHost -Port $RedisPort
        if ($RedisRunning) {
            $CheckSummary += @{ Status = "PASS"; Message = "Redis Service: Started and responding on port $RedisPort" }
        } else {
            $CheckSummary += @{ Status = "FAIL"; Message = "Redis Service: Not running on port $RedisPort and could not be started" }
            $FailedItems += "Redis is not running"
            $WhatToInstall += "Install/Start Redis:
      - Docker: docker run -d -p 6379:6379 --name redis redis:alpine
      - Or install Memurai/Redis for Windows: https://github.com/microsoftarchive/redis/releases"
        }
    } else {
        $CheckSummary += @{ Status = "PASS"; Message = "Redis Service: Running on port $RedisPort" }
    }
} else {
    $CheckSummary += @{ Status = "INFO"; Message = "Redis Service: Remote or disabled via REDIS_URL" }
}

# ------------------------------------------------------------------------------
# 5. Check PostgreSQL database connectivity
# ------------------------------------------------------------------------------
$DbHost = "127.0.0.1"
$DbPort = 5432

if ($DatabaseUrl -match "@([^:/]+):?(\d*)/") {
    $DbHost = $Matches[1]
    if ($Matches[2]) {
        $DbPort = [int]$Matches[2]
    }
}

$DbRunning = Test-TcpPort -ComputerName $DbHost -Port $DbPort
if (-not $DbRunning -and ($DbHost -eq "127.0.0.1" -or $DbHost -eq "localhost")) {
    $pgService = Get-Service -Name "postgresql*", "postgres*" -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($pgService -and $pgService.Status -ne "Running") {
        try {
            Start-Service -Name $pgService.Name -ErrorAction SilentlyContinue
            Start-Sleep -Seconds 1
            $DbRunning = Test-TcpPort -ComputerName $DbHost -Port $DbPort
        } catch {}
    }
}

if ($DbRunning) {
    $CheckSummary += @{ Status = "PASS"; Message = "PostgreSQL: Accepting connections on $DbHost`:$DbPort" }
} else {
    $CheckSummary += @{ Status = "FAIL"; Message = "PostgreSQL: Unreachable at $DbHost`:$DbPort" }
    $FailedItems += "PostgreSQL database is unreachable"
    $WhatToInstall += "Ensure PostgreSQL service is running on $DbHost`:$DbPort (Start PostgreSQL service or run backend/db/init.sql)"
}

# ------------------------------------------------------------------------------
# 5b. Apply pending database migrations (00001-00012: workspaces, uploads, snapshots ...)
# ------------------------------------------------------------------------------
if ($env:SKIP_MIGRATE -eq "1") {
    $CheckSummary += @{ Status = "INFO"; Message = "Migrations: Skipped (SKIP_MIGRATE=1); run: go run ./cmd/api migrate up" }
} elseif ($AutoMigrate -eq "true") {
    $CheckSummary += @{ Status = "INFO"; Message = "Migrations: AUTO_MIGRATE=true: the gateway applies them when it starts" }
} elseif ($DbRunning -and $DatabaseUrl -and $GoCmd) {
    Write-Host "[INFO] Checking database migrations ..." -ForegroundColor Blue
    $statusOut = go run ./cmd/api migrate status 2>$null
    $pending = @($statusOut | Where-Object { $_ -match "pending" }).Count
    if ($pending -gt 0) {
        Write-Host "[INFO] Applying $pending pending migration(s) ..." -ForegroundColor Blue
        $migOut = go run ./cmd/api migrate up 2>&1
        if ($LASTEXITCODE -eq 0) {
            $CheckSummary += @{ Status = "PASS"; Message = "Migrations: Applied $pending pending migration(s)" }
        } else {
            $CheckSummary += @{ Status = "FAIL"; Message = "Migrations: migrate up failed: $(@($migOut)[-1])" }
            $FailedItems += "Database migrations failed"
            $WhatToInstall += "Run: go run ./cmd/api migrate up (in backend\) and fix the reported error"
        }
    } else {
        $CheckSummary += @{ Status = "PASS"; Message = "Migrations: Database schema is up to date" }
    }
}

# ------------------------------------------------------------------------------
# 6. Check Decision Engine (Rules Engine URL) and the rules_version it reports
# ------------------------------------------------------------------------------
$DeHost = "127.0.0.1"
$DePort = 8000
if ($RulesEngineUrl -match "//([^:/]+)") {
    $DeHost = $Matches[1]
    if ($DeHost -eq "localhost") { $DeHost = "127.0.0.1" }
}
if ($RulesEngineUrl -match ":(\d+)") {
    $DePort = [int]$Matches[1]
}

if (Test-TcpPort -ComputerName $DeHost -Port $DePort) {
    $DeVersion = $null
    try {
        $DeVersion = (Invoke-RestMethod -Uri "$RulesEngineUrl/health" -TimeoutSec 2 -ErrorAction Stop).rules_version
    } catch {}
    if ($DeVersion) {
        $CheckSummary += @{ Status = "PASS"; Message = "Decision Engine: Reachable on port $DePort (rules version $DeVersion; the cache key follows it)" }
    } else {
        $CheckSummary += @{ Status = "WARN"; Message = "Decision Engine: Reachable on port $DePort but reports no rules_version (older engine? update decision-engine\)" }
    }
} else {
    $CheckSummary += @{ Status = "INFO"; Message = "Decision Engine: Not running on port $DePort (Start in another terminal: cd ..\decision-engine; .\start.ps1)" }
}

# ------------------------------------------------------------------------------
# 6b. Security and upload worker settings
# ------------------------------------------------------------------------------
switch ($AuthMode) {
    "required" { $CheckSummary += @{ Status = "INFO"; Message = "Sign-in: AUTH_MODE=required (bearer tokens). First admin + token: go run ./cmd/api bootstrap -email you@example.com -workspace `"Finance`"" } }
    "off" { $CheckSummary += @{ Status = "INFO"; Message = "Sign-in: AUTH_MODE=off (single default workspace, no tokens; for local use only)" } }
    default {
        $CheckSummary += @{ Status = "FAIL"; Message = "Sign-in: AUTH_MODE='$AuthMode' (must be off or required)" }
        $FailedItems += "AUTH_MODE is invalid"
        $WhatToInstall += "Set AUTH_MODE=off or AUTH_MODE=required in backend\.env"
    }
}
if ($RateLimitPerMinute -eq "0") {
    $CheckSummary += @{ Status = "WARN"; Message = "Rate limit: Disabled (RATE_LIMIT_PER_MINUTE=0)" }
} else {
    $CheckSummary += @{ Status = "PASS"; Message = "Rate limit: $RateLimitPerMinute requests/minute per client IP" }
}
if ($WorkerEnabled -eq "false") {
    $CheckSummary += @{ Status = "WARN"; Message = "Upload worker: WORKER_ENABLED=false (stored uploads stay queued unless another gateway runs the worker)" }
} else {
    $CheckSummary += @{ Status = "PASS"; Message = "Upload worker: Runs inside the gateway (parses stored uploads, retries with backoff)" }
}

# ------------------------------------------------------------------------------
# 7. Check Backend Port availability
# ------------------------------------------------------------------------------
if (Test-TcpPort -ComputerName "127.0.0.1" -Port $Port) {
    $CheckSummary += @{ Status = "WARN"; Message = "Port $Port`: Port $Port is currently in use (may conflict with backend)" }
} else {
    $CheckSummary += @{ Status = "PASS"; Message = "Port $Port`: Available" }
}

# ------------------------------------------------------------------------------
# Print Summary Table
# ------------------------------------------------------------------------------
Write-Host ""
Write-Host "Check Results:" -ForegroundColor White
foreach ($chk in $CheckSummary) {
    switch ($chk.Status) {
        "PASS" { Write-Host "  [PASS] " -ForegroundColor Green -NoNewline; Write-Host $chk.Message }
        "FAIL" { Write-Host "  [FAIL] " -ForegroundColor Red -NoNewline; Write-Host $chk.Message }
        "WARN" { Write-Host "  [WARN] " -ForegroundColor Yellow -NoNewline; Write-Host $chk.Message }
        default { Write-Host "  [INFO] " -ForegroundColor Blue -NoNewline; Write-Host $chk.Message }
    }
}
Write-Host ""

# ------------------------------------------------------------------------------
# Final Result & Action Required
# ------------------------------------------------------------------------------
if ($FailedItems.Count -gt 0) {
    Write-Host "=======================================================" -ForegroundColor Red
    Write-Host "               PRE-FLIGHT CHECKS FAILED!               " -ForegroundColor Red
    Write-Host "=======================================================" -ForegroundColor Red
    Write-Host "What is left to install / fix:" -ForegroundColor White
    $idx = 1
    foreach ($item in $WhatToInstall) {
        Write-Host "  $idx. " -ForegroundColor Yellow -NoNewline
        Write-Host $item
        $idx++
    }
    Write-Host ""
    Write-Host "Please resolve the issues above before running Backend." -ForegroundColor Red
    exit 1
}

Write-Host "=======================================================" -ForegroundColor Green
Write-Host "        ALL CHECKS PASSED - STARTING APP               " -ForegroundColor Green
Write-Host "=======================================================" -ForegroundColor Green
Write-Host "Command: go run ./cmd/api" -ForegroundColor White
Write-Host "Backend API URL: http://localhost:$Port  (sign-in: $AuthMode)" -ForegroundColor Cyan
Write-Host ""

# Execute backend
go run ./cmd/api
