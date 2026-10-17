#!/usr/bin/env bash
# ==============================================================================
# Backend Startup Script (Linux/macOS)
# Performs pre-flight checks, dependency downloads, .env verification,
# starts Redis, checks PostgreSQL, and runs: go run ./cmd/api
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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo -e "${BOLD}${CYAN}=======================================================${RESET}"
echo -e "${BOLD}${CYAN}            BACKEND PRE-FLIGHT CHECKS                  ${RESET}"
echo -e "${BOLD}${CYAN}=======================================================${RESET}"

FAILED_ITEMS=()
WHAT_TO_INSTALL=()
CHECK_SUMMARY=()

# Helper function to check TCP port
check_tcp_port() {
    local host="$1"
    local port="$2"
    if command -v nc &>/dev/null; then
        nc -z -w 1 "$host" "$port" &>/dev/null
        return $?
    elif (echo > /dev/tcp/"$host"/"$port") 2>/dev/null; then
        return 0
    else
        return 1
    fi
}

# ------------------------------------------------------------------------------
# 1. Check Go runtime
# ------------------------------------------------------------------------------
if ! command -v go &>/dev/null; then
    CHECK_SUMMARY+=("${RED}[FAIL] Go Runtime:${RESET} Not found in PATH")
    FAILED_ITEMS+=("Go runtime is missing")
    WHAT_TO_INSTALL+=("Install Go 1.22+ (Ubuntu/Debian: sudo apt update && sudo apt install -y golang-go, or download from https://go.dev/dl/)")
else
    GO_VERSION=$(go version | awk '{print $3}')
    CHECK_SUMMARY+=("${GREEN}[PASS] Go Runtime:${RESET} $GO_VERSION")
fi

# ------------------------------------------------------------------------------
# 2. Check .env configuration file
# ------------------------------------------------------------------------------
if [ -f ".env" ]; then
    CHECK_SUMMARY+=("${GREEN}[PASS] Environment File:${RESET} backend/.env is present")
else
    if [ -f ".env.example" ]; then
        cp ".env.example" ".env"
        CHECK_SUMMARY+=("${GREEN}[PASS] Environment File:${RESET} Created backend/.env from .env.example")
    else
        CHECK_SUMMARY+=("${RED}[FAIL] Environment File:${RESET} Neither .env nor .env.example found")
        FAILED_ITEMS+=(".env configuration missing")
        WHAT_TO_INSTALL+=("Create backend/.env with DATABASE_URL, PORT, REDIS_URL, etc.")
    fi
fi

# Parse .env if available
PORT="8080"
DATABASE_URL=""
REDIS_URL="redis://localhost:6379/0"
RULES_ENGINE_URL="http://localhost:8000"

if [ -f ".env" ]; then
    # Read variables without evaluating commands
    while IFS='=' read -r key val || [ -n "$key" ]; do
        key=$(echo "$key" | tr -d ' \r\t')
        [[ "$key" =~ ^#.*$ ]] && continue
        [ -z "$key" ] && continue
        val=$(echo "$val" | sed -e 's/^[ \t]*//' -e 's/[ \t]*$//' -e 's/\r$//')
        case "$key" in
            PORT) PORT="$val" ;;
            DATABASE_URL) DATABASE_URL="$val" ;;
            REDIS_URL) REDIS_URL="$val" ;;
            RULES_ENGINE_URL) RULES_ENGINE_URL="$val" ;;
        esac
    done < ".env"

    if [[ "$DATABASE_URL" == *"CHANGE_ME"* ]]; then
        CHECK_SUMMARY+=("${YELLOW}[WARN] Database Config:${RESET} DATABASE_URL contains placeholder password 'CHANGE_ME'. Ensure database user is configured.")
    fi
fi

# ------------------------------------------------------------------------------
# 3. Check & Download Go dependencies
# ------------------------------------------------------------------------------
if command -v go &>/dev/null; then
    if [ -f "go.mod" ]; then
        echo -e "${BLUE}[INFO] Verifying and downloading Go modules ...${RESET}"
        if go mod download 2>&1; then
            CHECK_SUMMARY+=("${GREEN}[PASS] Dependencies:${RESET} Go modules verified and downloaded")
        else
            CHECK_SUMMARY+=("${RED}[FAIL] Dependencies:${RESET} go mod download failed")
            FAILED_ITEMS+=("Go dependencies download failed")
            WHAT_TO_INSTALL+=("Run: go mod download in backend/ (check internet connection or proxy)")
        fi
    else
        CHECK_SUMMARY+=("${RED}[FAIL] Dependencies:${RESET} go.mod not found in backend/")
        FAILED_ITEMS+=("backend/go.mod is missing")
    fi
