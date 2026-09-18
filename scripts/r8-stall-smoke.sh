#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/gradle-plugin-smoke.sh"
KEEP_SMOKE_DIR=1
export GRADLE_OPTS="${GRADLE_OPTS:--Dorg.gradle.jvmargs=-Xmx2g\ -XX:MaxMetaspaceSize=768m}"
main "$@"
fixture_dir="$SMOKE_RUN_DIR/consumer"
adb="$(resolve_android_sdk_dir)/platform-tools/adb"
: "${ANDROID_SERIAL:?Set ANDROID_SERIAL for the stall fixture}"
mkdir -p "$fixture_dir/app/src/main/kotlin/com/example/jhsmoke"
cp "$SCRIPT_DIR/fixtures/r8-stall/StallExecutionProbe.kt" "$fixture_dir/app/src/main/kotlin/com/example/jhsmoke/"
python3 - "$fixture_dir" <<'PY'
from pathlib import Path
import sys
root = Path(sys.argv[1])
activity = root / "app/src/main/java/com/example/jhsmoke/MainActivity.java"
source = activity.read_text()
assert "        Helper.work();" in source
activity.write_text(source.replace("        Helper.work();", "        getWindow().getDecorView().post(() -> StallExecutionProbe.run(this));\n        Helper.work();"))
with (root / "app/build.gradle.kts").open("a") as output:
    output.write('''
dependencies { implementation("androidx.lifecycle:lifecycle-livedata-core:2.9.4") }
android.buildTypes.getByName("release").signingConfig = android.signingConfigs.getByName("debug")
android.buildTypes.getByName("release").isShrinkResources = true
''')
PY
"$ANDROID_DIR/gradlew" -p "$fixture_dir" :app:assembleDebug :app:assembleRelease --configuration-cache --console=plain --stacktrace
go -C "$ROOT_DIR/cli" build -o "$SMOKE_RUN_DIR/jankhunter" ./cmd/jankhunter
for variant in debug release; do
  python3 "$SCRIPT_DIR/run-r8-stall-probe.py" "$SMOKE_RUN_DIR" "$SMOKE_RUN_DIR/stall-$variant" \
    --adb "$adb" --serial "$ANDROID_SERIAL" --variant "$variant"
  python3 "$SCRIPT_DIR/validate-r8-stall.py" "$SMOKE_RUN_DIR/stall-$variant/events.jsonl"
done
# SDK-only runs validate raw evidence. With the offline bundle, additionally verify the CLI chain.
if [[ -n "${JANK_HUNTER_RETRACE_HOME:-}" ]]; then
  "$SMOKE_RUN_DIR/jankhunter" inspect "$SMOKE_RUN_DIR/stall-release/"*.jhlog \
    --mapping "$SMOKE_RUN_DIR/stall-release/mapping.txt" --json --out "$SMOKE_RUN_DIR/stall-report.html" \
    > "$SMOKE_RUN_DIR/stall-report.json-output"
  python3 "$SCRIPT_DIR/validate-r8-stall.py" "$SMOKE_RUN_DIR/stall-release/events.jsonl" "$SMOKE_RUN_DIR/stall-report.json-output"
fi
log "Bounded raw stall evidence PASS: $SMOKE_RUN_DIR"
