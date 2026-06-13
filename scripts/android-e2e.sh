#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_ID="${APP_ID:-io.jankhunter.sample}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/android-e2e}"
LOG_DIR="$OUT_DIR/logs"

rm -rf "$OUT_DIR"
mkdir -p "$LOG_DIR"

cd "$ROOT_DIR/android"
./gradlew :sample-app:connectedDebugAndroidTest --no-daemon

adb exec-out run-as "$APP_ID" tar -C files/jankhunter-e2e -cf - . | tar -xf - -C "$LOG_DIR"

cd "$ROOT_DIR/cli"
go run ./cmd/jankhunter inspect "$LOG_DIR"/*.jhlog --json --out "$OUT_DIR/report.html" > "$OUT_DIR/inspect.json"

echo "logs: $LOG_DIR"
echo "report: $OUT_DIR/report.html"
echo "json: $OUT_DIR/inspect.json"
