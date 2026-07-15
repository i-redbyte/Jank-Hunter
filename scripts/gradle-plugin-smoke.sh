#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ANDROID_DIR="$ROOT_DIR/android"
KEEP_SMOKE_DIR="${KEEP_SMOKE_DIR:-0}"
SMOKE_COMPILE_SDK="${SMOKE_COMPILE_SDK:-35}"
SMOKE_JAVA_HOME="${SMOKE_JAVA_HOME:-}"
SMOKE_AGP_VERSION="${SMOKE_AGP_VERSION:-}"
ANDROID_BUILD_TOOLS_VERSION="${ANDROID_BUILD_TOOLS_VERSION:-}"
SMOKE_CONFIGURATION_CACHE="${SMOKE_CONFIGURATION_CACHE:-1}"
SMOKE_MARKER_NAME=".jankhunter-gradle-smoke-owned"
SMOKE_MARKER_VALUE="jankhunter-gradle-smoke:v1"
SMOKE_RUN_DIR=""

usage() {
  cat <<'EOF'
Usage:
  scripts/gradle-plugin-smoke.sh

Builds and publishes Jank Hunter into an isolated Maven repository, then compiles an external
Android application/library fixture and verifies generated artifacts.

Options:
  -h, --help    Show this help.

Environment:
  SMOKE_JAVA_HOME              JDK home (Java 17 or newer).
  SMOKE_AGP_VERSION            AGP version for the consumer; defaults to android/build.gradle.kts.
  SMOKE_COMPILE_SDK            Compile/target SDK. Default: 35.
  ANDROID_BUILD_TOOLS_VERSION  Installed Build Tools version; defaults to the highest installed.
  SMOKE_CONFIGURATION_CACHE    Set to 0 to disable the create/reuse configuration-cache check.
  SMOKE_WORK_DIR               Parent for a unique cold run directory preserved for inspection.
  KEEP_SMOKE_DIR               Set to 1 to preserve an automatically created work directory.
EOF
}

log() {
  printf '[jankhunter-gradle-smoke] %s\n' "$*"
}

fail() {
  printf '[jankhunter-gradle-smoke] error: %s\n' "$*" >&2
  exit 1
}

require_boolean_environment() {
  local name="$1"
  local value="$2"
  case "$value" in
    0|1) ;;
    *) fail "$name must be 0 or 1, found: $value" ;;
  esac
}

require_single_line() {
  local name="$1"
  local value="$2"
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* ]] ||
    fail "$name must not contain line breaks"
}

validate_version_value() {
  local name="$1"
  local value="$2"
  [[ "$value" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] ||
    fail "$name contains unsupported characters: $value"
}

kotlin_string_escape() {
  local value="$1"
  require_single_line "Kotlin string value" "$value"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//\$/\\\$}"
  printf '%s' "$value"
}

properties_value_escape() {
  local value="$1"
  require_single_line "properties value" "$value"
  value="${value//\\/\\\\}"
  value="${value//:/\\:}"
  value="${value//=/\\=}"
  printf '%s' "$value"
}

resolve_java17_home() {
  if [[ -n "$SMOKE_JAVA_HOME" ]]; then
    validate_java_home "$SMOKE_JAVA_HOME" "SMOKE_JAVA_HOME"
    return 0
  fi

  if [[ -n "${JAVA_HOME:-}" ]]; then
    validate_java_home "$JAVA_HOME" "JAVA_HOME"
    return 0
  fi

  if [[ -x /usr/libexec/java_home ]]; then
    local java_home
    java_home="$(/usr/libexec/java_home -v 17 2>/dev/null || true)"
    if [[ -n "$java_home" && -x "$java_home/bin/java" ]]; then
      validate_java_home "$java_home" "/usr/libexec/java_home"
      return 0
    fi
  fi

  if command -v java >/dev/null 2>&1; then
    local java_home
    java_home="$(java -XshowSettings:properties -version 2>&1 |
      sed -nE 's/^[[:space:]]*java[.]home[[:space:]]*=[[:space:]]*(.*)$/\1/p' |
      head -n 1)"
    if [[ -n "$java_home" ]]; then
      validate_java_home "$java_home" "java from PATH"
      return 0
    fi
  fi

  fail "Java 17 or newer was not found. Set SMOKE_JAVA_HOME=/path/to/jdk."
}

