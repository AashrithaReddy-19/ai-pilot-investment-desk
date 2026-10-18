# ==============================================================================
# Frontend Startup Script (Windows PowerShell)
# Performs pre-flight checks, installs dependencies, and starts the Vite dev
# server on :5173, proxying /api, /health and /ready to the Go gateway
# (VITE_PROXY_TARGET, default http://127.0.0.1:8080).
# ==============================================================================

[CmdletBinding()]
param()

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

Write-Host "=======================================================" -ForegroundColor Cyan
Write-Host "          FRONTEND PRE-FLIGHT CHECKS                   " -ForegroundColor Cyan
Write-Host "=======================================================" -ForegroundColor Cyan

$FailedItems = @()
$WhatToInstall = @()
$CheckSummary = @()

# ------------------------------------------------------------------------------
# 1. Check Node.js and npm
# ------------------------------------------------------------------------------
if (Get-Command node -ErrorAction SilentlyContinue) {
    try {
        $NodeMajor = [int]((node -p 'process.versions.node.split(".")[0]') 2>$null)
        if ($NodeMajor -ge 20) {
            $CheckSummary += @{ Status = "PASS"; Message = "Node.js: $(node --version) (npm $(npm --version))" }
        } else {
            $CheckSummary += @{ Status = "FAIL"; Message = "Node.js: Version $(node --version) is too old (>= 20 required)" }
            $FailedItems += "Node.js 20+ is required"
            $WhatToInstall += "Upgrade Node.js: download the LTS from https://nodejs.org (or: winget install OpenJS.NodeJS.LTS)"
        }
    } catch {
        $CheckSummary += @{ Status = "FAIL"; Message = "Node.js: version check failed" }
        $FailedItems += "Node.js version could not be verified"
    }
} else {
    $CheckSummary += @{ Status = "FAIL"; Message = "Node.js: Not found in PATH" }
    $FailedItems += "Node.js is missing"
    $WhatToInstall += "Install Node.js 20 LTS (winget install OpenJS.NodeJS.LTS or https://nodejs.org)"
}

# ------------------------------------------------------------------------------
# 2. Install dependencies if node_modules is missing
# ------------------------------------------------------------------------------
if (Get-Command npm -ErrorAction SilentlyContinue) {
    if ((Test-Path "node_modules") -and (Test-Path "node_modules\.bin\vite.cmd")) {
        $CheckSummary += @{ Status = "PASS"; Message = "Dependencies: node_modules present (vite installed)" }
    } else {
        Write-Host "[INFO] Installing npm dependencies ..." -ForegroundColor Blue
        # npm ci installs exactly what package-lock.json pins and never rewrites the tracked lockfile
        npm ci
        if ($LASTEXITCODE -eq 0) {
            $CheckSummary += @{ Status = "PASS"; Message = "Dependencies: npm ci completed" }
        } else {
            $CheckSummary += @{ Status = "FAIL"; Message = "Dependencies: npm ci failed" }
            $FailedItems += "npm ci failed"
            $WhatToInstall += "Run: npm ci in frontend/ (check network or proxy)"
        }
    }
}

# ------------------------------------------------------------------------------
# 3. Gateway reachability (the Vite proxy target: VITE_PROXY_TARGET, default http://127.0.0.1:8080)
# ------------------------------------------------------------------------------
$ProxyTarget = $env:VITE_PROXY_TARGET
foreach ($f in @(".env.local", ".env")) {
    if (-not $ProxyTarget -and (Test-Path $f)) {
        $line = Get-Content $f | Where-Object { $_ -match '^VITE_PROXY_TARGET=' } | Select-Object -Last 1
        if ($line) { $ProxyTarget = ($line -split "=", 2)[1].Trim().Trim('"') }
    }
}
if (-not $ProxyTarget) { $ProxyTarget = "http://127.0.0.1:8080" }
$GwHost = "127.0.0.1"; $GwPort = 8080
if ($ProxyTarget -match "//([^:/]+)") { $GwHost = $Matches[1]; if ($GwHost -eq "localhost") { $GwHost = "127.0.0.1" } }
if ($ProxyTarget -match ":(\d+)") { $GwPort = [int]$Matches[1] }

$GatewayOk = $false
try {
    $tcp = New-Object Net.Sockets.TcpClient
    $tcp.Connect($GwHost, $GwPort)
    $GatewayOk = $tcp.Connected
    $tcp.Close()
} catch { $GatewayOk = $false }

if ($GatewayOk) {
    $GwHealth = $null
    try { $GwHealth = Invoke-RestMethod -Uri "$ProxyTarget/health" -TimeoutSec 2 -ErrorAction Stop } catch {}
    $rulesNote = if ($GwHealth -and $GwHealth.rules_version) { ", engine rules version $($GwHealth.rules_version)" } else { "" }
    $CheckSummary += @{ Status = "PASS"; Message = "Go gateway: Reachable at $ProxyTarget (proxy target)$rulesNote" }
    if ($GwHealth -and $GwHealth.auth_mode -eq "required") {
        $CheckSummary += @{ Status = "WARN"; Message = "Sign-in: The gateway runs with AUTH_MODE=required; these screens do not send a token yet, so API calls will answer 401. Use AUTH_MODE=off for local UI work." }
    }
} else {
    $CheckSummary += @{ Status = "WARN"; Message = "Go gateway: Not running at $ProxyTarget yet (start it in another terminal: cd ..\backend; .\start.ps1). Pages will show a connection notice until it is up." }
}

# ------------------------------------------------------------------------------
# 4. Check port 5173 availability
# ------------------------------------------------------------------------------
$PortOk = $false
try {
    $tcp = New-Object Net.Sockets.TcpClient
    $tcp.Connect("127.0.0.1", 5173)
    $PortOk = $tcp.Connected
    $tcp.Close()
} catch { $PortOk = $false }

if ($PortOk) {
    $CheckSummary += @{ Status = "WARN"; Message = "Port 5173: In use (is the dev server already running? Vite will pick the next free port)" }
} else {
    $CheckSummary += @{ Status = "PASS"; Message = "Port 5173: Available" }
}

# ------------------------------------------------------------------------------
# Print Summary Table
# ------------------------------------------------------------------------------
Write-Host ""
Write-Host "Check Results:" -ForegroundColor Bold
foreach ($line in $CheckSummary) {
    if ($line.Status -eq "PASS") {
        Write-Host "  [PASS] $($line.Message)" -ForegroundColor Green
    } elseif ($line.Status -eq "WARN") {
        Write-Host "  [WARN] $($line.Message)" -ForegroundColor Yellow
    } else {
        Write-Host "  [FAIL] $($line.Message)" -ForegroundColor Red
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
    Write-Host "What is left to install / fix:" -ForegroundColor Yellow
    $index = 1
    foreach ($item in $WhatToInstall) {
        Write-Host "  $index. $item" -ForegroundColor Yellow
        $index++
    }
    Write-Host ""
    Write-Host "Please resolve the issues above before running the frontend." -ForegroundColor Red
    exit 1
}

Write-Host "=======================================================" -ForegroundColor Green
Write-Host "        ALL CHECKS PASSED - STARTING APP               " -ForegroundColor Green
Write-Host "=======================================================" -ForegroundColor Green
Write-Host "Command: npm run dev"
Write-Host "Frontend URL: http://localhost:5173" -ForegroundColor Cyan
Write-Host ""

npm run dev
