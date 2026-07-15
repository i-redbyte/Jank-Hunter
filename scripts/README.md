# Служебные скрипты Jank Hunter

Скрипты в этой директории подключают Jank Hunter к Android-проекту, проверяют Gradle-плагин и сквозной сценарий на устройстве, а также фиксируют локальный performance-контракт. Запускайте их из корня репозитория Jank Hunter: так команды и пути в примерах совпадут с фактическим окружением.

## Что здесь находится

| Файл | Назначение |
| --- | --- |
| `integrate-android-project.sh` | Локально публикует Android-артефакты и CLI, затем подключает их к другому Android-проекту. Поддерживает как первичную, так и повторную интеграцию. |
| `gradle-plugin-smoke.sh` | Публикует Jank Hunter в изолированный Maven-репозиторий, собирает внешний тестовый app/library-проект и проверяет контракт Gradle-плагина. |
| `android-e2e.sh` | Запускает instrumentation-тест sample app на устройстве, забирает `.jhlog` и строит JSON/HTML-отчёт. |
| `performance-baseline.py` | Создаёт воспроизводимый локальный performance-снимок и сравнивает candidate с reference и порогами качества. |
| `test_performance_baseline.py` | Быстрые unit-тесты формата performance-снимка, парсеров и fail-closed проверок. Реальные бенчмарки не запускает. |
| `test_scripts.py` | Regression-тесты интегратора, Android E2E quality gate и валидации smoke-скрипта на временных fixtures. Устройство и полную Android-сборку не запускает. |

У каждого исполняемого скрипта есть актуальная справка:

```bash
./scripts/integrate-android-project.sh --help
./scripts/gradle-plugin-smoke.sh --help
./scripts/android-e2e.sh --help
python3 -B scripts/performance-baseline.py --help
python3 -B scripts/performance-baseline.py capture --help
python3 -B scripts/performance-baseline.py check --help
```

Быстрая проверка логики всех шести скриптов:

```bash
python3 -B scripts/test_scripts.py -v
python3 -B scripts/test_performance_baseline.py -v
```

## Общие требования и безопасный запуск

Базовый набор инструментов:

- Bash и стандартные Unix-утилиты (`awk`, `sed`, `find`, `grep`, `tar`), а для интегратора также Perl;
- Go для сборки CLI и генерации отчётов;
- JDK 17 или новее для Android/Gradle-задач;
- Android SDK с нужной платформой и установленными Build Tools;
- Gradle wrapper из `android/`; для `--verify` также желателен wrapper целевого проекта;
- Python 3 для performance runner и его unit-тестов;
- `adb` и онлайн-устройство/эмулятор только для `android-e2e.sh`.

Перед изменением реального приложения рекомендуется иметь чистое состояние Git и сначала выполнить интегратор с `--dry-run`. Скрипты не создают коммиты и не отправляют изменения в удалённый репозиторий.

## Интеграция в Android-проект

### Первый запуск

Минимальный вариант:

```bash
./scripts/integrate-android-project.sh ~/work/MyApp
```

Практический вариант с явной областью ASM и физическим лимитом `.jhlog`:

```bash
./scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --module :app \
  --build-type debug \
  --include-package com.myapp.feature \
  --include-package com.myapp.data \
  --exclude-packages com.myapp.generated,com.myapp.di \
  --max-session-log-size-mib 16 \
  --verify
```

Если `--module` не задан, скрипт ищет Android application-модули и предпочитает настоящий запускаемый app-модуль. Для нестандартного или многомодульного проекта безопаснее передать один или несколько модулей явно:

```bash
./scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --module :app \
  --module :demo
```

Повторяемые `--module`, `--include-package`, `--exclude-package` и `--build-type` накапливаются. Для package/build type поддерживается и список через запятую. Дубликаты удаляются с сохранением порядка.

### Что изменяется

По умолчанию скрипт:

1. Читает group/version из `android/gradle.properties` текущего checkout Jank Hunter.
2. Публикует Android-артефакты в `<target>/.jankhunter/maven`.
3. Собирает CLI и копирует executable в `<target>/.jankhunter/bin/jankhunter`.
4. Добавляет локальный Maven-репозиторий в `pluginManagement` и `dependencyResolutionManagement` файла `settings.gradle(.kts)`.
5. Создаёт или обновляет `sdk.dir` в `local.properties`.
6. Добавляет plugin `io.jankhunter.android` в выбранные `build.gradle(.kts)`; DSL-блок создаётся только для явно переданных опций, иначе работают defaults плагина.
7. Добавляет одну публичную зависимость `implementation("io.jankhunter:jankhunter-android-sdk:<version>")`; runtime, annotations и OkHttp/WebSocket support приходят транзитивно.
8. Добавляет `.jankhunter/`, `.jankhunter-backups/`, `local.properties`, а также явные custom `--maven-dir`/`--cli-dir` в `.gitignore`.

Перед записью существующие файлы копируются с сохранением относительного пути в:

```text
<target>/.jankhunter-backups/YYYYMMDD-HHMMSS-<pid>.<random>/
```

`--maven-dir` и `--cli-dir` принимают только относительные пути внутри target. Выход через symlink, обход через `..`, повисшие ссылки и не-директории в пути отклоняются. Backup base `.jankhunter-backups` тоже канонизируется и обязан оставаться внутри target.

До публикации артефактов и любой записи в target выполняется полный preflight: `settings`, root Gradle, все выбранные модули, version catalog, managed markers и все затрагиваемые файлы. Неоднозначная Gradle-структура, дублирующиеся/перепутанные `BEGIN/END` или symlink вместо `settings.gradle(.kts)`, root/module build file, `libs.versions.toml`, `local.properties` или `.gitignore` завершают работу до изменений.

Файлы target пишутся через private temporary file и atomic rename. После старта транзакции любая ошибка, включая неуспешный `--verify`, восстанавливает Gradle/config-файлы из backup. Локальный Maven repo, CLI и Gradle build outputs в эту транзакцию не входят и при ошибке могут остаться для диагностики.

### Повторный запуск поверх существующей интеграции

Повторный запуск предназначен для обновления уже подключённого проекта:

- managed repository/config/helper blocks распознаются только по exact `BEGIN/END` markers, заменяются на месте и не дублируются;
- ручные `jankHunter { ... }`, dependencies, repositories и комментарии без markers сохраняются, включая manual `jankhunter-*` dependencies;
- helper-блок старой версии скрипта мигрируется с учётом Gradle-строк, скобок и комментариев; посторонние строки из него не теряются;
- версия literal `io.jankhunter.android` обновляется в модуле, `pluginManagement` файла settings и корневом `plugins { ... apply false }`; поддерживаются и one-line `plugins`, а текст в строках/комментариях за declaration не выдаётся;
- standard `gradle/libs.versions.toml` поддерживает inline `version` и отдельный, не используемый другими alias `version.ref`;
- shared `version.ref`, unknown/custom alias, non-literal/legacy apply-versioning, дублирующиеся plugin declarations и другая неоднозначность приводят к fail-fast без записи;
- повторный запуск с теми же аргументами оставляет Gradle/settings/catalog byte-for-byte неизменными.

Если на повторном запуске не передать ни одной DSL/network/build-type опции, скрипт сохранит свой прежний managed overlay и helper. Если передана хотя бы одна такая опция, managed overlay пересобирается только из явно указанных значений, а скрипт пишет об этом в лог. Ручной `jankHunter`-блок остаётся, но более поздний managed overlay имеет precedence для тех же полей.

Настройки DI-анализа и лимита размера добавляются в overlay только при явном выборе:

```bash
./scripts/integrate-android-project.sh ~/work/MyApp \
  --analyze-di \
  --max-session-log-size-mib 16

./scripts/integrate-android-project.sh ~/work/MyApp \
  --no-analyze-di \
  --no-session-log-size-limit
```

