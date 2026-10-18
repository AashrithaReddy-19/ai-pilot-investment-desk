#!/usr/bin/env bash
# ==============================================================================
# Integration Test Script for C23 Backend & Decision Engine (Linux/macOS)
# Runs curl checks against the running gateway and cleans up every row it created.
#
#   ./test.sh                                   gateway on http://localhost:8080, AUTH_MODE=off
#   API_BASE=http://host:8090 ./test.sh         another gateway
#   C23_TOKEN=c23_... C23_WORKSPACE_ID=<uuid> ./test.sh
#                                               a gateway with AUTH_MODE=required (token from
#                                               `go run ./cmd/api bootstrap`; the token needs the admin role)
#   DATABASE_URL=postgres://... ./test.sh       database for cleanup (default: backend/.env)
#
# Tests 1-10 cover evaluation, portfolios and the Phase 6 lifecycle; 11-24 cover the engine's
# economics/decision/dashboard/pricing endpoints, stored proposals and versions, budget settings,
# workspace snapshots (stale detection and replay), approvals, stored uploads (worker, review,
# corrections, confirm, download, duplicates, rejections), the audit trail and the security layer.
# ==============================================================================

set -uo pipefail

if [ -t 1 ]; then
    BOLD="\033[1m"; GREEN="\033[0;32m"; RED="\033[0;31m"; YELLOW="\033[1;33m"; BLUE="\033[0;34m"; CYAN="\033[0;36m"; RESET="\033[0m"
else
    BOLD=""; GREEN=""; RED=""; YELLOW=""; BLUE=""; CYAN=""; RESET=""
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PORTFOLIO_NAME="run_1"
API_BASE="${API_BASE:-http://localhost:8080}"
API="$API_BASE/api/v1"
RUN_ID="$(date +%s)$$"
TMP_DIR="$(mktemp -d)"

if ! command -v jq &>/dev/null; then
    echo -e "${RED}[ERROR] jq is required (sudo apt install jq / brew install jq).${RESET}"
    exit 1
fi

# Auth headers: only sent when a token is given (AUTH_MODE=required).
AUTH_ARGS=()
if [ -n "${C23_TOKEN:-}" ]; then
    AUTH_ARGS+=(-H "Authorization: Bearer $C23_TOKEN")
fi
if [ -n "${C23_WORKSPACE_ID:-}" ]; then
    AUTH_ARGS+=(-H "X-Workspace-ID: $C23_WORKSPACE_ID")
fi

# api METHOD PATH [JSON]  -> response body on stdout (gateway auth headers included)
api() {
    local method="$1" path="$2" body="${3:-}"
    if [ -n "$body" ]; then
        curl -s -X "$method" ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} -H "Content-Type: application/json" -d "$body" "$API$path"
    else
        curl -s -X "$method" ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$API$path"
    fi
}
# api_code METHOD PATH [JSON] -> HTTP status only
api_code() {
    local method="$1" path="$2" body="${3:-}"
    if [ -n "$body" ]; then
        curl -s -o /dev/null -w "%{http_code}" -X "$method" ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} -H "Content-Type: application/json" -d "$body" "$API$path"
    else
        curl -s -o /dev/null -w "%{http_code}" -X "$method" ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$API$path"
    fi
}

# DATABASE_URL: environment wins, else backend/.env (used only for cleanup)
DATABASE_URL="${DATABASE_URL:-}"
if [ -z "$DATABASE_URL" ] && [ -f "$ROOT_DIR/backend/.env" ]; then
    DATABASE_URL=$(grep -E '^DATABASE_URL=' "$ROOT_DIR/backend/.env" | cut -d '=' -f2- | tr -d '\r"')
fi
HAS_PSQL=false
if [ -n "$DATABASE_URL" ] && command -v psql &>/dev/null; then
    HAS_PSQL=true
fi
sql() { psql -q "$DATABASE_URL" -t -A -c "$1" 2>/dev/null; }

# Pre-record the newest ids so cleanup deletes only rows this run created
PLAN_PILOT="AI1"
PREV_EVAL_ID=""; PREV_OBS_ID=""; PREV_DEC_ID=""; PREV_AUDIT_ID=""
if [ "$HAS_PSQL" = true ]; then
    PREV_EVAL_ID=$(sql "SELECT COALESCE(MAX(id), 0) FROM evaluations;" || echo "")
    PREV_OBS_ID=$(sql "SELECT COALESCE(MAX(id), 0) FROM observations;" || echo "")
    PREV_DEC_ID=$(sql "SELECT COALESCE(MAX(id), 0) FROM decisions;" || echo "")
    PREV_AUDIT_ID=$(sql "SELECT COALESCE(MAX(id), 0) FROM audit_events;" || echo "")
