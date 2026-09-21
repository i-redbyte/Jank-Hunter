#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ANDROID_DIR="$ROOT_DIR/android"
GRADLE_PROPERTIES="$ANDROID_DIR/gradle.properties"

REPO_NAME="jankhunter"
VERSION_OVERRIDE=""
ANDROID_BUILD_TOOLS_VERSION="${ANDROID_BUILD_TOOLS_VERSION:-}"

usage() {
  cat <<'EOF'
Usage:
  scripts/publish-to-local-maven.sh [options] [REPO_NAME] [VERSION]

Publishes Jank Hunter Android SDK modules and the standalone Gradle plugin into a local
Maven repository (not ~/.m2/repository unless you pass that path as REPO_NAME).

Defaults:
  REPO_NAME   jankhunter — directory under the Jank Hunter repository root
  VERSION     jankHunterVersion from android/gradle.properties

Options:
  --name NAME       Local Maven repository directory name or absolute path.
  --version VER     Override jankHunterVersion for this publish.
  -h, --help        Show this help.

Environment:
  ANDROID_HOME / ANDROID_SDK_ROOT   Android SDK location.
  ANDROID_BUILD_TOOLS_VERSION       Build Tools version; defaults to the highest installed.
  JAVA_HOME                         JDK 17+ for Gradle.

Examples:
  scripts/publish-to-local-maven.sh
  scripts/publish-to-local-maven.sh my-maven-cache 1.0.11
  scripts/publish-to-local-maven.sh --name "$HOME/work/MyApp/.jankhunter/maven"
EOF
}

log() {
  printf '[jankhunter-publish] %s\n' "$*"
}

fail() {
  printf '[jankhunter-publish] error: %s\n' "$*" >&2
  exit 1
}

