# Backend smoke test against a RUNNING API (Docker stack or scripts/start_backend.ps1). Windows PowerShell 5.1+.
#   powershell -ExecutionPolicy Bypass -File scripts/smoke_test.ps1 [-BaseUrl http://127.0.0.1:8080] [-ExpectAI ok|degraded|any]
#                                                                  [-ExpectCache ok|degraded|disabled|any] [-SaveExamples]
# Exit code 0 = every check passed. Results: backend/reports/smoke_test.json
param(
  [string]$BaseUrl = 'http://127.0.0.1:8080',
  [ValidateSet('ok', 'degraded', 'disabled', 'any')] [string]$ExpectAI = 'any',
  [ValidateSet('ok', 'degraded', 'disabled', 'any')] [string]$ExpectCache = 'any',
  [switch]$SaveExamples
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$examples = Join-Path $root 'docs\api-examples'
if ($SaveExamples) { New-Item -ItemType Directory -Force $examples | Out-Null }
$stamp = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$results = New-Object System.Collections.ArrayList

function Call([string]$Method, [string]$Path, $Body = $null, [string]$Example = '') {
  $req = [System.Net.HttpWebRequest]::Create($BaseUrl + $Path)
  $req.Method = $Method; $req.ContentType = 'application/json'; $req.Timeout = 20000
  $req.Headers.Add('X-Actor', 'smoke-test')
  $reqText = $null
  if ($null -ne $Body) {
    $reqText = $(if ($Body -is [string]) { $Body } else { $Body | ConvertTo-Json -Depth 10 -Compress })
    $bytes = [Text.Encoding]::UTF8.GetBytes($reqText); $req.ContentLength = $bytes.Length
    $s = $req.GetRequestStream(); $s.Write($bytes, 0, $bytes.Length); $s.Close()
  } elseif ($Method -ne 'GET') { $req.ContentLength = 0 }
  try { $resp = $req.GetResponse() } catch [System.Net.WebException] { $resp = $_.Exception.Response; if ($null -eq $resp) { throw } }
  $code = [int]$resp.StatusCode
  $text = (New-Object IO.StreamReader($resp.GetResponseStream(), [Text.Encoding]::UTF8)).ReadToEnd(); $resp.Close()
  $json = $null; try { $json = $text | ConvertFrom-Json } catch {}
  if ($SaveExamples -and $Example) {
    $reqBody = $null
    if ($reqText) { try { $reqBody = $reqText | ConvertFrom-Json } catch { $reqBody = "RAW (invalid JSON on purpose): $reqText" } }
    $doc = [ordered]@{ request = [ordered]@{ method = $Method; path = $Path; body = $reqBody }; status = $code; response = $json }
    [IO.File]::WriteAllText((Join-Path $examples "$Example.json"), ($doc | ConvertTo-Json -Depth 40), (New-Object Text.UTF8Encoding($false)))
  }
  return @{ code = $code; json = $json; text = $text }
}

function Check([string]$Name, [bool]$Ok, [string]$Detail = '') {
  [void]$results.Add([ordered]@{ check = $Name; pass = $Ok; detail = $Detail })
  $tag = $(if ($Ok) { 'PASS' } else { 'FAIL' })
  Write-Host ("[{0}] {1}{2}" -f $tag, $Name, $(if ($Detail) { " - $Detail" } else { '' }))
}

function M($ev) { return $ev.calculated_metrics.stated_basis }
function Scenario([string]$Id, $Overrides, [string]$Example = '') {
  return (Call 'POST' "/api/v1/pilots/$Id/scenarios" @{ name = 'smoke'; overrides = $Overrides } $Example).json.result.scenario
}

try {
  # --- platform
  $h = Call 'GET' '/health' $null 'health'
  Check 'GET /health 200 + database ok' ($h.code -eq 200 -and $h.json.components.database -eq 'ok') ($h.json.components | ConvertTo-Json -Compress)
  if ($ExpectAI -ne 'any') { Check "AI service status = $ExpectAI" ($h.json.components.ai_service -eq $ExpectAI) $h.json.components.ai_service }
  if ($ExpectCache -ne 'any') { Check "cache status = $ExpectCache" ($h.json.components.redis -eq $ExpectCache) $h.json.components.redis }
  $r = Call 'GET' '/ready' $null 'ready'
  Check 'GET /ready 200' ($r.code -eq 200)
  $imp = Call 'POST' '/api/v1/import/official' $null 'import-official'
  Check 'import official fixture (201, or 409 if already seeded)' ($imp.code -eq 201 -or $imp.code -eq 409) "HTTP $($imp.code)"
  $p = Call 'GET' '/api/v1/pilots' $null 'list-pilots'
  Check 'pilots persisted in PostgreSQL (>= 5)' ($p.code -eq 200 -and $p.json.count -ge 5) "count $($p.json.count)"

  # --- official reference + changed input
  $e = Call 'POST' '/api/v1/pilots/AI1/evaluate' $null 'evaluate-ai1'
  $m = M $e.json.evaluation
  Check 'DEMO1 official reference: net 120000, monthly 10000, payback 20' ($m.net_annual_benefit_inr -eq 120000 -and $m.monthly_net_benefit_inr -eq 10000 -and $m.payback_months -eq 20) "ai_status=$($e.json.ai_status)"
  if ($ExpectAI -ne 'any') { Check "evaluate ai_status = $ExpectAI (deterministic result unaffected)" ($e.json.ai_status -eq $ExpectAI -and $m.payback_months -eq 20) $e.json.ai_status }
  $s = Scenario 'AI1' @{ annual_running_cost_inr = 120000 } 'scenario-running-120000'
  Check 'DEMO2 running 120000: net 60000, monthly 5000, payback 40' ((M $s).net_annual_benefit_inr -eq 60000 -and (M $s).monthly_net_benefit_inr -eq 5000 -and (M $s).payback_months -eq 40)
  Check 'scenario does not modify the official pilot' ((Call 'GET' '/api/v1/pilots/AI1' $null 'get-pilot-ai1').json.annual_running_cost_inr -eq 60000)

  # --- rules
  $s = Scenario 'AI1' @{ actual_adoption_pct = 20; actual_quality_pct = 90 }
  Check 'low adoption 20% (< break-even 33.33%) -> STOP' ($s.decision -eq 'STOP' -and $s.primary_reason -eq 'ADOPTION_BELOW_BREAKEVEN')
  $s = Scenario 'AI1' @{ actual_adoption_pct = 45; actual_quality_pct = 90 }
  Check 'low adoption 45% (< minimum 60%) -> REVISE LOW_ADOPTION' ($s.decision -eq 'REVISE' -and $s.primary_reason -eq 'LOW_ADOPTION')
  $s = Scenario 'AI1' @{ actual_adoption_pct = 70; actual_quality_pct = 60 } 'scenario-quality-60'
  Check 'failed quality 60% (< 80%) -> STOP even with 20-month payback' ($s.decision -eq 'STOP' -and $s.primary_reason -eq 'QUALITY_THRESHOLD_FAILED' -and (M $s).payback_months -eq 20)
  $s = Scenario 'AI1' @{ annual_running_cost_inr = $null } 'scenario-running-null'
  Check 'missing running cost -> payback null (NOT_CALCULABLE), REVISE' ($s.decision -eq 'REVISE' -and $null -eq (M $s).payback_months -and (M $s).payback_status -eq 'NOT_CALCULABLE')
  $s = Scenario 'AI1' @{ annual_running_cost_inr = 200000 }
  Check 'negative economics (running 200000) -> net -20000, NEVER_PAYS_BACK, STOP' ($s.decision -eq 'STOP' -and (M $s).net_annual_benefit_inr -eq -20000 -and (M $s).payback_status -eq 'NEVER_PAYS_BACK')
  $tf = (Call 'POST' '/api/v1/evaluate' @{ id = 'TF1'; task = 't'; setup_cost_inr = 200000; annual_running_cost_inr = 60000; annual_benefit_inr = 180000; benefit_type = 'time freed'; overlap_group = 'g' } 'evaluate-stateless-time-freed').json.evaluation
  Check 'time freed is not cash -> REVIEW TIME_NOT_CASH, cash net unknown' ($tf.decision -eq 'REVIEW' -and $tf.primary_reason -eq 'TIME_NOT_CASH' -and $null -eq $tf.calculated_metrics.cash_basis.net_annual_benefit_inr)

  # --- budget, overlap, optimizer
  $pv = Call 'POST' '/api/v1/preview/portfolio' @{ budget_inr = 600000; pilot_ids = @('AI1', 'AI2', 'AI3', 'AI4') } 'preview-over-budget'
  Check 'budget 600000 vs setup 800000 -> REJECTED BUDGET_EXCEEDED' ($pv.json.assessment.status -eq 'REJECTED' -and ($pv.json.assessment.blockers.code -contains 'BUDGET_EXCEEDED') -and $pv.json.assessment.budget_remaining_inr -eq -200000)
  $ov = Call 'POST' '/api/v1/portfolios' @{ name = "smoke overlap $stamp"; budget_inr = 600000; pilot_ids = @('AI4', 'AI5') } 'create-portfolio-overlap'
  $ova = $ov.json.assessment
  Check 'AI4 + AI5 -> DOUBLE_COUNT_RISK, recognised 180000 (not 360000), NEEDS_REVIEW' ($ov.code -eq 201 -and $ova.overlaps[0].code -eq 'DOUBLE_COUNT_RISK' -and $ova.total_recognized_benefit_inr -eq 180000 -and $ov.json.status -eq 'NEEDS_REVIEW')
  $ap = Call 'POST' "/api/v1/portfolios/$($ov.json.id)/approve" @{ version = $ov.json.version } 'approve-blocked'
  Check 'unresolved overlap blocks approval (409 APPROVAL_BLOCKED)' ($ap.code -eq 409 -and $ap.json.error.code -eq 'APPROVAL_BLOCKED')
  $ol = Call 'GET' "/api/v1/portfolios/$($ov.json.id)/overlaps" $null 'portfolio-overlaps'
  Check 'overlap report: deterministic overlap authoritative, AI advisory' ($ol.code -eq 200 -and $ol.json.overlaps[0].status -eq 'UNRESOLVED') "ai_status=$($ol.json.ai_status), suggestions=$(@($ol.json.ai_suggestions).Count)"
  $rs = Call 'POST' "/api/v1/portfolios/$($ov.json.id)/overlaps/support/resolve" @{ resolution_type = 'CUSTOM_ALLOCATION'; allocations_inr = @{ AI4 = 120000; AI5 = 60000 }; review_note = 'smoke'; version = $ov.json.version } 'resolve-overlap'
  Check 'resolution AI4 120000 + AI5 60000 -> recognised 180000, overlap RESOLVED' ($rs.code -eq 200 -and $rs.json.assessment.total_recognized_benefit_inr -eq 180000 -and $rs.json.assessment.overlaps[0].status -eq 'RESOLVED')
  $bad = Call 'POST' "/api/v1/portfolios/$($ov.json.id)/overlaps/support/resolve" @{ resolution_type = 'CUSTOM_ALLOCATION'; allocations_inr = @{ AI4 = 180000; AI5 = 180000 }; version = $rs.json.version } 'resolve-overlap-invalid'
  Check 'over-allocation (360000 > pool 180000) rejected 422' ($bad.code -eq 422)

  $newId = "SMOKE$stamp"
  $np = Call 'POST' '/api/v1/pilots' @{ id = $newId; task = 'unknown overlap pilot'; setup_cost_inr = 100000; annual_running_cost_inr = 10000; annual_benefit_inr = 80000; benefit_type = 'cashable'; overlap_group = $null; actual_adoption_pct = 80; actual_quality_pct = 90 } 'create-pilot'
  $uo = Call 'POST' '/api/v1/portfolios' @{ name = "smoke unknown overlap $stamp"; budget_inr = 600000; pilot_ids = @($newId) }
  $uoap = Call 'POST' "/api/v1/portfolios/$($uo.json.id)/approve" @{ version = 1 }
  Check 'unknown overlap group blocks approval' ($np.code -eq 201 -and $uo.json.status -eq 'NEEDS_REVIEW' -and ($uo.json.assessment.blockers.code -contains 'OVERLAP_UNKNOWN') -and $uoap.code -eq 409)

  $o1 = (Call 'POST' '/api/v1/preview/optimize' @{ budget_inr = 600000 } 'preview-optimize').json.optimization.selected -join ','
  $o2 = (Call 'POST' '/api/v1/preview/optimize' @{ budget_inr = 600000 }).json.optimization.selected -join ','
  $pl = @($p.json.pilots | Where-Object { $_.pilot.id -match '^AI\d$' } | ForEach-Object { $x = $_.pilot; @{ id = $x.id; task = $x.task; setup_cost_inr = $x.setup_cost_inr; annual_running_cost_inr = $x.annual_running_cost_inr; annual_benefit_inr = $x.annual_benefit_inr; benefit_type = $x.benefit_type_raw; overlap_group = $x.overlap_group; expected_adoption_pct = $x.expected_adoption_pct; readiness_label = $x.readiness_label } })
  [array]::Reverse($pl)
  $o3 = (Call 'POST' '/api/v1/optimize' @{ budget_inr = 600000; pilots = $pl } 'optimize-stateless').json.result.selected -join ','
  Check 'optimizer deterministic (repeat + reversed input) -> AI1,AI2,AI4' ($o1 -eq 'AI1,AI2,AI4' -and $o2 -eq $o1 -and $o3 -eq $o1) "$o1 | $o2 | $o3"
  $o4 = (Call 'POST' '/api/v1/preview/optimize' @{ budget_inr = 100000 }).json.optimization.selected
  Check 'budget reduced to 100000 -> empty portfolio' (@($o4).Count -eq 0)

  # --- persistence, scenarios, audit
  $sv = Call 'POST' '/api/v1/portfolios' @{ name = "smoke plan $stamp"; budget_inr = 600000; pilot_ids = @('AI1', 'AI2', 'AI4') } 'create-portfolio-valid'
  $apv = Call 'POST' "/api/v1/portfolios/$($sv.json.id)/approve" @{ version = $sv.json.version } 'approve-ok'
  $rl = Call 'GET' "/api/v1/portfolios/$($sv.json.id)" $null 'get-portfolio'
  Check 'save -> approve -> reload from PostgreSQL (APPROVED, recognised 540000)' ($sv.json.status -eq 'VALID' -and $apv.code -eq 200 -and $rl.json.status -eq 'APPROVED' -and $rl.json.assessment.total_recognized_benefit_inr -eq 540000 -and $rl.json.approval_stale -eq $false)
  $sc = Call 'GET' '/api/v1/pilots/AI1/scenarios' $null 'list-scenarios'
  Check 'scenario runs persisted and reloadable' ($sc.code -eq 200 -and @($sc.json.scenarios | Where-Object { $_.result.scenario.calculated_metrics.stated_basis.payback_months -eq 40 }).Count -ge 1)
  $dh = Call 'GET' '/api/v1/pilots/AI1/decisions' $null 'list-decisions'
  Check 'decision history with rules version + input snapshot' ($dh.code -eq 200 -and @($dh.json.decisions).Count -ge 1 -and $dh.json.decisions[0].rules_version -eq 'c23-rules-v1' -and $null -ne $dh.json.decisions[0].input_snapshot)
  $au = Call 'GET' "/api/v1/audit/portfolio/$($sv.json.id)" $null 'audit-portfolio'
  Check 'audit history: create + approve recorded' ($au.code -eq 200 -and ($au.json.entries.action -contains 'create') -and ($au.json.entries.action -contains 'approve'))
  $ex = Call 'GET' "/api/v1/export/$($sv.json.id)?format=json" $null 'export-json'
  Check 'export evidence carries the synthetic-data disclaimer' ($ex.code -eq 200 -and $ex.json.disclaimer -match 'not proven enterprise ROI')

  # --- validation & security
  $v = Call 'POST' '/api/v1/pilots' '{"id":"BAD1","task":"t","setup_cost_inr":"2 lakh"}' 'error-validation'
  Check 'structured validation error ("2 lakh" -> 422 NOT_NUMERIC)' ($v.code -eq 422 -and $v.json.error.code -eq 'VALIDATION_FAILED' -and $v.json.error.fields[0].code -eq 'NOT_NUMERIC' -and $v.json.error.request_id)
  $mj = Call 'POST' '/api/v1/pilots' '{"id":' 'error-malformed-json'
  Check 'malformed JSON -> 400 MALFORMED_JSON' ($mj.code -eq 400 -and $mj.json.error.code -eq 'MALFORMED_JSON')
  $nan = Call 'POST' '/api/v1/evaluate' '{"id":"X","task":"t","setup_cost_inr":NaN}'
  Check 'NaN literal -> 400' ($nan.code -eq 400)
  $pct = Call 'POST' '/api/v1/evaluate' '{"id":"X","task":"t","actual_quality_pct":101}'
  Check 'percentage > 100 -> 422 OUT_OF_RANGE' ($pct.code -eq 422 -and $pct.json.error.fields[0].code -eq 'OUT_OF_RANGE')
  $dup = Call 'POST' '/api/v1/pilots' '{"id":"ai1","task":"t"}'
  Check 'duplicate id (case-insensitive) -> 409 CONFLICT' ($dup.code -eq 409)
  $dk = Call 'POST' '/api/v1/pilots' '{"id":"DK1","task":"t","setup_cost_inr":-1,"setup_cost_inr":1}'
  Check 'duplicate JSON key -> 422 DUPLICATE_KEY' ($dk.code -eq 422 -and $dk.json.error.fields[0].code -eq 'DUPLICATE_KEY')
  $nb = Call 'POST' '/api/v1/portfolios' '{"name":"x","budget_inr":null}'
  $ngb = Call 'POST' '/api/v1/portfolios' '{"name":"x","budget_inr":-1}'
  $dpp = Call 'POST' '/api/v1/portfolios' '{"name":"x","budget_inr":1,"pilot_ids":["AI1","AI1"]}'
  $unk = Call 'POST' '/api/v1/portfolios' '{"name":"x","budget_inr":1,"pilot_ids":["NOPE99"]}'
  Check 'budget null / negative, duplicate pilot, unknown pilot -> 422' ($nb.code -eq 422 -and $ngb.code -eq 422 -and $dpp.code -eq 422 -and $unk.code -eq 422)
  $sq = Call 'GET' "/api/v1/pilots/x'%20OR%20'1'='1" $null 'error-invalid-id'
  $nf = Call 'GET' '/api/v1/pilots/AI99' $null 'error-not-found'
  Check 'SQL-injection-shaped id -> 400 INVALID_ID; unknown pilot -> 404' ($sq.code -eq 400 -and $nf.code -eq 404)
  $big = Call 'POST' '/api/v1/pilots' ('{"id":"BIG","task":"' + ('a' * 1100000) + '"}')
  Check 'oversized body (> 1 MiB) -> 413' ($big.code -eq 413)
  $after = Call 'GET' '/api/v1/pilots'
  Check 'data intact after attacks' ($after.json.count -eq ($p.json.count + 1))
} catch {
  Check 'smoke test aborted' $false $_.Exception.Message
}

$failed = @($results | Where-Object { -not $_.pass }).Count
$out = [ordered]@{ base_url = $BaseUrl; expect_ai = $ExpectAI; expect_cache = $ExpectCache; total = $results.Count; passed = $results.Count - $failed; failed = $failed; checks = $results }
$dir = Join-Path $root 'backend\reports'; New-Item -ItemType Directory -Force $dir | Out-Null
[IO.File]::WriteAllText((Join-Path $dir 'smoke_test.json'), ($out | ConvertTo-Json -Depth 6), (New-Object Text.UTF8Encoding($false)))
Write-Host "`nSMOKE TEST: $($out.passed)/$($out.total) passed"
if ($failed -gt 0) { exit 1 }
exit 0