fi

# Things the run creates, removed in cleanup
PROPOSAL_IDS=()
UPLOAD_IDS=()
SNAPSHOT_IDS=()
PREV_SETTINGS=""

CLEANUP_DONE=false
TESTS_STARTED=false

cleanup() {
    if [ "$CLEANUP_DONE" = true ] || [ "$TESTS_STARTED" = false ]; then
        rm -rf "$TMP_DIR"
        return
    fi
    CLEANUP_DONE=true

    echo ""
    echo -e "${BOLD}${CYAN}=======================================================${RESET}"
    echo -e "${BOLD}${CYAN}               CLEANING UP TEST DATA                   ${RESET}"
    echo -e "${BOLD}${CYAN}=======================================================${RESET}"

    # 1. Portfolio via the API
    HTTP_CODE=$(api_code DELETE "/portfolios/$PORTFOLIO_NAME" || echo "000")
    if [ "$HTTP_CODE" = "204" ] || [ "$HTTP_CODE" = "200" ]; then
        echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted portfolio '$PORTFOLIO_NAME' via API (HTTP $HTTP_CODE)"
    elif [ "$HTTP_CODE" = "404" ]; then
        echo -e "${BLUE}[CLEANUP INFO]${RESET} Portfolio '$PORTFOLIO_NAME' not found or already deleted"
    else
        echo -e "${YELLOW}[CLEANUP WARN]${RESET} DELETE /api/v1/portfolios/$PORTFOLIO_NAME returned HTTP $HTTP_CODE"
    fi

    # 2. Proposals via the API (cascades to their evidence and approvals)
    for pid in ${PROPOSAL_IDS[@]+"${PROPOSAL_IDS[@]}"}; do
        code=$(api_code DELETE "/proposals/$pid" || echo "000")
        if [ "$code" = "204" ]; then
            echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted proposal $pid via API (approvals and evidence cascade)"
        else
            echo -e "${YELLOW}[CLEANUP WARN]${RESET} DELETE /api/v1/proposals/$pid returned HTTP $code"
        fi
    done

    # 3. Budget settings: restore what the workspace had before the run
    if [ -n "$PREV_SETTINGS" ]; then
        code=$(api_code PUT "/settings" "$PREV_SETTINGS" || echo "000")
        if [ "$code" = "200" ]; then
            echo -e "${GREEN}[CLEANUP PASS]${RESET} Restored the workspace budget settings"
        else
            echo -e "${YELLOW}[CLEANUP WARN]${RESET} Could not restore settings (HTTP $code): $PREV_SETTINGS"
        fi
    fi

    if [ "$HAS_PSQL" = true ]; then
        # 4. Uploads and snapshots have no delete endpoint (they are records): remove this run's rows
        for uid in ${UPLOAD_IDS[@]+"${UPLOAD_IDS[@]}"}; do
            [ -n "$(sql "DELETE FROM uploads WHERE id = '$uid' RETURNING id;")" ] && \
                echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted upload $uid (and its corrections) from Postgres"
        done
        for sid in ${SNAPSHOT_IDS[@]+"${SNAPSHOT_IDS[@]}"}; do
            [ -n "$(sql "DELETE FROM snapshots WHERE id = '$sid' RETURNING id;")" ] && \
                echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted snapshot $sid from Postgres"
        done
        # 5. Evaluation history
        if [ -n "$PREV_EVAL_ID" ]; then
            n=$(sql "DELETE FROM evaluations WHERE id > $PREV_EVAL_ID RETURNING id;" | grep -cv '^$' || true)
            [ "$n" -gt 0 ] && echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted $n evaluation history row(s) from Postgres"
        fi
        # 6. Phase 6 rows (the plan is a per-pilot upsert, so deleting by pilot id removes exactly this run's row)
        if sql "DELETE FROM pilot_plans WHERE pilot_id = '$PLAN_PILOT' RETURNING pilot_id;" | grep -q "$PLAN_PILOT"; then
            echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted pilot plan for $PLAN_PILOT from Postgres"
        fi
        if [ -n "$PREV_OBS_ID" ]; then
            n=$(sql "DELETE FROM observations WHERE id > $PREV_OBS_ID RETURNING id;" | grep -cv '^$' || true)
            [ "$n" -gt 0 ] && echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted $n observation row(s) from Postgres"
        fi
        if [ -n "$PREV_DEC_ID" ]; then
            n=$(sql "DELETE FROM decisions WHERE id > $PREV_DEC_ID RETURNING id;" | grep -cv '^$' || true)
            [ "$n" -gt 0 ] && echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted $n decision row(s) from Postgres"
        fi
        # 7. Audit rows written by this run (the application itself never deletes audit rows)
        if [ -n "$PREV_AUDIT_ID" ]; then
            n=$(sql "DELETE FROM audit_events WHERE id > $PREV_AUDIT_ID RETURNING id;" | grep -cv '^$' || true)
            [ "$n" -gt 0 ] && echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted $n audit row(s) written by the test run"
        fi
    else
        if [ ${#UPLOAD_IDS[@]} -gt 0 ] || [ ${#SNAPSHOT_IDS[@]} -gt 0 ]; then
            echo -e "${YELLOW}[CLEANUP WARN]${RESET} psql/DATABASE_URL unavailable: uploads (${UPLOAD_IDS[*]:-}) and snapshots (${SNAPSHOT_IDS[*]:-}) were left in place"
        fi
    fi

    # 8. Verify the portfolio is gone
    VERIFY_CODE=$(api_code GET "/portfolios/$PORTFOLIO_NAME" || echo "000")
    if [ "$VERIFY_CODE" = "404" ]; then
        echo -e "${GREEN}[CLEANUP VERIFIED]${RESET} Portfolio '$PORTFOLIO_NAME' verified removed from Postgres (404 Not Found)"
    fi
    echo -e "${BOLD}${CYAN}=======================================================${RESET}"
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

FAILED_TESTS=0
PASSED_TESTS=0
SKIPPED_TESTS=0
pass() { echo -e "${GREEN}--> Test $1 Passed: $2${RESET}"; PASSED_TESTS=$((PASSED_TESTS + 1)); }
fail() { echo -e "${RED}--> Test $1 Failed: $2${RESET}"; FAILED_TESTS=$((FAILED_TESTS + 1)); }
skip() { echo -e "${YELLOW}--> Test $1 Skipped: $2${RESET}"; SKIPPED_TESTS=$((SKIPPED_TESTS + 1)); }
title() { echo ""; echo -e "${BOLD}${BLUE}# $1${RESET}"; }
show() { echo "$1" | jq "${2:-.}" 2>/dev/null | head -"${3:-30}" || echo "$1" | head -"${3:-30}"; }

echo -e "${BOLD}${CYAN}=======================================================${RESET}"
echo -e "${BOLD}${CYAN}             API INTEGRATION TESTS                     ${RESET}"
echo -e "${BOLD}${CYAN}=======================================================${RESET}"

if ! curl -s --connect-timeout 2 "$API_BASE/health" &>/dev/null; then
    echo -e "${RED}[ERROR] Backend is not reachable at $API_BASE!${RESET}"
    echo -e "${YELLOW}Please ensure both services are running before testing:${RESET}"
    echo -e "  Terminal 1: cd decision-engine && ./start.sh"
    echo -e "  Terminal 2: cd backend && ./start.sh"
    exit 1
fi
HEALTH=$(curl -s "$API_BASE/health")
AUTH_MODE=$(echo "$HEALTH" | jq -r '.auth_mode // "off"')
if [ "$AUTH_MODE" = "required" ] && [ -z "${C23_TOKEN:-}" ]; then
    echo -e "${RED}[ERROR] The gateway runs with AUTH_MODE=required: set C23_TOKEN (and C23_WORKSPACE_ID).${RESET}"
    echo -e "  cd backend && go run ./cmd/api bootstrap -email you@example.com"
    exit 1
fi
echo -e "Gateway ${CYAN}$API_BASE${RESET} · auth ${CYAN}$AUTH_MODE${RESET} · run id ${CYAN}$RUN_ID${RESET}"

TESTS_STARTED=true
api DELETE "/portfolios/$PORTFOLIO_NAME" &>/dev/null || true

# ------------------------------------------------------------------------------
title "1. Readiness check (verifies Postgres, FastAPI, and Redis status)"
READY_RESP=$(curl -s "$API_BASE/ready")
show "$READY_RESP"
if [ "$(echo "$READY_RESP" | jq -r .ready)" = "true" ]; then pass 1 "System is READY"; else fail 1 "System reported checks down (inspect above)"; fi

# ------------------------------------------------------------------------------
title "2. Evaluate both modes (independent + conservative) and store evaluation history"
EVAL_RESP=$(api POST /evaluate '{}')
show "$EVAL_RESP" '{evaluation_id, independent: .independent.selected, conservative: .conservative.selected}'
if echo "$EVAL_RESP" | jq -e '.independent and .conservative' >/dev/null; then
    pass 2 "Dual-mode evaluation computed successfully"
else
    fail 2 "Did not receive expected evaluation shape"
fi

# ------------------------------------------------------------------------------
title "3. Save a portfolio to Postgres"
SAVE_RESP=$(api POST /portfolios "{\"name\":\"$PORTFOLIO_NAME\",\"mode\":\"independent\",\"budget\":600000}")
show "$SAVE_RESP" '{name, mode, budget_inr, selected: [.pilots[] | select(.selected) | .pilot_id]}'
if [ "$(echo "$SAVE_RESP" | jq -r .name)" = "$PORTFOLIO_NAME" ]; then
    pass 3 "Portfolio '$PORTFOLIO_NAME' saved to database"
else
    fail 3 "Portfolio save did not return expected name"
fi

# ------------------------------------------------------------------------------
title "4. Generate a 30-day plan for AI1"
PLAN_RESP=$(api POST /plans '{"pilot_id":"AI1"}')
show "$PLAN_RESP" '.plan'
if echo "$PLAN_RESP" | jq -e '.plan.status == "planned" and .plan.pilot_id == "AI1"' >/dev/null; then
    pass 4 "Plan stored with status planned"
else
    fail 4 "Plan creation did not return a planned plan for AI1"
fi

# ------------------------------------------------------------------------------
title "5. Record observation for AI1 (adoption 72 / quality 85)"
OBS_RESP=$(api POST /pilots/AI1/observations '{"observed_adoption_pct":72,"observed_quality_pct":85}')
show "$OBS_RESP" '{observation: .observation.id, decision: .decision.decision}'
if echo "$OBS_RESP" | jq -e '.observation.id' >/dev/null; then pass 5 "Observation recorded with auto decision"; else fail 5 "Observation was not recorded"; fi

# ------------------------------------------------------------------------------
title "6. Tracker progress for AI1"
PROGRESS_RESP=$(api GET /plans/AI1)
show "$PROGRESS_RESP" '{actual, gaps, checkpoint: .verdict.checkpoint}'
if echo "$PROGRESS_RESP" | jq -e '.gaps.adoption_gap_pct != null and .verdict.checkpoint == "extend" and .actual.adoption_pct == 72' >/dev/null; then
    pass 6 "Gaps computed (+12/+5) and verdict re-run (extend)"
else
    fail 6 "Progress did not carry gaps and an extend verdict"
fi

# ------------------------------------------------------------------------------
title "7. Checkpoint preview for AI1"
CHECKPOINT_RESP=$(api POST /checkpoint/AI1 '{}')
show "$CHECKPOINT_RESP" '{checkpoint, stop_rule: .stop_rule.decision}'
if echo "$CHECKPOINT_RESP" | jq -e '.checkpoint == "extend" and .stop_rule' >/dev/null; then
    pass 7 "Checkpoint answered with outcome and stop rule beside it"
else
    fail 7 "Checkpoint preview missing outcome or stop rule"
fi

# ------------------------------------------------------------------------------
title "8. Move plan status revise then stopped"
REVISE_RESP=$(api POST /plans/AI1/status '{"status":"revise"}')
STOP_RESP=$(api POST /plans/AI1/status '{"status":"stopped"}')
if [ "$(echo "$REVISE_RESP" | jq -r .status)" = "revise" ] && [ "$(echo "$STOP_RESP" | jq -r .status)" = "stopped" ]; then
    pass 8 "Status transitions revise then stopped accepted"
else
    fail 8 "Plan status transition rejected"
fi

# ------------------------------------------------------------------------------
title "9. Reallocation recommendation after stopping AI1"
REALLOC_RESP=$(api POST /reallocation '{"pilot_id":"AI1"}')
show "$REALLOC_RESP" '{accepted, freed_budget_inr, message}'
if echo "$REALLOC_RESP" | jq -e '.accepted == false and .freed_budget_inr == 200000' >/dev/null; then
    pass 9 "AI1 freed 200000 as a recommendation only"
else
    fail 9 "Reallocation did not free AI1's 200000 (or auto-accepted)"
fi

# ------------------------------------------------------------------------------
title "10. Decision memo for AI1 (markdown)"
MEMO_RESP=$(api POST /export/memo '{"pilot_id":"AI1"}')
echo "$MEMO_RESP" | head -8
if echo "$MEMO_RESP" | grep -q "Decision memo — AI1" && echo "$MEMO_RESP" | grep -q "## Audit trail"; then
    pass 10 "Memo rendered as one-page markdown with audit trail"
else
    fail 10 "Memo missing heading or audit trail"
fi

# ==============================================================================
# Engine rules version 7+: economics, decisions, dashboard, pricing (stateless pass-through)
# ==============================================================================
EXAMPLE='{"proposals":[
  {"name":"A","setup_cost_inr":200000,"annual_benefit_inr":180000,"ai_running_cost_inr":60000},
  {"name":"B","setup_cost_inr":500000,"annual_benefit_inr":400000,"ai_running_cost_inr":150000},
  {"name":"C","setup_cost_inr":100000,"annual_benefit_inr":90000,"ai_running_cost_inr":70000}],
  "budget":700000,"extension":{"defaults":{"discount_rate":0}}}'

title "11. The gateway's cache key follows the engine's own rules version"
show "$HEALTH"
GW_RV=$(echo "$HEALTH" | jq -r '.rules_version // empty')
READY_RV=$(echo "$READY_RESP" | jq -r '.rules_version // empty')
if [ -n "$GW_RV" ] && [ "$GW_RV" != "fallback" ] && [ "$GW_RV" = "$READY_RV" ]; then
    pass 11 "Gateway reports engine rules version $GW_RV (nobody sets RULES_VERSION by hand)"
else
    fail 11 "Gateway did not report the engine's rules version (health=$GW_RV ready=$READY_RV)"
fi

title "12. Executive dashboard for the plan's worked example (A, B, C at 7 L)"
DASH=$(api POST /views/dashboard "$EXAMPLE")
show "$DASH" '{headline: .summary.headline, kpis: ([.kpis[] | {(.key): .value}] | add), consistency: [.consistency[].ok]}'
if echo "$DASH" | jq -e '([.kpis[] | {(.key): .value}] | add) as $k
      | $k.net_annual_savings_inr == 370000 and $k.profit_inr == 410000 and $k.breakeven_month == 23
      and ([.consistency[].ok] | all) and (.charts.series_order | length == 3)' >/dev/null; then
    pass 12 "KPIs match the worked example (3.7 L/yr, 4.1 L profit, month 23) and every consistency check holds"
else
    fail 12 "Dashboard KPIs or consistency checks are wrong"
fi

title "13. Decision ranking, yearly savings and the decision report"
DEC=$(api POST /decision "$EXAMPLE")
YEARLY=$(api POST /views/yearly-savings "$EXAMPLE")
REPORT_CODE=$(curl -s -o "$TMP_DIR/report.md" -w "%{http_code}" -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} -H "Content-Type: application/json" -d "$EXAMPLE" "$API/export/decision-report")
show "$DEC" '{overall: .overall.outcome, ranking: [.ranking[] | {id, score, outcome}]}'
if echo "$DEC" | jq -e '.overall.outcome and (.ranking | length == 3) and (.decisions.C.outcome == "renegotiate")' >/dev/null \
   && echo "$YEARLY" | jq -e '[.proposals[] | select(.id == "A") | .years[].net_inr] == [-80000, 120000, 120000]' >/dev/null \
   && [ "$REPORT_CODE" = "200" ] && grep -q "INR 370,000" "$TMP_DIR/report.md"; then
    pass 13 "Ranking + outcomes, A's yearly net (−80k, 120k, 120k) and the report agree"
