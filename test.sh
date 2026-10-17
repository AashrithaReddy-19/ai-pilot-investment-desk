#!/usr/bin/env bash
# ==============================================================================
# Integration Test Script for C23 Backend & Decision Engine (Linux/macOS)
# Runs test curl commands against the running API and cleans up any created data.
# ==============================================================================

set -uo pipefail

# ANSI color codes
if [ -t 1 ]; then
    BOLD="\033[1m"
    GREEN="\033[0;32m"
    RED="\033[0;31m"
    YELLOW="\033[1;33m"
    BLUE="\033[0;34m"
    CYAN="\033[0;36m"
    RESET="\033[0m"
else
    BOLD=""
    GREEN=""
    RED=""
    YELLOW=""
    BLUE=""
    CYAN=""
    RESET=""
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PORTFOLIO_NAME="run_1"
API_BASE="http://localhost:8080"

# Read DATABASE_URL from backend/.env if available for evaluations cleanup
DATABASE_URL=""
if [ -f "$ROOT_DIR/backend/.env" ]; then
    DATABASE_URL=$(grep -E '^DATABASE_URL=' "$ROOT_DIR/backend/.env" | cut -d '=' -f2- | tr -d '\r"')
fi

# Pre-record max evaluation ID before tests run
PREV_EVAL_ID=""
if [ -n "$DATABASE_URL" ] && command -v psql &>/dev/null; then
    PREV_EVAL_ID=$(psql "$DATABASE_URL" -t -A -c "SELECT COALESCE(MAX(id), 0) FROM evaluations;" 2>/dev/null || echo "")
fi

# Pre-record counts/ids of the Phase 6 lifecycle tables so tests delete only their own rows
PLAN_PILOT="AI1"   # the plan the tracker tests upsert (per-pilot row)
PREV_OBS_ID=""
PREV_DEC_ID=""
if [ -n "$DATABASE_URL" ] && command -v psql &>/dev/null; then
    PREV_OBS_ID=$(psql "$DATABASE_URL" -t -A -c "SELECT COALESCE(MAX(id), 0) FROM observations;" 2>/dev/null || echo "")
    PREV_DEC_ID=$(psql "$DATABASE_URL" -t -A -c "SELECT COALESCE(MAX(id), 0) FROM decisions;" 2>/dev/null || echo "")
fi

# Cleanup function registered on EXIT
CLEANUP_DONE=false
TESTS_STARTED=false

cleanup() {
    if [ "$CLEANUP_DONE" = true ] || [ "$TESTS_STARTED" = false ]; then
        return
    fi
    CLEANUP_DONE=true

    echo ""
    echo -e "${BOLD}${CYAN}=======================================================${RESET}"
    echo -e "${BOLD}${CYAN}               CLEANING UP TEST DATA                   ${RESET}"
    echo -e "${BOLD}${CYAN}=======================================================${RESET}"

    # 1. Delete created portfolio via API
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X DELETE "$API_BASE/api/v1/portfolios/$PORTFOLIO_NAME" 2>/dev/null || echo "000")
    if [ "$HTTP_CODE" = "204" ] || [ "$HTTP_CODE" = "200" ]; then
        echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted portfolio '$PORTFOLIO_NAME' via API (HTTP $HTTP_CODE)"
    elif [ "$HTTP_CODE" = "404" ]; then
        echo -e "${BLUE}[CLEANUP INFO]${RESET} Portfolio '$PORTFOLIO_NAME' not found or already deleted"
    else
        echo -e "${YELLOW}[CLEANUP WARN]${RESET} DELETE /api/v1/portfolios/$PORTFOLIO_NAME returned HTTP $HTTP_CODE"
    fi

    # 2. Delete evaluation history record if psql is available
    if [ -n "$PREV_EVAL_ID" ] && [ -n "$DATABASE_URL" ] && command -v psql &>/dev/null; then
        DELETED_EVALS=$(psql -q "$DATABASE_URL" -t -A -c "DELETE FROM evaluations WHERE id > $PREV_EVAL_ID RETURNING id;" 2>/dev/null | grep -v '^$' | wc -l || echo "0")
        if [ "$DELETED_EVALS" -gt 0 ]; then
            echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted $DELETED_EVALS evaluation history row(s) from Postgres"
        fi
    fi

    # 2b. Delete the Phase 6 lifecycle rows the tracker tests created. The plan is a
    # per-pilot upsert row (the test replaces any existing one for the same pilot),
    # so deleting by pilot id removes exactly what the run added or replaced.
    if [ -n "$DATABASE_URL" ] && command -v psql &>/dev/null; then
        if psql "$DATABASE_URL" -t -A -c "DELETE FROM pilot_plans WHERE pilot_id = '$PLAN_PILOT' RETURNING pilot_id;" 2>/dev/null | grep -q "$PLAN_PILOT"; then
            echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted pilot plan for $PLAN_PILOT from Postgres"
        fi
    fi
    if [ -n "$PREV_OBS_ID" ] && [ -n "$DATABASE_URL" ] && command -v psql &>/dev/null; then
        DELETED_OBS=$(psql -q "$DATABASE_URL" -t -A -c "DELETE FROM observations WHERE id > $PREV_OBS_ID RETURNING id;" 2>/dev/null | grep -v '^$' | wc -l || echo "0")
        [ "$DELETED_OBS" -gt 0 ] && echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted $DELETED_OBS observation row(s) from Postgres"
    fi
    if [ -n "$PREV_DEC_ID" ] && [ -n "$DATABASE_URL" ] && command -v psql &>/dev/null; then
        DELETED_DECS=$(psql -q "$DATABASE_URL" -t -A -c "DELETE FROM decisions WHERE id > $PREV_DEC_ID RETURNING id;" 2>/dev/null | grep -v '^$' | wc -l || echo "0")
        [ "$DELETED_DECS" -gt 0 ] && echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted $DELETED_DECS decision row(s) from Postgres"
    fi

    # 3. Verify portfolio is deleted
    VERIFY_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$API_BASE/api/v1/portfolios/$PORTFOLIO_NAME" 2>/dev/null || echo "000")
    if [ "$VERIFY_CODE" = "404" ]; then
        echo -e "${GREEN}[CLEANUP VERIFIED]${RESET} Portfolio '$PORTFOLIO_NAME' verified removed from Postgres (404 Not Found)"
    fi
    echo -e "${BOLD}${CYAN}=======================================================${RESET}"
}
trap cleanup EXIT INT TERM

