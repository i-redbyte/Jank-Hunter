#!/usr/bin/env bash
set -euo pipefail

PACKAGE=""
OUT_DIR=""
ADB_BIN="${ADB:-adb}"
DEVICE=""
CLI_BIN="${JANKHUNTER_CLI:-}"
REPORT_NAME="report.html"

usage() {
  cat <<'USAGE'
Usage:
  collect-android-leak-report.sh --package com.example.app --out /tmp/jankhunter-leaks [options]

Options:
  --adb /path/to/adb          adb binary, defaults to adb or $ADB
  --device SERIAL            adb device serial
  --cli /path/to/jankhunter  built CLI binary. If omitted, script uses `go run ./cmd/jankhunter` from cli/
  --report NAME.html         report file name inside --out, defaults to report.html

The script pulls files/jankhunter from a debuggable app via `run-as`, then runs:
  jankhunter inspect <pulled .jhlog> --heap-dump <pulled .hprof> --out <report>

It produces report.html plus report-leaks.html, report-math.html and other companion reports.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --package)
      PACKAGE="${2:-}"; shift 2 ;;
    --out)
      OUT_DIR="${2:-}"; shift 2 ;;
    --adb)
      ADB_BIN="${2:-}"; shift 2 ;;
    --device)
      DEVICE="${2:-}"; shift 2 ;;
    --cli)
      CLI_BIN="${2:-}"; shift 2 ;;
    --report)
      REPORT_NAME="${2:-}"; shift 2 ;;
    -h|--help)
      usage; exit 0 ;;
    *)
      echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "$PACKAGE" || -z "$OUT_DIR" ]]; then
  usage >&2
  exit 2
fi

ADB_ARGS=()
if [[ -n "$DEVICE" ]]; then
  ADB_ARGS=(-s "$DEVICE")
fi

mkdir -p "$OUT_DIR/pulled"

echo "Pulling files/jankhunter from $PACKAGE..."
if ! "$ADB_BIN" "${ADB_ARGS[@]}" exec-out run-as "$PACKAGE" sh -c 'cd files 2>/dev/null && [ -d jankhunter ] && tar cf - jankhunter' \
  | tar -C "$OUT_DIR/pulled" -xf -; then
  echo "Could not pull files/jankhunter. Make sure $PACKAGE is debuggable, was launched, and has Jank Hunter enabled." >&2
  exit 1
fi

LOGS=()
while IFS= read -r path; do
  LOGS+=("$path")
done < <(find "$OUT_DIR/pulled" -type f -name '*.jhlog' | sort)

HEAPS=()
while IFS= read -r path; do
  HEAPS+=("$path")
done < <(find "$OUT_DIR/pulled" -type f -name '*.hprof' | sort)

if [[ ${#LOGS[@]} -eq 0 ]]; then
  echo "No .jhlog files found under $OUT_DIR/pulled. Run the app scenario and call JankHunter.flush()." >&2
  exit 1
fi

REPORT_PATH="$OUT_DIR/$REPORT_NAME"
CMD=()
if [[ -n "$CLI_BIN" ]]; then
  CMD=("$CLI_BIN")
else
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  CLI_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
  CMD=(go run "$CLI_DIR/cmd/jankhunter")
fi

ARGS=(inspect "${LOGS[@]}" --out "$REPORT_PATH")
if [[ ${#HEAPS[@]} -gt 0 ]]; then
  HEAP_JOINED="$(IFS=,; echo "${HEAPS[*]}")"
  ARGS+=(--heap-dump "$HEAP_JOINED")
fi

echo "Running ${CMD[*]} ${ARGS[*]}"
"${CMD[@]}" "${ARGS[@]}"

echo "Report: $REPORT_PATH"
echo "Leak report: ${REPORT_PATH%.html}-leaks.html"
