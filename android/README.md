# Jank Hunter Android SDK

Jank Hunter Android SDK - это легкий runtime и набор optional-интеграций для сбора performance-диагностики внутри Android-приложения.

Цель SDK - аккуратно собирать локальный `.jhlog` файл, который потом разбирает `jankhunter` CLI. SDK не пытается отправлять данные в сеть, не требует backend и не должен ломать старые host-приложения зависимостями.

## Принципы

- Core runtime пишется только на Kotlin.
- Core runtime не зависит от AndroidX, OkHttp, RxJava или Compose.
- Все потенциально конфликтные интеграции вынесены в отдельные модули.
- Auto-init работает через `ContentProvider`, поэтому `Application.onCreate()` можно не менять.
- Высокочастотные данные пишутся агрегатами, а не сырым потоком событий.
- `.jhlog` оптимизирован под машинный разбор: varint, bit flags, dictionary IDs.

## Модули

```text
android/
  jankhunter-runtime/        Core SDK без внешних runtime-зависимостей
  jankhunter-okhttp3/        Optional OkHttp EventListener integration
  jankhunter-gradle-plugin/  Gradle plugin DSL и ASM-инструментация debug/QA build variants
  sample-app/                Маленькое приложение для dogfooding runtime
```

## Publishing

Версия Android artifacts задается в `android/gradle.properties`:

```properties
jankHunterVersion=0.1.0-SNAPSHOT
```

Local publish dry run:

```bash
cd android
./gradlew publishToMavenLocal --no-daemon
```

Gradle publishing metadata уже содержит artifact name/description, license, SCM и developer fields. Signing и remote repositories настраиваются через env vars; подробности в [release docs](../docs/release.md).

## Подключение debug-only

Для большого host-приложения базовый вариант лучше подключать только в debug/QA variants:

```kotlin
dependencies {
    debugImplementation("io.jankhunter:jankhunter-runtime:0.1.0-SNAPSHOT")
    debugImplementation("io.jankhunter:jankhunter-okhttp3:0.1.0-SNAPSHOT")
}
```

Gradle plugin тоже обычно включают только для debug/QA build types через DSL:

```kotlin
jankHunter {
    enabledBuildTypes.add("debug")
    enabledBuildTypes.add("qa")
    instrument {
        okhttp = true
        webSockets = true
        handlers = true
        executors = true
        methodCounters = false
        includePackages.add("com.example.app")
    }
}
```

## Что уже собирает runtime

- старт сессии: версия приложения, build, модель устройства, SDK API, Android release/security patch, ABI, manufacturer/brand/hardware/board/product;
- process name в session metadata;
- текущий screen по `ActivityLifecycleCallbacks`;
- system context: battery, available/total memory, low-memory flag, network kind, metered/validated/VPN state, UID traffic, free/total app data storage;
- FPS и UI frame windows через `Choreographer`;
- jank count по порогу длительности кадра;
- p50/p95/p99 длительности кадров внутри окна;
- main-thread stalls через watchdog;
- memory snapshots: PSS, Java heap, native heap;
- previous process exit summary через `ApplicationExitInfo` на Android 11+;
- retained object watcher без внешних зависимостей;
- counters/gauges;
- explicit owner attribution через `JankHunter.withOwner(...)`.

## FPS monitor

В core runtime есть `FpsMonitor`, построенный на `Choreographer.FrameCallback`.

Он пишет агрегированное UI-событие примерно раз в секунду:

```text
screen_id
window_ms
frame_count
jank_count
p50_ms
p95_ms
p99_ms
```

Из `frame_count / window_ms` CLI считает средний FPS. Дополнительно runtime пишет gauge `ui.fps_x100`, где значение `5800` означает `58.00 FPS`.

Порог jank по умолчанию: `32 ms`.

## Настройка runtime

По умолчанию SDK стартует сам через provider. Auto-init читает manifest `meta-data`.
Если `io.jankhunter.enabled` не задан, runtime включается только когда host-приложение debuggable.