else
    fail 13 "Decision, yearly view or report did not answer as expected (report HTTP $REPORT_CODE)"
fi

title "14. Pricing plans priced against a backtested usage forecast"
OPT=$(api POST /pricing/optimise '{"history":[100,110,120,130,140,150],"months":3,
  "plans":[{"name":"payg","rule":{"type":"per_unit","unit_price":1}},
           {"name":"commit","rule":{"type":"commitment","committed_units":200,"commit_price":150,"overage_unit_price":2}}]}')
show "$OPT" '{recommended, robust_choice, method: .forecast.method, base: .forecast.base}'
if echo "$OPT" | jq -e '.forecast.method == "linear_trend" and .forecast.base == [160,170,180] and .recommended' >/dev/null; then
    pass 14 "Linear trend won the backtest (160, 170, 180) and a plan was recommended"
else
    fail 14 "Usage forecast or plan choice missing"
fi

# ==============================================================================
# Stored proposals, budget settings and workspace snapshots
# ==============================================================================
title "15. Stored proposals with versioned inputs"
PA=$(api POST /proposals "{\"name\":\"IT invoice extraction $RUN_ID\",\"inputs\":{\"setup_cost_inr\":200000,\"annual_benefit_inr\":180000,
  \"ai_running_cost_inr\":60000,\"readiness\":\"high\",\"overlap_group\":\"finance\",
  \"baseline\":{\"volume_per_month\":2000,\"minutes_per_unit\":6,\"loaded_cost_per_hour\":1000}}}")
