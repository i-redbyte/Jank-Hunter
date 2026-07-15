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
CLI_DIR=".jankhunter/bin"
ANDROID_SDK_DIR=""
RESOLVED_ANDROID_SDK_DIR=""
ANDROID_BUILD_TOOLS_VERSION=""
RESOLVED_ANDROID_BUILD_TOOLS_VERSION=""
DRY_RUN=0
SKIP_PUBLISH=0
SKIP_CLI_BUILD=0
SKIP_LOCAL_PROPERTIES=0
VERIFY=0
ADD_GITIGNORE=1
RUNTIME_CALL_GRAPH=-1
ASM_PROGRESS_LOG=-1
OKHTTP_HOOKS=-1
WEBSOCKET_HOOKS=-1
DI_ANALYSIS=-1
SESSION_LOG_SIZE_LIMIT=-1
MAX_SESSION_LOG_SIZE_MIB=""
BUILD_TYPES_EXPLICIT=0
TRANSACTION_ACTIVE=0
TRANSACTION_COMMITTED=0
ROLLBACK_RUNNING=0
LAST_TEMP_FILE=""
CATALOG_FILE=""
CATALOG_JH_ALIAS=""
CATALOG_ALIASES=""
USES_JH_CATALOG=0
CENTRAL_PLUGIN_DECLARATIONS=0
ROOT_CENTRAL_PLUGIN_DECLARATIONS=0
ROOT_BUILD_FILE=""

MODULES=()
INCLUDE_PACKAGES=()
EXCLUDE_PACKAGES=()
BUILD_TYPES=("debug")
MODULE_BUILD_FILES=()
MODULE_USES_JH_ALIAS=()
MODULE_ADD_LITERAL_VERSION=()
MODULE_LEGACY_HELPER=()
MODULE_MANAGED_HELPER=()
MODULE_CURRENT_SDK_DEPENDENCY=()
MODULE_MANAGED_CONFIGURATION=()
TRANSACTION_FILES=()
TRANSACTION_EXISTED=()
TEMP_FILES=()

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
  --runtime-call-graph          Enable runtime caller -> callee graph hooks.
  --okhttp                     Enable OkHttp hooks.
  --websockets                 Enable WebSocket hooks.
  --analyze-di                 Add build-time Dagger/Hilt/Koin analysis to generated artifacts.
  --build-type debug            Enabled build type. Can be repeated or comma-separated.
  --max-session-log-size-mib N Enable the physical .jhlog limiter and set its size in MiB.
  --no-session-log-size-limit  Disable the physical .jhlog size limiter.
  --maven-dir PATH              Local Maven repo inside target project. Default: .jankhunter/maven.
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
  --asm-progress-log            Enable the one-line ASM build progress indicator.
  --no-gitignore                Do not update target .gitignore.

Overlay ownership:
  Existing user-owned jankHunter/dependencies blocks are preserved. Options omitted on a rerun
  preserve the script-managed overlay. Supplying any managed DSL option replaces that managed
  overlay with the explicitly supplied values; user-owned blocks still remain untouched.

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

require_value() {
  local option="$1"
  local value="${2:-}"
  [[ -n "$value" && "$value" != --* ]] || fail "$option requires a value"
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
      require_value "$1" "${2:-}"
      TARGET_ROOT="${2:-}"
      shift 2
      ;;
    --jankhunter)
      require_value "$1" "${2:-}"
      JANKHUNTER_ROOT="${2:-}"
      shift 2
      ;;
    --module|--app-module)
      require_value "$1" "${2:-}"
      MODULES+=("${2:-}")
      shift 2
      ;;
    --include-package|--include)
      require_value "$1" "${2:-}"
      INCLUDE_PACKAGES+=("${2:-}")
      shift 2
      ;;
    --include-packages|--includes)
      require_value "$1" "${2:-}"
      split_csv_into "${2:-}" INCLUDE_PACKAGES
      shift 2
      ;;
    --exclude-package|--exclude)
      require_value "$1" "${2:-}"
      EXCLUDE_PACKAGES+=("${2:-}")
      shift 2
      ;;
    --exclude-packages|--excludes)
      require_value "$1" "${2:-}"
      split_csv_into "${2:-}" EXCLUDE_PACKAGES
      shift 2
      ;;
    --runtime-call-graph)
      RUNTIME_CALL_GRAPH=1
      shift
      ;;
    --no-runtime-call-graph)
      RUNTIME_CALL_GRAPH=0
      shift
      ;;
    --okhttp)
      OKHTTP_HOOKS=1
      shift
      ;;
    --no-okhttp)
      OKHTTP_HOOKS=0
      shift
      ;;
    --websockets)
      WEBSOCKET_HOOKS=1
      shift
      ;;
    --no-websockets)
      WEBSOCKET_HOOKS=0
      shift
      ;;
    --analyze-di|--analyse-di)
      DI_ANALYSIS=1
      shift
      ;;
    --no-analyze-di|--no-analyse-di)
      DI_ANALYSIS=0
      shift
      ;;
    --build-type)
      require_value "$1" "${2:-}"
      if [[ "$BUILD_TYPES_EXPLICIT" -eq 0 ]]; then
        BUILD_TYPES=()
        BUILD_TYPES_EXPLICIT=1
      fi
      split_csv_into "${2:-}" BUILD_TYPES
      shift 2
      ;;
    --max-session-log-size-mib)
      require_value "$1" "${2:-}"
      MAX_SESSION_LOG_SIZE_MIB="$2"
      SESSION_LOG_SIZE_LIMIT=1
      shift 2
      ;;
    --no-session-log-size-limit)
      SESSION_LOG_SIZE_LIMIT=0
      MAX_SESSION_LOG_SIZE_MIB=""
      shift
      ;;
    --maven-dir)
      require_value "$1" "${2:-}"
      MAVEN_DIR="${2:-}"
      shift 2
      ;;
    --cli-dir)
      require_value "$1" "${2:-}"
      CLI_DIR="${2:-}"
      shift 2
      ;;
    --android-sdk|--android-sdk-dir)
      require_value "$1" "${2:-}"
      ANDROID_SDK_DIR="${2:-}"
      shift 2
      ;;
    --android-build-tools|--android-build-tools-version)
      require_value "$1" "${2:-}"
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
    --asm-progress-log)
      ASM_PROGRESS_LOG=1
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

[[ -d "$JANKHUNTER_ROOT" ]] || fail "Jank Hunter root does not exist: $JANKHUNTER_ROOT"
[[ -d "$TARGET_ROOT" ]] || fail "target project does not exist: $TARGET_ROOT"
JANKHUNTER_ROOT="$(cd "$JANKHUNTER_ROOT" && pwd -P)"
TARGET_ROOT="$(cd "$TARGET_ROOT" && pwd -P)"

if [[ "$SKIP_PUBLISH" -eq 0 ]]; then
  [[ -x "$JANKHUNTER_ROOT/android/gradlew" ]] ||
    fail "Jank Hunter Android Gradle wrapper not found or not executable: $JANKHUNTER_ROOT/android/gradlew"
fi
if [[ "$SKIP_CLI_BUILD" -eq 0 ]]; then
  [[ -f "$JANKHUNTER_ROOT/cli/Makefile" ]] || fail "Jank Hunter CLI Makefile not found: $JANKHUNTER_ROOT/cli/Makefile"
fi
[[ -f "$JANKHUNTER_ROOT/android/gradle.properties" ]] ||
  fail "Jank Hunter android/gradle.properties not found"
[[ -n "$MAVEN_DIR" ]] || fail "--maven-dir cannot be empty"
[[ -n "$CLI_DIR" ]] || fail "--cli-dir cannot be empty"

validate_target_relative_path() {
  local option="$1"
  local value="$2"
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* ]] || fail "$option cannot contain line breaks"
  case "$value" in
    /*|.|..|./*|../*|*/./*|*/../*|*/.|*/..|*//*|*/)
      fail "$option must be a relative path inside the target project: $value"
      ;;
  esac
}

validate_target_relative_path --maven-dir "$MAVEN_DIR"
validate_target_relative_path --cli-dir "$CLI_DIR"

VERSION="$(awk -F= '$1 == "jankHunterVersion" { print $2; exit }' "$JANKHUNTER_ROOT/android/gradle.properties")"
GROUP="$(awk -F= '$1 == "jankHunterGroup" { print $2; exit }' "$JANKHUNTER_ROOT/android/gradle.properties")"
[[ -n "$VERSION" ]] || fail "could not read jankHunterVersion from android/gradle.properties"
[[ -n "$GROUP" ]] || fail "could not read jankHunterGroup from android/gradle.properties"

