# Jank Hunter Android

Android-часть Jank Hunter отвечает за сбор сигналов внутри приложения и запись компактных `.jhlog` файлов. Эти файлы потом разбирает утилита `jankhunter` из каталога `cli/`.

Данные не отправляются в сеть. По умолчанию логи лежат в песочнице приложения, пока вы сами не заберёте их через `adb`, общий доступ Android или плагин для Android Studio.

## Модули

- `jankhunter-annotations`: лёгкие аннотации для атрибуции и управления ASM-внедрением.
- `jankhunter-runtime`: Android-библиотека сбора сигналов и записи `.jhlog`.
- `jankhunter-okhttp3`: слушатель OkHttp и помощники для WebSocket.
- `jankhunter-android-sdk`: единая публичная зависимость, которая транзитивно подключает runtime, annotations и OkHttp/WebSocket support.
- `jankhunter-gradle-plugin`: Gradle-плагин, который добавляет настройки манифеста, создаёт карту владельцев, граф классов, диагностику ASM и внедряет перехватчики в байткод.
- `sample-app`: пример приложения для ручной проверки, утечек, сравнения прогонов и демонстрации отчётов.

## Быстрое Подключение

Для первого шага подключайте только отладочные или проверочные сборки:

```kotlin
plugins {
    id("io.jankhunter.android") version "1.0.0"
}
dependencies {
    implementation("io.jankhunter:jankhunter-android-sdk:1.0.0")
}
```

Это единственная пользовательская dependency: SDK экспортирует runtime, annotations и
OkHttp/WebSocket support. Плагин проверяет её наличие до ASM и останавливает небезопасное
внедрение, если dependency resolution был вручную изменён.

Для каждого включённого application-варианта плагин создаёт manifest metadata с `io.jankhunter.enabled=true` и runtime-настройками. `io.jankhunter.runtime.JankHunterAutoInitProvider` добавляется в этот манифест только при `autoInit = true`; при `autoInit = false` настройки сохраняются для ручного `JankHunter.init(...)`, но provider не генерируется. Library-модули и выключенные варианты не получают ни runtime manifest, ни provider.

При фактической сборке любого включённого application-варианта Gradle один раз выводит заметный баннер с версией Jank Hunter. Он не зависит от `verboseLogs`; выключенные варианты его не выводят:

```text
================JANK HUNTER 1.0.0 ENABLED================
```

Без Gradle-плагина можно подключить runtime вручную и вызвать `JankHunter.init(...)`:

```kotlin
dependencies {
    debugImplementation("io.jankhunter:jankhunter-runtime:1.0.0")
}
```

Аннотации без ASM-прохода сами по себе ничего не внедряют. Для ручного auto-init нужно самостоятельно объявить `JankHunterAutoInitProvider` в манифесте; безопаснее начать с явного вызова `JankHunter.init(...)`.

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
<meta-data android:name="io.jankhunter.session_log_size_limit_enabled" android:value="true" />
<meta-data android:name="io.jankhunter.max_session_log_size_mib" android:value="16" />
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
    .mainLooperDispatchMonitorEnabled(false)
    .retainedHeapDumpEnabled(false)
    .retainedHeapDumpPrivacyApproved(false)
    .fpsMonitorEnabled(true)
    .jankStatsEnabled(true)
    .jankFrameThresholdMs(32)
    .uiWindowP95ThresholdMs(32)
    .maxQueueSize(2048)
    .sessionLogSizeLimitEnabled(true)
    .maxSessionLogSizeMiB(16)
    .flushIntervalMs(5_000)
    .build()

JankHunter.init(context, config)
```

При включённых `jankStats` и `fpsMonitor` используется один поток UI-событий: JankStats имеет
приоритет для активного окна, а Choreographer включается только как fallback. Глобальный
`MainLooper.setMessageLogging(Printer)` по умолчанию выключен; включайте
`mainLooperDispatchMonitor` явно только для короткой углублённой диагностики.

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

При выключении библиотека сбрасывает буфер, останавливает сборщики и запечатывает текущий `.jhlog`. При включении начинается новая сессия сбора с новым файлом. Перезапуск приложения не нужен.

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
- подключает `io.jankhunter.android`; annotations и runtime добавляются Gradle-плагином, а опциональный OkHttp helper — явной variant-зависимостью;
- создаёт `jankHunter { ... }` с осторожными начальными настройками;
- оставляет копии изменённых файлов в `.jankhunter-backups/<timestamp>`.

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
import io.jankhunter.annotations.JankHunterIgnore
import io.jankhunter.annotations.JankHunterOwner

@JankHunterOwner("FeedRepository")
class FeedRepository {
    fun refresh() {
        // Перехватчики внутри метода получат владельца FeedRepository.
    }

    @JankHunterIgnore
    fun generatedOrTooNoisyPath() {
        // Этот метод не будет обрабатываться ASM-проходом Jank Hunter.
    }
}
```