PB=$(api POST /proposals "{\"name\":\"IT ticket triage $RUN_ID\",\"inputs\":{\"setup_cost_inr\":500000,\"annual_benefit_inr\":400000,
  \"ai_running_cost_inr\":150000,\"readiness\":\"medium\"}}")
PA_ID=$(echo "$PA" | jq -r '.id // empty'); PB_ID=$(echo "$PB" | jq -r '.id // empty')
[ -n "$PA_ID" ] && PROPOSAL_IDS+=("$PA_ID")
[ -n "$PB_ID" ] && PROPOSAL_IDS+=("$PB_ID")
NOTES_V=$(api PATCH "/proposals/$PB_ID" '{"notes":"notes do not bump the version"}' | jq -r .version)
if [ -n "$PA_ID" ] && [ -n "$PB_ID" ] && [ "$(echo "$PA" | jq -r .version)" = "1" ] && [ "$NOTES_V" = "1" ]; then
    pass 15 "Two proposals stored at inputs version 1; a notes edit keeps the version"
else
    fail 15 "Proposals not stored or versioned as expected: $PA $PB"
fi

title "16. Observation linked to a proposal feeds the next engine run"
LINK=$(api POST /pilots/AI1/observations "{\"observed_adoption_pct\":72,\"observed_quality_pct\":90,\"proposal_id\":\"$PA_ID\"}")
FOREIGN=$(api_code POST /pilots/AI1/observations '{"observed_adoption_pct":50,"proposal_id":"00000000-0000-0000-0000-00000000dead"}')
if echo "$LINK" | jq -e --arg p "$PA_ID" '.observation.proposal_id == $p' >/dev/null && [ "$FOREIGN" = "422" ]; then
    pass 16 "Observation linked to the proposal; an unknown proposal id is refused (422)"
