# Mathematical Analysis Ideas For Jank Hunter

Этот документ фиксирует, какой математический аппарат может быть полезен для анализа Android performance логов Jank Hunter. Главный принцип: математика должна давать инженерное действие, а не просто красивый график.

## Что уже есть в данных

`.jhlog` уже дает несколько типов сигналов:

- временные ряды: UI frame windows, FPS, jank count, memory samples, system context, custom gauges;
- point events: HTTP calls, stalls, retained objects, counters;
- категориальные состояния: screen, route, owner, process, network, SDK, device, app/build cohort;
- сравнение выборок: baseline/candidate summaries и deltas.

Это значит, что самые полезные методы будут лежать в четырех группах:

- анализ временных рядов;
- вероятностные модели состояний;
- robust statistics для шумных latency/FPS распределений;
- многомерная нормализация и сравнение когорт.

## Fourier / Spectral Analysis

Ряды Фурье или FFT полезны, когда сигнал периодический или квазипериодический. Для performance это не про единичный HTTP p95, а про ритм деградации во времени.

Практические применения:

- найти периодические jank spikes: например, GC/bitmap cache cleanup каждые N секунд;
- обнаружить регулярный polling, reconnect storm или timer-driven work;
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
- warning: `periodic jank detected around 10s period`;
- correlation с owner counters, если период совпадает с custom metric или network burst.

Ограничения:

- для коротких логов FFT почти бесполезен;
- irregular point events надо сначала bin-ить по времени;
- Fourier плохо ловит редкие single spikes; для этого лучше change point / EVT.

Вердикт: полезно как Stage 2 для длинных manual/soak прогонов, не как базовый smoke-test.

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
- periodic jank warnings.

### Stage 6: Advanced / Research

Добавить при наличии истории:

- Bayesian hierarchical compare;
- graph-based blame ranking;
- PCA/UMAP run clustering;
- queueing metrics при расширении runtime events.

## Главный вывод

Самый полезный порядок внедрения:

1. Heuristic verdict.
2. Robust statistics and confidence.
3. Change point detection.
4. Markov state transitions.
5. Spectral/Fourier analysis for long runs.
6. Graph blame ranking.

Fourier analysis действительно может пригодиться, но только для периодических или длинных временных рядов. Для ежедневного performance triage быстрее окупятся robust statistics, change points и Markov-state модель.
