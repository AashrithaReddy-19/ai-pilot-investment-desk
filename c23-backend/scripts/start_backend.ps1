# Start the full backend locally WITHOUT Docker (Windows PowerShell):
#   embedded PostgreSQL + Go API (:8080, via backend/cmd/devstack) + Redis-protocol cache + FastAPI AI service (:8001)
#   powershell -ExecutionPolicy Bypass -File scripts/start_backend.ps1 [-Fresh] [-NoAI] [-NoCache] [-RedisDown]
# Stop with scripts/stop_backend.ps1. Logs: %TEMP%\c23-backend\*.log
param([switch]$Fresh, [switch]$NoAI, [switch]$NoCache, [switch]$RedisDown)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$logs = Join-Path $env:TEMP 'c23-backend'; New-Item -ItemType Directory -Force $logs | Out-Null
$goBin = Join-Path $env:LOCALAPPDATA 'c23-tools\go\bin'; if (Test-Path $goBin) { $env:Path = "$goBin;$env:Path" }
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go 1.24+ not found on PATH' }
$py = Join-Path $root 'decision-engine\.venv\Scripts\python.exe'

& (Join-Path $PSScriptRoot 'stop_backend.ps1') | Out-Null
Push-Location (Join-Path $root 'backend'); go build -o (Join-Path $env:TEMP 'c23-devstack.exe') ./cmd/devstack; Pop-Location

Remove-Item Env:REDIS_URL, Env:DATABASE_URL -ErrorAction SilentlyContinue
$env:PORT = '8080'
if ($NoAI) { $env:AI_SERVICE_URL = 'http://127.0.0.1:1' } else {
  if (-not (Test-Path $py)) { throw "Python venv missing: cd decision-engine; py -3.13 -m venv .venv; .venv\Scripts\pip install -r requirements.txt" }
  $env:AI_SERVICE_URL = 'http://127.0.0.1:8001'
  Start-Process -FilePath $py -ArgumentList '-m', 'uvicorn', 'app.ai_service:app', '--port', '8001', '--log-level', 'warning' `
    -WorkingDirectory (Join-Path $root 'decision-engine') -WindowStyle Hidden -RedirectStandardOutput "$logs\ai.log" -RedirectStandardError "$logs\ai.err.log" | Out-Null
}
$devArgs = @()
if ($Fresh) { $devArgs += '-fresh' }
if ($RedisDown) { $env:REDIS_URL = 'redis://127.0.0.1:1/0' } elseif (-not $NoCache) { $devArgs += '-redis' }
$p = @{ FilePath = (Join-Path $env:TEMP 'c23-devstack.exe'); WorkingDirectory = (Join-Path $root 'backend'); WindowStyle = 'Hidden'
        RedirectStandardOutput = "$logs\api.log"; RedirectStandardError = "$logs\api.err.log" }
if ($devArgs.Count) { $p.ArgumentList = $devArgs }
Start-Process @p | Out-Null

for ($i = 0; $i -lt 120; $i++) {
  try { Invoke-RestMethod http://127.0.0.1:8080/ready -TimeoutSec 2 | Out-Null; break } catch { Start-Sleep 1 }
  if ($i -eq 119) { throw "API not ready; see $logs\api.err.log" }
}
if (-not $NoAI) { for ($i = 0; $i -lt 30; $i++) { try { Invoke-RestMethod http://127.0.0.1:8001/health -TimeoutSec 2 | Out-Null; break } catch { Start-Sleep 1 } } }
$h = Invoke-RestMethod http://127.0.0.1:8080/health
Write-Host "Backend ready: API http://127.0.0.1:8080  health=$($h.components | ConvertTo-Json -Compress)"