else
    fail 16 "Proposal link not stored, or unknown proposal accepted (HTTP $FOREIGN)"
fi

title "17. Workspace budget settings (validated, restored after the run)"
PREV_SETTINGS=$(api GET /settings)
[ "$(echo "$PREV_SETTINGS" | jq -r 'type')" = "object" ] || PREV_SETTINGS=""
SET=$(api PUT /settings '{"budget":700000,"budget_set":{"total_inr":1500000,"contingency_pct":10},"defaults":{"discount_rate":0}}')
BAD_SET=$(api_code PUT /settings '{"budget":-1}')
show "$SET"
if echo "$SET" | jq -e '.budget == 700000 and .budget_set.total_inr == 1500000' >/dev/null && [ "$BAD_SET" = "422" ]; then
    pass 17 "Settings saved; a negative budget is rejected (422)"
else
    fail 17 "Settings not saved or not validated (bad PUT gave HTTP $BAD_SET)"
fi

title "18. Dashboard from the stored proposals, kept as a reproducible snapshot"
WS_DASH=$(api POST /workspace/dashboard "{\"proposal_ids\":[\"$PA_ID\",\"$PB_ID\"],\"ai_burn_to_date\":1550000}")
SNAP_ID=$(echo "$WS_DASH" | jq -r '.snapshot.id // empty')
[ -n "$SNAP_ID" ] && SNAPSHOT_IDS+=("$SNAP_ID")
show "$WS_DASH" '{snapshot: .snapshot.id, rules_version: .snapshot.rules_version, headline: .result.summary.headline,
  states: [.result.proposals[] | {(.id): [.state, .outcome]}] | add, consistency: [.result.consistency[].ok]}'