require_value() {
  local option="$1"
  local value="${2:-}"
  [[ -n "$value" && "$value" != --* ]] || fail "$option requires a value"
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

validate_repo_name() {
  local name="$1"
  require_single_line "repository name" "$name"
  [[ "$name" != *".."* ]] || fail "repository name must not contain '..': $name"
  if [[ "$name" = /* ]]; then
    return 0
  fi
  [[ "$name" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]*$ ]] ||
    fail "repository name contains unsupported characters: $name"
}

properties_value() {
  local key="$1"
  local file="$2"
  awk -F= -v key="$key" '$1 == key { print $2; exit }' "$file"
}

read_default_version() {
  [[ -f "$GRADLE_PROPERTIES" ]] ||
    fail "gradle properties were not found: $GRADLE_PROPERTIES"
  local version
  version="$(properties_value jankHunterVersion "$GRADLE_PROPERTIES")"
  [[ -n "$version" ]] || fail "jankHunterVersion is not set in $GRADLE_PROPERTIES"
  printf '%s' "$version"
}

resolve_maven_repo_dir() {
  local name="$1"
  validate_repo_name "$name"
  local repo_dir=""
  if [[ "$name" = /* ]]; then
    repo_dir="$name"
  else
    repo_dir="$ROOT_DIR/$name"
  fi
  mkdir -p "$repo_dir"
  (cd "$repo_dir" && pwd -P)
}

local_properties_sdk_dir() {
  local file="$ANDROID_DIR/local.properties"
  [[ -f "$file" ]] || return 0
  awk '
    /^[[:space:]]*sdk[.]dir[[:space:]]*=/ {
      sub(/^[^=]*=/, "")
      gsub(/^[[:space:]]+|[[:space:]]+$/, "")
      print
      exit
    }
  ' "$file"
}

resolve_android_sdk_dir() {
  local candidate=""
  local local_properties_sdk=""
  local_properties_sdk="$(local_properties_sdk_dir || true)"
  if [[ -n "${ANDROID_HOME:-}" ]]; then
    candidate="$ANDROID_HOME"
  elif [[ -n "${ANDROID_SDK_ROOT:-}" ]]; then
    candidate="$ANDROID_SDK_ROOT"
  elif [[ -n "$local_properties_sdk" ]]; then
    candidate="$local_properties_sdk"
  elif [[ -n "${HOME:-}" && -d "$HOME/Library/Android/sdk" ]]; then
    candidate="$HOME/Library/Android/sdk"
  elif [[ -n "${HOME:-}" && -d "$HOME/Android/Sdk" ]]; then
    candidate="$HOME/Android/Sdk"
  fi

  [[ -n "$candidate" ]] || fail "Android SDK was not found. Set ANDROID_HOME or ANDROID_SDK_ROOT."
  [[ -d "$candidate" ]] || fail "Android SDK path does not exist: $candidate"
  require_single_line "Android SDK path" "$candidate"
  (cd "$candidate" && pwd)
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

run_publish() {
  local maven_repo="$1"
  local version="$2"
  local sdk_dir="$3"
  local build_tools_version="$4"

  local -a gradle_args=(
    publishToMavenLocal
    -Dmaven.repo.local="$maven_repo"
    -PjankHunterBuildToolsVersion="$build_tools_version"
    --no-daemon
    --console=plain
    --stacktrace
  )
  if [[ -n "$version" ]]; then
    gradle_args+=(-PjankHunterVersion="$version")
  fi

  log "publishing Android modules (version ${version:-from gradle.properties})"
  (
    cd "$ANDROID_DIR"
    ANDROID_HOME="$sdk_dir" ANDROID_SDK_ROOT="$sdk_dir" \
      ./gradlew "${gradle_args[@]}"
  )

  log "publishing Gradle plugin marker and plugin artifacts"
  (
    cd "$ANDROID_DIR"
    ANDROID_HOME="$sdk_dir" ANDROID_SDK_ROOT="$sdk_dir" \
      ./gradlew -p jankhunter-gradle-plugin "${gradle_args[@]}"
  )
}

POSITIONAL=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    --name)
      require_value "$1" "${2:-}"
      REPO_NAME="$2"
      shift 2
      ;;
    --version)
      require_value "$1" "${2:-}"
      VERSION_OVERRIDE="$2"
      shift 2
      ;;
    --)
      shift
      POSITIONAL+=("$@")
      break
      ;;
    -*)
      fail "unknown option: $1"
      ;;
    *)
      POSITIONAL+=("$1")
      shift
      ;;
  esac
done

if [[ ${#POSITIONAL[@]} -gt 0 ]]; then
  REPO_NAME="${POSITIONAL[0]}"
fi
if [[ ${#POSITIONAL[@]} -gt 1 ]]; then
  VERSION_OVERRIDE="${POSITIONAL[1]}"
fi
if [[ ${#POSITIONAL[@]} -gt 2 ]]; then
  fail "too many arguments; expected at most REPO_NAME and VERSION"
fi

[[ -x "$ANDROID_DIR/gradlew" ]] || fail "Gradle wrapper was not found: $ANDROID_DIR/gradlew"

version="$(read_default_version)"
if [[ -n "$VERSION_OVERRIDE" ]]; then
  version="$VERSION_OVERRIDE"
fi
validate_version_value "version" "$version"

group="$(properties_value jankHunterGroup "$GRADLE_PROPERTIES")"
[[ -n "$group" ]] || group="io.jankhunter"

maven_repo="$(resolve_maven_repo_dir "$REPO_NAME")"
sdk_dir="$(resolve_android_sdk_dir)"
build_tools_version="$(resolve_build_tools_version "$sdk_dir")"

log "repository: $maven_repo"
log "coordinates: $group:*:$version"
log "Android SDK: $sdk_dir"
log "Android Build Tools: $build_tools_version"

run_publish "$maven_repo" "$version" "$sdk_dir" "$build_tools_version"

log "done"
log "Point the consumer at: -Dmaven.repo.local=$maven_repo"
log "or maven { url = uri(\"$maven_repo\") }"
