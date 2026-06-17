# Jank Hunter Android

Android-часть Jank Hunter нужна, чтобы приложение само писало компактный `.jhlog` с сигналами производительности. Потом этот файл разбирает CLI и делает HTML-отчет.

Jank Hunter ничего не отправляет в сеть. Логи лежат внутри песочницы приложения, пока вы сами их не заберете.

## Как подключить

Для начала лучше включать только debug/QA сборки:

```kotlin
dependencies {
    compileOnly("io.jankhunter:jankhunter-annotations:0.1.0-SNAPSHOT")
    debugImplementation("io.jankhunter:jankhunter-runtime:0.1.0-SNAPSHOT")
    debugImplementation("io.jankhunter:jankhunter-okhttp3:0.1.0-SNAPSHOT")
}
```

Runtime стартует через `ContentProvider`. Если приложение debuggable, Jank Hunter включится сам, даже без правок `Application.onCreate()`.

Минимальная manifest-настройка:

```xml
<meta-data android:name="io.jankhunter.enabled" android:value="true" />
```

Чаще полезно сразу задать пороги и лимиты:

```xml
<meta-data android:name="io.jankhunter.main_thread_stall_threshold_ms" android:value="700" />
<meta-data android:name="io.jankhunter.memory_sample_interval_ms" android:value="10000" />
<meta-data android:name="io.jankhunter.system_sampler_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.system_sample_interval_ms" android:value="15000" />
<meta-data android:name="io.jankhunter.fps_monitor_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.jank_frame_threshold_ms" android:value="32" />
<meta-data android:name="io.jankhunter.max_queue_size" android:value="2048" />
<meta-data android:name="io.jankhunter.max_log_bytes" android:value="5242880" />
<meta-data android:name="io.jankhunter.max_log_directory_bytes" android:value="26214400" />
<meta-data android:name="io.jankhunter.log_compression_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.flush_interval_ms" android:value="5000" />
```

Если нужна ручная инициализация:

```kotlin
val config = JankHunterConfig.builder()
    .enabled(true)
    .autoStartCollectors(true)
    .mainThreadStallThresholdMs(700)
    .memorySampleIntervalMs(10_000)
    .systemSamplerEnabled(true)
    .systemSampleIntervalMs(15_000)
    .fpsMonitorEnabled(true)
    .jankFrameThresholdMs(32)
    .maxQueueSize(2048)
    .maxLogBytes(5 * 1024 * 1024)
    .maxLogDirectoryBytes(25 * 1024 * 1024)
    .logCompressionEnabled(true)
    .flushIntervalMs(5_000)
    .build()

JankHunter.init(context, config)
```

## Автоподключение в проект

Для первого подключения к существующему многомодульному Android-проекту можно использовать macOS/bash-скрипт из корня Jank Hunter:

```bash
scripts/integrate-android-project.sh ~/work/MyApp
```

Если нужно сразу сузить ASM и включить runtime-граф вызовов:

```bash
scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --module :app \
  --include-package com.myapp.feature \
  --include-package com.myapp.data \
  --exclude-packages com.myapp.generated,com.myapp.di \
  --runtime-call-graph
```

Что делает скрипт:

- публикует Jank Hunter в локальный Maven repo целевого проекта: `.jankhunter/maven`;
- собирает CLI и кладет бинарник в `.jankhunter/bin/jankhunter`;
- прописывает Android SDK в `local.properties` через `sdk.dir`;
- добавляет этот repo в `settings.gradle` или `settings.gradle.kts`;
- подключает `io.jankhunter.android`, `jankhunter-annotations`, `jankhunter-runtime` и `jankhunter-okhttp3` в указанный модуль;
- добавляет `jankHunter { ... }` с безопасными дефолтами для debug-сборки;
- оставляет backup измененных файлов в `.jankhunter-backups/<timestamp>`.

