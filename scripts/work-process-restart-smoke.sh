#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
adb="${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"
serial="${ANDROID_SERIAL:-$("$adb" get-serialno)}"
[[ -n "$serial" && "$serial" != "unknown" ]] || { echo 'No Android device' >&2; exit 1; }
adb_args=(-s "$serial")
"$root/android/gradlew" -p "$root/android" :jankhunter-workmanager:assembleDebugAndroidTest --offline
"$adb" "${adb_args[@]}" install -r \
  "$root/android/jankhunter-workmanager/build/outputs/apk/androidTest/debug/jankhunter-workmanager-debug-androidTest.apk"

run_stage() {
  local method="$1" stage="$2" result
  result="$("$adb" "${adb_args[@]}" shell am instrument -w \
    -e class "io.jankhunter.workmanager.WorkProcessRestartArtTest#$method" \
    -e jankhunterProcessRestartStage "$stage" \
    io.jankhunter.workmanager.test/androidx.test.runner.AndroidJUnitRunner)"
  printf '%s\n' "$result"
  [[ "$result" == *'OK (1 test)'* ]] || { echo "Stage $stage failed" >&2; exit 1; }
}

run_stage stageOneEnqueuesDelayedWork one
"$adb" "${adb_args[@]}" shell am force-stop io.jankhunter.workmanager.test
run_stage stageTwoObservesExecutionInRestartedProcess two
echo 'Work process restart PASS'
