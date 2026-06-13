#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_ID="${APP_ID:-io.jankhunter.sample}"
MAIN_COMPONENT="${MAIN_COMPONENT:-$APP_ID/.MainActivity}"
TMP_DIR="${TMP_DIR:-$ROOT_DIR/tmp}"
RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)}"
OUT_DIR="$TMP_DIR/sample-app-$RUN_ID"
EMULATOR_LOG="$OUT_DIR/emulator.log"
ADB="${ADB:-adb}"
EMULATOR="${EMULATOR:-emulator}"
AVD_NAME="${AVD_NAME:-}"
DEVICE_SERIAL="${DEVICE_SERIAL:-}"
KEEP_EMULATOR="${KEEP_EMULATOR:-0}"
UI_LANG="${JH_LANG:-}"

STARTED_EMULATOR_PID=""
REPORT_PATH=""
PULL_COUNT=0
CLEANED_UP=0

log() {
  printf '[jankhunter] %s\n' "$*"
}

fail() {
  printf '[jankhunter] error: %s\n' "$*" >&2
  exit 1
}

detect_ui_language() {
  local lang="$UI_LANG"

  if [[ -z "$lang" ]] && command -v defaults >/dev/null 2>&1; then
    lang="$(defaults read -g AppleLanguages 2>/dev/null | awk -F\" '/"/ { print $2; exit }' || true)"
  fi

  if [[ -z "$lang" ]]; then
    lang="${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}"
  fi

  lang="$(printf '%s' "$lang" | tr '[:upper:]' '[:lower:]')"
  case "$lang" in
    ru | ru_* | ru-* )
      UI_LANG="ru"
      ;;
    * )
      UI_LANG="en"
      ;;
  esac
}

resolve_android_tools() {
  if ! command -v "$ADB" >/dev/null 2>&1; then
    if [[ -n "${ANDROID_HOME:-}" && -x "$ANDROID_HOME/platform-tools/adb" ]]; then
      ADB="$ANDROID_HOME/platform-tools/adb"
    elif [[ -n "${ANDROID_SDK_ROOT:-}" && -x "$ANDROID_SDK_ROOT/platform-tools/adb" ]]; then
      ADB="$ANDROID_SDK_ROOT/platform-tools/adb"
    else
      fail "adb not found. Install Android platform-tools or set ADB=/path/to/adb."
    fi
  fi

  if ! command -v "$EMULATOR" >/dev/null 2>&1; then
    if [[ -n "${ANDROID_HOME:-}" && -x "$ANDROID_HOME/emulator/emulator" ]]; then
      EMULATOR="$ANDROID_HOME/emulator/emulator"
    elif [[ -n "${ANDROID_SDK_ROOT:-}" && -x "$ANDROID_SDK_ROOT/emulator/emulator" ]]; then
      EMULATOR="$ANDROID_SDK_ROOT/emulator/emulator"
    fi
  fi
}

adb_cmd() {
  if [[ -n "$DEVICE_SERIAL" ]]; then
    "$ADB" -s "$DEVICE_SERIAL" "$@"
  else
    "$ADB" "$@"
  fi
}

first_ready_device() {
  "$ADB" devices | awk 'NR > 1 && $2 == "device" { print $1; exit }'
}

first_ready_emulator() {
  "$ADB" devices | awk 'NR > 1 && $1 ~ /^emulator-/ && $2 == "device" { print $1; exit }'
}

pick_avd() {
  if [[ -n "$AVD_NAME" ]]; then
    printf '%s\n' "$AVD_NAME"
    return
  fi
  "$EMULATOR" -list-avds | sed '/^[[:space:]]*$/d' | head -n 1
}

wait_for_device_serial() {
  local serial=""
  for _ in $(seq 1 120); do
    serial="$(first_ready_emulator || true)"
    if [[ -n "$serial" ]]; then
      DEVICE_SERIAL="$serial"
      return
    fi
    sleep 2
  done
  fail "emulator did not appear in adb devices. See $EMULATOR_LOG"
}

wait_for_boot() {
  log "waiting for Android boot on $DEVICE_SERIAL"
  for _ in $(seq 1 180); do
    local boot_completed=""
    boot_completed="$(adb_cmd shell getprop sys.boot_completed 2>/dev/null | tr -d '\r' || true)"
    if [[ "$boot_completed" == "1" ]]; then
      adb_cmd shell input keyevent 82 >/dev/null 2>&1 || true
      log "device is ready"
      return
    fi
    sleep 2
  done
  fail "device boot timed out"
}

ensure_device() {
  "$ADB" start-server >/dev/null

  if [[ -n "$DEVICE_SERIAL" ]]; then
    log "using requested device: $DEVICE_SERIAL"
    wait_for_boot
    return
  fi

  local existing=""
  existing="$(first_ready_device || true)"
  if [[ -n "$existing" ]]; then
    DEVICE_SERIAL="$existing"
    log "using existing device/emulator: $DEVICE_SERIAL"
    wait_for_boot
    return
  fi

  command -v "$EMULATOR" >/dev/null 2>&1 || fail "emulator not found. Set EMULATOR=/path/to/emulator or start a device manually."

  local avd=""
  avd="$(pick_avd)"
  [[ -n "$avd" ]] || fail "no Android Virtual Device found. Create one in Android Studio or set AVD_NAME."

  log "starting emulator AVD: $avd"
  "$EMULATOR" -avd "$avd" -netdelay none -netspeed full -no-snapshot-save >"$EMULATOR_LOG" 2>&1 &
  STARTED_EMULATOR_PID="$!"
  wait_for_device_serial
  wait_for_boot
}

