#!/usr/bin/env bash
# Backend smoke test (Linux/macOS/CI; needs curl + jq). Same core checks as scripts/smoke_test.ps1.
#   BASE_URL=http://localhost:8080 EXPECT_AI=ok|degraded|any bash scripts/smoke_test.sh
set -u
BASE_URL="${BASE_URL:-http://localhost:8080}"
EXPECT_AI="${EXPECT_AI:-any}"
STAMP="$(date +%s%N)"
PASS=0; FAIL=0
H='Content-Type: application/json'

call() { # method path [body] -> prints "<status>\n<json>"
  if [ $# -ge 3 ]; then curl -s -o /tmp/c23body -w '%{http_code}' -X "$1" -H "$H" -H 'X-Actor: smoke-test' --data "$3" "$BASE_URL$2"
  else curl -s -o /tmp/c23body -w '%{http_code}' -X "$1" -H "$H" -H 'X-Actor: smoke-test' "$BASE_URL$2"; fi
  echo; cat /tmp/c23body
}
check() { if [ "$2" = "true" ]; then PASS=$((PASS+1)); echo "[PASS] $1"; else FAIL=$((FAIL+1)); echo "[FAIL] $1"; fi; }
st() { head -n1 <<<"$1"; }
js() { tail -n +2 <<<"$1" | jq -r "$2"; }

r=$(call GET /health);  check "GET /health + database ok" "$( [ "$(st "$r")" = 200 ] && [ "$(js "$r" .components.database)" = ok ] && echo true)"
[ "$EXPECT_AI" != any ] && check "ai_service = $EXPECT_AI" "$( [ "$(js "$r" .components.ai_service)" = "$EXPECT_AI" ] && echo true)"
r=$(call GET /ready);   check "GET /ready 200" "$( [ "$(st "$r")" = 200 ] && echo true)"
r=$(call POST /api/v1/import/official); check "import official (201/409)" "$( [[ "$(st "$r")" =~ ^(201|409)$ ]] && echo true)"

r=$(call POST /api/v1/pilots/AI1/evaluate)
check "DEMO1 120000 / 10000 / 20 months" "$( [ "$(js "$r" '.evaluation.calculated_metrics.stated_basis | "\(.net_annual_benefit_inr)/\(.monthly_net_benefit_inr)/\(.payback_months)"')" = 120000/10000/20 ] && echo true)"
[ "$EXPECT_AI" != any ] && check "evaluate ai_status = $EXPECT_AI" "$( [ "$(js "$r" .ai_status)" = "$EXPECT_AI" ] && echo true)"

sc() { call POST /api/v1/pilots/AI1/scenarios "{\"name\":\"smoke\",\"overrides\":$1}"; }
r=$(sc '{"annual_running_cost_inr":120000}')
check "DEMO2 60000 / 5000 / 40 months" "$( [ "$(js "$r" '.result.scenario.calculated_metrics.stated_basis | "\(.net_annual_benefit_inr)/\(.monthly_net_benefit_inr)/\(.payback_months)"')" = 60000/5000/40 ] && echo true)"
r=$(sc '{"actual_adoption_pct":20,"actual_quality_pct":90}'); check "low adoption -> STOP" "$( [ "$(js "$r" .result.scenario.decision)" = STOP ] && echo true)"
r=$(sc '{"actual_adoption_pct":45,"actual_quality_pct":90}'); check "adoption 45 -> REVISE LOW_ADOPTION" "$( [ "$(js "$r" .result.scenario.primary_reason)" = LOW_ADOPTION ] && echo true)"
r=$(sc '{"actual_adoption_pct":70,"actual_quality_pct":60}'); check "quality 60 -> STOP" "$( [ "$(js "$r" .result.scenario.primary_reason)" = QUALITY_THRESHOLD_FAILED ] && echo true)"
r=$(sc '{"annual_running_cost_inr":null}')
check "missing running cost -> null payback, REVISE" "$( [ "$(js "$r" '.result.scenario | "\(.decision)/\(.calculated_metrics.stated_basis.payback_months)"')" = REVISE/null ] && echo true)"

r=$(call POST /api/v1/preview/portfolio '{"budget_inr":600000,"pilot_ids":["AI1","AI2","AI3","AI4"]}')
check "budget exceeded -> REJECTED" "$( [ "$(js "$r" .assessment.status)" = REJECTED ] && echo true)"
r=$(call POST /api/v1/portfolios "{\"name\":\"smoke overlap $STAMP\",\"budget_inr\":600000,\"pilot_ids\":[\"AI4\",\"AI5\"]}")
PID_OV=$(js "$r" .id)
check "AI4+AI5 DOUBLE_COUNT_RISK, recognised 180000" "$( [ "$(js "$r" '"\(.assessment.overlaps[0].code)/\(.assessment.total_recognized_benefit_inr)"')" = DOUBLE_COUNT_RISK/180000 ] && echo true)"
r=$(call POST "/api/v1/portfolios/$PID_OV/approve" '{"version":1}'); check "unresolved overlap blocks approval" "$( [ "$(st "$r")" = 409 ] && echo true)"

r=$(call POST /api/v1/preview/optimize '{"budget_inr":600000}'); check "optimizer -> AI1,AI2,AI4" "$( [ "$(js "$r" '.optimization.selected | join(",")')" = AI1,AI2,AI4 ] && echo true)"
r=$(call POST /api/v1/portfolios "{\"name\":\"smoke plan $STAMP\",\"budget_inr\":600000,\"pilot_ids\":[\"AI1\",\"AI2\",\"AI4\"]}"); PID_OK=$(js "$r" .id)
call POST "/api/v1/portfolios/$PID_OK/approve" '{"version":1}' >/dev/null
r=$(call GET "/api/v1/portfolios/$PID_OK"); check "save -> approve -> reload APPROVED" "$( [ "$(js "$r" .status)" = APPROVED ] && echo true)"
r=$(call GET "/api/v1/audit/portfolio/$PID_OK"); check "audit has approve" "$( js "$r" '[.entries[].action] | index("approve") != null' )"
r=$(call GET /api/v1/pilots/AI1/scenarios); check "scenarios persisted" "$( [ "$(js "$r" '.scenarios | length')" -ge 1 ] && echo true)"

r=$(call POST /api/v1/pilots '{"id":"BAD1","task":"t","setup_cost_inr":"2 lakh"}'); check "\"2 lakh\" -> 422 NOT_NUMERIC" "$( [ "$(js "$r" '.error.fields[0].code')" = NOT_NUMERIC ] && echo true)"
r=$(call POST /api/v1/pilots '{"id":'); check "malformed JSON -> 400" "$( [ "$(st "$r")" = 400 ] && echo true)"

echo; echo "SMOKE TEST: $PASS/$((PASS+FAIL)) passed"
[ "$FAIL" -eq 0 ]
