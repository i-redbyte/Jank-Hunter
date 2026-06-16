#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ANDROID_DIR="$ROOT_DIR/android"
KEEP_SMOKE_DIR="${KEEP_SMOKE_DIR:-0}"
SMOKE_COMPILE_SDK="${SMOKE_COMPILE_SDK:-35}"
SMOKE_JAVA_HOME="${SMOKE_JAVA_HOME:-}"
ANDROID_BUILD_TOOLS_VERSION="${ANDROID_BUILD_TOOLS_VERSION:-}"
SMOKE_CONFIGURATION_CACHE="${SMOKE_CONFIGURATION_CACHE:-1}"

log() {
  printf '[jankhunter-gradle-smoke] %s\n' "$*"
}

fail() {
  printf '[jankhunter-gradle-smoke] error: %s\n' "$*" >&2
  exit 1
}

resolve_java17_home() {
  if [[ -n "$SMOKE_JAVA_HOME" ]]; then
    [[ -x "$SMOKE_JAVA_HOME/bin/java" ]] || fail "SMOKE_JAVA_HOME does not contain bin/java: $SMOKE_JAVA_HOME"
    printf '%s\n' "$SMOKE_JAVA_HOME"
    return
  fi

  if [[ -x /usr/libexec/java_home ]]; then
    local java_home
    java_home="$(/usr/libexec/java_home -v 17 2>/dev/null || true)"
    if [[ -n "$java_home" && -x "$java_home/bin/java" ]]; then
      printf '%s\n' "$java_home"
      return
    fi
  fi

  fail "Java 17 was not found. Set SMOKE_JAVA_HOME=/path/to/jdk17."
}

resolve_android_sdk_dir() {
  local candidate=""
  if [[ -n "${ANDROID_HOME:-}" ]]; then
    candidate="$ANDROID_HOME"
  elif [[ -n "${ANDROID_SDK_ROOT:-}" ]]; then
    candidate="$ANDROID_SDK_ROOT"
  elif [[ -d "$HOME/Library/Android/sdk" ]]; then
    candidate="$HOME/Library/Android/sdk"
  fi

  [[ -n "$candidate" ]] || fail "Android SDK was not found. Set ANDROID_HOME."
  [[ -d "$candidate" ]] || fail "Android SDK path does not exist: $candidate"
  (cd "$candidate" && pwd)
}