Обычно `--module` указывать не нужно: скрипт сам ранжирует app-кандидаты и выбирает основной app по Android application plugin или alias, launchable manifest, manifest `android:name`, `Application` subclass, `applicationId`, совпадению с именем проекта, имени модуля и штрафу для test/benchmark/sample-модулей. Если проект совсем нестандартный, можно переопределить выбор через `--module :mobile:app`. Для нескольких Android-модулей флаг можно повторять. Для большого проекта удобно начать с `--include-package`, а `--include-whole-application` включать только когда понятен список `excludePackages`.

Путь к Android SDK скрипт берет из `--android-sdk`, `ANDROID_HOME`, `ANDROID_SDK_ROOT` или стандартного macOS пути `~/Library/Android/sdk`. Этот путь используется и для публикации Jank Hunter из локального clone, и для `local.properties` целевого проекта. Для публикации скрипт также выбирает установленную версию Build Tools из `$ANDROID_HOME/build-tools`; при необходимости ее можно задать явно через `--android-build-tools 35.0.0`. Если `local.properties` уже содержит `sdk.dir`, обычный запуск его не перезаписывает.

Сбросить буфер вручную:

```kotlin
JankHunter.flush()
```

## Аннотации attribution

Если не хочется прописывать owner вручную вокруг каждого блока, можно добавить lightweight-аннотации. Они нужны только на compile classpath и не тянут Android/runtime-зависимости:

```kotlin
import io.jankhunter.annotations.JankIgnore
import io.jankhunter.annotations.JankOwner

@JankOwner("FeedRepository")
class FeedRepository {
    fun refresh() {
        // ASM hooks внутри метода получат owner FeedRepository.
    }

    @JankIgnore
    fun generatedOrTooNoisyPath() {
        // Метод не будет инструментирован Jank Hunter ASM hooks.
    }
}
```

`@JankTrace`, `@JankFlow` и `@JankScreen` уже распознаются Gradle plugin как metadata для следующих attribution-слоев.

## Где лежит лог

По умолчанию:

```text
context.filesDir/jankhunter/session-<process>-<timestamp>-<segment>.jhlog
```

Например:

```text
/data/data/com.myapp/files/jankhunter/session-main-1781410978146-1.jhlog
/data/data/com.myapp/files/jankhunter/session-remote-1781410978146-1.jhlog
```

Каждый сегмент `.jhlog` ограничен `max_log_bytes`. Когда суммарный размер папки становится больше `max_log_directory_bytes`, Jank Hunter удаляет самые старые завершенные сегменты и продолжает писать в текущий файл. По умолчанию тело `.jhlog` сжимается потоковым gzip после magic-заголовка, поэтому длинные QA-сессии занимают меньше места на диске и быстрее вытаскиваются через `adb`.

Путь можно поменять:

```kotlin
JankHunterConfig.builder()
    .logDirectory(File(context.filesDir, "my-jankhunter-logs"))
    .build()
```

## Как забрать лог через adb

Для debuggable-приложения самый простой путь - `run-as`:

```bash
APP_ID=com.myapp
mkdir -p logs

adb shell run-as "$APP_ID" ls files/jankhunter
adb exec-out run-as "$APP_ID" tar -C files/jankhunter -cf - . | tar -xf - -C logs
```

После этого строим отчет:

```bash
jankhunter inspect logs/*.jhlog --out report.html
```

Если `jankhunter` еще не установлен:

```bash
cd ../cli
make build
./bin/jankhunter inspect ../android/logs/*.jhlog --out /tmp/jankhunter-report.html
```

Для сравнения двух прогонов:

```bash
jankhunter compare \
  --baseline "logs/baseline/*.jhlog" \
  --candidate "logs/candidate/*.jhlog" \
  --out compare.html
```

## Как отдать лог через FileProvider

Если неудобно использовать `adb run-as`, можно добавить FileProvider в само приложение и расшарить `.jhlog` через системный share sheet. Это уже код host-приложения, не обязательная часть Jank Hunter.

Зависимость, если AndroidX Core еще нет:

```kotlin
debugImplementation("androidx.core:core:<ваша-версия>")
```

Manifest:

```xml
<provider
    android:name="androidx.core.content.FileProvider"
    android:authorities="${applicationId}.jankhunter-files"
    android:exported="false"
    android:grantUriPermissions="true">
    <meta-data
        android:name="android.support.FILE_PROVIDER_PATHS"
        android:resource="@xml/jankhunter_file_paths" />
</provider>
```

`res/xml/jankhunter_file_paths.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<paths>
    <files-path name="jankhunter" path="jankhunter/" />
</paths>
```

Пример кнопки “поделиться последним логом”:

```kotlin
val dir = File(filesDir, "jankhunter")
val latest = dir
    .listFiles { file -> file.extension == "jhlog" && file.length() > 0L }
    ?.maxByOrNull { it.lastModified() }
    ?: return

val uri = FileProvider.getUriForFile(
    this,
    "${BuildConfig.APPLICATION_ID}.jankhunter-files",
    latest,
)

val intent = Intent(Intent.ACTION_SEND)
    .setType("application/octet-stream")
    .putExtra(Intent.EXTRA_STREAM, uri)
    .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)

startActivity(Intent.createChooser(intent, "Share Jank Hunter log"))
```

Для release-сборок такой provider лучше не оставлять без явной причины. Для debug/QA это удобный способ быстро вытащить лог с чужого устройства.

## Что собирает runtime

- информация о сессии: app version/build, process, модель устройства, Android/API/security patch, ABI, brand/hardware/board/product, рут-доступ;
- текущий экран через `ActivityLifecycleCallbacks`;
- контекст системы: батарея, RAM, low-memory flag, тип сети, metered/validated/VPN, UID traffic, свободное место в app data partition;
- FPS и UI frame windows через `Choreographer`;
- main-thread stalls через watchdog;
- memory snapshots: PSS, Java heap, native heap;
- previous process exit summary на Android 11+;
- retained objects без heap dump в легком runtime-режиме; CLI может дополнительно связать эти события с HPROF/heap evidence;
- counters/gauges;
- owner attribution через `JankHunter.withOwner(...)`;
- атрибуция флоу/контекста: экран, флоу, шаг и источник работ;
- агрегированные проблемные окна: медленный HTTP, паузы главного потока, UI-подтормаживания, удержания и спам логами;
- log spam monitor для `android.util.Log.*` и Timber без записи текста логов.

Высокочастотные события пишутся агрегатами. Это важно для больших приложений и особенно для проектов, где ANR-watch реагирует даже на задержки около 2 мс.

## OkHttp

Если включен ASM-перехватчик `okhttp = true`, отдельный вызов `eventListenerFactory(...)` обычно не нужен: plugin перехватывает `OkHttpClient.Builder.build()`, читает текущую фабрику слушателей, оборачивает ее слушателем Jank Hunter и возвращает тот же builder. Если фабрика уже была обернута, повторной обертки не будет.

Ручное подключение остается полезным, когда ASM выключен или вы хотите явно контролировать конкретный клиент:

Подключение:

```kotlin
val client = OkHttpClient.Builder()
    .eventListenerFactory(JankHunterEventListenerFactory())
    .build()
```

Если уже есть свой `EventListener.Factory`:

```kotlin
val client = OkHttpClient.Builder()
    .eventListenerFactory(JankHunterEventListenerFactory(existingFactory))
    .build()
```

Собирается route вида `METHOD /path`, длительность запроса, DNS/connect/TTFB, status class, bytes, reused connection, TLS/failure flag и текущий owner.

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

## Owner attribution

Самый надежный сигнал - явный owner:

```kotlin
JankHunter.withOwner("FeedRepository.refresh") {
    repository.refresh()
}
```

Тогда в отчете видно не только “HTTP p95 вырос”, но и “какой код чаще всего был рядом с проблемой”.

## Flow / Interaction API

Флоу связывает просадку с пользовательским сценарием: “открытие checkout”, “сетевой шаг”, “рендер списка”, “клик по кнопке”.