# Check JSON formatter
JSON_FORMATTER="cat"
if command -v jq &>/dev/null; then
    JSON_FORMATTER="jq"
elif command -v python3 &>/dev/null; then
    JSON_FORMATTER="python3 -m json.tool"
fi

echo -e "${BOLD}${CYAN}=======================================================${RESET}"
echo -e "${BOLD}${CYAN}             API INTEGRATION TESTS                     ${RESET}"
echo -e "${BOLD}${CYAN}=======================================================${RESET}"

# Check backend availability
if ! curl -s --connect-timeout 2 "$API_BASE/health" &>/dev/null; then
    echo -e "${RED}[ERROR] Backend is not reachable at $API_BASE!${RESET}"
    echo -e "${YELLOW}Please ensure both services are running before testing:${RESET}"
    echo -e "  Terminal 1: cd decision-engine && ./start.sh"
    echo -e "  Terminal 2: cd backend && ./start.sh"
    exit 1
fi

TESTS_STARTED=true

# Ensure clean slate: delete leftover portfolio if present
curl -s -X DELETE "$API_BASE/api/v1/portfolios/$PORTFOLIO_NAME" &>/dev/null || true

# ------------------------------------------------------------------------------
# Test 1: Readiness check (verifies Postgres, FastAPI, and Redis status)
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}${BLUE}# 1. Readiness check (verifies Postgres, FastAPI, and Redis status)${RESET}"
echo -e "${CYAN}curl -s http://localhost:8080/ready | jq${RESET}"
READY_RESP=$(curl -s "$API_BASE/ready")
echo "$READY_RESP" | $JSON_FORMATTER

FAILED_TESTS=0

# Check readiness status
IS_READY=$(echo "$READY_RESP" | grep -o '"ready":true' || true)
if [ -n "$IS_READY" ]; then
    echo -e "${GREEN}--> Test 1 Passed: System is READY${RESET}"
else
    echo -e "${RED}--> Test 1 Failed: System reported checks down (inspect above)${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# ------------------------------------------------------------------------------
# Test 2: Evaluate both modes (independent + conservative) and store evaluation history
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}${BLUE}# 2. Evaluate both modes (independent + conservative) and store evaluation history${RESET}"
echo -e "${CYAN}curl -s -X POST http://localhost:8080/api/v1/evaluate -d '{}' | jq${RESET}"
EVAL_RESP=$(curl -s -X POST "$API_BASE/api/v1/evaluate" -d '{}')
echo "$EVAL_RESP" | $JSON_FORMATTER

