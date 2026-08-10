#!/usr/bin/env bash
set -euo pipefail

ADB_BIN="${ADB_BIN:-adb}"
SOAK_ITERATIONS="${SOAK_ITERATIONS:-20}"
REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"

device_count="$($ADB_BIN devices | awk 'NR > 1 && $2 == "device" { count++ } END { print count + 0 }')"
if [[ "$device_count" -ne 1 ]]; then
  echo "Expected exactly one authorized Android device, found $device_count." >&2
  exit 2
fi

abi="$($ADB_BIN shell getprop ro.product.cpu.abi | tr -d '\r')"
if [[ "$abi" != "arm64-v8a" ]]; then
  echo "ARM64 soak requires arm64-v8a, device reports '$abi'." >&2
  exit 3
fi

cd "$REPO_DIR/android"
./gradlew \
  :jankhunter-runtime:connectedDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.class=io.jankhunter.runtime.RuntimeGraphArtTest \
  -Pandroid.testInstrumentationRunnerArguments.jankhunterSoakIterations="$SOAK_ITERATIONS"
