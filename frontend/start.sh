#!/usr/bin/env bash
# ==============================================================================
# Frontend Startup Script (Linux/macOS)
# Performs pre-flight checks, installs dependencies, and starts the Vite dev
# server on :5173, proxying /api, /health and /ready to the Go gateway
# (VITE_PROXY_TARGET, default http://127.0.0.1:8080).
# ==============================================================================

set -uo pipefail

# ANSI color codes
if [ -t 1 ]; then
    BOLD="\033[1m"; GREEN="\033[0;32m"; RED="\033[0;31m"; YELLOW="\033[1;33m"; BLUE="\033[0;34m"; CYAN="\033[0;36m"; RESET="\033[0m"
else
    BOLD=""; GREEN=""; RED=""; YELLOW=""; BLUE=""; CYAN=""; RESET=""
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo -e "${BOLD}${CYAN}=======================================================${RESET}"
echo -e "${BOLD}${CYAN}          FRONTEND PRE-FLIGHT CHECKS                   ${RESET}"
echo -e "${BOLD}${CYAN}=======================================================${RESET}"

FAILED_ITEMS=()
WHAT_TO_INSTALL=()
CHECK_SUMMARY=()

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
# 1. Check Node.js and npm
# ------------------------------------------------------------------------------
NODE_MAJOR=0
if command -v node &>/dev/null; then
    NODE_MAJOR=$(node -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo 0)
    if [ "$NODE_MAJOR" -ge 20 ] 2>/dev/null; then
        CHECK_SUMMARY+=("${GREEN}[PASS] Node.js:${RESET} $(node --version) (npm $(npm --version))")
    else
        CHECK_SUMMARY+=("${RED}[FAIL] Node.js:${RESET} Version $(node --version) is too old (>= 20 required)")
        FAILED_ITEMS+=("Node.js 20+ is required")
        WHAT_TO_INSTALL+=("Upgrade Node.js: sudo apt install -y nodejs npm a newer Node LTS (or use nvm: nvm install --lts)")
    fi
else
    CHECK_SUMMARY+=("${RED}[FAIL] Node.js:${RESET} Not found in PATH")
    FAILED_ITEMS+=("Node.js is missing")
    WHAT_TO_INSTALL+=("Install Node.js 20 LTS (Ubuntu/Debian: use nvm (https://github.com/nvm-sh/nvm) or download from https://nodejs.org)")
fi

# ------------------------------------------------------------------------------
# 2. Install dependencies if node_modules is missing
# ------------------------------------------------------------------------------
if command -v npm &>/dev/null; then
    if [ -d "node_modules" ] && [ -x "node_modules/.bin/vite" ]; then
        CHECK_SUMMARY+=("${GREEN}[PASS] Dependencies:${RESET} node_modules present (vite installed)")
    else
        echo -e "${BLUE}[INFO] Installing npm dependencies ...${RESET}"
        # npm ci installs exactly what package-lock.json pins and never rewrites the tracked lockfile
        if npm ci; then
            CHECK_SUMMARY+=("${GREEN}[PASS] Dependencies:${RESET} npm ci completed")
        else
            CHECK_SUMMARY+=("${RED}[FAIL] Dependencies:${RESET} npm ci failed")
            FAILED_ITEMS+=("npm ci failed")
            WHAT_TO_INSTALL+=("Run: npm ci in frontend/ (check network or proxy)")
        fi
    fi
fi

# ------------------------------------------------------------------------------
# 3. Gateway reachability (the Vite proxy target: VITE_PROXY_TARGET, default http://127.0.0.1:8080)
# ------------------------------------------------------------------------------
PROXY_TARGET="${VITE_PROXY_TARGET:-}"
for f in .env.local .env; do
    if [ -z "$PROXY_TARGET" ] && [ -f "$f" ]; then
        PROXY_TARGET=$(grep -E '^VITE_PROXY_TARGET=' "$f" | tail -1 | cut -d '=' -f2- | tr -d '\r"')
    fi
done
PROXY_TARGET="${PROXY_TARGET:-http://127.0.0.1:8080}"
GW_HOST="127.0.0.1"; GW_PORT="8080"
if [[ "$PROXY_TARGET" =~ //([^:/]+) ]]; then GW_HOST="${BASH_REMATCH[1]}"; [ "$GW_HOST" = "localhost" ] && GW_HOST="127.0.0.1"; fi
if [[ "$PROXY_TARGET" =~ :([0-9]+) ]]; then GW_PORT="${BASH_REMATCH[1]}"; fi

if check_tcp_port "$GW_HOST" "$GW_PORT"; then
    GW_HEALTH=""
    command -v curl &>/dev/null && GW_HEALTH=$(curl -s --max-time 2 "$PROXY_TARGET/health" || true)
    GW_AUTH=$(echo "$GW_HEALTH" | sed -n 's/.*"auth_mode": *"\([^"]*\)".*/\1/p')
    GW_RULES=$(echo "$GW_HEALTH" | sed -n 's/.*"rules_version": *"\([^"]*\)".*/\1/p')
    CHECK_SUMMARY+=("${GREEN}[PASS] Go gateway:${RESET} Reachable at $PROXY_TARGET (proxy target)${GW_RULES:+, engine rules version $GW_RULES}")
    if [ "$GW_AUTH" = "required" ]; then
        CHECK_SUMMARY+=("${YELLOW}[WARN] Sign-in:${RESET} The gateway runs with AUTH_MODE=required; these screens do not send a token yet, so API calls will answer 401. Use AUTH_MODE=off for local UI work.")
    fi
else
    CHECK_SUMMARY+=("${YELLOW}[WARN] Go gateway:${RESET} Not running at $PROXY_TARGET yet (start it in another terminal: cd ../backend && ./start.sh). Pages will show a connection notice until it is up.")
fi

# ------------------------------------------------------------------------------
# 4. Check port 5173 availability
# ------------------------------------------------------------------------------
if check_tcp_port "127.0.0.1" 5173; then
    CHECK_SUMMARY+=("${YELLOW}[WARN] Port 5173:${RESET} In use (is the dev server already running? Vite will pick the next free port)")
else
    CHECK_SUMMARY+=("${GREEN}[PASS] Port 5173:${RESET} Available")
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
    echo -e "${RED}Please resolve the issues above before running the frontend.${RESET}"
    exit 1
fi

echo -e "${BOLD}${GREEN}=======================================================${RESET}"
echo -e "${BOLD}${GREEN}        ALL CHECKS PASSED - STARTING APP               ${RESET}"
echo -e "${BOLD}${GREEN}=======================================================${RESET}"
echo -e "Command: ${BOLD}npm run dev${RESET}"
echo -e "Frontend URL: ${CYAN}http://localhost:5173${RESET}"
echo ""

exec npm run dev
