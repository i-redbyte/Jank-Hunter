#!/usr/bin/env bash
set -euo pipefail

fixture_dir="$(cd "$(dirname "$0")" && pwd)"
artifact_dir="$fixture_dir/build/fixture-artifacts"
external_dir="$artifact_dir/external"
file_dir="$artifact_dir/file"
module_dir="$fixture_dir/fixture-maven/com/fixture/external/1.0"
host_local_properties="$fixture_dir/../../../../../local.properties"

if [[ -f "$host_local_properties" ]]; then
    sed -n '/^sdk\.dir=/p' "$host_local_properties" > "$fixture_dir/local.properties"
fi

mkdir -p "$external_dir/classes" "$file_dir/classes" "$module_dir" "$fixture_dir/app/libs"
javac -g -d "$external_dir/classes" \
    "$fixture_dir/external-src/com/fixture/app/thirdparty/ExternalThreadOrigin.java"
jar --create --file "$external_dir/classes.jar" -C "$external_dir/classes" .
cat > "$external_dir/AndroidManifest.xml" <<'EOF'
<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.fixture.app.thirdparty" />
EOF
jar --create --file "$module_dir/external-1.0.aar" \
    -C "$external_dir" classes.jar -C "$external_dir" AndroidManifest.xml
cat > "$module_dir/external-1.0.pom" <<'EOF'
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.fixture</groupId>
  <artifactId>external</artifactId>
  <version>1.0</version>
  <packaging>aar</packaging>
</project>
EOF
javac -g -d "$file_dir/classes" "$fixture_dir/file-src/com/fixture/unknown/FileJarThreadOrigin.java"
jar --create --file "$fixture_dir/app/libs/unknown.jar" -C "$file_dir/classes" .
