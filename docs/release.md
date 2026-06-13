# Release and Publishing

Этот документ описывает release path для Android SDK artifacts и Go CLI.

## Version bump

Единая версия Android artifacts хранится в:

```properties
android/gradle.properties
jankHunterVersion=0.1.0-SNAPSHOT
```

Перед release:

1. Убрать `-SNAPSHOT`.
2. Обновить changelog/release notes.
3. Прогнать полный набор проверок.
4. Создать git tag вида `v0.1.0`.
5. После release вернуть следующую `-SNAPSHOT` версию.

Sample app берет `versionName` из `jankHunterVersion` без `-SNAPSHOT`.

## Android publish

Local dry run:

```bash
cd android
./gradlew publishToMavenLocal --no-daemon
```

Artifacts:

- `io.jankhunter:jankhunter-runtime`
- `io.jankhunter:jankhunter-okhttp3`
- Gradle plugin marker for `io.jankhunter.android`

Publishing metadata is configured for Maven-compatible repositories:

- artifact name/description;
- license;
- SCM;
- developer metadata;
- optional signing;
- GitHub Packages repository;
- generic release repository through env vars.

### Signing

Signing is disabled by default for local development. To require signing:

```bash
export JANKHUNTER_SIGNING_REQUIRED=true
export JANKHUNTER_SIGNING_KEY="$(cat private-key.asc)"
export JANKHUNTER_SIGNING_PASSWORD="..."
```

Gradle properties fallback:

```properties
signingInMemoryKey=...
signingInMemoryKeyPassword=...
```

### GitHub Packages

```bash
export GITHUB_ACTOR="..."
export GITHUB_TOKEN="..."
cd android
./gradlew publishAllPublicationsToGitHubPackagesRepository --no-daemon
```

### Maven Central or staging repository

Set a repository URL and credentials supplied by the release system:

```bash
export MAVEN_REPOSITORY_URL="https://..."
export MAVEN_REPOSITORY_USERNAME="..."
export MAVEN_REPOSITORY_PASSWORD="..."
cd android
./gradlew publishAllPublicationsToRemoteReleaseRepository --no-daemon
```

## CLI release

Build local binary:

```bash
cd cli
make build VERSION=0.1.0
```

Build macOS/Linux archives and checksums:

```bash
cd cli
make release VERSION=0.1.0
```

Output:

```text
cli/dist/jankhunter_0.1.0_darwin_amd64.tar.gz
cli/dist/jankhunter_0.1.0_darwin_arm64.tar.gz
cli/dist/jankhunter_0.1.0_linux_amd64.tar.gz
cli/dist/jankhunter_0.1.0_linux_arm64.tar.gz
cli/dist/checksums.txt
```

Install from archive:

```bash
tar -xzf jankhunter_0.1.0_darwin_arm64.tar.gz
install -m 0755 jankhunter /usr/local/bin/jankhunter
jankhunter version
```

## CI

CI jobs are intentionally split:

- Kotlin-only Android source check;
- Go CLI tests;
- Android test/assemble;
- Maven Local publish dry run;
- CLI release archives and sample report artifact.

## Compatibility policy

### Android SDK

Minor versions may add APIs and optional modules. Patch versions should avoid breaking source or binary compatibility for public runtime APIs. Potentially conflict-prone integrations stay in optional modules.

### CLI

CLI output intended for humans can evolve between minor versions. `--json` output should remain additive when possible; removing fields requires a minor-version release note.

### `.jhlog`

The binary header version is `FormatVersion` in `cli/internal/jhlog/format.go`.

Pre-release policy:

- `.jhlog` is not frozen yet.
- Runtime and CLI only need to agree on the current in-repo format.
- Old pre-release logs may become unreadable after format changes.
- Do not keep compatibility branches only for pre-release formats.

Rules after explicit format freeze:

- Do not change existing event payload order.
- Add new optional fields at the end of payloads.
- Keep readers tolerant of older payloads.
- Bump format version only when an old reader cannot safely ignore the change.
- When bumping, keep the CLI reader backward-compatible with all released versions.
