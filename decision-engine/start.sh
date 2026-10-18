#!/usr/bin/env bash
# ==============================================================================
# Decision Engine Startup Script (Linux/macOS)
# Performs pre-flight checks, dependency installs, .env verification,
# and starts the service: uvicorn app.main:app on HOST:PORT from .env (reload when RELOAD=true)
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
echo -e "${BOLD}${CYAN}         DECISION ENGINE PRE-FLIGHT CHECKS             ${RESET}"
echo -e "${BOLD}${CYAN}=======================================================${RESET}"

FAILED_ITEMS=()
WHAT_TO_INSTALL=()
CHECK_SUMMARY=()

# ------------------------------------------------------------------------------
# 1. Check Python runtime
# ------------------------------------------------------------------------------
PYTHON_BIN=""
if command -v python3 &>/dev/null; then
    PYTHON_BIN="python3"
elif command -v python &>/dev/null; then
    PYTHON_BIN="python"
fi

if [ -z "$PYTHON_BIN" ]; then
    CHECK_SUMMARY+=("${RED}[FAIL] Python Runtime:${RESET} Not found")
    FAILED_ITEMS+=("Python 3 is missing")
    WHAT_TO_INSTALL+=("Install Python 3 (Ubuntu/Debian: sudo apt update && sudo apt install -y python3 python3-venv python3-pip)")
else
    PY_VERSION=$("$PYTHON_BIN" -c "import sys; print('.'.join(map(str, sys.version_info[:3])))" 2>/dev/null || echo "unknown")
    if "$PYTHON_BIN" -c "import sys; sys.exit(0 if sys.version_info >= (3, 10) else 1)" 2>/dev/null; then
        CHECK_SUMMARY+=("${GREEN}[PASS] Python Runtime:${RESET} $PYTHON_BIN (v$PY_VERSION)")
    else
        CHECK_SUMMARY+=("${RED}[FAIL] Python Runtime:${RESET} $PYTHON_BIN v$PY_VERSION is too old (3.10+ required)")
        FAILED_ITEMS+=("Python 3.10+ is required")
        WHAT_TO_INSTALL+=("Install Python 3.10 or newer (Ubuntu/Debian: sudo apt install -y python3 python3-venv)")
    fi
fi

# ------------------------------------------------------------------------------
# 2. Check .env configuration file
# ------------------------------------------------------------------------------
if [ -f ".env" ]; then
    CHECK_SUMMARY+=("${GREEN}[PASS] Environment File:${RESET} .env is present")
else
    if [ -f ".env.example" ]; then
        cp ".env.example" ".env"
        CHECK_SUMMARY+=("${GREEN}[PASS] Environment File:${RESET} Created .env from .env.example")
    else
        # Generate default .env.example and .env if neither exists
        cat << 'EOF' > .env.example
# Decision Engine Configuration
PORT=8000
HOST=0.0.0.0
RELOAD=true
EOF
        cp ".env.example" ".env"
        CHECK_SUMMARY+=("${GREEN}[PASS] Environment File:${RESET} Generated default .env from .env.example")
    fi
fi

# Read PORT / HOST / RELOAD from .env (real environment variables win)
ENV_PORT=""; ENV_HOST=""; ENV_RELOAD=""
if [ -f ".env" ]; then
    while IFS='=' read -r key val || [ -n "$key" ]; do
        key=$(echo "$key" | tr -d ' \r\t')
        [[ "$key" =~ ^#.*$ ]] && continue
        val=$(echo "$val" | sed -e 's/^[ \t]*//' -e 's/[ \t]*$//' -e 's/\r$//')
        case "$key" in
            PORT) ENV_PORT="$val" ;;
            HOST) ENV_HOST="$val" ;;
            RELOAD) ENV_RELOAD="$val" ;;
        esac
    done < ".env"
fi
APP_PORT="${PORT:-${ENV_PORT:-8000}}"
APP_HOST="${HOST:-${ENV_HOST:-0.0.0.0}}"
APP_RELOAD="${RELOAD:-${ENV_RELOAD:-true}}"
if ! [[ "$APP_PORT" =~ ^[0-9]+$ ]]; then
    CHECK_SUMMARY+=("${RED}[FAIL] Port:${RESET} PORT='$APP_PORT' is not a number")
    FAILED_ITEMS+=("PORT is invalid")
    WHAT_TO_INSTALL+=("Set PORT to a number in decision-engine/.env (default 8000)")
    APP_PORT=8000
fi

# ------------------------------------------------------------------------------
# 3. Check Virtual Environment (.venv / venv)
# ------------------------------------------------------------------------------
VENV_DIR=""
if [ -d ".venv" ] && [ -f ".venv/bin/activate" ]; then
    VENV_DIR=".venv"
elif [ -d "venv" ] && [ -f "venv/bin/activate" ]; then
    VENV_DIR="venv"
fi

if [ -z "$VENV_DIR" ]; then
    if [ -n "$PYTHON_BIN" ]; then
        echo -e "${BLUE}[INFO] Creating Python virtual environment in .venv ...${RESET}"
        if "$PYTHON_BIN" -m venv .venv 2>/dev/null; then
            VENV_DIR=".venv"
            CHECK_SUMMARY+=("${GREEN}[PASS] Virtual Environment:${RESET} Created and configured at .venv")
        else
            CHECK_SUMMARY+=("${RED}[FAIL] Virtual Environment:${RESET} Failed to create virtual environment")
            FAILED_ITEMS+=("Python venv creation failed")
            WHAT_TO_INSTALL+=("Install python3-venv package (Ubuntu/Debian: sudo apt install -y python3-venv)")
        fi
    fi
