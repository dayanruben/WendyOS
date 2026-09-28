#!/usr/bin/env bash
# .github/scripts/install-scripts_test.sh
# Tests the shared resolver block (Task 1) and cli.sh deferral (Task 2).
set -euo pipefail
cd "$(dirname "$0")"

REPO_ROOT="$(cd ../.. && pwd)"
CLI="${REPO_ROOT}/go/internal/cli/assets/docs/cli.sh"
AGENT="${REPO_ROOT}/go/internal/cli/assets/docs/agent.sh"
BEGIN='# >>> wendy-install-shared'
END='# <<< wendy-install-shared'

fail=0
check() { if [ "$2" != "$3" ]; then echo "FAIL $1: expected [$2] got [$3]"; fail=1; else echo "ok $1"; fi; }
contains() { case "$2" in *"$3"*) echo "ok $1";; *) echo "FAIL $1: [$2] does not contain [$3]"; fail=1;; esac; }
absent()  { case "$2" in *"$3"*) echo "FAIL $1: [$2] unexpectedly contains [$3]"; fail=1;; *) echo "ok $1";; esac; }

# Extract the marked block from a script (exclusive of the marker lines).
extract_block() { awk "/${BEGIN}/{f=1;next} /${END}/{f=0} f" "$1"; }

# --- Test A: both scripts carry a byte-identical shared block ---
cli_block="$(extract_block "$CLI")"
agent_block="$(extract_block "$AGENT")"
check "block.nonempty" "yes" "$([ -n "$cli_block" ] && echo yes || echo no)"
check "block.identical" "yes" "$([ "$cli_block" = "$agent_block" ] && echo yes || echo no)"

# --- Harness: fake curl/wget servable from a table of url->file, logging calls ---
setup_net() { # $1 = dir with manifest.json / github.json (optional)
  BIN="$(mktemp -d)"; REQ_LOG="$(mktemp)"; SERVE_DIR="$1"
  cat > "$BIN/curl" <<EOF
#!/usr/bin/env bash
url=""; out=""
while [ \$# -gt 0 ]; do
  case "\$1" in
    -o) out="\$2"; shift 2;;
    http*|https*) url="\$1"; shift;;
    *) shift;;
  esac
done
echo "\$url" >> "$REQ_LOG"
case "\$url" in
  *install.wendy.dev/manifest.json) src="$SERVE_DIR/manifest.json";;
  *api.github.com/*) src="$SERVE_DIR/github.json";;
  */releases/download/*) src="$SERVE_DIR/\${url##*/}";;
  *) src="";;
esac
[ -n "\$src" ] && [ -f "\$src" ] || exit 22   # mimic curl -f on missing/non-2xx
if [ -n "\$out" ]; then cat "\$src" > "\$out"; else cat "\$src"; fi
EOF
  cp "$BIN/curl" "$BIN/wget" 2>/dev/null || true  # not used, but present
  chmod +x "$BIN/curl" "$BIN/wget"
}

# Build a script that sources ONLY the shared block, then calls resolve_version.
run_resolver() { # env: WENDY_VERSION optional
  local tmp; tmp="$(mktemp)"
  # Mirror the real scripts' shell options so the test catches errexit bugs
  # (a failing command substitution under `set -e` must NOT abort the fallback).
  { echo 'set -euo pipefail'; echo 'REPO="wendylabsinc/wendy-agent"'; extract_block "$CLI"; echo 'resolve_version'; } > "$tmp"
  PATH="$BIN:$PATH" bash "$tmp"
}

# --- Test B: WENDY_VERSION override wins ---
D="$(mktemp -d)"; setup_net "$D"
printf '{"latest":"2026.01.01-000000"}\n' > "$D/manifest.json"
export WENDY_VERSION=9.9.9
out="$(run_resolver)"
unset WENDY_VERSION
check "resolve.override" "9.9.9" "$out"
absent "resolve.override.no_net" "$(cat "$REQ_LOG")" "manifest.json"

# --- Test C: GCS manifest latest is preferred ---
D="$(mktemp -d)"; setup_net "$D"
printf '{"latest":"2026.07.19-143000","latest_nightly":"2026.07.20-010101"}\n' > "$D/manifest.json"
printf '{"tag_name":"2000.00.00-000000"}\n' > "$D/github.json"
out="$(run_resolver)"
check "resolve.gcs" "2026.07.19-143000" "$out"
contains "resolve.gcs.hit_manifest" "$(cat "$REQ_LOG")" "install.wendy.dev/manifest.json"

