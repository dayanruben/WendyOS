#!/bin/bash
set -uo pipefail

# Smoke test for ESP32 (WendyLite) — WASM build + optional deploy.
# Clones WendyLite, builds WASM apps with available toolchains, cleans up.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/test-harness.sh"

# ── Usage ───────────────────────────────────────────────────────────

usage() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Smoke test for ESP32 (WendyLite). Builds WASM apps with available toolchains
and optionally deploys them to a device over WiFi.

Options:
  --device-ip IP              ESP32 device IP (enables deployment after build)
  --udp-port PORT             UDP reload port (default: 4210)
  --lite-dir PATH             Use local WendyLite dir instead of cloning
  --lite-branch BRANCH        Git branch (default: main)
  --help                      Show this help message

Examples:
  $(basename "$0")                                   # build-only
  $(basename "$0") --device-ip 192.168.1.42          # build + deploy
  $(basename "$0") --lite-dir ../WendyLite           # local WendyLite
EOF
    exit 0
}

# ── Parse arguments ─────────────────────────────────────────────────

DEVICE_IP=""
UDP_PORT=4210
LITE_DIR=""
LITE_BRANCH="main"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --device-ip)    DEVICE_IP="$2"; shift 2 ;;
        --udp-port)     UDP_PORT="$2"; shift 2 ;;
        --lite-dir)     LITE_DIR="$2"; shift 2 ;;
        --lite-branch)  LITE_BRANCH="$2"; shift 2 ;;
        --help)         usage ;;
        *)              echo "Unknown option: $1"; usage ;;
    esac
done

# ── Phase 1: Setup ──────────────────────────────────────────────────

echo -e "${BOLD}==> Phase 1: Setup${RESET}"
echo ""

# ── Phase 2: Clone WendyLite ────────────────────────────────────────

echo -e "${BOLD}==> Phase 2: Acquire WendyLite${RESET}"

CLONE_DIR=""
if [[ -n "$LITE_DIR" ]]; then
    if [[ ! -d "$LITE_DIR" ]]; then
        echo -e "${RED}ERROR: WendyLite directory not found: $LITE_DIR${RESET}"
        exit 1
    fi
    LITE_DIR="$(cd "$LITE_DIR" && pwd)"
    echo "Using local WendyLite: $LITE_DIR"
else
    CLONE_DIR=$(mktemp -d)
    trap 'rm -rf "$CLONE_DIR"' EXIT
    echo "Cloning WendyLite ($LITE_BRANCH) into $CLONE_DIR..."
    git clone --depth 1 --branch "$LITE_BRANCH" \
        https://github.com/wendylabsinc/wendy-lite.git "$CLONE_DIR" 2>&1 | tail -1
    LITE_DIR="$CLONE_DIR"
fi

WASM_DIR="$LITE_DIR/wasm_apps"
TOOLS_DIR="$LITE_DIR/tools"
echo ""

# ── Phase 3: Toolchain detection ───────────────────────────────────

echo -e "${BOLD}==> Phase 3: Toolchain detection${RESET}"

HAS_CLANG=false
HAS_WAT2WASM=false
HAS_RUSTUP=false
HAS_SWIFT=false
HAS_NPM=false

if require_tool clang; then
    # Check wasm32 target support
    if clang --target=wasm32 -x c -c /dev/null -o /dev/null 2>/dev/null; then
        HAS_CLANG=true
        echo -e "  clang (wasm32):        ${GREEN}available${RESET}"
    else
        echo -e "  clang (wasm32):        ${YELLOW}no wasm32 target${RESET}"
    fi
else
    echo -e "  clang (wasm32):        ${YELLOW}not found${RESET}"
fi

if require_tool wat2wasm; then
    HAS_WAT2WASM=true
    echo -e "  wat2wasm (WABT):       ${GREEN}available${RESET}"
else
    echo -e "  wat2wasm (WABT):       ${YELLOW}not found${RESET}"
fi

if require_tool rustup; then
    if rustup target list --installed 2>/dev/null | grep -q wasm32-unknown-unknown; then
        HAS_RUSTUP=true
        echo -e "  rustup (wasm32):       ${GREEN}available${RESET}"
    else
        echo -e "  rustup (wasm32):       ${YELLOW}wasm32-unknown-unknown target not installed${RESET}"
    fi