fi

# ------------------------------------------------------------------------------
# 4. Check & Start Redis
# ------------------------------------------------------------------------------
REDIS_HOST="127.0.0.1"
REDIS_PORT="6379"

# Determine if Redis is configured on localhost
IS_LOCAL_REDIS=false
if [[ "$REDIS_URL" =~ (localhost|127\.0\.0\.1):?([0-9]*) ]]; then
    IS_LOCAL_REDIS=true
    if [ -n "${BASH_REMATCH[2]:-}" ]; then
        REDIS_PORT="${BASH_REMATCH[2]}"
    fi
fi

if [ "$IS_LOCAL_REDIS" = true ]; then
    REDIS_RUNNING=false

    if command -v redis-cli &>/dev/null; then
        if redis-cli -p "$REDIS_PORT" ping 2>/dev/null | grep -q "PONG"; then
            REDIS_RUNNING=true
        fi
    elif check_tcp_port "$REDIS_HOST" "$REDIS_PORT"; then
        REDIS_RUNNING=true
    fi

    if [ "$REDIS_RUNNING" = false ]; then
        echo -e "${BLUE}[INFO] Redis is not running. Attempting to start Redis server ...${RESET}"

        # Attempt 1: Start redis-server as daemon directly
        if command -v redis-server &>/dev/null; then
            redis-server --daemonize yes --port "$REDIS_PORT" &>/dev/null || true
            sleep 1
        fi

        # Attempt 2: Try systemctl / service if redis-server is still not responding
        if ! check_tcp_port "$REDIS_HOST" "$REDIS_PORT" && command -v systemctl &>/dev/null; then
            systemctl start redis-server 2>/dev/null || systemctl start redis 2>/dev/null || true
            sleep 1
        elif ! check_tcp_port "$REDIS_HOST" "$REDIS_PORT" && command -v service &>/dev/null; then
            service redis-server start 2>/dev/null || service redis start 2>/dev/null || true
            sleep 1
        fi

        # Attempt 3: Docker container if docker is running
        if ! check_tcp_port "$REDIS_HOST" "$REDIS_PORT" && command -v docker &>/dev/null; then
            docker start redis &>/dev/null || true
            sleep 1
        fi

        # Verify if Redis started
        if command -v redis-cli &>/dev/null && redis-cli -p "$REDIS_PORT" ping 2>/dev/null | grep -q "PONG"; then
            REDIS_RUNNING=true
        elif check_tcp_port "$REDIS_HOST" "$REDIS_PORT"; then
            REDIS_RUNNING=true
        fi

        if [ "$REDIS_RUNNING" = true ]; then
            CHECK_SUMMARY+=("${GREEN}[PASS] Redis Service:${RESET} Started and responding on port $REDIS_PORT (PONG)")
        else
            CHECK_SUMMARY+=("${RED}[FAIL] Redis Service:${RESET} Not running on port $REDIS_PORT and could not be started")
            FAILED_ITEMS+=("Redis is not running")
            if ! command -v redis-server &>/dev/null; then
                WHAT_TO_INSTALL+=("Install Redis server:
      - Ubuntu/Debian: sudo apt update && sudo apt install -y redis-server
      - macOS: brew install redis && brew services start redis
      - Docker: docker run -d -p 6379:6379 --name redis redis:alpine")
            else
                WHAT_TO_INSTALL+=("Start Redis server: redis-server --daemonize yes (or: sudo systemctl start redis)")
            fi
        fi
    else
        CHECK_SUMMARY+=("${GREEN}[PASS] Redis Service:${RESET} Running on port $REDIS_PORT (PONG)")
    fi
else
    CHECK_SUMMARY+=("${BLUE}[INFO] Redis Service:${RESET} Remote or disabled via REDIS_URL")
fi

# ------------------------------------------------------------------------------
# 5. Check PostgreSQL database connectivity
# ------------------------------------------------------------------------------
DB_HOST="127.0.0.1"
DB_PORT="5432"

if [[ "$DATABASE_URL" =~ @([^:/]+):?([0-9]*)/ ]]; then
    DB_HOST="${BASH_REMATCH[1]}"
    if [ -n "${BASH_REMATCH[2]:-}" ]; then
        DB_PORT="${BASH_REMATCH[2]}"
    fi
fi

if check_tcp_port "$DB_HOST" "$DB_PORT" || (command -v pg_isready &>/dev/null && pg_isready -h "$DB_HOST" -p "$DB_PORT" &>/dev/null); then
    CHECK_SUMMARY+=("${GREEN}[PASS] PostgreSQL:${RESET} Accepting connections on $DB_HOST:$DB_PORT")
else
    # Try starting postgresql service if local
    if [[ "$DB_HOST" == "127.0.0.1" || "$DB_HOST" == "localhost" ]]; then
        if command -v systemctl &>/dev/null; then
            systemctl start postgresql 2>/dev/null || true
            sleep 1
        elif command -v service &>/dev/null; then
            service postgresql start 2>/dev/null || true
            sleep 1
        fi
    fi

    if check_tcp_port "$DB_HOST" "$DB_PORT" || (command -v pg_isready &>/dev/null && pg_isready -h "$DB_HOST" -p "$DB_PORT" &>/dev/null); then
        CHECK_SUMMARY+=("${GREEN}[PASS] PostgreSQL:${RESET} Started and accepting connections on $DB_HOST:$DB_PORT")
    else
        CHECK_SUMMARY+=("${RED}[FAIL] PostgreSQL:${RESET} Unreachable at $DB_HOST:$DB_PORT")
        FAILED_ITEMS+=("PostgreSQL database is unreachable")
        WHAT_TO_INSTALL+=("Ensure PostgreSQL is running and database role/database exists:
      - Ubuntu/Debian: sudo apt install -y postgresql postgresql-contrib && sudo systemctl start postgresql
      - Initialize database: sudo -u postgres psql -f db/init.sql")
    fi
fi

# ------------------------------------------------------------------------------
# 6. Check Decision Engine (Rules Engine URL)
# ------------------------------------------------------------------------------
DE_HOST="127.0.0.1"
DE_PORT="8000"
if [[ "$RULES_ENGINE_URL" =~ :([0-9]+) ]]; then
    DE_PORT="${BASH_REMATCH[1]}"
fi

if check_tcp_port "$DE_HOST" "$DE_PORT"; then
    CHECK_SUMMARY+=("${GREEN}[PASS] Decision Engine:${RESET} Reachable on port $DE_PORT")
else
    CHECK_SUMMARY+=("${BLUE}[INFO] Decision Engine:${RESET} Not running on port $DE_PORT (Start it in another terminal: cd ../decision-engine && ./start.sh)")
fi

# ------------------------------------------------------------------------------
# 7. Check Backend Port availability
# ------------------------------------------------------------------------------
if check_tcp_port "127.0.0.1" "$PORT"; then
    CHECK_SUMMARY+=("${YELLOW}[WARN] Port $PORT:${RESET} Port $PORT is currently in use (may conflict with backend)")
else
    CHECK_SUMMARY+=("${GREEN}[PASS] Port $PORT:${RESET} Available")
fi

# ------------------------------------------------------------------------------
# Print Summary Table
# ------------------------------------------------------------------------------
echo ""
echo -e "${BOLD}Check Results:${RESET}"
for line in "${CHECK_SUMMARY[@]}"; do
    echo -e "  $line"
done
echo ""

# ------------------------------------------------------------------------------
# Final Result & Action Required
# ------------------------------------------------------------------------------
if [ ${#FAILED_ITEMS[@]} -gt 0 ]; then
    echo -e "${BOLD}${RED}=======================================================${RESET}"
    echo -e "${BOLD}${RED}               PRE-FLIGHT CHECKS FAILED!               ${RESET}"
    echo -e "${BOLD}${RED}=======================================================${RESET}"
    echo -e "${BOLD}What is left to install / fix:${RESET}"
    index=1
    for item in "${WHAT_TO_INSTALL[@]}"; do
        echo -e "  ${YELLOW}$index.${RESET} $item"
        ((index++))
    done
    echo ""
    echo -e "${RED}Please resolve the issues above before running Backend.${RESET}"
    exit 1
fi

echo -e "${BOLD}${GREEN}=======================================================${RESET}"
echo -e "${BOLD}${GREEN}        ALL CHECKS PASSED - STARTING APP               ${RESET}"
echo -e "${BOLD}${GREEN}=======================================================${RESET}"
echo -e "Command: ${BOLD}go run ./cmd/api${RESET}"
echo -e "Backend API URL: ${CYAN}http://localhost:${PORT}${RESET}"
echo ""

# Execute backend
exec go run ./cmd/api