REPLAY=$(api POST "/snapshots/$SNAP_ID/replay")
if echo "$WS_DASH" | jq -e '.snapshot.id and (.snapshot.proposals | length == 2) and ([.result.consistency[].ok] | all)
      and ([.result.kpis[] | select(.key == "ai_burn_to_date_inr") | .value] == [1550000])
      and ([.result.proposals[] | select(.id | startswith("IT invoice")) | .outcome] == ["limited_pilot"])' >/dev/null \
   && [ "$(echo "$REPLAY" | jq -r .matches)" = "true" ]; then
    pass 18 "Snapshot stored with both proposal versions; the linked observation moved A to limited_pilot; replay reproduces it exactly"
else
    fail 18 "Workspace dashboard, snapshot or replay wrong: $(echo "$REPLAY" | jq -c . 2>/dev/null)"
fi

title "19. Editing inputs bumps the version and marks the snapshot stale"
V2=$(api PATCH "/proposals/$PB_ID" '{"inputs":{"setup_cost_inr":450000,"annual_benefit_inr":400000,"ai_running_cost_inr":150000}}' | jq -r .version)
STALE=$(api GET "/snapshots/$SNAP_ID")
show "$STALE" '{stale, reasons: .staleness.reasons}'
if [ "$V2" = "2" ] && echo "$STALE" | jq -e '.stale == true and (.staleness.reasons[0] | contains("v1 → v2"))' >/dev/null; then
    pass 19 "Inputs version 2; the snapshot reports it is stale and why"
