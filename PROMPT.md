# Подробный промт для Codex / GPT 5.5: Jank Hunter

Ты работаешь над проектом **Jank Hunter** в репозитории `github.com/i-redbyte/Jank-Hunter`.

Jank Hunter - это набор инструментов для Android-приложений, который помогает находить, измерять и сравнивать performance-регрессии: UI jank/FPS, ANR, зависания main thread, сетевые задержки, DNS/connect/TTFB, websocket reconnect storms, memory pressure, GC, утечки памяти, log/request/error spam, системный контекст устройства и сети.

## Главная цель

Сделать систему, которую можно подключить к большому старому Android-приложению с Activity, Fragment, Jetpack Compose, Kotlin coroutines, RxJava, Java concurrency и OkHttp, желательно без ручной инициализации в `Application`.

Система должна:

1. Аккуратно собирать performance-события и агрегаты в файл.
2. Минимизировать overhead в приложении.
3. Не тащить лишние runtime-зависимости и не конфликтовать со старыми библиотеками host-приложения.
4. Уметь связывать деградации с вероятными владельцами: класс, метод, owner-id, route, screen, stack signature.
5. Давать CLI-утилиту, которая анализирует один лог или сравнивает несколько логов/пулы логов.
6. Генерировать красивый standalone HTML+CSS+JS отчет с графиками, summary, regression table и top suspects.

## Выбранный стек

- Android runtime SDK: Java-first, минимум зависимостей.
- Gradle plugin / ASM instrumentation: Kotlin или Java, build-time only.
- CLI: Go, без VM, допускается GC.
- Формат логов: бинарный `.jhlog`, оптимизированный под machine parsing, varint, bit flags, dictionary encoding.
- Debug/export формат: JSONL, генерируется CLI, не является основным runtime-форматом.

## Структура репозитория

```text
Jank-Hunter/
  cli/
    cmd/jankhunter/
    internal/jhlog/
    internal/analyze/
    internal/report/
  android/
    jankhunter-runtime/
    jankhunter-okhttp3/
    jankhunter-gradle-plugin/
  docs/
```

## CLI требования

CLI называется `jankhunter`.

Базовые команды:

```bash
jankhunter inspect app.jhlog --out report.html
jankhunter inspect logs/*.jhlog --out report.html
jankhunter compare --baseline old/*.jhlog --candidate new/*.jhlog --out compare.html
jankhunter export app.jhlog --format jsonl --out app.jhlog.jsonl
jankhunter sample --out sample.jhlog
```

CLI должен:

- читать `.jhlog`;
- поддерживать streaming parser, чтобы большие логи не требовали загрузки всего файла в память;
- агрегировать основные метрики;
- сравнивать baseline/candidate;
- показывать p50/p95/p99, rate/min, count/session, deltas;
- группировать по screen, route, owner, stack signature, network type, device tier;
- генерировать standalone HTML report без внешних CDN;
- уметь экспортировать события в JSONL для отладки.

## `.jhlog` формат

Основной формат бинарный.

Идея:

```text
magic/version
record*

record:
  event_type: uvarint
  timestamp_delta_ms: uvarint
  flags: uvarint
  payload_len: uvarint
  payload: event-specific bytes
```

Высокочастотные события нельзя писать по одному без нужды. UI frames, counters и memory samples должны агрегироваться окнами.

Строки нельзя писать постоянно. Использовать словари:

```text
id -> "FeedRepository.refresh"
id -> "GET /feed"
id -> "FeedScreen"
id -> "com.app.checkout.CheckoutActivity"
```

Runtime пишет `owner_id`, `route_id`, `screen_id`, `class_id`, `stack_sig_id`.
CLI раскрывает ID в человекочитаемые имена.

Bit flags использовать для дешевых булевых признаков:

```text
HTTP_REUSED_CONNECTION
HTTP_FAILED
HTTP_TLS
THREAD_MAIN
APP_FOREGROUND
NETWORK_METERED
```

## Android SDK требования

Модули:

1. `jankhunter-runtime`
   - Java-only Android library.
   - Никаких third-party runtime dependencies.
   - Auto-init через manifest `ContentProvider`, без AndroidX App Startup.
   - Event queue + background writer.
   - Binary log writer.
   - Activity lifecycle tracking.
   - Main thread watchdog.
   - Memory sampler.
   - Public API для owner attribution:

```java
JankHunter.withOwner("FeedRepository.refresh", () -> {
    // measured block
});
```

2. `jankhunter-okhttp3`
   - Optional integration module.
   - Не должен приносить OkHttp в host-приложение как runtime-зависимость.
   - Должен использовать compileOnly/provided-подход.
   - EventListener.Factory должен быть composite-friendly и не затирать существующий listener.

3. `jankhunter-gradle-plugin`
   - Build-time plugin.
   - DSL:

```kotlin
plugins {
    id("io.jankhunter.android")
}

jankHunter {
    enabledBuildTypes.set(setOf("debug", "qa"))
    autoInit.set(true)

    instrument {
        activities.set(true)
        fragments.set(true)
        okhttp.set(true)
        webSockets.set(true)
        handlers.set(true)
        executors.set(true)
        rxJava.set(true)
        coroutines.set(false)

        includePackages.set(listOf("com.myapp"))
        excludePackages.set(listOf("com.myapp.generated"))
    }
}
```

Gradle plugin должен в будущем использовать Android Gradle Plugin ASM APIs и генерировать owner map:

```json
{
  "1842": "com.myapp.feed.FeedRepository.refresh(FeedRepository.kt:47)"
}
```

Runtime должен писать только короткие owner IDs, а CLI должен раскрывать их через map.

## Attribution / поиск виновников

Нужно стремиться к модели:

1. Explicit owner: разработчик явно вызывает `withOwner`.
2. Generated owner: Gradle/ASM вставляет owner-id в call-site.
3. Sampled stack: SDK редко снимает stack trace только для slow/error events.
4. Triggered profiling: тяжелая диагностика включается только при симптомах.

Главные output-секции отчета:

- Top slow routes.
- Top janky screens.
- Top main thread stall owners.
- Top memory growth / retained classes.
- Top websocket reconnect sources.
- Top changed metrics between baseline/candidate.
- Top suspects.

## Ограничения и стиль реализации

- Минимизировать внешние зависимости.
- Runtime SDK не должен зависеть от Kotlin stdlib.
- Runtime SDK не должен зависеть от AndroidX в core-модуле.
- Optional integrations должны быть отдельными модулями.
- Стабильность host-приложения важнее полноты метрик.
- Все тяжелые операции должны быть sampled, rate-limited или debug/QA-only.
- Нельзя логировать PII: URL query, headers, body, user id, tokens.
- Логи должны быть append-only и устойчивы к обрыву записи.
- CLI должен терпимо читать частично поврежденный файл и показывать предупреждения.

## Приоритет MVP

1. Go CLI:
   - `.jhlog` reader/writer;
   - sample generator;
   - inspect;
   - compare;
   - export JSONL;
   - HTML report.

2. Android runtime:
   - auto-init provider;
   - config;
   - event queue;
   - binary writer;
   - activity lifecycle;
   - main thread watchdog;
   - memory sampler;
   - public owner API.

3. Optional OkHttp:
   - EventListener.Factory scaffold;
   - DNS/connect/TLS/request/response/failure timings.

4. Gradle plugin:
   - DSL scaffold;
   - variant enable/disable;
   - future ASM hook points;
   - owner map design.

## Критерии качества

- `go test ./...` проходит в `cli/`.
- `jankhunter sample` создает валидный `.jhlog`.
- `jankhunter inspect` генерирует HTML-отчет.
- `jankhunter compare` генерирует HTML-сравнение.
- Android core module не содержит third-party runtime dependencies.
- Публичные API маленькие и стабильные.
- README объясняет запуск и архитектуру.

Когда работаешь над проектом, сначала проверяй существующие файлы и не ломай структуру. Если меняешь формат `.jhlog`, обновляй документацию и sample/export. Если добавляешь Android-зависимость, объясняй, почему она не конфликтует с host-приложением.
