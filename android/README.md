# Jank Hunter Android

Android-часть Jank Hunter отвечает за сбор сигналов внутри приложения и запись компактных `.jhlog` файлов. Эти файлы потом разбирает утилита `jankhunter` из каталога `cli/`.

Данные не отправляются в сеть. По умолчанию логи лежат в песочнице приложения, пока вы сами не заберёте их через `adb`, общий доступ Android или плагин для Android Studio.

## Модули

- `jankhunter-annotations`: лёгкие аннотации для атрибуции и управления ASM-внедрением.
- `jankhunter-runtime`: Android-библиотека сбора сигналов и записи `.jhlog`.
- `jankhunter-okhttp3`: слушатель OkHttp и помощники для WebSocket.
- `jankhunter-gradle-plugin`: Gradle-плагин, который добавляет настройки манифеста, создаёт карту владельцев, граф классов, диагностику ASM и внедряет перехватчики в байткод.
- `sample-app`: пример приложения для ручной проверки, утечек, сравнения прогонов и демонстрации отчётов.

## Быстрое Подключение

Для первого шага подключайте только отладочные или проверочные сборки:

```kotlin
dependencies {
    compileOnly("io.jankhunter:jankhunter-annotations:1.0.0")
    debugImplementation("io.jankhunter:jankhunter-runtime:1.0.0")
    debugImplementation("io.jankhunter:jankhunter-okhttp3:1.0.0")
}
```

`jankhunter-runtime` стартует через `ContentProvider`. В отладочном приложении сбор включается сам, если явно не выключен в манифесте или настройках.

Минимальные `meta-data`:

```xml
<meta-data android:name="io.jankhunter.enabled" android:value="true" />
<meta-data android:name="io.jankhunter.runtime_enabled" android:value="true" />
```

Часто полезно сразу задать пороги и лимиты:

```xml
<meta-data android:name="io.jankhunter.main_thread_stall_threshold_ms" android:value="700" />
<meta-data android:name="io.jankhunter.memory_sample_interval_ms" android:value="10000" />
<meta-data android:name="io.jankhunter.system_sampler_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.system_sample_interval_ms" android:value="15000" />
<meta-data android:name="io.jankhunter.fps_monitor_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.jank_frame_threshold_ms" android:value="32" />
<meta-data android:name="io.jankhunter.max_queue_size" android:value="2048" />
<meta-data android:name="io.jankhunter.max_log_bytes" android:value="524288" />
<meta-data android:name="io.jankhunter.max_log_directory_bytes" android:value="2097152" />
<meta-data android:name="io.jankhunter.log_bucket" android:value="session" />
<meta-data android:name="io.jankhunter.flush_interval_ms" android:value="5000" />
```

Ручная инициализация тоже доступна:

```kotlin
val config = JankHunterConfig.builder()
    .enabled(true)
    .runtimeEnabled(true)
    .autoStartCollectors(true)
    .mainThreadStallThresholdMs(700)
    .ownerBlockThresholdMs(250)
    .httpSlowThresholdMs(1_000)
    .memorySampleIntervalMs(10_000)
    .systemSamplerEnabled(true)
    .systemSampleIntervalMs(15_000)
    .mainLooperDispatchMonitorEnabled(true)
    .retainedHeapDumpEnabled(false)
    .retainedHeapDumpPrivacyApproved(false)
    .fpsMonitorEnabled(true)
    .jankStatsEnabled(true)
    .jankFrameThresholdMs(32)
    .uiWindowP95ThresholdMs(32)
    .maxQueueSize(2048)
    .maxLogBytes(512 * 1024)
    .maxLogDirectoryBytes(2 * 1024 * 1024)
    .logBucket(JankHunterLogBucket.SESSION)
    .flushIntervalMs(5_000)
    .build()

JankHunter.init(context, config)
```

## Переключатель Сбора

`io.jankhunter.enabled` является жёстким установочным переключателем. Если он равен `false`, библиотека не поднимется автоматически.

Для продуктовых или проверочных переключателей используйте `runtime_enabled`:

```xml
<meta-data android:name="io.jankhunter.enabled" android:value="true" />
<meta-data android:name="io.jankhunter.runtime_enabled" android:value="false" />
```