else
    echo -e "  rustup (wasm32):       ${YELLOW}not found${RESET}"
fi

if require_tool swiftly; then
    HAS_SWIFT=true
    echo -e "  swiftly (Swift 6.2+):  ${GREEN}available${RESET}"
else
    echo -e "  swiftly (Swift 6.2+):  ${YELLOW}not found${RESET}"
fi

if require_tool npm; then
    HAS_NPM=true
    echo -e "  npm (AssemblyScript):  ${GREEN}available${RESET}"
else
    echo -e "  npm (AssemblyScript):  ${YELLOW}not found${RESET}"
fi

echo ""

# ── Phase 4: Build tests ───────────────────────────────────────────

echo -e "${BOLD}==> Phase 4: Build tests${RESET}"

# Track successfully built WASM files for deploy phase (parallel arrays for bash 3 compat)
BUILT_NAMES=()
BUILT_PATHS=()

built_wasm_add() {
    BUILT_NAMES+=("$1")
    BUILT_PATHS+=("$2")
}

built_wasm_get() {
    local name="$1"
    local i
    for (( i=0; i<${#BUILT_NAMES[@]}; i++ )); do
        if [[ "${BUILT_NAMES[$i]}" == "$name" ]]; then
            echo "${BUILT_PATHS[$i]}"
            return 0
        fi
    done
    return 1
}

# --- C apps (clang) ---

if [[ "$HAS_CLANG" == true ]]; then
    run_test "build: blink (C)" \
        make -C "$WASM_DIR" blink
    if [[ -s "$WASM_DIR/blink.wasm" ]]; then
        built_wasm_add blink "$WASM_DIR/blink.wasm"
    fi

    run_test "build: i2c_sensor (C)" \
        make -C "$WASM_DIR" i2c_sensor
    if [[ -s "$WASM_DIR/i2c_sensor.wasm" ]]; then
        built_wasm_add i2c_sensor "$WASM_DIR/i2c_sensor.wasm"
    fi
else
    skip_test "build: blink (C) — no clang"
    skip_test "build: i2c_sensor (C) — no clang"
fi

# --- WAT app (wat2wasm) ---

if [[ "$HAS_WAT2WASM" == true ]]; then
    run_test "build: wat_blink (WAT)" \
        make -C "$WASM_DIR" wat_blink
    if [[ -s "$WASM_DIR/wat_blink.wasm" ]]; then
        built_wasm_add wat_blink "$WASM_DIR/wat_blink.wasm"
    fi
else
    skip_test "build: wat_blink (WAT) — no wat2wasm"
fi

# --- Rust app (rustup) ---

if [[ "$HAS_RUSTUP" == true ]]; then
    run_test "build: rust_blink (Rust)" \
        make -C "$WASM_DIR" rust_blink
    if [[ -s "$WASM_DIR/rust_blink.wasm" ]]; then
        built_wasm_add rust_blink "$WASM_DIR/rust_blink.wasm"
    fi
else
    skip_test "build: rust_blink (Rust) — no rustup"
fi

# --- Swift apps (swiftly) ---

if [[ "$HAS_SWIFT" == true ]]; then
    run_test "build: swift_blink (Swift)" \
        bash -c "cd '$WASM_DIR/swift_blink' && swiftly run +6.2.3 swift build --triple wasm32-unknown-none-wasm -c release"
    # Locate the built .wasm — product name from Package.swift
    SWIFT_BLINK_WASM=$(find "$WASM_DIR/swift_blink/.build" -name "*.wasm" 2>/dev/null | head -1)
    if [[ -n "$SWIFT_BLINK_WASM" ]] && [[ -s "$SWIFT_BLINK_WASM" ]]; then
        built_wasm_add swift_blink "$SWIFT_BLINK_WASM"
    fi

    run_test "build: swift_display (Swift)" \
        bash -c "cd '$WASM_DIR/swift_display' && swiftly run +6.2.3 swift build --triple wasm32-unknown-none-wasm -c release"
    SWIFT_DISPLAY_WASM=$(find "$WASM_DIR/swift_display/.build" -name "*.wasm" 2>/dev/null | head -1)
    if [[ -n "$SWIFT_DISPLAY_WASM" ]] && [[ -s "$SWIFT_DISPLAY_WASM" ]]; then
        built_wasm_add swift_display "$SWIFT_DISPLAY_WASM"
    fi
else
    skip_test "build: swift_blink (Swift) — no swiftly"
    skip_test "build: swift_display (Swift) — no swiftly"
fi

# --- AssemblyScript app (npm) ---

if [[ "$HAS_NPM" == true ]]; then
    run_test "build: ts_blink (AssemblyScript)" \
        bash -c "cd '$WASM_DIR/ts_blink' && npm install && npm run build"
    # AssemblyScript typically outputs to build/
    TS_BLINK_WASM=$(find "$WASM_DIR/ts_blink/build" -name "*.wasm" 2>/dev/null | head -1)
    if [[ -n "$TS_BLINK_WASM" ]] && [[ -s "$TS_BLINK_WASM" ]]; then
        built_wasm_add ts_blink "$TS_BLINK_WASM"
    fi
else
    skip_test "build: ts_blink (AssemblyScript) — no npm"
fi

# Verify .wasm output exists for each successful build
echo ""
echo -e "${BOLD}--- Build artifact verification ---${RESET}"
for (( idx=0; idx<${#BUILT_NAMES[@]}; idx++ )); do
    app="${BUILT_NAMES[$idx]}"
    wasm_path="${BUILT_PATHS[$idx]}"
    artifact_size=$(wc -c < "$wasm_path" | tr -d ' ')
    run_test "artifact: $app.wasm (${artifact_size} bytes)" \
        test -s "$wasm_path"
done

echo ""

# ── Phase 5: Deploy tests (optional) ───────────────────────────────

echo -e "${BOLD}==> Phase 5: Deploy tests${RESET}"

if [[ -z "$DEVICE_IP" ]]; then
    echo "Skipping deploy tests (no --device-ip provided)"
    echo ""
else
    echo "Deploying to $DEVICE_IP (UDP port $UDP_PORT)"
    echo ""

    SERVE_SCRIPT="$TOOLS_DIR/wendy_serve.py"
    if [[ ! -f "$SERVE_SCRIPT" ]]; then
        echo -e "${RED}ERROR: wendy_serve.py not found at $SERVE_SCRIPT${RESET}"
        echo "Skipping all deploy tests."
    else
        for (( idx=0; idx<${#BUILT_NAMES[@]}; idx++ )); do
            app="${BUILT_NAMES[$idx]}"
            wasm_file="${BUILT_PATHS[$idx]}"

            # Skip i2c_sensor — requires physical I2C hardware
            if [[ "$app" == "i2c_sensor" ]]; then
                skip_test "deploy: $app (requires I2C hardware)"
                continue
            fi

            printf "  %-50s " "deploy: $app"

            # Start wendy_serve.py in background
            SERVE_LOG=$(mktemp)
            python3 "$SERVE_SCRIPT" "$wasm_file" \
                --device "$DEVICE_IP" \
                --udp-port "$UDP_PORT" \
                --reload \
                >"$SERVE_LOG" 2>&1 &
            SERVE_PID=$!

            # Monitor output for HTTP GET (device pulled the binary)
            GOT_REQUEST=false
            for i in $(seq 1 15); do
                sleep 1
                if grep -qi "served\|GET\|200" "$SERVE_LOG" 2>/dev/null; then
                    GOT_REQUEST=true
                    break
                fi
                # Check if serve process is still alive
                if ! kill -0 "$SERVE_PID" 2>/dev/null; then
                    break
                fi
            done

            # Kill serve process
            kill "$SERVE_PID" 2>/dev/null
            wait "$SERVE_PID" 2>/dev/null

            if [[ "$GOT_REQUEST" == true ]]; then
                echo -e "${GREEN}PASS${RESET}"
                ((PASS_COUNT++))
            else
                echo -e "${RED}FAIL${RESET} (no device request within 15s)"
                echo "    Log: $(head -5 "$SERVE_LOG")"
                ((FAIL_COUNT++))
            fi

            rm -f "$SERVE_LOG"
        done
    fi
    echo ""
fi

# ── Phase 6: Summary ───────────────────────────────────────────────

print_summary
exit $?
