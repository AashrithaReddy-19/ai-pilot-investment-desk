# ==============================================================================
# Decision Engine Startup Script (Windows PowerShell)
# Performs pre-flight checks, dependency installs, .env verification,
# and starts the service: uvicorn app.main:app on HOST:PORT from .env (reload when RELOAD=true)
# ==============================================================================

[CmdletBinding()]
param()

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

Write-Host "=======================================================" -ForegroundColor Cyan
Write-Host "         DECISION ENGINE PRE-FLIGHT CHECKS             " -ForegroundColor Cyan
Write-Host "=======================================================" -ForegroundColor Cyan

$FailedItems = @()
$WhatToInstall = @()
$CheckSummary = @()

# ------------------------------------------------------------------------------
# 1. Check Python runtime
# ------------------------------------------------------------------------------
$PythonBin = $null
if (Get-Command python -ErrorAction SilentlyContinue) {
    $PythonBin = "python"
} elseif (Get-Command py -ErrorAction SilentlyContinue) {
    $PythonBin = "py"
} elseif (Get-Command python3 -ErrorAction SilentlyContinue) {
    $PythonBin = "python3"
}

if (-not $PythonBin) {
    $CheckSummary += @{ Status = "FAIL"; Message = "Python Runtime: Not found in PATH" }
    $FailedItems += "Python 3 is missing"
    $WhatToInstall += "Install Python 3 (Run: winget install Python.Python.3.12 or download from https://www.python.org/downloads/)"
} else {
    try {
        $PyVer = & $PythonBin -c "import sys; print('.'.join(map(str, sys.version_info[:3])))" 2>$null
        & $PythonBin -c "import sys; sys.exit(0 if sys.version_info >= (3, 10) else 1)" 2>$null
        if ($LASTEXITCODE -eq 0) {
            $CheckSummary += @{ Status = "PASS"; Message = "Python Runtime: $PythonBin (v$PyVer)" }
        } else {
            $CheckSummary += @{ Status = "FAIL"; Message = "Python Runtime: $PythonBin v$PyVer is too old (3.10+ required)" }
            $FailedItems += "Python 3.10+ is required"
            $WhatToInstall += "Install Python 3.10 or newer (winget install Python.Python.3.12)"
        }
    } catch {
        $CheckSummary += @{ Status = "PASS"; Message = "Python Runtime: $PythonBin" }
    }
}

# ------------------------------------------------------------------------------
# 2. Check .env configuration file
# ------------------------------------------------------------------------------
if (Test-Path ".env") {
    $CheckSummary += @{ Status = "PASS"; Message = "Environment File: .env is present" }
} else {
    if (Test-Path ".env.example") {
        Copy-Item -Path ".env.example" -Destination ".env"
        $CheckSummary += @{ Status = "PASS"; Message = "Environment File: Created .env from .env.example" }
    } else {
        $DefaultEnv = @"
# Decision Engine Configuration
PORT=8000
HOST=0.0.0.0
RELOAD=true
"@
        Set-Content -Path ".env.example" -Value $DefaultEnv
        Copy-Item -Path ".env.example" -Destination ".env"
        $CheckSummary += @{ Status = "PASS"; Message = "Environment File: Generated default .env from .env.example" }
    }
}

# Read PORT / HOST / RELOAD from .env (real environment variables win)
$EnvValues = @{}
if (Test-Path ".env") {
    Get-Content ".env" | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith("#")) {
            $parts = $line -split "=", 2
            if ($parts.Length -eq 2) { $EnvValues[$parts[0].Trim()] = $parts[1].Trim() }
        }
    }
}
$AppPort = if ($env:PORT) { $env:PORT } elseif ($EnvValues["PORT"]) { $EnvValues["PORT"] } else { "8000" }
$AppHost = if ($env:HOST) { $env:HOST } elseif ($EnvValues["HOST"]) { $EnvValues["HOST"] } else { "0.0.0.0" }
$AppReload = if ($env:RELOAD) { $env:RELOAD } elseif ($EnvValues["RELOAD"]) { $EnvValues["RELOAD"] } else { "true" }
if ($AppPort -notmatch '^\d+$') {
    $CheckSummary += @{ Status = "FAIL"; Message = "Port: PORT='$AppPort' is not a number" }
    $FailedItems += "PORT is invalid"
    $WhatToInstall += "Set PORT to a number in decision-engine\.env (default 8000)"
    $AppPort = "8000"
}

# ------------------------------------------------------------------------------
# 3. Check Virtual Environment (.venv / venv)
# ------------------------------------------------------------------------------
$VenvDir = $null
if (Test-Path ".venv\Scripts\Activate.ps1") {
    $VenvDir = ".venv"
} elseif (Test-Path "venv\Scripts\Activate.ps1") {
    $VenvDir = "venv"
} elseif (Test-Path ".venv\bin\activate") {
    $VenvDir = ".venv"
} elseif (Test-Path "venv\bin\activate") {
    $VenvDir = "venv"
}

if (-not $VenvDir) {
    if ($PythonBin) {
        Write-Host "[INFO] Creating Python virtual environment in .venv ..." -ForegroundColor Blue
        try {
            & $PythonBin -m venv .venv
            if (Test-Path ".venv") {
                $VenvDir = ".venv"
                $CheckSummary += @{ Status = "PASS"; Message = "Virtual Environment: Created and configured at .venv" }
            } else {
                throw "Venv creation failed"
            }
        } catch {
            $CheckSummary += @{ Status = "FAIL"; Message = "Virtual Environment: Failed to create virtual environment" }
            $FailedItems += "Python venv creation failed"
            $WhatToInstall += "Ensure Python virtualenv/venv is functional: $PythonBin -m venv .venv"
        }
    }
} else {
    $CheckSummary += @{ Status = "PASS"; Message = "Virtual Environment: Found at $VenvDir" }
}

