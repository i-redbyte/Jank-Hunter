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
  jankhunter-gradle-plugin/  Gradle plugin DSL и будущая ASM-инструментация
  sample-app/                Маленькое приложение для dogfooding runtime
```

## Что уже собирает runtime

- старт сессии: версия приложения, build, модель устройства, SDK API;
- текущий screen по `ActivityLifecycleCallbacks`;
- system context: battery, available memory, low-memory flag, network kind, metered/validated state, UID traffic;
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
<meta-data android:name="io.jankhunter.fps_monitor_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.fps_window_ms" android:value="1000" />
<meta-data android:name="io.jankhunter.jank_frame_threshold_ms" android:value="32" />
<meta-data android:name="io.jankhunter.max_queue_size" android:value="2048" />
<meta-data android:name="io.jankhunter.max_log_bytes" android:value="5242880" />
<meta-data android:name="io.jankhunter.flush_interval_ms" android:value="5000" />
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
    .fpsMonitorEnabled(true)
    .fpsWindowMs(1_000)
    .jankFrameThresholdMs(32)
    .maxQueueSize(2048)
    .maxLogBytes(5 * 1024 * 1024)
    .flushIntervalMs(5_000)
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

В будущем Gradle plugin будет добавлять generated owner IDs через ASM, чтобы не писать такие обертки вручную везде.

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
```

Если объект остается достижимым дольше `retainedObjectDelayMs`, SDK пишет `retained_object` event. Это ранний сигнал, а не полноценная замена LeakCanary.

## JankStats

Core не тянет AndroidX, но содержит reflection bridge. Если host-приложение само подключило `androidx.metrics:metrics-performance`, можно включить точный JankStats-сигнал:

```kotlin
JankHunterJankStats.install(window)
```

Если класса JankStats нет в classpath, метод вернет `null` и ничего не сломает.

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
`methodCounters` по умолчанию выключен: при включении visitor добавляет легкий `JankHunter.recordCounter("owner.<class>.<method>", 1)` на входе методов классов, прошедших include/exclude-фильтры.

Для каждого enabled variant создается owner-map seed:

```text
build/generated/jankhunter/<variant>/owner-map.json
```

Следующий этап для plugin - более специализированная ASM-инструментация OkHttp builder/newWebSocket, Handler, Executor, Runnable/Callable, RxJava и coroutine continuation points.

## Где лежат логи

По умолчанию:

```text
context.filesDir/jankhunter/session-<timestamp>.jhlog
```

Путь можно изменить через:

```kotlin
JankHunterConfig.builder()
    .logDirectory(customDir)
    .build()
```

## Важные ограничения

- Core SDK сейчас не содержит JankStats, чтобы не тянуть AndroidX.
- FPS через `Choreographer` дает универсальный lightweight-сигнал, но не заменяет будущую точную интеграцию с JankStats.
- Heap dump и LeakCanary пока не включены в core, чтобы не утяжелять host-приложение.
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