# --- Test D: falls back to GitHub when manifest is missing ---
D="$(mktemp -d)"; setup_net "$D"    # no manifest.json in dir
printf '{"tag_name":"2026.07.18-120000"}\n' > "$D/github.json"
out="$(run_resolver)"
check "resolve.fallback" "2026.07.18-120000" "$out"
contains "resolve.fallback.hit_github" "$(cat "$REQ_LOG")" "api.github.com"

# --- Test E: cli.sh Homebrew path makes zero GitHub/manifest calls (deferral) ---
D="$(mktemp -d)"; setup_net "$D"           # curl fails on every URL and logs it
printf '{"latest":"2026.07.19-143000"}\n' > "$D/manifest.json"
STUB="$(mktemp -d)"
# uname stub: pretend Apple Silicon macOS so the darwin/brew branch is taken.
cat > "$STUB/uname" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  -s) echo "Darwin";;
  -m) echo "arm64";;
  *) echo "Darwin";;
esac
EOF
# brew stub: present, but "brew help trust" fails so the trust steps are skipped;
# every other subcommand is a successful no-op.
cat > "$STUB/brew" <<'EOF'
#!/usr/bin/env bash
[ "$1" = "help" ] && exit 1
exit 0
EOF
chmod +x "$STUB/uname" "$STUB/brew"
: > "$REQ_LOG"
PATH="$STUB:$BIN:$PATH" bash "$CLI" -y >/dev/null 2>&1 || true
absent "defer.no_github"   "$(cat "$REQ_LOG")" "api.github.com"
absent "defer.no_manifest" "$(cat "$REQ_LOG")" "install.wendy.dev/manifest.json"

# ===== No-TTY installs (agent shells, CI, `curl | bash` with no terminal) =====

# no_tty runs a command with no controlling terminal. setsid detaches it from
# the session's tty, so opening /dev/tty fails with ENXIO exactly as it does in
# an agent shell — even when this test itself is run from a terminal.
no_tty() {
  perl -MPOSIX -e 'my $pid = fork() // die "fork: $!"; if ($pid) { waitpid($pid, 0); exit($? >> 8) } POSIX::setsid() or die "setsid: $!"; exec @ARGV or die "exec: $!"' "$@"
}

# Only system dirs on PATH, so a developer's Homebrew/package managers can't leak in.
BASE_PATH="/usr/bin:/bin:/usr/sbin:/sbin"

# make_stubs OS ARCH: fresh STUB dir with uname (reporting OS/ARCH), id (a
# non-root user) and sudo (logs its args to SUDO_LOG, then fails the way
# `sudo -n` does with no cached credentials).
make_stubs() {
  STUB="$(mktemp -d)"; SUDO_LOG="$(mktemp)"
  cat > "$STUB/uname" <<EOF
#!/usr/bin/env bash
case "\$1" in -s) echo "$1";; -m) echo "$2";; *) echo "$1";; esac
EOF
  cat > "$STUB/id" <<'EOF'
#!/usr/bin/env bash
if [ "$1" = "-u" ]; then echo 1000; else exec /usr/bin/id "$@"; fi
EOF
  cat > "$STUB/sudo" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "$SUDO_LOG"
echo "sudo: a password is required" >&2
exit 1
EOF
  chmod +x "$STUB/uname" "$STUB/id" "$STUB/sudo"
}

# serve_cli_release DIR OS ARCH: manifest.json plus the matching release
# tarball (holding a stub `wendy`) for the fake curl to serve.
serve_cli_release() {
  local dir="$1" os="$2" arch="$3" v="2026.07.19-143000" pkg
  printf '{"latest":"%s"}\n' "$v" > "$dir/manifest.json"
  pkg="$(mktemp -d)"
  mkdir -p "$pkg/wendy-cli-${os}-${arch}"
  printf '#!/bin/sh\necho "wendy version %s"\n' "$v" > "$pkg/wendy-cli-${os}-${arch}/wendy"
  chmod +x "$pkg/wendy-cli-${os}-${arch}/wendy"
  tar -czf "$dir/wendy-cli-${os}-${arch}-${v}.tar.gz" -C "$pkg" "wendy-cli-${os}-${arch}"
}

# run_no_tty OUT SCRIPT ARGS...: SCRIPT with no terminal, stdin from
# /dev/null, the stub PATH and a throwaway HOME. Returns the script's exit code.
run_no_tty() {
  local out="$1" script="$2"; shift 2
  no_tty env PATH="$STUB:$BIN:$BASE_PATH" HOME="$FAKE_HOME" bash "$script" "$@" </dev/null >"$out" 2>&1
}

