# Нативное ядро Jank Hunter ART TI

`jankhunter-artti` — опциональный AAR с агентом ART TI. Ядро на C++20 не зависит от типов
JNI/JVMTI, поэтому его можно собрать и протестировать на хосте:

```bash
cmake -S android/jankhunter-artti/src/main/cpp \
  -B /private/tmp/jh-artti-host \
  -DJH_BUILD_HOST_TESTS=ON
cmake --build /private/tmp/jh-artti-host --parallel
ctest --test-dir /private/tmp/jh-artti-host --output-on-failure
/private/tmp/jh-artti-host/jh_artti_core_bench
```

Сборка с санитайзерами:

```bash
cmake -S android/jankhunter-artti/src/main/cpp \
  -B /private/tmp/jh-artti-asan \
  -DJH_BUILD_HOST_TESTS=ON \
  -DJH_ENABLE_SANITIZERS=ON
cmake --build /private/tmp/jh-artti-asan --parallel
ctest --test-dir /private/tmp/jh-artti-asan --output-on-failure
```

Сборка с ThreadSanitizer, если он поддерживается компилятором и средой выполнения на хосте:

```bash
cmake -S android/jankhunter-artti/src/main/cpp \
  -B /private/tmp/jh-artti-tsan \
  -DJH_BUILD_HOST_TESTS=ON \
  -DJH_ENABLE_TSAN=ON
cmake --build /private/tmp/jh-artti-tsan --parallel
ctest --test-dir /private/tmp/jh-artti-tsan --output-on-failure
```

Фаззинг нативного декодера с помощью Clang, в котором доступна для линковки среда выполнения
libFuzzer:

```bash
cmake -S android/jankhunter-artti/src/main/cpp \
  -B /private/tmp/jh-artti-fuzz \
  -DJH_BUILD_HOST_TESTS=ON \
  -DJH_ENABLE_FUZZER=ON
cmake --build /private/tmp/jh-artti-fuzz --target jh_artti_batch_decoder_fuzz --parallel
/private/tmp/jh-artti-fuzz/jh_artti_batch_decoder_fuzz -max_total_time=30
```

Если выбранная установка Clang не может слинковать libFuzzer, конфигурация завершится ошибкой с
понятным сообщением. Такое ограничение есть у текущего набора инструментов Xcode AppleClang.
Эквивалентную цель фаззинга канонической полезной нагрузки на Go по-прежнему можно запустить
командой
`go test ./internal/jhlog -run '^$' -fuzz FuzzAgentPayloadNeverPanics -fuzztime 30s`.

Бенчмарки выводят строки JSON с перцентилями задержки, пропускной способностью и количеством
потерь. Они предназначены для выявления регрессий, а не для принятия решения о выпуске на
устройствах.

## Smoke-проверки на устройстве и при публикации

При подключённом эмуляторе или устройстве, доступном для отладки:

```bash
cd android
./gradlew :sample-app:connectedDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.class=io.jankhunter.sample.ArtTiHardeningTest
./gradlew :sample-app:connectedDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.class=io.jankhunter.sample.ArtTiPerformanceSmokeTest
```

Сценарий проверки устойчивости запускает настоящую сборку мусора, интенсивное создание и
завершение потоков, конкуренцию за мониторы и принудительное получение стеков. Затем он создаёт
ограниченную перегрузку, останавливает SDK и проверяет структуру записанных событий агента v9,
включая итоговые сведения о потерях и состоянии. Тест производительности записывает только
агрегированные значения времени, загрузки CPU, PSS/нативной кучи и интервалов между кадрами; он
не экспортирует полезную нагрузку приложения.

Скрипт `scripts/gradle-plugin-smoke.sh` публикует все артефакты в изолированный локальный
Maven-репозиторий, дважды собирает внешнее минифицированное приложение-потребитель с повторным
использованием кэша конфигурации и проверяет, что опубликованный AAR ART TI добавляет библиотеки
arm64/x86_64 только в настроенный debug-вариант.

## Нативный пакетный протокол V1

- конфигурация фиксированного размера и структуры рукопожатия в формате little-endian с полями
  `structSize`, а также версиями схемы и ABI;
- 32-байтный заголовок пакета, после которого следуют записи длиной не менее 88 байт с явно
  указанным размером;
- неизвестные записи пропускаются согласно заявленной длине;
- Kotlin считывает данные в повторно используемый прямой `ByteBuffer`;
- декодер передаёт посетителю одно изменяемое представление записи, не создавая отдельный объект
  JVM для каждого нативного события;
- через JNI передаются коды состояния, но не нативные исключения и не владение объектами STL.
