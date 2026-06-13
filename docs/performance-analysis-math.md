# Mathematical Analysis Ideas For Jank Hunter

Этот документ фиксирует, какой математический аппарат может быть полезен для анализа Android performance логов Jank Hunter. Главный принцип: математика должна давать инженерное действие, а не просто красивый график.

## Что уже есть в данных

`.jhlog` уже дает несколько типов сигналов:

- временные ряды: UI frame windows, FPS, jank count, memory samples, system context, custom gauges;
- point events: HTTP calls, stalls, retained objects, counters;
- категориальные состояния: screen, route, owner, process, network, SDK, device, app/build cohort;
- сравнение выборок: baseline/candidate summaries и deltas.
- Android-side diagnostic gauges: executor queue/active count, GC/heap pressure, main dispatch latency,
  reconnect/retry counters, lifecycle timings, CPU/power/thermal/battery metrics.

Это значит, что самые полезные методы будут лежать в четырех группах:

- анализ временных рядов;
- вероятностные модели состояний;
- robust statistics для шумных latency/FPS распределений;
- спектральный и графовый анализ network loops;
- многомерная нормализация и сравнение когорт.

## Fourier / Spectral Analysis

Ряды Фурье или FFT полезны, когда сигнал периодический или квазипериодический. Для performance это не про единичный HTTP p95, а про ритм деградации во времени.

Практические применения:

- найти периодические jank spikes: например, GC/bitmap cache cleanup каждые N секунд;
- обнаружить регулярный polling, reconnect storm или timer-driven work;
- обнаружить циклические DNS resolve / reconnect bursts, когда route или owner повторяет один и тот же network motif;
- понять, совпадает ли периодическая просадка FPS с network/system sampler cadence;
- отделить случайный шум от повторяющегося паттерна.

Какие данные нужны:

- равномерная временная сетка, например FPS/jank windows по 1s или 5s;
- достаточно длинная запись: минимум десятки окон, лучше несколько минут;
- желательно не один лог, а несколько прогонов одного сценария.

Что считать:

- detrend signal: убрать медленный тренд памяти/FPS;
- windowing: Hann/Hamming window перед FFT, чтобы снизить leakage;
- power spectrum;
- top frequency peaks;
- spectral entropy: низкая entropy означает сильную периодичность;
- peak-to-background ratio: насколько пик сильнее шума.

Что можно добавить в Jank Hunter:

- секцию `Periodic Signals`;
- для UI jank/FPS: top periods вроде `every 5.0s` или `every 16.7s`;
- для network: top periods по `dns_count`, `connect_count`, `http_failures`, `route_request_count`, `reconnect_count`;
- warning: `DNS/reconnect loop detected around 2.0s period for route GET /config`;
- probable owner attribution: route/owner рядом с периодическим burst и shortest path в causal graph;
- warning: `periodic jank detected around 10s period`;
- correlation с owner counters, если период совпадает с custom metric или network burst.

Ограничения:

- для коротких логов FFT почти бесполезен;
- irregular point events надо сначала bin-ить по времени;
- Fourier плохо ловит редкие single spikes; для этого лучше change point / EVT.

Вердикт: полезно как Stage 2 для длинных manual/soak прогонов, не как базовый smoke-test.

Для network-heavy приложений это один из самых практичных spectral use cases. Irregular HTTP events надо bin-ить по времени, например 1s buckets:

```text
t=0s: dns=14 connect=12 fail=8 route=/config owner=NetworkClient
t=1s: dns=2  connect=1  fail=0
t=2s: dns=13 connect=12 fail=8 route=/config owner=NetworkClient
```

Если autocorrelation и FFT показывают один и тот же период, а motif mining видит повторяющуюся последовательность `dns -> connect_fail -> reconnect`, отчет должен показывать это как loop finding, а не просто как latency regression.

## Wavelets

Wavelet analysis лучше Fourier, когда деградация локальна: например, 20 секунд было плохо, потом снова нормально.

Практические применения:

- найти короткие интервалы с burst jank;
- увидеть, что периодичность появилась только во второй половине сценария;
- ловить transient regressions после screen transition.

Что можно добавить:

- multi-resolution heatmap по FPS/jank;
- список suspicious windows: `t=42s..58s, dominant scale=4s`;
- drill-down к событиям внутри окна.

Вердикт: потенциально очень полезно для больших логов, но сложнее объяснить пользователю. Начинать лучше с simpler rolling-window anomaly detection.

## Autocorrelation

Автокорреляция проще и объяснимее FFT.

Применения:

- проверить, повторяется ли jank через фиксированный лаг;
- найти lag между traffic burst и memory/FPS degradation;
- оценить, насколько сигнал “липкий”: если плохое состояние продолжается долго, autocorrelation будет высокой.

