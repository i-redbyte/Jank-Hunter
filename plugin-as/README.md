# Jank Hunter Android Studio Plugin

This directory contains the IntelliJ Platform plugin for running the local Jank Hunter CLI from Android Studio or IntelliJ IDEA.

## Requirements

- IntelliJ IDEA 2026.1.3 or another IntelliJ-based IDE with JBR 21.
- The included Gradle wrapper. A system Gradle installation is not required.
- A built Jank Hunter CLI binary, usually `../cli/bin/jankhunter`, or `jankhunter` available on `PATH`.

## Build

```bash
./gradlew buildPlugin
```

The plugin ZIP is generated under:

```text
build/distributions/
```

## Run In Sandbox

```bash
./gradlew runIde
```

By default the Gradle build uses the local IDE at `/Applications/IntelliJ IDEA.app`. If that path is not present, it falls back to the `2026.1.3` IntelliJ IDEA dependency.

Override the local IDE path when needed:

```bash
./gradlew runIde -PlocalIdePath="/Applications/Android Studio.app"
```

## CLI Setup

Build the CLI first:

```bash
cd ../cli
make build
```

Then open the Jank Hunter tool window and point `CLI` to `../cli/bin/jankhunter` if it was not detected automatically.