```xml
<provider
    android:name="io.jankhunter.runtime.AutoInitProvider"
    android:authorities="${applicationId}.jankhunter-init"
    android:exported="false"
    android:initOrder="100" />
```

Полезные manifest-настройки:

```xml
<meta-data android:name="io.jankhunter.enabled" android:value="true" />
<meta-data android:name="io.jankhunter.auto_start_collectors" android:value="true" />
<meta-data android:name="io.jankhunter.main_thread_stall_threshold_ms" android:value="700" />
<meta-data android:name="io.jankhunter.memory_sample_interval_ms" android:value="10000" />
<meta-data android:name="io.jankhunter.system_sampler_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.system_sample_interval_ms" android:value="15000" />
<meta-data android:name="io.jankhunter.process_exit_info_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.object_watcher_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.retained_object_delay_ms" android:value="5000" />
<meta-data android:name="io.jankhunter.retained_object_force_gc_enabled" android:value="false" />
<meta-data android:name="io.jankhunter.fps_monitor_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.jankstats_enabled" android:value="false" />
<meta-data android:name="io.jankhunter.fps_window_ms" android:value="1000" />
<meta-data android:name="io.jankhunter.jank_frame_threshold_ms" android:value="32" />
<meta-data android:name="io.jankhunter.max_queue_size" android:value="2048" />
<meta-data android:name="io.jankhunter.max_log_bytes" android:value="5242880" />
<meta-data android:name="io.jankhunter.max_log_directory_bytes" android:value="26214400" />
<meta-data android:name="io.jankhunter.flush_interval_ms" android:value="5000" />
<meta-data android:name="io.jankhunter.main_process_only" android:value="false" />
<meta-data android:name="io.jankhunter.allowed_processes" android:value="com.example.app,com.example.app:remote" />
```

Если нужна ручная настройка:

```kotlin
val config = JankHunterConfig.builder()
    .enabled(true)
    .autoStartCollectors(true)
    .mainThreadStallThresholdMs(700)
    .memorySampleIntervalMs(10_000)
    .systemSamplerEnabled(true)
    .systemSampleIntervalMs(15_000)
    .processExitInfoEnabled(true)
    .objectWatcherEnabled(true)
    .retainedObjectDelayMs(5_000)
    .retainedObjectForceGcEnabled(false)
    .fpsMonitorEnabled(true)
    .jankStatsEnabled(false)
    .fpsWindowMs(1_000)
    .jankFrameThresholdMs(32)
    .maxQueueSize(2048)
    .maxLogBytes(5 * 1024 * 1024)
    .maxLogDirectoryBytes(25 * 1024 * 1024)
    .flushIntervalMs(5_000)
    .mainProcessOnly(false)
    .allowedProcesses(listOf("com.example.app", "com.example.app:remote"))
    .processNameRedactor { processName -> processName?.replace("private", "redacted") }
    .build()

JankHunter.init(context, config)
```

При уходе приложения в background runtime пишет lifecycle counter и делает best-effort flush.
Для ручного сброса буфера можно вызвать:

```kotlin
JankHunter.flush()
```

## Privacy и redaction

По умолчанию HTTP route проходит через `JankHunterRedactor.default()`:

- query string не собирается в OkHttp-интеграции;
- numeric path segments заменяются на `{id}`;
- UUID заменяются на `{uuid}`;
- email-like значения заменяются на `{email}`;
- длинные hex-токены заменяются на `{hex}`.

Можно отключить или заменить redactor:

```kotlin
JankHunterConfig.builder()
    .routeRedactor(JankHunterRedactor.none())
    .build()
```

Process name тоже можно отредактировать программно через `processNameRedactor`. Это влияет на session metadata и suffix имени файла. Policy checks (`mainProcessOnly`, `allowedProcesses`) выполняются по raw process name до redaction.

## Multi-process policy

Runtime определяет process name так:

- API 28+: `Application.getProcessName()`;
- fallback: `ActivityManager.runningAppProcesses` по текущему PID;
- последний fallback: `context.packageName`.

