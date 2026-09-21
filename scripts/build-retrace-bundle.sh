#!/usr/bin/env bash
set -euo pipefail

# Downloads only at build time. The resulting directory is sufficient for offline use.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
output="${1:?usage: build-retrace-bundle.sh OUTPUT_DIRECTORY}"
version=9.0.32
sha256=a561da8b3d2419f3c0b1936546f6a756790112653f834563f65cebc4fce0bddd
mkdir -p "$output"
output="$(cd "$output" && pwd)"
work="$(mktemp -d "$output/.retrace-build.XXXXXX")"
trap 'rm -rf "$work"' EXIT
if [[ -n "${JANK_HUNTER_R8_JAR:-}" ]]; then
  cp "$JANK_HUNTER_R8_JAR" "$work/r8lib.jar"
elif [[ -f "$output/r8lib-$version.jar" ]]; then
  cp "$output/r8lib-$version.jar" "$work/r8lib.jar"
else
  curl --fail --location --proto '=https' --tlsv1.2 --retry 2 \
    "https://storage.googleapis.com/r8-releases/raw/$version/r8lib.jar" -o "$work/r8lib.jar"
fi
if command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$work/r8lib.jar" | awk '{print $1}')"
else
  actual="$(sha256sum "$work/r8lib.jar" | awk '{print $1}')"
fi
[[ "$actual" == "$sha256" ]] || { echo 'Official Retrace SHA-256 mismatch' >&2; exit 1; }
java_bin="${JAVA_HOME:+$JAVA_HOME/bin/}"
mkdir "$work/classes"
"${java_bin}javac" --release 17 -g:none -cp "$work/r8lib.jar" -d "$work/classes" \
  "$script_dir/../cli/retrace/src/io/jankhunter/retrace/Main.java"
"${java_bin}jar" --create --file "$work/jankhunter-retrace.jar" --date=2020-01-01T00:00:00Z -C "$work/classes" .
(cd "$work" && "${java_bin}jar" --extract --file r8lib.jar LICENSE)
test -s "$work/LICENSE"
printf 'Official R8 Retrace %s\nSHA-256 %s\nRequires Java 17 or newer. No runtime downloads.\n' "$version" "$sha256" > "$work/NOTICE.txt"
mv "$work/r8lib.jar" "$output/r8lib-$version.jar"
mv "$work/jankhunter-retrace.jar" "$output/jankhunter-retrace.jar"
mv "$work/LICENSE" "$output/R8-LICENSE.txt"
mv "$work/NOTICE.txt" "$output/NOTICE.txt"
