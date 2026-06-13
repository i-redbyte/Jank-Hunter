# Jank Hunter Architecture

## Overview

Jank Hunter has two independent execution surfaces:

- Android SDK: low-overhead collection and local log writing.
- Go CLI: offline decoding, aggregation, comparison, and report generation.

The Android side must stay conservative because it runs inside somebody else's app.
The CLI side can do heavier analysis because it runs after the fact.

## Repository layout

```text
cli/      Go CLI and report generator.
android/  Runtime SDK, optional integrations, Gradle plugin scaffold.
docs/     Format and architecture notes.
```

## Android module strategy

`jankhunter-runtime` is the dependency-safe core. Android source is Kotlin-only and avoids AndroidX/OkHttp/RxJava/Compose in the core artifact.

Optional integrations are separate artifacts:

- `jankhunter-okhttp3` for OkHttp EventListener integration.
- future `jankhunter-rxjava2`, `jankhunter-rxjava3`, `jankhunter-compose`, etc.

This avoids forcing host apps to upgrade libraries.

## Log strategy

Runtime writes compact binary `.jhlog` records:

- varint encoding for integers;
- bit flags for common booleans;
- dictionary IDs for names;
- windowed aggregates for high-frequency metrics.

The CLI owns human-readable rendering.

## Attribution strategy

Jank Hunter should not claim perfect blame. It should surface likely suspects:

- explicit owners from `JankHunter.withOwner`;
- generated owner IDs from Gradle/ASM instrumentation;
- sampled stack signatures for slow/error paths;
- top offenders grouped by route, screen, class, owner, and stack.

## Future instrumentation

The Gradle plugin will eventually use Android Gradle Plugin ASM APIs to weave debug/QA builds:

- OkHttp builder/listener wrapping;
- Handler/Executor/Runnable/Callable timing;
- coroutine and RxJava hook setup;
- owner map generation.

Release builds should receive either a noop runtime or a deliberately lightweight opt-in runtime.
