#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/gradle-plugin-smoke.sh"
KEEP_SMOKE_DIR=1
export GRADLE_OPTS="${GRADLE_OPTS:--Dorg.gradle.jvmargs=-Xmx2g\ -XX:MaxMetaspaceSize=768m}"
main "$@"

fixture_dir="$SMOKE_RUN_DIR/consumer"
# Optional ABI matrix: publish an older plugin with distinct coordinates, keeping the new SDK.
# Pass an isolated source checkout containing android/jankhunter-gradle-plugin.
if [[ -n "${SMOKE_LEGACY_PLUGIN_DIR:-}" ]]; then
  runtime_version="$(properties_value jankHunterVersion "$ANDROID_DIR/gradle.properties")"
  legacy_version="$runtime_version-legacy-abi-fixture"
  "$ANDROID_DIR/gradlew" -p "$SMOKE_LEGACY_PLUGIN_DIR/android/jankhunter-gradle-plugin" publishToMavenLocal \
    -PjankHunterVersion="$legacy_version" -Dmaven.repo.local="$SMOKE_RUN_DIR/maven" --console=plain
  python3 - "$fixture_dir/build.gradle.kts" "$legacy_version" "$runtime_version" <<'PY_LEGACY'
from pathlib import Path
import re
import sys
p = Path(sys.argv[1])
s, count = re.subn(r'(id\("io\.jankhunter\.android"\) version ")[^"]+(" apply false)',
                   lambda m: m[1] + sys.argv[2] + m[2], p.read_text())
assert count == 1
# The old plugin injects its own version of SDK dependencies; deliberately pair it with the new SDK.
s += """
subprojects {
    configurations.configureEach {
        resolutionStrategy.eachDependency {
            if (requested.group == "io.jankhunter" && requested.version == "%s") {
                useVersion("%s")
            }
        }
    }
}
""" % (sys.argv[2], sys.argv[3])
p.write_text(s)
PY_LEGACY
fi
sdk_dir="$(resolve_android_sdk_dir)"
adb="$sdk_dir/platform-tools/adb"
adb_command=("$adb")
if [[ -n "${ANDROID_SERIAL:-}" ]]; then adb_command+=(-s "$ANDROID_SERIAL"); fi
"${adb_command[@]}" get-state
mkdir -p "$fixture_dir/app/src/main/kotlin/com/example/jhsmoke" "$fixture_dir/app/src/main/res/layout"
cp "$SCRIPT_DIR/fixtures/r8-lifecycle/LifecycleExecutionProbe.kt" "$fixture_dir/app/src/main/kotlin/com/example/jhsmoke/"
cp "$SCRIPT_DIR/fixtures/r8-lifecycle/LegacyLifecycleCaller.java" "$fixture_dir/app/src/main/java/com/example/jhsmoke/"
mkdir -p "$fixture_dir/feature/src/main/kotlin/com/example/jhsmoke/feature" "$fixture_dir/feature/src/main/res/layout"
cp "$SCRIPT_DIR/fixtures/r8-lifecycle/LibraryBindingFragment.kt" "$fixture_dir/feature/src/main/kotlin/com/example/jhsmoke/feature/"
cat > "$fixture_dir/app/src/main/res/layout/probe.xml" <<'XML'
<TextView xmlns:android="http://schemas.android.com/apk/res/android"
    android:layout_width="match_parent" android:layout_height="wrap_content" android:text="Lifecycle probe" />
XML
cp "$fixture_dir/app/src/main/res/layout/probe.xml" "$fixture_dir/feature/src/main/res/layout/feature_probe.xml"
printf '\nandroid.buildFeatures.viewBinding = true\n' >> "$fixture_dir/feature/build.gradle.kts"
python3 - "$fixture_dir" <<'PY'
import sys
from pathlib import Path
root = Path(sys.argv[1])
manifest = root / "app/src/main/AndroidManifest.xml"
manifest.write_text(manifest.read_text().replace("</application>", '''
        <activity android:name=".LifecycleExecutionProbe$LifecycleActivity" android:exported="false" />
        <service android:name=".LifecycleExecutionProbe$LifecycleService" android:exported="false" />
    </application>'''))
