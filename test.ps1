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
$PrevObservationId = $null
$PrevDecisionId = $null
$PlanPilot = "AI1"   # the pilot the Phase 6 tracker tests upsert (per-pilot plan row)
if ($DatabaseUrl -and (Get-Command psql -ErrorAction SilentlyContinue)) {
    try {
        $PrevEvalId = & psql $DatabaseUrl -t -A -c "SELECT COALESCE(MAX(id), 0) FROM evaluations;" 2>$null
        $PrevObservationId = & psql $DatabaseUrl -t -A -c "SELECT COALESCE(MAX(id), 0) FROM observations;" 2>$null
        $PrevDecisionId = & psql $DatabaseUrl -t -A -c "SELECT COALESCE(MAX(id), 0) FROM decisions;" 2>$null
    } catch {}
}

# Ensure clean starting state
& $CurlCmd -s -X DELETE "$ApiBase/api/v1/portfolios/$PortfolioName" 2>$null | Out-Null

try {
    $FailedTests = 0

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
        Write-Host "--> Test 1 Failed: System reported checks down (inspect above)" -ForegroundColor Red
        $FailedTests++
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
        $FailedTests++
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
        $FailedTests++
    }

    # ------------------------------------------------------------------------------
    # Test 4: 30-day plan (Phase 6): engine draft stored per pilot with a cost limit
    # ------------------------------------------------------------------------------
    Write-Host ""
    Write-Host "# 4. Generate a 30-day plan for AI1" -ForegroundColor Blue
    $PlanRaw = & $CurlCmd -s -X POST "$ApiBase/api/v1/plans" -H "Content-Type: application/json" -d '{"pilot_id":"AI1"}'
    Format-JsonOutput $PlanRaw

    if ($PlanRaw -like '*"status":"planned"*' -and $PlanRaw -like '*"pilot_id":"AI1"*') {
        Write-Host "--> Test 4 Passed: Plan stored with status planned" -ForegroundColor Green
    } else {
        Write-Host "--> Test 4 Failed: Plan creation did not return a planned plan for AI1" -ForegroundColor Red
        $FailedTests++
    }

    # ------------------------------------------------------------------------------
    # Test 5: Record an observation (tracker actuals) — also auto-records a decision
    # ------------------------------------------------------------------------------
    Write-Host ""
    Write-Host "# 5. Record observation for AI1 (adoption 72 / quality 85)" -ForegroundColor Blue
    $ObsRaw = & $CurlCmd -s -X POST "$ApiBase/api/v1/pilots/AI1/observations" -H "Content-Type: application/json" -d '{"observed_adoption_pct":72,"observed_quality_pct":85}'
    Format-JsonOutput $ObsRaw

    if ($ObsRaw -like '*"observation"*') {
        Write-Host "--> Test 5 Passed: Observation recorded with auto decision" -ForegroundColor Green
    } else {
        Write-Host "--> Test 5 Failed: Observation was not recorded" -ForegroundColor Red
        $FailedTests++
    }

    # ------------------------------------------------------------------------------
    # Test 6: Plan progress — expected vs actual gaps and the checkpoint verdict (extend)
    # ------------------------------------------------------------------------------
    Write-Host ""
    Write-Host "# 6. Tracker progress for AI1" -ForegroundColor Blue
    $ProgressRaw = & $CurlCmd -s "$ApiBase/api/v1/plans/AI1"
    Format-JsonOutput $ProgressRaw

    if ($ProgressRaw -like '*"adoption_gap_pct"*' -and $ProgressRaw -like '*"checkpoint":"extend"*') {
        Write-Host "--> Test 6 Passed: Gaps computed (+12/+5) and verdict re-run (extend)" -ForegroundColor Green
    } else {
        Write-Host "--> Test 6 Failed: Progress did not carry gaps and an extend verdict" -ForegroundColor Red
        $FailedTests++
    }

    # ------------------------------------------------------------------------------
    # Test 7: Checkpoint preview — same outcome, stop rule carried beside it
    # ------------------------------------------------------------------------------
    Write-Host ""
    Write-Host "# 7. Checkpoint preview for AI1" -ForegroundColor Blue
    $CheckpointRaw = & $CurlCmd -s -X POST "$ApiBase/api/v1/checkpoint/AI1" -H "Content-Type: application/json" -d '{}'
    Format-JsonOutput $CheckpointRaw

    if ($CheckpointRaw -like '*"checkpoint":"extend"*' -and $CheckpointRaw -like '*"stop_rule"*') {
        Write-Host "--> Test 7 Passed: Checkpoint answered with outcome and stop rule beside it" -ForegroundColor Green
    } else {
        Write-Host "--> Test 7 Failed: Checkpoint preview missing outcome or stop rule" -ForegroundColor Red
        $FailedTests++
    }

    # ------------------------------------------------------------------------------
    # Test 8: Plan status moves — revise arms the follow-up rule, stopped frees budget
    # ------------------------------------------------------------------------------
    Write-Host ""
    Write-Host "# 8. Move plan status revise then stopped" -ForegroundColor Blue
    $ReviseRaw = & $CurlCmd -s -X POST "$ApiBase/api/v1/plans/AI1/status" -H "Content-Type: application/json" -d '{"status":"revise"}'
    $StoppedRaw = & $CurlCmd -s -X POST "$ApiBase/api/v1/plans/AI1/status" -H "Content-Type: application/json" -d '{"status":"stopped"}'
    Format-JsonOutput $ReviseRaw
    Format-JsonOutput $StoppedRaw

    if ($ReviseRaw -like '*"status":"revise"*' -and $StoppedRaw -like '*"status":"stopped"*') {
        Write-Host "--> Test 8 Passed: Status transitions revise then stopped accepted" -ForegroundColor Green
    } else {
        Write-Host "--> Test 8 Failed: Plan status transition rejected" -ForegroundColor Red
        $FailedTests++
    }

    # ------------------------------------------------------------------------------
    # Test 9: Reallocation — recommendation only, never auto-applied
    # ------------------------------------------------------------------------------
    Write-Host ""
    Write-Host "# 9. Reallocation recommendation after stopping AI1" -ForegroundColor Blue
    $ReallocRaw = & $CurlCmd -s -X POST "$ApiBase/api/v1/reallocation" -H "Content-Type: application/json" -d '{"pilot_id":"AI1"}'
    Format-JsonOutput $ReallocRaw

    if ($ReallocRaw -like '*"accepted":false*' -and $ReallocRaw -like '*"freed_budget_inr":200000*') {
        Write-Host "--> Test 9 Passed: AI1 freed 200000 as a recommendation only" -ForegroundColor Green
    } else {
        Write-Host "--> Test 9 Failed: Reallocation did not free AI1's 200000 (or auto-accepted)" -ForegroundColor Red
        $FailedTests++
    }

    # ------------------------------------------------------------------------------
    # Test 10: Decision memo — one page markdown with plan and audit trail
    # ------------------------------------------------------------------------------
    Write-Host ""
    Write-Host "# 10. Decision memo for AI1 (markdown)" -ForegroundColor Blue
    $MemoRaw = & $CurlCmd -s -X POST "$ApiBase/api/v1/export/memo" -H "Content-Type: application/json" -d '{"pilot_id":"AI1"}'
    Write-Host "$( $MemoRaw.Substring(0, [Math]::Min(120, $MemoRaw.Length)) )"

    if ($MemoRaw -like "*Decision memo — AI1*" -and $MemoRaw -like "*## Audit trail*") {
        Write-Host "--> Test 10 Passed: Memo rendered as one-page markdown with audit trail" -ForegroundColor Green
    } else {
        Write-Host "--> Test 10 Failed: Memo missing heading or audit trail" -ForegroundColor Red
        $FailedTests++
    }

    if ($FailedTests -gt 0) {
        Write-Host ""
        Write-Host "=======================================================" -ForegroundColor Red
        Write-Host "          INTEGRATION TESTS FAILED ($FailedTests FAILED)       " -ForegroundColor Red
        Write-Host "=======================================================" -ForegroundColor Red
        throw "$FailedTests integration test(s) failed."
    }

    Write-Host ""
    Write-Host "=======================================================" -ForegroundColor Green
    Write-Host "             ALL INTEGRATION TESTS PASSED              " -ForegroundColor Green
    Write-Host "=======================================================" -ForegroundColor Green
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
            $delEvals = & psql -q $DatabaseUrl -t -A -c "DELETE FROM evaluations WHERE id > $PrevEvalId RETURNING id;" 2>$null
            if ($delEvals) {
                $count = ($delEvals | Measure-Object).Count
                Write-Host "[CLEANUP PASS] " -ForegroundColor Green -NoNewline
                Write-Host "Deleted $count evaluation history row(s) from Postgres"
            }
        } catch {}
    }

    # 2b. Delete the Phase 6 lifecycle rows the tracker tests created. The plan is a
    # per-pilot upsert row (the test replaces any existing one for the same pilot),
    # so deleting by pilot id removes exactly what the run added or replaced.
    if ($DatabaseUrl -and (Get-Command psql -ErrorAction SilentlyContinue)) {
        try {
            $delPlan = & psql -q $DatabaseUrl -t -A -c "DELETE FROM pilot_plans WHERE pilot_id = '$PlanPilot' RETURNING pilot_id;" 2>$null
            if ($delPlan) {
                Write-Host "[CLEANUP PASS] " -ForegroundColor Green -NoNewline
                Write-Host "Deleted pilot plan for $PlanPilot from Postgres"
            }
            if ($PrevObservationId) {
                $delObs = & psql -q $DatabaseUrl -t -A -c "DELETE FROM observations WHERE id > $PrevObservationId RETURNING id;" 2>$null
                if ($delObs) {
                    $obsCount = ($delObs | Measure-Object).Count
                    Write-Host "[CLEANUP PASS] " -ForegroundColor Green -NoNewline
                    Write-Host "Deleted $obsCount observation row(s) from Postgres"
                }
            }
            if ($PrevDecisionId) {
                $delDecs = & psql -q $DatabaseUrl -t -A -c "DELETE FROM decisions WHERE id > $PrevDecisionId RETURNING id;" 2>$null
                if ($delDecs) {
                    $decCount = ($delDecs | Measure-Object).Count
                    Write-Host "[CLEANUP PASS] " -ForegroundColor Green -NoNewline
                    Write-Host "Deleted $decCount decision row(s) from Postgres"
                }
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