Без новых DSL-опций эти строки сохраняются. Чтобы изменить их, передайте новый полный набор явных overlay-опций.

### Опции интегратора

| Опция | Эффект |
| --- | --- |
| `PATH` или `--target PATH` | Корень изменяемого Android-проекта; обязателен. |
| `--jankhunter PATH` | Checkout Jank Hunter, из которого берутся версия, артефакты и CLI. |
| `--module :app` | Модуль для изменения; можно повторять. Алиас: `--app-module`. |
| `--include-package PACKAGE` | Include-prefix ASM; можно повторять. Алиас: `--include`. |
| `--include-packages a,b` | Несколько include-prefix. Алиас: `--includes`. |
| `--exclude-package PACKAGE` | Exclude-prefix ASM; можно повторять. Алиас: `--exclude`. |
| `--exclude-packages a,b` | Несколько exclude-prefix. Алиас: `--excludes`. |
| `--build-type TYPE` | Явный список включённых build types; можно повторять или передать CSV. Без опции existing overlay сохраняется, новая интеграция полагается на default плагина. |
| `--runtime-call-graph` / `--no-runtime-call-graph` | Включает/выключает глубокий runtime caller → callee graph. |
| `--okhttp` / `--no-okhttp` | Включает/выключает OkHttp hooks. Единая SDK-зависимость не меняется. |
| `--websockets` / `--no-websockets` | Включает/выключает WebSocket hooks. Единая SDK-зависимость не меняется. |
| `--analyze-di` / `--no-analyze-di` | Включает/выключает build-time анализ Dagger/Hilt/Koin. Поддерживаются также варианты `analyse`. |
| `--asm-progress-log` / `--no-asm-progress-log` | Управляет однострочной ASM-диагностикой во время сборки. |
| `--max-session-log-size-mib N` | Включает физический лимит одной сессии `.jhlog`, `N` — положительное число MiB. |
| `--no-session-log-size-limit` | Явно отключает физический лимит файла. |
| `--maven-dir PATH` | Локальный Maven repo внутри target; default `.jankhunter/maven`. |
| `--cli-dir PATH` | Директория CLI внутри target; default `.jankhunter/bin`. |
| `--android-sdk PATH` | Явный Android SDK. Алиас: `--android-sdk-dir`. |
| `--android-build-tools VERSION` | Явная установленная версия Build Tools. Алиас: `--android-build-tools-version`. |
| `--verify` | После записи запускает `<module>:tasks` и проверяет Gradle resolution. |
| `--dry-run` | Выполняет тот же fail-fast preflight и показывает план, но не собирает, не публикует и не пишет; `--verify` пропускается. |
| `--skip-publish` | Не публикует Android-артефакты; требуемые файлы уже должны находиться в выбранном Maven repo. Алиас: `--no-build`. |
| `--skip-cli-build` | Не собирает CLI; в target уже должен быть executable `jankhunter`. |
| `--skip-local-properties` | Не меняет `local.properties`. SDK всё равно используется при публикации Android-артефактов. |
| `--no-gitignore` | Не меняет `.gitignore`. |

SDK ищется в следующем порядке: `--android-sdk`, `ANDROID_HOME`, `ANDROID_SDK_ROOT`, валидный `sdk.dir` target, валидный `android/local.properties` Jank Hunter, стандартный путь macOS `~/Library/Android/sdk`, затем Linux `~/Android/Sdk`. Если версия Build Tools не задана, выбирается максимальная установленная числовая версия.

Release-like build types намеренно отклоняются. Для релизной интеграции нужны ручная настройка `releaseSafety` и отдельные замеры влияния библиотеки на приложение.

### Regression-тесты shell-скриптов

```bash
python3 -B scripts/test_scripts.py -v
```

Тесты создают temporary Kotlin/Groovy projects и проверяют first/re-integration, managed/manual ownership, root/settings/catalog versioning, idempotence, fail-fast/rollback/symlink safety, fake Android E2E quality gate и smoke argument validation. Они не заменяют полный `gradle-plugin-smoke.sh` и реальный E2E на устройстве.