По умолчанию SDK может стартовать в любом process, но каждый process пишет отдельный файл:

```text
context.filesDir/jankhunter/session-main-<timestamp>-1.jhlog
context.filesDir/jankhunter/session-remote-<timestamp>-1.jhlog
```

Для больших приложений можно ограничить запуск:

```kotlin
JankHunterConfig.builder()
    .mainProcessOnly(true)
    .build()
```

или разрешить конкретный список:

```kotlin
JankHunterConfig.builder()
    .allowedProcesses(listOf("com.example.app", "com.example.app:sync"))
    .build()
```

`AutoInitProvider` не открывает writer и не стартует collectors в запрещенных process.

или:

```kotlin
JankHunterConfig.builder()
    .routeRedactor { route -> route?.replace(Regex("/private/[^/]+"), "/private/{value}") }
    .build()
```

## Owner attribution

Для старого большого приложения важно понимать не только “что стало хуже”, но и “кто вероятный источник”.

Минимальный ручной API:

```kotlin
JankHunter.withOwner("FeedRepository.refresh") {
    // код, который хотим связать с owner
}
```

Gradle plugin может добавлять generated owner labels через ASM для выбранных build variants, чтобы не писать такие обертки вручную везде. Label строится из `class.method` и стабильного `fnv1a64(class.method+descriptor)` suffix; CLI может раскрывать такие labels через owner-map.

## OkHttp

Модуль `jankhunter-okhttp3` содержит `JankHunterEventListenerFactory`.

Он собирает:

- request duration;
- DNS duration;
- connect duration;
- TTFB;
- status class;
- request/response body bytes, если OkHttp отдал byte count;
- reused connection flag;
- TLS flag по secure connect / connection handshake;
- failure flag;
- route в виде `METHOD /path`;
- owner из текущего `JankHunter.currentOwner()`.

Интеграция:

```kotlin
val client = OkHttpClient.Builder()
    .eventListenerFactory(JankHunterEventListenerFactory())
    .build()
```

WebSocket:

```kotlin
client.newWebSocket(
    request,
    JankHunterWebSocketListener(
        owner = "RealtimeFeed",
        route = "wss /feed",
        delegate = existingListener,
    ),
)
```

## Object watcher

Core runtime содержит легкий retained-object watcher без heap dump:

```kotlin
JankHunter.watchObject(fragment, "FeedFragment")
JankHunter.watchActivity(activity)
JankHunter.watchFragment(fragment, "FeedFragment")
JankHunter.watchCloseable(closeableOwner, "FeedRepository")
```

Если объект остается достижимым дольше `retainedObjectDelayMs`, SDK делает повторную проверку перед report. При `retainedObjectForceGcEnabled(true)` watcher перед повторной проверкой просит lightweight GC; этот режим предназначен для debug/QA, а не для release.

Watcher группирует retained objects по class/safe owner name и пишет один `retained_object` event с `count` и максимальным age внутри группы. Он не пишет `object.toString()`, fields, heap dump или пользовательские данные. Это ранний сигнал, а не полноценная замена LeakCanary.

## JankStats

Core не тянет AndroidX, но содержит reflection bridge. Если host-приложение само подключило `androidx.metrics:metrics-performance`, можно включить точный JankStats-сигнал вручную:

```kotlin
val handle = JankHunterJankStats.install(window, "CheckoutActivity")
handle?.addState("screen", "Checkout")
handle?.uninstall()
```

Если класса JankStats нет в classpath, метод вернет `null` и ничего не сломает.

Для Activity auto-install без AndroidX dependency в core:

```kotlin
JankHunterConfig.builder()
    .jankStatsEnabled(true)
    .build()
```

Auto-install работает через `ActivityLifecycleCallbacks`: при start/resume SDK пытается вызвать `JankStats.createAndTrack(...)` через reflection, а при destroy/shutdown отключает tracking. Choreographer FPS остается включенным как fallback и пишет `ui_window`; JankStats пишет отдельные `jankstats.*` counters/gauges, чтобы CLI мог показать richer section без смешивания двух источников.

