#!/bin/bash

set -euo pipefail

# CyberStrikeAI one-click deployment startup script
ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT_DIR"

# Color definition
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

#Print colored messages
info() { echo -e "${BLUE}ℹ️  $1${NC}"; }
success() { echo -e "${GREEN}✅ $1${NC}"; }
warning() { echo -e "${YELLOW}⚠️  $1${NC}"; }
error() { echo -e "${RED}❌ $1${NC}"; }
note() { echo -e "${CYAN}ℹ️  $1${NC}"; }

# Temporary source configuration (only takes effect in this script)
PIP_INDEX_URL="${PIP_INDEX_URL:-https://pypi.tuna.tsinghua.edu.cn/simple}"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

# Save original environment variables (for recovery)
ORIGINAL_PIP_INDEX_URL="${PIP_INDEX_URL:-}"
ORIGINAL_GOPROXY="${GOPROXY:-}"

# Progress display function
show_progress() {
    local pid=$1
    local message=$2
    local i=0
    local dots=""
    
# Check if the process exists
    if ! kill -0 "$pid" 2>/dev/null; then
# The process has ended, return immediately
        return 0
    fi
    
    while kill -0 "$pid" 2>/dev/null; do
        i=$((i + 1))
        case $((i % 4)) in
            0) dots="." ;;
            1) dots=".." ;;
            2) dots="..." ;;
            3) dots="...." ;;
        esac
        printf "\r${BLUE}⏳ %s%s${NC}" "$message" "$dots"
        sleep 0.5
        
#Check again whether the process still exists
        if ! kill -0 "$pid" 2>/dev/null; then
            break
        fi
    done
    printf "\r"
}

echo ""
echo "=========================================="
Echo "CyberStrikeAI one-click deployment startup script"
echo "=========================================="
echo ""

# Display temporary source configuration information
echo ""
Warning "⚠️ NOTE: This script will use a temporary mirror source to speed up downloads"
echo ""
Info "Python pip temporary image source:"
echo "  ${PIP_INDEX_URL}"
Info "Go Proxy temporary mirror source:"
echo "  ${GOPROXY}"
echo ""
Note "These settings only take effect while the script is running and do not modify the system configuration"
echo ""
sleep 1

CONFIG_FILE="$ROOT_DIR/config.yaml"
VENV_DIR="$ROOT_DIR/venv"
REQUIREMENTS_FILE="$ROOT_DIR/requirements.txt"
BINARY_NAME="cyberstrike-ai"

# Check configuration file
if [ ! -f "$CONFIG_FILE" ]; then
Error "The configuration file config.yaml does not exist"
Info "Please make sure to run this script in the project root directory"
    exit 1
fi

# Check and install Python environment
check_python() {
    if ! command -v python3 >/dev/null 2>&1; then
Error "python3 not found"
        echo ""
Info "Please install Python 3.10 or higher first:"
        echo "  macOS:   brew install python3"
        echo "  Ubuntu:  sudo apt-get install python3 python3-venv"
        echo "  CentOS:  sudo yum install python3 python3-pip"
        exit 1
    fi
    
    PYTHON_VERSION=$(python3 --version 2>&1 | awk '{print $2}')
    PYTHON_MAJOR=$(echo "$PYTHON_VERSION" | cut -d. -f1)
    PYTHON_MINOR=$(echo "$PYTHON_VERSION" | cut -d. -f2)
    
    if [ "$PYTHON_MAJOR" -lt 3 ] || ([ "$PYTHON_MAJOR" -eq 3 ] && [ "$PYTHON_MINOR" -lt 10 ]); then
Error "Python version is too low: $PYTHON_VERSION (requires 3.10+)"
        exit 1
    fi
    
Success "Python environment check passed: $PYTHON_VERSION"
}

# Check and install Go environment
check_go() {
    if ! command -v go >/dev/null 2>&1; then
Error "Go not found"
        echo ""
Info "Please install Go 1.21 or higher first:"
        echo "  macOS:   brew install go"
        echo "  Ubuntu:  sudo apt-get install golang-go"
        echo "  CentOS:  sudo yum install golang"
Echo "or visit: https://go.dev/dl/"
        exit 1
    fi
    
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    GO_MAJOR=$(echo "$GO_VERSION" | cut -d. -f1)
    GO_MINOR=$(echo "$GO_VERSION" | cut -d. -f2)
    
    if [ "$GO_MAJOR" -lt 1 ] || ([ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 21 ]); then
Error "Go version is too low: $GO_VERSION (requires 1.21+)"
        exit 1
    fi
    
Success "Go environment check passed: $(go version)"
}