Метрики:

- autocorrelation at lag N;
- first significant lag;
- decay half-life.

Вердикт: хороший первый шаг перед FFT.

## Markov Processes

Марковские процессы подходят, если представить состояние приложения дискретно:

- `Healthy`;
- `NetworkSlow`;
- `Janky`;
- `Stalled`;
- `MemoryPressure`;
- `Recovering`.

Состояние можно вычислять по каждому временному окну, например 1s или 5s.

Что дает Markov model:

- transition probabilities: как часто `Healthy -> Janky`;
- expected time to recovery: сколько окон нужно, чтобы вернуться в `Healthy`;
- absorbing-risk states: например, `MemoryPressure -> Janky -> Stalled`;
- сравнение baseline/candidate как разницу transition matrix.

Пример полезного вывода:

```text
Candidate increases Healthy -> Janky transition probability from 4% to 11%.
Janky -> Healthy recovery drops from 72% to 45%.
```

Что нужно:

- binned timeline;
- правила классификации состояния окна;
- минимум несколько десятков окон.

Вердикт: очень полезно для “сценарий как последовательность состояний”. Это может стать сильной фичей compare-отчета.

## Hidden Markov Models

HMM полезен, если реальные состояния скрыты, а мы видим только noisy observations: FPS, jank, latency, memory.

Плюсы:

- может найти latent modes: `cold start`, `steady state`, `degraded network`, `memory pressure`;
- устойчивее к шуму, чем ручные thresholds.

Минусы:

- сложнее объяснить;
- требует обучения/инициализации;
- риск “магических” выводов без доверия.

Вердикт: исследовательская фича, не первая очередь. Лучше сначала явная Markov chain с понятными состояниями.

## Change Point Detection

Это один из самых практичных методов.

Задача: найти момент, где распределение метрики изменилось.

Применения:

- после конкретного экрана FPS упал;
- после серии HTTP вызовов memory baseline поднялся;
- после 40 секунд scenario latency стала хуже.

Методы:

- CUSUM;
- Bayesian online change point detection;
- rolling median/MAD break detection;
- PELT для offline анализа.

Что добавить:

- `Change Points` section;
- timestamp, affected metric, before/after median/p95;
- события рядом с change point: screen, route, owner.

Вердикт: высокая практическая ценность, проще объяснить, чем FFT.

## Robust Statistics

Performance данные шумные и heavy-tailed. Среднее часто врет.

Полезные методы:

- median, p90/p95/p99;
- MAD вместо standard deviation;
- trimmed mean;
- bootstrap confidence intervals для p95;
- Mann-Whitney U / Cliff's delta для baseline/candidate;
- Holm-Bonferroni correction, если сравниваем много метрик.

Что добавить:

- confidence bands для p95/FPS/jank;
- effect size рядом с severity;
- warning, когда sample size слишком мал.
- generic gauge distributions, чтобы новые Android метрики автоматически получали median/p95/MAD
  без ручной ветки на каждый сигнал.

Вердикт: must-have. Это полезнее почти любой “экзотики”.

## Extreme Value Theory

EVT полезна для tail latency и редких freezes.

Применения:

- оценить риск очень длинных stalls;
- моделировать tail HTTP latency выше p95/p99;
- отделить обычный шум от действительно опасного хвоста.

Ограничение: нужна большая выборка.

Вердикт: полезно для больших production/QA логов, не для маленького sample app.

## Queueing Theory

Если есть очереди работы: main thread, executor, network, DB, disk.

Применения:

- оценить saturation;
- понять, что latency растет нелинейно при росте arrival rate;
- моделировать `service time`, `wait time`, utilization.

Что нужно добавить в runtime:

- executor queue depth;
- task enqueue/dequeue timestamps;
- thread pool active count;
- disk/network queue markers, если доступны.

Вердикт: очень сильный аппарат, но требует новых runtime-событий.

## Euler-Style Integrals

Методы Эйлера в смысле численного интегрирования полезны не как “поиск причины”, а как оценка суммарной боли сценария. Performance часто плох не только из-за пика, а из-за площади под кривой деградации.

Что считать:

- `jank_pressure_area`: интеграл jank rate выше healthy threshold по timeline;
- `latency_pain_area`: интеграл route p95 или rolling median latency выше baseline/threshold;
- `network_failure_burn`: интеграл failure/reconnect/DNS rate over time;
- `memory_pressure_area`: интеграл дефицита free RAM или роста PSS;
- `recovery_debt`: площадь между degradation onset и return-to-healthy.

Текущее внедрение в CLI:

- `jank_pressure_area = Σ ((janky_frames / frames) * 100) * Δt`;
- `latency_pain_area = Σ max(0, HTTP p95 - 300ms) * Δt`;
- `network_failure_burn = Σ (HTTP_ошибки + 0.25*DNS + 0.25*connect) * Δt + Σ выгорание_цикла`;
- `memory_pressure_area = Σ max(0, PSS - min(PSS)) * Δt + Σ max(0, 256MB - свободная_RAM) * Δt`;
- `recovery_debt = Σ длительность_плохой_серии * Δt`.

Все оценки считаются прямоугольным интегрированием по timeline buckets. В compare-отчете показываются база,
кандидат, абсолютная дельта, процентная дельта, формула и инженерное объяснение на русском.

Практический вывод:

```text
Candidate p95 improved by 8%, but network_failure_burn grew 3.2x because reconnect loops lasted longer.
```

Вердикт: полезно для сравнения сценариев и для composite health score. Это объяснимее, чем один “магический” score, если отчет показывает формулу и вклад метрик.

## Affine Transformations

Аффинные преобразования сами по себе не анализируют performance, но полезны для нормализации и сравнения векторных признаков.

Применения:

- привести разные метрики к одной шкале: latency, FPS, memory, failures;
- нормализовать device cohorts: `z = (x - median_device) / MAD_device`;
- построить composite health score как weighted affine transform;
- визуализировать logs в 2D/3D после PCA/UMAP.

Где осторожно:

- нельзя “схлопывать” всё в один score без объяснимости;
- веса должны быть прозрачными.

Вердикт: полезно для report scoring и visualization, но не как самостоятельная диагностика.

## Multivariate Methods

PCA/UMAP/t-SNE:

- помогают увидеть кластеры логов;
- находят выбросы;
- полезны для больших наборов runs.

Regression / quantile regression:

- позволяет понять влияние SDK/device/network на p95;
- полезно, когда сравниваем разные cohorts.

Bayesian hierarchical models:

- хороши для сравнения baseline/candidate с учетом device/network variance;
- дают posterior probability regression.

Вердикт: перспективно для больших команд и CI history, но требует накопления истории.

## Graph Analysis

Можно построить граф:

```text
screen -> owner -> route/stall/metric -> symptom
```

Применения:

- blame ranking;
- PageRank-like score для owners;
- shortest path от symptom к probable cause;
- community detection для связанных проблем.

Вердикт: очень подходящий аппарат для Jank Hunter, потому что owner attribution уже есть.

### Dijkstra

Дейкстра хорошо ложится на causal graph. Узлы:

```text
screen, owner, route, network phase, failure class, state, symptom
```

Ребра:

```text
screen -> owner
owner -> route
route -> dns_burst
route -> reconnect
dns_burst -> http_failure
http_failure -> network_loop_symptom
```

Вес ребра должен быть “стоимостью объяснения”: временная близость, частота, severity, confidence, owner attribution и совпадение route/screen/cohort. Shortest path от symptom к owner/route дает понятное объяснение:

```text
network_loop -> route GET /config -> owner ConfigRepository.refresh -> screen Launch
```

Вердикт: must-have для “где и почему”.

### Floyd-Warshall

Floyd-Warshall полезен после агрегации графа, когда узлов уже мало. Он считает shortest paths между всеми парами и помогает найти:

- owners, которые являются мостами между разными симптомами;
- routes, которые связывают network loops и UI degradation;
- baseline/candidate изменение связности;
- скрытые пересечения проблем, например `AuthInterceptor` связан и с DNS burst, и с UI stalls.

Не запускать на сырых event-level графах: сложность `O(V^3)`. Сначала сжать события в route/owner/screen/state graph.

### Floyd Cycle Finding

Floyd tortoise/hare на сырых performance логах слишком строгий, потому что события шумные и нерегулярные. Но после канонизации sequence он может быть полезен как быстрый детектор точных циклов:

```text
dns -> connect_fail -> reconnect -> dns -> connect_fail -> reconnect
```

Для реальных логов лучше связка:

- canonical event tokens;
- n-gram/motif mining;
- autocorrelation по binned counts;
- FFT top periods;
- graph cycle detection.

Вердикт: использовать как часть `Network Loop Detector`, но не как единственный метод.

## Network Loop Detector

Это отдельный killer-feature слой для приложений с большим числом сетевых запросов.

Сигналы:

- route request count per bucket;
- failure count per bucket;
- DNS duration/count per bucket;
- connect duration/count per bucket;
- TLS/TTFB burst;
- reconnect/websocket events;
- owner labels around network call sites;
- screen/process/network cohort.

Pipeline:

1. Bin HTTP/network events в равномерные окна.
2. Построить route/owner buckets.
3. Найти bursts через rolling median/MAD.
4. Найти periodicity через autocorrelation и FFT.
5. Найти repeated motifs в canonical event sequence.
6. Построить causal graph.
7. Запустить Dijkstra от loop symptom к вероятным owners/routes.
8. В compare-режиме показать, появился ли loop, исчез ли loop, изменился ли period, burn area и confidence.

Пример вывода:

```text
High-confidence network loop:
route=GET /config, owner=ConfigRepository.refresh, period=2.1s, confidence=0.88
motif=dns -> connect_fail -> reconnect
candidate loop burn +320% vs baseline
probable path: LaunchScreen -> ConfigRepository.refresh -> GET /config -> dns_burst -> reconnect_loop
```

Вердикт: делать раньше HMM/UMAP, потому что это практично, объяснимо и напрямую отвечает на реальные production/debug боли.

Текущее внедрение в CLI:

- inspect строит раздел `Сетевые циклы` по HTTP events и compact metric events без загрузки полного event list;
- Android-side counters `network.route.*.dns.lookup.count`, `network.route.*.connect.attempt.count`,
  `network.request.retry_or_reconnect.count`, `network.route.*.retry_or_reconnect.count`,
  `websocket.*.reconnect.count` и `websocket.*.failure.count` автоматически попадают в detector;
- route/owner request storms считаются тем же pipeline: MAD burst candidates -> autocorrelation -> DFT peak -> motif mining;
- canonical tokens остаются внутренним компактным представлением, а HTML показывает русские подписи вроде
  `DNS-всплеск`, `retry/reconnect-всплеск`, `маршрут: GET /config`;
- compare показывает `появился`, `исчез`, `изменился` или `усилился`, а также период, дельту выгорания и дельту доверия.

## Практический roadmap

### Stage 1: Heuristic Verdict

Уже полезно сейчас:

- rule-based итог;
- serious/warning/healthy status;
- findings и recommendations;
- sample-size предупреждение.

### Stage 2: Robust Compare

Добавить:

- bootstrap CI для p95;
- effect size;
- Mann-Whitney/Cliff's delta для latency;
- explicit sample-size quality.

### Stage 3: Timeline Analysis

Добавить:

- binned timeline;
- rolling median/MAD anomaly detection;
- change points;
- suspicious time windows with nearby events.

### Stage 4: State Model

Добавить:

- window state classifier;
- Markov transition matrix;
- expected recovery time;
- baseline/candidate transition delta.

### Stage 5: Spectral Analysis

Добавить только для длинных прогонов:

- autocorrelation;
- FFT top periods;
- spectral entropy;
- periodic jank warnings;
- общий периодический сигнал для HTTP/DNS/connect/route counts.

### Stage 6: Network Loop Detector

Добавлено поверх spectral foundation:

- DNS/connect/retry/websocket loop detector;
- motif mining для повторяющихся route/owner sequences;
- оценка выгорания цикла и дельта сравнения;
- вероятная причина и начальный graph path для найденного цикла.

### Stage 7: Интегральные оценки боли

Добавлено поверх timeline и network loop detector:

- площадь давления jank;
- площадь сетевой задержки;
- сетевое выгорание с вкладом loop burn;
- площадь давления памяти;
- долг восстановления;
- compare delta и русские формулы в math HTML.

### Stage 8: Advanced / Research

Добавить при наличии истории:

- Bayesian hierarchical compare;
- graph-based blame ranking;
- PCA/UMAP run clustering;
- queueing metrics при расширении runtime events.

### Stage 9: Causal Graph

Добавить после timeline/spectral foundation:

- route/owner/screen/symptom graph;
- Dijkstra shortest explanation path;
- Floyd-Warshall all-pairs influence map on aggregated graph;
- PageRank-like owner blame score;
- compare graph delta: new edges, stronger edges, broken recovery paths.

### Stage 10: Math Analysis Report Page

Отдельная HTML-страница рядом с основным отчетом:

- основная кнопка в report hero: зеленая futuristic кнопка `λ Анализ`;
- inspect math page: timeline, robust stats, change points, FFT/autocorrelation, network loop detector, graph blame;
- compare math page: baseline/candidate deltas по periodicity, loop burn, transition matrix, graph paths;
- все тексты на русском, без внешних CDN;
- разделы раскрываемые, потому что данных может быть много.

## Главный вывод

Самый полезный порядок внедрения:

1. Heuristic verdict.
2. Robust statistics and confidence.
3. Change point detection.
4. Markov state transitions.
5. Spectral/Fourier analysis for long runs.
6. Graph blame ranking.

Fourier analysis действительно может пригодиться, но только для периодических или длинных временных рядов. Для ежедневного performance triage быстрее окупятся robust statistics, change points и Markov-state модель.
