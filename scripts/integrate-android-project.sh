#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -d "$PWD/cli" && -f "$PWD/android/gradlew" ]]; then
  DEFAULT_JANKHUNTER_ROOT="$PWD"
else
  DEFAULT_JANKHUNTER_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
fi

JANKHUNTER_ROOT="$DEFAULT_JANKHUNTER_ROOT"
TARGET_ROOT=""
MAVEN_DIR=".jankhunter/maven"
AAR_DIR=".jankhunter/lib"
CLI_DIR=".jankhunter/bin"
ANDROID_SDK_DIR=""
RESOLVED_ANDROID_SDK_DIR=""
ANDROID_BUILD_TOOLS_VERSION=""
RESOLVED_ANDROID_BUILD_TOOLS_VERSION=""
DRY_RUN=0
USE_AAR=0
SKIP_PUBLISH=0
SKIP_CLI_BUILD=0
SKIP_LOCAL_PROPERTIES=0
VERIFY=0
ADD_GITIGNORE=1
INCLUDE_WHOLE_APPLICATION=0
RUNTIME_CALL_GRAPH=0
ASM_PROGRESS_LOG=1

MODULES=()
INCLUDE_PACKAGES=()
EXCLUDE_PACKAGES=()
BUILD_TYPES=("debug")

usage() {
  cat <<'EOF'
Usage:
  scripts/integrate-android-project.sh /path/to/android/project
  scripts/integrate-android-project.sh --target /path/to/android/project [options]

Required:
  PATH or --target PATH         Root of the target Android project.

Common options:
  --jankhunter PATH             Path to Jank Hunter clone. Defaults to current directory when it
                                contains cli/ and android/, otherwise to this script's repository.
  --module :app                 Android module to patch. Can be repeated. If omitted, the script
                                ranks application modules and prefers the real launchable app.
  --include-package com.myapp   ASM include package. Can be repeated.
  --include-packages a,b,c      Comma-separated ASM include packages.
  --exclude-package com.myapp.generated
                                ASM exclude package. Can be repeated.
  --exclude-packages a,b,c      Comma-separated ASM exclude packages.
  --include-whole-application   Use the target module namespace as an include package.
  --runtime-call-graph          Enable runtime caller -> callee graph hooks.
  --build-type debug            Enabled build type. Can be repeated or comma-separated.
  --maven-dir PATH              Local Maven repo inside target project. Default: .jankhunter/maven.
  --use-aar, --useAar           Copy Jank Hunter runtime/OkHttp AARs, annotations JAR and Gradle
                                plugin JAR into the target project instead of adding a local Maven
                                dependency repository. Default artifact dir: .jankhunter/lib.
  --aar-dir PATH                Target artifact directory for --use-aar. Default: .jankhunter/lib.
  --cli-dir PATH                Target directory for CLI binary. Default: .jankhunter/bin.
  --android-sdk PATH            Android SDK path for target local.properties. If omitted, the
                                script uses ANDROID_HOME, ANDROID_SDK_ROOT, or ~/Library/Android/sdk.
  --android-build-tools VERSION Android Build Tools version to use while publishing Jank Hunter.
                                If omitted, the highest installed SDK build-tools version is used.
  --verify                      Run target Gradle task resolution after patching.
  --dry-run                     Print what would be changed without writing files.

Advanced:
  --skip-publish                Do not publish/copy Jank Hunter Android artifacts.
  --skip-cli-build              Do not build/copy the jankhunter CLI binary.
  --skip-local-properties       Do not create or update target local.properties. Gradle still gets
                                the resolved SDK path through ANDROID_HOME during publishing.
  --no-asm-progress-log         Disable one-line ASM progress log in generated config.
  --no-gitignore                Do not update target .gitignore.

Example:
  scripts/integrate-android-project.sh \
    --target ~/work/MyApp \
    --module :app \
    --include-package com.myapp.feature \
    --include-package com.myapp.data \
    --exclude-packages com.myapp.generated,com.myapp.di \
    --runtime-call-graph \
    --verify

Minimal:
  cd /path/to/Jank-Hunter
  scripts/integrate-android-project.sh ~/work/MyApp
EOF
}

log() {
  printf '[jankhunter-integrate] %s\n' "$*"
}

fail() {
  printf '[jankhunter-integrate] error: %s\n' "$*" >&2
  exit 1
}

split_csv_into() {
  local raw="$1"
  local target_name="$2"
  local old_ifs="$IFS"
  IFS=','
  read -r -a parts <<< "$raw"
  IFS="$old_ifs"
  local part
  for part in "${parts[@]}"; do
    part="$(trim "$part")"
    [[ -n "$part" ]] || continue
    eval "$target_name+=(\"\$part\")"
  done
}

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target)
      TARGET_ROOT="${2:-}"
      shift 2
      ;;
    --jankhunter)
      JANKHUNTER_ROOT="${2:-}"
      shift 2
      ;;
    --module|--app-module)
      MODULES+=("${2:-}")
      shift 2
      ;;
    --include-package|--include)
      INCLUDE_PACKAGES+=("${2:-}")
      shift 2
      ;;
    --include-packages|--includes)
      split_csv_into "${2:-}" INCLUDE_PACKAGES
      shift 2
      ;;
    --exclude-package|--exclude)
      EXCLUDE_PACKAGES+=("${2:-}")
      shift 2
      ;;
    --exclude-packages|--excludes)
      split_csv_into "${2:-}" EXCLUDE_PACKAGES
      shift 2
      ;;
    --include-whole-application)
      INCLUDE_WHOLE_APPLICATION=1
      shift
      ;;
    --runtime-call-graph)
      RUNTIME_CALL_GRAPH=1
      shift
      ;;
    --no-runtime-call-graph)
      RUNTIME_CALL_GRAPH=0
      shift
      ;;
    --build-type)
      BUILD_TYPES=()
      split_csv_into "${2:-}" BUILD_TYPES
      shift 2
      ;;
    --maven-dir)
      MAVEN_DIR="${2:-}"
      shift 2
      ;;
    --use-aar|--useAar)
      USE_AAR=1
      shift
      ;;
    --aar-dir)
      AAR_DIR="${2:-}"
      shift 2
      ;;
    --cli-dir)
      CLI_DIR="${2:-}"
      shift 2
      ;;
    --android-sdk|--android-sdk-dir)
      ANDROID_SDK_DIR="${2:-}"
      shift 2
      ;;
    --android-build-tools|--android-build-tools-version)
      ANDROID_BUILD_TOOLS_VERSION="${2:-}"
      shift 2
      ;;
    --verify)
      VERIFY=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --skip-publish|--no-build)
      SKIP_PUBLISH=1
      shift
      ;;
    --skip-cli-build)
      SKIP_CLI_BUILD=1
      shift
      ;;
    --skip-local-properties)
      SKIP_LOCAL_PROPERTIES=1
      shift
      ;;
    --no-asm-progress-log)
      ASM_PROGRESS_LOG=0
      shift
      ;;
    --no-gitignore)
      ADD_GITIGNORE=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      while [[ $# -gt 0 ]]; do
        if [[ -z "$TARGET_ROOT" ]]; then
          TARGET_ROOT="$1"
        else
          fail "unexpected positional argument: $1"
        fi
        shift
      done
      ;;
    -*)
      fail "unknown argument: $1"
      ;;
    *)
      if [[ -z "$TARGET_ROOT" ]]; then
        TARGET_ROOT="$1"
      else
        fail "unexpected positional argument: $1"
      fi
      shift
      ;;
  esac
