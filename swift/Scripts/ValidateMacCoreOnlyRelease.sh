#!/bin/bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <WendyAgentMac.app>" >&2
  exit 64
fi

APP_PATH="$1"

if [[ ! -d "$APP_PATH" ]]; then
  echo "Missing macOS app bundle: $APP_PATH" >&2
  exit 1
fi

WENDY_NET_PATH=$(find "$APP_PATH" -name '*WendyNet*' -print -quit)
if [[ -n "$WENDY_NET_PATH" ]]; then
  echo "Core-only macOS release contains WendyNet: $WENDY_NET_PATH" >&2
  exit 1
fi

ENTITLEMENTS_PATH=$(mktemp)
trap 'rm -f "$ENTITLEMENTS_PATH"' EXIT

assert_no_restricted_entitlements() {
  local code_path="$1"
  local entitlement

  codesign --display --entitlements :- "$code_path" > "$ENTITLEMENTS_PATH" 2>/dev/null

  for entitlement in \
    com.apple.developer.networking.networkextension \
    com.apple.developer.system-extension.install
  do
    if /usr/libexec/PlistBuddy -c "Print :$entitlement" "$ENTITLEMENTS_PATH" \
      >/dev/null 2>&1
    then
      echo "Core-only macOS release contains restricted entitlement: $entitlement ($code_path)" >&2
      exit 1
    fi
  done
}

codesign --verify --deep --strict --verbose=2 "$APP_PATH"
assert_no_restricted_entitlements "$APP_PATH"
while IFS= read -r -d '' nested_code; do
  assert_no_restricted_entitlements "$nested_code"
done < <(find "$APP_PATH/Contents" -type d \
  \( -name '*.app' -o -name '*.framework' -o -name '*.xpc' -o -name '*.appex' \
    -o -name '*.systemextension' \) -print0)

echo "Validated core-only macOS release: $APP_PATH"