Затем включайте сбор из своей системы настроек:

```kotlin
val enabledForUser = featureFlags.isEnabled("jank_hunter_runtime")
JankHunter.setRuntimeEnabled(enabledForUser, "remote_config")
```

При выключении библиотека сбрасывает буфер, останавливает сборщики и закрывает запись. При включении снова открывается сегмент `.jhlog` и записываются сведения о сессии. Перезапуск приложения не нужен.

Проверки состояния:

```kotlin
JankHunter.isRuntimeEnabled()
JankHunter.isStarted()
JankHunter.flush()
```

## Автоподключение К Проекту

Из корня репозитория:

```bash
scripts/integrate-android-project.sh ~/work/MyApp
```

С явным модулем, пакетами и графом вызовов:

```bash
scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --module :app \
  --include-package com.myapp.feature \
  --include-package com.myapp.data \
  --exclude-packages com.myapp.generated,com.myapp.di \
  --runtime-call-graph
```

Скрипт:

- выбирает app-модуль, если `--module` не указан;
- публикует Android-модули Jank Hunter в `.jankhunter/maven`;
- собирает утилиту `jankhunter` в `.jankhunter/bin/jankhunter`;
- добавляет репозиторий в `settings.gradle` или `settings.gradle.kts`;
- прописывает `sdk.dir` в `local.properties`, если это нужно;
- подключает `io.jankhunter.android`, `jankhunter-annotations`, `jankhunter-runtime` и `jankhunter-okhttp3`;
- создаёт `jankHunter { ... }` с осторожными начальными настройками;
- оставляет копии изменённых файлов в `.jankhunter-backups/<timestamp>`.

Если проект должен собираться без локального Maven-репозитория, используйте AAR/JAR-файлы:

```bash
scripts/integrate-android-project.sh ~/work/MyApp --use-aar
```

Файлы попадут в `.jankhunter/lib`. Этот каталог не добавляется в `.gitignore`, чтобы его можно было хранить в репозитории приложения.

Полезные флаги:

```bash
scripts/integrate-android-project.sh ~/work/MyApp --android-sdk "$ANDROID_HOME"
scripts/integrate-android-project.sh ~/work/MyApp --android-build-tools 35.0.0
scripts/integrate-android-project.sh ~/work/MyApp --verify
scripts/integrate-android-project.sh ~/work/MyApp --dry-run
```

## Атрибуция Кода И Сценариев

Явный владелец работы:

```kotlin
JankHunter.withOwner("FeedRepository.refresh") {
    repository.refresh()
}
```

Сценарий и шаг:

```kotlin
JankHunter.withFlow("checkout.open") {
    JankHunter.markFlowStep("network")
    repository.loadCheckout()

    JankHunter.markFlowStep("render_list")
    adapter.submitList(items)
}
```

Аннотации нужны только на пути компиляции:

```kotlin
import io.jankhunter.annotations.JankIgnore
import io.jankhunter.annotations.JankOwner

@JankOwner("FeedRepository")
class FeedRepository {
    fun refresh() {
        // Перехватчики внутри метода получат владельца FeedRepository.
    }

    @JankIgnore
    fun generatedOrTooNoisyPath() {
        // Этот метод не будет обрабатываться ASM-проходом Jank Hunter.
    }
}
```

`@JankTrace`, `@JankFlow` и `@JankScreen` раскрываются Gradle-плагином в контекст выполнения. В отчётах они видны как экран, сценарий, шаг и трасса.

## Gradle-Плагин И ASM

Подключение:

```kotlin
plugins {
    id("io.jankhunter.android")
}
```

Пример для отладочных и проверочных сборок:

```kotlin
jankHunter {
    enabled = true
    enabledBuildTypes.add("debug")
    enabledBuildTypes.add("qa")
    autoInit = true
    logBucket = "session"

    runtime {
        mainThreadStallThresholdMs = 700
        ownerBlockThresholdMs = 250
        httpSlowThresholdMs = 1_000
        mainLooperDispatchMonitor = true
        jankStats = true
        mainProcessOnly = true
    }

    retainedHeapDump {
        enabled = false
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
        lifecycleLeaks = true
        logSpam = true
        classGraph = true
        runtimeCallGraph = true
        methodCounters = false
        allowEmptyIncludePackages = true
        asmProgressLog = false

        includePackages("com.myapp.feature", "com.myapp.data")
        excludePackages("com.myapp.generated", "com.myapp.di")
    }
}
```