`@JankHunterTrace`, `@JankHunterFlow` и `@JankHunterScreen` раскрываются Gradle-плагином в контекст выполнения. В отчётах они видны как экран, сценарий, шаг и трасса.

## Gradle-Плагин И ASM

Подключение:

```kotlin
import io.jankhunter.gradle.JankHunterFeatureMode
import io.jankhunter.gradle.JankHunterSymbolMode

plugins {
    id("io.jankhunter.android")
}
```

Пример для отладочных и проверочных сборок:

```kotlin
jankHunter {
    // Самодостаточный .jhlog: системному CLI не нужны файлы Gradle-сборки.
    symbolMode = JankHunterSymbolMode.EMBEDDED
    enabled = true
    enabledBuildTypes.add("debug")
    enabledBuildTypes.add("qa")
    autoInit = true
    sessionLogSizeLimitEnabled = true
    maxSessionLogSizeMiB = 16
    verboseLogs = false
    dependencyInjectionAnalysis = JankHunterFeatureMode.DISABLED

    runtime {
        mainThreadStallThresholdMs = 700
        ownerBlockThresholdMs = 250
        httpSlowThresholdMs = 1_000
        mainLooperDispatchMonitor = false
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
        okhttp = false
        webSockets = false
        handlers = true
        executors = true
        coroutines = false
        flowInteractions = true
        lifecycleLeaks = true
        logSpam = true
        classGraph = true
        runtimeCallGraph = false
        methodCounters = false
        includeWholeApplication = false
        asmProgressLog = false

        includePackages("com.myapp.feature", "com.myapp.data")
        excludePackages("com.myapp.generated", "com.myapp.di")
    }
}
```

`instrument.okhttp` и `instrument.webSockets` используют support из единой
`jankhunter-android-sdk`; отдельная `jankhunter-okhttp3` dependency не нужна.

Метаданные устройства собираются всегда для стабильной атрибуции локальных отчётов.
Для проверки приватности используйте `releaseSafety.privacyReviewed`, `processNameRedactor`
и redactor сетевых маршрутов; сами `.jhlog` файлы остаются локальными, пока приложение
явно их не выгрузит.

Временное выключение всего Gradle-вклада:

```kotlin
jankHunter {
    enabled = false
}
```

Без include-настроек плагин уже использует `namespace` модуля как безопасную границу. Дополнительные пакеты можно указать явно:

```kotlin
jankHunter {
    instrument {
        includePackages("com.myapp.shared")
        excludePackages("com.myapp.generated", "com.myapp.di")
    }
}
```

Чтобы application-модуль инструментировал классы всех подключённых project-модулей и внешних
зависимостей, включите `includeWholeApplication = true`. Системные пакеты, AndroidX, Kotlin,
OkHttp и сам Jank Hunter при этом остаются исключёнными; дополнительные исключения задаются через
`excludePackages`.

Что внедряется:

- `okhttp`: оборачивает `OkHttpClient.Builder.build()` и `eventListenerFactory(...)`.
- `webSockets`: оборачивает `WebSocketListener`.
- `handlers`: оборачивает `Handler.post*`, сохраняя работу `removeCallbacks`, `removeCallbacksAndMessages` и `hasCallbacks`.
- `executors`: оборачивает `Runnable` и `Callable` в `Executor` и `ExecutorService`.
- `coroutines`: оборачивает основные создатели корутин без зависимости Android-библиотеки от `kotlinx.coroutines`; по умолчанию выключено из-за цены ASM и wrapper-объектов.
- `flowInteractions`: создаёт сценарий клика при `View.setOnClickListener`, если явный сценарий ещё не задан.
- `lifecycleLeaks`: помогает связать удержанные объекты с жизненным циклом.
- `logSpam`: считает вызовы `android.util.Log.*` и Timber, не записывая текст логов.
- `classGraph`: пишет статический граф классов во время сборки.
- `runtimeCallGraph`: пишет агрегированные связи `caller -> callee` по реально выполненным методам.
- `methodCounters`: пишет счётчики входов в методы, по умолчанию выключено.