done

[[ -n "$TARGET_ROOT" ]] || { usage >&2; fail "--target is required"; }

JANKHUNTER_ROOT="$(cd "$JANKHUNTER_ROOT" && pwd)"
TARGET_ROOT="$(cd "$TARGET_ROOT" && pwd)"

[[ -f "$JANKHUNTER_ROOT/android/gradlew" ]] || fail "Jank Hunter Android Gradle wrapper not found: $JANKHUNTER_ROOT/android/gradlew"
[[ -f "$JANKHUNTER_ROOT/cli/Makefile" ]] || fail "Jank Hunter CLI Makefile not found: $JANKHUNTER_ROOT/cli/Makefile"
[[ -d "$TARGET_ROOT" ]] || fail "target project does not exist: $TARGET_ROOT"
[[ -n "$MAVEN_DIR" ]] || fail "--maven-dir cannot be empty"
[[ -n "$AAR_DIR" ]] || fail "--aar-dir cannot be empty"
[[ -n "$CLI_DIR" ]] || fail "--cli-dir cannot be empty"

VERSION="$(awk -F= '$1 == "jankHunterVersion" { print $2; exit }' "$JANKHUNTER_ROOT/android/gradle.properties")"
GROUP="$(awk -F= '$1 == "jankHunterGroup" { print $2; exit }' "$JANKHUNTER_ROOT/android/gradle.properties")"
[[ -n "$VERSION" ]] || fail "could not read jankHunterVersion from android/gradle.properties"
[[ -n "$GROUP" ]] || fail "could not read jankHunterGroup from android/gradle.properties"

