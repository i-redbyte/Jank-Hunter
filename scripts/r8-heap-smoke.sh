#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/gradle-plugin-smoke.sh"
KEEP_SMOKE_DIR=1
export GRADLE_OPTS="${GRADLE_OPTS:--Dorg.gradle.jvmargs=-Xmx2g\ -XX:MaxMetaspaceSize=768m}"
main "$@"
fixture_dir="$SMOKE_RUN_DIR/consumer"
adb="$(resolve_android_sdk_dir)/platform-tools/adb"
: "${JANK_HUNTER_RETRACE_HOME:?Set JANK_HUNTER_RETRACE_HOME to the offline Retrace bundle}"
: "${ANDROID_SERIAL:?Set ANDROID_SERIAL for the heap fixture}"
mkdir -p "$fixture_dir/app/src/main/kotlin/com/example/jhsmoke"
cp "$SCRIPT_DIR/fixtures/r8-heap/HeapExecutionProbe.kt" "$fixture_dir/app/src/main/kotlin/com/example/jhsmoke/"
python3 - "$fixture_dir" <<'PY'
from pathlib import Path
import sys
root = Path(sys.argv[1])
activity = root / "app/src/main/java/com/example/jhsmoke/MainActivity.java"
source = activity.read_text()
assert "        Helper.work();" in source
activity.write_text(source.replace("        Helper.work();", "        HeapExecutionProbe.run(this);\n        Helper.work();"))
# Preserve only fixture object shape while still requiring R8 to rename fields/classes.
(root / "app/heap-probe.pro").write_text("-keep,allowobfuscation class com.example.jhsmoke.HeapExecutionProbe$* { *; }\n-keepclassmembers,allowobfuscation class com.example.jhsmoke.HeapExecutionProbe { public static *** retainedHolder; }\n")
with (root / "app/build.gradle.kts").open("a") as output:
    output.write('''
android.buildTypes.getByName("release").signingConfig = android.signingConfigs.getByName("debug")
android.buildTypes.getByName("release").isShrinkResources = true
android.buildTypes.getByName("release").proguardFiles("heap-probe.pro")
''')
PY
"$ANDROID_DIR/gradlew" -p "$fixture_dir" :app:assembleRelease --configuration-cache --console=plain --stacktrace
go -C "$ROOT_DIR/cli" build -o "$SMOKE_RUN_DIR/jankhunter" ./cmd/jankhunter
python3 "$SCRIPT_DIR/run-r8-heap-probe.py" "$SMOKE_RUN_DIR" "$SMOKE_RUN_DIR/heap-release" \
  --adb "$adb" --serial "$ANDROID_SERIAL" --cli "$SMOKE_RUN_DIR/jankhunter"
log "Ordinary inherited field Retrace PASS: $SMOKE_RUN_DIR"
