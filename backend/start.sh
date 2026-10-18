#!/usr/bin/env bash
# ==============================================================================
# Backend Startup Script (Linux/macOS)
# Performs pre-flight checks, dependency downloads, .env verification,
# starts Redis, checks PostgreSQL, applies pending migrations (SKIP_MIGRATE=1 to skip),
# checks the rules engine and its rules_version, and runs: go run ./cmd/api
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
    WHAT_TO_INSTALL+=("Install Go 1.25+ (download from https://go.dev/dl/; distro packages are often older)")
else
    GO_VERSION=$(go version | awk '{print $3}')
    GO_MINOR=$(echo "$GO_VERSION" | sed -E 's/^go1\.([0-9]+).*/\1/')
    if [[ "$GO_MINOR" =~ ^[0-9]+$ ]] && [ "$GO_MINOR" -lt 25 ]; then
        CHECK_SUMMARY+=("${RED}[FAIL] Go Runtime:${RESET} $GO_VERSION is too old (go.mod needs 1.25+)")
        FAILED_ITEMS+=("Go 1.25+ is required")
        WHAT_TO_INSTALL+=("Upgrade Go to 1.25+ (https://go.dev/dl/)")
    else
        CHECK_SUMMARY+=("${GREEN}[PASS] Go Runtime:${RESET} $GO_VERSION")
    fi
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
AUTH_MODE="off"
AUTO_MIGRATE="false"
WORKER_ENABLED="true"
RATE_LIMIT_PER_MINUTE="600"

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
            AUTH_MODE) AUTH_MODE=$(echo "$val" | tr '[:upper:]' '[:lower:]') ;;
            AUTO_MIGRATE) AUTO_MIGRATE="$val" ;;
            WORKER_ENABLED) WORKER_ENABLED="$val" ;;
            RATE_LIMIT_PER_MINUTE) RATE_LIMIT_PER_MINUTE="$val" ;;
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
# 5b. Apply pending database migrations (00001-00012: workspaces, uploads, snapshots ...)
# ------------------------------------------------------------------------------
DB_UP=false
if check_tcp_port "$DB_HOST" "$DB_PORT" || (command -v pg_isready &>/dev/null && pg_isready -h "$DB_HOST" -p "$DB_PORT" &>/dev/null); then
    DB_UP=true
fi
if [ "${SKIP_MIGRATE:-}" = "1" ]; then
    CHECK_SUMMARY+=("${BLUE}[INFO] Migrations:${RESET} Skipped (SKIP_MIGRATE=1); run: go run ./cmd/api migrate up")
elif [ "$AUTO_MIGRATE" = "true" ]; then
    CHECK_SUMMARY+=("${BLUE}[INFO] Migrations:${RESET} AUTO_MIGRATE=true: the gateway applies them when it starts")
elif [ "$DB_UP" = true ] && [ -n "$DATABASE_URL" ] && command -v go &>/dev/null; then
    echo -e "${BLUE}[INFO] Checking database migrations ...${RESET}"
    PENDING=$(go run ./cmd/api migrate status 2>/dev/null | grep -ci "pending" || true)
    if [ "$PENDING" -gt 0 ] 2>/dev/null; then
        echo -e "${BLUE}[INFO] Applying $PENDING pending migration(s) ...${RESET}"
        if MIG_OUT=$(go run ./cmd/api migrate up 2>&1); then
            CHECK_SUMMARY+=("${GREEN}[PASS] Migrations:${RESET} Applied $PENDING pending migration(s)")
        else
            CHECK_SUMMARY+=("${RED}[FAIL] Migrations:${RESET} migrate up failed: $(echo "$MIG_OUT" | tail -1)")
            FAILED_ITEMS+=("Database migrations failed")
            WHAT_TO_INSTALL+=("Run: go run ./cmd/api migrate up (in backend/) and fix the reported error")
        fi
    else
        CHECK_SUMMARY+=("${GREEN}[PASS] Migrations:${RESET} Database schema is up to date")
    fi
fi

# ------------------------------------------------------------------------------
# 6. Check Decision Engine (Rules Engine URL) and the rules_version it reports
# ------------------------------------------------------------------------------
DE_HOST="127.0.0.1"
DE_PORT="8000"
if [[ "$RULES_ENGINE_URL" =~ //([^:/]+) ]]; then
    DE_HOST="${BASH_REMATCH[1]}"
    [ "$DE_HOST" = "localhost" ] && DE_HOST="127.0.0.1"
fi
if [[ "$RULES_ENGINE_URL" =~ :([0-9]+) ]]; then
    DE_PORT="${BASH_REMATCH[1]}"
fi

if check_tcp_port "$DE_HOST" "$DE_PORT"; then
    DE_VERSION=""
    if command -v curl &>/dev/null; then
        DE_VERSION=$(curl -s --max-time 2 "$RULES_ENGINE_URL/health" | sed -n 's/.*"rules_version": *"\([^"]*\)".*/\1/p')
    fi
    if [ -n "$DE_VERSION" ]; then
        CHECK_SUMMARY+=("${GREEN}[PASS] Decision Engine:${RESET} Reachable on port $DE_PORT (rules version $DE_VERSION; the cache key follows it)")
    else
        CHECK_SUMMARY+=("${YELLOW}[WARN] Decision Engine:${RESET} Reachable on port $DE_PORT but reports no rules_version (older engine? update decision-engine/)")
    fi
else
    CHECK_SUMMARY+=("${BLUE}[INFO] Decision Engine:${RESET} Not running on port $DE_PORT (Start it in another terminal: cd ../decision-engine && ./start.sh)")
fi

# ------------------------------------------------------------------------------
# 6b. Security and upload worker settings
# ------------------------------------------------------------------------------
case "$AUTH_MODE" in
    required)
        CHECK_SUMMARY+=("${BLUE}[INFO] Sign-in:${RESET} AUTH_MODE=required (bearer tokens). First admin + token: go run ./cmd/api bootstrap -email you@example.com -workspace \"Finance\"")
        ;;
    off)
        CHECK_SUMMARY+=("${BLUE}[INFO] Sign-in:${RESET} AUTH_MODE=off (single default workspace, no tokens; for local use only)")
        ;;
    *)
        CHECK_SUMMARY+=("${RED}[FAIL] Sign-in:${RESET} AUTH_MODE='$AUTH_MODE' (must be off or required)")
        FAILED_ITEMS+=("AUTH_MODE is invalid")
        WHAT_TO_INSTALL+=("Set AUTH_MODE=off or AUTH_MODE=required in backend/.env")
        ;;
esac
if [ "$RATE_LIMIT_PER_MINUTE" = "0" ]; then
    CHECK_SUMMARY+=("${YELLOW}[WARN] Rate limit:${RESET} Disabled (RATE_LIMIT_PER_MINUTE=0)")
else
    CHECK_SUMMARY+=("${GREEN}[PASS] Rate limit:${RESET} $RATE_LIMIT_PER_MINUTE requests/minute per client IP")
fi
if [ "$WORKER_ENABLED" = "false" ]; then
    CHECK_SUMMARY+=("${YELLOW}[WARN] Upload worker:${RESET} WORKER_ENABLED=false (stored uploads stay queued unless another gateway runs the worker)")
else
    CHECK_SUMMARY+=("${GREEN}[PASS] Upload worker:${RESET} Runs inside the gateway (parses stored uploads, retries with backoff)")
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
echo -e "Backend API URL: ${CYAN}http://localhost:${PORT}${RESET}  (sign-in: ${AUTH_MODE})"
echo ""

# Execute backend
exec go run ./cmd/api