if echo "$EVAL_RESP" | grep -q '"independent"' && echo "$EVAL_RESP" | grep -q '"conservative"'; then
    echo -e "${GREEN}--> Test 2 Passed: Dual-mode evaluation computed successfully${RESET}"
else
    echo -e "${RED}--> Test 2 Failed: Did not receive expected evaluation shape${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# ------------------------------------------------------------------------------
# Test 3: Save a portfolio to Postgres
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}${BLUE}# 3. Save a portfolio to Postgres${RESET}"
echo -e "${CYAN}curl -s -X POST http://localhost:8080/api/v1/portfolios \\${RESET}"
echo -e "${CYAN}  -H \"Content-Type: application/json\" \\${RESET}"
echo -e "${CYAN}  -d '{\"name\":\"$PORTFOLIO_NAME\",\"mode\":\"independent\",\"budget\":600000}' | jq${RESET}"
SAVE_RESP=$(curl -s -X POST "$API_BASE/api/v1/portfolios" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$PORTFOLIO_NAME\",\"mode\":\"independent\",\"budget\":600000}")
echo "$SAVE_RESP" | $JSON_FORMATTER

if echo "$SAVE_RESP" | grep -q "\"name\":\"$PORTFOLIO_NAME\""; then
    echo -e "${GREEN}--> Test 3 Passed: Portfolio '$PORTFOLIO_NAME' saved to database${RESET}"
else
    echo -e "${RED}--> Test 3 Failed: Portfolio save did not return expected name${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# ------------------------------------------------------------------------------
# Test 4: 30-day plan (Phase 6): engine draft stored per pilot with a cost limit
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}${BLUE}# 4. Generate a 30-day plan for AI1${RESET}"
echo -e "${CYAN}curl -s -X POST http://localhost:8080/api/v1/plans -d '{\"pilot_id\":\"AI1\"}' | jq${RESET}"
PLAN_RESP=$(curl -s -X POST "$API_BASE/api/v1/plans" -H "Content-Type: application/json" -d '{"pilot_id":"AI1"}')
echo "$PLAN_RESP" | $JSON_FORMATTER

if echo "$PLAN_RESP" | grep -q '"status":"planned"' && echo "$PLAN_RESP" | grep -q '"pilot_id":"AI1"'; then
    echo -e "${GREEN}--> Test 4 Passed: Plan stored with status planned${RESET}"
else
    echo -e "${RED}--> Test 4 Failed: Plan creation did not return a planned plan for AI1${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# ------------------------------------------------------------------------------
# Test 5: Record an observation (tracker actuals) — also auto-records a decision
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}${BLUE}# 5. Record observation for AI1 (adoption 72 / quality 85)${RESET}"
OBS_RESP=$(curl -s -X POST "$API_BASE/api/v1/pilots/AI1/observations" -H "Content-Type: application/json" -d '{"observed_adoption_pct":72,"observed_quality_pct":85}')
echo "$OBS_RESP" | $JSON_FORMATTER

if echo "$OBS_RESP" | grep -q '"observation"'; then
    echo -e "${GREEN}--> Test 5 Passed: Observation recorded with auto decision${RESET}"
else
    echo -e "${RED}--> Test 5 Failed: Observation was not recorded${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# ------------------------------------------------------------------------------
# Test 6: Plan progress — expected vs actual gaps and the checkpoint verdict (extend)
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}${BLUE}# 6. Tracker progress for AI1${RESET}"
echo -e "${CYAN}curl -s http://localhost:8080/api/v1/plans/AI1 | jq${RESET}"
PROGRESS_RESP=$(curl -s "$API_BASE/api/v1/plans/AI1")
echo "$PROGRESS_RESP" | $JSON_FORMATTER

if echo "$PROGRESS_RESP" | grep -q '"adoption_gap_pct"' && echo "$PROGRESS_RESP" | grep -q '"checkpoint":"extend"' && echo "$PROGRESS_RESP" | grep -q '"adoption_pct": *72'; then
    echo -e "${GREEN}--> Test 6 Passed: Gaps computed (+12/+5) and verdict re-run (extend)${RESET}"
else
    echo -e "${RED}--> Test 6 Failed: Progress did not carry gaps and an extend verdict${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# ------------------------------------------------------------------------------