# cli_with_default_dir DIR prints the path of a copy of cli.sh whose built-in
# default install dir is DIR instead of /usr/local/bin. The ~/.local/bin
# fallback only applies to the default dir (an explicit -d never relocates), so
# this is how the tests reach it without going near the real /usr/local/bin.
# Aborts the suite if the rewrite didn't apply, rather than let a test run
# against the real default.
cli_with_default_dir() {
  local copy; copy="$(mktemp)"
  sed "s|^INSTALL_DIR=\"/usr/local/bin\"\$|INSTALL_DIR=\"$1\"|" "$CLI" > "$copy"
  if ! grep -qxF "INSTALL_DIR=\"$1\"" "$copy"; then
    echo "FAIL harness: could not rewrite the default INSTALL_DIR in cli.sh"
    exit 1
  fi
  echo "$copy"
}

# --- Test F: no TTY, Homebrew path, no -y: proceeds instead of dying on /dev/tty ---
make_stubs Darwin arm64
BREW_LOG="$(mktemp)"
cat > "$STUB/brew" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "$BREW_LOG"
[ "\$1" = "help" ] && exit 1
exit 0
EOF
chmod +x "$STUB/brew"
D="$(mktemp -d)"; setup_net "$D"; FAKE_HOME="$(mktemp -d)"; OUT="$(mktemp)"
rc=0; run_no_tty "$OUT" "$CLI" || rc=$?
check "notty.brew.exit" "0" "$rc"
contains "notty.brew.autoyes" "$(cat "$OUT")" "continuing as if -y was passed"
contains "notty.brew.installed" "$(cat "$BREW_LOG")" "install wendylabsinc/tap/wendy"
absent "notty.brew.no_tty_error" "$(cat "$OUT")" "/dev/tty"
absent "notty.brew.no_tour_hint" "$(cat "$OUT")" "tour"

# Root ignores directory permissions, so the read-only-dir cases need a normal user.
if [ "$(/usr/bin/id -u)" -ne 0 ]; then
  # --- Test G: no TTY, default install dir unwritable, sudo needs a password: ~/.local/bin ---
  make_stubs Darwin arm64
  D="$(mktemp -d)"; setup_net "$D"; serve_cli_release "$D" darwin arm64
  FAKE_HOME="$(mktemp -d)"; OUT="$(mktemp)"; RO="$(mktemp -d)"; chmod 555 "$RO"
  SCRIPT="$(cli_with_default_dir "$RO")"
  rc=0; run_no_tty "$OUT" "$SCRIPT" || rc=$?
  chmod 755 "$RO"
  check "notty.fallback.exit" "0" "$rc"
  check "notty.fallback.binary" "yes" "$([ -x "$FAKE_HOME/.local/bin/wendy" ] && echo yes || echo no)"
  check "notty.fallback.ro_untouched" "no" "$([ -e "$RO/wendy" ] && echo yes || echo no)"
  contains "notty.fallback.says_where" "$(cat "$OUT")" "installing to $FAKE_HOME/.local/bin instead"
  check "notty.fallback.one_path_line" "1" "$(grep -c 'export PATH=' "$OUT")"
  check "notty.fallback.sudo_only_probed" "-n true" "$(sort -u "$SUDO_LOG")"

  # --- Test N: an explicit -d that can't be written fails with one line; never relocates ---
  make_stubs Darwin arm64
  D="$(mktemp -d)"; setup_net "$D"; serve_cli_release "$D" darwin arm64
  FAKE_HOME="$(mktemp -d)"; OUT="$(mktemp)"; RO="$(mktemp -d)"; chmod 555 "$RO"
  rc=0; run_no_tty "$OUT" "$CLI" -d "$RO" || rc=$?
  chmod 755 "$RO"
  check "notty.explicit_dir.exit" "1" "$rc"
  contains "notty.explicit_dir.says_why" "$(cat "$OUT")" "Error: $RO is not writable"
  contains "notty.explicit_dir.says_what_to_do" "$(cat "$OUT")" "Re-run with -d <writable dir>."
  check "notty.explicit_dir.one_error_line" "1" "$(grep -c 'Error:' "$OUT")"
  check "notty.explicit_dir.no_fallback" "no" "$([ -e "$FAKE_HOME/.local/bin/wendy" ] && echo yes || echo no)"
  check "notty.explicit_dir.ro_untouched" "no" "$([ -e "$RO/wendy" ] && echo yes || echo no)"
  check "notty.explicit_dir.sudo_only_probed" "-n true" "$(sort -u "$SUDO_LOG")"

  # --- Test J: Linux, apt present, no TTY, no passwordless sudo: standalone binary ---
  make_stubs Linux x86_64
  APT_LOG="$(mktemp)"
  printf '#!/usr/bin/env bash\necho "$*" >> "%s"\n' "$APT_LOG" > "$STUB/apt-get"; chmod +x "$STUB/apt-get"
  D="$(mktemp -d)"; setup_net "$D"; serve_cli_release "$D" linux amd64
  FAKE_HOME="$(mktemp -d)"; OUT="$(mktemp)"; RO="$(mktemp -d)"; chmod 555 "$RO"
  SCRIPT="$(cli_with_default_dir "$RO")"
  rc=0; run_no_tty "$OUT" "$SCRIPT" || rc=$?
  chmod 755 "$RO"
  check "notty.linux.exit" "0" "$rc"
  check "notty.linux.no_apt" "" "$(cat "$APT_LOG")"
  contains "notty.linux.says_standalone" "$(cat "$OUT")" "installing the standalone binary"
  check "notty.linux.binary" "yes" "$([ -x "$FAKE_HOME/.local/bin/wendy" ] && echo yes || echo no)"