## Smoke-тест Gradle-плагина

Запуск:

```bash
./scripts/gradle-plugin-smoke.sh
```

Это host-side проверка, устройство не требуется. Скрипт:

- находит JDK 17+ и Android SDK;
- публикует артефакты в изолированный временный Maven repo;
- создаёт внешний Kotlin DSL fixture с application- и library-модулями;
- собирает `:app:assembleDebug` с Handler/Log/OkHttp/WebSocket, ASM/runtime call graph и DI analysis fixtures;
- проверяет metadata опубликованных plugin/runtime/annotations/OkHttp артефактов;
- проверяет, что build banner `JANK HUNTER <version> ENABLED` появился ровно один раз;
- проверяет owner map, class graph, instrumentation diagnostics, DI catalog, runtime manifest, auto-init provider, kill switch и metadata лимита сессии;
- по умолчанию повторяет consumer build и требует reuse configuration cache;
- убеждается, что library-модуль не получил runtime manifest приложения.

Требуются JDK 17+, Android SDK, платформа `android-35` по умолчанию, Build Tools и доступ к зависимостям Gradle. Настройка выполняется переменными окружения:

| Переменная | Назначение |
| --- | --- |
| `SMOKE_JAVA_HOME` | JDK 17+; затем проверяются `JAVA_HOME`, macOS helper и `java` из `PATH`. |
| `SMOKE_AGP_VERSION` | AGP consumer fixture; по умолчанию читается из `android/build.gradle.kts`. |
| `SMOKE_COMPILE_SDK` | `compileSdk`/`targetSdk`, default `35`. |
| `ANDROID_BUILD_TOOLS_VERSION` | Установленная версия Build Tools; без неё берётся максимальная. |
| `SMOKE_CONFIGURATION_CACHE=0` | Отключает двойную проверку create/reuse configuration cache. |
| `SMOKE_WORK_DIR` | Родитель для новой уникальной cold-run директории. Она сохраняется после завершения. |
| `KEEP_SMOKE_DIR=1` | Сохраняет автоматически созданную директорию `/tmp/jankhunter-gradle-smoke.*`. |

Пример для диагностики:

```bash
SMOKE_JAVA_HOME="$JAVA_HOME" \
SMOKE_WORK_DIR=/tmp/jankhunter-smoke-debug \
./scripts/gradle-plugin-smoke.sh
```

Каждый запуск создаёт новый cold fixture и не переиспользует вывод прошлого прогона. Без `SMOKE_WORK_DIR` и `KEEP_SMOKE_DIR=1` временная директория удаляется при выходе только при наличии корректного ownership marker. При сохранении изучайте `consumer-build.txt`, `consumer-build-cached.txt`, `maven/` и `consumer/build/`.

## Сквозной Android E2E

Запуск при одном подключённом устройстве:

```bash
./scripts/android-e2e.sh
```

Если устройств несколько, serial обязателен:

```bash
adb devices
./scripts/android-e2e.sh --serial emulator-5554

# Эквивалент через стандартную переменную adb:
ANDROID_SERIAL=emulator-5554 ./scripts/android-e2e.sh
```

Скрипт учитывает только устройства в состоянии `device`; `offline` и `unauthorized` не выбираются. Перед стартом должны быть доступны `adb`, `go`, `tar`, Android Gradle wrapper и онлайн-устройство/эмулятор. Скрипт намеренно работает только с `io.jankhunter.sample`: package ID обоих APK проверяются через `aapt` до установки.

Параметры и их env-эквиваленты:

| Опция | Переменная | Default |
| --- | --- | --- |
| `--out-dir PATH` | `OUT_DIR` | `reports/android-e2e` |
| `--serial SERIAL` | `ANDROID_SERIAL` | единственное онлайн-устройство |
| `--instrumentation-diagnostics PATH` | — | не передаётся |
| — | `ADB` | `adb` из `PATH` |
| — | `PYTHON` | `python3` из `PATH` |
| — | `ANDROID_HOME` / `ANDROID_SDK_ROOT` | Android SDK; затем `android/local.properties` и standard path |
| — | `ANDROID_BUILD_TOOLS_VERSION` | максимальная установленная числовая версия |

`--instrumentation-diagnostics` принимает непустой JSONL, созданный Gradle-плагином, и передаёт его CLI-команде `inspect`. Если файл указан, предупреждение об отсутствующей ASM-диагностике считается ошибкой; без файла допускается только это одно диагностическое предупреждение.

Последовательность проверки:

1. Gradle собирает debug APK sample app и instrumentation APK с найденной версией Build Tools.
2. До установки `aapt` подтверждает package ID, а `adb shell pm list packages` проверяет, что sample app и test package ещё не установлены. Скрипт не перезаписывает и не удаляет существующее приложение с данными.
3. Скрипт устанавливает оба APK, напрямую запускает ровно `SampleEndToEndLogTest` и требует итог `OK (1 test)`.
4. Пока package ещё установлен, `adb exec-out run-as io.jankhunter.sample` копирует `files/jankhunter-e2e` с устройства; затем оба тестовых package строго удаляются. Ошибка uninstall делает прогон неуспешным, а EXIT trap повторяет cleanup и печатает диагностику.
5. CLI получает все найденные `.jhlog`, автоматически подключает полный debug bundle Gradle-плагина, если он создан, и строит JSON и HTML. Явный `--instrumentation-diagnostics` имеет приоритет для diagnostics-файла.
6. Fail-closed quality gate проверяет ненулевые события/словарь, sealed chain, отсутствие потерь и overflow/truncation, обязательные sample counters/gauge/screen/owner и запрещённые warnings.
7. Ошибка instrumentation/cleanup, пустые логи, отсутствующие выходные файлы или неполные данные считаются ошибкой.

Результат:

```text
reports/android-e2e/
├── .jankhunter-android-e2e-owned
├── instrumentation.txt
├── logs/*.jhlog
├── inspect.json
└── report.html
```

Важно: скрипт очищает `--out-dir` только при наличии точного ownership marker. На первом запуске marker создаётся только в пустой директории. Непустой чужой каталог, symlink/marker с неверным содержимым, `/`, домашняя директория и корень репозитория не очищаются.

## Performance reference/candidate

### Что измеряется

`performance-baseline.py capture` создаёт детерминированный синтетический `.jhlog` и измеряет:

- Go-бенчмарки decoder/analyzer/report: `ns/op`, `B/op`, `allocs/op`;
- Android runtime hot paths;
- размеры runtime AAR, annotations JAR, OkHttp AAR, Gradle plugin JAR и sample APK;
- wall time и peak RSS команд CLI `size`, `inspect --json`, HTML `inspect` и `compare`;
- суммарный размер и состав страниц HTML-отчётов;
- точную композицию fixture, счётчики событий/словаря и структурированный `CollectionQuality`.

Текущие схемы: capture `4`, fixture `2`, acceptance `3`. При capture проверяются exact fields, типы и numeric bounds. На `check` обе стороны — reference и candidate — обязаны иметь pristine `CollectionQuality`: `level=high`, complete/valid sealed chain, ноль unsealed/lost/overflow/truncated, равные accepted/written/fixture event counts, пустые issues/reasons и ни одного forbidden quality warning. Плохой reference не может «узаконить» потери в candidate.

Профиль `representative` предназначен для реального сравнения, `smoke` — только для быстрой отладки runner.

### Требования

Для полного capture нужны Python 3, Go, JDK, Android SDK/Gradle и `/usr/bin/time` на macOS/Linux для peak RSS. При `--skip-android` Android runtime/artifacts пропускаются; при `--skip-go-benchmarks` пропускаются только Go microbenchmarks, но Go всё ещё нужен для fixture и CLI.

