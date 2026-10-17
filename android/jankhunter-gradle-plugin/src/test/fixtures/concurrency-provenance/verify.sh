#!/usr/bin/env bash
set -euo pipefail

fixture_dir="$(cd "$(dirname "$0")" && pwd)"
gradlew="$fixture_dir/../../../../../gradlew"
"$fixture_dir/prepare-artifacts.sh"

"$gradlew" -p "$fixture_dir" :app:assembleDebug :app:assembleRelease --offline --configuration-cache
"$gradlew" -p "$fixture_dir" :app:assembleDebug :app:assembleRelease --offline --configuration-cache

catalog="$fixture_dir/app/build/intermediates/jankhunter/debug/code-origin-v1.txt"
for name in \
    com/fixture/app/AppThreadOrigin \
    com/fixture/feature/FeatureThreadOrigin \
    com/fixture/runtimeonly/RuntimeOnlyThreadOrigin \
    com/fixture/included/IncludedThreadOrigin; do
    rg -q "^1[[:space:]]+$name$" "$catalog"
done
rg -q '^2[[:space:]]+com/fixture/app/thirdparty/ExternalThreadOrigin$' "$catalog"
rg -q '^0[[:space:]]+com/fixture/unknown/FileJarThreadOrigin$' "$catalog"

transformed="$fixture_dir/app/build/intermediates/classes/debug/ALL/instrumentDebugJankHunterCausalProvenance/classes.jar"
for class_name in com.fixture.app.AppThreadOrigin com.fixture.feature.FeatureThreadOrigin; do
    count="$(javap -c -p -classpath "$transformed" "$class_name" | rg -c 'JankHunterHooks.beginThreadStart' || true)"
    test "$count" = "1"
done
javap -v -p -classpath "$transformed" com.fixture.app.AppThreadOrigin \
    | rg -q 'com.fixture.app.AppThreadOrigin#startBoth\(\)V'
test -s "$fixture_dir/app/build/outputs/mapping/release/mapping.txt"

# THREAD_SAMPLING alone must retain legacy thread lifecycle hooks without a hidden dependency
# on CONCURRENCY_DIAGNOSTICS or EXECUTORS.
"$gradlew" -p "$fixture_dir" :app:assembleDebug --offline --configuration-cache -PthreadSamplingOnly=true
for class_name in com.fixture.app.AppThreadOrigin com.fixture.feature.FeatureThreadOrigin; do
    bytecode="$(javap -c -p -classpath "$transformed" "$class_name")"
    test "$(rg -c 'JankHunterHooks.observeThreadStart' <<<"$bytecode" || true)" = "1"
    ! rg -q 'JankHunterHooks.beginThreadStart' <<<"$bytecode"
done

# WHOLE_APPLICATION must reach the scoped causal transform, including file dependencies
# outside the app namespace. The preceding runs deliberately leave this UNKNOWN input out.
"$gradlew" -p "$fixture_dir" :app:assembleDebug --offline --configuration-cache -PwholeApplication=true
bytecode="$(javap -c -p -classpath "$transformed" com.fixture.unknown.FileJarThreadOrigin)"
test "$(rg -c 'JankHunterHooks.beginThreadStart' <<<"$bytecode" || true)" = "1"