activity = root / "app/src/main/java/com/example/jhsmoke/MainActivity.java"
activity.write_text(activity.read_text().replace("extends Activity", "extends androidx.fragment.app.FragmentActivity").replace(
    "        Helper.work();",
    "        getWindow().getDecorView().post(() -> LifecycleExecutionProbe.run(this));\n        Helper.work();"))
with (root / "app/build.gradle.kts").open("a") as output:
    output.write('''
dependencies {
    implementation("androidx.fragment:fragment:1.8.9")
    implementation("androidx.lifecycle:lifecycle-viewmodel:2.9.4")
}
android.buildFeatures.viewBinding = true
android.buildTypes.getByName("release").signingConfig = android.signingConfigs.getByName("debug")
android.buildTypes.getByName("release").isShrinkResources = true
''')
PY
"$ANDROID_DIR/gradlew" -p "$fixture_dir" :app:assembleDebug :app:assembleRelease :app:bundleRelease --configuration-cache --console=plain --stacktrace

python3 - "$fixture_dir/app/build/outputs/mapping/release/mapping.txt" <<'PY_MAPPING'
from pathlib import Path
import re
import sys
mapping = dict(re.findall(r"^([^\s].*) -> (.*):$", Path(sys.argv[1]).read_text(), re.M))
for framework in ("androidx.fragment.app.Fragment", "androidx.lifecycle.ViewModel", "androidx.viewbinding.ViewBinding"):
    assert mapping.get(framework) == framework, "Consumer reflection rule missing: " + framework
for suffix in ("LegacyFragment", "LegacyModel"):
    application = "com.example.jhsmoke.LifecycleExecutionProbe$" + suffix
    assert application in mapping and mapping[application] != application, "Legacy application fixture was not obfuscated"
PY_MAPPING

for variant in debug release; do
  if [[ -z "${SMOKE_LEGACY_PLUGIN_DIR:-}" ]]; then
  python3 - "$fixture_dir" "$variant" <<'PY_IDENTITY'
import hashlib
import json
import sys
from pathlib import Path
from zipfile import ZipFile
root, variant = Path(sys.argv[1]), sys.argv[2]
with ZipFile(root / f"app/build/outputs/apk/{variant}/app-{variant}.apk") as apk:
    identity = apk.read("assets/jankhunter/build-identity-v1.txt")
assert len(identity) <= 512
fields = dict(line.split("=", 1) for line in identity.decode("ascii").splitlines())
assert set(fields) == {"schema", "state", "mapping-sha256", "symbol-namespace"}
assert fields["schema"] == "1"
metadata = json.loads((root / f"app/build/generated/jankhunter/{variant}/artifact-metadata.json").read_text())
assert fields["symbol-namespace"] == metadata["symbolNamespace"]
for kind in (("apk", "aab") if variant == "release" else ("apk",)):
    manifest = json.loads((root / f"app/build/generated/jankhunter/{variant}/build-manifest-{kind}.json").read_text())
    assert manifest["format"] == 1 and manifest["kind"] == "build-manifest"
    assert manifest["mappingSha256"] == fields["mapping-sha256"]
    assert manifest["symbolNamespace"] == fields["symbol-namespace"]
    assert manifest["identityAssetSha256"] == hashlib.sha256(identity).hexdigest()
    expected_artifacts = {"artifact-metadata.json", "class-graph.jsonl", "lambda-captures.jsonl", "instrumentation-diagnostics.jsonl", "android-components-catalog.jsonl", "di-catalog.jsonl"}
    assert {item["file"] for item in manifest["artifacts"]} == expected_artifacts
    for item in manifest["artifacts"]:
        artifact = root / f"app/build/generated/jankhunter/{variant}" / item["file"]
        assert hashlib.sha256(artifact.read_bytes()).hexdigest() == item["sha256"]
    assert len(manifest["packages"]) == 1
    package = root / (f"app/build/outputs/apk/{variant}/app-{variant}.apk" if kind == "apk" else "app/build/outputs/bundle/release/app-release.aab")
    assert manifest["packages"][0] == {"file": package.name, "sha256": hashlib.sha256(package.read_bytes()).hexdigest()}