install_and_launch() {
  log "building and installing sample app"
  (cd "$ROOT_DIR/android" && ANDROID_SERIAL="$DEVICE_SERIAL" ./gradlew :sample-app:installDebug --no-daemon)

  log "clearing previous sample app data"
  adb_cmd shell am force-stop "$APP_ID" >/dev/null 2>&1 || true
  adb_cmd shell pm clear "$APP_ID" >/dev/null

  log "launching $MAIN_COMPONENT"
  adb_cmd shell am start -n "$MAIN_COMPONENT" >/dev/null
}

print_help() {
  if [[ "$UI_LANG" == "ru" ]]; then
    cat <<EOF

Команды:
  log      выгрузить текущие .jhlog и сгенерировать HTML-отчет, приложение продолжит работать
  report   то же самое, что log
  stop     выгрузить логи, сгенерировать отчет, остановить приложение и выйти
  стоп     то же самое, что stop
  open     открыть последний сгенерированный отчет на macOS
  help     показать эту подсказку
  quit     выйти без выгрузки логов

Директория вывода:
  $OUT_DIR

EOF
    return
  fi

  cat <<EOF

Commands:
  log      pull current .jhlog files and generate an HTML report, keep app running
  report   same as log
  stop     pull logs, generate report, stop app and exit
  стоп     same as stop
  open     open the latest generated report on macOS
  help     show this help
  quit     exit without pulling logs

Output directory:
  $OUT_DIR

EOF
}

pull_and_report() {
  PULL_COUNT=$((PULL_COUNT + 1))
  local pull_id
  pull_id="$(date +%H%M%S)-$PULL_COUNT"
  local pull_dir="$OUT_DIR/pull-$pull_id"
  local log_dir="$pull_dir/logs"
  local report="$pull_dir/report.html"
  local json="$pull_dir/inspect.json"

  mkdir -p "$log_dir"

  log "waiting for the writer flush interval"
  sleep 2

  if ! adb_cmd shell run-as "$APP_ID" sh -c "ls files/jankhunter/*.jhlog >/dev/null 2>&1"; then
    fail "no .jhlog files found in app data. Interact with the sample app first, then run 'log' or 'stop'."
  fi

  log "pulling .jhlog files to $log_dir"
  adb_cmd exec-out run-as "$APP_ID" tar -C files/jankhunter -cf - . | tar -xf - -C "$log_dir"

  local logs=()
  while IFS= read -r file; do
    logs+=("$file")
  done < <(find "$log_dir" -type f -name '*.jhlog' -size +0c | sort)

  if [[ "${#logs[@]}" -eq 0 ]]; then
    fail "pulled logs are empty in $log_dir"
  fi

  log "generating CLI report"
  command -v go >/dev/null 2>&1 || fail "go not found. Install Go or add it to PATH before generating the report."
  (cd "$ROOT_DIR/cli" && go run ./cmd/jankhunter inspect "${logs[@]}" --json --out "$report" >"$json")

  REPORT_PATH="$report"
  log "logs: $log_dir"
  log "report: $report"
  log "json: $json"
}

open_latest_report() {
  if [[ -z "$REPORT_PATH" ]]; then
    log "no report generated yet; run 'log' first"
    return
  fi
  if command -v open >/dev/null 2>&1; then
    open "$REPORT_PATH"
  else
    log "latest report: $REPORT_PATH"
  fi
}

cleanup_emulator() {
  if [[ "$CLEANED_UP" == "1" ]]; then
    return
  fi
  CLEANED_UP=1
  if [[ -n "$STARTED_EMULATOR_PID" && "$KEEP_EMULATOR" != "1" ]]; then
    log "stopping emulator started by this script"
    adb_cmd emu kill >/dev/null 2>&1 || kill "$STARTED_EMULATOR_PID" >/dev/null 2>&1 || true
  fi
}

main() {
  trap cleanup_emulator EXIT
  mkdir -p "$OUT_DIR"
  detect_ui_language
  resolve_android_tools
  ensure_device
  install_and_launch
  print_help

  while true; do
    read -r -p "jankhunter> " command || command="stop"
    case "${command:-help}" in
      log | pull | report)
        pull_and_report
        ;;
      open)
        open_latest_report
        ;;
      stop | "стоп")
        pull_and_report
        adb_cmd shell am force-stop "$APP_ID" >/dev/null 2>&1 || true
        cleanup_emulator
        log "done"
        break
        ;;
      quit | exit)
        cleanup_emulator
        log "exited without pulling logs"
        break
        ;;
      help | "?")
        print_help
        ;;
      *)
        log "unknown command: $command"
        print_help
        ;;
    esac
  done
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  detect_ui_language
  print_help
  exit 0
fi

main "$@"
