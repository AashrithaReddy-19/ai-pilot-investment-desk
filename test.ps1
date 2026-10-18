# ==============================================================================
# Integration Test Script for C23 Backend & Decision Engine (Windows PowerShell)
# PowerShell twin of test.sh: the same 24 checks and the same cleanup of every row it created.
#
#   .\test.ps1                                   gateway on http://localhost:8080, AUTH_MODE=off
#   $env:API_BASE = "http://host:8090"; .\test.ps1
#   $env:C23_TOKEN = "c23_..."; $env:C23_WORKSPACE_ID = "<uuid>"; .\test.ps1
#                                                a gateway with AUTH_MODE=required (token from
#                                                `go run ./cmd/api bootstrap`; it needs the admin role)
#   $env:DATABASE_URL = "postgres://..."         database for cleanup (default: backend\.env)
#
# Tests 1-10 cover evaluation, portfolios and the Phase 6 lifecycle; 11-24 cover the engine's
# economics/decision/dashboard/pricing endpoints, stored proposals and versions, budget settings,
# workspace snapshots (stale detection and replay), approvals, stored uploads, the audit trail and
# the security layer. Needs curl.exe (built into Windows 10+). Works in Windows PowerShell 5.1 and 7+.
# ==============================================================================

[CmdletBinding()]
param()

$ErrorActionPreference = "Continue"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $ScriptDir

$ApiBase = if ($env:API_BASE) { $env:API_BASE.TrimEnd("/") } else { "http://localhost:8080" }
$Api = "$ApiBase/api/v1"
$PortfolioName = "run_1"
$RunId = "{0}{1}" -f [DateTimeOffset]::UtcNow.ToUnixTimeSeconds(), $PID
$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "c23-test-$RunId"
New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null

$CurlCmd = "curl.exe"
if (-not (Get-Command "curl.exe" -ErrorAction SilentlyContinue)) { $CurlCmd = "curl" }

# Auth headers: only sent when a token is given (AUTH_MODE=required)
$AuthArgs = @()
if ($env:C23_TOKEN) { $AuthArgs += @("-H", "Authorization: Bearer $($env:C23_TOKEN)") }
if ($env:C23_WORKSPACE_ID) { $AuthArgs += @("-H", "X-Workspace-ID: $($env:C23_WORKSPACE_ID)") }

# ---------------------------------------------------------------- HTTP helpers
# Bodies go through a temp file: Windows PowerShell mangles quotes in native command arguments.
$BodyFile = Join-Path $TmpDir "body.json"
$OutFile = Join-Path $TmpDir "out.txt"

function Invoke-Api {
    param([string]$Method, [string]$Path, [string]$Body = $null, [switch]$Raw, [string[]]$Extra = @())
    $cargs = @("-s", "-X", $Method) + $AuthArgs + $Extra
    if ($Body) {
        [System.IO.File]::WriteAllText($BodyFile, $Body, (New-Object System.Text.UTF8Encoding($false)))
        $cargs += @("-H", "Content-Type: application/json", "--data-binary", "@$BodyFile")
    }
    $cargs += @("-o", $OutFile, "-w", "%{http_code}", "$Api$Path")
    $code = & $CurlCmd @cargs
    $text = if (Test-Path $OutFile) { [System.IO.File]::ReadAllText($OutFile, [System.Text.Encoding]::UTF8) } else { "" }
    Remove-Item $OutFile -ErrorAction SilentlyContinue
    $json = $null
    if (-not $Raw -and $text) { try { $json = $text | ConvertFrom-Json } catch { $json = $null } }
    return [pscustomobject]@{ Code = [string]$code; Text = $text; Json = $json }
}

function Invoke-Upload {
    param([string]$FilePath, [string]$Kind = "balance_sheet")
    $cargs = @("-s", "-X", "POST") + $AuthArgs + @("-F", "kind=$Kind", "-F", "file=@$FilePath", "-o", $OutFile, "-w", "%{http_code}", "$Api/uploads")
    $code = & $CurlCmd @cargs
    $text = if (Test-Path $OutFile) { [System.IO.File]::ReadAllText($OutFile, [System.Text.Encoding]::UTF8) } else { "" }
    Remove-Item $OutFile -ErrorAction SilentlyContinue
    $json = $null
    try { $json = $text | ConvertFrom-Json } catch {}
    return [pscustomobject]@{ Code = [string]$code; Text = $text; Json = $json }
}

function Show-Json($obj) {
    if ($null -eq $obj) { return }
    $s = $obj | ConvertTo-Json -Depth 6 -Compress
    if ($s.Length -gt 600) { $s = $s.Substring(0, 600) + " ..." }
    Write-Host $s
}

