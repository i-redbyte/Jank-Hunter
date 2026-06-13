# Jank Hunter Maintainer Prompt

Ты работаешь над проектом **Jank Hunter** в репозитории `github.com/i-redbyte/Jank-Hunter`.

Jank Hunter - это Android performance SDK + Go CLI для локального сбора `.jhlog`, анализа performance-регрессий, HTML-отчетов и debug/QA-инструментации больших Android-приложений.

## Жесткие правила

- Android SDK и Gradle plugin пишутся только на Kotlin.
- Java-файлов в `android/` быть не должно.
- Core runtime не должен тянуть AndroidX, OkHttp, RxJava, Compose или другие конфликтные runtime-зависимости.
- Конфликтные интеграции живут в optional modules.
- CLI пишется на Go.
- `.jhlog` reader должен оставаться backward-compatible.
- После крупного этапа: проверки, commit, push в `master`.

Минимальные проверки:

```bash
cd cli
GOCACHE=/private/tmp/jh-go-cache go test ./...

cd ../android
./gradlew test assemble --no-daemon

cd ..
rg --files -g '*.java' .
```

Если менялась publishing/Gradle-логика:

```bash
cd android
./gradlew publishToMavenLocal --no-daemon
```

## Текущий статус

Реализовано:

- binary `.jhlog` writer/reader;
- tolerant parser для частично оборванных файлов;
- streaming aggregation в CLI;
- JSONL export;
- `sample`, `inspect`, `compare`, `export`, `version`;
- standalone HTML reports без CDN;
- JSON output для `inspect` и `compare`;
- threshold config и CI regression gate;
- owner-map import и Top Owners;
- cohort warnings по app version/build/SDK/device/process/network;
- Android runtime collectors: session, screen, system context, FPS, main-thread stalls, memory, process exit info, retained objects, counters, gauges;
- multi-process policy и per-process log files;
- retained-object grouping, age buckets и debug/QA forced-GC option;
- optional OkHttp/WebSocket module;
- reflection-only JankStats bridge без AndroidX dependency в core;
- Gradle plugin ASM hooks for method counters, OkHttp factories, WebSocket listeners, Handler scheduling, Executor/ExecutorService work;
- runtime `Runnable`/`Callable` owner wrappers;
- owner-map seed generation;
- Maven Local publishing, Maven/GitHub Packages metadata, signing placeholders;
- CLI Makefile for macOS/Linux archives and checksums;
- split GitHub Actions CI.

## Репозиторий

```text
android/
  jankhunter-runtime/
  jankhunter-okhttp3/
  jankhunter-gradle-plugin/
  sample-app/
cli/
  cmd/jankhunter/
  internal/jhlog/
  internal/analyze/
  internal/report/
docs/
  architecture.md
  release.md
```

## Runtime principles

- Host app stability beats metric completeness.
- High-frequency signals must be aggregated before writing.
- Blocking the app because diagnostics are busy is unacceptable.
- Release usage must remain explicit and lightweight.
- Debug/QA builds can enable richer instrumentation.
- Privacy defaults matter: no URL query, headers, bodies, tokens, object fields, or `toString()`.

## CLI principles

- Keep parsing streaming-first.
- Do not require a backend or external browser assets.
- Prefer valid JSON output for automation.
- In compare mode, call out sample size, confidence and cohort mismatch before claiming a regression.
- Threshold gates should fail CI only after producing useful artifacts where possible.

## Useful commands

```bash
cd cli
go run ./cmd/jankhunter sample --out /tmp/sample.jhlog
go run ./cmd/jankhunter inspect /tmp/sample.jhlog --json --out /tmp/report.html
go run ./cmd/jankhunter compare --baseline /tmp/sample.jhlog --candidate /tmp/sample.jhlog --out /tmp/compare.html
make release VERSION=ci

cd ../android
./gradlew test assemble --no-daemon
./gradlew publishToMavenLocal --no-daemon
```

## Known limitations

- Confidence intervals are approximate and based on available aggregates, not full bootstrap statistics over raw events.
- Handler instrumentation covers a safe subset of common scheduling APIs.
- Executor instrumentation targets JDK `Executor`, `ExecutorService`, and `ScheduledExecutorService` signatures.
- OkHttp/WebSocket ASM hooks require the optional `jankhunter-okhttp3` artifact in the host app.
- Retained-object watcher is an early signal and not a heap dump or LeakCanary replacement.
- JankStats auto-install only produces data when the host app already has AndroidX JankStats on the classpath.
- Large legacy app validation requires an external target application and should produce a separate validation report.

## Next maintainer tasks

- Add reproducible Android instrumented end-to-end log tests for the sample app.
- Add a host-side script/task to pull `.jhlog` from a connected device/emulator and run CLI inspect.
- Keep docs synchronized with code after each stage.
- Run large-app validation once a target application is available.

## Reference docs

- [README.md](README.md)
- [android/README.md](android/README.md)
- [cli/README.md](cli/README.md)
- [docs/architecture.md](docs/architecture.md)
- [docs/release.md](docs/release.md)
- [docs/large-app-validation.md](docs/large-app-validation.md)
