#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/gradle-plugin-smoke.sh"
KEEP_SMOKE_DIR=1
export GRADLE_OPTS="${GRADLE_OPTS:--Dorg.gradle.jvmargs=-Xmx2g\ -XX:MaxMetaspaceSize=768m}"
main "$@"
fixture_dir="$SMOKE_RUN_DIR/consumer"
adb="$(resolve_android_sdk_dir)/platform-tools/adb"
: "${ANDROID_SERIAL:?Set ANDROID_SERIAL for the identity fixture}"
mkdir -p "$fixture_dir/app/src/main/kotlin/com/example/jhsmoke"
cp "$SCRIPT_DIR/fixtures/r8-identity/IdentityExecutionProbe.kt" "$fixture_dir/app/src/main/kotlin/com/example/jhsmoke/"
python3 - "$fixture_dir" <<'PY'
from pathlib import Path
import sys
root = Path(sys.argv[1])
activity = root / "app/src/main/java/com/example/jhsmoke/MainActivity.java"
source = activity.read_text()
assert "        Helper.work();" in source
activity.write_text(source.replace("        Helper.work();", "        IdentityExecutionProbe.run(this);\n        Helper.work();"))
manifest = root / "app/src/main/AndroidManifest.xml"
source = manifest.read_text()
assert source.count("</application>") == 1
manifest.write_text(source.replace("</application>", '<service android:name=".IdentityService" android:exported="false" android:process=":identity" /></application>'))
with (root / "app/build.gradle.kts").open("a") as output:
    output.write('''
android.buildTypes.getByName("release").signingConfig = android.signingConfigs.getByName("debug")
android.buildTypes.getByName("release").isShrinkResources = true
''')
PY
"$ANDROID_DIR/gradlew" -p "$fixture_dir" :app:assembleRelease --configuration-cache --console=plain --stacktrace
go -C "$ROOT_DIR/cli" build -o "$SMOKE_RUN_DIR/jankhunter" ./cmd/jankhunter
python3 "$SCRIPT_DIR/run-r8-identity-probe.py" "$SMOKE_RUN_DIR" "$SMOKE_RUN_DIR/identity-release" \
  --adb "$adb" --serial "$ANDROID_SERIAL"
log "Two-process mapping identity PASS: $SMOKE_RUN_DIR"
