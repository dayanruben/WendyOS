#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SWIFT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
VALIDATOR="$SCRIPT_DIR/ValidateMacCoreOnlyRelease.sh"
CORE_ENTITLEMENTS="$SWIFT_DIR/WendyAgentMac/Support/WendyAgentMacCore.entitlements"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

APP_PATH="$TEMP_DIR/WendyAgentMac.app"
mkdir -p "$APP_PATH/Contents/MacOS"
cat > "$APP_PATH/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key>
  <string>WendyAgentMac</string>
  <key>CFBundleIdentifier</key>
  <string>sh.wendy.core-only-validation-test</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
</dict>
</plist>
PLIST
cat > "$TEMP_DIR/main.c" <<'C'
int main(void) { return 0; }
C
clang "$TEMP_DIR/main.c" -o "$APP_PATH/Contents/MacOS/WendyAgentMac"

sign_app() {
  codesign --force --sign - --entitlements "$1" "$APP_PATH" >/dev/null
}

expect_failure() {
  local expected_message="$1"
  if "$VALIDATOR" "$APP_PATH" > "$TEMP_DIR/stdout" 2> "$TEMP_DIR/stderr"; then
    echo "Validator unexpectedly accepted: $expected_message" >&2
    exit 1
  fi
  grep -F "$expected_message" "$TEMP_DIR/stderr" >/dev/null
}

sign_app "$CORE_ENTITLEMENTS"
"$VALIDATOR" "$APP_PATH"

for entitlement in \
  com.apple.developer.networking.networkextension \
  com.apple.developer.system-extension.install
do
  cat > "$TEMP_DIR/restricted.entitlements" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>$entitlement</key>
  <true/>
</dict>
</plist>
PLIST
  sign_app "$TEMP_DIR/restricted.entitlements"
  expect_failure "restricted entitlement: $entitlement"
done

sign_app "$CORE_ENTITLEMENTS"
mkdir -p \
  "$APP_PATH/Contents/Library/SystemExtensions/sh.wendy.WendyAgentMac.WendyNet.systemextension"
expect_failure "contains WendyNet"

echo "Core-only macOS release validator tests passed"