if variant == "release":
    assert fields["state"] == "mapped"
    assert fields["mapping-sha256"] == hashlib.sha256((root / "app/build/outputs/mapping/release/mapping.txt").read_bytes()).hexdigest()
    with ZipFile(root / "app/build/outputs/bundle/release/app-release.aab") as bundle:
        assert bundle.read("base/assets/jankhunter/build-identity-v1.txt") == identity
else:
    assert fields["state"] == "unminified" and fields["mapping-sha256"] == ""
PY_IDENTITY
  fi
  "${adb_command[@]}" install -r "$fixture_dir/app/build/outputs/apk/$variant/app-$variant.apk"
  "${adb_command[@]}" shell am start -S -W -n com.example.jhsmoke/.MainActivity
  pid="$("${adb_command[@]}" shell pidof com.example.jhsmoke | tr -d '\r')"
  [[ "$pid" =~ ^[0-9]+$ ]] || fail "Lifecycle probe process did not start"
  output="$SMOKE_RUN_DIR/lifecycle-$variant.log"
  "${adb_command[@]}" logcat --pid="$pid" > "$output" &
  capture_pid=$!
  outcome=1
  for attempt in {1..25}; do
    if grep -Fq 'EXECUTION PASS' "$output"; then outcome=0; break; fi
    if grep -Eq 'FATAL EXCEPTION|EXECUTION FAIL' "$output"; then break; fi
    sleep 1
  done
  kill "$capture_pid" 2>/dev/null || true
  wait "$capture_pid" 2>/dev/null || true
  [[ "$outcome" == 0 ]] || fail "Lifecycle $variant probe failed: $output"
  archive_path="$(sed -n 's/.*EXECUTION PASS .* archive=//p' "$output" | tail -n 1 | tr -d '\r')"
  [[ "$archive_path" == /storage/emulated/0/Android/data/com.example.jhsmoke/files/lifecycle-execution-*.zip ]] || fail "Unexpected archive path"
  "${adb_command[@]}" pull "$archive_path" "$SMOKE_RUN_DIR/lifecycle-$variant.zip"
  python3 - "$SMOKE_RUN_DIR" "$variant" <<'PY'
from pathlib import Path
from zipfile import ZipFile
import sys
root, variant = Path(sys.argv[1]), sys.argv[2]
logs = root / f"lifecycle-{variant}-logs"
logs.mkdir()
with ZipFile(root / f"lifecycle-{variant}.zip") as archive:
    for entry in archive.infolist():
        assert Path(entry.filename).name == entry.filename and entry.filename.endswith(".jhlog")
        (logs / entry.filename).write_bytes(archive.read(entry))
PY
done
go -C "$ROOT_DIR/cli" build -o "$SMOKE_RUN_DIR/jankhunter" ./cmd/jankhunter
for variant in debug release; do
  "$SMOKE_RUN_DIR/jankhunter" export "$SMOKE_RUN_DIR/lifecycle-$variant-logs/"*.jhlog --out "$SMOKE_RUN_DIR/lifecycle-$variant-events.jsonl"
  if [[ -n "${SMOKE_LEGACY_PLUGIN_DIR:-}" ]]; then
    "$SMOKE_RUN_DIR/jankhunter" inspect "$SMOKE_RUN_DIR/lifecycle-$variant-logs/"*.jhlog --json > "$SMOKE_RUN_DIR/lifecycle-$variant-summary.json"
    python3 "$SCRIPT_DIR/validate-legacy-lifecycle.py" "$SMOKE_RUN_DIR/lifecycle-$variant-events.jsonl" "$SMOKE_RUN_DIR/lifecycle-$variant-summary.json"
  else
    python3 "$SCRIPT_DIR/validate-r8-lifecycle.py" "$SMOKE_RUN_DIR/lifecycle-$variant-events.jsonl" "$SMOKE_RUN_DIR/lifecycle-$variant.log"
  fi
done
if [[ -n "${SMOKE_LEGACY_PLUGIN_DIR:-}" ]]; then
  log "Legacy lifecycle ABI with explicit partial coverage PASS: $SMOKE_RUN_DIR"
else
  log "Lifecycle automatic registration PASS: $SMOKE_RUN_DIR"
fi