target_abs_path() {
  local path="$1"
  if [[ "$path" == /* ]]; then
    printf '%s\n' "$path"
  else
    printf '%s\n' "$TARGET_ROOT/$path"
  fi
}

MAVEN_REPO_ABS="$(target_abs_path "$MAVEN_DIR")"
AAR_DIR_ABS="$(target_abs_path "$AAR_DIR")"
CLI_DIR_ABS="$(target_abs_path "$CLI_DIR")"
BACKUP_ROOT="$TARGET_ROOT/.jankhunter-backups/$(date +%Y%m%d-%H%M%S)"

run_cmd() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '[dry-run] %q' "$1"
    shift
    local arg
    for arg in "$@"; do
      printf ' %q' "$arg"
    done
    printf '\n'
  else
    "$@"
  fi
}

write_file() {
  local file="$1"
  local content="$2"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would write $file"
    return
  fi
  printf '%s' "$content" > "$file"
}

backup_file() {
  local file="$1"
  [[ -f "$file" ]] || return 0
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would back up $file"
    return
  fi
  local rel="${file#$TARGET_ROOT/}"
  local dest="$BACKUP_ROOT/$rel"
  mkdir -p "$(dirname "$dest")"
  cp "$file" "$dest"
}

detect_settings_file() {
  if [[ -f "$TARGET_ROOT/settings.gradle.kts" ]]; then
    printf '%s\n' "$TARGET_ROOT/settings.gradle.kts"
  elif [[ -f "$TARGET_ROOT/settings.gradle" ]]; then
    printf '%s\n' "$TARGET_ROOT/settings.gradle"
  else
    fail "settings.gradle.kts/settings.gradle not found in target root"
  fi
}

module_to_dir() {
  local module="$1"
  module="${module#:}"
  module="${module//:/\/}"
  if [[ -z "$module" ]]; then
    printf '%s\n' "$TARGET_ROOT"
  else
    printf '%s\n' "$TARGET_ROOT/$module"
  fi
}

module_build_file() {
  local module="$1"
  local dir
  dir="$(module_to_dir "$module")"
  if [[ -f "$dir/build.gradle.kts" ]]; then
    printf '%s\n' "$dir/build.gradle.kts"
  elif [[ -f "$dir/build.gradle" ]]; then
    printf '%s\n' "$dir/build.gradle"
  else
    fail "build.gradle.kts/build.gradle not found for module $module at $dir"
  fi
}

detect_app_module() {
  local file rel dir module total=0
  local candidates=""
  while IFS= read -r file; do
    rel="${file#$TARGET_ROOT/}"
    dir="$(dirname "$rel")"
    if [[ "$dir" == "." ]]; then
      module=":"
    else
      module=":${dir//\//:}"
    fi
    if is_android_application_candidate "$module" "$file"; then
      total=$((total + 1))
      candidates+="$(score_app_module "$module" "$file")"$'\n'
    fi
  done < <(
    find "$TARGET_ROOT" \
      \( -path "$TARGET_ROOT/.gradle" -o -path "$TARGET_ROOT/build" -o -path "$TARGET_ROOT/.jankhunter" -o -path "$TARGET_ROOT/.jankhunter-backups" \) -prune \
      -o \( -name build.gradle.kts -o -name build.gradle \) -type f -print | sort
  )

  [[ "$total" -gt 0 ]] || fail "could not detect Android application module. Pass --module :app"

  local ranked top_line selected score rest reason
  ranked="$(printf '%s' "$candidates" | sed '/^[[:space:]]*$/d' | sort -t '|' -k1,1nr -k2,2)"
  top_line="$(printf '%s\n' "$ranked" | head -n 1)"
  score="${top_line%%|*}"
  rest="${top_line#*|}"
  selected="${rest%%|*}"
  reason="${rest#*|}"

  if [[ "$total" -gt 1 ]]; then
    printf '[jankhunter-integrate] application module candidates ranked by auto-detection:\n' >&2
    while IFS='|' read -r candidate_score candidate_module candidate_reason; do
      [[ -n "$candidate_module" ]] || continue
      printf '[jankhunter-integrate]   %s score=%s %s\n' "$candidate_module" "$candidate_score" "$candidate_reason" >&2
    done <<< "$ranked"
  fi
  if is_suspicious_app_module "$selected"; then
    printf '[jankhunter-integrate] warning: selected module looks test-like: %s\n' "$selected" >&2
  fi
  printf '[jankhunter-integrate] selected Android application module: %s score=%s %s\n' "$selected" "$score" "$reason" >&2
  printf '%s\n' "$selected"
}

is_android_application_candidate() {
  local module="$1"
  local build_file="$2"
  local module_dir
  module_dir="$(dirname "$build_file")"
  if has_android_application_plugin_marker "$build_file"; then
    if grep -Eq 'android[[:space:]]*\{' "$build_file" || ! grep -Eq 'apply[[:space:]]+false' "$build_file"; then
      return 0
    fi
  fi
  if grep -Eq 'applicationId[[:space:]]*(=|[[:space:]])' "$build_file"; then
    return 0
  fi
  if module_has_launcher_manifest "$module_dir"; then
    return 0
  fi
  if module_manifest_references_application "$module_dir"; then
    return 0
  fi
  if module_matches_project_name "$module" && grep -Eq 'android[[:space:]]*\{' "$build_file"; then
    return 0
  fi
  if module_matches_project_name "$module" && module_has_application_class "$module_dir"; then
    return 0
  fi
  if module_name_is_app_like "$module" && grep -Eq 'android[[:space:]]*\{' "$build_file"; then
    return 0
  fi
  if module_name_is_app_like "$module" && module_has_application_class "$module_dir"; then
    return 0
  fi
  return 1
}

module_matches_project_name() {
  local module="$1"
  local base
  base="${module##*:}"
  local normalized_base
  normalized_base="$(normalize_name "$base")"
  [[ -n "$normalized_base" ]] || return 1

  local hint
  while IFS= read -r hint; do
    hint="$(normalize_name "$hint")"
    [[ -n "$hint" ]] || continue
    if [[ "$normalized_base" == "$hint" ]]; then
      return 0
    fi
  done < <(project_name_hints)
  return 1
}

project_name_hints() {
  basename "$TARGET_ROOT"
  local settings=""
  if [[ -f "$TARGET_ROOT/settings.gradle.kts" ]]; then
    settings="$TARGET_ROOT/settings.gradle.kts"
  elif [[ -f "$TARGET_ROOT/settings.gradle" ]]; then
    settings="$TARGET_ROOT/settings.gradle"
  fi
  [[ -n "$settings" ]] || return 0
  sed -nE "s/^[[:space:]]*rootProject[.]name[[:space:]]*=[[:space:]]*['\"]([^'\"]+)['\"].*$/\1/p" "$settings" | head -n 1
}

normalize_name() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]//g'
}

module_name_is_app_like() {
  local module="$1"
  local lower base
  lower="$(printf '%s' "$module" | tr '[:upper:]' '[:lower:]')"
  base="${lower##*:}"
  case "$lower" in
    :app|*:app|*:application|*:mobile|*:main|*:prod|*:production|*:client|*:shell|*:android|*:phone|*app)
      return 0
      ;;
  esac
  case "$base" in
    app|application|mobile|main|prod|production|client|shell|android|phone|*-app|android-*)
      return 0
      ;;
  esac
  return 1
}

has_android_application_plugin_marker() {
  local build_file="$1"
  if grep -Eq 'com\.android\.application|android[._-]?application|androidApplication|android_application' "$build_file"; then
    return 0
  fi
  return 1
}

score_app_module() {
  local module="$1"
  local build_file="$2"
  local module_dir
  module_dir="$(dirname "$build_file")"
  local base="${module##*:}"
  local lower
  lower="$(printf '%s' "$module" | tr '[:upper:]' '[:lower:]')"
  local score=0
  local reason=""

  if module_has_launcher_manifest "$module_dir"; then
    score=$((score + 140))
    reason+=", launcher"
  fi
  if module_manifest_references_application "$module_dir"; then
    score=$((score + 65))
    reason+=", manifest application"
  fi
  if module_has_application_class "$module_dir"; then
    score=$((score + 60))
    reason+=", Application subclass"
  fi
  if grep -Eq 'com\.android\.application' "$build_file"; then
    score=$((score + 45))
    reason+=", android application plugin"
  elif has_android_application_plugin_marker "$build_file"; then
    score=$((score + 30))
    reason+=", android application alias"
  fi
  if grep -Eq 'applicationId[[:space:]]*(=|[[:space:]])' "$build_file"; then
    score=$((score + 55))
    reason+=", applicationId"
  fi
  if module_matches_project_name "$module"; then
    score=$((score + 85))
    reason+=", project name"
  fi
  if [[ "$module" == ":app" || "$base" == "app" ]]; then
    score=$((score + 90))
    reason+=", app name"
  elif module_name_is_app_like "$module"; then
    score=$((score + 35))
    reason+=", app-like name"
  else
    score=$((score + 10))
    reason+=", regular app module"
  fi
  if find "$module_dir" -maxdepth 3 -name google-services.json -type f -print -quit | grep -q .; then
    score=$((score + 20))
    reason+=", google-services"
  fi
  if grep -Eq 'release[[:space:]]*\{' "$build_file"; then
    score=$((score + 10))
    reason+=", release buildType"
  fi
  if is_suspicious_app_module "$module"; then
    score=$((score - 180))
    reason+=", test/benchmark/sample penalty"
  fi

  reason="${reason#, }"
  printf '%s|%s|(%s)\n' "$score" "$module" "$reason"
}

module_has_launcher_manifest() {
  local module_dir="$1"
  local manifest
  while IFS= read -r manifest; do
    if grep -q 'android.intent.action.MAIN' "$manifest" && grep -q 'android.intent.category.LAUNCHER' "$manifest"; then
      return 0
    fi
  done < <(
    find "$module_dir/src" -path '*/AndroidManifest.xml' -type f 2>/dev/null | sort
  )
  return 1
}

module_manifest_references_application() {
  local module_dir="$1"
  local manifest
  while IFS= read -r manifest; do
    if awk '
      /<application([[:space:]>]|$)/ { in_application = 1 }
      in_application && /android:name[[:space:]]*=/ { found = 1 }
      in_application && />/ { in_application = 0 }
      END { exit found ? 0 : 1 }
    ' "$manifest"; then
      return 0
    fi
  done < <(
    find "$module_dir/src" -path '*/AndroidManifest.xml' -type f 2>/dev/null | sort
  )
  return 1
}

module_has_application_class() {
  local module_dir="$1"
  local source
  while IFS= read -r source; do
    if awk '
      {
        text = text " " $0
      }
      END {
        gsub(/[[:space:]]+/, " ", text)
        found = text ~ /class[[:space:]]+[A-Za-z_][A-Za-z0-9_]*[^{};]*(extends[[:space:]]+[A-Za-z0-9_.]*Application|:[^{};]*[A-Za-z0-9_.]*Application[[:space:]]*\()/
        exit found ? 0 : 1
      }
    ' "$source"; then
      return 0
    fi
  done < <(
    find "$module_dir/src" \( -name '*.kt' -o -name '*.java' \) -type f 2>/dev/null | sort
  )
  return 1
}

is_suspicious_app_module() {
  local module="$1"
  local lower
  lower="$(printf '%s' "$module" | tr '[:upper:]' '[:lower:]')"
  case "$lower" in
    *test*|*tests*|*benchmark*|*benchmarks*|*fixture*|*fixtures*|*uitest*|*androidtest*|*sample*|*demo*|*playground*|*sandbox*)
      return 0
      ;;
  esac
  return 1
}

dedupe_array() {
  local input_name="$1"
  local values=()
  local result_lines=""
  local value found
  local restore_nounset=0
  case "$-" in
    *u*)
      restore_nounset=1
      set +u
      ;;
  esac
  eval "values=(\"\${$input_name[@]}\")"
  for value in "${values[@]}"; do
    value="$(trim "$value")"
    [[ -n "$value" ]] || continue
    found=0
    case $'\n'"$result_lines" in
      *$'\n'"$value"$'\n'*)
        found=1
        ;;
    esac
    [[ "$found" -eq 0 ]] && result_lines+="$value"$'\n'
  done
  eval "$input_name=()"
  while IFS= read -r value; do
    [[ -n "$value" ]] || continue
    eval "$input_name+=(\"\$value\")"
  done <<< "$result_lines"
  [[ "$restore_nounset" -eq 1 ]] && set -u
}