```kotlin
JankHunter.withFlow("checkout.open") {
    JankHunter.markFlowStep("network")
    repository.loadCheckout()

    JankHunter.markFlowStep("render_list")
    adapter.submitList(items)
}
```

Если нужен ручной жизненный цикл:

```kotlin
val flow = JankHunter.startFlow("feed.refresh")
try {
    JankHunter.markFlowStep("network")
    repository.refresh()
} finally {
    JankHunter.endFlow(flow)
}
```

Runtime держит текущий `screen + owner + flow + step` и приклеивает этот контекст к HTTP, паузам главного потока, wrapped work, coroutine, пользовательским метрикам и UI-окнам. В `.jhlog` это пишется компактно: строки лежат в словаре, а событие хранит только ID и bitmask полей, которые реально есть.

## Gradle plugin и ASM

Plugin подключается в приложении:

```kotlin
plugins {
    id("io.jankhunter.android")
}
```

Пример для debug/QA:

```kotlin
jankHunter {
    enabledBuildTypes.add("debug")
    enabledBuildTypes.add("qa")
    autoInit = true

    retainedHeapDump {
        enabled = true
        minIntervalMs = 600_000
        maxCount = 1
        minRetainedAgeMs = 30_000
    }

    instrument {
        okhttp = true
        webSockets = true
        handlers = true
        executors = true
        coroutines = true
        flowInteractions = true
        logSpam = true
        classGraph = true
        runtimeCallGraph = false
        methodCounters = false
        allowEmptyIncludePackages = false
        asmProgressLog = false

        includePackages("com.myapp.feature", "com.myapp.data")
        excludePackages("com.myapp.generated", "com.myapp.di")
    }
}
```

Если модулей очень много:

```kotlin
jankHunter {
    instrument {
        includeWholeApplication = true
        excludePackages(
            "com.myapp.generated",
            "com.myapp.di",
        )
    }
}
```

`includeWholeApplication = true` берет Android `namespace` variant и добавляет его в include-list. Это удобно, когда в проекте 200+ модулей и руками перечислять пакеты бессмысленно.

`asmProgressLog = true` печатает build-time прогресс ASM в одну строку: сколько классов просканировано, сколько обработано, примерная ETA и последний класс. При ручной настройке по умолчанию выключено. Скрипт автоподключения включает прогресс в сгенерированном конфиге; если это мешает, запускайте его с `--no-asm-progress-log`.

Что умеют ASM-перехватчики:

- `okhttp` - подмешивает `JankHunterEventListenerFactory`: перехватывает явный `eventListenerFactory(...)` и финальный `OkHttpClient.Builder.build()`;
- `webSockets` - оборачивает `WebSocketListener`;
- `handlers` - оборачивает `Handler.post`, `postAtFrontOfQueue`, `postDelayed` и `postAtTime`; runtime ведет слабый реестр оберток, поэтому `removeCallbacks`, `removeCallbacksAndMessages` и `hasCallbacks` продолжают работать с исходным `Runnable`;
- `executors` - оборачивает `Runnable`/`Callable` в `Executor`/`ExecutorService`;
- `coroutines` - оборачивает основные `kotlinx.coroutines` builders без compile-time зависимости runtime от coroutines;
- `flowInteractions` - оборачивает `View.setOnClickListener` и создает flow для клика, если явный flow еще не задан;
- `logSpam` - считает вызовы `android.util.Log.*` и Timber по class/method/level, не сохраняя текст логов;
- `classGraph` - во время ASM-прохода пишет статический граф вызовов классов в отдельный файл. Байткод приложения ради этого не меняется;
- `runtimeCallGraph` - опционально добавляет легкие enter/exit hooks и пишет агрегированные runtime-связи `caller -> callee`;
- `methodCounters` - пишет счетчики входа в методы, по умолчанию выключено.

Для каждого variant plugin пишет owner-map и class graph:

```text
build/generated/jankhunter/<variant>/owner-map.json
build/generated/jankhunter/<variant>/class-graph.jsonl
```

CLI принимает их так:

```bash
jankhunter inspect logs/*.jhlog \
  --owner-map build/generated/jankhunter/debug/owner-map.json \
  --class-graph build/generated/jankhunter/debug/class-graph.jsonl \
  --out report.html
```

`class-graph.jsonl` нужен для отдельного отчета `report-influence.html`: там видно, какие классы стали “злыми” узлами, через какие связи они влияют на другие классы и где это подтвердилось runtime-сигналами. Узел без runtime-доказательств не считается виновником: он просто связан со статическим графом, но в конкретном прогоне мог не выполниться.

`runtimeCallGraph = true` добавляет runtime-ребра между реально выполненными методами. Лог не пишет каждое событие вызова: runtime держит счетчики по `screen + caller + flow + step + callee`, а в `.jhlog` сбрасывает агрегаты пачками. Для большого приложения это лучше включать после smoke-сборки и сначала на ограниченные include-пакеты или на `includeWholeApplication = true` с хорошим списком exclude.

## Heap dump для утечек

В Gradle plugin heap dump выключен по умолчанию и включается только явно для выбранных debug/QA variant. В новом `jankHunter { ... }` блок настроек сразу лежит рядом с instrumentation-настройками, чтобы его можно было быстро включить и задать лимиты:

```kotlin
jankHunter {
    retainedHeapDump {
        enabled = true
        minIntervalMs = 600_000
        maxCount = 1
        minRetainedAgeMs = 30_000
    }
}
```

В app-модуле плагин сам проставит runtime meta-data, и `AutoInitProvider` соберет такой же конфиг, как при ручном `JankHunterConfig.builder().retainedHeapDumpEnabled(true)`. В library-модулях плагин только инструментирует классы текущего модуля и не добавляет runtime manifest-настройки в consuming app. `minRetainedAgeMs` не дает снимать HPROF по слишком молодым объектам. Если блок не указан или `enabled = false`, SDK остается в легком режиме без HPROF.

Если Gradle plugin не используется или нужен ручной override, можно включить те же настройки через manifest:

```xml
<meta-data android:name="io.jankhunter.retained_heap_dump_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.retained_heap_dump_min_interval_ms" android:value="600000" />
<meta-data android:name="io.jankhunter.retained_heap_dump_max_count" android:value="1" />
<meta-data android:name="io.jankhunter.retained_heap_dump_min_retained_age_ms" android:value="30000" />
```

При подтвержденном retained object runtime сохранит `.hprof` в `files/jankhunter/heap-dumps/` и запишет counters/gauges `jankhunter.heap_dump.*` в `.jhlog`. Дальше передайте дамп в CLI:

```bash
jankhunter inspect logs/*.jhlog \
  --heap-dump heap-dumps/retained-*.hprof \
  --out report.html
```

Heap dump останавливает VM на время записи, поэтому держите режим выключенным по умолчанию и используйте лимиты для больших приложений.

## Что с overhead

Коротко: библиотека не должна подвешивать большое приложение, если не включать все подряд.

Практические правила:

- начинайте с runtime + OkHttp + FPS + memory/system sampler;
- ASM включайте сначала на `com.myapp.feature` / `com.myapp.data`, потом расширяйте;
- `includeWholeApplication = true` используйте осознанно и с `excludePackages`;
- `classGraph` можно оставлять включенным: он работает на build-time и не добавляет runtime-вызовы;
- `runtimeCallGraph` включайте осознанно: он агрегирует вызовы, но все равно добавляет enter/exit hook в выбранные методы;
- `main_looper_dispatch_monitor_enabled` держите выключенным, пока реально не нужен;
- `methodCounters` не включайте на весь проект без причины;
- `coroutines` включайте после smoke-сборки, потому что это широкий bytecode hook.

Runtime пишет асинхронно, через очередь и flush-интервал. Если очередь переполнена, события дропаются счетчиком, а не блокируют приложение.