JankStats bridge собирает:

- frame count и janky frame count;
- frame duration gauge;
- screen-scoped counters/gauges;
- state tags из `FrameData.states`, если они доступны в подключенной версии AndroidX.

Если у приложения уже есть свой `EventListener.Factory`, передайте его как delegate:

```kotlin
val existing: EventListener.Factory = ...

val client = OkHttpClient.Builder()
    .eventListenerFactory(JankHunterEventListenerFactory(existing))
    .build()
```

## Gradle plugin

Текущий plugin уже задает DSL и точку входа:

```kotlin
plugins {
    id("io.jankhunter.android")
}

jankHunter {
    enabledBuildTypes.add("debug")
    autoInit = true

    instrument {
        okhttp = true
        webSockets = true
        handlers = true
        executors = true
        rxJava = true
        coroutines = false
        methodCounters = true
        includePackages.add("com.myapp")
        excludePackages.add("com.myapp.generated")
    }
}
```

Plugin регистрирует ASM transform через Android Components API только для `enabledBuildTypes`.
Классы проходят include/exclude-фильтры; platform, Kotlin, OkHttp и сам Jank Hunter исключены встроенно.

Реализованные hooks:

- `methodCounters`: добавляет `JankHunter.recordCounter("owner.<class>.<method>#<hash>", 1)` на входе методов. По умолчанию выключен.
- `okhttp`: перед вызовом `OkHttpClient.Builder.eventListenerFactory(...)` оборачивает factory через `JankHunterEventListenerFactory`, не оборачивая повторно уже Jank Hunter factory. Для этого в host app должен быть подключен optional artifact `jankhunter-okhttp3`.
- `webSockets`: перед `OkHttpClient.newWebSocket(...)` оборачивает `WebSocketListener` в `JankHunterWebSocketListener` и сохраняет delegate. Требует optional artifact `jankhunter-okhttp3`.
- `handlers`: safe subset для `Handler.post`, `postDelayed`, `postAtTime`, `postAtFrontOfQueue` оборачивает `Runnable` в `JankHunterRunnable`; `sendMessage*` пишет lightweight counter по call-site.
- `executors`: safe subset для `Executor.execute`, `ExecutorService.submit`, `ScheduledExecutorService.schedule`, `scheduleAtFixedRate`, `scheduleWithFixedDelay` оборачивает `Runnable`/`Callable` в Jank Hunter wrappers.

Runtime wrappers сохраняют delegate semantics: исключения пробрасываются как раньше, `Callable` возвращает исходный результат, а Future cancellation остается на стороне executor. Чтобы не раздувать поток событий, wrappers пишут duration gauge только для slow work и failure counter только при исключениях.

Для каждого enabled variant создается owner-map seed:

```text
build/generated/jankhunter/<variant>/owner-map.json
```

Owner-map seed фиксирует variant, hook flags, include/exclude policy и схему `owners`. Текущие ASM labels уже детерминированы, а внешний build tooling может дополнить `owners` map для более коротких или командных имен. CLI читает этот файл через `--owner-map`.

Ограничения:

- OkHttp/WebSocket hooks намеренно живут в optional модуле; если включить ASM hook без зависимости `jankhunter-okhttp3`, приложение не соберется.
- Handler instrumentation не перехватывает все возможные scheduling APIs, только safe subset с известными сигнатурами.
- Executor instrumentation ограничена JDK `Executor`/`ExecutorService`/`ScheduledExecutorService` и основными реализациями; кастомные `execute(Runnable)` методы не переписываются без явного расширения matcher.

## Где лежат логи

По умолчанию:

```text
context.filesDir/jankhunter/session-<process>-<timestamp>-<segment>.jhlog
```