validate_java_home() {
  local java_home="$1"
  local source="$2"
  require_single_line "$source Java home" "$java_home"
  [[ -x "$java_home/bin/java" ]] || fail "$source does not contain executable bin/java: $java_home"
  local version_line major
  version_line="$("$java_home/bin/java" -version 2>&1 | head -n 1)"
  major="$(printf '%s\n' "$version_line" | sed -nE 's/.*version "([0-9]+).*/\1/p')"
  [[ "$major" =~ ^[0-9]+$ ]] || fail "could not determine Java version from $source: $version_line"
  [[ "$major" -ge 17 ]] || fail "$source must point to Java 17 or newer, found: $version_line"
  (cd "$java_home" && pwd -P)
}

resolve_android_sdk_dir() {
  local candidate=""
  if [[ -n "${ANDROID_HOME:-}" ]]; then
    candidate="$ANDROID_HOME"
  elif [[ -n "${ANDROID_SDK_ROOT:-}" ]]; then
    candidate="$ANDROID_SDK_ROOT"
  elif [[ -n "${HOME:-}" && -d "$HOME/Library/Android/sdk" ]]; then
    candidate="$HOME/Library/Android/sdk"
  elif [[ -n "${HOME:-}" && -d "$HOME/Android/Sdk" ]]; then
    candidate="$HOME/Android/Sdk"
  fi

  [[ -n "$candidate" ]] || fail "Android SDK was not found. Set ANDROID_HOME."
  [[ -d "$candidate" ]] || fail "Android SDK path does not exist: $candidate"
  require_single_line "Android SDK path" "$candidate"
  (cd "$candidate" && pwd)
}