Отдельный build-time анализ DI включается явно:

```kotlin
jankHunter {
    dependencyInjectionAnalysis = JankHunterFeatureMode.ENABLED
}
```

Он читает `@Inject`, `@Provides`, `@Binds`, Hilt metadata, Dagger Factory/MembersInjector и
Koin annotations/KSP. Generated DI-классы не получают `JankHunterHooks` или marker и не создают
runtime-события. Произвольный Koin runtime DSL намеренно не интерпретируется без достоверного
статического контракта.

`runtimeCallGraph`, `methodCounters`, OkHttp и WebSocket hooks по умолчанию выключены. В `<init>`
call-site hooks добавляются только после обязательного вызова `super`/`this`, а `<clinit>` не меняется.
Повторный ASM-проход распознаётся по marker-аннотации, runtime-вызовы идут через fail-open
`JankHunterHooks`. По умолчанию `.jhlog` сам хранит определения реально встретившихся методов
`stable ID -> class.method`. Системный CLI поэтому раскрывает имена и runtime-связи, имея только
лог; определения пишутся один раз на метод и не расходуют лимит обычного runtime-словаря.

Developer-only режим с минимальным размером лога включается явно:

```kotlin
jankHunter {
    symbolMode = JankHunterSymbolMode.STABLE_EXTERNAL
}
```

В нём `.jhlog` хранит только стабильные 64-битные ID вида `stable:0x0123abcd...`, поэтому для
анализа обязательны matching Gradle artifacts и ключ CLI `--external-symbols`. Не используйте
этот режим для логов, которые будут разбирать люди без доступа к сборке проекта.

Плагин вычисляет глобальный 16-байтный `symbolNamespace` из точной версии алгоритма
stable ID и owner-map schema. Он намеренно одинаков для application и library модулей: один
процесс может исполнять инструментированный код из нескольких модулей, а в `.jhlog`
есть один header. Fingerprint попадает в manifest и `owner-map.json`; CLI отклоняет карты с
несовместимым контрактом.

Для каждого варианта сборки создаются:

```text
build/generated/jankhunter/<variant>/owner-map.json
build/generated/jankhunter/<variant>/class-graph.jsonl
build/generated/jankhunter/<variant>/instrumentation-diagnostics.jsonl
build/generated/jankhunter/<variant>/di-catalog.jsonl  # только при ENABLED
```

В стандартном `EMBEDDED`-режиме эти файлы необязательны. Их можно передать разработчику для
дополнительного статического графа, ASM-диагностики, DI-каталога и deobfuscation:

```bash
jankhunter inspect logs/*.jhlog \
  --owner-map app/build/generated/jankhunter/debug/owner-map.json \
  --owner-map feature/feed/build/generated/jankhunter/debug/owner-map.json \
  --mapping app/build/outputs/mapping/debug/mapping.txt \
  --class-graph app/build/generated/jankhunter/debug/class-graph.jsonl \
  --instrumentation-diagnostics app/build/generated/jankhunter/debug/instrumentation-diagnostics.jsonl \
  --di-catalog app/build/generated/jankhunter/debug/di-catalog.jsonl \
  --out report.html
```

Так в едином `report.html` появляются вкладки «Граф влияния», «ASM диагностика» и «DI-каталог».
Повторяйте `--owner-map` для `app` и каждого инструментированного feature/library-модуля.
Все карты должны иметь один `symbolNamespace`: это общий контракт stable-ID algorithm и
owner-map schema, а не revision исходников. CLI останавливает анализ, если namespace карт
различается или один stable ID указывает на разные методы. Эти требования к owner-map относятся
к `STABLE_EXTERNAL`; в `EMBEDDED` имена уже находятся в `.jhlog`. Для внешнего режима добавьте
обязательный ключ `--external-symbols`:

```bash
jankhunter inspect logs/*.jhlog \
  --external-symbols \
  --artifacts-dir app/build/generated/jankhunter/debug \
  --out report.html
```

Вкладка «DI-каталог» всегда отделяет build-time wiring от телеметрии. DI edges не передаются в
leak/runtime score, severity, evidence или граф влияния.

## Защита Релизных Сборок

Плагин считает варианты вроде `release` и `paidRelease` чувствительными. Если вы всё-таки включаете Jank Hunter там, нужно явно подтвердить решение:

