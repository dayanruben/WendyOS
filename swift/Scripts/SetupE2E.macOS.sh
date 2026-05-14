#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Prepare macOS for WendyAgent Swift E2E tests.

The setup verifies required developer tools and configures passwordless SSH
loopback for the current user.

Options:
  --help, -h  Show this help message.
EOF
}

logStep() {
  printf '==> %s\n' "$1"
}

checkCommand() {
  local command_name="$1"
  local label="${2:-$command_name}"

  printf 'Checking `%s` installed ... ' "$label"
  if command -v "$command_name" >/dev/null 2>&1; then
    printf '\033[32mYes\033[0m\n'
  else
    printf 'No\n' >&2
    echo "ERROR: Missing required tool: $label" >&2
    exit 1
  fi
}

sshLoopbackWorks() {
  ssh \
    -o BatchMode=yes \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o LogLevel=ERROR \
    -o ConnectTimeout=10 \
    localhost true >/dev/null 2>&1
}

startSSHServiceIfPossible() {
  if sshLoopbackWorks; then
    return 0
  fi

  if sudo -n /usr/sbin/systemsetup -setremotelogin on >/dev/null 2>&1; then
    sudo -n /bin/launchctl kickstart -k system/com.openssh.sshd >/dev/null 2>&1 || true
    return 0
  fi

  echo "ERROR: SSH loopback is required for Swift E2E sessions." >&2
  echo "Enable macOS Remote Login, or allow this runner to run without a sudo prompt:" >&2
  echo "  sudo systemsetup -setremotelogin on" >&2
  return 1
}

setupSSHLoopback() {
  logStep "Setting up SSH loopback for E2E sessions"

  mkdir -p "$HOME/.ssh"
  chmod 700 "$HOME/.ssh"

  if [ ! -f "$HOME/.ssh/id_ed25519" ]; then
    ssh-keygen -q -t ed25519 -N "" -C "${USER:-wendy-e2e}@$(hostname)" -f "$HOME/.ssh/id_ed25519"
  fi

  touch "$HOME/.ssh/authorized_keys"
  chmod 600 "$HOME/.ssh/authorized_keys"

  local public_key
  public_key="$(cat "$HOME/.ssh/id_ed25519.pub")"
  if ! grep -qxF "$public_key" "$HOME/.ssh/authorized_keys"; then
    printf '%s\n' "$public_key" >> "$HOME/.ssh/authorized_keys"
  fi

  startSSHServiceIfPossible

  if ! sshLoopbackWorks; then
    echo "ERROR: Could not establish passwordless SSH to localhost." >&2
    echo "Swift E2E sessions execute local commands through SSH; verify Remote Login/sshd and ~/.ssh/authorized_keys." >&2
    exit 1
  fi
}

setupE2EMacOS() {
  logStep "Setting up Swift E2E dependencies for macOS"

  checkCommand bash
  checkCommand curl
  checkCommand git
  checkCommand go
  checkCommand make
  checkCommand swift
  checkCommand zip
  checkCommand ssh "openssh-client"
  checkCommand ssh-keygen
  checkCommand xcodebuild "Xcode command line tools"

  setupSSHLoopback
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

case "$(uname -s)" in
  Darwin)
    setupE2EMacOS
    ;;
  *)
    echo "ERROR: SetupE2E.macOS.sh must run on macOS; current platform: $(uname -s)" >&2
    exit 1
    ;;
esac