fi

# --- Test H: no TTY, writable install dir: installs there without touching sudo ---
make_stubs Darwin arm64
D="$(mktemp -d)"; setup_net "$D"; serve_cli_release "$D" darwin arm64
FAKE_HOME="$(mktemp -d)"; OUT="$(mktemp)"; DEST="$(mktemp -d)"
rc=0; run_no_tty "$OUT" "$CLI" -d "$DEST" || rc=$?
check "notty.writable.exit" "0" "$rc"
check "notty.writable.binary" "yes" "$([ -x "$DEST/wendy" ] && echo yes || echo no)"
check "notty.writable.no_sudo" "" "$(cat "$SUDO_LOG")"

# --- Test K: both installers' confirm() proceeds without a terminal ---
for script in "$CLI" "$AGENT"; do
  tmp="$(mktemp)"
  { echo 'set -euo pipefail'; echo 'YES=false'; extract_block "$script"
    awk '/^confirm\(\) \{/{f=1} f{print} f&&/^\}/{exit}' "$script"
    echo 'confirm "Proceed?"; echo "rc=$?"'; } > "$tmp"
  out="$(no_tty bash "$tmp" </dev/null 2>&1 || true)"
  name="$(basename "$script")"
  contains "notty.confirm.${name}.proceeds" "$out" "rc=0"
  contains "notty.confirm.${name}.says_so" "$out" "continuing as if -y was passed"
done

# with_pty runs a command attached to a fresh pseudo-terminal (so /dev/tty
# works), feeding this function's stdin to it as keystrokes. BSD/macOS and
# util-linux `script` take different arguments.
with_pty() {
  if script --version >/dev/null 2>&1; then
    script -qec "$(printf '%q ' "$@")" /dev/null
  else
    script -q /dev/null "$@"
  fi
}

# --- Test L: with a terminal, the prompt is still shown and "n" aborts ---
make_stubs Darwin arm64
BREW_LOG="$(mktemp)"
cat > "$STUB/brew" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "$BREW_LOG"
[ "\$1" = "help" ] && exit 1
exit 0
EOF
chmod +x "$STUB/brew"
D="$(mktemp -d)"; setup_net "$D"; FAKE_HOME="$(mktemp -d)"
# Keep stdin open past the answer: BSD script ends the session at stdin EOF.
out="$({ printf 'n\n'; sleep 3; } | with_pty env PATH="$STUB:$BIN:$BASE_PATH" HOME="$FAKE_HOME" bash "$CLI" 2>&1 || true)"
contains "tty.prompted" "$out" "Proceed? [y/N]"
contains "tty.aborted" "$out" "Aborted."
absent "tty.no_install" "$(cat "$BREW_LOG")" "install"

# --- Test M: no TTY, no sudo, default dir unwritable, HOME unset: one clear error, not "unbound variable" ---
if [ "$(/usr/bin/id -u)" -ne 0 ]; then
  make_stubs Darwin arm64
  D="$(mktemp -d)"; setup_net "$D"; serve_cli_release "$D" darwin arm64
  OUT="$(mktemp)"; RO="$(mktemp -d)"; chmod 555 "$RO"
  SCRIPT="$(cli_with_default_dir "$RO")"
  rc=0; no_tty env -u HOME PATH="$STUB:$BIN:$BASE_PATH" bash "$SCRIPT" </dev/null >"$OUT" 2>&1 || rc=$?
  chmod 755 "$RO"
  check "notty.nohome.exit" "1" "$rc"
  contains "notty.nohome.says_why" "$(cat "$OUT")" "HOME is unset. Re-run with -d <writable dir>."
  absent "notty.nohome.no_unbound" "$(cat "$OUT")" "unbound variable"
fi

exit $fail