# Test 7: Checkpoint preview — same outcome, stop rule carried beside it
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}${BLUE}# 7. Checkpoint preview for AI1${RESET}"
echo -e "${CYAN}curl -s -X POST http://localhost:8080/api/v1/checkpoint/AI1 -d '{}' | jq${RESET}"
CHECKPOINT_RESP=$(curl -s -X POST "$API_BASE/api/v1/checkpoint/AI1" -H "Content-Type: application/json" -d '{}')
echo "$CHECKPOINT_RESP" | $JSON_FORMATTER

if echo "$CHECKPOINT_RESP" | grep -q '"checkpoint":"extend"' && echo "$CHECKPOINT_RESP" | grep -q '"stop_rule"'; then
    echo -e "${GREEN}--> Test 7 Passed: Checkpoint answered with outcome and stop rule beside it${RESET}"
else
    echo -e "${RED}--> Test 7 Failed: Checkpoint preview missing outcome or stop rule${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# ------------------------------------------------------------------------------
# Test 8: Plan status moves — revise arms the follow-up rule, stopped frees budget
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}${BLUE}# 8. Move plan status revise then stopped${RESET}"
REVISE_RESP=$(curl -s -X POST "$API_BASE/api/v1/plans/AI1/status" -H "Content-Type: application/json" -d '{"status":"revise"}')
STOP_RESP=$(curl -s -X POST "$API_BASE/api/v1/plans/AI1/status" -H "Content-Type: application/json" -d '{"status":"stopped"}')
echo "$REVISE_RESP" | $JSON_FORMATTER
echo "$STOP_RESP" | $JSON_FORMATTER

if echo "$REVISE_RESP" | grep -q '"status":"revise"' && echo "$STOP_RESP" | grep -q '"status":"stopped"'; then
    echo -e "${GREEN}--> Test 8 Passed: Status transitions revise then stopped accepted${RESET}"
else
    echo -e "${RED}--> Test 8 Failed: Plan status transition rejected${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# ------------------------------------------------------------------------------
# Test 9: Reallocation — recommendation only, never auto-applied
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}${BLUE}# 9. Reallocation recommendation after stopping AI1${RESET}"
echo -e "${CYAN}curl -s -X POST http://localhost:8080/api/v1/reallocation -d '{\"pilot_id\":\"AI1\"}' | jq${RESET}"
REALLOC_RESP=$(curl -s -X POST "$API_BASE/api/v1/reallocation" -H "Content-Type: application/json" -d '{"pilot_id":"AI1"}')
echo "$REALLOC_RESP" | $JSON_FORMATTER

if echo "$REALLOC_RESP" | grep -q '"accepted":false' && echo "$REALLOC_RESP" | grep -q '"freed_budget_inr":200000'; then
    echo -e "${GREEN}--> Test 9 Passed: AI1 freed 200000 as a recommendation only${RESET}"
else
    echo -e "${RED}--> Test 9 Failed: Reallocation did not free AI1's 200000 (or auto-accepted)${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# ------------------------------------------------------------------------------
# Test 10: Decision memo — one page markdown with plan and audit trail
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}${BLUE}# 10. Decision memo for AI1 (markdown)${RESET}"
echo -e "${CYAN}curl -s -X POST http://localhost:8080/api/v1/export/memo -d '{\"pilot_id\":\"AI1\"}'${RESET}"
MEMO_RESP=$(curl -s -X POST "$API_BASE/api/v1/export/memo" -H "Content-Type: application/json" -d '{"pilot_id":"AI1"}')

echo "$MEMO_RESP" | head -12

if echo "$MEMO_RESP" | grep -q "Decision memo — AI1" && echo "$MEMO_RESP" | grep -q "## Audit trail"; then
    echo -e "${GREEN}--> Test 10 Passed: Memo rendered as one-page markdown with audit trail${RESET}"
else
    echo -e "${RED}--> Test 10 Failed: Memo missing heading or audit trail${RESET}"
    FAILED_TESTS=$((FAILED_TESTS + 1))
fi

if [ "$FAILED_TESTS" -gt 0 ]; then
    echo ""
    echo -e "${BOLD}${RED}=======================================================${RESET}"
    echo -e "${BOLD}${RED}            INTEGRATION TESTS FAILED ($FAILED_TESTS FAILED)     ${RESET}"
    echo -e "${BOLD}${RED}=======================================================${RESET}"
    exit 1
fi

echo ""
echo -e "${BOLD}${GREEN}=======================================================${RESET}"
echo -e "${BOLD}${GREEN}             ALL INTEGRATION TESTS PASSED              ${RESET}"
echo -e "${BOLD}${GREEN}=======================================================${RESET}"

# (Cleanup runs automatically via trap cleanup EXIT)