# ------------------------------------------------------------------------------
# 4. Check & Install Dependencies
# ------------------------------------------------------------------------------
$VenvPython = $null
$VenvPip = $null
$VenvUvicorn = $null

if ($VenvDir) {
    if (Test-Path "$VenvDir\Scripts\python.exe") {
        $VenvPython = "$VenvDir\Scripts\python.exe"
        $VenvPip = "$VenvDir\Scripts\pip.exe"
        $VenvUvicorn = "$VenvDir\Scripts\uvicorn.exe"
    } elseif (Test-Path "$VenvDir/bin/python") {
        $VenvPython = "$VenvDir/bin/python"
        $VenvPip = "$VenvDir/bin/pip"
        $VenvUvicorn = "$VenvDir/bin/uvicorn"
    }
}

if ($VenvPython -and (Test-Path $VenvPython)) {
    $DepsInstalled = $false
    try {
        # python-multipart serves the CSV/XLSX upload endpoints (XLSX itself is read with the stdlib)
        & $VenvPython -c "import fastapi, uvicorn, httpx, pytest, importlib.util as u; assert u.find_spec('python_multipart') or u.find_spec('multipart')" 2>$null
        if ($LASTEXITCODE -eq 0) {
            $DepsInstalled = $true
        }
    } catch {
        $DepsInstalled = $false
    }

    if (-not $DepsInstalled) {
        if (Test-Path "requirements.txt") {
            Write-Host "[INFO] Installing missing dependencies from requirements.txt ..." -ForegroundColor Blue
            & $VenvPip install -r requirements.txt
            if ($LASTEXITCODE -eq 0) {
                $CheckSummary += @{ Status = "PASS"; Message = "Dependencies: Installed required packages successfully" }
            } else {
                $CheckSummary += @{ Status = "FAIL"; Message = "Dependencies: Package installation failed" }
                $FailedItems += "Dependencies installation failed"
                $WhatToInstall += "Run: & `"$VenvPip`" install -r requirements.txt (check network / pip logs)"
            }
        } else {
            $CheckSummary += @{ Status = "FAIL"; Message = "Dependencies: requirements.txt is missing" }
            $FailedItems += "requirements.txt missing"
            $WhatToInstall += "Create requirements.txt with: fastapi, uvicorn, python-multipart, pytest, httpx"
        }
    } else {
        $CheckSummary += @{ Status = "PASS"; Message = "Dependencies: All required packages installed" }
    }
} else {
    if ($FailedItems.Count -eq 0) {
        $CheckSummary += @{ Status = "FAIL"; Message = "Dependencies: Cannot verify without active virtual environment" }
        $FailedItems += "No virtual environment found"
    }
}

# ------------------------------------------------------------------------------
# 5. Check port availability
# ------------------------------------------------------------------------------
$PortInUse = $false
try {
    $Socket = New-Object System.Net.Sockets.TcpClient
    $AsyncResult = $Socket.BeginConnect("127.0.0.1", [int]$AppPort, $null, $null)
    $Wait = $AsyncResult.AsyncWaitHandle.WaitOne(800, $false)
    if ($Wait -and $Socket.Connected) {
        $PortInUse = $true
        $Socket.EndConnect($AsyncResult)
    }
    $Socket.Close()
} catch {
    $PortInUse = $false
}

if ($PortInUse) {
    $CheckSummary += @{ Status = "WARN"; Message = "Port $AppPort`: Port $AppPort is currently in use (may conflict with uvicorn)" }
} else {
    $CheckSummary += @{ Status = "PASS"; Message = "Port $AppPort`: Available" }
}

# ------------------------------------------------------------------------------
# 6. Engine versions (reported on GET /health; the gateway's cache key follows rules_version)
# ------------------------------------------------------------------------------
if (Test-Path "app\version.py") {
    $verText = Get-Content "app\version.py" -Raw
    $Rv = if ($verText -match 'RULES_VERSION = "([^"]+)"') { $Matches[1] } else { "?" }
    $Cv = if ($verText -match 'CONTRACT_VERSION = (\d+)') { $Matches[1] } else { "?" }
    $CheckSummary += @{ Status = "PASS"; Message = "Engine version: rules $Rv, contract $Cv (examples in contracts\engine\)" }
    if ((Test-Path "contracts\VERSION") -and ((Get-Content "contracts\VERSION" -Raw).Trim() -ne $Cv)) {
        $CheckSummary += @{ Status = "WARN"; Message = "Contracts: contracts\VERSION differs from CONTRACT_VERSION; run: python scripts\build_contracts.py" }
    }
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
    Write-Host "Please resolve the issues above before running Decision Engine." -ForegroundColor Red
    exit 1
}

Write-Host "=======================================================" -ForegroundColor Green
Write-Host "        ALL CHECKS PASSED - STARTING APP               " -ForegroundColor Green
Write-Host "=======================================================" -ForegroundColor Green
$UvicornArgs = @("app.main:app", "--host", $AppHost, "--port", $AppPort)
if ($AppReload -eq "true") { $UvicornArgs += "--reload" }
Write-Host "Command: uvicorn $($UvicornArgs -join ' ')" -ForegroundColor White
Write-Host "API Docs: http://localhost:$AppPort/docs" -ForegroundColor Cyan
Write-Host ""

# Activate virtualenv if script exists and execute uvicorn
if (Test-Path "$VenvDir\Scripts\Activate.ps1") {
    & "$VenvDir\Scripts\Activate.ps1"
}

if ($VenvUvicorn -and (Test-Path $VenvUvicorn)) {
    & $VenvUvicorn @UvicornArgs
} else {
    & $VenvPython -m uvicorn @UvicornArgs
}
