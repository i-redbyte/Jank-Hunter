#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Reuse the external published SDK/plugin fixture and its owned temporary directory.
source "$SCRIPT_DIR/gradle-plugin-smoke.sh"
KEEP_SMOKE_DIR=1
export GRADLE_OPTS="${GRADLE_OPTS:--Dorg.gradle.jvmargs=-Xmx2g\ -XX:MaxMetaspaceSize=768m}"
main "$@"

fixture_dir="$SMOKE_RUN_DIR/consumer"
sdk_dir="$(resolve_android_sdk_dir)"
adb="$sdk_dir/platform-tools/adb"
adb_args=()
if [[ -n "${ANDROID_SERIAL:-}" ]]; then adb_args=(-s "$ANDROID_SERIAL"); fi
"$adb" "${adb_args[@]}" get-state

launch_probe() {
  local apk="$1" expected="$2" output="$3" pid capture_pid outcome=1
  "$adb" "${adb_args[@]}" install -r "$apk"
  "$adb" "${adb_args[@]}" shell am start -S -W -n com.example.jhsmoke/.MainActivity
  pid="$("$adb" "${adb_args[@]}" shell pidof com.example.jhsmoke | tr -d '\r')"
  [[ "$pid" =~ ^[0-9]+$ ]] || fail "Probe process did not start"
  # Stream this process so a busy device cannot evict evidence between polls.
  "$adb" "${adb_args[@]}" logcat --pid="$pid" > "$output" &
  capture_pid=$!
  for attempt in {1..20}; do
    if grep -Fq "$expected" "$output"; then outcome=0; break; fi
    if grep -Eq 'FATAL EXCEPTION|EXECUTION FAIL' "$output"; then break; fi
    sleep 1
  done
  kill "$capture_pid" 2>/dev/null || true
  wait "$capture_pid" 2>/dev/null || true
  [[ "$outcome" == 0 ]] || fail "Probe did not report $expected: $output"
}

# The original fixture has no WorkManager dependency. Exercise unrelated SDK functions
# and classification of an unknown object before adding that optional dependency.
launch_probe "$fixture_dir/app/build/outputs/apk/debug/app-debug.apk" ready "$SMOKE_RUN_DIR/no-workmanager.log"
"$sdk_dir/build-tools/$(resolve_build_tools_version "$sdk_dir")/apksigner" sign \
  --ks "$HOME/.android/debug.keystore" --ks-pass pass:android \
  --out "$SMOKE_RUN_DIR/no-workmanager-release.apk" \
  "$fixture_dir/app/build/outputs/apk/release/app-release-unsigned.apk"
launch_probe "$SMOKE_RUN_DIR/no-workmanager-release.apk" ready "$SMOKE_RUN_DIR/no-workmanager-release.log"

mkdir -p "$fixture_dir/app/src/main/kotlin/com/example/jhsmoke"
cp "$SCRIPT_DIR/fixtures/r8-worker/WorkerReleaseProbe.kt" "$fixture_dir/app/src/main/kotlin/com/example/jhsmoke/"
cp "$SCRIPT_DIR/fixtures/r8-worker/WorkerExecutionProbe.kt" "$fixture_dir/app/src/main/kotlin/com/example/jhsmoke/"
cp "$SCRIPT_DIR/fixtures/r8-worker/LegacyWorkerCaller.java" "$fixture_dir/app/src/main/java/com/example/jhsmoke/"
python3 - "$fixture_dir" <<'PY'
import sys
from pathlib import Path
root = Path(sys.argv[1])
activity = root / "app/src/main/java/com/example/jhsmoke/MainActivity.java"
activity.write_text(activity.read_text().replace("        Helper.work();", "        WorkerReleaseProbe.run();\n        WorkerExecutionProbe.run(getApplicationContext());\n        Helper.work();"))
with (root / "app/build.gradle.kts").open("a") as output:
    output.write('''
dependencies { implementation("androidx.work:work-runtime:2.11.2") }
jankHunter.enable(io.jankhunter.gradle.JankHunterFeature.WORKERS)
android.buildTypes.getByName("release").signingConfig = android.signingConfigs.getByName("debug")
''')
PY
group="$(properties_value jankHunterGroup "$ANDROID_DIR/gradle.properties")"
version="$(properties_value jankHunterVersion "$ANDROID_DIR/gradle.properties")"
printf '\ndependencies { implementation("%s:jankhunter-workmanager:%s") }\n' "$group" "$version" >> "$fixture_dir/app/build.gradle.kts"
"$ANDROID_DIR/gradlew" -p "$fixture_dir" :app:assembleRelease --configuration-cache --console=plain --stacktrace
launch_probe "$fixture_dir/app/build/outputs/apk/release/app-release.apk" 'EXECUTION PASS cases=11' "$SMOKE_RUN_DIR/worker-release.log"
grep -Fq 'PASS cases=7' "$SMOKE_RUN_DIR/worker-release.log" || fail "Legacy worker classification did not pass"
python3 - "$fixture_dir/app/build/outputs/mapping/release/mapping.txt" "$SMOKE_RUN_DIR/worker-release.log" <<'PY_MAPPING'
from pathlib import Path
import re
import sys
mapping = dict(re.findall(r"^([^\s].*) -> (.*):$", Path(sys.argv[1]).read_text(), re.M))
for suffix in ("Success", "Failure", "Retry"):
    framework = "androidx.work.ListenableWorker$Result$" + suffix
    assert mapping.get(framework) == framework, "Consumer reflection rule missing: " + framework
# R8 may horizontally merge the three empty application classes into one mapped class.
# Check the types actually passed to both legacy classifiers, including merged aliases.
types = dict(re.findall(r"case=(\d+) hook=\d+ port=\S+ type=(\S+)", Path(sys.argv[2]).read_text()))
for case in ("3", "4", "5"):
    actual = types[case]
    assert actual in mapping.values() and not actual.startswith("com.example.jhsmoke."), actual
PY_MAPPING
archive_path="$(sed -n 's/.*EXECUTION PASS cases=11 archive=//p' "$SMOKE_RUN_DIR/worker-release.log" | tail -n 1 | tr -d '\r')"
[[ "$archive_path" == /storage/emulated/0/Android/data/com.example.jhsmoke/files/worker-execution-*.zip ]] || \
  fail "Unexpected fixture archive path: $archive_path"
"$adb" "${adb_args[@]}" pull "$archive_path" "$SMOKE_RUN_DIR/worker-execution.zip"
python3 - "$SMOKE_RUN_DIR" <<'PY'
from pathlib import Path
from zipfile import ZipFile
import sys
root = Path(sys.argv[1])
logs = root / "worker-logs"
logs.mkdir()
with ZipFile(root / "worker-execution.zip") as archive:
    for entry in archive.infolist():
        assert Path(entry.filename).name == entry.filename and entry.filename.endswith(".jhlog")
        (logs / entry.filename).write_bytes(archive.read(entry))
PY
go -C "$ROOT_DIR/cli" build -o "$SMOKE_RUN_DIR/jankhunter" ./cmd/jankhunter
"$SMOKE_RUN_DIR/jankhunter" export "$SMOKE_RUN_DIR/worker-logs/"*.jhlog --out "$SMOKE_RUN_DIR/worker-events.jsonl"
python3 "$SCRIPT_DIR/validate-r8-workers.py" "$SMOKE_RUN_DIR/worker-events.jsonl"
log "R8 worker runtime PASS: $SMOKE_RUN_DIR"
