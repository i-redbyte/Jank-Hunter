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
DRY_RUN=0
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
  --module :app                 Android module to patch. Can be repeated. If omitted, an
                                application module is detected with :app preferred over test modules.
  --include-package com.myapp   ASM include package. Can be repeated.
  --include-packages a,b,c      Comma-separated ASM include packages.
  --exclude-package com.myapp.generated
                                ASM exclude package. Can be repeated.
  --exclude-packages a,b,c      Comma-separated ASM exclude packages.
  --include-whole-application   Use the target module namespace as an include package.
  --runtime-call-graph          Enable runtime caller -> callee graph hooks.
  --build-type debug            Enabled build type. Can be repeated or comma-separated.
  --maven-dir PATH              Local Maven repo inside target project. Default: .jankhunter/maven.
  --cli-dir PATH                Target directory for CLI binary. Default: .jankhunter/bin.
  --android-sdk PATH            Android SDK path for target local.properties. If omitted, the
                                script uses ANDROID_HOME, ANDROID_SDK_ROOT, or ~/Library/Android/sdk.
  --verify                      Run target Gradle task resolution after patching.
  --dry-run                     Print what would be changed without writing files.

Advanced:
  --skip-publish                Do not publish Jank Hunter artifacts into the target Maven repo.
  --skip-cli-build              Do not build/copy the jankhunter CLI binary.
  --skip-local-properties       Do not create or update target local.properties. Gradle still gets
                                the resolved SDK path through ANDROID_HOME during publishing.
  --no-asm-progress-log         Disable one-line ASM progress log in generated config.
  --no-gitignore                Do not add the local Maven repo to target .gitignore.

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
    --cli-dir)
      CLI_DIR="${2:-}"
      shift 2
      ;;
    --android-sdk|--android-sdk-dir)
      ANDROID_SDK_DIR="${2:-}"
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

VERSION="$(awk -F= '$1 == "jankHunterVersion" { print $2; exit }' "$JANKHUNTER_ROOT/android/gradle.properties")"
GROUP="$(awk -F= '$1 == "jankHunterGroup" { print $2; exit }' "$JANKHUNTER_ROOT/android/gradle.properties")"
[[ -n "$VERSION" ]] || fail "could not read jankHunterVersion from android/gradle.properties"
[[ -n "$GROUP" ]] || fail "could not read jankHunterGroup from android/gradle.properties"

MAVEN_REPO_ABS="$TARGET_ROOT/$MAVEN_DIR"
CLI_DIR_ABS="$TARGET_ROOT/$CLI_DIR"
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
  local file rel dir module base total=0
  local preferred=()
  local regular=()
  local suspicious=()
  while IFS= read -r file; do
    if grep -Eq 'com\.android\.application' "$file"; then
      rel="${file#$TARGET_ROOT/}"
      dir="$(dirname "$rel")"
      if [[ "$dir" == "." ]]; then
        module=":"
      else
        module=":${dir//\//:}"
      fi
      total=$((total + 1))
      base="${module##*:}"
      if [[ "$module" == ":app" || "$base" == "app" ]]; then
        preferred+=("$module")
      elif is_suspicious_app_module "$module"; then
        suspicious+=("$module")
      else
        regular+=("$module")
      fi
    fi
  done < <(
    find "$TARGET_ROOT" \
      \( -path "$TARGET_ROOT/.gradle" -o -path "$TARGET_ROOT/build" -o -path "$TARGET_ROOT/.jankhunter" \) -prune \
      -o \( -name build.gradle.kts -o -name build.gradle \) -type f -print | sort
  )

  if [[ "$total" -gt 1 ]]; then
    printf '[jankhunter-integrate] application module candidates: %s %s %s\n' "${preferred[*]-}" "${regular[*]-}" "${suspicious[*]-}" >&2
    printf '[jankhunter-integrate] use --module :your-app if auto-detection picks the wrong module\n' >&2
  fi
  if [[ "${#preferred[@]}" -gt 0 ]]; then
    printf '%s\n' "${preferred[0]}"
    return
  fi
  if [[ "${#regular[@]}" -gt 0 ]]; then
    printf '%s\n' "${regular[0]}"
    return
  fi
  if [[ "${#suspicious[@]}" -gt 0 ]]; then
    printf '[jankhunter-integrate] warning: only test-like application modules were found\n' >&2
    printf '%s\n' "${suspicious[0]}"
    return
  fi
  fail "could not detect com.android.application module. Pass --module :app"
}

is_suspicious_app_module() {
  local module="$1"
  local lower
  lower="$(printf '%s' "$module" | tr '[:upper:]' '[:lower:]')"
  case "$lower" in
    *test*|*tests*|*benchmark*|*benchmarks*|*fixture*|*fixtures*|*uitest*|*androidtest*)
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
    escaped="${value//\\/\\\\}"
    escaped="${escaped//\"/\\\"}"
    if [[ -n "$out" ]]; then
      out+=", "
    fi
    out+="\"$escaped\""
  done
  printf '%s' "$out"
}