Для долгих прогонов включены два защитных механизма:

- adaptive sampling для стабильных memory/context snapshots: повторяющиеся значения пропускаются, но заметный сдвиг PSS/heap/RAM/traffic/network/low-memory сразу записывается;
- агрегация counters/gauges: горячие метрики складываются в окне `io.jankhunter.metric_aggregation_window_ms`, counters пишутся суммой, gauges — средним и max.

Настройки:

```xml
<meta-data android:name="io.jankhunter.adaptive_sampling_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.adaptive_memory_stable_interval_ms" android:value="60000" />
<meta-data android:name="io.jankhunter.adaptive_context_stable_interval_ms" android:value="60000" />
<meta-data android:name="io.jankhunter.metric_aggregation_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.metric_aggregation_window_ms" android:value="5000" />
<meta-data android:name="io.jankhunter.max_metric_aggregation_keys" android:value="2048" />
<meta-data android:name="io.jankhunter.max_log_spam_keys" android:value="2048" />
<meta-data android:name="io.jankhunter.max_runtime_call_graph_keys" android:value="4096" />
<meta-data android:name="io.jankhunter.max_handler_tracking_entries" android:value="4096" />
<meta-data android:name="io.jankhunter.max_handler_wrappers_per_runnable" android:value="32" />
```

При превышении cardinality runtime пишет счетчики `jankhunter.metric_aggregation.dropped.count`, `jankhunter.log_spam.dropped_keys.count`, `jankhunter.runtime_call_graph.dropped.count`, `jankhunter.handler_wrapper.dropped_entries.count` или `jankhunter.handler_wrapper.dropped_wrappers.count`, а не раздувает `.jhlog` новыми ключами и registry-записями.

## Бенчмарки overhead

В runtime есть opt-in unit benchmarks для горячих путей: flow API, счетчик log spam, создание и выполнение wrapper, ASM enter/exit guard, runtime wrappers, log writer, агрегация метрик и coroutine propagation. Они не запускаются по умолчанию, чтобы не замедлять обычные тесты и не делать CI шумным.

Запуск:

```bash
cd android
./gradlew :jankhunter-runtime:testDebugUnitTest \
  -Djankhunter.benchmark=true \
  -Djankhunter.benchmark.iterations=200000 \
  --tests io.jankhunter.runtime.JankHunterRuntimeBenchmarkTest \
  --no-daemon
```

Это быстрый локальный sanity-check overhead. Для финального решения по большому приложению лучше отдельно добавить device benchmark на реальном устройстве или CI-стенде, потому что JVM unit benchmark не видит планировщик Android, ART/JIT и реальную нагрузку UI-потока.

## Проверка

```bash
cd android
./gradlew detekt :jankhunter-runtime:testDebugUnitTest :sample-app:assembleDebug --no-daemon
```

`detekt` настроен как Kotlin/ktlint formatting-check с official code style. Отчеты лежат в `build/reports/detekt` каждого Android-модуля.

Проверка Gradle plugin как внешнего потребителя через локальный Maven:

```bash
cd ..
./scripts/gradle-plugin-smoke.sh
```

End-to-end:

```bash
cd ..
./scripts/android-e2e.sh
```

Отчет появится здесь:

```text
reports/android-e2e/report.html
reports/android-e2e/inspect.json
```

## Локальная публикация

```bash
cd android
./gradlew publishToMavenLocal --no-daemon
```

Snapshot-координаты:

```text
io.jankhunter:jankhunter-runtime:0.1.0-SNAPSHOT
io.jankhunter:jankhunter-annotations:0.1.0-SNAPSHOT
io.jankhunter:jankhunter-okhttp3:0.1.0-SNAPSHOT
io.jankhunter:jankhunter-gradle-plugin:0.1.0-SNAPSHOT
io.jankhunter.android:io.jankhunter.android.gradle.plugin:0.1.0-SNAPSHOT
```