```kotlin
jankHunter {
    enabledBuildTypes.add("release")

    runtime {
        mainProcessOnly = true
    }

    releaseSafety {
        allowInstrumentation = true
        privacyReviewed = true
        performanceBudgetEvidence = "docs/jankhunter-release-budget.md"
        allowHeapDumps = false
        allowSecondaryProcesses = false
    }
}
```

`releaseSafety` не переписывает runtime-настройки молча: он только останавливает сборку, если для release-like варианта включена чувствительная возможность без явного подтверждения. Если включены дампы памяти, нужен ещё `allowHeapDumps = true`; если `runtime.mainProcessOnly = false`, нужен `allowSecondaryProcesses = true`.

В application-модуле ASM visitors работают с `InstrumentationScope.ALL`, поэтому могут видеть код
подключённых модулей и зависимостей; в library-модуле область остаётся `PROJECT`. Android namespace
задаёт безопасную область пакетов по умолчанию, `includePackages` расширяет её, а
`includeWholeApplication = true` снимает пакетное ограничение для прикладных классов. Системные и
служебные пакеты по-прежнему исключаются, а `excludePackages` позволяет точечно исключить код.

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

Watcher сначала записывает лёгкий сигнал удержания, но не держит сам объект сильной ссылкой.
Если `minRetainedAgeMs` ещё не достигнут, слабое наблюдение продолжается до этого порога:
HPROF создаётся только когда объект всё ещё жив. Освободившийся до порога объект не вызывает dump.

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

Вкладка «Утечки памяти» в `report.html` в лёгком режиме показывает вероятную цепочку выполнения. С HPROF она показывает путь `GC root -> holder field -> retained object`, размер удержания, альтернативные пути и чеклист проверки.

Готовый помощник для отладочного приложения:

```bash
../cli/scripts/collect-android-leak-report.sh \
  --package com.example.app \
  --out /tmp/jankhunter-leaks
```

## Где Лежит Лог

По умолчанию:

```text
context.filesDir/jankhunter/jh-session-log.YYYY-MM-DD.<index>.jhlog
```

Пример:

```text
/data/data/com.myapp/files/jankhunter/jh-session-log.2026-07-14.42.jhlog
```

Одна сессия сбора пишет ровно один append-only файл. Локальная дата фиксируется при старте сессии, а десятичный `index` монотонно растёт и не переиспользуется. Имя файла намеренно не содержит process name: run/process/session identity хранится в заголовке `.jhlog v9`, и CLI группирует данные по содержимому, а не по имени. Уход приложения в фон только сбрасывает уже накопленные чанки и не начинает новый файл.

Файл и writer thread создаются лениво — только после первого принятого события. Частые метрики и runtime edges используют bounded bulk-очередь, а session/lifecycle, stall, problem, retained и heap-dump evidence имеют отдельный critical-резерв. Насыщение bulk-очереди поэтому не вытесняет критические данные; writer объединяет обе очереди по глобальному admission sequence и сохраняет детерминированный порядок.

Внутренний ограничитель текущего файла управляется `session_log_size_limit_enabled` и
`max_session_log_size_mib`; по умолчанию он включён и равен 16 МиБ. При достижении лимита
writer best-effort записывает terminal `SIZE_LIMIT`, запечатывает этот же файл и не создаёт
продолжение или recovery-файл. `session_log_size_limit_enabled=false` снимает внутренний
per-session cap у встроенного хранилища. Положительный `JankHunterBinaryStorage.fileSizeLimitBytes`
действует всегда; если включены оба ограничителя, writer использует меньшее значение.

Встроенное хранилище автоматически удаляет старые закрытые сессии сверх фиксированного бюджета
64 МиБ, не затрагивая текущий или другой активный файл. Пользовательский `JankHunterBinaryStorage`
самостоятельно управляет архивным бюджетом и очисткой через `cleanup`.

`.jhlog v9` — не один gzip-поток после magic. Файл состоит из заголовка и независимо gzip-сжатых чанков с CRC и commit trailer. Поэтому CLI читает только полностью зафиксированный префикс; незавершённый хвост активного файла помечается как `open_with_tail`, а не как повреждение.

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
- высокочастотный ASM ограничен `InstrumentationScope.PROJECT`, а lifecycle-only проход — Android `namespace`;
- дополнительные include/exclude задавайте по границам пакетов;
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