По умолчанию writer ротирует сегмент при `io.jankhunter.max_log_bytes=5242880` (5 МБ) и хранит последние сегменты текущего процесса в пределах `io.jankhunter.max_log_directory_bytes=26214400` (25 МБ). Это кольцевое хранение: при превышении общего бюджета самые старые `session-<process>-*.jhlog` удаляются, а активный файл записи не трогается. Если общий бюджет меньше одного сегмента, текущий сегмент может временно быть больше бюджета; для больших приложений держите directory budget как минимум в несколько раз больше segment budget.

Путь можно изменить через:

```kotlin
JankHunterConfig.builder()
    .logDirectory(customDir)
    .build()
```

Для debuggable-приложения логи можно вытащить через `run-as`:

```bash
APP_ID=com.example.app
mkdir -p logs
adb shell run-as "$APP_ID" ls files/jankhunter
adb exec-out run-as "$APP_ID" tar -C files/jankhunter -cf - . | tar -xf - -C logs
```

Дальше:

```bash
cd ../cli
go run ./cmd/jankhunter inspect ../android/logs/*.jhlog --out /tmp/jankhunter-report.html
go run ./cmd/jankhunter compare --baseline "old/*.jhlog" --candidate "new/*.jhlog" --out /tmp/jankhunter-compare.html
```

## Важные ограничения

- Core SDK не содержит AndroidX JankStats dependency, чтобы не тянуть AndroidX.
- FPS через `Choreographer` дает универсальный lightweight-сигнал; JankStats bridge добавляет отдельный richer signal, когда AndroidX уже есть в host app.
- Heap dump и LeakCanary не включены в core, чтобы не утяжелять host-приложение.
- Release-режим должен быть opt-in и сильно ограничен по sampling/rate limit.

## Проверка

В Android-папке есть Gradle wrapper:

```bash
cd android
./gradlew test assemble --no-daemon
```

Проверенные команды:

```bash
./gradlew :jankhunter-runtime:assemble --no-daemon
./gradlew :sample-app:assembleDebug --no-daemon
./gradlew test assemble --no-daemon
```

CLI-часть уже умеет читать `.jhlog`, который пишет runtime, потому что формат событий синхронизирован между `android/jankhunter-runtime` и `cli/internal/jhlog`.

## End-to-end проверка

Instrumented test sample app проверяет путь:

```text
sample app -> runtime collectors -> .jhlog -> host pull -> CLI inspect -> HTML/JSON report
```

Локально с подключенным emulator/device:

```bash
cd android
./gradlew :sample-app:connectedDebugAndroidTest --no-daemon
```

Для ручного dogfooding можно поставить debug build и нажать кнопки sample app:

```bash
cd android
./gradlew :sample-app:installDebug --no-daemon
adb shell am start -n io.jankhunter.sample/.MainActivity
adb logcat | grep JankHunter
```

Sample app пишет локальные UI/background/leak-события и HTTP-события через optional `jankhunter-okhttp3` integration. Кнопки `Fetch JSONPlaceholder` и `Fetch HTTP 503` делают запросы к публичным test API и помогают увидеть в `.jhlog` успешный `2xx` и ошибочный `5xx` network path.

Полный host-side smoke из корня репозитория:

```bash
./scripts/android-e2e.sh
```

Script запускает `connectedDebugAndroidTest`, вытаскивает `files/jankhunter-e2e/*.jhlog` через `run-as`, затем генерирует:

```text
reports/android-e2e/report.html
reports/android-e2e/inspect.json
```

В GitHub Actions этот путь вынесен в manual workflow `Android E2E`, чтобы обычный CI не зависел от emulator startup time.

## Публикация

Локальная публикация:

```bash
cd android
./gradlew publishToMavenLocal --no-daemon
```

Координаты snapshot-артефактов:

```text
io.jankhunter:jankhunter-runtime:0.1.0-SNAPSHOT
io.jankhunter:jankhunter-okhttp3:0.1.0-SNAPSHOT
io.jankhunter:jankhunter-gradle-plugin:0.1.0-SNAPSHOT
io.jankhunter.android:io.jankhunter.android.gradle.plugin:0.1.0-SNAPSHOT
```
