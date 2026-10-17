# ==============================================================================
# Integration Test Script for C23 Backend & Decision Engine (Windows PowerShell)
# Runs test curl commands against the running API and cleans up any created data.
# ==============================================================================

[CmdletBinding()]
param()

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

$ApiBase = "http://localhost:8080"
$PortfolioName = "run_1"

Write-Host "=======================================================" -ForegroundColor Cyan
Write-Host "             API INTEGRATION TESTS                     " -ForegroundColor Cyan
Write-Host "=======================================================" -ForegroundColor Cyan

# Test backend availability
$BackendRunning = $false
try {
    $resp = Invoke-WebRequest -Uri "$ApiBase/health" -TimeoutSec 2 -UseBasicParsing -ErrorAction Stop
    if ($resp.StatusCode -eq 200) { $BackendRunning = $true }
} catch {
    $BackendRunning = $false
}

if (-not $BackendRunning) {
    Write-Host "[ERROR] Backend is not reachable at $ApiBase!" -ForegroundColor Red
    Write-Host "Please ensure both services are running before testing:" -ForegroundColor Yellow
    Write-Host "  Terminal 1: cd decision-engine; .\start.ps1"
    Write-Host "  Terminal 2: cd backend; .\start.ps1"
    exit 1
}

# Determine curl command
$CurlCmd = "curl.exe"
if (-not (Get-Command "curl.exe" -ErrorAction SilentlyContinue)) {
    $CurlCmd = "curl"
}

# Determine JSON formatter
$HasJq = [bool](Get-Command "jq" -ErrorAction SilentlyContinue)

function Format-JsonOutput {
    param([string]$JsonString)
    if ($HasJq) {
        $JsonString | jq
    } else {
        try {
            $JsonString | ConvertFrom-Json | ConvertTo-Json -Depth 10
        } catch {
            Write-Host $JsonString
        }
    }
}

# Read DATABASE_URL from backend/.env if available for evaluations cleanup
$DatabaseUrl = $null
if (Test-Path "backend\.env") {
    Get-Content "backend\.env" | ForEach-Object {
        $line = $_.Trim()
        if ($line -like "DATABASE_URL=*") {
            $DatabaseUrl = $line.Substring("DATABASE_URL=".Length).Trim('"')
        }
    }
}

$PrevEvalId = $null
if ($DatabaseUrl -and (Get-Command psql -ErrorAction SilentlyContinue)) {
    try {
        $PrevEvalId = & psql $DatabaseUrl -t -A -c "SELECT COALESCE(MAX(id), 0) FROM evaluations;" 2>$null
    } catch {}
}

# Ensure clean starting state
& $CurlCmd -s -X DELETE "$ApiBase/api/v1/portfolios/$PortfolioName" 2>$null | Out-Null

