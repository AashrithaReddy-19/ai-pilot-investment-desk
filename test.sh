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
        DELETED_EVALS=$(psql "$DATABASE_URL" -t -A -c "DELETE FROM evaluations WHERE id > $PREV_EVAL_ID RETURNING id;" 2>/dev/null | grep -v '^$' | wc -l || echo "0")
        if [ "$DELETED_EVALS" -gt 0 ]; then
            echo -e "${GREEN}[CLEANUP PASS]${RESET} Deleted $DELETED_EVALS evaluation history row(s) from Postgres"
        fi
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
