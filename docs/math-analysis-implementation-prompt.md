# Codex Prompt: Implement Math Analysis Killer Feature

Скопируй этот промт в новый Codex thread, запущенный в корне репозитория `Jank-Hunter`.

```text
Ты Codex / GPT-5.5, senior performance engineer и maintainer проекта Jank Hunter.

Цель: пошагово реализовать математический анализ для Jank Hunter и после каждого завершенного этапа делать commit и push в master.

Контекст проекта:
- Репозиторий: Jank-Hunter.
- Android runtime пишет `.jhlog`.
- CLI в `cli/` читает `.jhlog`, делает inspect/compare и генерирует standalone HTML.
- Основной отчет уже кибер-футуристичный, русская локализация включена, длинные таблицы раскрываются.
- Нужно добавить отдельную математическую HTML-страницу рядом с основным отчетом и зеленую анимированную кнопку `λ Анализ` в основном отчете.
- Все пользовательские тексты math report должны быть на русском.
- Никаких внешних CDN. Все HTML/CSS/JS inline/self-contained.
- Данные могут быть огромными, поэтому все длинные списки должны быть раскрываемыми/скроллируемыми.
- После каждого этапа:
  1. прогнать релевантные тесты;
  2. `git diff --check`;
  3. commit с понятным сообщением;
  4. `git push origin master`;
  5. коротко написать, что сделано и какой SHA запушен.

Общие инженерные правила:
- Сначала изучи `docs/performance-analysis-math.md`, `cli/internal/analyze`, `cli/internal/jhlog`, `cli/internal/report`, `cli/cmd/jankhunter/main.go`.
- Не ломай существующий `inspect`, `compare`, `export`, `sample`.
- До явной фиксации версии `.jhlog` формат можно менять без legacy-совместимости, но Android runtime и CLI должны оставаться синхронизированы, а тесты должны покрывать текущий формат. После отдельной команды "фиксируем версию" вернуться к append-only совместимости.
- Для анализа больших логов предпочитай streaming / compact aggregation.
- Для structured data не парси HTML строками; добавляй модели в Go.
- Не добавляй heavy dependencies без крайней необходимости. Для FFT сначала можно сделать простую DFT/FFT на Go stdlib или radix-2 helper внутри CLI.
- Все графики можно рисовать SVG/HTML/CSS inline.
- Русский текст отчета должен быть нормальным инженерным русским, не машинным переводом.
- Если этап слишком большой, разбей его на меньшие commits, но каждый commit должен оставлять master зеленым.

Definition of Done для всей фичи:
- `jankhunter inspect ... --out report.html` создает основной отчет и отдельный math report рядом с ним, например `report-math.html`.
- `jankhunter compare ... --out compare.html` создает `compare-math.html`.
- В основном отчете есть зеленая анимированная кнопка `λ Анализ`, ведущая на math page.
- Math page показывает:
  - обзор качества данных и sample-size warnings;
  - timeline buckets;
  - robust statistics;
  - change points;
  - autocorrelation и FFT/spectral peaks;
  - network DNS/reconnect/request loop detector;
  - Markov state model;
  - graph analysis с Dijkstra shortest explanation paths;
  - compare deltas для baseline/candidate;
  - понятные русские findings и recommendations.
- Тесты:
  - `cd cli && GOCACHE=/private/tmp/jh-go-cache go test ./...`
  - `cd android && ./gradlew :jankhunter-runtime:testDebugUnitTest :sample-app:assembleDebug --no-daemon` если менялся Android runtime/sample.
  - HTML smoke: sample inspect/compare, проверка отсутствия `ZgotmplZ`, проверка наличия `λ Анализ`, `Математический анализ`, `Сетевые циклы`.

Работай этапами ниже.

Этап 1. Math analysis models and report plumbing.
- Добавь пакет `cli/internal/mathanalysis` или аналогичный.
- Введи модели:
  - `MathReport`
  - `MathSection`
  - `Finding`
  - `TimelineBucket`
  - `Series`
  - `SpectralPeak`
  - `NetworkLoopFinding`
  - `GraphPath`
  - `CompareMathReport`
- Добавь функции:
  - `AnalyzeInspect(paths []string, options analyze.Options) (MathReport, error)`
  - `AnalyzeCompare(baselinePaths, candidatePaths []string, options analyze.Options) (CompareMathReport, error)`
- Пока можно заполнить минимальными данными из существующего summary и skeleton sections, но API должен быть готов к следующим этапам.
- Добавь `report.WriteMathInspect(path string, math MathReport)` и `report.WriteMathCompare(path string, math CompareMathReport)`.
- При `--out report.html` основной отчет должен рядом писать `report-math.html`.
- При `--out compare.html` должен писать `compare-math.html`.
- Основной HTML должен получить зеленую кнопку:
  - текст `λ Анализ`;
  - href на math page basename;
  - animated glow;
  - не ломать мобильную верстку.
- Тесты:
  - report tests проверяют наличие кнопки и отдельной страницы;
  - CLI smoke генерирует оба файла.
- Commit: `Add math analysis report plumbing`
- Push master.

Этап 2. Timeline bucket engine.
- Реализуй binned timeline из `.jhlog`:
  - default bucket: 1000ms;
  - HTTP request count;
  - HTTP failure count;
  - average/p95 HTTP duration per bucket;
  - DNS duration/count proxy from existing HTTP `dns_ms`;
  - connect duration/count proxy from existing HTTP `connect_ms`;
  - TTFB;
  - UI frames/jank;
  - memory PSS/free RAM;
  - traffic RX/TX deltas if possible.
- Для irregular point events аккуратно bin by event.TimeMS.
- Math page: секция `Таймлайн сигналов` с компактными SVG sparklines/bar charts.
- Если данных мало, явно показывать `Недостаточно данных для надежного анализа`.
- Тесты на synthetic log с несколькими buckets.
- Commit: `Add binned timeline math analysis`
- Push master.

Этап 3. Robust statistics.
- Добавь robust stats по route/screen/owner:
  - median;
  - p90/p95/p99;
  - MAD;
  - trimmed mean;
  - sample quality;
  - bootstrap confidence interval для p95, если sample size позволяет.
- Compare:
  - effect size;
  - Cliff's delta или Mann-Whitney U approximation для latency distributions;
  - confidence labels.
- Math page: секция `Робастная статистика`.
- Тесты на deterministic arrays.
- Commit: `Add robust math statistics`
- Push master.

Этап 4. Change point detection.
- Реализуй rolling median/MAD change point detection:
  - latency shifts;
  - jank shifts;
  - memory baseline shifts;
  - network failure bursts.
- Для каждого change point найти nearby events:
  - screen;
  - route;
  - owner;
  - process/network cohort.
- Math page: секция `Точки изменения`, таблица + mini chart.
- Compare: показать появившиеся/исчезнувшие change points.
- Тесты на synthetic step function.
- Commit: `Add change point detection`
- Push master.

Этап 5. Autocorrelation and FFT.
- Реализуй autocorrelation for binned series:
  - first significant lag;
  - top lags;
  - decay.
- Реализуй FFT/DFT:
  - Hann window;
  - detrend mean;
  - power spectrum;
  - top peaks;
  - spectral entropy;
  - peak-to-background ratio.
- Если series слишком короткая, показывать insufficient data.
- Сигналы:
  - UI jank rate;
  - HTTP request count;
  - HTTP failure count;
  - DNS duration/count;
  - connect duration/count;
  - route-specific request count for top routes.
- Math page: секция `Периодические сигналы`.
- Тесты:
  - synthetic sine/periodic pulse should detect expected period within tolerance.
- Commit: `Add autocorrelation and spectral analysis`
- Push master.

Этап 6. Network loop detector.
- Реализуй detector для loop scenarios:
  - repeated DNS bursts;
  - repeated connect failures;
  - repeated reconnect/websocket-like bursts;
  - route request storms;
  - owner-specific network bursts.
- Используй комбинацию:
  - rolling MAD bursts;
  - autocorrelation;
  - FFT peak confirmation;
  - repeated motif mining over canonical event tokens.
- Canonical tokens examples:
  - `route:GET /config`;
  - `dns_high`;
  - `connect_high`;
  - `http_5xx`;
  - `http_failed`;
  - `owner:ConfigRepository.refresh`.
- Finding fields:
  - route;
  - owner;
  - period;
  - confidence;
  - motif;
  - first/last timestamp;
  - loop burn score;
  - probable cause text.
- Math page: секция `Сетевые циклы`.
- Compare:
  - loop appeared/disappeared;
  - period changed;
  - burn score delta;
  - confidence delta.
- Тесты на synthetic DNS/reconnect loop.
- Commit: `Add network loop detector`
- Push master.

Этап 7. Euler-style integral scores.
- Добавь интегральные scores:
  - `jank_pressure_area`;
  - `latency_pain_area`;
  - `network_failure_burn`;
  - `memory_pressure_area`;
  - `recovery_debt`.
- Используй simple rectangular/trapezoidal integration over buckets.
- Math page: секция `Интегральная оценка боли`.
- Compare: delta по каждому score.
- Обязательно показывать формулу/объяснение на русском.
- Тесты на simple known series.
- Commit: `Add integral pain scores`
- Push master.

Этап 8. Markov state model.
- Классифицируй каждый bucket в состояния:
  - `Healthy`;
  - `NetworkLoop`;
  - `NetworkSlow`;
  - `Janky`;
  - `Stalled`;
  - `MemoryPressure`;
  - `Recovering`.
- Построй transition matrix.
- Посчитай:
  - Healthy -> bad transitions;
  - bad -> Healthy recovery probability;
  - expected recovery windows;
  - sticky states.
- Math page: секция `Марковская модель состояний`.
- Compare: baseline/candidate transition deltas.
- Тесты на known sequence.
- Commit: `Add Markov state analysis`
- Push master.

Этап 9. Causal graph with Dijkstra and Floyd-Warshall.
- Построй aggregated graph:
  - nodes: screen, owner, route, network phase, state, symptom;
  - edges: temporal/cohort/owner relationships;
  - edge weights: inverse confidence/correlation/frequency/severity.
- Реализуй:
  - Dijkstra shortest explanation path from symptom to probable owners/routes;
  - Floyd-Warshall all-pairs on aggregated graph only;
  - PageRank-like owner blame score if manageable.
- Для network loop finding показывай path:
  - `LaunchScreen -> ConfigRepository.refresh -> GET /config -> dns_burst -> reconnect_loop`.
- Math page: секция `Граф причинности`.
- Compare:
  - new/stronger edges;
  - changed shortest path;
  - owner blame score delta.
- Тесты на tiny graph with known shortest path.
- Commit: `Add causal graph analysis`
- Push master.

Этап 10. Final UX polish.
- Основной отчет:
  - зеленая кнопка `λ Анализ` должна выглядеть как killer feature, но не перекрывать existing hero/device context.
  - Кнопка должна быть в inspect и compare.
- Math page:
  - cyber-futuristic green accent;
  - all sections collapsible;
  - sticky mini-nav;
  - summary at top with severity/finding cards;
  - SVG charts readable on mobile;
  - no huge raw lists by default.
- Документация:
  - обновить `README.md`;
  - обновить `cli/README.md`;
  - обновить `docs/performance-analysis-math.md`;
  - добавить usage examples.
- Final full checks:
  - `cd cli && GOCACHE=/private/tmp/jh-go-cache go test ./...`
  - `cd android && ./gradlew :jankhunter-runtime:testDebugUnitTest :sample-app:assembleDebug --no-daemon`
  - generate sample inspect/compare and verify:
    - main report exists;
    - math report exists;
    - no `ZgotmplZ`;
    - contains `λ Анализ`, `Математический анализ`, `Сетевые циклы`, `Граф причинности`.
- Commit: `Polish math analysis report UX`
- Push master.

Important constraints:
- Never leave master broken between stages.
- If an implementation stage becomes too large, split it into smaller safe commits but keep the stage order.
- Do not skip tests before push.
- Do not silently drop features. If a method is approximated, document the approximation in the report and in docs.
- Keep generated reports useful first and beautiful second; the visual style should support diagnosis.
```
