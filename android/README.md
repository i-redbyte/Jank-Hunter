# Jank Hunter Android

Android-часть Jank Hunter нужна, чтобы приложение само писало компактный `.jhlog` с performance-сигналами. Потом этот файл разбирает CLI и делает HTML-отчет.

SDK ничего не отправляет в сеть. Логи лежат внутри sandbox приложения, пока вы сами их не заберете.

Можно говорить “Jank Hunter SDK”, когда речь про Android-интеграцию: runtime, OkHttp-интеграцию и Gradle plugin. Если речь про весь набор целиком, точнее писать “Jank Hunter Android SDK + CLI”, потому что отчеты строит отдельная консольная утилита.

## Как подключить

Для начала лучше включать только debug/QA сборки:

```kotlin
dependencies {
    debugImplementation("io.jankhunter:jankhunter-runtime:0.1.0-SNAPSHOT")
    debugImplementation("io.jankhunter:jankhunter-okhttp3:0.1.0-SNAPSHOT")
}
```

Runtime стартует через `ContentProvider`. Если приложение debuggable, SDK включится сам, даже без правок `Application.onCreate()`.

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
    .flushIntervalMs(5_000)
    .build()

JankHunter.init(context, config)
```

Сбросить буфер вручную:

```kotlin
JankHunter.flush()
```

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

Runtime ротирует файлы по `max_log_bytes` и держит общий бюджет папки по `max_log_directory_bytes`. Если приложение много пишет, старые сегменты удаляются, активный файл не трогается.

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

Если неудобно использовать `adb run-as`, можно добавить FileProvider в само приложение и расшарить `.jhlog` через системный share sheet. Это уже код host-приложения, не обязательная часть SDK.

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
- retained objects без heap dump;
- counters/gauges;
- owner attribution через `JankHunter.withOwner(...)`;
- атрибуция флоу/контекста: экран, флоу, шаг и источник работ;
- агрегированные проблемные окна: медленный HTTP, паузы главного потока, UI-подтормаживания, удержания и спам логами;
- log spam monitor для `android.util.Log.*` и Timber без записи текста логов.

Высокочастотные события пишутся агрегатами. Это важно для больших приложений и особенно для проектов, где ANR-watch реагирует даже на задержки около 2 мс.

## OkHttp

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

    instrument {
        okhttp = true
        webSockets = true
        handlers = true
        executors = true
        coroutines = true
        flowInteractions = true
        logSpam = true
        classGraph = true
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

`asmProgressLog = true` печатает build-time прогресс ASM в одну строку: сколько классов просканировано, сколько обработано, примерная ETA и последний класс. По умолчанию выключено.

Что умеют hooks:

- `okhttp` - подмешивает `JankHunterEventListenerFactory`;
- `webSockets` - оборачивает `WebSocketListener`;
- `handlers` - оборачивает основные `Handler.post*`;
- `executors` - оборачивает `Runnable`/`Callable` в `Executor`/`ExecutorService`;
- `coroutines` - оборачивает основные `kotlinx.coroutines` builders без compile-time зависимости runtime от coroutines;
- `flowInteractions` - оборачивает `View.setOnClickListener` и создает flow для клика, если явный flow еще не задан;
- `logSpam` - считает вызовы `android.util.Log.*` и Timber по class/method/level, не сохраняя текст логов;
- `classGraph` - во время ASM-прохода пишет статический граф вызовов классов в отдельный файл. Байткод приложения ради этого не меняется;
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

## Что с overhead

Коротко: библиотека не должна подвешивать большое приложение, если не включать все подряд.

Практические правила:

- начинайте с runtime + OkHttp + FPS + memory/system sampler;
- ASM включайте сначала на `com.myapp.feature` / `com.myapp.data`, потом расширяйте;
- `includeWholeApplication = true` используйте осознанно и с `excludePackages`;
- `classGraph` можно оставлять включенным: он работает на build-time и не добавляет runtime-вызовы;
- `main_looper_dispatch_monitor_enabled` держите выключенным, пока реально не нужен;
- `methodCounters` не включайте на весь проект без причины;
- `coroutines` включайте после smoke-сборки, потому что это широкий bytecode hook.

Runtime пишет асинхронно, через очередь и flush-интервал. Если очередь переполнена, события дропаются счетчиком, а не блокируют приложение.

## Бенчмарки overhead

В runtime есть opt-in unit benchmarks для горячих путей: flow API, счетчик log spam и создание wrapper. Они не запускаются по умолчанию, чтобы не замедлять обычные тесты и не делать CI шумным.

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
./gradlew :jankhunter-runtime:testDebugUnitTest :sample-app:assembleDebug --no-daemon
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
io.jankhunter:jankhunter-okhttp3:0.1.0-SNAPSHOT
io.jankhunter:jankhunter-gradle-plugin:0.1.0-SNAPSHOT
io.jankhunter.android:io.jankhunter.android.gradle.plugin:0.1.0-SNAPSHOT
```