Версию Build Tools можно зафиксировать через `--android-build-tools VERSION` (алиас `--android-build-tools-version`); иначе runner ищет SDK candidate с stable numeric Build Tools и берёт максимальную версию. Порядок candidates: `ANDROID_HOME`, `ANDROID_SDK_ROOT`, `android/local.properties`, standard SDK path для ОС. Выбранная версия записывается в capture и передаётся Gradle.

Для Java runner берёт `$JAVA_HOME/bin/java`, если `JAVA_HOME` задан, иначе `java` из `PATH`. В environment capture попадают OS/release, architecture, CPU identifier, Python, Go и Java. Снимайте reference и candidate на одной машине, с теми же toolchains, Build Tools, power mode и без тяжёлой фоновой нагрузки. Профиль, counts, skip-флаги, CPU/environment и доступность RSS должны совпадать.

### Capture options

| Опция | Эффект |
| --- | --- |
| `--out PATH` | Обязательный output; suffix должен быть ровно `.json` с учётом регистра. |
| `--profile PROFILE` | Fixture profile: `representative` или `smoke`; default `representative`. |
| `--benchmark-count N` | Число Go benchmark samples, `1..1000`; default `3`. |
| `--runtime-iterations N` | Android runtime iterations, `1..1000000000`; default `100000`. |
| `--android-build-tools VERSION` | Явная Build Tools version; без неё берётся максимальная stable numeric. |
| `--skip-android` | Диагностический capture без Android runtime/artifacts. |
| `--skip-go-benchmarks` | Диагностический capture без Go microbenchmarks. |

### Рабочий процесс

На последней заведомо хорошей ревизии:

```bash
python3 -B scripts/performance-baseline.py capture \
  --out benchmarks/results/reference.json \
  --profile representative \
  --benchmark-count 3 \
  --runtime-iterations 100000
```

После переключения на проверяемую ревизию:

```bash
python3 -B scripts/performance-baseline.py capture \
  --out benchmarks/results/candidate.json \
  --profile representative \
  --benchmark-count 3 \
  --runtime-iterations 100000
```

Сравнение:

```bash
python3 -B scripts/performance-baseline.py check \
  --reference benchmarks/results/reference.json \
  --candidate benchmarks/results/candidate.json
```

По умолчанию применяется production-контракт `benchmarks/acceptance.json`. Он требует от обеих сторон все production surfaces: Go benchmarks, Android runtime, Android artifacts, CLI и reports, а также все обязательные inspect/compare HTML pages. Поэтому capture с `--skip-android` или `--skip-go-benchmarks` намеренно не может получить `PASS` со штатным acceptance.

Для локальной быстрой итерации можно отключить одинаковые поверхности с обеих сторон:

```bash
python3 -B scripts/performance-baseline.py capture \
  --skip-android \
  --out benchmarks/results/reference-cli.json

python3 -B scripts/performance-baseline.py capture \
  --skip-android \
  --out benchmarks/results/candidate-cli.json
```

Для focused-диагностики можно снять одинаково урезанные reference/candidate, но проверять их нужно с отдельным `--acceptance PATH`, где `required_surfaces` явно сужен. Не подменяйте им production-контракт. Reference и candidate в любом режиме обязаны иметь одинаковые profile, fixture metadata, capture config, environment и surfaces. Один и тот же файл, symlink на него или hard link нельзя выдать одновременно за reference и candidate.

Рядом с `<name>.json` создаётся `<name>-artifacts/` с fixture, stdout/stderr, локальным CLI, JSON и HTML. Директория пересоздаётся только при наличии exact ownership marker `.jankhunter-performance-artifacts-v1`; symlink или чужая непустая директория не удаляются. Public JSON пишется атомарно и только после полной валидации.

Для каждого output постоянно остаётся lock `.jankhunter-performance-<output.name>.lock`. Runner проверяет magic ownership, отклоняет symlink/чужой lock и не даёт двум capture одновременно писать один output. Lock после освобождения намеренно не удаляется.

