#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: sign-macos.sh <Atlas.app> <output.zip>" >&2
  exit 2
fi

app=$1
output=$2
keychain=""
certificate=""
notary_key=""
cleanup() {
  if [[ -n "$keychain" ]]; then
    security delete-keychain "$keychain" >/dev/null 2>&1 || true
  fi
  [[ -z "$certificate" ]] || rm -f "$certificate"
  [[ -z "$notary_key" ]] || rm -f "$notary_key"
}
trap cleanup EXIT

if [[ -n "${MACOS_CERTIFICATE_P12_BASE64:-}" ]]; then
  : "${MACOS_CERTIFICATE_PASSWORD:?required with MACOS_CERTIFICATE_P12_BASE64}"
  : "${MACOS_SIGNING_IDENTITY:?required with MACOS_CERTIFICATE_P12_BASE64}"
  certificate="$RUNNER_TEMP/atlas-release.p12"
  keychain="$RUNNER_TEMP/atlas-release.keychain-db"
  printf '%s' "$MACOS_CERTIFICATE_P12_BASE64" | base64 -D > "$certificate"
  security create-keychain -p atlas-release "$keychain"
  security unlock-keychain -p atlas-release "$keychain"
  security import "$certificate" -k "$keychain" -P "$MACOS_CERTIFICATE_PASSWORD" -T /usr/bin/codesign
  security set-key-partition-list -S apple-tool:,apple: -s -k atlas-release "$keychain"
  codesign --force --deep --options runtime --timestamp \
    --keychain "$keychain" --sign "$MACOS_SIGNING_IDENTITY" "$app"
  codesign --verify --deep --strict --verbose=2 "$app"
else
  echo "MACOS_CERTIFICATE_P12_BASE64 is empty; packaging an unsigned app"
fi

ditto -c -k --keepParent "$app" "$output"

if [[ -n "${MACOS_NOTARY_KEY_BASE64:-}" ]]; then
  [[ -n "${MACOS_CERTIFICATE_P12_BASE64:-}" ]] || {
    echo "MACOS_CERTIFICATE_P12_BASE64 is required when notarization is configured" >&2
    exit 2
  }
  : "${MACOS_NOTARY_KEY_ID:?required with MACOS_NOTARY_KEY_BASE64}"
  : "${MACOS_NOTARY_ISSUER_ID:?required with MACOS_NOTARY_KEY_BASE64}"
  notary_key="$RUNNER_TEMP/AuthKey_$MACOS_NOTARY_KEY_ID.p8"
  printf '%s' "$MACOS_NOTARY_KEY_BASE64" | base64 -D > "$notary_key"
  xcrun notarytool submit "$output" --wait \
    --key "$notary_key" --key-id "$MACOS_NOTARY_KEY_ID" --issuer "$MACOS_NOTARY_ISSUER_ID"
  xcrun stapler staple "$app"
  xcrun stapler validate "$app"
  rm -f "$output"
  ditto -c -k --keepParent "$app" "$output"
fi