function Get-Kpis($dashboard) {
    $map = @{}
    foreach ($k in $dashboard.kpis) { $map[$k.key] = $k.value }
    return $map
}

function All-True($values) {
    foreach ($v in $values) { if (-not $v) { return $false } }
    return @($values).Count -gt 0
}

# ---------------------------------------------------------------- reporting
$script:FailedTests = 0
$script:PassedTests = 0
function Pass([int]$n, [string]$msg) { Write-Host "--> Test $n Passed: $msg" -ForegroundColor Green; $script:PassedTests++ }
function Fail([int]$n, [string]$msg) { Write-Host "--> Test $n Failed: $msg" -ForegroundColor Red; $script:FailedTests++ }
function Title([string]$msg) { Write-Host ""; Write-Host "# $msg" -ForegroundColor Blue }

# ---------------------------------------------------------------- database (cleanup only)
$DatabaseUrl = $env:DATABASE_URL
if (-not $DatabaseUrl -and (Test-Path "backend\.env")) {
    Get-Content "backend\.env" | ForEach-Object {
        $line = $_.Trim()
        if ($line -like "DATABASE_URL=*") { $DatabaseUrl = $line.Substring("DATABASE_URL=".Length).Trim('"') }
    }
}
$HasPsql = [bool]($DatabaseUrl -and (Get-Command psql -ErrorAction SilentlyContinue))
function Invoke-Sql([string]$query) {
    if (-not $HasPsql) { return $null }
    try { return (& psql -q $DatabaseUrl -t -A -c $query 2>$null) } catch { return $null }
}
function Count-Rows($output) { return @($output | Where-Object { $_ -and $_.Trim() -ne "" }).Count }

$PlanPilot = "AI1"
$PrevEvalId = Invoke-Sql "SELECT COALESCE(MAX(id), 0) FROM evaluations;"
$PrevObservationId = Invoke-Sql "SELECT COALESCE(MAX(id), 0) FROM observations;"
$PrevDecisionId = Invoke-Sql "SELECT COALESCE(MAX(id), 0) FROM decisions;"
$PrevAuditId = Invoke-Sql "SELECT COALESCE(MAX(id), 0) FROM audit_events;"

$ProposalIds = New-Object System.Collections.ArrayList
$UploadIds = New-Object System.Collections.ArrayList
$SnapshotIds = New-Object System.Collections.ArrayList
$PrevSettings = $null

# ---------------------------------------------------------------- preflight
Write-Host "=======================================================" -ForegroundColor Cyan
Write-Host "             API INTEGRATION TESTS                     " -ForegroundColor Cyan
Write-Host "=======================================================" -ForegroundColor Cyan

$Health = $null
try { $Health = Invoke-RestMethod -Uri "$ApiBase/health" -TimeoutSec 3 -ErrorAction Stop } catch {}
if (-not $Health) {
    Write-Host "[ERROR] Backend is not reachable at $ApiBase!" -ForegroundColor Red
    Write-Host "Please ensure both services are running before testing:" -ForegroundColor Yellow
    Write-Host "  Terminal 1: cd decision-engine; .\start.ps1"
    Write-Host "  Terminal 2: cd backend; .\start.ps1"
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
    exit 1
}
$AuthMode = if ($Health.auth_mode) { $Health.auth_mode } else { "off" }
if ($AuthMode -eq "required" -and -not $env:C23_TOKEN) {
    Write-Host "[ERROR] The gateway runs with AUTH_MODE=required: set `$env:C23_TOKEN (and `$env:C23_WORKSPACE_ID)." -ForegroundColor Red
    Write-Host "  cd backend; go run ./cmd/api bootstrap -email you@example.com"
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
    exit 1
}
Write-Host "Gateway $ApiBase - auth $AuthMode - run id $RunId" -ForegroundColor Cyan

Invoke-Api DELETE "/portfolios/$PortfolioName" | Out-Null