resolve_agp_version() {
  if [[ -n "$SMOKE_AGP_VERSION" ]]; then
    printf '%s\n' "$SMOKE_AGP_VERSION"
    return 0
  fi
  local version
  version="$(sed -nE 's/^[[:space:]]*id\("com[.]android[.]library"\)[[:space:]]+version[[:space:]]+"([^"]+)".*$/\1/p' \
    "$ANDROID_DIR/build.gradle.kts" | head -n 1)"
  [[ -n "$version" ]] || fail "could not resolve AGP version from $ANDROID_DIR/build.gradle.kts"
  printf '%s\n' "$version"
}

resolve_build_tools_version() {
  local sdk_dir="$1"
  local build_tools_dir="$sdk_dir/build-tools"
  [[ -d "$build_tools_dir" ]] || fail "Android Build Tools directory was not found: $build_tools_dir"

  if [[ -n "$ANDROID_BUILD_TOOLS_VERSION" ]]; then
    [[ "$ANDROID_BUILD_TOOLS_VERSION" =~ ^[0-9]+(\.[0-9]+){1,2}$ ]] ||
      fail "ANDROID_BUILD_TOOLS_VERSION must be a numeric version, found: $ANDROID_BUILD_TOOLS_VERSION"
    [[ -d "$build_tools_dir/$ANDROID_BUILD_TOOLS_VERSION" ]] ||
      fail "Android Build Tools $ANDROID_BUILD_TOOLS_VERSION was not found in $build_tools_dir"
    printf '%s\n' "$ANDROID_BUILD_TOOLS_VERSION"
    return
  fi

  local version
  version="$(find "$build_tools_dir" -maxdepth 1 -mindepth 1 -type d -exec basename {} \; 2>/dev/null |
    sed -nE '/^[0-9]+([.][0-9]+){1,2}$/p' |
    sort -t. -k1,1n -k2,2n -k3,3n |
    tail -n 1)"
  [[ -n "$version" ]] || fail "No Android Build Tools versions were found in $build_tools_dir"
  printf '%s\n' "$version"
}

properties_value() {
  local key="$1"
  local file="$2"
  awk -F= -v key="$key" '$1 == key { print $2; exit }' "$file"
}

assert_manifest_metadata() {
  local file="$1"
  local name="$2"
  local expected_value="$3"
  if ! MANIFEST_NAME="$name" MANIFEST_VALUE="$expected_value" perl -0ne '
    my $name = $ENV{"MANIFEST_NAME"};
    my $value = $ENV{"MANIFEST_VALUE"};
    while (/<meta-data\b(.*?)(?:\/\s*>|>)/sg) {
      my $attributes = $1;
      if ($attributes =~ /\bandroid:name\s*=\s*["\x27]\Q$name\E["\x27]/s &&
          $attributes =~ /\bandroid:value\s*=\s*["\x27]\Q$value\E["\x27]/s) {
        exit 0;
      }
    }
    exit 1;
  ' "$file"; then
    fail "Generated runtime manifest does not contain $name=$expected_value: $file"
  fi
}

require_file_contains() {
  local file="$1"
  local expected="$2"
  local description="$3"
  [[ -s "$file" ]] || fail "$description was not generated: $file"
  grep -Fq "$expected" "$file" || fail "$description does not contain '$expected': $file"
}

require_file_not_contains() {
  local file="$1"
  local unexpected="$2"
  local description="$3"
  [[ -s "$file" ]] || fail "$description was not generated: $file"
  if grep -Fq "$unexpected" "$file"; then
    fail "$description unexpectedly contains '$unexpected': $file"
  fi
}

cleanup_smoke_work_dir() {
  [[ -n "$SMOKE_RUN_DIR" ]] || return 0
  local marker="$SMOKE_RUN_DIR/$SMOKE_MARKER_NAME"
  if [[ ! -f "$marker" || -L "$marker" || "$(<"$marker")" != "$SMOKE_MARKER_VALUE" ]]; then
    log "refusing to clean unowned smoke directory: $SMOKE_RUN_DIR"
    return 0
  fi
  rm -rf -- "$SMOKE_RUN_DIR"
}

write_fixture() {
  local fixture_dir="$1"
  local maven_repo="$2"
  local sdk_dir="$3"
  local build_tools_version="$4"
  local group="$5"
  local version="$6"
  local maven_repo_kts sdk_dir_properties
  maven_repo_kts="$(kotlin_string_escape "$maven_repo")"
  sdk_dir_properties="$(properties_value_escape "$sdk_dir")"

  mkdir -p "$fixture_dir/app/src/main/java/com/example/jhsmoke"
  mkdir -p "$fixture_dir/feature/src/main/java/com/example/jhsmoke/feature"

  cat > "$fixture_dir/settings.gradle.kts" <<EOF
pluginManagement {
    repositories {
        maven { url = uri("$maven_repo_kts") }
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        maven { url = uri("$maven_repo_kts") }
        google()
        mavenCentral()
    }
}

rootProject.name = "JankHunterPluginSmoke"
include(":app", ":feature")
EOF

  cat > "$fixture_dir/build.gradle.kts" <<EOF
plugins {
    id("com.android.application") version "$SMOKE_AGP_VERSION" apply false
    id("com.android.library") version "$SMOKE_AGP_VERSION" apply false
    id("io.jankhunter.android") version "$version" apply false
}
EOF

  cat > "$fixture_dir/app/build.gradle.kts" <<EOF
plugins {
    id("com.android.application")
    id("io.jankhunter.android")
}

android {
    namespace = "com.example.jhsmoke"
    compileSdk = $SMOKE_COMPILE_SDK
    buildToolsVersion = "$build_tools_version"

    defaultConfig {
        applicationId = "com.example.jhsmoke"
        minSdk = 23
        targetSdk = $SMOKE_COMPILE_SDK
        versionCode = 1
        versionName = "1.0"
    }
}

jankHunter {
    enabled = true
    enabledBuildTypes.add("debug")
    autoInit = true
    dependencyInjectionAnalysis = io.jankhunter.gradle.JankHunterFeatureMode.ENABLED
    sessionLogSizeLimitEnabled = true
    maxSessionLogSizeMiB = 8
    retainedHeapDump {
        enabled = true
        privacyApproved = true
        minIntervalMs = 600_000
        maxCount = 1
        minRetainedAgeMs = 30_000
    }
    instrument {
        okhttp = true
        webSockets = true
        methodCounters = true
        handlers = true
        logSpam = true
        classGraph = true
        runtimeCallGraph = true
        asmProgressLog = false
    }
}

dependencies {
    implementation(project(":feature"))
    implementation("com.squareup.okhttp3:okhttp:3.12.13")
    debugImplementation("$group:jankhunter-okhttp3:$version")
}
EOF

  cat > "$fixture_dir/feature/build.gradle.kts" <<EOF
plugins {
    id("com.android.library")
    id("io.jankhunter.android")
}

android {
    namespace = "com.example.jhsmoke.feature"
    compileSdk = $SMOKE_COMPILE_SDK
    buildToolsVersion = "$build_tools_version"

    defaultConfig {
        minSdk = 23
    }
}

jankHunter {
    enabled = true
    enabledBuildTypes.add("debug")
    instrument {
        methodCounters = true
        handlers = true
        logSpam = true
        classGraph = true
        runtimeCallGraph = true
        asmProgressLog = false
    }
}
EOF

  cat > "$fixture_dir/app/src/main/AndroidManifest.xml" <<'EOF'
<manifest xmlns:android="http://schemas.android.com/apk/res/android">
    <application
        android:name=".SmokeApplication"
        android:theme="@style/AppTheme"
        android:label="Jank Hunter Smoke">
        <activity
            android:name=".MainActivity"
            android:exported="true">
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
        </activity>
    </application>
</manifest>
EOF

  mkdir -p "$fixture_dir/app/src/main/res/values"
  cat > "$fixture_dir/app/src/main/res/values/styles.xml" <<'EOF'
<resources>
    <style name="AppTheme" parent="android:style/Theme.Material.Light.NoActionBar" />
</resources>
EOF

  cat > "$fixture_dir/app/src/main/java/com/example/jhsmoke/SmokeApplication.java" <<'EOF'
package com.example.jhsmoke;

import android.app.Application;

public class SmokeApplication extends Application {
}
EOF

  cat > "$fixture_dir/app/src/main/java/com/example/jhsmoke/Helper.java" <<'EOF'
package com.example.jhsmoke;

public final class Helper {
    private Helper() {
    }

    public static void work() {
    }
}
EOF

  cat > "$fixture_dir/app/src/main/java/com/example/jhsmoke/DaggerSmokeComponent.java" <<'EOF'
package com.example.jhsmoke;

public final class DaggerSmokeComponent {
    private DaggerSmokeComponent() {
    }
}
EOF

  cat > "$fixture_dir/app/src/main/java/com/example/jhsmoke/MainActivity.java" <<'EOF'
package com.example.jhsmoke;

import android.app.Activity;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.util.Log;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.WebSocketListener;

public class MainActivity extends Activity {
    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        Helper.work();
        com.example.jhsmoke.feature.FeatureEntry.touch();
        new Handler(Looper.getMainLooper()).post(new Runnable() {
            @Override
            public void run() {
                Log.d("JankHunterSmoke", "ready");
            }
        });
    }

    @SuppressWarnings("unused")
    private void networkHookFixture() {
        OkHttpClient client = new OkHttpClient.Builder().build();
        Request request = new Request.Builder().url("ws://localhost/").build();
        client.newWebSocket(request, new WebSocketListener() { });
    }
}
EOF

  cat > "$fixture_dir/feature/src/main/AndroidManifest.xml" <<'EOF'
<manifest xmlns:android="http://schemas.android.com/apk/res/android" />
EOF

  cat > "$fixture_dir/feature/src/main/java/com/example/jhsmoke/feature/FeatureEntry.java" <<'EOF'
package com.example.jhsmoke.feature;

import io.jankhunter.annotations.JankHunterOwner;

@JankHunterOwner("smoke.feature")
public final class FeatureEntry {
    private FeatureEntry() {
    }

    public static void touch() {
    }
}
EOF

  cat > "$fixture_dir/feature/src/main/java/com/example/jhsmoke/feature/FeatureFragment.java" <<'EOF'
package com.example.jhsmoke.feature;

import android.app.Fragment;

@SuppressWarnings("deprecation")
public final class FeatureFragment extends Fragment {
    @Override
    public void onDestroyView() {
        super.onDestroyView();
    }
}
EOF

  cat > "$fixture_dir/local.properties" <<EOF
sdk.dir=$sdk_dir_properties
EOF
}

