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
```

## Что уже собирает runtime

- старт сессии: версия приложения, build, модель устройства, SDK API;
- текущий screen по `ActivityLifecycleCallbacks`;
- FPS и UI frame windows через `Choreographer`;
- jank count по порогу длительности кадра;
- p50/p95/p99 длительности кадров внутри окна;
- main-thread stalls через watchdog;
- memory snapshots: PSS, Java heap, native heap;
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

По умолчанию SDK стартует сам через provider:

```xml
<provider
    android:name="io.jankhunter.runtime.AutoInitProvider"
    android:authorities="${applicationId}.jankhunter-init"
    android:exported="false"
    android:initOrder="100" />
```

Если нужна ручная настройка:

```kotlin
val config = JankHunterConfig.builder()
    .enabled(true)
    .autoStartCollectors(true)
    .mainThreadStallThresholdMs(700)
    .memorySampleIntervalMs(10_000)
    .fpsMonitorEnabled(true)
    .fpsWindowMs(1_000)
    .jankFrameThresholdMs(32)
    .maxQueueSize(2048)
    .build()

JankHunter.init(context, config)
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
- failure flag;
- route в виде `METHOD /path`;
- owner из текущего `JankHunter.currentOwner()`.

Интеграция:

```kotlin
val client = OkHttpClient.Builder()
    .eventListenerFactory(JankHunterEventListenerFactory())
    .build()
```

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
        includePackages.add("com.myapp")
        excludePackages.add("com.myapp.generated")
    }
}
```

Следующий этап для plugin - ASM-инструментация OkHttp, Handler, Executor, Runnable/Callable и owner-map generation.

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
./gradlew assemble --no-daemon
```

Проверенные команды:

```bash
./gradlew :jankhunter-runtime:assemble --no-daemon
./gradlew assemble --no-daemon
```

CLI-часть уже умеет читать `.jhlog`, который пишет runtime, потому что формат событий синхронизирован между `android/jankhunter-runtime` и `cli/internal/jhlog`.