patch_settings_file() {
  local file="$1"
  local plugin_block dependency_block
  plugin_block=$'    // Jank Hunter plugin repository\n    repositories {\n        maven { url = uri(".jankhunter/maven") }\n    }\n'
  dependency_block=$'    // Jank Hunter dependency repository\n    repositories {\n        maven { url = uri(".jankhunter/maven") }\n    }\n'

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
  [[ "$ADD_GITIGNORE" -eq 1 ]] || return
  local file="$TARGET_ROOT/.gitignore"
  local needs_jankhunter=1
  local needs_local_properties=1

  if [[ -f "$file" ]] && grep -q '^\.jankhunter/$' "$file"; then
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
      printf '\n# Jank Hunter local Maven repo and generated CLI\n.jankhunter/\n'
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
  log "Android SDK: $RESOLVED_ANDROID_SDK_DIR"
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

  local plugin_line runtime_dep okhttp_dep jh_block includes excludes build_type
  if [[ "$dsl" == "kts" ]]; then
    plugin_line="    id(\"io.jankhunter.android\") version \"$VERSION\""
    runtime_dep="    debugImplementation(\"$GROUP:jankhunter-runtime:$VERSION\")"
    okhttp_dep="    debugImplementation(\"$GROUP:jankhunter-okhttp3:$VERSION\")"
  else
    plugin_line="    id 'io.jankhunter.android' version '$VERSION'"
    runtime_dep="    debugImplementation \"$GROUP:jankhunter-runtime:$VERSION\""
    okhttp_dep="    debugImplementation \"$GROUP:jankhunter-okhttp3:$VERSION\""
  fi

  jh_block=$'\n\njankHunter {\n'
  for build_type in "${BUILD_TYPES[@]}"; do
    jh_block+="    enabledBuildTypes.add(\"$build_type\")"$'\n'
  done
  jh_block+=$'    autoInit = true\n\n'
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

  if ! grep -q "$GROUP:jankhunter-runtime" "$file"; then
    RUNTIME_DEP="$runtime_dep" OKHTTP_DEP="$okhttp_dep" perl -0pi -e '
      my $deps = $ENV{"RUNTIME_DEP"} . "\n" . $ENV{"OKHTTP_DEP"} . "\n";
      if ($_ =~ /dependencies\s*\{/) {
        s/(dependencies\s*\{)/$1\n$deps/s;
      } else {
        $_ .= "\n\ndependencies {\n$deps}\n";
      }
    ' "$file"
  fi

  if ! grep -q 'jankHunter[[:space:]]*{' "$file"; then
    printf '%s' "$jh_block" >> "$file"
  fi
}

publish_artifacts_if_needed() {
  [[ "$SKIP_PUBLISH" -eq 0 ]] || {
    log "skipping publish step"
    return
  }

  local runtime_aar="$MAVEN_REPO_ABS/$GROUP_PATH/jankhunter-runtime/$VERSION/jankhunter-runtime-$VERSION.aar"
  local okhttp_aar="$MAVEN_REPO_ABS/$GROUP_PATH/jankhunter-okhttp3/$VERSION/jankhunter-okhttp3-$VERSION.aar"
  local plugin_pom="$MAVEN_REPO_ABS/io/jankhunter/android/io.jankhunter.android.gradle.plugin/$VERSION/io.jankhunter.android.gradle.plugin-$VERSION.pom"

  if [[ -f "$runtime_aar" && -f "$okhttp_aar" && -f "$plugin_pom" ]]; then
    log "Jank Hunter artifacts already exist in $MAVEN_REPO_ABS"
    return
  fi

  log "publishing Jank Hunter artifacts into $MAVEN_REPO_ABS"
  run_cmd mkdir -p "$MAVEN_REPO_ABS"
  if [[ "$DRY_RUN" -eq 0 ]]; then
    if ! (cd "$JANKHUNTER_ROOT/android" && ANDROID_HOME="$RESOLVED_ANDROID_SDK_DIR" ANDROID_SDK_ROOT="$RESOLVED_ANDROID_SDK_DIR" ./gradlew publishToMavenLocal -Dmaven.repo.local="$MAVEN_REPO_ABS" --no-daemon --stacktrace); then
      fail "failed to publish Jank Hunter artifacts. Android SDK: $RESOLVED_ANDROID_SDK_DIR"
    fi
  else
    log "would run: cd $JANKHUNTER_ROOT/android && ANDROID_HOME=$RESOLVED_ANDROID_SDK_DIR ./gradlew publishToMavenLocal -Dmaven.repo.local=$MAVEN_REPO_ABS --no-daemon --stacktrace"
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
  [[ "$VERIFY" -eq 1 ]] || return
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
log "Local Maven repo: $MAVEN_REPO_ABS"
log "CLI binary: $CLI_DIR_ABS/jankhunter"
log "Modules: ${MODULES[*]}"

prepare_jankhunter_android_sdk
publish_artifacts_if_needed
build_cli_if_needed
patch_target_local_properties
patch_settings_file "$SETTINGS_FILE"
patch_gitignore

for module in "${MODULES[@]}"; do
  build_file="$(module_build_file "$module")"
  patch_module_build_file "$module" "$build_file"
done

verify_target_project

log "done"
log "Backups: $BACKUP_ROOT"
log "CLI: $CLI_DIR_ABS/jankhunter"
log "Next: build the target app and inspect generated owner-map/class-graph under each patched module build/generated/jankhunter/<variant>/"