else
    fail 19 "Version bump or stale detection missing (version $V2)"
fi

title "20. Approval on the proposal's current version"
APPR=$(api POST "/proposals/$PA_ID/approvals" "{\"decision\":\"approved\",\"snapshot_id\":\"$SNAP_ID\",\"comment\":\"start as a limited pilot\"}")
BAD_APPR=$(api_code POST "/proposals/$PA_ID/approvals" '{"decision":"maybe"}')
LIST_APPR=$(api GET "/proposals/$PA_ID/approvals")
show "$APPR" '{proposal_version, decision, engine_outcome}'
if echo "$APPR" | jq -e '.proposal_version == 1 and .decision == "approved" and .engine_outcome == "limited_pilot"' >/dev/null \
   && echo "$LIST_APPR" | jq -e '.approvals[0].stale == false' >/dev/null && [ "$BAD_APPR" = "422" ]; then
    pass 20 "Approval records version 1 and the engine outcome the approver saw; bad decisions are 422"
else
    fail 20 "Approval not recorded as expected (bad decision HTTP $BAD_APPR)"
fi

# ==============================================================================
# Stored uploads: worker, review, corrections, confirm
# ==============================================================================
title "21. Upload a balance sheet: stored, parsed by the worker, corrected, confirmed"
CSV="$TMP_DIR/balance-$RUN_ID.csv"
printf 'Account,Monthly spend (INR)\nAI software licences,100000\nRent,25000\nCloud AI platform,80000\nRun %s,1\n' "$RUN_ID" > "$CSV"
UP=$(curl -s -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} -F kind=balance_sheet -F "file=@$CSV" "$API/uploads")
UP_ID=$(echo "$UP" | jq -r '.upload.id // empty')
[ -n "$UP_ID" ] && UPLOAD_IDS+=("$UP_ID")
STATUS=""
for _ in $(seq 1 40); do
    STATUS=$(api GET "/uploads/$UP_ID" | jq -r '.upload.status // empty')
    [ "$STATUS" = "parsed" ] || [ "$STATUS" = "failed" ] && break
    sleep 0.5
done
CORR=$(api POST "/uploads/$UP_ID/corrections" '{"row_ref":4,"field":"amount_inr","value":50000,"note":"per invoice"}')
CONF=$(api POST "/uploads/$UP_ID/confirm" '{"rows":[2,4]}')
show "$CONF" '{status: .upload.status, ai_burn_to_date: .result.ai_burn_to_date, annualised: .result.ai_burn_annualised_inr, flags: [.result.flags[].code]}'
if [ "$(echo "$UP" | jq -r .upload.sha256 | wc -c)" -eq 65 ] && [ "$STATUS" = "parsed" ] \
   && echo "$CORR" | jq -e '.original_value == 80000 and .corrected_value == 50000' >/dev/null \
   && echo "$CONF" | jq -e '.upload.status == "confirmed" and .result.ai_burn_to_date == 150000 and .result.ai_burn_annualised_inr == 1800000' >/dev/null; then
    pass 21 "Worker parsed it; the correction kept 80,000 beside 50,000; the engine summed 1.5 L (18 L a year)"
else
    fail 21 "Upload pipeline wrong (status=$STATUS): $(echo "$CONF" | jq -c . 2>/dev/null | head -c 300)"
fi