Временное выключение всего Gradle-вклада:

```kotlin
jankHunter {
    enabled = false
}
```

Если модулей много, можно начать от `namespace` приложения:

```kotlin
jankHunter {
    instrument {
        includeWholeApplication = true
        excludePackages("com.myapp.generated", "com.myapp.di")
    }
}
```

Что внедряется:

- `okhttp`: оборачивает `OkHttpClient.Builder.build()` и `eventListenerFactory(...)`.
- `webSockets`: оборачивает `WebSocketListener`.
- `handlers`: оборачивает `Handler.post*`, сохраняя работу `removeCallbacks`, `removeCallbacksAndMessages` и `hasCallbacks`.
- `executors`: оборачивает `Runnable` и `Callable` в `Executor` и `ExecutorService`.
- `coroutines`: оборачивает основные создатели корутин без зависимости Android-библиотеки от `kotlinx.coroutines`.
- `flowInteractions`: создаёт сценарий клика при `View.setOnClickListener`, если явный сценарий ещё не задан.
- `lifecycleLeaks`: помогает связать удержанные объекты с жизненным циклом.
- `logSpam`: считает вызовы `android.util.Log.*` и Timber, не записывая текст логов.
- `classGraph`: пишет статический граф классов во время сборки.
- `runtimeCallGraph`: пишет агрегированные связи `caller -> callee` по реально выполненным методам.
- `methodCounters`: пишет счётчики входов в методы, по умолчанию выключено.

Для каждого варианта сборки создаются:

```text
build/generated/jankhunter/<variant>/owner-map.json
build/generated/jankhunter/<variant>/class-graph.jsonl
build/generated/jankhunter/<variant>/instrumentation-diagnostics.jsonl
```

Передавайте их в утилиту:

```bash
jankhunter inspect logs/*.jhlog \
  --owner-map build/generated/jankhunter/debug/owner-map.json \
  --mapping app/build/outputs/mapping/debug/mapping.txt \
  --class-graph build/generated/jankhunter/debug/class-graph.jsonl \
  --instrumentation-diagnostics build/generated/jankhunter/debug/instrumentation-diagnostics.jsonl \
  --out report.html
```

Так появляются `report-influence.html` и `report-diagnostics.html`.

## Защита Релизных Сборок

Плагин считает варианты вроде `release` и `paidRelease` чувствительными. Если вы всё-таки включаете Jank Hunter там, нужно явно подтвердить решение:

```kotlin
jankHunter {
    enabledBuildTypes.add("release")

    releaseSafety {
        allowInstrumentation = true
        privacyReviewed = true
        performanceBudgetEvidence = "docs/jankhunter-release-budget.md"
        allowDeviceInfo = false
        allowHeapDumps = false
        allowSecondaryProcesses = false
    }
}
```

Если включены дампы памяти, нужен ещё `allowHeapDumps = true`. Инструментирование зависимостей в релизных вариантах требует `allowDependencyInstrumentation = true`. Иначе плагин остановит сборку. Это не бюрократия, а ремень безопасности: на Плюке без него далеко не улетишь.

## Утечки Памяти И HPROF

Лёгкий режим включён всегда: библиотека записывает удержанные объекты, владельца, экран, сценарий, шаг, возраст и число удержаний.

Дампы памяти выключены по умолчанию. Для короткой диагностической сессии:

```kotlin
jankHunter {
    retainedHeapDump {
        enabled = true
        privacyApproved = true
        minIntervalMs = 600_000
        maxCount = 1
        minRetainedAgeMs = 30_000
    }
}
```

Или через манифест:

```xml
<meta-data android:name="io.jankhunter.retained_heap_dump_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.retained_heap_dump_privacy_approved" android:value="true" />
<meta-data android:name="io.jankhunter.retained_heap_dump_min_interval_ms" android:value="600000" />
<meta-data android:name="io.jankhunter.retained_heap_dump_max_count" android:value="1" />
<meta-data android:name="io.jankhunter.retained_heap_dump_min_retained_age_ms" android:value="30000" />
```