gradle_string_args() {
  local out=""
  local value escaped
  for value in "$@"; do
    escaped="$(gradle_escape_string "$value")"
    if [[ -n "$out" ]]; then
      out+=", "
    fi
    out+="\"$escaped\""
  done
  printf '%s' "$out"
}

gradle_escape_string() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s' "$value"
}

patch_settings_file() {
  local file="$1"
  local plugin_block dependency_block
  local maven_repo_path
  maven_repo_path="$(gradle_escape_string "$MAVEN_DIR")"
  plugin_block="    // Jank Hunter plugin repository
    repositories {
        maven { url = uri(\"$maven_repo_path\") }
    }
"
  dependency_block="    // Jank Hunter dependency repository
    repositories {
        maven { url = uri(\"$maven_repo_path\") }
    }
"

  if grep -q 'Jank Hunter plugin repository' "$file" && grep -q 'Jank Hunter dependency repository' "$file"; then
    log "settings already contains Jank Hunter repositories"
    return
  fi

  backup_file "$file"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would patch settings repositories in $file"
    return
  fi

  PLUGIN_BLOCK="$plugin_block" DEPENDENCY_BLOCK="$dependency_block" perl -0pi -e '
    my $plugin = $ENV{"PLUGIN_BLOCK"};
    my $dependency = $ENV{"DEPENDENCY_BLOCK"};

    if (index($_, "Jank Hunter plugin repository") < 0) {
      if ($_ =~ /pluginManagement\s*\{/) {
        s/(pluginManagement\s*\{)/$1\n$plugin/s;
      } else {
        $_ = "pluginManagement {\n$plugin    repositories {\n        google()\n        mavenCentral()\n        gradlePluginPortal()\n    }\n}\n\n" . $_;
      }
    }

    if (index($_, "Jank Hunter dependency repository") < 0) {
      if ($_ =~ /dependencyResolutionManagement\s*\{/) {
        s/(dependencyResolutionManagement\s*\{)/$1\n$dependency/s;
      } else {
        $_ .= "\n\ndependencyResolutionManagement {\n$dependency    repositories {\n        google()\n        mavenCentral()\n    }\n}\n";
      }
    }
  ' "$file"
}

patch_gitignore() {
  [[ "$ADD_GITIGNORE" -eq 1 ]] || return 0
  local file="$TARGET_ROOT/.gitignore"
  local needs_jankhunter=1
  local needs_local_properties=1
  local jankhunter_ignore_entry=".jankhunter/"
  local jankhunter_ignore_comment="# Jank Hunter local Maven repo and generated CLI"

  if [[ "$USE_AAR" -eq 1 ]]; then
    jankhunter_ignore_entry="${CLI_DIR%/}/"
    jankhunter_ignore_comment="# Jank Hunter generated CLI"
  fi

  if [[ -f "$file" ]] && grep -Fxq "$jankhunter_ignore_entry" "$file"; then
    needs_jankhunter=0
  fi
  if [[ -f "$file" ]] && grep -q '^local\.properties$' "$file"; then
    needs_local_properties=0
  fi
  if [[ "$needs_jankhunter" -eq 0 && "$needs_local_properties" -eq 0 ]]; then
    return
  fi
  backup_file "$file"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would update $file"
    return
  fi
  {
    [[ -f "$file" ]] && cat "$file"
    if [[ "$needs_jankhunter" -eq 1 ]]; then
      printf '\n%s\n%s\n' "$jankhunter_ignore_comment" "$jankhunter_ignore_entry"
    fi
    if [[ "$needs_local_properties" -eq 1 ]]; then
      printf '\n# Local Android SDK path\nlocal.properties\n'
    fi
  } > "$file.tmp"
  mv "$file.tmp" "$file"
}

properties_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  printf '%s' "$value"
}

properties_sdk_dir() {
  local file="$1"
  [[ -f "$file" ]] || return 0
  awk '
    /^[[:space:]]*sdk\.dir[[:space:]]*=/ {
      sub(/^[^=]*=/, "")
      print
      exit
    }
  ' "$file"
}

local_properties_sdk_dir() {
  properties_sdk_dir "$TARGET_ROOT/local.properties"
}

resolve_android_sdk_dir() {
  if [[ -n "$RESOLVED_ANDROID_SDK_DIR" ]]; then
    printf '%s\n' "$RESOLVED_ANDROID_SDK_DIR"
    return 0
  fi

  local target_existing="${1:-}"
  local jankhunter_existing=""
  jankhunter_existing="$(properties_sdk_dir "$JANKHUNTER_ROOT/android/local.properties" || true)"
  local candidate=""
  if [[ -n "$ANDROID_SDK_DIR" ]]; then
    candidate="$ANDROID_SDK_DIR"
  elif [[ -n "${ANDROID_HOME:-}" ]]; then
    candidate="$ANDROID_HOME"
  elif [[ -n "${ANDROID_SDK_ROOT:-}" ]]; then
    candidate="$ANDROID_SDK_ROOT"
  elif [[ -d "$HOME/Library/Android/sdk" ]]; then
    candidate="$HOME/Library/Android/sdk"
  elif [[ -n "$target_existing" ]]; then
    candidate="$target_existing"
  elif [[ -n "$jankhunter_existing" ]]; then
    candidate="$jankhunter_existing"
  else
    fail "Android SDK path was not found. Pass --android-sdk /path/to/sdk or set ANDROID_HOME."
  fi

  [[ -d "$candidate" ]] || fail "Android SDK path does not exist: $candidate"
  RESOLVED_ANDROID_SDK_DIR="$(cd "$candidate" && pwd)"
  printf '%s\n' "$RESOLVED_ANDROID_SDK_DIR"
}

resolve_android_build_tools_version() {
  if [[ -n "$RESOLVED_ANDROID_BUILD_TOOLS_VERSION" ]]; then
    printf '%s\n' "$RESOLVED_ANDROID_BUILD_TOOLS_VERSION"
    return 0
  fi

  local sdk_dir
  sdk_dir="$(resolve_android_sdk_dir "$(local_properties_sdk_dir || true)")"
  local build_tools_dir="$sdk_dir/build-tools"
  local version=""
  [[ -d "$build_tools_dir" ]] || fail "Android Build Tools directory was not found: $build_tools_dir. Install Build Tools with: \"$sdk_dir/cmdline-tools/latest/bin/sdkmanager\" \"build-tools;36.0.0\""
  if [[ -n "$ANDROID_BUILD_TOOLS_VERSION" ]]; then
    version="$ANDROID_BUILD_TOOLS_VERSION"
    [[ -d "$build_tools_dir/$version" ]] || fail "Android Build Tools $version was not found in $build_tools_dir. Install it with: \"$sdk_dir/cmdline-tools/latest/bin/sdkmanager\" \"build-tools;$version\""
  else
    version="$(find "$build_tools_dir" -maxdepth 1 -mindepth 1 -type d -exec basename {} \; 2>/dev/null | sed -nE '/^[0-9]+([.][0-9]+){1,2}$/p' | sort -t. -k1,1n -k2,2n -k3,3n | tail -n 1)"
    [[ -n "$version" ]] || fail "Android Build Tools were not found in $build_tools_dir. Install them with: \"$sdk_dir/cmdline-tools/latest/bin/sdkmanager\" \"build-tools;36.0.0\""
  fi

  RESOLVED_ANDROID_BUILD_TOOLS_VERSION="$version"
  printf '%s\n' "$RESOLVED_ANDROID_BUILD_TOOLS_VERSION"
}

write_sdk_local_properties() {
  local file="$1"
  local label="$2"
  local use_target_backup="$3"
  local current=""
  current="$(properties_sdk_dir "$file" || true)"
  if [[ -n "$current" && -z "$ANDROID_SDK_DIR" ]]; then
    log "$label local.properties already contains sdk.dir=$current"
    return
  fi

  local sdk_dir
  sdk_dir="$(resolve_android_sdk_dir "$current")"
  local escaped_sdk_dir
  escaped_sdk_dir="$(properties_escape "$sdk_dir")"

  if [[ "$use_target_backup" -eq 1 ]]; then
    backup_file "$file"
  fi
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would write sdk.dir=$escaped_sdk_dir to $file"
    return
  fi

  mkdir -p "$(dirname "$file")"
  if [[ -f "$file" ]] && grep -Eq '^[[:space:]]*sdk\.dir[[:space:]]*=' "$file"; then
    SDK_DIR_VALUE="$escaped_sdk_dir" perl -0pi -e '
      my $value = $ENV{"SDK_DIR_VALUE"};
      s/^[ \t]*sdk\.dir[ \t]*=.*$/sdk.dir=$value/m;
    ' "$file"
  else
    {
      [[ -f "$file" ]] && cat "$file"
      [[ -f "$file" ]] && printf '\n'
      printf 'sdk.dir=%s\n' "$escaped_sdk_dir"
    } > "$file.tmp"
    mv "$file.tmp" "$file"
  fi
}

prepare_jankhunter_android_sdk() {
  resolve_android_sdk_dir "$(local_properties_sdk_dir || true)" >/dev/null
  resolve_android_build_tools_version >/dev/null
  log "Android SDK: $RESOLVED_ANDROID_SDK_DIR"
  log "Android Build Tools: $RESOLVED_ANDROID_BUILD_TOOLS_VERSION"
}

patch_target_local_properties() {
  [[ "$SKIP_LOCAL_PROPERTIES" -eq 0 ]] || {
    log "skipping target local.properties"
    return
  }
  write_sdk_local_properties "$TARGET_ROOT/local.properties" "target" 1
}

patch_module_build_file() {
  local module="$1"
  local file="$2"
  local dsl="groovy"
  [[ "$file" == *.kts ]] && dsl="kts"

  local plugin_line plugin_apply_line plugin_buildscript_block annotations_dep runtime_dep okhttp_dep jh_block includes excludes build_type
  local artifact_base
  artifact_base="$(gradle_escape_string "$AAR_DIR")"
  if [[ "$USE_AAR" -eq 1 ]]; then
    if [[ "$dsl" == "kts" ]]; then
      plugin_apply_line='apply(plugin = "io.jankhunter.android")'
      plugin_buildscript_block="buildscript {
    repositories {
        google()
        mavenCentral()
    }
    dependencies {
        classpath(files(rootProject.file(\"$artifact_base/jankhunter-gradle-plugin-$VERSION.jar\")))
        classpath(\"org.ow2.asm:asm-commons:9.7.1\")
    }
}

"
      annotations_dep="    compileOnly(files(rootProject.file(\"$artifact_base/jankhunter-annotations-$VERSION.jar\")))"
      runtime_dep="    debugImplementation(files(rootProject.file(\"$artifact_base/jankhunter-runtime-$VERSION.aar\")))"
      okhttp_dep="    debugImplementation(files(rootProject.file(\"$artifact_base/jankhunter-okhttp3-$VERSION.aar\")))"
    else
      plugin_apply_line="apply plugin: 'io.jankhunter.android'"
      plugin_buildscript_block="buildscript {
    repositories {
        google()
        mavenCentral()
    }
    dependencies {
        classpath files(rootProject.file(\"$artifact_base/jankhunter-gradle-plugin-$VERSION.jar\"))
        classpath \"org.ow2.asm:asm-commons:9.7.1\"
    }
}

"
      annotations_dep="    compileOnly files(rootProject.file(\"$artifact_base/jankhunter-annotations-$VERSION.jar\"))"
      runtime_dep="    debugImplementation files(rootProject.file(\"$artifact_base/jankhunter-runtime-$VERSION.aar\"))"
      okhttp_dep="    debugImplementation files(rootProject.file(\"$artifact_base/jankhunter-okhttp3-$VERSION.aar\"))"
    fi
  else
    if [[ "$dsl" == "kts" ]]; then
      plugin_line="    id(\"io.jankhunter.android\") version \"$VERSION\""
      annotations_dep="    compileOnly(\"$GROUP:jankhunter-annotations:$VERSION\")"
      runtime_dep="    debugImplementation(\"$GROUP:jankhunter-runtime:$VERSION\")"
      okhttp_dep="    debugImplementation(\"$GROUP:jankhunter-okhttp3:$VERSION\")"
    else
      plugin_line="    id 'io.jankhunter.android' version '$VERSION'"
      annotations_dep="    compileOnly \"$GROUP:jankhunter-annotations:$VERSION\""
      runtime_dep="    debugImplementation \"$GROUP:jankhunter-runtime:$VERSION\""
      okhttp_dep="    debugImplementation \"$GROUP:jankhunter-okhttp3:$VERSION\""
    fi
  fi

  if [[ "$USE_AAR" -eq 1 && "$dsl" == "kts" ]]; then
    jh_block=$'\n\nextensions.getByType(io.jankhunter.gradle.JankHunterExtension::class.java).apply {\n'
  else
    jh_block=$'\n\njankHunter {\n'
  fi
  for build_type in "${BUILD_TYPES[@]}"; do
    jh_block+="    enabledBuildTypes.add(\"$build_type\")"$'\n'
  done
  jh_block+=$'    autoInit = true\n\n'
  jh_block+=$'    retainedHeapDump {\n'
  jh_block+=$'        enabled = true\n'
  jh_block+=$'        minIntervalMs = 600_000\n'
  jh_block+=$'        maxCount = 1\n'
  jh_block+=$'        minRetainedAgeMs = 30_000\n'
  jh_block+=$'    }\n\n'
  jh_block+=$'    instrument {\n'
  jh_block+=$'        okhttp = true\n'
  jh_block+=$'        webSockets = true\n'
  jh_block+=$'        handlers = true\n'
  jh_block+=$'        executors = true\n'
  jh_block+=$'        coroutines = true\n'
  jh_block+=$'        flowInteractions = true\n'
  jh_block+=$'        logSpam = true\n'
  jh_block+=$'        classGraph = true\n'
  if [[ "$RUNTIME_CALL_GRAPH" -eq 1 ]]; then
    jh_block+=$'        runtimeCallGraph = true\n'
  else
    jh_block+=$'        runtimeCallGraph = false\n'
  fi
  jh_block+=$'        methodCounters = false\n'
  jh_block+=$'        allowEmptyIncludePackages = false\n'
  if [[ "$INCLUDE_WHOLE_APPLICATION" -eq 1 ]]; then
    jh_block+=$'        includeWholeApplication = true\n'
  else
    jh_block+=$'        includeWholeApplication = false\n'
  fi
  if [[ "$ASM_PROGRESS_LOG" -eq 1 ]]; then
    jh_block+=$'        asmProgressLog = true\n'
  else
    jh_block+=$'        asmProgressLog = false\n'
  fi
  if [[ "${#INCLUDE_PACKAGES[@]}" -gt 0 ]]; then
    includes="$(gradle_string_args "${INCLUDE_PACKAGES[@]}")"
    jh_block+=$'\n'
    jh_block+="        includePackages($includes)"$'\n'
  fi
  if [[ "${#EXCLUDE_PACKAGES[@]}" -gt 0 ]]; then
    excludes="$(gradle_string_args "${EXCLUDE_PACKAGES[@]}")"
    jh_block+=$'\n'
    jh_block+="        excludePackages($excludes)"$'\n'
  fi
  jh_block+=$'    }\n}\n'

  backup_file "$file"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would patch module $module build file: $file"
    return
  fi

  if [[ "$USE_AAR" -eq 1 ]]; then
    JH_GROUP="$GROUP" perl -0pi -e '
      my $group = quotemeta($ENV{"JH_GROUP"});
      s/^[ \t]*id\s*(?:\(|\s+)[\x27"]io\.jankhunter\.android[\x27"].*?\n//mg;
      s/^[ \t]*(?:compileOnly|debugImplementation)\s*(?:\(\s*)?[\x27"]$group:jankhunter-(?:annotations|runtime|okhttp3):[^\x27"]+[\x27"]\s*\)?[ \t]*\n//mg;
    ' "$file"

    PLUGIN_BUILDSCRIPT_BLOCK="$plugin_buildscript_block" perl -0pi -e '
      my $block = $ENV{"PLUGIN_BUILDSCRIPT_BLOCK"};
      if (index($_, "jankhunter-gradle-plugin") < 0) {
        if ($_ =~ /\A((?:(?:[ \t]*import[^\n]*|[ \t]*)\n)*)/) {
          substr($_, length($1), 0) = $block;
        } else {
          $_ = $block . $_;
        }
      }
    ' "$file"

    if ! grep -q "io.jankhunter.android" "$file"; then
      printf '\n%s\n' "$plugin_apply_line" >> "$file"
    fi
  else
    PLUGIN_LINE="$plugin_line" perl -0pi -e '
      my $line = $ENV{"PLUGIN_LINE"};
      if (index($_, "io.jankhunter.android") < 0) {
        if ($_ =~ /plugins\s*\{/) {
          s/(plugins\s*\{)/$1\n$line/s;
        } else {
          $_ = "plugins {\n$line\n}\n\n" . $_;
        }
      }
    ' "$file"
  fi

  local runtime_marker="$GROUP:jankhunter-runtime"
  if [[ "$USE_AAR" -eq 1 ]]; then
    runtime_marker="jankhunter-runtime-$VERSION.aar"
  fi
  if ! grep -Fq "$runtime_marker" "$file"; then
    if [[ "$USE_AAR" -eq 1 ]]; then
      printf '\n\ndependencies {\n%s\n%s\n%s\n}\n' "$annotations_dep" "$runtime_dep" "$okhttp_dep" >> "$file"
    else
      ANNOTATIONS_DEP="$annotations_dep" RUNTIME_DEP="$runtime_dep" OKHTTP_DEP="$okhttp_dep" perl -0pi -e '
        my $deps = $ENV{"ANNOTATIONS_DEP"} . "\n" . $ENV{"RUNTIME_DEP"} . "\n" . $ENV{"OKHTTP_DEP"} . "\n";
        if ($_ =~ /dependencies\s*\{/) {
          s/(dependencies\s*\{)/$1\n$deps/s;
        } else {
          $_ .= "\n\ndependencies {\n$deps}\n";
        }
      ' "$file"
    fi
  fi

  if [[ "$USE_AAR" -eq 0 ]] && ! grep -q "$GROUP:jankhunter-annotations" "$file"; then
    ANNOTATIONS_DEP="$annotations_dep" perl -0pi -e '
      my $dep = $ENV{"ANNOTATIONS_DEP"} . "\n";
      if ($_ =~ /dependencies\s*\{/) {
        s/(dependencies\s*\{)/$1\n$dep/s;
      } else {
        $_ .= "\n\ndependencies {\n$dep}\n";
      }
    ' "$file"
  fi

  if ! grep -Eq 'jankHunter[[:space:]]*\{|JankHunterExtension::class[.]java' "$file"; then
    printf '%s' "$jh_block" >> "$file"
  fi
}

publish_artifacts_if_needed() {
  [[ "$SKIP_PUBLISH" -eq 0 ]] || {
    log "skipping publish step"
    return
  }

  local runtime_aar="$MAVEN_REPO_ABS/$GROUP_PATH/jankhunter-runtime/$VERSION/jankhunter-runtime-$VERSION.aar"
  local annotations_jar="$MAVEN_REPO_ABS/$GROUP_PATH/jankhunter-annotations/$VERSION/jankhunter-annotations-$VERSION.jar"
  local okhttp_aar="$MAVEN_REPO_ABS/$GROUP_PATH/jankhunter-okhttp3/$VERSION/jankhunter-okhttp3-$VERSION.aar"
  local plugin_pom="$MAVEN_REPO_ABS/io/jankhunter/android/io.jankhunter.android.gradle.plugin/$VERSION/io.jankhunter.android.gradle.plugin-$VERSION.pom"

  if [[ -f "$runtime_aar" && -f "$annotations_jar" && -f "$okhttp_aar" && -f "$plugin_pom" ]]; then
    log "refreshing Jank Hunter artifacts in $MAVEN_REPO_ABS"
  else
    log "publishing Jank Hunter artifacts into $MAVEN_REPO_ABS"
  fi
  run_cmd mkdir -p "$MAVEN_REPO_ABS"
  if [[ "$DRY_RUN" -eq 0 ]]; then
    if ! (cd "$JANKHUNTER_ROOT/android" && ANDROID_HOME="$RESOLVED_ANDROID_SDK_DIR" ANDROID_SDK_ROOT="$RESOLVED_ANDROID_SDK_DIR" ./gradlew publishToMavenLocal -PjankHunterBuildToolsVersion="$RESOLVED_ANDROID_BUILD_TOOLS_VERSION" -Dmaven.repo.local="$MAVEN_REPO_ABS" --no-daemon --stacktrace); then
      fail "failed to publish Jank Hunter artifacts. Android SDK: $RESOLVED_ANDROID_SDK_DIR, Build Tools: $RESOLVED_ANDROID_BUILD_TOOLS_VERSION"
    fi
  else
    log "would run: cd $JANKHUNTER_ROOT/android && ANDROID_HOME=$RESOLVED_ANDROID_SDK_DIR ./gradlew publishToMavenLocal -PjankHunterBuildToolsVersion=$RESOLVED_ANDROID_BUILD_TOOLS_VERSION -Dmaven.repo.local=$MAVEN_REPO_ABS --no-daemon --stacktrace"
  fi
}

prepare_aar_artifacts_if_needed() {
  [[ "$SKIP_PUBLISH" -eq 0 ]] || {
    log "skipping AAR/JAR artifact build and copy"
    return
  }

  local runtime_target="$AAR_DIR_ABS/jankhunter-runtime-$VERSION.aar"
  local annotations_target="$AAR_DIR_ABS/jankhunter-annotations-$VERSION.jar"
  local okhttp_target="$AAR_DIR_ABS/jankhunter-okhttp3-$VERSION.aar"
  local plugin_target="$AAR_DIR_ABS/jankhunter-gradle-plugin-$VERSION.jar"

  if [[ -f "$runtime_target" && -f "$annotations_target" && -f "$okhttp_target" && -f "$plugin_target" ]]; then
    log "refreshing Jank Hunter AAR/JAR artifacts in $AAR_DIR_ABS"
  else
    log "building and copying Jank Hunter AAR/JAR artifacts into $AAR_DIR_ABS"
  fi
  run_cmd mkdir -p "$AAR_DIR_ABS"
  if [[ "$DRY_RUN" -eq 0 ]]; then
    if ! (cd "$JANKHUNTER_ROOT/android" && ANDROID_HOME="$RESOLVED_ANDROID_SDK_DIR" ANDROID_SDK_ROOT="$RESOLVED_ANDROID_SDK_DIR" ./gradlew :jankhunter-runtime:assembleRelease :jankhunter-okhttp3:assembleRelease :jankhunter-annotations:jar :jankhunter-gradle-plugin:jar -PjankHunterBuildToolsVersion="$RESOLVED_ANDROID_BUILD_TOOLS_VERSION" --no-daemon --stacktrace); then
      fail "failed to build Jank Hunter AAR/JAR artifacts. Android SDK: $RESOLVED_ANDROID_SDK_DIR, Build Tools: $RESOLVED_ANDROID_BUILD_TOOLS_VERSION"
    fi

    local runtime_source="$JANKHUNTER_ROOT/android/jankhunter-runtime/build/outputs/aar/jankhunter-runtime-release.aar"
    local annotations_source="$JANKHUNTER_ROOT/android/jankhunter-annotations/build/libs/jankhunter-annotations-$VERSION.jar"
    local okhttp_source="$JANKHUNTER_ROOT/android/jankhunter-okhttp3/build/outputs/aar/jankhunter-okhttp3-release.aar"
    local plugin_source="$JANKHUNTER_ROOT/android/jankhunter-gradle-plugin/build/libs/jankhunter-gradle-plugin-$VERSION.jar"

    [[ -f "$runtime_source" ]] || fail "runtime AAR was not produced: $runtime_source"
    [[ -f "$annotations_source" ]] || fail "annotations JAR was not produced: $annotations_source"
    [[ -f "$okhttp_source" ]] || fail "OkHttp AAR was not produced: $okhttp_source"
    [[ -f "$plugin_source" ]] || fail "Gradle plugin JAR was not produced: $plugin_source"

    cp "$runtime_source" "$runtime_target"
    cp "$annotations_source" "$annotations_target"
    cp "$okhttp_source" "$okhttp_target"
    cp "$plugin_source" "$plugin_target"
  else
    log "would run: cd $JANKHUNTER_ROOT/android && ANDROID_HOME=$RESOLVED_ANDROID_SDK_DIR ./gradlew :jankhunter-runtime:assembleRelease :jankhunter-okhttp3:assembleRelease :jankhunter-annotations:jar :jankhunter-gradle-plugin:jar -PjankHunterBuildToolsVersion=$RESOLVED_ANDROID_BUILD_TOOLS_VERSION --no-daemon --stacktrace"
    log "would copy AAR/JAR artifacts to $AAR_DIR_ABS"
  fi
}

build_cli_if_needed() {
  [[ "$SKIP_CLI_BUILD" -eq 0 ]] || {
    log "skipping CLI build"
    return
  }

  local source_binary="$JANKHUNTER_ROOT/cli/bin/jankhunter"
  local target_binary="$CLI_DIR_ABS/jankhunter"

  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would run: cd $JANKHUNTER_ROOT/cli && make build"
    log "would copy CLI binary to $target_binary"
    return
  fi

  log "building Jank Hunter CLI"
  (cd "$JANKHUNTER_ROOT/cli" && make build)
  [[ -x "$source_binary" ]] || fail "CLI binary was not produced: $source_binary"

  mkdir -p "$CLI_DIR_ABS"
  cp "$source_binary" "$target_binary"
  chmod 0755 "$target_binary"
}

verify_target_project() {
  [[ "$VERIFY" -eq 1 ]] || return 0
  local gradlew="$TARGET_ROOT/gradlew"
  [[ -x "$gradlew" ]] || gradlew="gradle"
  local tasks=()
  local module
  for module in "${MODULES[@]}"; do
    if [[ "$module" == ":" ]]; then
      tasks+=("tasks")
    else
      tasks+=("$module:tasks")
    fi
  done
  log "verifying target Gradle resolution"
  if [[ "$gradlew" == "gradle" ]]; then
    (cd "$TARGET_ROOT" && gradle "${tasks[@]}" --no-daemon)
  else
    (cd "$TARGET_ROOT" && ./gradlew "${tasks[@]}" --no-daemon)
  fi
}

dedupe_array MODULES
dedupe_array INCLUDE_PACKAGES
dedupe_array EXCLUDE_PACKAGES
dedupe_array BUILD_TYPES

if [[ "${#MODULES[@]}" -eq 0 ]]; then
  detected_module="$(detect_app_module)"
  MODULES=("$detected_module")
  log "detected Android application module: $detected_module"
fi

GROUP_PATH="${GROUP//./\/}"

SETTINGS_FILE="$(detect_settings_file)"

log "Jank Hunter: $JANKHUNTER_ROOT"
log "Target: $TARGET_ROOT"
log "Version: $GROUP:$VERSION"
if [[ "$USE_AAR" -eq 1 ]]; then
  log "Artifact mode: embedded AAR/JAR"
  log "AAR/JAR artifacts: $AAR_DIR_ABS"
else
  log "Artifact mode: local Maven"
  log "Local Maven repo: $MAVEN_REPO_ABS"
fi
log "CLI binary: $CLI_DIR_ABS/jankhunter"
log "Modules: ${MODULES[*]}"

prepare_jankhunter_android_sdk
if [[ "$USE_AAR" -eq 1 ]]; then
  prepare_aar_artifacts_if_needed
else
  publish_artifacts_if_needed
fi
build_cli_if_needed
patch_target_local_properties
if [[ "$USE_AAR" -eq 1 ]]; then
  log "using embedded AAR/JAR mode; target settings repositories are unchanged"
else
  patch_settings_file "$SETTINGS_FILE"
fi
patch_gitignore

for module in "${MODULES[@]}"; do
  build_file="$(module_build_file "$module")"
  patch_module_build_file "$module" "$build_file"
done

verify_target_project

log "done"
log "Backups: $BACKUP_ROOT"
if [[ "$USE_AAR" -eq 1 ]]; then
  log "AAR/JAR artifacts: $AAR_DIR_ABS"
else
  log "Local Maven repo: $MAVEN_REPO_ABS"
fi
log "CLI: $CLI_DIR_ABS/jankhunter"
log "Next: build the target app and inspect generated owner-map/class-graph under each patched module build/generated/jankhunter/<variant>/"