resolve_build_tools_version() {
  local sdk_dir="$1"
  local build_tools_dir="$sdk_dir/build-tools"
  [[ -d "$build_tools_dir" ]] || fail "Android Build Tools directory was not found: $build_tools_dir"

  if [[ -n "$ANDROID_BUILD_TOOLS_VERSION" ]]; then
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

write_fixture() {
  local fixture_dir="$1"
  local maven_repo="$2"
  local sdk_dir="$3"
  local build_tools_version="$4"
  local group="$5"
  local version="$6"

  mkdir -p "$fixture_dir/app/src/main/java/com/example/jhsmoke"
  mkdir -p "$fixture_dir/feature/src/main/java/com/example/jhsmoke/feature"

  cat > "$fixture_dir/settings.gradle.kts" <<EOF
pluginManagement {
    repositories {
        maven { url = uri("$maven_repo") }
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        maven { url = uri("$maven_repo") }
        google()
        mavenCentral()
    }
}

rootProject.name = "JankHunterPluginSmoke"
include(":app", ":feature")
EOF

  cat > "$fixture_dir/build.gradle.kts" <<EOF
plugins {
    id("com.android.application") version "9.0.1" apply false
    id("com.android.library") version "9.0.1" apply false
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
    enabledBuildTypes.add("debug")
    retainedHeapDump {
        enabled = true
        minIntervalMs = 600_000
        maxCount = 1
        minRetainedAgeMs = 30_000
    }
    instrument {
        includeWholeApplication = true
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
    debugImplementation("$group:jankhunter-runtime:$version")
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
    enabledBuildTypes.add("debug")
    retainedHeapDump {
        enabled = true
        minIntervalMs = 600_000
        maxCount = 1
        minRetainedAgeMs = 30_000
    }
    instrument {
        includeWholeApplication = true
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

  cat > "$fixture_dir/app/src/main/java/com/example/jhsmoke/MainActivity.java" <<'EOF'
package com.example.jhsmoke;

import android.app.Activity;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.util.Log;

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
}
EOF

  cat > "$fixture_dir/feature/src/main/AndroidManifest.xml" <<'EOF'
<manifest xmlns:android="http://schemas.android.com/apk/res/android" />
EOF

  cat > "$fixture_dir/feature/src/main/java/com/example/jhsmoke/feature/FeatureEntry.java" <<'EOF'
package com.example.jhsmoke.feature;

public final class FeatureEntry {
    private FeatureEntry() {
    }

    public static void touch() {
    }
}
EOF

  cat > "$fixture_dir/local.properties" <<EOF
sdk.dir=$sdk_dir
EOF
}

main() {
  [[ -x "$ANDROID_DIR/gradlew" ]] || fail "Gradle wrapper not found: $ANDROID_DIR/gradlew"

  local java17_home
  java17_home="$(resolve_java17_home)"
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

  local work_dir
  work_dir="${SMOKE_WORK_DIR:-$(mktemp -d /tmp/jankhunter-gradle-smoke.XXXXXX)}"
  local maven_repo="$work_dir/maven"
  local fixture_dir="$work_dir/consumer"

  if [[ "$KEEP_SMOKE_DIR" != "1" && -z "${SMOKE_WORK_DIR:-}" ]]; then
    trap "rm -rf '$work_dir'" EXIT
  fi

  log "work dir: $work_dir"
  log "Java 17: $java17_home"
  log "Android SDK: $sdk_dir"
  log "Android Build Tools: $build_tools_version"

  log "publishing Jank Hunter artifacts"
  JAVA_HOME="$java17_home" ANDROID_HOME="$sdk_dir" ANDROID_SDK_ROOT="$sdk_dir" \
    "$ANDROID_DIR/gradlew" -p "$ANDROID_DIR" publishToMavenLocal \
    -PjankHunterBuildToolsVersion="$build_tools_version" \
    -Dmaven.repo.local="$maven_repo" \
    --no-daemon --warning-mode all

  local group_path
  group_path="$(printf '%s' "$group" | tr '.' '/')"
  local plugin_module="$maven_repo/$group_path/jankhunter-gradle-plugin/$version/jankhunter-gradle-plugin-$version.module"
  [[ -f "$plugin_module" ]] || fail "Published plugin metadata was not found: $plugin_module"
  grep -q '"org.gradle.jvm.version": 17' "$plugin_module" ||
    fail "Published Gradle plugin metadata is not Java 17-compatible: $plugin_module"

  write_fixture "$fixture_dir" "$maven_repo" "$sdk_dir" "$build_tools_version" "$group" "$version"

  log "building external Android consumer"
  local consumer_args=(
    -p "$fixture_dir"
    :app:assembleDebug
    --no-daemon
    --warning-mode all
  )
  if [[ "$SMOKE_CONFIGURATION_CACHE" == "1" ]]; then
    consumer_args+=(--configuration-cache)
  fi

  JAVA_HOME="$java17_home" ANDROID_HOME="$sdk_dir" ANDROID_SDK_ROOT="$sdk_dir" \
    "$ANDROID_DIR/gradlew" "${consumer_args[@]}"

  local owner_map="$fixture_dir/app/build/generated/jankhunter/debug/owner-map.json"
  [[ -s "$owner_map" ]] || fail "Owner map was not generated: $owner_map"
  local feature_owner_map="$fixture_dir/feature/build/generated/jankhunter/debug/owner-map.json"
  [[ -s "$feature_owner_map" ]] || fail "Feature owner map was not generated: $feature_owner_map"
  if [[ -e "$fixture_dir/feature/build/generated/jankhunterRuntimeManifest/debug/AndroidManifest.xml" ]]; then
    fail "Library module should not generate Jank Hunter runtime manifest"
  fi

  log "ok"
}

main "$@"