try {
    # --------------------------------------------------------------------------
    Title "1. Readiness check (verifies Postgres, FastAPI, and Redis status)"
    $Ready = $null
    try { $Ready = Invoke-RestMethod -Uri "$ApiBase/ready" -TimeoutSec 5 -ErrorAction Stop } catch {
        try { $Ready = $_.ErrorDetails.Message | ConvertFrom-Json } catch {}
    }
    Show-Json $Ready
    if ($Ready -and $Ready.ready -eq $true) { Pass 1 "System is READY" } else { Fail 1 "System reported checks down (inspect above)" }

    # --------------------------------------------------------------------------
    Title "2. Evaluate both modes (independent + conservative) and store evaluation history"
    $r = Invoke-Api POST "/evaluate" "{}"
    Show-Json @{ evaluation_id = $r.Json.evaluation_id; independent = $r.Json.independent.selected; conservative = $r.Json.conservative.selected }
    if ($r.Json -and $r.Json.independent -and $r.Json.conservative) { Pass 2 "Dual-mode evaluation computed successfully" } else { Fail 2 "Did not receive expected evaluation shape" }

    # --------------------------------------------------------------------------
    Title "3. Save a portfolio to Postgres"
    $r = Invoke-Api POST "/portfolios" ('{"name":"' + $PortfolioName + '","mode":"independent","budget":600000}')
    if ($r.Json -and $r.Json.name -eq $PortfolioName) { Pass 3 "Portfolio '$PortfolioName' saved to database" } else { Fail 3 "Portfolio save did not return expected name: $($r.Text)" }

    # --------------------------------------------------------------------------
    Title "4. Generate a 30-day plan for AI1"
    $r = Invoke-Api POST "/plans" '{"pilot_id":"AI1"}'
    Show-Json $r.Json.plan
    if ($r.Json -and $r.Json.plan.status -eq "planned" -and $r.Json.plan.pilot_id -eq "AI1") { Pass 4 "Plan stored with status planned" } else { Fail 4 "Plan creation did not return a planned plan for AI1" }

    # --------------------------------------------------------------------------
    Title "5. Record observation for AI1 (adoption 72 / quality 85)"
    $r = Invoke-Api POST "/pilots/AI1/observations" '{"observed_adoption_pct":72,"observed_quality_pct":85}'
    if ($r.Json -and $r.Json.observation.id) { Pass 5 "Observation recorded with auto decision ($($r.Json.decision.decision))" } else { Fail 5 "Observation was not recorded" }

    # --------------------------------------------------------------------------
    Title "6. Tracker progress for AI1"
    $r = Invoke-Api GET "/plans/AI1"
    Show-Json @{ actual = $r.Json.actual; gaps = $r.Json.gaps; checkpoint = $r.Json.verdict.checkpoint }
    if ($r.Json -and $null -ne $r.Json.gaps.adoption_gap_pct -and $r.Json.verdict.checkpoint -eq "extend" -and $r.Json.actual.adoption_pct -eq 72) {
        Pass 6 "Gaps computed (+12/+5) and verdict re-run (extend)"
    } else { Fail 6 "Progress did not carry gaps and an extend verdict" }

    # --------------------------------------------------------------------------
    Title "7. Checkpoint preview for AI1"
    $r = Invoke-Api POST "/checkpoint/AI1" "{}"
    if ($r.Json -and $r.Json.checkpoint -eq "extend" -and $r.Json.stop_rule) { Pass 7 "Checkpoint answered with outcome and stop rule beside it" } else { Fail 7 "Checkpoint preview missing outcome or stop rule" }

    # --------------------------------------------------------------------------
    Title "8. Move plan status revise then stopped"
    $rev = Invoke-Api POST "/plans/AI1/status" '{"status":"revise"}'
    $stp = Invoke-Api POST "/plans/AI1/status" '{"status":"stopped"}'
    if ($rev.Json.status -eq "revise" -and $stp.Json.status -eq "stopped") { Pass 8 "Status transitions revise then stopped accepted" } else { Fail 8 "Plan status transition rejected" }

    # --------------------------------------------------------------------------
    Title "9. Reallocation recommendation after stopping AI1"
    $r = Invoke-Api POST "/reallocation" '{"pilot_id":"AI1"}'
    Show-Json @{ accepted = $r.Json.accepted; freed_budget_inr = $r.Json.freed_budget_inr }
    if ($r.Json -and $r.Json.accepted -eq $false -and $r.Json.freed_budget_inr -eq 200000) { Pass 9 "AI1 freed 200000 as a recommendation only" } else { Fail 9 "Reallocation did not free AI1's 200000 (or auto-accepted)" }

    # --------------------------------------------------------------------------
    Title "10. Decision memo for AI1 (markdown)"
    $r = Invoke-Api POST "/export/memo" '{"pilot_id":"AI1"}' -Raw
    ($r.Text -split "`n" | Select-Object -First 6) | ForEach-Object { Write-Host $_ }
    if ($r.Text -match "# Decision memo .{1,4}AI1" -and $r.Text -match "## Audit trail") { Pass 10 "Memo rendered as one-page markdown with audit trail" } else { Fail 10 "Memo missing heading or audit trail" }

    # ==========================================================================
    # Engine rules version 7+: economics, decisions, dashboard, pricing (stateless pass-through)
    # ==========================================================================
    $Example = '{"proposals":[{"name":"A","setup_cost_inr":200000,"annual_benefit_inr":180000,"ai_running_cost_inr":60000},' +
        '{"name":"B","setup_cost_inr":500000,"annual_benefit_inr":400000,"ai_running_cost_inr":150000},' +
        '{"name":"C","setup_cost_inr":100000,"annual_benefit_inr":90000,"ai_running_cost_inr":70000}],' +
        '"budget":700000,"extension":{"defaults":{"discount_rate":0}}}'

    Title "11. The gateway's cache key follows the engine's own rules version"
    Show-Json $Health
    $gwRv = $Health.rules_version
    if ($gwRv -and $gwRv -ne "fallback" -and $Ready -and $gwRv -eq $Ready.rules_version) {
        Pass 11 "Gateway reports engine rules version $gwRv (nobody sets RULES_VERSION by hand)"
    } else { Fail 11 "Gateway did not report the engine's rules version (health=$gwRv ready=$($Ready.rules_version))" }

    Title "12. Executive dashboard for the plan's worked example (A, B, C at 7 L)"
    $r = Invoke-Api POST "/views/dashboard" $Example
    $k = if ($r.Json) { Get-Kpis $r.Json } else { @{} }
    Show-Json @{ headline = $r.Json.summary.headline; kpis = $k; consistency = @($r.Json.consistency | ForEach-Object { $_.ok }) }
    if ($r.Json -and $k["net_annual_savings_inr"] -eq 370000 -and $k["profit_inr"] -eq 410000 -and $k["breakeven_month"] -eq 23 `
        -and (All-True @($r.Json.consistency | ForEach-Object { $_.ok })) -and @($r.Json.charts.series_order).Count -eq 3) {
        Pass 12 "KPIs match the worked example (3.7 L/yr, 4.1 L profit, month 23) and every consistency check holds"
    } else { Fail 12 "Dashboard KPIs or consistency checks are wrong" }

    Title "13. Decision ranking, yearly savings and the decision report"
    $dec = Invoke-Api POST "/decision" $Example
    $yr = Invoke-Api POST "/views/yearly-savings" $Example
    $rep = Invoke-Api POST "/export/decision-report" $Example -Raw
    $aYears = @(($yr.Json.proposals | Where-Object { $_.id -eq "A" }).years | ForEach-Object { $_.net_inr })
    Show-Json @{ overall = $dec.Json.overall.outcome; ranking = @($dec.Json.ranking | ForEach-Object { "$($_.id)=$($_.score)/$($_.outcome)" }); a_years = $aYears }
    if ($dec.Json -and $dec.Json.overall.outcome -and @($dec.Json.ranking).Count -eq 3 -and $dec.Json.decisions.C.outcome -eq "renegotiate" `
        -and ($aYears -join ",") -eq "-80000,120000,120000" -and $rep.Code -eq "200" -and $rep.Text -match "INR 370,000") {
        Pass 13 "Ranking + outcomes, A's yearly net (-80k, 120k, 120k) and the report agree"
    } else { Fail 13 "Decision, yearly view or report did not answer as expected (report HTTP $($rep.Code))" }

    Title "14. Pricing plans priced against a backtested usage forecast"
    $r = Invoke-Api POST "/pricing/optimise" ('{"history":[100,110,120,130,140,150],"months":3,"plans":[' +
        '{"name":"payg","rule":{"type":"per_unit","unit_price":1}},' +
        '{"name":"commit","rule":{"type":"commitment","committed_units":200,"commit_price":150,"overage_unit_price":2}}]}')
    Show-Json @{ recommended = $r.Json.recommended; robust = $r.Json.robust_choice; method = $r.Json.forecast.method; base = $r.Json.forecast.base }
    if ($r.Json -and $r.Json.forecast.method -eq "linear_trend" -and (@($r.Json.forecast.base) -join ",") -eq "160,170,180" -and $r.Json.recommended) {
        Pass 14 "Linear trend won the backtest (160, 170, 180) and a plan was recommended"
    } else { Fail 14 "Usage forecast or plan choice missing" }

    # ==========================================================================
    # Stored proposals, budget settings and workspace snapshots
    # ==========================================================================
    Title "15. Stored proposals with versioned inputs"
    $pa = Invoke-Api POST "/proposals" ('{"name":"IT invoice extraction ' + $RunId + '","inputs":{"setup_cost_inr":200000,"annual_benefit_inr":180000,' +
        '"ai_running_cost_inr":60000,"readiness":"high","overlap_group":"finance",' +
        '"baseline":{"volume_per_month":2000,"minutes_per_unit":6,"loaded_cost_per_hour":1000}}}')
    $pb = Invoke-Api POST "/proposals" ('{"name":"IT ticket triage ' + $RunId + '","inputs":{"setup_cost_inr":500000,"annual_benefit_inr":400000,' +
        '"ai_running_cost_inr":150000,"readiness":"medium"}}')
    $PaId = $pa.Json.id; $PbId = $pb.Json.id
    if ($PaId) { [void]$ProposalIds.Add($PaId) }
    if ($PbId) { [void]$ProposalIds.Add($PbId) }
    $notes = Invoke-Api PATCH "/proposals/$PbId" '{"notes":"notes do not bump the version"}'
    if ($PaId -and $PbId -and $pa.Json.version -eq 1 -and $notes.Json.version -eq 1) {
        Pass 15 "Two proposals stored at inputs version 1; a notes edit keeps the version"
    } else { Fail 15 "Proposals not stored or versioned as expected: $($pa.Text) $($pb.Text)" }

    Title "16. Observation linked to a proposal feeds the next engine run"
    $link = Invoke-Api POST "/pilots/AI1/observations" ('{"observed_adoption_pct":72,"observed_quality_pct":90,"proposal_id":"' + $PaId + '"}')
    $foreign = Invoke-Api POST "/pilots/AI1/observations" '{"observed_adoption_pct":50,"proposal_id":"00000000-0000-0000-0000-00000000dead"}'
    if ($link.Json -and $link.Json.observation.proposal_id -eq $PaId -and $foreign.Code -eq "422") {
        Pass 16 "Observation linked to the proposal; an unknown proposal id is refused (422)"
    } else { Fail 16 "Proposal link not stored, or unknown proposal accepted (HTTP $($foreign.Code))" }

    Title "17. Workspace budget settings (validated, restored after the run)"
    $prev = Invoke-Api GET "/settings"
    if ($prev.Code -eq "200" -and $prev.Text.Trim().StartsWith("{")) { $PrevSettings = $prev.Text }
    $set = Invoke-Api PUT "/settings" '{"budget":700000,"budget_set":{"total_inr":1500000,"contingency_pct":10},"defaults":{"discount_rate":0}}'
    $badSet = Invoke-Api PUT "/settings" '{"budget":-1}'
    Show-Json $set.Json
    if ($set.Json -and $set.Json.budget -eq 700000 -and $set.Json.budget_set.total_inr -eq 1500000 -and $badSet.Code -eq "422") {
        Pass 17 "Settings saved; a negative budget is rejected (422)"
    } else { Fail 17 "Settings not saved or not validated (bad PUT gave HTTP $($badSet.Code))" }

    Title "18. Dashboard from the stored proposals, kept as a reproducible snapshot"
    $ws = Invoke-Api POST "/workspace/dashboard" ('{"proposal_ids":["' + $PaId + '","' + $PbId + '"],"ai_burn_to_date":1550000}')
    $SnapId = $ws.Json.snapshot.id
    if ($SnapId) { [void]$SnapshotIds.Add($SnapId) }
    $replay = Invoke-Api POST "/snapshots/$SnapId/replay"
    $wsK = if ($ws.Json) { Get-Kpis $ws.Json.result } else { @{} }
    $aOutcome = @($ws.Json.result.proposals | Where-Object { $_.id -like "IT invoice*" } | ForEach-Object { $_.outcome })
    Show-Json @{ snapshot = $SnapId; rules_version = $ws.Json.snapshot.rules_version; headline = $ws.Json.result.summary.headline; replay = $replay.Json.matches }
    if ($SnapId -and @($ws.Json.snapshot.proposals).Count -eq 2 -and (All-True @($ws.Json.result.consistency | ForEach-Object { $_.ok })) `
        -and $wsK["ai_burn_to_date_inr"] -eq 1550000 -and ($aOutcome -join ",") -eq "limited_pilot" -and $replay.Json.matches -eq $true) {
        Pass 18 "Snapshot stored with both proposal versions; the linked observation moved A to limited_pilot; replay reproduces it exactly"
    } else { Fail 18 "Workspace dashboard, snapshot or replay wrong: $($replay.Text)" }

    Title "19. Editing inputs bumps the version and marks the snapshot stale"
    $v2 = Invoke-Api PATCH "/proposals/$PbId" '{"inputs":{"setup_cost_inr":450000,"annual_benefit_inr":400000,"ai_running_cost_inr":150000}}'
    $stale = Invoke-Api GET "/snapshots/$SnapId"
    Show-Json @{ stale = $stale.Json.stale; reasons = $stale.Json.staleness.reasons }
    if ($v2.Json.version -eq 2 -and $stale.Json.stale -eq $true -and (@($stale.Json.staleness.reasons)[0] -match "v1 .{1,3} v2")) {
        Pass 19 "Inputs version 2; the snapshot reports it is stale and why"
    } else { Fail 19 "Version bump or stale detection missing (version $($v2.Json.version))" }

    Title "20. Approval on the proposal's current version"
    $appr = Invoke-Api POST "/proposals/$PaId/approvals" ('{"decision":"approved","snapshot_id":"' + $SnapId + '","comment":"start as a limited pilot"}')
    $badAppr = Invoke-Api POST "/proposals/$PaId/approvals" '{"decision":"maybe"}'
    $listAppr = Invoke-Api GET "/proposals/$PaId/approvals"
    Show-Json @{ proposal_version = $appr.Json.proposal_version; decision = $appr.Json.decision; engine_outcome = $appr.Json.engine_outcome }
    if ($appr.Json -and $appr.Json.proposal_version -eq 1 -and $appr.Json.decision -eq "approved" -and $appr.Json.engine_outcome -eq "limited_pilot" `
        -and @($listAppr.Json.approvals)[0].stale -eq $false -and $badAppr.Code -eq "422") {
        Pass 20 "Approval records version 1 and the engine outcome the approver saw; bad decisions are 422"
    } else { Fail 20 "Approval not recorded as expected (bad decision HTTP $($badAppr.Code))" }

    # ==========================================================================
    # Stored uploads: worker, review, corrections, confirm
    # ==========================================================================
    Title "21. Upload a balance sheet: stored, parsed by the worker, corrected, confirmed"
    $Csv = Join-Path $TmpDir "balance-$RunId.csv"
    $csvText = "Account,Monthly spend (INR)`nAI software licences,100000`nRent,25000`nCloud AI platform,80000`nRun $RunId,1`n"
    [System.IO.File]::WriteAllText($Csv, $csvText, (New-Object System.Text.UTF8Encoding($false)))
    $up = Invoke-Upload $Csv
    $UpId = $up.Json.upload.id
    if ($UpId) { [void]$UploadIds.Add($UpId) }
    $status = ""
    for ($i = 0; $i -lt 40; $i++) {
        $status = (Invoke-Api GET "/uploads/$UpId").Json.upload.status
        if ($status -eq "parsed" -or $status -eq "failed") { break }
        Start-Sleep -Milliseconds 500
    }
    $corr = Invoke-Api POST "/uploads/$UpId/corrections" '{"row_ref":4,"field":"amount_inr","value":50000,"note":"per invoice"}'
    $conf = Invoke-Api POST "/uploads/$UpId/confirm" '{"rows":[2,4]}'
    Show-Json @{ status = $conf.Json.upload.status; ai_burn_to_date = $conf.Json.result.ai_burn_to_date; annualised = $conf.Json.result.ai_burn_annualised_inr }
    if ($up.Json -and ([string]$up.Json.upload.sha256).Length -eq 64 -and $status -eq "parsed" `
        -and $corr.Json.original_value -eq 80000 -and $corr.Json.corrected_value -eq 50000 `
        -and $conf.Json.upload.status -eq "confirmed" -and $conf.Json.result.ai_burn_to_date -eq 150000 -and $conf.Json.result.ai_burn_annualised_inr -eq 1800000) {
        Pass 21 "Worker parsed it; the correction kept 80,000 beside 50,000; the engine summed 1.5 L (18 L a year)"
    } else { Fail 21 "Upload pipeline wrong (status=$status): $($conf.Text)" }

    Title "22. Uploads: original download, duplicates and rejected files"
    $dlPath = Join-Path $TmpDir "dl.csv"
    $dlArgs = @("-s") + $AuthArgs + @("-o", $dlPath, "-w", "%{http_code}", "$Api/uploads/$UpId/file")
    $dlCode = & $CurlCmd @dlArgs
    $sameBytes = (Test-Path $dlPath) -and ((Get-FileHash $dlPath).Hash -eq (Get-FileHash $Csv).Hash)
    $dup = (Invoke-Upload $Csv).Json.duplicate
    $pdf = Join-Path $TmpDir "report-$RunId.pdf"
    [System.IO.File]::WriteAllText($pdf, "%PDF-1.4 not a sheet")
    $pdfCode = (Invoke-Upload $pdf).Code
    $notes = Join-Path $TmpDir "notes-$RunId.csv"
    [System.IO.File]::WriteAllText($notes, "Notes`nHello $RunId`n", (New-Object System.Text.UTF8Encoding($false)))
    $bad = Invoke-Upload $notes
    $BadId = $bad.Json.upload.id
    if ($BadId) { [void]$UploadIds.Add($BadId) }
    $badStatus = ""; $badErr = ""
    for ($i = 0; $i -lt 40; $i++) {
        $b = (Invoke-Api GET "/uploads/$BadId").Json.upload
        $badStatus = $b.status; $badErr = $b.last_error
        if ($badStatus -eq "failed") { break }
        Start-Sleep -Milliseconds 500
    }
    Write-Host "download HTTP $dlCode - duplicate=$dup - pdf HTTP $pdfCode - unreadable sheet: $badStatus ($badErr)"
    if ($dlCode -eq "200" -and $sameBytes -and $dup -eq $true -and $pdfCode -eq "415" -and $badStatus -eq "failed" -and $badErr) {
        Pass 22 "Original returned byte for byte; same file is one upload; PDF refused (415); unreadable sheet failed with the engine's reason"
    } else { Fail 22 "Download, duplicate detection or rejection behaved unexpectedly" }

    # ==========================================================================
    # Audit trail and security
    # ==========================================================================
    Title "23. Audit trail records the run's edits, downloads and approvals"
    $audit = Invoke-Api GET "/audit?limit=50"
    $actions = @($audit.Json.events | ForEach-Object { $_.action } | Sort-Object -Unique)
    Write-Host "actions: $($actions -join ', ')"
    $needed = @("upload.confirm", "upload.correct", "upload.download", "approval.create", "settings.update", "snapshot.create")
    $missing = @($needed | Where-Object { $actions -notcontains $_ })
    if ($audit.Json -and $missing.Count -eq 0) {
        Pass 23 "Uploads, corrections, downloads, approvals, settings and snapshots are all in the audit trail"
    } else { Fail 23 "Audit trail is missing actions from this run: $($missing -join ', ')" }

    Title "24. Security: identity, sign-in and workspace isolation"
    $me = Invoke-Api GET "/me"
    Show-Json @{ auth_mode = $me.Json.auth_mode; role = $me.Json.role; workspace = $me.Json.workspace.id }
    if ($AuthMode -eq "required") {
        $sink = Join-Path $TmpDir "sink.txt"
        $noToken = & $CurlCmd -s -o $sink -w "%{http_code}" "$Api/proposals"
        $badToken = & $CurlCmd -s -o $sink -w "%{http_code}" -H "Authorization: Bearer not-a-token" "$Api/proposals"
        $otherWs = & $CurlCmd -s -o $sink -w "%{http_code}" -H "Authorization: Bearer $($env:C23_TOKEN)" -H "X-Workspace-ID: 00000000-0000-0000-0000-00000000beef" "$Api/proposals"
        Write-Host "no token HTTP $noToken - bad token HTTP $badToken - foreign workspace HTTP $otherWs"
        if ($noToken -eq "401" -and $badToken -eq "401" -and $otherWs -eq "403" -and $me.Json.user.email) {
            Pass 24 "No token and bad tokens are 401; another organisation's workspace is 403"
        } else { Fail 24 "Sign-in or workspace isolation did not hold" }
    } else {
        if ($me.Json -and $me.Json.auth_mode -eq "off" -and $me.Json.role -eq "admin") {
            Pass 24 "AUTH_MODE=off: default workspace as admin (rerun with C23_TOKEN against AUTH_MODE=required for the sign-in checks)"
        } else { Fail 24 "/me did not describe the auth-off mode" }
    }

    Write-Host ""
    if ($script:FailedTests -gt 0) {
        Write-Host "=======================================================" -ForegroundColor Red
        Write-Host "   INTEGRATION TESTS FAILED ($($script:FailedTests) failed, $($script:PassedTests) passed)" -ForegroundColor Red
        Write-Host "=======================================================" -ForegroundColor Red
        $global:LASTEXITCODE = 1
    } else {
        Write-Host "=======================================================" -ForegroundColor Green
        Write-Host "   ALL $($script:PassedTests) INTEGRATION TESTS PASSED" -ForegroundColor Green
        Write-Host "=======================================================" -ForegroundColor Green
    }
} finally {
    Write-Host ""
    Write-Host "=======================================================" -ForegroundColor Cyan
    Write-Host "               CLEANING UP TEST DATA                   " -ForegroundColor Cyan
    Write-Host "=======================================================" -ForegroundColor Cyan

    # 1. Portfolio via the API
    $del = Invoke-Api DELETE "/portfolios/$PortfolioName"
    if ($del.Code -eq "204" -or $del.Code -eq "200") { Write-Host "[CLEANUP PASS] Deleted portfolio '$PortfolioName' via API (HTTP $($del.Code))" -ForegroundColor Green }
    elseif ($del.Code -eq "404") { Write-Host "[CLEANUP INFO] Portfolio '$PortfolioName' not found or already deleted" -ForegroundColor Blue }
    else { Write-Host "[CLEANUP WARN] DELETE /api/v1/portfolios/$PortfolioName returned HTTP $($del.Code)" -ForegroundColor Yellow }

    # 2. Proposals via the API (cascades to their evidence and approvals)
    foreach ($pid2 in $ProposalIds) {
        $d = Invoke-Api DELETE "/proposals/$pid2"
        if ($d.Code -eq "204") { Write-Host "[CLEANUP PASS] Deleted proposal $pid2 via API (approvals and evidence cascade)" -ForegroundColor Green }
        else { Write-Host "[CLEANUP WARN] DELETE /api/v1/proposals/$pid2 returned HTTP $($d.Code)" -ForegroundColor Yellow }
    }

    # 3. Budget settings: restore what the workspace had before the run
    if ($PrevSettings) {
        $rs = Invoke-Api PUT "/settings" $PrevSettings
        if ($rs.Code -eq "200") { Write-Host "[CLEANUP PASS] Restored the workspace budget settings" -ForegroundColor Green }
        else { Write-Host "[CLEANUP WARN] Could not restore settings (HTTP $($rs.Code)): $PrevSettings" -ForegroundColor Yellow }
    }

    if ($HasPsql) {
        # 4. Uploads and snapshots have no delete endpoint (they are records): remove this run's rows
        foreach ($u in $UploadIds) {
            if (Count-Rows (Invoke-Sql "DELETE FROM uploads WHERE id = '$u' RETURNING id;")) { Write-Host "[CLEANUP PASS] Deleted upload $u (and its corrections) from Postgres" -ForegroundColor Green }
        }
        foreach ($s in $SnapshotIds) {
            if (Count-Rows (Invoke-Sql "DELETE FROM snapshots WHERE id = '$s' RETURNING id;")) { Write-Host "[CLEANUP PASS] Deleted snapshot $s from Postgres" -ForegroundColor Green }
        }
        # 5. Evaluation history
        if ($PrevEvalId) {
            $n = Count-Rows (Invoke-Sql "DELETE FROM evaluations WHERE id > $PrevEvalId RETURNING id;")
            if ($n -gt 0) { Write-Host "[CLEANUP PASS] Deleted $n evaluation history row(s) from Postgres" -ForegroundColor Green }
        }
        # 6. Phase 6 rows (the plan is a per-pilot upsert, so deleting by pilot id removes exactly this run's row)
        if (Count-Rows (Invoke-Sql "DELETE FROM pilot_plans WHERE pilot_id = '$PlanPilot' RETURNING pilot_id;")) {
            Write-Host "[CLEANUP PASS] Deleted pilot plan for $PlanPilot from Postgres" -ForegroundColor Green
        }
        if ($PrevObservationId) {
            $n = Count-Rows (Invoke-Sql "DELETE FROM observations WHERE id > $PrevObservationId RETURNING id;")
            if ($n -gt 0) { Write-Host "[CLEANUP PASS] Deleted $n observation row(s) from Postgres" -ForegroundColor Green }
        }
        if ($PrevDecisionId) {
            $n = Count-Rows (Invoke-Sql "DELETE FROM decisions WHERE id > $PrevDecisionId RETURNING id;")
            if ($n -gt 0) { Write-Host "[CLEANUP PASS] Deleted $n decision row(s) from Postgres" -ForegroundColor Green }
        }
        # 7. Audit rows written by this run (the application itself never deletes audit rows)
        if ($PrevAuditId) {
            $n = Count-Rows (Invoke-Sql "DELETE FROM audit_events WHERE id > $PrevAuditId RETURNING id;")
            if ($n -gt 0) { Write-Host "[CLEANUP PASS] Deleted $n audit row(s) written by the test run" -ForegroundColor Green }
        }
    } elseif ($UploadIds.Count -gt 0 -or $SnapshotIds.Count -gt 0) {
        Write-Host "[CLEANUP WARN] psql/DATABASE_URL unavailable: uploads ($($UploadIds -join ', ')) and snapshots ($($SnapshotIds -join ', ')) were left in place" -ForegroundColor Yellow
    }

    # 8. Verify the portfolio is gone
    if ((Invoke-Api GET "/portfolios/$PortfolioName").Code -eq "404") {
        Write-Host "[CLEANUP VERIFIED] Portfolio '$PortfolioName' verified removed from Postgres (404 Not Found)" -ForegroundColor Green
    }
    Write-Host "=======================================================" -ForegroundColor Cyan
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}

if ($script:FailedTests -gt 0) { exit 1 }
exit 0