# Set up Python virtual environment
setup_python_env() {
    if [ ! -d "$VENV_DIR" ]; then
Info "Create a Python virtual environment..."
        python3 -m venv "$VENV_DIR"
Success "Virtual environment creation completed"
    else
Info "Python virtual environment already exists"
    fi
    
Info "Activate virtual environment..."
    # shellcheck disable=SC1091
    source "$VENV_DIR/bin/activate"
    
    if [ -f "$REQUIREMENTS_FILE" ]; then
        echo ""
        note "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
Note "⚠️ Use temporary pip mirror source (valid only for this script run)"
        note "   镜像地址: ${PIP_INDEX_URL}"
Note "For permanent configuration, please set the environment variable PIP_INDEX_URL"
        note "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        
Info "upgrade pip..."
        pip install --index-url "$PIP_INDEX_URL" --upgrade pip >/dev/null 2>&1 || true
        
Info "Install Python dependency packages..."
        echo ""
        
# Try to install dependencies, capture error output and display progress
        PIP_LOG=$(mktemp)
        (
            set +e  # Disable error exit in subshell
            pip install --index-url "$PIP_INDEX_URL" -r "$REQUIREMENTS_FILE" >"$PIP_LOG" 2>&1
            echo $? > "${PIP_LOG}.exit"
        ) &
        PIP_PID=$!
        
# Wait for a short period of time to ensure that the process starts
        sleep 0.1
        
# Show progress (if the process is still running)
        if kill -0 "$PIP_PID" 2>/dev/null; then
Show_progress "$PIP_PID" "Installing dependency packages"
        else
# The process has ended, wait a moment to ensure that the exit code file has been written
            sleep 0.2
        fi
        
# Wait for the process to complete, ignoring the exit code of wait
        wait "$PIP_PID" 2>/dev/null || true
        
        PIP_EXIT_CODE=0
        if [ -f "${PIP_LOG}.exit" ]; then
            PIP_EXIT_CODE=$(cat "${PIP_LOG}.exit" 2>/dev/null || echo "1")
            rm -f "${PIP_LOG}.exit" 2>/dev/null || true
        else
# If there is no exit code file, check the log for errors
            if [ -f "$PIP_LOG" ] && grep -q -i "error\|failed\|exception" "$PIP_LOG" 2>/dev/null; then
                PIP_EXIT_CODE=1
            fi
        fi
        
        if [ $PIP_EXIT_CODE -eq 0 ]; then
Success "Python dependency installation completed"
        else
# Check if angr installation failed (requires Rust)
            if grep -q "angr" "$PIP_LOG" && grep -q "Rust compiler\|can't find Rust" "$PIP_LOG"; then
Warning "angr installation failed (requires Rust compiler)"
                echo ""
Info "angr is an optional dependency, mainly used for binary analysis tools"
Info "If you need to use angr, please install Rust first:"
                echo "  macOS:   curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh"
                echo "  Ubuntu:  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh"
Echo "or visit: https://rustup.rs/"
                echo ""
Info "Other dependencies have been installed and can continue to be used (some tools may not be available)"
            else
Warning "The installation of some Python dependencies failed, but you can continue to try to run"
Warning "If you encounter problems, please check error messages and install missing dependencies manually"
# Display the last few lines of error messages
                echo ""
Info "Error details (last 10 lines):"
                tail -n 10 "$PIP_LOG" | sed 's/^/  /'
                echo ""
            fi
        fi
        rm -f "$PIP_LOG"
    else
Warning "requirements.txt not found, skipping Python dependency installation"
    fi
}