main() {
  if [[ $# -gt 0 ]]; then
    if [[ $# -eq 1 && ( "$1" == "-h" || "$1" == "--help" ) ]]; then
      usage
      return 0
    fi
    fail "unknown or unexpected arguments: $*"
  fi

  require_boolean_environment KEEP_SMOKE_DIR "$KEEP_SMOKE_DIR"
  require_boolean_environment SMOKE_CONFIGURATION_CACHE "$SMOKE_CONFIGURATION_CACHE"
  [[ "$SMOKE_COMPILE_SDK" =~ ^[1-9][0-9]*$ ]] ||
    fail "SMOKE_COMPILE_SDK must be a positive integer, found: $SMOKE_COMPILE_SDK"
  require_single_line "SMOKE_WORK_DIR" "${SMOKE_WORK_DIR:-}"
  require_single_line "SMOKE_JAVA_HOME" "$SMOKE_JAVA_HOME"
  if [[ -n "$SMOKE_AGP_VERSION" ]]; then
    validate_version_value SMOKE_AGP_VERSION "$SMOKE_AGP_VERSION"
  fi

  [[ -x "$ANDROID_DIR/gradlew" ]] || fail "Gradle wrapper not found: $ANDROID_DIR/gradlew"
  command -v perl >/dev/null 2>&1 || fail "required command was not found: perl"

  local java17_home
  java17_home="$(resolve_java17_home)"
  SMOKE_AGP_VERSION="$(resolve_agp_version)"
  validate_version_value SMOKE_AGP_VERSION "$SMOKE_AGP_VERSION"
  local sdk_dir
  sdk_dir="$(resolve_android_sdk_dir)"
  local build_tools_version
  build_tools_version="$(resolve_build_tools_version "$sdk_dir")"
  local platform_dir="$sdk_dir/platforms/android-$SMOKE_COMPILE_SDK"
  [[ -d "$platform_dir" ]] || fail "Android platform android-$SMOKE_COMPILE_SDK was not found: $platform_dir"

  local group version
  group="$(properties_value jankHunterGroup "$ANDROID_DIR/gradle.properties")"
  version="$(properties_value jankHunterVersion "$ANDROID_DIR/gradle.properties")"
  [[ -n "$group" ]] || fail "Could not read jankHunterGroup"
  [[ -n "$version" ]] || fail "Could not read jankHunterVersion"
  [[ "$group" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] || fail "invalid jankHunterGroup: $group"
  validate_version_value jankHunterVersion "$version"

  local work_parent="/tmp"
  local preserve_work_dir=0
  if [[ -n "${SMOKE_WORK_DIR:-}" ]]; then
    mkdir -p "$SMOKE_WORK_DIR"
    [[ -d "$SMOKE_WORK_DIR" ]] || fail "SMOKE_WORK_DIR is not a directory: $SMOKE_WORK_DIR"
    work_parent="$(cd "$SMOKE_WORK_DIR" && pwd -P)"
    preserve_work_dir=1
  fi
  local work_dir
  work_dir="$(mktemp -d "$work_parent/jankhunter-gradle-smoke.XXXXXX")"
  SMOKE_RUN_DIR="$work_dir"
  printf '%s\n' "$SMOKE_MARKER_VALUE" > "$work_dir/$SMOKE_MARKER_NAME"
  local maven_repo="$work_dir/maven"
  local fixture_dir="$work_dir/consumer"

  if [[ "$KEEP_SMOKE_DIR" == "1" || "$preserve_work_dir" == "1" ]]; then
    trap - EXIT
  else
    trap cleanup_smoke_work_dir EXIT
  fi

  log "work dir: $work_dir"
  log "Java 17: $java17_home"
  log "Android Gradle Plugin: $SMOKE_AGP_VERSION"
  log "Android SDK: $sdk_dir"
  log "Android Build Tools: $build_tools_version"

  log "publishing Jank Hunter artifacts"
  JAVA_HOME="$java17_home" ANDROID_HOME="$sdk_dir" ANDROID_SDK_ROOT="$sdk_dir" \
    "$ANDROID_DIR/gradlew" -p "$ANDROID_DIR" publishToMavenLocal \
    -PjankHunterBuildToolsVersion="$build_tools_version" \
    -Dmaven.repo.local="$maven_repo" \
    --no-daemon --console=plain --warning-mode all

  local group_path
  group_path="$(printf '%s' "$group" | tr '.' '/')"
  local plugin_module="$maven_repo/$group_path/jankhunter-gradle-plugin/$version/jankhunter-gradle-plugin-$version.module"
  local runtime_module="$maven_repo/$group_path/jankhunter-runtime/$version/jankhunter-runtime-$version.module"
  local annotations_module="$maven_repo/$group_path/jankhunter-annotations/$version/jankhunter-annotations-$version.module"
  local okhttp_module="$maven_repo/$group_path/jankhunter-okhttp3/$version/jankhunter-okhttp3-$version.module"
  [[ -f "$plugin_module" ]] || fail "Published plugin metadata was not found: $plugin_module"
  [[ -f "$runtime_module" ]] || fail "Published runtime metadata was not found: $runtime_module"
  [[ -f "$annotations_module" ]] || fail "Published annotations metadata was not found: $annotations_module"
  [[ -f "$okhttp_module" ]] || fail "Published OkHttp helper metadata was not found: $okhttp_module"
  grep -q '"org.gradle.jvm.version": 17' "$plugin_module" ||
    fail "Published Gradle plugin metadata is not Java 17-compatible: $plugin_module"

  write_fixture "$fixture_dir" "$maven_repo" "$sdk_dir" "$build_tools_version" "$group" "$version"

  log "building external Android consumer"
  local consumer_args=(
    -p "$fixture_dir"
    :app:assembleDebug
    --no-daemon
    --console=plain
    --warning-mode all
  )
  if [[ "$SMOKE_CONFIGURATION_CACHE" == "1" ]]; then
    consumer_args+=(--configuration-cache)
  fi

  local consumer_output="$work_dir/consumer-build.txt"
  if ! JAVA_HOME="$java17_home" ANDROID_HOME="$sdk_dir" ANDROID_SDK_ROOT="$sdk_dir" \
    "$ANDROID_DIR/gradlew" "${consumer_args[@]}" 2>&1 | tee "$consumer_output"; then
    fail "external Android consumer build failed; see $consumer_output"
  fi
  assert_single_banner "$consumer_output" "$version"
  if [[ "$SMOKE_CONFIGURATION_CACHE" == "1" ]]; then
    grep -Fq 'Configuration cache entry stored.' "$consumer_output" ||
      fail "external consumer did not store a cold configuration-cache entry: $consumer_output"
  fi

  if [[ "$SMOKE_CONFIGURATION_CACHE" == "1" ]]; then
    local cached_output="$work_dir/consumer-build-cached.txt"
    log "rebuilding external consumer with the configuration cache"
    if ! JAVA_HOME="$java17_home" ANDROID_HOME="$sdk_dir" ANDROID_SDK_ROOT="$sdk_dir" \
      "$ANDROID_DIR/gradlew" "${consumer_args[@]}" 2>&1 | tee "$cached_output"; then
      fail "cached external Android consumer build failed; see $cached_output"
    fi
    grep -Fq 'Configuration cache entry reused.' "$cached_output" ||
      fail "external consumer did not reuse the configuration cache: $cached_output"
    assert_single_banner "$cached_output" "$version"
  fi

  local owner_map="$fixture_dir/app/build/generated/jankhunter/debug/owner-map.json"
  require_file_contains "$owner_map" '"class":"com.example.jhsmoke.MainActivity"' "Application owner map"
  require_file_contains "$owner_map" '"methodCounters":true' "Application owner map metadata"
  require_file_contains "$owner_map" '"okhttp":true' "Application owner map metadata"
  require_file_contains "$owner_map" '"webSockets":true' "Application owner map metadata"
  require_file_contains "$owner_map" '"handlers":true' "Application owner map metadata"
  require_file_contains "$owner_map" '"logSpam":true' "Application owner map metadata"
  require_file_contains "$owner_map" '"classGraph":true' "Application owner map metadata"
  require_file_contains "$owner_map" '"runtimeCallGraph":true' "Application owner map metadata"
  local feature_owner_map="$fixture_dir/feature/build/generated/jankhunter/debug/owner-map.json"
  require_file_contains "$feature_owner_map" '"class":"com.example.jhsmoke.feature.FeatureEntry"' "Feature owner map"
  require_file_contains "$feature_owner_map" '"method":"touch"' "Feature owner map"

  local class_graph="$fixture_dir/app/build/generated/jankhunter/debug/class-graph.jsonl"
  require_file_contains "$class_graph" '"class":"com.example.jhsmoke.MainActivity"' "Application class graph"
  require_file_contains "$class_graph" '"calleeClass":"com.example.jhsmoke.Helper"' "Application class graph"
  require_file_contains "$class_graph" '"calleeClass":"com.example.jhsmoke.feature.FeatureEntry"' "Application class graph"

  local diagnostics="$fixture_dir/app/build/generated/jankhunter/debug/instrumentation-diagnostics.jsonl"
  require_file_contains "$diagnostics" '"class":"com.example.jhsmoke.MainActivity"' "Application instrumentation diagnostics"
  require_file_contains "$diagnostics" '"intent":"handler.wrap_runnable.single_runnable"' "Handler hook diagnostics"
  require_file_contains "$diagnostics" '"intent":"logspam.android.util.Log.d"' "Log hook diagnostics"
  require_file_contains "$diagnostics" '"intent":"okhttp.install_event_listener_factory"' "OkHttp hook diagnostics"
  require_file_contains "$diagnostics" '"intent":"okhttp.wrap_websocket_listener"' "WebSocket hook diagnostics"
  local feature_diagnostics="$fixture_dir/feature/build/generated/jankhunter/debug/instrumentation-diagnostics.jsonl"
  require_file_contains "$feature_diagnostics" '"owner":"smoke.feature"' "Feature annotation diagnostics"
  require_file_contains "$feature_diagnostics" '"class":"com.example.jhsmoke.feature.FeatureFragment"' "Feature lifecycle diagnostics"
  require_file_contains "$feature_diagnostics" '"intent":"lifecycle.watch_retained"' "Feature lifecycle diagnostics"
  require_file_not_contains "$diagnostics" '"class":"com.example.jhsmoke.feature.FeatureFragment"' "Application lifecycle diagnostics"

  local di_catalog="$fixture_dir/app/build/generated/jankhunter/debug/di-catalog.jsonl"
  require_file_contains "$di_catalog" '"kind":"metadata"' "Dependency injection catalog metadata"
  require_file_contains "$di_catalog" '"semantics":"build_time_di"' "Dependency injection catalog metadata"
  require_file_contains "$di_catalog" '"runtimeTracing":false' "Dependency injection catalog metadata"
  require_file_contains "$di_catalog" '"affectsScore":false' "Dependency injection catalog metadata"
  require_file_contains "$di_catalog" '"kind":"class","name":"com.example.jhsmoke.DaggerSmokeComponent","framework":"dagger2"' "Dagger dependency injection catalog"
  require_file_contains "$di_catalog" '"generated":true' "Dagger dependency injection catalog"

  local runtime_manifest="$fixture_dir/app/build/generated/manifests/generateDebugJankHunterRuntimeManifest/AndroidManifest.xml"
  [[ -s "$runtime_manifest" ]] || fail "Application runtime manifest was not generated: $runtime_manifest"
  grep -q 'io.jankhunter.runtime.JankHunterAutoInitProvider' "$runtime_manifest" ||
    fail "Generated runtime manifest does not declare JankHunterAutoInitProvider: $runtime_manifest"
  assert_manifest_metadata "$runtime_manifest" io.jankhunter.enabled true
  assert_manifest_metadata "$runtime_manifest" io.jankhunter.session_log_size_limit_enabled true
  assert_manifest_metadata "$runtime_manifest" io.jankhunter.max_session_log_size_mib 8
  assert_manifest_metadata "$runtime_manifest" io.jankhunter.retained_heap_dump_enabled true
  assert_manifest_metadata "$runtime_manifest" io.jankhunter.retained_heap_dump_privacy_approved true
  assert_manifest_metadata "$runtime_manifest" io.jankhunter.retained_heap_dump_min_interval_ms 600000
  assert_manifest_metadata "$runtime_manifest" io.jankhunter.retained_heap_dump_max_count 1
  assert_manifest_metadata "$runtime_manifest" io.jankhunter.retained_heap_dump_min_retained_age_ms 30000
  if [[ -e "$fixture_dir/feature/build/generated/manifests/generateDebugJankHunterRuntimeManifest/AndroidManifest.xml" ]]; then
    fail "Library module should not generate Jank Hunter runtime manifest"
  fi

  log "ok"
}

assert_single_banner() {
  local output="$1"
  local version="$2"
  local expected="================JANK HUNTER $version ENABLED================"
  local count
  count="$(grep -Fxc "$expected" "$output" || true)"
  [[ "$count" -eq 1 ]] || fail "expected one Jank Hunter build banner, found $count in $output"
}

main "$@"