title "22. Uploads: original download, duplicates and rejected files"
DL_CODE=$(curl -s -o "$TMP_DIR/dl.csv" -w "%{http_code}" ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} "$API/uploads/$UP_ID/file")
DUP=$(curl -s -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} -F kind=balance_sheet -F "file=@$CSV" "$API/uploads" | jq -r .duplicate)
printf '%%PDF-1.4 not a sheet' > "$TMP_DIR/report-$RUN_ID.pdf"
PDF_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} -F kind=balance_sheet -F "file=@$TMP_DIR/report-$RUN_ID.pdf" "$API/uploads")
printf 'Notes\nHello %s\n' "$RUN_ID" > "$TMP_DIR/notes-$RUN_ID.csv"
BAD=$(curl -s -X POST ${AUTH_ARGS[@]+"${AUTH_ARGS[@]}"} -F kind=balance_sheet -F "file=@$TMP_DIR/notes-$RUN_ID.csv" "$API/uploads")
BAD_ID=$(echo "$BAD" | jq -r '.upload.id // empty')
[ -n "$BAD_ID" ] && UPLOAD_IDS+=("$BAD_ID")
BAD_STATUS=""
for _ in $(seq 1 40); do
    BAD_STATUS=$(api GET "/uploads/$BAD_ID" | jq -r '.upload.status // empty')
    [ "$BAD_STATUS" = "failed" ] && break
    sleep 0.5
done
BAD_ERR=$(api GET "/uploads/$BAD_ID" | jq -r '.upload.last_error // empty')
echo "download HTTP $DL_CODE · duplicate=$DUP · pdf HTTP $PDF_CODE · unreadable sheet: $BAD_STATUS ($BAD_ERR)"
if [ "$DL_CODE" = "200" ] && cmp -s "$CSV" "$TMP_DIR/dl.csv" && [ "$DUP" = "true" ] && [ "$PDF_CODE" = "415" ] \
   && [ "$BAD_STATUS" = "failed" ] && [ -n "$BAD_ERR" ]; then
    pass 22 "Original returned byte for byte; same file is one upload; PDF refused (415); unreadable sheet failed with the engine's reason"
else
    fail 22 "Download, duplicate detection or rejection behaved unexpectedly"
fi

# ==============================================================================
# Audit trail and security
# ==============================================================================
title "23. Audit trail records the run's edits, downloads and approvals"
AUDIT=$(api GET "/audit?limit=50")
ACTIONS=$(echo "$AUDIT" | jq -r '[.events[].action] | unique | join(", ")')
echo "actions: $ACTIONS"
if echo "$AUDIT" | jq -e '[.events[].action] as $a | ["upload.confirm","upload.correct","upload.download","approval.create","settings.update","snapshot.create"]
      | all(. as $x | $a | index($x))' >/dev/null; then
    pass 23 "Uploads, corrections, downloads, approvals, settings and snapshots are all in the audit trail"
else
    fail 23 "Audit trail is missing actions from this run"
fi

title "24. Security: identity, sign-in and workspace isolation"
ME=$(api GET /me)
show "$ME" '{auth_mode, role, workspace: .workspace.id}'
if [ "$AUTH_MODE" = "required" ]; then
    NO_TOKEN=$(curl -s -o /dev/null -w "%{http_code}" "$API/proposals")
    BAD_TOKEN=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer not-a-token" "$API/proposals")
    OTHER_WS=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $C23_TOKEN" -H "X-Workspace-ID: 00000000-0000-0000-0000-00000000beef" "$API/proposals")
    echo "no token HTTP $NO_TOKEN · bad token HTTP $BAD_TOKEN · foreign workspace HTTP $OTHER_WS"
    if [ "$NO_TOKEN" = "401" ] && [ "$BAD_TOKEN" = "401" ] && [ "$OTHER_WS" = "403" ] && echo "$ME" | jq -e '.user.email' >/dev/null; then
        pass 24 "No token and bad tokens are 401; another organisation's workspace is 403"
    else
        fail 24 "Sign-in or workspace isolation did not hold"
    fi
else
    if echo "$ME" | jq -e '.auth_mode == "off" and .role == "admin"' >/dev/null; then
        pass 24 "AUTH_MODE=off: default workspace as admin (rerun with C23_TOKEN against AUTH_MODE=required for the sign-in checks)"
    else
        fail 24 "/me did not describe the auth-off mode"
    fi
fi

# ------------------------------------------------------------------------------
echo ""
if [ "$FAILED_TESTS" -gt 0 ]; then
    echo -e "${BOLD}${RED}=======================================================${RESET}"
    echo -e "${BOLD}${RED}   INTEGRATION TESTS FAILED ($FAILED_TESTS failed, $PASSED_TESTS passed)${RESET}"
    echo -e "${BOLD}${RED}=======================================================${RESET}"
    exit 1
fi
echo -e "${BOLD}${GREEN}=======================================================${RESET}"
echo -e "${BOLD}${GREEN}   ALL $PASSED_TESTS INTEGRATION TESTS PASSED${RESET}"
echo -e "${BOLD}${GREEN}=======================================================${RESET}"

# (Cleanup runs automatically via trap cleanup EXIT)