target_abs_path() {
  local option="$1"
  local path="$2"
  local current="$TARGET_ROOT"
  local component
  local old_ifs="$IFS"
  local path_components=()
  IFS='/'
  read -r -a path_components <<< "$path"
  IFS="$old_ifs"

  for component in "${path_components[@]}"; do
    [[ -n "$component" ]] || fail "$option contains an empty path component: $path"
    if [[ -e "$current/$component" || -L "$current/$component" ]]; then
      [[ -d "$current/$component" ]] ||
        fail "$option has a non-directory or dangling symlink component: $current/$component"
      current="$(cd "$current/$component" && pwd -P)"
    else
      current="$current/$component"
    fi
    case "$current" in
      "$TARGET_ROOT"|"$TARGET_ROOT"/*)
        ;;
      *)
        fail "$option resolves outside the target project through a symlink: $path"
        ;;
    esac
  done
  printf '%s\n' "$current"
}

MAVEN_REPO_ABS="$(target_abs_path --maven-dir "$MAVEN_DIR")"
CLI_DIR_ABS="$(target_abs_path --cli-dir "$CLI_DIR")"
BACKUP_BASE="$(target_abs_path --backup-dir ".jankhunter-backups")"
BACKUP_ROOT="$BACKUP_BASE/$(date +%Y%m%d-%H%M%S)-$$"

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

path_is_inside_target() {
  local path="$1"
  case "$path" in
    "$TARGET_ROOT"|"$TARGET_ROOT"/*)
      return 0
      ;;
  esac
  return 1
}

validate_target_file_path() {
  local label="$1"
  local file="$2"
  path_is_inside_target "$file" || fail "$label is outside the target project: $file"
  if [[ -L "$file" ]]; then
    local resolved=""
    resolved="$(perl -MCwd=abs_path -e 'my $path = abs_path($ARGV[0]); print $path if defined $path' "$file")"
    [[ -n "$resolved" ]] || fail "$label is a dangling symlink: $file"
    path_is_inside_target "$resolved" || fail "$label symlink resolves outside the target project: $file -> $resolved"
    fail "$label is a symlink; replace it with a regular file before integration: $file"
  fi
  if [[ -e "$file" && ! -f "$file" ]]; then
    fail "$label must be a regular file: $file"
  fi
}

transaction_file_index() {
  local file="$1"
  local index
  set +u
  for ((index = 0; index < ${#TRANSACTION_FILES[@]}; index++)); do
    if [[ "${TRANSACTION_FILES[$index]}" == "$file" ]]; then
      printf '%s\n' "$index"
      set -u
      return 0
    fi
  done
  set -u
  return 1
}

transaction_register_file() {
  local file="$1"
  transaction_file_index "$file" >/dev/null 2>&1 && return 0
  local rel="${file#$TARGET_ROOT/}"
  [[ "$rel" != "$file" && -n "$rel" ]] || fail "cannot register file outside target transaction: $file"
  local existed=0
  if [[ -f "$file" ]]; then
    existed=1
    local dest="$BACKUP_ROOT/$rel"
    mkdir -p "$(dirname "$dest")"
    cp -p "$file" "$dest"
  fi
  TRANSACTION_FILES+=("$file")
  TRANSACTION_EXISTED+=("$existed")
}

rollback_target_files() {
  [[ "$TRANSACTION_ACTIVE" -eq 1 && "$TRANSACTION_COMMITTED" -eq 0 ]] || return 0
  [[ "$ROLLBACK_RUNNING" -eq 0 ]] || return 0
  ROLLBACK_RUNNING=1
  set +e
  local index file rel backup
  set +u
  for ((index = ${#TRANSACTION_FILES[@]} - 1; index >= 0; index--)); do
    file="${TRANSACTION_FILES[$index]}"
    if [[ "${TRANSACTION_EXISTED[$index]}" -eq 1 ]]; then
      rel="${file#$TARGET_ROOT/}"
      backup="$BACKUP_ROOT/$rel"
      if [[ -f "$backup" ]]; then
        cp -p "$backup" "$file"
      fi
    else
      rm -f "$file"
    fi
  done
  set -u
  log "rolled back target Gradle/configuration files; backups remain in $BACKUP_ROOT" >&2
}

cleanup_temp_files() {
  local file
  set +u
  for file in "${TEMP_FILES[@]}"; do
    [[ -n "$file" ]] && rm -f "$file"
  done
  set -u
}

on_exit() {
  local status=$?
  trap - EXIT
  if [[ "$status" -ne 0 ]]; then
    rollback_target_files
  fi
  cleanup_temp_files
  exit "$status"
}

make_temp_for() {
  local file="$1"
  local directory base
  directory="$(dirname "$file")"
  base="$(basename "$file")"
  LAST_TEMP_FILE="$(mktemp "$directory/.${base}.jankhunter.XXXXXX")" || fail "cannot create temporary file next to $file"
  TEMP_FILES+=("$LAST_TEMP_FILE")
}

copy_to_temp() {
  local file="$1"
  make_temp_for "$file"
  if [[ -f "$file" ]]; then
    cp -p "$file" "$LAST_TEMP_FILE"
  else
    : > "$LAST_TEMP_FILE"
  fi
}

atomic_replace() {
  local temp="$1"
  local file="$2"
  mv -f "$temp" "$file"
}

backup_file() {
  local file="$1"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    [[ -f "$file" ]] && log "would back up $file"
    return 0
  fi
  transaction_register_file "$file"
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
  if [[ "$module" != ":" && ! "$module" =~ ^(:[A-Za-z0-9_.-]+)+$ ]]; then
    fail "invalid Gradle module path: $module"
  fi
  [[ "$module" != *":.."* && "$module" != *":."* ]] || fail "unsafe Gradle module path: $module"
  module="${module#:}"
  module="${module//:/\/}"
  local directory
  if [[ -z "$module" ]]; then
    directory="$TARGET_ROOT"
  else
    directory="$TARGET_ROOT/$module"
  fi
  [[ -d "$directory" ]] || fail "module directory does not exist: $directory"
  directory="$(cd "$directory" && pwd -P)"
  case "$directory" in
    "$TARGET_ROOT"|"$TARGET_ROOT"/*)
      printf '%s\n' "$directory"
      ;;
    *)
      fail "module resolves outside the target project: $directory"
      ;;
  esac
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
      -type d \( -name .git -o -name .gradle -o -name build -o -name node_modules \
        -o -name .jankhunter -o -name .jankhunter-backups \) -prune \
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
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* ]] || fail "Gradle value contains a line break"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//\$/\\\$}"
  printf '%s' "$value"
}

validate_managed_block() {
  local file="$1"
  local label="$2"
  local begin_marker="$3"
  local end_marker="$4"
  local result
  if ! result="$(awk -v begin="$begin_marker" -v end="$end_marker" '
    {
      line = $0
      sub(/\r$/, "", line)
      sub(/^[[:space:]]+/, "", line)
      sub(/[[:space:]]+$/, "", line)
      if (line == begin) {
        begin_count++
        begin_line = NR
      }
      if (line == end) {
        end_count++
        end_line = NR
      }
    }
    END {
      printf "BEGIN=%d END=%d BEGIN_LINE=%d END_LINE=%d", begin_count + 0, end_count + 0, begin_line + 0, end_line + 0
      if (begin_count != end_count || begin_count > 1 || (begin_count == 1 && begin_line >= end_line)) {
        exit 2
      }
    }
  ' "$file")"; then
    fail "$label managed block is malformed in $file ($result); markers must be exact standalone lines in BEGIN-before-END order"
  fi
}

gradle_file_tool() {
  local command="$1"
  local file="$2"
  perl - "$command" "$file" <<'PERL'
use strict;
use warnings;

my ($command, $file) = @ARGV;
open my $input, '<', $file or die "cannot read $file: $!\n";
local $/;
my $text = <$input>;
close $input;

my $plugin_id = 'io.jankhunter.android';
my $groovy_dsl = $file =~ /\.gradle\z/;

sub slashy_can_start {
  my ($tokens) = @_;
  return 1 unless @$tokens;
  my $previous = $tokens->[-1];
  if ($previous->{type} eq 'symbol') {
    # These tokens cannot finish a Groovy expression, so a following slash starts a literal rather
    # than the division operator. Ambiguous command-style literals are handled separately below.
    return $previous->{value} =~ /\A(?:[=(:,\[\{!&|?;~])\z/;
  }
  return $previous->{type} eq 'identifier' &&
    $previous->{value} =~ /\A(?:assert|case|in|return|throw|yield)\z/;
}

sub dollar_slashy_end {
  my ($source, $start) = @_;
  my $length = length($source);
  my $index = $start + 2;
  while ($index < $length) {
    my $char = substr($source, $index, 1);
    if ($char eq '$' && $index + 1 < $length) {
      my $next = substr($source, $index + 1, 1);
      if ($next eq '$' || $next eq '/') {
        # In a dollar-slashy string '$$' escapes '$' and '$/' escapes '/'. This also
        # skips the '/$' inside the escaped closing delimiter '$/$$'.
        $index += 2;
        next;
      }
    }
    return $index if $char eq '/' && substr($source, $index + 1, 1) eq '$';
    $index++;
  }
  return -1;
}

sub slashy_end {
  my ($source, $start) = @_;
  my $length = length($source);
  my $index = $start + 1;
  while ($index < $length) {
    my $char = substr($source, $index, 1);
    if ($char eq '\\') {
      $index += 2;
      next;
    }
    return $index if $char eq '/';
    $index++;
  }
  return -1;
}

sub tokenise {
  my ($source) = @_;
  my @tokens;
  my $length = length($source);
  my $index = 0;
  while ($index < $length) {
    my $char = substr($source, $index, 1);
    if ($char =~ /\s/) {
      $index++;
      next;
    }
    if (substr($source, $index, 2) eq '//') {
      my $newline = index($source, "\n", $index + 2);
      $index = $newline < 0 ? $length : $newline + 1;
      next;
    }
    if (substr($source, $index, 2) eq '/*') {
      my $depth = 1;
      $index += 2;
      while ($index < $length && $depth > 0) {
        if (substr($source, $index, 2) eq '/*') {
          $depth++;
          $index += 2;
        } elsif (substr($source, $index, 2) eq '*/') {
          $depth--;
          $index += 2;
        } else {
          $index++;
        }
      }
      die "unterminated block comment in $file\n" if $depth > 0;
      next;
    }
    if ($groovy_dsl && substr($source, $index, 2) eq '$/') {
      my $start = $index;
      my $end = dollar_slashy_end($source, $index);
      die "unterminated dollar-slashy string in $file\n" if $end < 0;
      push @tokens, {
        type => 'string', value => substr($source, $index + 2, $end - $index - 2),
        start => $start, end => $end + 2, content_start => $index + 2, content_end => $end,
      };
      $index = $end + 2;
      next;
    }
    if ($groovy_dsl && $char eq '/' && slashy_can_start(\@tokens)) {
      my $start = $index;
      my $end = slashy_end($source, $index);
      die "unterminated Groovy slashy string in $file\n" if $end < 0;
      push @tokens, {
        type => 'string', value => substr($source, $index + 1, $end - $index - 1),
        start => $start, end => $end + 1, content_start => $index + 1, content_end => $end,
      };
      $index = $end + 1;
      next;
    }
    if ($groovy_dsl && $char eq '/') {
      my $candidate_end = slashy_end($source, $index);
      if ($candidate_end >= 0) {
        my $candidate = substr($source, $index + 1, $candidate_end - $index - 1);
        if ($candidate !~ /[\r\n]/ && $candidate =~ /[{}]/) {
          die "ambiguous Groovy slashy string in $file; place it after '=' or inside parentheses so Jank Hunter can patch the file safely\n";
        }
      }
    }
    if ($char eq q{"} || $char eq q{'}) {
      my $quote = $char;
      my $delimiter_length = substr($source, $index, 3) eq ($quote x 3) ? 3 : 1;
      my $start = $index;
      my $content_start = $index + $delimiter_length;
      $index = $content_start;
      my $content_end = -1;
      while ($index < $length) {
        if ($delimiter_length == 3) {
          if (substr($source, $index, 3) eq ($quote x 3)) {
            $content_end = $index;
            $index += 3;
            last;
          }
          $index++;
        } else {
          if (substr($source, $index, 1) eq '\\') {
            $index += 2;
            next;
          }
          if (substr($source, $index, 1) eq $quote) {
            $content_end = $index;
            $index++;
            last;
          }
          $index++;
        }
      }
      die "unterminated string literal in $file\n" if $content_end < 0;
      my $value = substr($source, $content_start, $content_end - $content_start);
      $value =~ s/\\([\\"'])/$1/g if $delimiter_length == 1;
      push @tokens, {
        type => 'string', value => $value, start => $start, end => $index,
        content_start => $content_start, content_end => $content_end,
      };
      next;
    }
    if ($char =~ /[A-Za-z_]/) {
      my $start = $index;
      $index++ while $index < $length && substr($source, $index, 1) =~ /[A-Za-z0-9_]/;
      push @tokens, { type => 'identifier', value => substr($source, $start, $index - $start), start => $start, end => $index };
      next;
    }
    push @tokens, { type => 'symbol', value => $char, start => $index, end => $index + 1 };
    $index++;
  }
  return \@tokens;
}

sub token_depths {
  my ($tokens) = @_;
  my @depths;
  my $depth = 0;
  for (my $index = 0; $index < @$tokens; $index++) {
    $depths[$index] = $depth;
    my $value = $tokens->[$index]{value};
    if ($tokens->[$index]{type} eq 'symbol' && $value eq '{') {
      $depth++;
    } elsif ($tokens->[$index]{type} eq 'symbol' && $value eq '}') {
      $depth--;
      die "unbalanced closing brace in $file\n" if $depth < 0;
    }
  }
  die "unbalanced opening brace in $file\n" if $depth != 0;
  return \@depths;
}

sub matching_symbol {
  my ($tokens, $opening_index, $opening, $closing) = @_;
  my $depth = 0;
  for (my $index = $opening_index; $index < @$tokens; $index++) {
    next unless $tokens->[$index]{type} eq 'symbol';
    my $value = $tokens->[$index]{value};
    $depth++ if $value eq $opening;
    if ($value eq $closing) {
      $depth--;
      return $index if $depth == 0;
    }
  }
  die "unbalanced $opening in $file\n";
}

sub named_blocks {
  my ($source, $name, $required_depth) = @_;
  my $tokens = tokenise($source);
  my $depths = token_depths($tokens);
  my @blocks;
  for (my $index = 0; $index + 1 < @$tokens; $index++) {
    next unless $tokens->[$index]{type} eq 'identifier' && $tokens->[$index]{value} eq $name;
    next unless $tokens->[$index + 1]{type} eq 'symbol' && $tokens->[$index + 1]{value} eq '{';
    next if defined($required_depth) && $depths->[$index] != $required_depth;
    my $close = matching_symbol($tokens, $index + 1, '{', '}');
    push @blocks, {
      tokens => $tokens, depths => $depths, name_index => $index,
      open_index => $index + 1, close_index => $close,
      depth => $depths->[$index], open_pos => $tokens->[$index + 1]{start},
      open_end => $tokens->[$index + 1]{end}, close_pos => $tokens->[$close]{start},
      close_end => $tokens->[$close]{end},
    };
  }
  return \@blocks;
}

sub parse_plugin_id {
  my ($tokens, $index, $limit) = @_;
  return undef unless $tokens->[$index]{type} eq 'identifier' && $tokens->[$index]{value} eq 'id';
  return undef if $index > 0 && $tokens->[$index - 1]{type} eq 'symbol' && $tokens->[$index - 1]{value} eq '.';
  my $cursor = $index + 1;
  my ($id_token, $id_end);
  if ($cursor < $limit && $tokens->[$cursor]{type} eq 'symbol' && $tokens->[$cursor]{value} eq '(') {
    return undef unless $cursor + 2 < $limit && $tokens->[$cursor + 1]{type} eq 'string' &&
      $tokens->[$cursor + 2]{type} eq 'symbol' && $tokens->[$cursor + 2]{value} eq ')';
    $id_token = $tokens->[$cursor + 1];
    $id_end = $tokens->[$cursor + 2]{end};
    $cursor += 3;
  } elsif ($cursor < $limit && $tokens->[$cursor]{type} eq 'string') {
    $id_token = $tokens->[$cursor];
    $id_end = $tokens->[$cursor]{end};
    $cursor++;
  } else {
    return undef;
  }
  my $version_token;
  my $version_keyword = 0;
  if ($cursor < $limit && $tokens->[$cursor]{type} eq 'identifier' && $tokens->[$cursor]{value} eq 'version') {
    $version_keyword = 1;
    $cursor++;
    if ($cursor < $limit && $tokens->[$cursor]{type} eq 'symbol' && $tokens->[$cursor]{value} eq '(') {
      if ($cursor + 2 < $limit && $tokens->[$cursor + 1]{type} eq 'string' &&
          $tokens->[$cursor + 2]{type} eq 'symbol' && $tokens->[$cursor + 2]{value} eq ')') {
        $version_token = $tokens->[$cursor + 1];
      }
    } elsif ($cursor < $limit && $tokens->[$cursor]{type} eq 'string') {
      $version_token = $tokens->[$cursor];
    }
  }
  return {
    id => $id_token->{value}, id_string_start => $id_token->{start}, id_end => $id_end,
    version => $version_token, unsupported_version => $version_keyword && !$version_token,
  };
}

sub plugin_declarations {
  my ($source, $top_level_only) = @_;
  my $blocks = named_blocks($source, 'plugins', $top_level_only ? 0 : undef);
  my @declarations;
  my @aliases;
  for my $block (@$blocks) {
    my $tokens = $block->{tokens};
    my $depths = $block->{depths};
    my %used_id_strings;
    for (my $index = $block->{open_index} + 1; $index < $block->{close_index}; $index++) {
      next unless $depths->[$index] == $block->{depth} + 1;
      my $declaration = parse_plugin_id($tokens, $index, $block->{close_index});
      if ($declaration) {
        if ($declaration->{id} eq $plugin_id && $declaration->{unsupported_version}) {
          die "non-literal Jank Hunter plugin version in $file cannot be updated safely\n";
        }
        $declaration->{block} = $block;
        push @declarations, $declaration;
        $used_id_strings{$declaration->{id_string_start}} = 1;
        next;
      }
      if ($tokens->[$index]{type} eq 'identifier' && $tokens->[$index]{value} eq 'alias' &&
          $index + 1 < $block->{close_index} && $tokens->[$index + 1]{type} eq 'symbol' &&
          $tokens->[$index + 1]{value} eq '(') {
        my $close = matching_symbol($tokens, $index + 1, '(', ')');
        die "alias(...) escapes plugins block in $file\n" if $close >= $block->{close_index};
        my $expression = substr($source, $tokens->[$index + 1]{end}, $tokens->[$close]{start} - $tokens->[$index + 1]{end});
        my $normalized = lc($expression);
        $normalized =~ s/[^a-z0-9]//g;
        push @aliases, $normalized;
      }
    }
    for (my $index = $block->{open_index} + 1; $index < $block->{close_index}; $index++) {
      next unless $tokens->[$index]{type} eq 'string' && $tokens->[$index]{value} eq $plugin_id;
      next if $used_id_strings{$tokens->[$index]{start}};
      die "unsupported or ambiguous Jank Hunter plugin declaration in $file\n";
    }
  }
  return ($blocks, \@declarations, \@aliases);
}

sub jh_declarations {
  my ($source, $top_level_only) = @_;
  my ($blocks, $declarations, $aliases) = plugin_declarations($source, $top_level_only);
  my @jh = grep { $_->{id} eq $plugin_id } @$declarations;
  return ($blocks, \@jh, $aliases);
}

sub replace_plugin_versions {
  my ($source, $add_missing, $top_level_only) = @_;
  my $version = $ENV{JH_VERSION} // die "JH_VERSION is required\n";
  my (undef, $declarations, undef) = jh_declarations($source, $top_level_only);
  my @replacements;
  for my $declaration (@$declarations) {
    if ($declaration->{version}) {
      push @replacements, [$declaration->{version}{content_start}, $declaration->{version}{content_end}, $version];
    } elsif ($add_missing) {
      push @replacements, [$declaration->{id_end}, $declaration->{id_end}, qq{ version "$version"}];
    }
  }
  for my $replacement (sort { $b->[0] <=> $a->[0] } @replacements) {
    substr($source, $replacement->[0], $replacement->[1] - $replacement->[0], $replacement->[2]);
  }
  return $source;
}

sub exact_marker_count {
  my ($source, $marker) = @_;
  my $count = 0;
  while ($source =~ /^[ \t]*\Q$marker\E[ \t]*\r?$/mg) { $count++; }
  return $count;
}

sub remove_managed_block {
  my ($source, $begin, $end) = @_;
  return $source unless exact_marker_count($source, $begin);
  my $removed = ($source =~ s{(?:\r?\n){0,2}^[ \t]*\Q$begin\E[ \t]*\r?\n.*?^[ \t]*\Q$end\E[ \t]*(?:\r?\n|\z)}{}ms);
  die "could not remove validated managed block $begin in $file\n" unless $removed;
  return $source;
}

sub migrate_legacy_helper {
  my ($source) = @_;
  my $marker = '// Jank Hunter optional OkHttp/WebSocket helper';
  my @markers;
  while ($source =~ /^[ \t]*\Q$marker\E[ \t]*\r?(?:\n|\z)/mg) {
    push @markers, [$-[0], $+[0]];
  }
  die "multiple legacy Jank Hunter helper markers in $file\n" if @markers > 1;
  return $source unless @markers;
  my ($marker_start, $marker_end) = @{$markers[0]};
  my $tokens = tokenise($source);
  my $opening_index = -1;
  for (my $index = 0; $index + 1 < @$tokens; $index++) {
    next if $tokens->[$index]{start} < $marker_end;
    die "legacy helper marker is not followed by dependencies { ... } in $file\n"
      unless $tokens->[$index]{type} eq 'identifier' && $tokens->[$index]{value} eq 'dependencies' &&
        $tokens->[$index + 1]{type} eq 'symbol' && $tokens->[$index + 1]{value} eq '{';
    $opening_index = $index + 1;
    last;
  }
  die "legacy helper marker has no dependencies block in $file\n" if $opening_index < 0;
  my $closing_index = matching_symbol($tokens, $opening_index, '{', '}');
  my $block_start = $tokens->[$opening_index - 1]{start};
  my $block_end = $tokens->[$closing_index]{end};
  my $between = substr($source, $marker_end, $block_start - $marker_end);
  my $block = substr($source, $block_start, $block_end - $block_start);
  my $group = quotemeta($ENV{JH_GROUP} // die "JH_GROUP is required\n");
  my @lines = split(/(?<=\n)/, $block, -1);
  my @kept;
  for my $line (@lines) {
    if ($line =~ /^[ \t]*(?:[A-Za-z_][A-Za-z0-9_]*Implementation|add)\b[^\n]*["']$group:jankhunter-okhttp3:[^"']+["'][^\n]*(?:\n|\z)$/) {
      next;
    }
    push @kept, $line;
  }
  my $migrated = join('', @kept);
  if ($migrated =~ /["']$group:jankhunter-okhttp3:[^"']+["']/) {
    die "legacy helper dependency uses unsupported multiline/custom syntax in $file; migrate it manually\n";
  }
  substr($source, $marker_start, $block_end - $marker_start, $between . $migrated);
  return $source;
}

sub top_level_block {
  my ($source, $name) = @_;
  my $blocks = named_blocks($source, $name, 0);
  die "multiple top-level $name blocks in $file\n" if @$blocks > 1;
  return @$blocks ? $blocks->[0] : undef;
}

sub insert_settings_block {
  my ($source, $name, $block, $fallback) = @_;
  my $target = top_level_block($source, $name);
  if ($target) {
    substr($source, $target->{open_end}, 0, "\n$block");
  } else {
    $source = $name eq 'pluginManagement' ? $fallback . "\n\n" . $source : $source . "\n\n" . $fallback . "\n";
  }
  return $source;
}

sub inspect_aliases {
  my ($source) = @_;
  my ($blocks, $declarations, $aliases) = jh_declarations($source, 1);
  my $all_tokens = tokenise($source);
  for (my $index = 0; $index < @$all_tokens; $index++) {
    next unless $all_tokens->[$index]{type} eq 'string' && $all_tokens->[$index]{value} eq $plugin_id;
    my $start = $index > 6 ? $index - 6 : 0;
    my $prefix = join(' ', map { $all_tokens->[$_]{value} } $start .. $index - 1);
    if ($prefix =~ /(?:^|\s)apply\s+plugin\s*:\s*$/ ||
        $prefix =~ /(?:^|\s)apply\s*\(\s*plugin\s*=\s*$/ ||
        $prefix =~ /pluginManager\s*\.\s*apply\s*\(\s*$/) {
      die "legacy/programmatic Jank Hunter plugin application in $file cannot be versioned safely; migrate it to plugins { id(...) }\n";
    }
  }
  die "multiple top-level plugins blocks in $file\n" if @$blocks > 1;
  die "multiple literal Jank Hunter plugin declarations in $file\n" if @$declarations > 1;
  my %known = map { $_ => 1 } grep { length } split(/,/, $ENV{CATALOG_ALIASES} // '');
  my $jh_alias = $ENV{JH_CATALOG_ALIAS} // '';
  my $has_catalog = ($ENV{HAS_STANDARD_CATALOG} // '0') eq '1';
  my $jh_alias_count = 0;
  if (!@$declarations) {
    for my $alias (@$aliases) {
      if (length($jh_alias) && $alias eq $jh_alias) {
        $jh_alias_count++;
      } elsif ($alias =~ /^libsplugins/) {
        die "unknown libs.plugins alias in $file; cannot safely determine whether Jank Hunter is already applied\n"
          unless $has_catalog && $known{$alias};
      } else {
        die "custom plugin alias in $file cannot be resolved safely; apply Jank Hunter literally or use gradle/libs.versions.toml\n";
      }
    }
  } elsif (length($jh_alias)) {
    $jh_alias_count++ for grep { $_ eq $jh_alias } @$aliases;
  }
  die "Jank Hunter is applied both literally and through an alias in $file\n" if @$declarations && $jh_alias_count;
  die "multiple Jank Hunter plugin aliases in $file\n" if $jh_alias_count > 1;
  my $missing_version = @$declarations && !$declarations->[0]{version} ? 1 : 0;
  return (scalar(@$declarations), $jh_alias_count, $missing_version);
}

if ($command eq 'preflight-settings') {
  top_level_block($text, 'pluginManagement');
  top_level_block($text, 'dependencyResolutionManagement');
  my (undef, $declarations, undef) = jh_declarations($text, 0);
  my @versioned = grep { $_->{version} } @$declarations;
  print scalar(@versioned);
} elsif ($command eq 'preflight-central') {
  my (undef, $declarations, undef) = jh_declarations($text, 0);
  my @versioned = grep { $_->{version} } @$declarations;
  print scalar(@versioned);
} elsif ($command eq 'preflight-module') {
  my $legacy_marker = '// Jank Hunter optional OkHttp/WebSocket helper';
  my $legacy_count = exact_marker_count($text, $legacy_marker);
  my $managed_helper_count = exact_marker_count($text, '// Jank Hunter optional helper dependencies - BEGIN');
  my $managed_configuration_count = exact_marker_count($text, '// Jank Hunter integration managed configuration - BEGIN');
  my $sdk_dependency = $ENV{JH_SDK_DEPENDENCY} // '';
  my $current_sdk_dependency = 0;
  if ($managed_helper_count == 1 && length($sdk_dependency) &&
      $text =~ m{^[ \t]*// Jank Hunter optional helper dependencies - BEGIN[ \t]*\r?\n(.*?)^[ \t]*// Jank Hunter optional helper dependencies - END[ \t]*\r?$}ms) {
    $current_sdk_dependency = index($1, $sdk_dependency) >= 0 ? 1 : 0;
  }
  migrate_legacy_helper($text);
  my ($literal, $alias, $missing) = inspect_aliases($text);
  print "$literal|$alias|$missing|$legacy_count|$managed_helper_count|$managed_configuration_count|$current_sdk_dependency";
} elsif ($command eq 'patch-central') {
  print replace_plugin_versions($text, 0, 0);
} elsif ($command eq 'patch-settings') {
  my $plugin_begin = $ENV{JH_PLUGIN_REPOSITORY_BEGIN} // die "plugin begin marker is required\n";
  my $plugin_end = $ENV{JH_PLUGIN_REPOSITORY_END} // die "plugin end marker is required\n";
  my $dependency_begin = $ENV{JH_DEPENDENCY_REPOSITORY_BEGIN} // die "dependency begin marker is required\n";
  my $dependency_end = $ENV{JH_DEPENDENCY_REPOSITORY_END} // die "dependency end marker is required\n";
  $text = remove_managed_block($text, $plugin_begin, $plugin_end);
  $text = remove_managed_block($text, $dependency_begin, $dependency_end);
  $text =~ s{^[ \t]*// Jank Hunter plugin repository[ \t]*\r?\n[ \t]*repositories\s*\{\s*maven\s*\{[^{}]*\}\s*\}[ \t]*(?:\r?\n|\z)}{}msg;
  $text =~ s{^[ \t]*// Jank Hunter dependency repository[ \t]*\r?\n[ \t]*repositories\s*\{\s*maven\s*\{[^{}]*\}\s*\}[ \t]*(?:\r?\n|\z)}{}msg;
  $text = replace_plugin_versions($text, 0, 0);
  my $plugin_block = $ENV{PLUGIN_BLOCK} // die "PLUGIN_BLOCK is required\n";
  my $dependency_block = $ENV{DEPENDENCY_BLOCK} // die "DEPENDENCY_BLOCK is required\n";
  my $plugin_fallback = "pluginManagement {\n$plugin_block    repositories {\n        google()\n        mavenCentral()\n        gradlePluginPortal()\n    }\n}";
  my $dependency_fallback = "dependencyResolutionManagement {\n$dependency_block    repositories {\n        google()\n        mavenCentral()\n    }\n}";
  $text = insert_settings_block($text, 'pluginManagement', $plugin_block, $plugin_fallback);
  $text = insert_settings_block($text, 'dependencyResolutionManagement', $dependency_block, $dependency_fallback);
  print $text;
} elsif ($command eq 'patch-module') {
  my $configuration_begin = $ENV{JH_CONFIGURATION_BEGIN} // die "configuration begin marker is required\n";
  my $configuration_end = $ENV{JH_CONFIGURATION_END} // die "configuration end marker is required\n";
  my $dependencies_begin = $ENV{JH_DEPENDENCIES_BEGIN} // die "dependencies begin marker is required\n";
  my $dependencies_end = $ENV{JH_DEPENDENCIES_END} // die "dependencies end marker is required\n";
  my $preserve_helper = ($ENV{JH_PRESERVE_MANAGED_HELPER} // '0') eq '1';
  my $preserve_configuration = ($ENV{JH_PRESERVE_MANAGED_CONFIGURATION} // '0') eq '1';
  $text = remove_managed_block($text, $dependencies_begin, $dependencies_end) unless $preserve_helper;
  $text = remove_managed_block($text, $configuration_begin, $configuration_end) unless $preserve_configuration;
  $text = migrate_legacy_helper($text);
  my ($blocks, $declarations, $aliases) = jh_declarations($text, 1);
  die "multiple top-level plugins blocks in $file\n" if @$blocks > 1;
  die "multiple literal Jank Hunter plugin declarations in $file\n" if @$declarations > 1;
  my $alias_used = ($ENV{JH_ALIAS_USED} // '0') eq '1';
  die "Jank Hunter alias disappeared while patching $file\n" if $alias_used && !@$aliases;
  if (@$declarations) {
    my $add_missing = ($ENV{JH_ADD_LITERAL_VERSION} // '0') eq '1';
    $text = replace_plugin_versions($text, $add_missing, 1);
  } elsif (!$alias_used) {
    my $line = $ENV{PLUGIN_LINE} // die "PLUGIN_LINE is required\n";
    my $plugin_blocks = named_blocks($text, 'plugins', 0);
    die "multiple top-level plugins blocks in $file\n" if @$plugin_blocks > 1;
    if (@$plugin_blocks) {
      substr($text, $plugin_blocks->[0]{open_end}, 0, "\n$line\n");
    } else {
      $text = "plugins {\n$line\n}\n\n" . $text;
    }
  }
  if (!$preserve_helper && length($ENV{JH_HELPER_DEPENDENCIES_BLOCK} // '')) {
    my $dependency_block = $ENV{JH_HELPER_DEPENDENCIES_BLOCK};
    if ($preserve_configuration && $text =~ /^[ \t]*\Q$configuration_begin\E[ \t]*\r?$/m) {
      substr($text, $-[0], 0, $dependency_block);
    } else {
      $text .= $dependency_block;
    }
  }
  if (!$preserve_configuration) {
    $text .= ($ENV{JH_CONFIGURATION_BLOCK} // die "configuration block is required\n");
  }
  print $text;
} else {
  die "unknown gradle file tool command: $command\n";
}
PERL
}

version_catalog_tool() {
  local command="$1"
  local file="$2"
  perl - "$command" "$file" <<'PERL'
use strict;
use warnings;

my ($command, $file) = @ARGV;
open my $input, '<', $file or die "cannot read $file: $!\n";
local $/;
my $text = <$input>;
close $input;

sub without_comments {
  my ($source) = @_;
  my $result = $source;
  my $length = length($source);
  my $quote = '';
  my $escaped = 0;
  for (my $index = 0; $index < $length; $index++) {
    my $char = substr($source, $index, 1);
    if (length($quote)) {
      if ($escaped) { $escaped = 0; next; }
      if ($char eq '\\') { $escaped = 1; next; }
      $quote = '' if $char eq $quote;
      next;
    }
    if ($char eq q{"} || $char eq q{'}) { $quote = $char; next; }
    if ($char eq '#') {
      my $newline = index($source, "\n", $index);
      my $end = $newline < 0 ? $length : $newline;
      substr($result, $index, $end - $index, ' ' x ($end - $index));
      $index = $end - 1;
    }
  }
  return $result;
}

sub section_span {
  my ($masked, $name) = @_;
  pos($masked) = 0;
  return undef unless $masked =~ /^\s*\[\Q$name\E\]\s*$/mg;
  my $start = $+[0];
  my $end = length($masked);
  if ($masked =~ /^\s*\[[^]]+\]\s*$/mg) {
    $end = $-[0] if $-[0] >= $start;
  }
  return [$start, $end];
}

sub parse_catalog {
  my ($source) = @_;
  my $masked = without_comments($source);
  my $plugins_span = section_span($masked, 'plugins');
  return { aliases => [] } unless $plugins_span;
  my $plugins = substr($masked, $plugins_span->[0], $plugins_span->[1] - $plugins_span->[0]);
  my @entries;
  while ($plugins =~ /^\s*([A-Za-z0-9_.-]+)\s*=\s*\{(.*?)\}\s*$/msg) {
    my ($key, $body) = ($1, $2);
    my $entry_start = $plugins_span->[0] + $-[0];
    my $body_start = $plugins_span->[0] + $-[2];
    next unless $body =~ /\bid\s*=\s*["']io[.]jankhunter[.]android["']/;
    my $mode = '';
    my ($value_start, $value_end, $ref_key);
    if ($body =~ /\bversion\s*=\s*["']([^"']+)["']/) {
      $mode = 'inline';
      $value_start = $body_start + $-[1];
      $value_end = $body_start + $+[1];
    } elsif ($body =~ /\bversion[.]ref\s*=\s*["']([^"']+)["']/) {
      $mode = 'ref';
      $ref_key = $1;
    } elsif ($body =~ /\bversion\s*=\s*\{\s*ref\s*=\s*["']([^"']+)["']\s*\}/) {
      $mode = 'ref';
      $ref_key = $1;
    } else {
      die "Jank Hunter version-catalog plugin '$key' has unsupported version syntax in $file\n";
    }
    push @entries, { key => $key, mode => $mode, value_start => $value_start, value_end => $value_end, ref => $ref_key };
  }
  die "multiple Jank Hunter plugin aliases in $file\n" if @entries > 1;

  my @all_aliases;
  while ($plugins =~ /^\s*([A-Za-z0-9_.-]+)\s*=\s*\{/mg) {
    my $normalized = lc($1);
    $normalized =~ s/[^a-z0-9]//g;
    push @all_aliases, "libsplugins$normalized";
  }

  if (@entries && $entries[0]{mode} eq 'ref') {
    my $versions_span = section_span($masked, 'versions')
      or die "version.ref '$entries[0]{ref}' has no [versions] section in $file\n";
    my $versions = substr($masked, $versions_span->[0], $versions_span->[1] - $versions_span->[0]);
    my $key = quotemeta($entries[0]{ref});
    my @matches;
    while ($versions =~ /^\s*$key\s*=\s*["']([^"']+)["']\s*$/mg) {
      push @matches, [$versions_span->[0] + $-[1], $versions_span->[0] + $+[1]];
    }
    die "version.ref '$entries[0]{ref}' is missing or duplicated in [versions] of $file\n" unless @matches == 1;
    my $ref = quotemeta($entries[0]{ref});
    my $usage_count = () = $masked =~ /\bversion[.]ref\s*=\s*["']$ref["']/g;
    my $nested_usage = () = $masked =~ /\bversion\s*=\s*\{\s*ref\s*=\s*["']$ref["']/g;
    die "version.ref '$entries[0]{ref}' is shared; refusing to change unrelated catalog entries in $file\n"
      unless $usage_count + $nested_usage == 1;
    ($entries[0]{value_start}, $entries[0]{value_end}) = @{$matches[0]};
  }
  return { aliases => \@all_aliases, entry => @entries ? $entries[0] : undef };
}

my $catalog = parse_catalog($text);
if ($command eq 'inspect') {
  print "ALIAS\t$_\n" for @{$catalog->{aliases}};
  if ($catalog->{entry}) {
    my $normalized = lc($catalog->{entry}{key});
    $normalized =~ s/[^a-z0-9]//g;
    print "JH\t$catalog->{entry}{key}\tlibsplugins$normalized\t$catalog->{entry}{mode}\n";
  }
} elsif ($command eq 'patch') {
  die "no Jank Hunter plugin alias in $file\n" unless $catalog->{entry};
  my $version = $ENV{JH_VERSION} // die "JH_VERSION is required\n";
  substr($text, $catalog->{entry}{value_start}, $catalog->{entry}{value_end} - $catalog->{entry}{value_start}, $version);
  print $text;
} else {
  die "unknown version catalog command: $command\n";
}
PERL
}

patch_settings_file() {
  local file="$1"
  local plugin_block dependency_block
  local plugin_begin="// Jank Hunter integration managed plugin repository - BEGIN"
  local plugin_end="// Jank Hunter integration managed plugin repository - END"
  local dependency_begin="// Jank Hunter integration managed dependency repository - BEGIN"
  local dependency_end="// Jank Hunter integration managed dependency repository - END"
  local maven_repo_path
  maven_repo_path="$(gradle_escape_string "$MAVEN_DIR")"
  plugin_block="    $plugin_begin
    repositories {
        maven { url = uri(\"$maven_repo_path\") }
    }
    $plugin_end
"
  dependency_block="    $dependency_begin
    repositories {
        maven { url = uri(\"$maven_repo_path\") }
    }
    $dependency_end
"

  validate_managed_block "$file" "plugin repository" "$plugin_begin" "$plugin_end"
  validate_managed_block "$file" "dependency repository" "$dependency_begin" "$dependency_end"

  backup_file "$file"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would patch settings repositories in $file"
    return
  fi

  copy_to_temp "$file"
  local temp="$LAST_TEMP_FILE"
  local transformed
  make_temp_for "$file"
  transformed="$LAST_TEMP_FILE"
  cp -p "$temp" "$transformed"
  if ! PLUGIN_BLOCK="$plugin_block" DEPENDENCY_BLOCK="$dependency_block" JH_VERSION="$VERSION" \
    JH_PLUGIN_REPOSITORY_BEGIN="$plugin_begin" JH_PLUGIN_REPOSITORY_END="$plugin_end" \
    JH_DEPENDENCY_REPOSITORY_BEGIN="$dependency_begin" JH_DEPENDENCY_REPOSITORY_END="$dependency_end" \
    gradle_file_tool patch-settings "$temp" > "$transformed"; then
    fail "could not safely patch settings file: $file"
  fi
  atomic_replace "$transformed" "$file"
}

patch_gitignore() {
  [[ "$ADD_GITIGNORE" -eq 1 ]] || return 0
  local file="$TARGET_ROOT/.gitignore"
  local entries=(".jankhunter/" ".jankhunter-backups/" "local.properties")
  local path escaped entry found
  for path in "$MAVEN_DIR" "$CLI_DIR"; do
    case "$path" in
      .jankhunter|.jankhunter/*)
        continue
        ;;
    esac
    escaped="$(perl -e 'my $value = shift; $value =~ s/([\\*?\[\]#! ])/\\$1/g; print "/$value/"' "$path")"
    entries+=("$escaped")
  done
  local missing=()
  for entry in "${entries[@]}"; do
    found=0
    if [[ -f "$file" ]] && grep -Fxq "$entry" "$file"; then
      found=1
    fi
    [[ "$found" -eq 1 ]] || missing+=("$entry")
  done
  [[ "${#missing[@]}" -gt 0 ]] || return 0
  backup_file "$file"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would update $file"
    return
  fi
  copy_to_temp "$file"
  local temp="$LAST_TEMP_FILE"
  {
    [[ -f "$file" ]] && cat "$file"
    printf '\n# Jank Hunter generated local files\n'
    for entry in "${missing[@]}"; do
      printf '%s\n' "$entry"
    done
  } > "$temp"
  atomic_replace "$temp" "$file"
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
  elif [[ -n "$target_existing" && -d "$target_existing" ]]; then
    candidate="$target_existing"
  elif [[ -n "$jankhunter_existing" && -d "$jankhunter_existing" ]]; then
    candidate="$jankhunter_existing"
  elif [[ -n "${HOME:-}" && -d "$HOME/Library/Android/sdk" ]]; then
    candidate="$HOME/Library/Android/sdk"
  elif [[ -n "${HOME:-}" && -d "$HOME/Android/Sdk" ]]; then
    candidate="$HOME/Android/Sdk"
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

  local sdk_dir
  sdk_dir="$(resolve_android_sdk_dir "$current")"
  if [[ -n "$current" && -d "$current" ]]; then
    local current_resolved
    current_resolved="$(cd "$current" && pwd -P)"
    if [[ "$current_resolved" == "$sdk_dir" ]]; then
      log "$label local.properties already contains sdk.dir=$current"
      return
    fi
  fi
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
  copy_to_temp "$file"
  local temp="$LAST_TEMP_FILE"
  if [[ -f "$file" ]] && grep -Eq '^[[:space:]]*sdk\.dir[[:space:]]*=' "$file"; then
    SDK_DIR_VALUE="$escaped_sdk_dir" perl -0pi -e '
      my $value = $ENV{"SDK_DIR_VALUE"};
      s/^[ \t]*sdk\.dir[ \t]*=.*$/sdk.dir=$value/m;
    ' "$temp"
  else
    {
      [[ -f "$file" ]] && cat "$file"
      [[ -f "$file" ]] && printf '\n'
      printf 'sdk.dir=%s\n' "$escaped_sdk_dir"
    } > "$temp"
  fi
  atomic_replace "$temp" "$file"
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
  local module_index="${3:-0}"
  local dsl="groovy"
  [[ "$file" == *.kts ]] && dsl="kts"

  local plugin_line jh_block helper_dependencies_block includes excludes build_types
  local configuration_begin="// Jank Hunter integration managed configuration - BEGIN"
  local configuration_end="// Jank Hunter integration managed configuration - END"
  local dependencies_begin="// Jank Hunter optional helper dependencies - BEGIN"
  local dependencies_end="// Jank Hunter optional helper dependencies - END"
  local okhttp_value="false"
  local websocket_value="false"
  local runtime_call_graph_value="false"
  local asm_progress_value="false"
  [[ "$OKHTTP_HOOKS" -eq 1 ]] && okhttp_value="true"
  [[ "$WEBSOCKET_HOOKS" -eq 1 ]] && websocket_value="true"
  [[ "$RUNTIME_CALL_GRAPH" -eq 1 ]] && runtime_call_graph_value="true"
  [[ "$ASM_PROGRESS_LOG" -eq 1 ]] && asm_progress_value="true"
  if [[ "$dsl" == "kts" ]]; then
    plugin_line="    id(\"io.jankhunter.android\") version \"$VERSION\""
  else
    plugin_line="    id 'io.jankhunter.android' version '$VERSION'"
  fi

  local has_top_level_configuration=0
  local has_instrument_configuration=0
  [[ "$BUILD_TYPES_EXPLICIT" -eq 1 || "$DI_ANALYSIS" -ge 0 || "$SESSION_LOG_SIZE_LIMIT" -ge 0 ]] &&
    has_top_level_configuration=1
  [[ "$OKHTTP_HOOKS" -ge 0 || "$WEBSOCKET_HOOKS" -ge 0 || "$RUNTIME_CALL_GRAPH" -ge 0 ||
    "$ASM_PROGRESS_LOG" -ge 0 || "${#INCLUDE_PACKAGES[@]}" -gt 0 || "${#EXCLUDE_PACKAGES[@]}" -gt 0 ]] &&
    has_instrument_configuration=1

  jh_block=""
  if [[ "$has_top_level_configuration" -eq 1 || "$has_instrument_configuration" -eq 1 ]]; then
    jh_block=$'\n\n'
    jh_block+="$configuration_begin"$'\n'
    jh_block+=$'jankHunter {\n'
    if [[ "$BUILD_TYPES_EXPLICIT" -eq 1 ]]; then
      build_types="$(gradle_string_args "${BUILD_TYPES[@]}")"
      if [[ "$dsl" == "kts" ]]; then
        jh_block+="    enabledBuildTypes.set(setOf($build_types))"$'\n'
      else
        jh_block+="    enabledBuildTypes.set([$build_types])"$'\n'
      fi
    fi
    if [[ "$DI_ANALYSIS" -ge 0 ]]; then
      if [[ "$DI_ANALYSIS" -eq 1 ]]; then
        jh_block+=$'    dependencyInjectionAnalysis = io.jankhunter.gradle.JankHunterFeatureMode.ENABLED\n'
      else
        jh_block+=$'    dependencyInjectionAnalysis = io.jankhunter.gradle.JankHunterFeatureMode.DISABLED\n'
      fi
    fi
    if [[ "$SESSION_LOG_SIZE_LIMIT" -ge 0 ]]; then
      if [[ "$SESSION_LOG_SIZE_LIMIT" -eq 1 ]]; then
        jh_block+=$'    sessionLogSizeLimitEnabled = true\n'
        jh_block+="    maxSessionLogSizeMiB = $MAX_SESSION_LOG_SIZE_MIB"$'\n'
      else
        jh_block+=$'    sessionLogSizeLimitEnabled = false\n'
      fi
    fi
    if [[ "$has_instrument_configuration" -eq 1 ]]; then
      [[ "$has_top_level_configuration" -eq 0 ]] || jh_block+=$'\n'
      jh_block+=$'    instrument {\n'
      [[ "$OKHTTP_HOOKS" -lt 0 ]] || jh_block+="        okhttp = $okhttp_value"$'\n'
      [[ "$WEBSOCKET_HOOKS" -lt 0 ]] || jh_block+="        webSockets = $websocket_value"$'\n'
      [[ "$RUNTIME_CALL_GRAPH" -lt 0 ]] || jh_block+="        runtimeCallGraph = $runtime_call_graph_value"$'\n'
      [[ "$ASM_PROGRESS_LOG" -lt 0 ]] || jh_block+="        asmProgressLog = $asm_progress_value"$'\n'
      if [[ "${#INCLUDE_PACKAGES[@]}" -gt 0 ]]; then
        includes="$(gradle_string_args "${INCLUDE_PACKAGES[@]}")"
        if [[ "$dsl" == "kts" ]]; then
          jh_block+="        includePackages.set(setOf($includes))"$'\n'
        else
          jh_block+="        includePackages.set([$includes])"$'\n'
        fi
      fi
      if [[ "${#EXCLUDE_PACKAGES[@]}" -gt 0 ]]; then
        excludes="$(gradle_string_args "${EXCLUDE_PACKAGES[@]}")"
        if [[ "$dsl" == "kts" ]]; then
          jh_block+="        excludePackages.set(setOf($excludes))"$'\n'
        else
          jh_block+="        excludePackages.set([$excludes])"$'\n'
        fi
      fi
      jh_block+=$'    }\n'
    fi
    jh_block+=$'}\n'
    jh_block+="$configuration_end"$'\n'
  fi

  # One public dependency is the integration contract. It exposes runtime, annotations and
  # OkHttp/WebSocket support transitively and replaces older per-build-type helper blocks.
  helper_dependencies_block=$'\n\n'
  helper_dependencies_block+="$dependencies_begin"$'\n'
  helper_dependencies_block+=$'dependencies {\n'
  helper_dependencies_block+="    implementation(\"$GROUP:jankhunter-android-sdk:$VERSION\")"$'\n'
  helper_dependencies_block+=$'}\n'
  helper_dependencies_block+="$dependencies_end"$'\n'

  local preserve_managed_helper="${MODULE_CURRENT_SDK_DEPENDENCY[$module_index]:-0}"

  validate_managed_block "$file" "module configuration" "$configuration_begin" "$configuration_end"
  validate_managed_block "$file" "optional helper dependencies" "$dependencies_begin" "$dependencies_end"
  backup_file "$file"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would patch module $module build file: $file"
    return
  fi

  local alias_used="${MODULE_USES_JH_ALIAS[$module_index]:-0}"
  local add_literal_version="${MODULE_ADD_LITERAL_VERSION[$module_index]:-0}"
  local preserve_managed_configuration=0
  if [[ "${MODULE_MANAGED_CONFIGURATION[$module_index]:-0}" -eq 1 && -z "$jh_block" ]]; then
    preserve_managed_configuration=1
  elif [[ "${MODULE_MANAGED_CONFIGURATION[$module_index]:-0}" -eq 1 && -n "$jh_block" ]]; then
    log "module $module: explicit DSL options replace the previous script-managed overlay; user-owned jankHunter blocks are preserved"
  fi
  copy_to_temp "$file"
  local temp="$LAST_TEMP_FILE"
  local transformed
  make_temp_for "$file"
  transformed="$LAST_TEMP_FILE"
  cp -p "$temp" "$transformed"
  if ! JH_GROUP="$GROUP" PLUGIN_LINE="$plugin_line" JH_VERSION="$VERSION" \
    JH_ALIAS_USED="$alias_used" JH_ADD_LITERAL_VERSION="$add_literal_version" \
    JH_PRESERVE_MANAGED_HELPER="$preserve_managed_helper" \
    JH_PRESERVE_MANAGED_CONFIGURATION="$preserve_managed_configuration" \
    JH_CONFIGURATION_BLOCK="$jh_block" JH_HELPER_DEPENDENCIES_BLOCK="$helper_dependencies_block" \
    JH_CONFIGURATION_BEGIN="$configuration_begin" JH_CONFIGURATION_END="$configuration_end" \
    JH_DEPENDENCIES_BEGIN="$dependencies_begin" JH_DEPENDENCIES_END="$dependencies_end" \
    gradle_file_tool patch-module "$temp" > "$transformed"; then
    fail "could not safely patch module $module build file: $file"
  fi
  atomic_replace "$transformed" "$file"
}

patch_central_plugin_versions() {
  local file="$1"
  [[ -n "$file" ]] || return 0
  backup_file "$file"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would update centralized Jank Hunter plugin version in $file"
    return 0
  fi
  copy_to_temp "$file"
  local temp="$LAST_TEMP_FILE"
  local transformed
  make_temp_for "$file"
  transformed="$LAST_TEMP_FILE"
  cp -p "$temp" "$transformed"
  if ! JH_VERSION="$VERSION" gradle_file_tool patch-central "$temp" > "$transformed"; then
    fail "could not safely update centralized Jank Hunter plugin version in $file"
  fi
  atomic_replace "$transformed" "$file"
}

patch_version_catalog() {
  [[ "$USES_JH_CATALOG" -eq 1 ]] || return 0
  backup_file "$CATALOG_FILE"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "would update Jank Hunter plugin alias version in $CATALOG_FILE"
    return 0
  fi
  copy_to_temp "$CATALOG_FILE"
  local temp="$LAST_TEMP_FILE"
  local transformed
  make_temp_for "$CATALOG_FILE"
  transformed="$LAST_TEMP_FILE"
  cp -p "$temp" "$transformed"
  if ! JH_VERSION="$VERSION" version_catalog_tool patch "$temp" > "$transformed"; then
    fail "could not safely update Jank Hunter version catalog entry in $CATALOG_FILE"
  fi
  atomic_replace "$transformed" "$CATALOG_FILE"
}

publish_artifacts_if_needed() {
  local sdk_aar="$MAVEN_REPO_ABS/$GROUP_PATH/jankhunter-android-sdk/$VERSION/jankhunter-android-sdk-$VERSION.aar"
  local runtime_aar="$MAVEN_REPO_ABS/$GROUP_PATH/jankhunter-runtime/$VERSION/jankhunter-runtime-$VERSION.aar"
  local annotations_jar="$MAVEN_REPO_ABS/$GROUP_PATH/jankhunter-annotations/$VERSION/jankhunter-annotations-$VERSION.jar"
  local okhttp_aar="$MAVEN_REPO_ABS/$GROUP_PATH/jankhunter-okhttp3/$VERSION/jankhunter-okhttp3-$VERSION.aar"
  local plugin_jar="$MAVEN_REPO_ABS/$GROUP_PATH/jankhunter-gradle-plugin/$VERSION/jankhunter-gradle-plugin-$VERSION.jar"
  local plugin_pom="$MAVEN_REPO_ABS/io/jankhunter/android/io.jankhunter.android.gradle.plugin/$VERSION/io.jankhunter.android.gradle.plugin-$VERSION.pom"

  if [[ "$SKIP_PUBLISH" -eq 1 ]]; then
    log "using existing Jank Hunter artifacts from $MAVEN_REPO_ABS"
    [[ "$DRY_RUN" -eq 1 ]] && return 0
    [[ -f "$sdk_aar" ]] || fail "--skip-publish requires existing artifact: $sdk_aar"
    [[ -f "$runtime_aar" ]] || fail "--skip-publish requires existing artifact: $runtime_aar"
    [[ -f "$annotations_jar" ]] || fail "--skip-publish requires existing artifact: $annotations_jar"
    [[ -f "$plugin_jar" ]] || fail "--skip-publish requires existing artifact: $plugin_jar"
    [[ -f "$plugin_pom" ]] || fail "--skip-publish requires existing plugin marker: $plugin_pom"
    [[ -f "$okhttp_aar" ]] || fail "--skip-publish requires existing artifact: $okhttp_aar"
    return 0
  fi

  if [[ -f "$sdk_aar" && -f "$runtime_aar" && -f "$annotations_jar" && -f "$okhttp_aar" && -f "$plugin_pom" ]]; then
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

build_cli_if_needed() {
  local source_binary="$JANKHUNTER_ROOT/cli/bin/jankhunter"
  local target_binary="$CLI_DIR_ABS/jankhunter"

  if [[ "$SKIP_CLI_BUILD" -eq 1 ]]; then
    log "using existing Jank Hunter CLI: $target_binary"
    [[ "$DRY_RUN" -eq 1 ]] && return 0
    [[ -x "$target_binary" ]] ||
      fail "--skip-cli-build requires an existing executable CLI: $target_binary"
    return 0
  fi

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
  if [[ "$DRY_RUN" -eq 1 ]]; then
    log "dry-run: target Gradle verification is skipped because no files were changed"
    return 0
  fi
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

validate_configuration_inputs() {
  [[ "${#BUILD_TYPES[@]}" -gt 0 ]] || fail "at least one non-empty --build-type is required"
  local value lower
  for value in "${BUILD_TYPES[@]}"; do
    [[ "$value" =~ ^[A-Za-z][A-Za-z0-9_]*$ ]] || fail "invalid Android build type: $value"
    lower="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
    [[ "$lower" != "release" && "$lower" != *release ]] ||
      fail "release-like build type '$value' requires manual releaseSafety and performance evidence; configure it without this helper"
  done
  local package_values=()
  set +u
  package_values=("${INCLUDE_PACKAGES[@]}" "${EXCLUDE_PACKAGES[@]}")
  for value in "${package_values[@]}"; do
    [[ "$value" =~ ^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$ ]] ||
      fail "invalid Java/Kotlin package prefix: $value"
  done
  set -u
  if [[ "$SESSION_LOG_SIZE_LIMIT" -eq 1 ]]; then
    [[ "$MAX_SESSION_LOG_SIZE_MIB" =~ ^[1-9][0-9]*$ ]] ||
      fail "--max-session-log-size-mib must be a positive integer"
  fi
}

detect_root_build_file() {
  if [[ -f "$TARGET_ROOT/build.gradle.kts" || -L "$TARGET_ROOT/build.gradle.kts" ]]; then
    printf '%s\n' "$TARGET_ROOT/build.gradle.kts"
  elif [[ -f "$TARGET_ROOT/build.gradle" || -L "$TARGET_ROOT/build.gradle" ]]; then
    printf '%s\n' "$TARGET_ROOT/build.gradle"
  fi
}

load_version_catalog_metadata() {
  CATALOG_FILE=""
  CATALOG_JH_ALIAS=""
  CATALOG_ALIASES=""
  local candidate="$TARGET_ROOT/gradle/libs.versions.toml"
  if [[ ! -e "$candidate" && ! -L "$candidate" ]]; then
    return 0
  fi
  validate_target_file_path "version catalog" "$candidate"
  CATALOG_FILE="$candidate"
  local metadata=""
  if ! metadata="$(version_catalog_tool inspect "$CATALOG_FILE")"; then
    fail "cannot safely inspect version catalog: $CATALOG_FILE"
  fi
  local kind first second mode
  local aliases=""
  while IFS=$'\t' read -r kind first second mode; do
    [[ -n "$kind" ]] || continue
    case "$kind" in
      ALIAS)
        if [[ -n "$aliases" ]]; then
          aliases+=","
        fi
        aliases+="$first"
        ;;
      JH)
        [[ -z "$CATALOG_JH_ALIAS" ]] || fail "multiple Jank Hunter aliases reported by $CATALOG_FILE"
        CATALOG_JH_ALIAS="$second"
        ;;
    esac
  done <<< "$metadata"
  CATALOG_ALIASES="$aliases"
}

preflight_target_project() {
  command -v perl >/dev/null 2>&1 || fail "perl is required to patch Gradle files safely"
  command -v mktemp >/dev/null 2>&1 || fail "mktemp is required to patch target files atomically"

  ROOT_CENTRAL_PLUGIN_DECLARATIONS=0
  USES_JH_CATALOG=0
  validate_target_file_path "settings file" "$SETTINGS_FILE"
  if [[ -e "$TARGET_ROOT/local.properties" || -L "$TARGET_ROOT/local.properties" ]]; then
    validate_target_file_path "target local.properties" "$TARGET_ROOT/local.properties"
  fi
  if [[ -e "$TARGET_ROOT/.gitignore" || -L "$TARGET_ROOT/.gitignore" ]]; then
    validate_target_file_path "target .gitignore" "$TARGET_ROOT/.gitignore"
  fi

  local plugin_repository_begin="// Jank Hunter integration managed plugin repository - BEGIN"
  local plugin_repository_end="// Jank Hunter integration managed plugin repository - END"
  local dependency_repository_begin="// Jank Hunter integration managed dependency repository - BEGIN"
  local dependency_repository_end="// Jank Hunter integration managed dependency repository - END"
  validate_managed_block "$SETTINGS_FILE" "plugin repository" "$plugin_repository_begin" "$plugin_repository_end"
  validate_managed_block "$SETTINGS_FILE" "dependency repository" "$dependency_repository_begin" "$dependency_repository_end"

  local settings_central=0
  if ! settings_central="$(JH_VERSION="$VERSION" gradle_file_tool preflight-settings "$SETTINGS_FILE")"; then
    fail "settings Gradle structure is ambiguous or unsafe to patch: $SETTINGS_FILE"
  fi
  [[ "$settings_central" =~ ^[0-9]+$ ]] || fail "invalid settings preflight result: $settings_central"
  CENTRAL_PLUGIN_DECLARATIONS="$settings_central"

  ROOT_BUILD_FILE="$(detect_root_build_file)"
  if [[ -n "$ROOT_BUILD_FILE" ]]; then
    validate_target_file_path "root build file" "$ROOT_BUILD_FILE"
    local root_central=0
    if ! root_central="$(JH_VERSION="$VERSION" gradle_file_tool preflight-central "$ROOT_BUILD_FILE")"; then
      fail "root Gradle structure is ambiguous or unsafe to patch: $ROOT_BUILD_FILE"
    fi
    [[ "$root_central" =~ ^[0-9]+$ ]] || fail "invalid root build preflight result: $root_central"
    ROOT_CENTRAL_PLUGIN_DECLARATIONS="$root_central"
    CENTRAL_PLUGIN_DECLARATIONS=$((CENTRAL_PLUGIN_DECLARATIONS + root_central))
  fi

  load_version_catalog_metadata

  MODULE_USES_JH_ALIAS=()
  MODULE_ADD_LITERAL_VERSION=()
  MODULE_LEGACY_HELPER=()
  MODULE_MANAGED_HELPER=()
  MODULE_CURRENT_SDK_DEPENDENCY=()
  MODULE_MANAGED_CONFIGURATION=()
  local index file result literal alias missing legacy managed_helper managed_configuration current_sdk_dependency
  for ((index = 0; index < ${#MODULE_BUILD_FILES[@]}; index++)); do
    file="${MODULE_BUILD_FILES[$index]}"
    validate_target_file_path "module build file" "$file"
    validate_managed_block "$file" "module configuration" \
      "// Jank Hunter integration managed configuration - BEGIN" \
      "// Jank Hunter integration managed configuration - END"
    validate_managed_block "$file" "optional helper dependencies" \
      "// Jank Hunter optional helper dependencies - BEGIN" \
      "// Jank Hunter optional helper dependencies - END"
    if ! result="$(
      JH_GROUP="$GROUP" JH_SDK_DEPENDENCY="$GROUP:jankhunter-android-sdk:$VERSION" \
        JH_CATALOG_ALIAS="$CATALOG_JH_ALIAS" CATALOG_ALIASES="$CATALOG_ALIASES" \
        HAS_STANDARD_CATALOG="$([[ -n "$CATALOG_FILE" ]] && printf 1 || printf 0)" \
        gradle_file_tool preflight-module "$file"
    )"; then
      fail "module Gradle structure is ambiguous or unsafe to patch: $file"
    fi
    IFS='|' read -r literal alias missing legacy managed_helper managed_configuration current_sdk_dependency <<< "$result"
    [[ "$literal" =~ ^[0-9]+$ && "$alias" =~ ^[0-9]+$ && "$missing" =~ ^[01]$ &&
      "$legacy" =~ ^[01]$ && "$managed_helper" =~ ^[01]$ && "$managed_configuration" =~ ^[01]$ &&
      "$current_sdk_dependency" =~ ^[01]$ ]] ||
      fail "invalid module preflight result for $file: $result"
    MODULE_USES_JH_ALIAS+=("$alias")
    MODULE_LEGACY_HELPER+=("$legacy")
    MODULE_MANAGED_HELPER+=("$managed_helper")
    MODULE_CURRENT_SDK_DEPENDENCY+=("$current_sdk_dependency")
    MODULE_MANAGED_CONFIGURATION+=("$managed_configuration")
    if [[ "$missing" -eq 1 && "$CENTRAL_PLUGIN_DECLARATIONS" -eq 0 ]]; then
      MODULE_ADD_LITERAL_VERSION+=(1)
    else
      MODULE_ADD_LITERAL_VERSION+=(0)
    fi
    [[ "$alias" -eq 0 ]] || USES_JH_CATALOG=1
  done
  if [[ "$USES_JH_CATALOG" -eq 1 && -z "$CATALOG_FILE" ]]; then
    fail "Jank Hunter plugin alias is used but gradle/libs.versions.toml was not found"
  fi
}

begin_target_transaction() {
  [[ "$DRY_RUN" -eq 0 ]] || return 0
  mkdir -p "$BACKUP_BASE"
  local resolved_backup_base
  resolved_backup_base="$(cd "$BACKUP_BASE" && pwd -P)"
  path_is_inside_target "$resolved_backup_base" || fail "backup directory escaped the target project: $resolved_backup_base"
  BACKUP_ROOT="$(mktemp -d "$resolved_backup_base/$(date +%Y%m%d-%H%M%S)-$$.XXXXXX")" ||
    fail "cannot create a private backup directory inside $resolved_backup_base"
  TRANSACTION_ACTIVE=1
  trap on_exit EXIT
  transaction_register_file "$SETTINGS_FILE"
  local file
  for file in "${MODULE_BUILD_FILES[@]}"; do
    transaction_register_file "$file"
  done
  if [[ -n "$ROOT_BUILD_FILE" && "$ROOT_CENTRAL_PLUGIN_DECLARATIONS" -gt 0 ]]; then
    transaction_register_file "$ROOT_BUILD_FILE"
  fi
  if [[ "$USES_JH_CATALOG" -eq 1 ]]; then
    transaction_register_file "$CATALOG_FILE"
  fi
  if [[ "$SKIP_LOCAL_PROPERTIES" -eq 0 ]]; then
    transaction_register_file "$TARGET_ROOT/local.properties"
  fi
  if [[ "$ADD_GITIGNORE" -eq 1 ]]; then
    transaction_register_file "$TARGET_ROOT/.gitignore"
  fi
}

dedupe_array MODULES
dedupe_array INCLUDE_PACKAGES
dedupe_array EXCLUDE_PACKAGES
dedupe_array BUILD_TYPES
validate_configuration_inputs

if [[ "${#MODULES[@]}" -eq 0 ]]; then
  detected_module="$(detect_app_module)"
  MODULES=("$detected_module")
  log "detected Android application module: $detected_module"
fi

GROUP_PATH="$(printf '%s' "$GROUP" | tr '.' '/')"

SETTINGS_FILE="$(detect_settings_file)"
MODULE_BUILD_FILES=()
for module in "${MODULES[@]}"; do
  MODULE_BUILD_FILES+=("$(module_build_file "$module")")
done

preflight_target_project

log "Jank Hunter: $JANKHUNTER_ROOT"
log "Target: $TARGET_ROOT"
log "Version: $GROUP:$VERSION"
log "Artifact mode: local Maven"
log "Local Maven repo: $MAVEN_REPO_ABS"
log "CLI binary: $CLI_DIR_ABS/jankhunter"
log "Modules: ${MODULES[*]}"

if [[ "$SKIP_PUBLISH" -eq 0 ]]; then
  prepare_jankhunter_android_sdk
elif [[ "$SKIP_LOCAL_PROPERTIES" -eq 0 ]]; then
  resolve_android_sdk_dir "$(local_properties_sdk_dir || true)" >/dev/null
  log "Android SDK: $RESOLVED_ANDROID_SDK_DIR"
else
  log "Android SDK resolution skipped"
fi
begin_target_transaction
publish_artifacts_if_needed
build_cli_if_needed
patch_target_local_properties
if [[ -n "$ROOT_BUILD_FILE" && "$ROOT_CENTRAL_PLUGIN_DECLARATIONS" -gt 0 ]]; then
  patch_central_plugin_versions "$ROOT_BUILD_FILE"
fi
patch_version_catalog
patch_settings_file "$SETTINGS_FILE"
patch_gitignore

for ((module_index = 0; module_index < ${#MODULES[@]}; module_index++)); do
  module="${MODULES[$module_index]}"
  build_file="${MODULE_BUILD_FILES[$module_index]}"
  patch_module_build_file "$module" "$build_file" "$module_index"
done

verify_target_project
TRANSACTION_COMMITTED=1

log "done"
log "Backups: $BACKUP_ROOT"
log "Local Maven repo: $MAVEN_REPO_ABS"
log "CLI: $CLI_DIR_ABS/jankhunter"
log "Next: build the target app. The integrated CLI automatically selects the newest complete build/generated/jankhunter/<variant> bundle; use --artifacts-dir to select another variant explicitly."