В Git добавляйте только осознанно отобранные JSON-контракты; личные `.jhlog`, heap dump, machine paths и сырые артефакты коммитить нельзя.

### Exit codes

- `capture`: `0` — успех; `2` — ошибка аргументов, окружения, команды, пути или внутренней валидации;
- `check`: `0` и `PASS` — контракт выполнен; `1` и `FAIL` — любое нарушение, включая unreadable/invalid JSON, schema/quality/surface/environment mismatch и одинаковый inode;
- argparse-ошибка до запуска команды завершается кодом `2`.

### Unit-тесты runner

Перед изменениями performance-контракта:

```bash
python3 -B scripts/test_performance_baseline.py -v
```

Тесты используют Python standard library и temporary files. Они проверяют production acceptance, schemas, symmetric quality, surface/config/environment parity, exact metrics/pages, numeric bounds, RSS, Build Tools/SDK/JAVA_HOME/CPU, owned artifacts/lock, atomic output и artifact provenance. Это проверка логики runner, а не измерение производительности.

## Диагностика проблем

### `Android SDK path was not found`

Передайте SDK явно или экспортируйте переменную:

```bash
export ANDROID_HOME="$HOME/Library/Android/sdk" # macOS
# export ANDROID_HOME="$HOME/Android/Sdk"       # Linux
```

Для интегратора также можно использовать `--android-sdk "$ANDROID_HOME"`.

### `Android Build Tools ... was not found`

Укажите реально установленную версию или установите нужную через `sdkmanager`:

```bash
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" "build-tools;36.0.0"
```

### Gradle использует неподходящую Java

Для smoke задайте `SMOKE_JAVA_HOME`. Для интегратора и целевого проекта проверьте `JAVA_HOME` и JVM, выбранную Gradle wrapper.

### E2E не видит устройство

Проверьте `adb devices`. Устройство должно иметь состояние `device`; подтвердите USB debugging. При нескольких устройствах задайте `--serial` или `ANDROID_SERIAL`.

### Instrumentation не вернул `OK (1 test)` или `.jhlog` не найден

Откройте `reports/android-e2e/instrumentation.txt`, проверьте debug-вариант sample app и причину падения теста. Скрипт держит app и test APK установленными до копирования, затем удаляет их; при cleanup-ошибке прогон завершается с ошибкой и EXIT trap повторяет попытку. `run-as` работает только для debuggable package, а скрипт читает каталог `files/jankhunter-e2e` внутри sandbox приложения.

### Интегратор выбрал не тот модуль

Передайте `--module :нужный-модуль` явно. Для нескольких приложений повторите опцию. Build types, похожие на release, скрипт намеренно не включает.

### После повторной интеграции изменились настройки hooks

Без DSL/network/build-type аргументов скрипт должен byte-for-byte сохранить прежний managed overlay. Если была явно передана хотя бы одна такая опция, весь managed overlay намеренно пересобирается только из явно указанных значений и скрипт логирует это. В таком случае повторите полный желаемый набор явных опций. Ручные блоки без Jank Hunter markers остаются; managed overlay применяется позже для тех же полей.

### Нужно увидеть изменения без сборки и записи

```bash
./scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --dry-run \
  --verify
```

В dry-run режиме сначала выполняется тот же полный preflight. Публикация, сборка CLI, изменение файлов и Gradle verification не выполняются; вывод показывает планируемые действия.

### Performance check сообщает `environment differs` или `captured surface differs`

Переснимите обе стороны на одном хосте и с одинаковыми опциями. Не редактируйте surfaces вручную: отсутствие обязательной метрики трактуется как ошибка, а не как нулевой расход.

### Performance capture завершился ошибкой

Смотрите соответствующие `*.stderr.txt`, `*.stdout.txt` и `*.time.txt` в `<name>-artifacts/`. Runner специально не записывает успешный контракт при неполных измерениях или рассинхронизации fixture/CLI.
