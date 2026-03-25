#!/bin/bash
# Shared test infrastructure for Wendy smoke tests.
# Source this file — do not execute directly.
#
# Exports:
#   Colors: GREEN, RED, YELLOW, BOLD, RESET
#   Counters: PASS_COUNT, FAIL_COUNT, SKIP_COUNT
#   Functions: run_test, run_test_expect_output, run_test_json, skip_test,
#              print_summary, require_tool, has_gpu_entitlement,
#              discover_device, validate_wendy_binary

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    echo "Error: test-harness.sh should be sourced, not executed directly." >&2
    exit 1
fi

# ── Colors ──────────────────────────────────────────────────────────

GREEN="\033[0;32m"
RED="\033[0;31m"
YELLOW="\033[0;33m"
BOLD="\033[1m"
RESET="\033[0m"

# ── Counters ────────────────────────────────────────────────────────

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0

# ── Test functions ──────────────────────────────────────────────────

run_test() {
    local name="$1"
    shift
    printf "  %-50s " "$name"
    local output
    output=$("$@" 2>&1)
    local rc=$?
    if [[ $rc -eq 0 ]]; then
        echo -e "${GREEN}PASS${RESET}"
        ((PASS_COUNT++))
    else
        echo -e "${RED}FAIL${RESET} (exit $rc)"
        echo "    Output: $(echo "$output" | head -5)"
        ((FAIL_COUNT++))
    fi
    return $rc
}

run_test_expect_output() {
    local name="$1"
    local pattern="$2"
    shift 2
    printf "  %-50s " "$name"
    local output
    output=$("$@" 2>&1)
    local rc=$?
    if [[ $rc -eq 0 ]] && echo "$output" | grep -qiE "$pattern"; then
        echo -e "${GREEN}PASS${RESET}"
        ((PASS_COUNT++))
    else
        echo -e "${RED}FAIL${RESET} (exit $rc)"
        echo "    Output: $(echo "$output" | head -5)"
        ((FAIL_COUNT++))
    fi
    return 0
}

run_test_json() {
    local name="$1"
    shift
    printf "  %-50s " "$name"
    local output
    output=$("$@" 2>&1)
    local rc=$?
    if [[ $rc -eq 0 ]] && echo "$output" | jq . >/dev/null 2>&1; then
        echo -e "${GREEN}PASS${RESET}"
        ((PASS_COUNT++))
    else
        echo -e "${RED}FAIL${RESET} (exit $rc)"
        echo "    Output: $(echo "$output" | head -5)"
        ((FAIL_COUNT++))
    fi
    return 0
}

skip_test() {
    local name="$1"
    printf "  %-50s " "$name"
    echo -e "${YELLOW}SKIP${RESET}"
    ((SKIP_COUNT++))
}

# ── Utilities ───────────────────────────────────────────────────────

print_summary() {
    local total=$((PASS_COUNT + FAIL_COUNT + SKIP_COUNT))
    echo ""
    echo -e "${BOLD}========================================${RESET}"
    echo -e "${BOLD}Results:${RESET} $total tests"
    echo -e "  ${GREEN}Passed:  $PASS_COUNT${RESET}"
    echo -e "  ${RED}Failed:  $FAIL_COUNT${RESET}"
    if [[ $SKIP_COUNT -gt 0 ]]; then
        echo -e "  ${YELLOW}Skipped: $SKIP_COUNT${RESET}"
    fi
    echo -e "${BOLD}========================================${RESET}"

    if [[ $FAIL_COUNT -gt 0 ]]; then
        return 1
    fi
    return 0
}

require_tool() {
    local name="$1"
    if ! command -v "$name" &>/dev/null; then
        return 1
    fi
    return 0
}

has_gpu_entitlement() {
    local wendy_json="$1"
    jq -e '.entitlements[]? | select(.type == "gpu")' "$wendy_json" >/dev/null 2>&1
}

discover_device() {
    local wendy_bin="$1"
    echo -e "${BOLD}==> Auto-discovering device...${RESET}"
    local discover_json
    discover_json=$("$wendy_bin" discover --json --timeout 5s 2>&1)
    HOSTNAME=$(echo "$discover_json" | jq -r '.lanDevices[0].hostname // empty' 2>/dev/null)
    if [[ -z "$HOSTNAME" ]]; then
        echo -e "${RED}ERROR: No LAN device found via 'wendy discover --json --timeout 5s'${RESET}"
        echo "    Output: $(echo "$discover_json" | head -5)"
        echo ""
        echo "Hint: pass -h <hostname> to skip auto-discovery."
        return 1
    fi
    return 0
}

validate_wendy_binary() {
    if [[ "$WENDY" != "wendy" ]]; then
        # Explicit path — check it exists and is executable
        if [[ ! -x "$WENDY" ]]; then
            echo -e "${RED}ERROR: wendy binary not found or not executable at $WENDY${RESET}"
            return 1
        fi
    else
        # Default — check wendy is on PATH
        if ! command -v wendy &>/dev/null; then
            echo -e "${RED}ERROR: 'wendy' not found on PATH${RESET}"
            echo "Hint: pass -w /path/to/wendy to specify the binary location."
            return 1
        fi
        WENDY="$(command -v wendy)"
    fi
    return 0
}