При подтверждённом удержании появится `retained-*.hprof` рядом с `.jhlog`. Утилита подключит его автоматически, если файл лежит рядом:

```bash
jankhunter inspect logs/*.jhlog --out report.html
```

Если дамп лежит отдельно:

```bash
jankhunter inspect logs/*.jhlog \
  --heap-dump retained-*.hprof \
  --out report.html
```

`report-leaks.html` в лёгком режиме показывает вероятную цепочку выполнения. С HPROF он показывает путь `GC root -> holder field -> retained object`, размер удержания, альтернативные пути и чеклист проверки.

Готовый помощник для отладочного приложения:

```bash
../cli/scripts/collect-android-leak-report.sh \
  --package com.example.app \
  --out /tmp/jankhunter-leaks
```

## Где Лежит Лог

По умолчанию:

```text
context.filesDir/jankhunter/session-<process>-<startMs>-<segment>.jhlog
```

Примеры:

```text
/data/data/com.myapp/files/jankhunter/session-main-1782338123456-1.jhlog
/data/data/com.myapp/files/jankhunter/session-remote-1782338123456-1.jhlog
```

`JankHunterLogBucket.SESSION` создаёт общий префикс для одного запуска сбора и добавляет сегменты при ротации. Уход приложения в фон только сбрасывает буфер, но не начинает новую сессию. Для дневной группировки есть `JankHunterLogBucket.DAILY`.

Каждый сегмент ограничен `max_log_bytes`, по умолчанию 512 КБ. Каталог ограничен `max_log_directory_bytes`, по умолчанию 2 МБ. Старые завершённые сегменты удаляются раньше текущего. Тело `.jhlog` сжимается gzip после magic-заголовка.

Забрать логи через `adb`:

```bash
APP_ID=com.myapp
mkdir -p logs

adb shell run-as "$APP_ID" ls files/jankhunter
adb exec-out run-as "$APP_ID" tar -C files/jankhunter -cf - . | tar -xf - -C logs

jankhunter inspect logs/*.jhlog --out report.html
```

Для сравнения:

```bash
jankhunter compare \
  --baseline "logs/baseline/*.jhlog" \
  --candidate "logs/candidate/*.jhlog" \
  --out compare.html
```

Если `adb run-as` неудобен, можно добавить в приложение отладочный `FileProvider` и отправлять `.jhlog` через системное окно общего доступа. В релизной сборке такой путь лучше не оставлять без отдельного решения по безопасности.

## Что С Собственной Нагрузкой

Практические правила:

- начните с Android-библиотеки, OkHttp, кадров, памяти и системного среза;
- ASM сначала ограничивайте своими пакетами;
- `includeWholeApplication = true` используйте вместе с `excludePackages`;
- `classGraph` не добавляет вызовы в приложение, он работает во время сборки;
- `runtimeCallGraph` добавляет лёгкие входы и выходы из методов, поэтому проверяйте затраты на быстрой проверочной сборке;
- `methodCounters` не включайте на весь проект без причины;
- HPROF включайте только для коротких проверочных сессий.

Сбор идёт асинхронно. При переполнении очереди событие отбрасывается со счётчиком, а не блокирует приложение.

Дополнительные ограничители:

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

## Пример Приложения И Проверки

Запуск примера:

```bash
../run-sample-app.sh
```

Сквозной прогон:

```bash
../scripts/android-e2e.sh
```

Проверки Android-части:

```bash
./gradlew detekt :jankhunter-gradle-plugin:test :jankhunter-okhttp3:testDebugUnitTest :jankhunter-runtime:testDebugUnitTest :sample-app:assembleDebug --no-daemon
```

Проверка Gradle-плагина как внешнего потребителя:

```bash
../scripts/gradle-plugin-smoke.sh
```

Локальные замеры горячих путей:

```bash
./gradlew :jankhunter-runtime:testDebugUnitTest \
  -Djankhunter.benchmark=true \
  -Djankhunter.benchmark.iterations=200000 \
  --tests io.jankhunter.runtime.JankHunterRuntimeBenchmarkTest \
  --no-daemon
```

Это быстрая проверка здравого смысла. Для окончательного решения по большому приложению всё равно нужен прогон на реальном устройстве или стенде.
