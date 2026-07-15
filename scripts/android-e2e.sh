#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_ID="io.jankhunter.sample"
TEST_APP_ID="io.jankhunter.sample.test"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/android-e2e}"
ADB="${ADB:-adb}"
DEVICE_SERIAL="${ANDROID_SERIAL:-}"
PYTHON="${PYTHON:-python3}"
ANDROID_BUILD_TOOLS_VERSION="${ANDROID_BUILD_TOOLS_VERSION:-}"
INSTRUMENTATION_DIAGNOSTICS=""
APP_APK=""
TEST_APK=""
AAPT=""
APP_INSTALLED=0
TEST_APP_INSTALLED=0
OUTPUT_MARKER_NAME=".jankhunter-android-e2e-owned"
OUTPUT_MARKER_VALUE="jankhunter-android-e2e:v1"

usage() {
  cat <<'EOF'
Usage:
  scripts/android-e2e.sh [options]

Options:
  --out-dir PATH    Output directory. Default: reports/android-e2e.
  --serial SERIAL   Android device/emulator serial. Required when several devices are online.
  --instrumentation-diagnostics PATH
                    Optional Gradle-plugin instrumentation diagnostics JSONL passed to inspect.
  -h, --help        Show this help.

Environment:
  OUT_DIR, ADB, ANDROID_SERIAL, PYTHON
  ANDROID_HOME / ANDROID_SDK_ROOT  Android SDK location.
  ANDROID_BUILD_TOOLS_VERSION     Installed numeric version; defaults to the highest installed.
EOF
}

log() {
  printf '[jankhunter-android-e2e] %s\n' "$*"
}

fail() {
  printf '[jankhunter-android-e2e] error: %s\n' "$*" >&2
  exit 1
}

require_value() {
  local option="$1"
  local value="${2:-}"
  [[ -n "$value" && "$value" != --* ]] || fail "$option requires a value"
}

require_command() {
  local command="$1"
  command -v "$command" >/dev/null 2>&1 || fail "required command was not found: $command"
}

local_properties_sdk_dir() {
  local file="$ROOT_DIR/android/local.properties"
  [[ -f "$file" ]] || return 0
  awk '
    /^[[:space:]]*sdk[.]dir[[:space:]]*=/ {
      sub(/^[^=]*=/, "")
      print
      exit
    }
  ' "$file"
}

resolve_android_sdk_dir() {
  local candidate=""
  local local_properties_sdk=""
  local_properties_sdk="$(local_properties_sdk_dir || true)"
  if [[ -n "${ANDROID_HOME:-}" ]]; then
    candidate="$ANDROID_HOME"
  elif [[ -n "${ANDROID_SDK_ROOT:-}" ]]; then
    candidate="$ANDROID_SDK_ROOT"
  elif [[ -n "$local_properties_sdk" && -d "$local_properties_sdk" ]]; then
    candidate="$local_properties_sdk"
  elif [[ -n "${HOME:-}" && -d "$HOME/Library/Android/sdk" ]]; then
    candidate="$HOME/Library/Android/sdk"
  elif [[ -n "${HOME:-}" && -d "$HOME/Android/Sdk" ]]; then
    candidate="$HOME/Android/Sdk"
  fi
  [[ -n "$candidate" ]] ||
    fail "Android SDK was not found; set ANDROID_HOME or ANDROID_SDK_ROOT"
  [[ -d "$candidate" ]] || fail "Android SDK path does not exist: $candidate"
  (cd "$candidate" && pwd -P)
}

resolve_android_build_tools_version() {
  local sdk_dir="$1"
  local build_tools_dir="$sdk_dir/build-tools"
  [[ -d "$build_tools_dir" ]] ||
    fail "Android Build Tools directory was not found: $build_tools_dir"
  if [[ -n "$ANDROID_BUILD_TOOLS_VERSION" ]]; then
    [[ "$ANDROID_BUILD_TOOLS_VERSION" =~ ^[0-9]+(\.[0-9]+){1,2}$ ]] ||
      fail "ANDROID_BUILD_TOOLS_VERSION must be a numeric version"
    [[ -d "$build_tools_dir/$ANDROID_BUILD_TOOLS_VERSION" ]] ||
      fail "Android Build Tools $ANDROID_BUILD_TOOLS_VERSION was not found in $build_tools_dir"
    printf '%s\n' "$ANDROID_BUILD_TOOLS_VERSION"
    return 0
  fi
  local resolved
  resolved="$(find "$build_tools_dir" -maxdepth 1 -mindepth 1 -type d -exec basename {} \; 2>/dev/null |
    sed -nE '/^[0-9]+([.][0-9]+){1,2}$/p' |
    sort -t. -k1,1n -k2,2n -k3,3n |
    tail -n 1)"
  [[ -n "$resolved" ]] || fail "No numeric Android Build Tools versions were found in $build_tools_dir"
  printf '%s\n' "$resolved"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-dir)
      require_value "$1" "${2:-}"
      OUT_DIR="$2"
      shift 2
      ;;
    --serial)
      require_value "$1" "${2:-}"
      DEVICE_SERIAL="$2"
      shift 2
      ;;
    --instrumentation-diagnostics)
      require_value "$1" "${2:-}"
      INSTRUMENTATION_DIAGNOSTICS="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