# Build Go project
build_go_project() {
    echo ""
    note "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
Note "⚠️ Use temporary Go Proxy (valid only for this script run)"
    note "   Proxy 地址: ${GOPROXY}"
Note "For permanent configuration, please set the environment variable GOPROXY"
    note "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    
Info "Download Go dependencies..."
    GO_DOWNLOAD_LOG=$(mktemp)
    (
        set +e  # Disable error exit in subshell
        export GOPROXY="$GOPROXY"
        go mod download >"$GO_DOWNLOAD_LOG" 2>&1
        echo $? > "${GO_DOWNLOAD_LOG}.exit"
    ) &
    GO_DOWNLOAD_PID=$!
    
# Wait for a short period of time to ensure that the process starts
    sleep 0.1
    
# Show progress (if the process is still running)
    if kill -0 "$GO_DOWNLOAD_PID" 2>/dev/null; then
Show_progress "$GO_DOWNLOAD_PID" "Downloading Go dependencies"
    else
# The process has ended, wait a moment to ensure that the exit code file has been written
        sleep 0.2
    fi
    
# Wait for the process to complete, ignoring the exit code of wait
    wait "$GO_DOWNLOAD_PID" 2>/dev/null || true
    
    GO_DOWNLOAD_EXIT_CODE=0
    if [ -f "${GO_DOWNLOAD_LOG}.exit" ]; then
        GO_DOWNLOAD_EXIT_CODE=$(cat "${GO_DOWNLOAD_LOG}.exit" 2>/dev/null || echo "1")
        rm -f "${GO_DOWNLOAD_LOG}.exit" 2>/dev/null || true
    else
# If there is no exit code file, check the log for errors
        if [ -f "$GO_DOWNLOAD_LOG" ] && grep -q -i "error\|failed" "$GO_DOWNLOAD_LOG" 2>/dev/null; then
            GO_DOWNLOAD_EXIT_CODE=1
        fi
    fi
    rm -f "$GO_DOWNLOAD_LOG" 2>/dev/null || true
    
    if [ $GO_DOWNLOAD_EXIT_CODE -ne 0 ]; then
Error "Go dependency download failed"
        exit 1
    fi
Success "Go dependency download completed"
    
Info "Build project..."
    GO_BUILD_LOG=$(mktemp)
    (
        set +e  # Disable error exit in subshell
        export GOPROXY="$GOPROXY"
        go build -o "$BINARY_NAME" cmd/server/main.go >"$GO_BUILD_LOG" 2>&1
        echo $? > "${GO_BUILD_LOG}.exit"
    ) &
    GO_BUILD_PID=$!
    
# Wait for a short period of time to ensure that the process starts
    sleep 0.1
    
# Show progress (if the process is still running)
    if kill -0 "$GO_BUILD_PID" 2>/dev/null; then
Show_progress "$GO_BUILD_PID" "Building project"
    else
# The process has ended, wait a moment to ensure that the exit code file has been written
        sleep 0.2
    fi
    
# Wait for the process to complete, ignoring the exit code of wait
    wait "$GO_BUILD_PID" 2>/dev/null || true
    
    GO_BUILD_EXIT_CODE=0
    if [ -f "${GO_BUILD_LOG}.exit" ]; then
        GO_BUILD_EXIT_CODE=$(cat "${GO_BUILD_LOG}.exit" 2>/dev/null || echo "1")
        rm -f "${GO_BUILD_LOG}.exit" 2>/dev/null || true
    else
# If there is no exit code file, check the log for errors
        if [ -f "$GO_BUILD_LOG" ] && grep -q -i "error\|failed" "$GO_BUILD_LOG" 2>/dev/null; then
            GO_BUILD_EXIT_CODE=1
        fi
    fi
    
    if [ $GO_BUILD_EXIT_CODE -eq 0 ]; then
Success "Project build completed: $BINARY_NAME"
        rm -f "$GO_BUILD_LOG"
    else
Error "Project build failed"
# Show build errors
        echo ""
Info "Build error details:"
        cat "$GO_BUILD_LOG" | sed 's/^/  /'
        echo ""
        rm -f "$GO_BUILD_LOG"
        exit 1
    fi
}

# Check if rebuilding is needed
need_rebuild() {
    if [ ! -f "$BINARY_NAME" ]; then
        return 0  # Need to build
    fi
    
# Check if the source code has been updated
    if [ "$BINARY_NAME" -ot cmd/server/main.go ] || \
       [ "$BINARY_NAME" -ot go.mod ] || \
       find internal cmd -name "*.go" -newer "$BINARY_NAME" 2>/dev/null | grep -q .; then
        return 0  # Need to rebuild
    fi
    
    return 1  # No need to build
}

# Main process
main() {
# Environment check
Info "Check the operating environment..."
    check_python
    check_go
    echo ""
    
# Set up Python environment
Info "Set up the Python environment..."
    setup_python_env
    echo ""
    
# Build Go project
    if need_rebuild; then
Info "Preparing to build the project..."
        build_go_project
    else
Success "The executable is already up to date, skip building"
    fi
    echo ""
    
# Start the server
Success "All preparations completed!"
    echo ""
Info "Start CyberStrikeAI server..."
    echo "=========================================="
    echo ""
    
# Run the server
    exec "./$BINARY_NAME"
}

# Execute the main process
main
