# Stop everything started by scripts/start_backend.ps1 (or a manually started devstack). Only matches C23 processes.
$n = 0
Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -match 'uvicorn app\.ai_service|c23-devstack|c23desk-devstack' } | ForEach-Object {
  Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue; $n++
}
Start-Sleep 1
Write-Host "stopped $n process(es)"