try {
    # ------------------------------------------------------------------------------
    # Test 1: Readiness check (verifies Postgres, FastAPI, and Redis status)
    # ------------------------------------------------------------------------------
    Write-Host ""
    Write-Host "# 1. Readiness check (verifies Postgres, FastAPI, and Redis status)" -ForegroundColor Blue
    Write-Host "curl -s http://localhost:8080/ready | jq" -ForegroundColor Cyan
    $ReadyRaw = & $CurlCmd -s "$ApiBase/ready"
    Format-JsonOutput $ReadyRaw

    if ($ReadyRaw -like '*"ready":true*') {
        Write-Host "--> Test 1 Passed: System is READY" -ForegroundColor Green
    } else {
        Write-Host "--> Test 1 Warning: System reported checks down (inspect above)" -ForegroundColor Yellow
    }

    # ------------------------------------------------------------------------------
    # Test 2: Evaluate both modes (independent + conservative) and store evaluation history
    # ------------------------------------------------------------------------------
    Write-Host ""
    Write-Host "# 2. Evaluate both modes (independent + conservative) and store evaluation history" -ForegroundColor Blue
    Write-Host "curl -s -X POST http://localhost:8080/api/v1/evaluate -d '{}' | jq" -ForegroundColor Cyan
    $EvalRaw = & $CurlCmd -s -X POST "$ApiBase/api/v1/evaluate" -d "{}"
    Format-JsonOutput $EvalRaw

    if ($EvalRaw -like '*"independent"*' -and $EvalRaw -like '*"conservative"*') {
        Write-Host "--> Test 2 Passed: Dual-mode evaluation computed successfully" -ForegroundColor Green
    } else {
        Write-Host "--> Test 2 Failed: Did not receive expected evaluation shape" -ForegroundColor Red
    }

    # ------------------------------------------------------------------------------
    # Test 3: Save a portfolio to Postgres
    # ------------------------------------------------------------------------------
    Write-Host ""
    Write-Host "# 3. Save a portfolio to Postgres" -ForegroundColor Blue
    Write-Host "curl -s -X POST http://localhost:8080/api/v1/portfolios \" -ForegroundColor Cyan
    Write-Host "  -H `"Content-Type: application/json`" \" -ForegroundColor Cyan
    Write-Host "  -d '{\`"name\`":\`"$PortfolioName\`",\`"mode\`":\`"independent\`",\`"budget\`":600000}' | jq" -ForegroundColor Cyan

    $SavePayload = '{"name":"' + $PortfolioName + '","mode":"independent","budget":600000}'
    $SaveRaw = & $CurlCmd -s -X POST "$ApiBase/api/v1/portfolios" -H "Content-Type: application/json" -d $SavePayload
    Format-JsonOutput $SaveRaw

    if ($SaveRaw -like "*`"name`":`"$PortfolioName`"*") {
        Write-Host "--> Test 3 Passed: Portfolio '$PortfolioName' saved to database" -ForegroundColor Green
    } else {
        Write-Host "--> Test 3 Failed: Portfolio save did not return expected name" -ForegroundColor Red
    }
}
finally {
    # ------------------------------------------------------------------------------
    # Guaranteed Cleanup: Delete created data
    # ------------------------------------------------------------------------------
    Write-Host ""
    Write-Host "=======================================================" -ForegroundColor Cyan
    Write-Host "               CLEANING UP TEST DATA                   " -ForegroundColor Cyan
    Write-Host "=======================================================" -ForegroundColor Cyan

    # 1. Delete created portfolio
    $DelHttpCode = & $CurlCmd -s -o /dev/null -w "%{http_code}" -X DELETE "$ApiBase/api/v1/portfolios/$PortfolioName" 2>$null
    if ($DelHttpCode -eq "204" -or $DelHttpCode -eq "200") {
        Write-Host "[CLEANUP PASS] " -ForegroundColor Green -NoNewline
        Write-Host "Deleted portfolio '$PortfolioName' via API (HTTP $DelHttpCode)"
    } elseif ($DelHttpCode -eq "404") {
        Write-Host "[CLEANUP INFO] " -ForegroundColor Blue -NoNewline
        Write-Host "Portfolio '$PortfolioName' not found or already deleted"
    } else {
        Write-Host "[CLEANUP WARN] " -ForegroundColor Yellow -NoNewline
        Write-Host "DELETE /api/v1/portfolios/$PortfolioName returned HTTP $DelHttpCode"
    }

    # 2. Delete test evaluation row from Postgres if psql was available
    if ($PrevEvalId -and $DatabaseUrl -and (Get-Command psql -ErrorAction SilentlyContinue)) {
        try {
            $delEvals = & psql $DatabaseUrl -t -A -c "DELETE FROM evaluations WHERE id > $PrevEvalId RETURNING id;" 2>$null
            if ($delEvals) {
                $count = ($delEvals | Measure-Object).Count
                Write-Host "[CLEANUP PASS] " -ForegroundColor Green -NoNewline
                Write-Host "Deleted $count evaluation history row(s) from Postgres"
            }
        } catch {}
    }

    # 3. Verify deletion
    $VerifyCode = & $CurlCmd -s -o /dev/null -w "%{http_code}" "$ApiBase/api/v1/portfolios/$PortfolioName" 2>$null
    if ($VerifyCode -eq "404") {
        Write-Host "[CLEANUP VERIFIED] " -ForegroundColor Green -NoNewline
        Write-Host "Portfolio '$PortfolioName' verified removed from Postgres (404 Not Found)"
    }
    Write-Host "=======================================================" -ForegroundColor Cyan
}