else
    CHECK_SUMMARY+=("${GREEN}[PASS] Virtual Environment:${RESET} Found at $VENV_DIR")
fi

# ------------------------------------------------------------------------------
# 4. Check & Install Dependencies
# ------------------------------------------------------------------------------
if [ -n "$VENV_DIR" ] && [ -f "$VENV_DIR/bin/python" ]; then
    VENV_PYTHON="$VENV_DIR/bin/python"
    VENV_PIP="$VENV_DIR/bin/pip"

    # Check if required packages are importable
    DEPS_OK=true
    # python-multipart serves the CSV/XLSX upload endpoints (no other new dependency: XLSX is read with the stdlib)
    if ! "$VENV_PYTHON" -c "import fastapi, uvicorn, httpx, pytest, importlib.util as u; assert u.find_spec('python_multipart') or u.find_spec('multipart')" &>/dev/null; then
        DEPS_OK=false
    fi

    if [ "$DEPS_OK" = false ]; then
        if [ -f "requirements.txt" ]; then
            echo -e "${BLUE}[INFO] Installing missing dependencies from requirements.txt ...${RESET}"
            if "$VENV_PIP" install -r requirements.txt; then
                CHECK_SUMMARY+=("${GREEN}[PASS] Dependencies:${RESET} Installed required packages successfully")
            else
                CHECK_SUMMARY+=("${RED}[FAIL] Dependencies:${RESET} Installation failed")
                FAILED_ITEMS+=("Dependencies installation failed")
                WHAT_TO_INSTALL+=("Run: $VENV_PIP install -r requirements.txt (check network / pip logs)")
            fi
        else
            CHECK_SUMMARY+=("${RED}[FAIL] Dependencies:${RESET} requirements.txt missing")
            FAILED_ITEMS+=("requirements.txt missing")
            WHAT_TO_INSTALL+=("Create requirements.txt with: fastapi, uvicorn, python-multipart, pytest, httpx")
        fi
    else
        CHECK_SUMMARY+=("${GREEN}[PASS] Dependencies:${RESET} All required packages installed")
    fi
else
    if [ ${#FAILED_ITEMS[@]} -eq 0 ]; then
        CHECK_SUMMARY+=("${RED}[FAIL] Dependencies:${RESET} Cannot verify dependencies without active venv")
        FAILED_ITEMS+=("No virtual environment")
    fi
fi

# ------------------------------------------------------------------------------
# 5. Check port availability
# ------------------------------------------------------------------------------
PORT_IN_USE=false
if [ -n "$PYTHON_BIN" ]; then
    PORT_IN_USE=$("$PYTHON_BIN" -c "
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
try:
    s.bind(('127.0.0.1', $APP_PORT))
    s.close()
    print('free')
except OSError:
    print('in_use')
" 2>/dev/null || echo "unknown")
fi

if [ "$PORT_IN_USE" = "in_use" ]; then
    CHECK_SUMMARY+=("${YELLOW}[WARN] Port $APP_PORT:${RESET} Port $APP_PORT is currently in use. (May conflict with uvicorn)")
elif [ "$PORT_IN_USE" = "free" ]; then
    CHECK_SUMMARY+=("${GREEN}[PASS] Port $APP_PORT:${RESET} Available")
else
    CHECK_SUMMARY+=("${BLUE}[INFO] Port $APP_PORT:${RESET} Check skipped")
fi

# ------------------------------------------------------------------------------
# 6. Engine versions (reported on GET /health; the gateway's cache key follows rules_version)
# ------------------------------------------------------------------------------
if [ -f "app/version.py" ]; then
    RV=$(sed -n 's/^RULES_VERSION = "\(.*\)"/\1/p' app/version.py)
    CV=$(sed -n 's/^CONTRACT_VERSION = \(.*\)/\1/p' app/version.py)
    CHECK_SUMMARY+=("${GREEN}[PASS] Engine version:${RESET} rules $RV · contract $CV (examples in contracts/engine/)")
    if [ -f "contracts/VERSION" ] && [ "$(tr -d '[:space:]' < contracts/VERSION)" != "$CV" ]; then
        CHECK_SUMMARY+=("${YELLOW}[WARN] Contracts:${RESET} contracts/VERSION differs from CONTRACT_VERSION; run: python scripts/build_contracts.py")
    fi
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
    echo -e "${RED}Please resolve the issues above before running Decision Engine.${RESET}"
    exit 1
fi

echo -e "${BOLD}${GREEN}=======================================================${RESET}"
echo -e "${BOLD}${GREEN}        ALL CHECKS PASSED - STARTING APP               ${RESET}"
echo -e "${BOLD}${GREEN}=======================================================${RESET}"
RELOAD_FLAG=""
[ "$APP_RELOAD" = "true" ] && RELOAD_FLAG="--reload"
echo -e "Command: ${BOLD}uvicorn app.main:app $RELOAD_FLAG --host $APP_HOST --port $APP_PORT${RESET}"
echo -e "API Docs: ${CYAN}http://localhost:$APP_PORT/docs${RESET}"
echo ""

# Activate virtualenv and run the engine with the configured host, port and reload setting
source "$VENV_DIR/bin/activate"
exec uvicorn app.main:app $RELOAD_FLAG --host "$APP_HOST" --port "$APP_PORT"