if [[ -n "$DEVICE_SERIAL" ]]; then
  [[ "$DEVICE_SERIAL" =~ ^[^[:space:]]+$ ]] || fail "invalid Android device serial"
fi
if [[ -n "$INSTRUMENTATION_DIAGNOSTICS" ]]; then
  [[ -f "$INSTRUMENTATION_DIAGNOSTICS" && -s "$INSTRUMENTATION_DIAGNOSTICS" ]] ||
    fail "instrumentation diagnostics file is missing or empty: $INSTRUMENTATION_DIAGNOSTICS"
  INSTRUMENTATION_DIAGNOSTICS="$(cd "$(dirname "$INSTRUMENTATION_DIAGNOSTICS")" && pwd -P)/$(basename "$INSTRUMENTATION_DIAGNOSTICS")"
fi

select_device() {
  local devices=()
  local device_output serial state rest
  if ! device_output="$("$ADB" devices 2>&1)"; then
    fail "adb devices failed: $device_output"
  fi
  while read -r serial state rest; do
    [[ "$state" == "device" ]] || continue
    devices+=("$serial")
  done <<< "$device_output"

  if [[ -n "$DEVICE_SERIAL" ]]; then
    local index
    for ((index = 0; index < ${#devices[@]}; index++)); do
      [[ "${devices[$index]}" == "$DEVICE_SERIAL" ]] && return 0
    done
    fail "device is not online or was not found: $DEVICE_SERIAL"
  fi

  if [[ "${#devices[@]}" -eq 0 ]]; then
    fail "no online Android device or emulator was found"
  fi
  if [[ "${#devices[@]}" -gt 1 ]]; then
    fail "several Android devices are online; pass --serial or set ANDROID_SERIAL"
  fi
  DEVICE_SERIAL="${devices[0]}"
}

prepare_output_directory() {
  [[ -n "$OUT_DIR" ]] || fail "output directory cannot be empty"
  mkdir -p "$OUT_DIR"
  OUT_DIR="$(cd "$OUT_DIR" && pwd -P)"
  case "$OUT_DIR" in
    /|"${HOME:-}"|"$ROOT_DIR")
      fail "refusing to clean unsafe output directory: $OUT_DIR"
      ;;
  esac
  local marker="$OUT_DIR/$OUTPUT_MARKER_NAME"
  if [[ -e "$marker" ]]; then
    [[ -f "$marker" && ! -L "$marker" ]] ||
      fail "refusing to clean output directory with an unsafe ownership marker: $marker"
    [[ "$(<"$marker")" == "$OUTPUT_MARKER_VALUE" ]] ||
      fail "refusing to clean output directory with an unknown ownership marker: $marker"
  else
    local entries=()
    shopt -s nullglob dotglob
    entries=("$OUT_DIR"/*)
    shopt -u nullglob dotglob
    [[ "${#entries[@]}" -eq 0 ]] ||
      fail "refusing to clean non-empty unowned output directory: $OUT_DIR"
    printf '%s\n' "$OUTPUT_MARKER_VALUE" > "$marker"
  fi
  find "$OUT_DIR" -mindepth 1 -maxdepth 1 ! -name "$OUTPUT_MARKER_NAME" -exec rm -rf -- {} +
  LOG_DIR="$OUT_DIR/logs"
  mkdir -p "$LOG_DIR"
}

cleanup_device_packages_best_effort() {
  if [[ "$TEST_APP_INSTALLED" -eq 1 ]]; then
    if "$ADB" -s "$DEVICE_SERIAL" uninstall "$TEST_APP_ID" >/dev/null 2>&1; then
      TEST_APP_INSTALLED=0
    else
      printf '[jankhunter-android-e2e] warning: could not uninstall %s during EXIT cleanup\n' \
        "$TEST_APP_ID" >&2
    fi
  fi
  if [[ "$APP_INSTALLED" -eq 1 ]]; then
    if "$ADB" -s "$DEVICE_SERIAL" uninstall "$APP_ID" >/dev/null 2>&1; then
      APP_INSTALLED=0
    else
      printf '[jankhunter-android-e2e] warning: could not uninstall %s during EXIT cleanup\n' \
        "$APP_ID" >&2
    fi
  fi
  return 0
}

uninstall_device_package() {
  local package_id="$1"
  local output=""
  if ! output="$("$ADB" -s "$DEVICE_SERIAL" uninstall "$package_id" 2>&1)"; then
    fail "could not uninstall $package_id from $DEVICE_SERIAL: $output"
  fi
  [[ "$output" == *Success* ]] ||
    fail "adb did not confirm uninstall of $package_id from $DEVICE_SERIAL: $output"
}

cleanup_device_packages_strict() {
  if [[ "$TEST_APP_INSTALLED" -eq 1 ]]; then
    uninstall_device_package "$TEST_APP_ID"
    TEST_APP_INSTALLED=0
  fi
  if [[ "$APP_INSTALLED" -eq 1 ]]; then
    uninstall_device_package "$APP_ID"
    APP_INSTALLED=0
  fi
}

require_built_apk() {
  local label="$1"
  local path="$2"
  [[ -f "$path" && -s "$path" ]] || fail "$label APK was not generated: $path"
}

require_apk_package() {
  local label="$1"
  local path="$2"
  local expected="$3"
  local badging=""
  local package_id=""
  if ! badging="$("$AAPT" dump badging "$path" 2>&1)"; then
    fail "could not inspect $label APK package: $badging"
  fi
  package_id="$(sed -n "s/^package: name='\([^']*\)'.*/\1/p" <<< "$badging")"
  [[ "$package_id" == "$expected" ]] ||
    fail "$label APK package is '$package_id', expected '$expected': $path"
}

require_package_absent() {
  local package_id="$1"
  local output=""
  if ! output="$("$ADB" -s "$DEVICE_SERIAL" shell pm list packages "$package_id" 2>&1)"; then
    fail "could not check whether $package_id is installed on $DEVICE_SERIAL: $output"
  fi
  [[ -z "${output//[[:space:]]/}" ]] ||
    fail "refusing to overwrite already installed package $package_id on $DEVICE_SERIAL"
}

run_instrumentation_test() {
  local output="$OUT_DIR/instrumentation.txt"
  local component="$TEST_APP_ID/androidx.test.runner.AndroidJUnitRunner"
  local test_class="io.jankhunter.sample.SampleEndToEndLogTest"
  if ! "$ADB" -s "$DEVICE_SERIAL" shell am instrument -w -r \
    -e class "$test_class" "$component" > "$output" 2>&1; then
    sed 's/^/[instrumentation] /' "$output" >&2 || true
    fail "instrumentation command failed on $DEVICE_SERIAL"
  fi
  if ! grep -Eq '^OK \(1 test\)[[:space:]]*$' "$output"; then
    sed 's/^/[instrumentation] /' "$output" >&2 || true
    fail "instrumentation did not report exactly one successful E2E test"
  fi
}

validate_inspect_json() {
  local inspect_json="$1"
  local diagnostics_supplied=0
  [[ -n "$INSTRUMENTATION_DIAGNOSTICS" ]] && diagnostics_supplied=1
  "$PYTHON" - "$inspect_json" "$diagnostics_supplied" <<'PY'
import json
import sys
from pathlib import Path


path = Path(sys.argv[1])
diagnostics_supplied = sys.argv[2] == "1"
failures = []

try:
    with path.open(encoding="utf-8") as source:
        summary = json.load(source)
except (OSError, UnicodeError, json.JSONDecodeError) as error:
    print(f"[jankhunter-android-e2e] error: invalid inspect JSON: {error}", file=sys.stderr)
    raise SystemExit(1)

if not isinstance(summary, dict):
    print("[jankhunter-android-e2e] error: inspect JSON root must be an object", file=sys.stderr)
    raise SystemExit(1)


def value(mapping, *names):
    if not isinstance(mapping, dict):
        return None
    for name in names:
        if name in mapping:
            return mapping[name]
    return None


def positive_integer(label, actual):
    if isinstance(actual, bool) or not isinstance(actual, int) or actual <= 0:
        failures.append(f"{label} must be a positive integer, found {actual!r}")


positive_integer("LogCount", value(summary, "LogCount", "log_count"))
positive_integer("EventCount", value(summary, "EventCount", "event_count"))
positive_integer("Dictionary", value(summary, "Dictionary", "dictionary"))
positive_integer("DataRecordCount", value(summary, "DataRecordCount", "data_record_count"))

quality = value(summary, "CollectionQuality", "collection_quality")
if not isinstance(quality, dict):
    failures.append("CollectionQuality is missing")
else:
    if value(quality, "complete", "Complete") is not True:
        failures.append("collection quality is incomplete")
    if value(quality, "chain_valid", "ChainValid") is not True:
        failures.append("collection segment chain is invalid")
    for label, names in (
        ("unsealed_segments", ("unsealed_segments", "UnsealedSegments")),
        ("known_lost_events", ("known_lost_events", "KnownLostEvents")),
        ("dictionary_overflow", ("dictionary_overflow", "DictionaryOverflow")),
        ("dictionary_truncated", ("dictionary_truncated", "DictionaryTruncated")),
    ):
        actual = value(quality, *names)
        if isinstance(actual, bool) or not isinstance(actual, int) or actual != 0:
            failures.append(f"collection quality {label} must be 0, found {actual!r}")
    accepted = value(quality, "accepted_events", "AcceptedEvents")
    written = value(quality, "written_events", "WrittenEvents")
    positive_integer("collection quality accepted_events", accepted)
    positive_integer("collection quality written_events", written)
    if isinstance(accepted, int) and not isinstance(accepted, bool) and isinstance(written, int) and not isinstance(written, bool) and accepted != written:
        failures.append(f"accepted_events differs from written_events: {accepted} != {written}")


def named_values(*names):
    rows = value(summary, *names)
    result = {}
    if not isinstance(rows, list):
        return result
    for row in rows:
        if not isinstance(row, dict):
            continue
        name = value(row, "Name", "name")
        actual = value(row, "Value", "value")
        if isinstance(name, str) and isinstance(actual, int) and not isinstance(actual, bool):
            result[name] = actual
    return result


counters = named_values("Counters", "counters")
for name in ("sample.e2e.retained.watch.count", "sample.e2e.background.count"):
    if counters.get(name, 0) <= 0:
        failures.append(f"expected positive counter is missing: {name}")

gauges = named_values("Gauges", "gauges")
if gauges.get("sample.e2e.background.duration_ms", 0) <= 0:
    failures.append("expected positive gauge is missing: sample.e2e.background.duration_ms")


def string_fields(collection_names, field_names):
    rows = value(summary, *collection_names)
    result = set()
    if not isinstance(rows, list):
        return result
    for row in rows:
        actual = value(row, *field_names)
        if isinstance(actual, str):
            result.add(actual)
    return result


screens = string_fields(("Screens", "screens"), ("Screen", "screen"))
screens.update(string_fields(("Flows", "flows"), ("Screen", "screen")))
screens.update(string_fields(("ProblemWindows", "problem_windows"), ("Screen", "screen")))
if "SampleEndToEnd" not in screens:
    failures.append("expected screen context is missing: SampleEndToEnd")

owners = string_fields(("Owners", "owners"), ("Owner", "owner"))
if "sample.e2e.synthetic_stall" not in owners:
    failures.append("expected owner is missing: sample.e2e.synthetic_stall")

warnings = value(summary, "Warnings", "warnings")
if warnings is None:
    warnings = []
if not isinstance(warnings, list) or any(not isinstance(item, str) for item in warnings):
    failures.append("Warnings must be an array of strings")
    warnings = []

for warning in warnings:
    lowered = warning.lower()
    missing_asm = "asm-диагностика не передана" in lowered
    if missing_asm and not diagnostics_supplied:
        continue
    forbidden = (
        "ignored partial",
        "partial trailing",
        "corrupt",
        "очередь writer",
        "очередь событий была заполнена",
        "writer видел ошибки",
        "writer потерял",
        "writer встретил ошибки",
        "события потеряны",
        "чанки не удалось",
        "закрытие writer",
        "runtime-граф",
        "runtime graph",
        "runtime_call_graph",
        "словарь .jhlog",
        "dictionary overflow",
        "dictionary truncated",
        "overflow-ссыл",
        "значения словаря были усечены",
        "asm-диагност",
    )
    if any(fragment in lowered for fragment in forbidden):
        failures.append(f"forbidden collection-quality warning: {warning}")

if failures:
    for failure in failures:
        print(f"[jankhunter-android-e2e] error: {failure}", file=sys.stderr)
    raise SystemExit(1)
PY
}

require_command "$ADB"
require_command go
require_command tar
require_command "$PYTHON"
[[ -x "$ROOT_DIR/android/gradlew" ]] || fail "Gradle wrapper not found: $ROOT_DIR/android/gradlew"
ANDROID_SDK_DIR="$(resolve_android_sdk_dir)"
RESOLVED_ANDROID_BUILD_TOOLS_VERSION="$(resolve_android_build_tools_version "$ANDROID_SDK_DIR")"
AAPT="$ANDROID_SDK_DIR/build-tools/$RESOLVED_ANDROID_BUILD_TOOLS_VERSION/aapt"
[[ -x "$AAPT" ]] || fail "aapt was not found or is not executable: $AAPT"
select_device
export ANDROID_SERIAL="$DEVICE_SERIAL"
prepare_output_directory

log "device: $DEVICE_SERIAL"
log "Android SDK: $ANDROID_SDK_DIR"
log "Android Build Tools: $RESOLVED_ANDROID_BUILD_TOOLS_VERSION"
log "building sample app and instrumentation APKs"
(
  cd "$ROOT_DIR/android"
  ANDROID_HOME="$ANDROID_SDK_DIR" ANDROID_SDK_ROOT="$ANDROID_SDK_DIR" \
    ./gradlew :sample-app:assembleDebug :sample-app:assembleDebugAndroidTest \
    -PjankHunterBuildToolsVersion="$RESOLVED_ANDROID_BUILD_TOOLS_VERSION" \
    --no-daemon --console=plain
)

APP_APK="$ROOT_DIR/android/sample-app/build/outputs/apk/debug/sample-app-debug.apk"
TEST_APK="$ROOT_DIR/android/sample-app/build/outputs/apk/androidTest/debug/sample-app-debug-androidTest.apk"
require_built_apk "sample app" "$APP_APK"
require_built_apk "instrumentation" "$TEST_APK"
require_apk_package "sample app" "$APP_APK" "$APP_ID"
require_apk_package "instrumentation" "$TEST_APK" "$TEST_APP_ID"
require_package_absent "$APP_ID"
require_package_absent "$TEST_APP_ID"

# Gradle's connectedDebugAndroidTest uninstalls both packages before returning, which makes
# a later run-as copy impossible. Keep the packages installed until the private logs are copied.
trap cleanup_device_packages_best_effort EXIT
log "installing sample app on $DEVICE_SERIAL"
APP_INSTALLED=1
"$ADB" -s "$DEVICE_SERIAL" install -r -t "$APP_APK" >/dev/null
TEST_APP_INSTALLED=1
"$ADB" -s "$DEVICE_SERIAL" install -r -t "$TEST_APK" >/dev/null

log "running sample app instrumentation test"
run_instrumentation_test

log "copying .jhlog files from $APP_ID"
if ! "$ADB" -s "$DEVICE_SERIAL" exec-out run-as "$APP_ID" \
  tar -C files/jankhunter-e2e -cf - . | tar -xf - -C "$LOG_DIR"; then
  fail "could not copy files/jankhunter-e2e from $APP_ID on $DEVICE_SERIAL"
fi
cleanup_device_packages_strict
trap - EXIT

shopt -s nullglob
logs=("$LOG_DIR"/*.jhlog)
shopt -u nullglob
[[ "${#logs[@]}" -gt 0 ]] || fail "device test completed but no .jhlog files were copied"

log "building HTML report and JSON summary"
(
  cd "$ROOT_DIR/cli"
  inspect_command=(
    go run ./cmd/jankhunter inspect "${logs[@]}" --json
    --out "$OUT_DIR/report.html"
  )
  generated_artifacts="$ROOT_DIR/android/sample-app/build/generated/jankhunter/debug"
  if [[ -s "$generated_artifacts/owner-map.json" &&
    -s "$generated_artifacts/class-graph.jsonl" &&
    -s "$generated_artifacts/instrumentation-diagnostics.jsonl" ]]; then
    inspect_command+=(--artifacts-dir "$generated_artifacts")
  fi
  if [[ -n "$INSTRUMENTATION_DIAGNOSTICS" ]]; then
    inspect_command+=(--instrumentation-diagnostics "$INSTRUMENTATION_DIAGNOSTICS")
  fi
  "${inspect_command[@]}" > "$OUT_DIR/inspect.json"
)

[[ -s "$OUT_DIR/report.html" ]] || fail "HTML report was not generated"
[[ -s "$OUT_DIR/inspect.json" ]] || fail "JSON summary was not generated"
validate_inspect_json "$OUT_DIR/inspect.json"

log "logs: $LOG_DIR"
log "instrumentation: $OUT_DIR/instrumentation.txt"
log "report: $OUT_DIR/report.html"
log "json: $OUT_DIR/inspect.json"
