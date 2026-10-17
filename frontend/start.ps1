# ==============================================================================
# Frontend Startup Script (Windows PowerShell)
# Performs pre-flight checks, installs dependencies, and starts the Vite dev
# server on :5173, proxying /api, /health and /ready to the Go gateway on :8080.
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
        if (npm install) {
            $CheckSummary += @{ Status = "PASS"; Message = "Dependencies: npm install completed" }
        } else {
            $CheckSummary += @{ Status = "FAIL"; Message = "Dependencies: npm install failed" }
            $FailedItems += "npm install failed"
            $WhatToInstall += "Run: npm install in frontend/ (check network or proxy)"
        }
    }
}

# ------------------------------------------------------------------------------
# 3. Gateway reachability (the Vite proxy target)
# ------------------------------------------------------------------------------
$GatewayOk = $false
try {
    $tcp = New-Object Net.Sockets.TcpClient
    $tcp.Connect("127.0.0.1", 8080)
    $GatewayOk = $tcp.Connected
    $tcp.Close()
} catch { $GatewayOk = $false }

if ($GatewayOk) {
    $CheckSummary += @{ Status = "PASS"; Message = "Go gateway: Reachable on :8080 (proxy target)" }
} else {
    $CheckSummary += @{ Status = "WARN"; Message = "Go gateway: Not running on :8080 yet (start it in another terminal: cd ..\backend; .\start.ps1). Pages will show a connection notice until it is up." }
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
