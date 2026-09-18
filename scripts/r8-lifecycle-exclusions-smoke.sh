#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/gradle-plugin-smoke.sh"
KEEP_SMOKE_DIR=1
export GRADLE_OPTS="${GRADLE_OPTS:--Dorg.gradle.jvmargs=-Xmx2g\ -XX:MaxMetaspaceSize=768m}"
main "$@"
fixture="$SMOKE_RUN_DIR/consumer"
write_case() {
  python3 - "$fixture" "$1" <<'PY'
from pathlib import Path
import sys
root, case = Path(sys.argv[1]), sys.argv[2]
base = root / "app/src/main/java/com/example/jhsmoke/excluded/Base.java"
base.parent.mkdir(parents=True, exist_ok=True)
annotation = "@io.jankhunter.annotations.JankHunterIgnore" if case == "ignored" else ""
body = "private android.view.View binding; public android.view.View existingBinding() { return binding; }"
parent = "android.app.Fragment"
if case == "harmless":
    body = "public static android.view.View unrelated;"
if case == "final":
    parent = "android.app.Service"
    body = "public android.os.IBinder onBind(android.content.Intent i) { return null; } public final void onDestroy() {}"
base.write_text(f"package com.example.jhsmoke.excluded; {annotation} public class Base extends {parent} {{ {body} }}\n")
implementation = ""
methods = ""
if case == "explicit":
    implementation = " implements io.jankhunter.runtime.JankHunterBindingAccessor"
    methods = "public void visitJankHunterBindings(io.jankhunter.runtime.JankHunterLifecycleTargetSinkV1 sink, String owner) { sink.accept(existingBinding(), owner); }"
(root / "app/src/main/java/com/example/jhsmoke/IncludedChild.java").write_text(
    f"package com.example.jhsmoke; public class IncludedChild extends com.example.jhsmoke.excluded.Base{implementation} {{ {methods} }}\n")
PY
}
reject_case() {
  local scenario="$1" expected="$2" variant log_file
  write_case "$scenario"
  for variant in Debug Release; do
    log_file="$SMOKE_RUN_DIR/exclusions-$scenario-$variant.log"
    if "$ANDROID_DIR/gradlew" -p "$fixture" ":app:assemble$variant" --configuration-cache --console=plain > "$log_file" 2>&1; then
      fail "$scenario $variant unexpectedly built"
    fi
    grep -Fq "$expected" "$log_file" || fail "Unexpected build failure; see $log_file"
    grep -Fq 'com/example/jhsmoke/excluded/Base' "$log_file" || fail "Missing ancestor diagnostic"
  done
}
reject_case ignored 'JankHunterBindingAccessor'
printf '\njankHunter { excludePackages("com.example.jhsmoke.excluded") }\n' >> "$fixture/app/build.gradle.kts"
reject_case excluded 'JankHunterBindingAccessor'
reject_case final 'requires a hook in excluded code'
for scenario in explicit harmless; do
  write_case "$scenario"
  "$ANDROID_DIR/gradlew" -p "$fixture" :app:assembleDebug :app:assembleRelease --configuration-cache --console=plain
  python3 - "$fixture" <<'PY'
from pathlib import Path
from zipfile import ZipFile
import sys
root = Path(sys.argv[1]) / "app/build/intermediates"
name = "com/example/jhsmoke/excluded/Base.class"
for variant in ("debug", "release"):
    title = variant.capitalize()
    original = root / f"javac/{variant}/compile{title}JavaWithJavac/classes" / name
    jar = root / f"classes/{variant}/ALL/instrument{title}JankHunterLifecycle/classes.jar"
    with ZipFile(jar) as output:
        assert output.read(name) == original.read_bytes(), "Excluded ancestor bytecode was modified"
PY
done
log "Lifecycle exclusions PASS: $SMOKE_RUN_DIR"
