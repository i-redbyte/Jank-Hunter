package report

import (
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

func bodyClass(base string, presentation bool, animated bool) string {
	classes := make([]string, 0, 3)
	classes = append(classes, strings.Fields(base)...)
	if presentation {
		classes = append(classes, "presentation-page")
	}
	if animated {
		classes = append(classes, "animated-background")
	}
	return strings.Join(classes, " ")
}

func clampPct(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func humanDuration(ms uint64) string {
	if ms == 0 {
		return "0 мс"
	}
	if ms < 1000 {
		return fmt.Sprintf("%d мс", ms)
	}
	totalSeconds := ms / 1000
	remMS := ms % 1000
	if totalSeconds < 60 {
		if remMS == 0 {
			return fmt.Sprintf("%d сек", totalSeconds)
		}
		return fmt.Sprintf("%.1f сек", float64(ms)/1000)
	}
	days := totalSeconds / 86400
	totalSeconds %= 86400
	hours := totalSeconds / 3600
	totalSeconds %= 3600
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d д", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d ч", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d мин", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d сек", seconds))
	}
	return strings.Join(parts, " ")
}

func humanMicroseconds(us uint64) string {
	if us < 1_000 {
		return fmt.Sprintf("%d мкс", us)
	}
	return fmt.Sprintf("%.2f мс", float64(us)/1_000)
}

func unixMillisTime(ms uint64) string {
	if ms == 0 || ms > math.MaxInt64 {
		return "нет данных"
	}
	return time.UnixMilli(int64(ms)).UTC().Format(time.RFC3339)
}

func formatDurationNs(ns uint64) string {
	if ns == 0 {
		return "0 мс"
	}
	if ns < 1_000_000 {
		return fmt.Sprintf("%.3f мс", float64(ns)/1_000_000)
	}
	return humanDuration(ns / 1_000_000)
}

func humanDataSizeKB(kb uint64) string {
	switch {
	case kb >= 1024*1024:
		return fmt.Sprintf("%.1f ГБ", float64(kb)/1024/1024)
	case kb >= 1024:
		return fmt.Sprintf("%.1f МБ", float64(kb)/1024)
	default:
		return fmt.Sprintf("%d КБ", kb)
	}
}

func tooltipHTML(label, body string) template.HTML {
	escapedLabel := template.HTMLEscapeString(label)
	escapedBody := template.HTMLEscapeString(body)
	return template.HTML(fmt.Sprintf(`<span class="explain" tabindex="0" data-tip="%s">%s</span>`, escapedBody, escapedLabel))
}

func inlineHTMLText(value string) template.HTML {
	return template.HTML(template.HTMLEscapeString(value))
}

func zeroNetworkBucket(bucket mathanalysis.TimelineBucket) bool {
	return bucket.HTTPCount == 0 &&
		bucket.HTTPFailed == 0 &&
		bucket.HTTPAvgDurationMS == 0 &&
		bucket.HTTPP95DurationMS == 0 &&
		bucket.DNSCount == 0 &&
		bucket.DNSDurationMS == 0 &&
		bucket.ConnectCount == 0 &&
		bucket.ConnectDurationMS == 0 &&
		bucket.TTFBMS == 0
}

func zeroUIBucket(bucket mathanalysis.TimelineBucket) bool {
	return bucket.UIFrames == 0 &&
		bucket.UIJankyFrames == 0 &&
		bucket.StallCount == 0 &&
		bucket.StallMaxMS == 0
}

func zeroMemoryBucket(bucket mathanalysis.TimelineBucket) bool {
	return bucket.MemoryPSSKB == 0 &&
		bucket.AvailableMemoryKB == 0 &&
		bucket.TrafficRxBytes == 0 &&
		bucket.TrafficTxBytes == 0
}

func significantMathFindings(findings []mathanalysis.Finding) []mathanalysis.Finding {
	out := make([]mathanalysis.Finding, 0, len(findings))
	for _, finding := range findings {
		if isSignificantSeverity(finding.Severity) {
			out = append(out, finding)
		}
	}
	return out
}

func significantReportFindings(findings []ReportFinding) []ReportFinding {
	out := make([]ReportFinding, 0, len(findings))
	for _, finding := range findings {
		if isSignificantSeverity(finding.Severity) {
			out = append(out, finding)
		}
	}
	return out
}

func significantMarkovStates(states []mathanalysis.MarkovBucketState) []mathanalysis.MarkovBucketState {
	out := make([]mathanalysis.MarkovBucketState, 0, len(states))
	for _, state := range states {
		if markovStateHasSignal(state) {
			out = append(out, state)
		}
	}
	return out
}

func hiddenMarkovStates(states []mathanalysis.MarkovBucketState) int {
	return len(states) - len(significantMarkovStates(states))
}

func significantMarkovTransitions(transitions []mathanalysis.MarkovTransition) []mathanalysis.MarkovTransition {
	out := make([]mathanalysis.MarkovTransition, 0, len(transitions))
	for _, transition := range transitions {
		if markovTransitionHasSignal(transition) {
			out = append(out, transition)
		}
	}
	return out
}

func hiddenMarkovTransitions(transitions []mathanalysis.MarkovTransition) int {
	return len(transitions) - len(significantMarkovTransitions(transitions))
}

func significantMarkovDeltas(deltas []mathanalysis.MarkovDelta) []mathanalysis.MarkovDelta {
	out := make([]mathanalysis.MarkovDelta, 0, len(deltas))
	for _, delta := range deltas {
		if isSignificantSeverity(delta.Severity) {
			out = append(out, delta)
		}
	}
	return out
}

func hiddenMarkovDeltas(deltas []mathanalysis.MarkovDelta) int {
	return len(deltas) - len(significantMarkovDeltas(deltas))
}

func isSignificantSeverity(severity string) bool {
	return severity == "high" || severity == "medium"
}

func markovStateHasSignal(state mathanalysis.MarkovBucketState) bool {
	switch state.State {
	case "Healthy":
		return false
	case "":
		return false
	default:
		return true
	}
}

func markovTransitionHasSignal(transition mathanalysis.MarkovTransition) bool {
	if transition.Count == 0 {
		return false
	}
	return transition.From != "Healthy" || transition.To != "Healthy"
}

type robustStatGroup struct {
	Title string
	Items []mathanalysis.RobustStat
}

func robustStatGroups(stats []mathanalysis.RobustStat) []robustStatGroup {
	order := []string{"Маршрут", "Экран", "Источник", "Пользовательская метрика", "Счетчик", "Память", "Контекст"}
	return groupRobustStats(stats, order)
}

func groupRobustStats(stats []mathanalysis.RobustStat, order []string) []robustStatGroup {
	byDimension := map[string][]mathanalysis.RobustStat{}
	for _, stat := range stats {
		byDimension[stat.Dimension] = append(byDimension[stat.Dimension], stat)
	}
	var groups []robustStatGroup
	seen := map[string]struct{}{}
	for _, dimension := range order {
		items := byDimension[dimension]
		if len(items) == 0 {
			continue
		}
		seen[dimension] = struct{}{}
		groups = append(groups, robustStatGroup{Title: dimension, Items: items})
	}
	var rest []string
	for dimension := range byDimension {
		if _, ok := seen[dimension]; !ok {
			rest = append(rest, dimension)
		}
	}
	sort.Strings(rest)
	for _, dimension := range rest {
		groups = append(groups, robustStatGroup{Title: dimension, Items: byDimension[dimension]})
	}
	return groups
}

type robustDeltaGroup struct {
	Title string
	Items []mathanalysis.RobustDelta
}

func robustDeltaGroups(deltas []mathanalysis.RobustDelta) []robustDeltaGroup {
	byDimension := map[string][]mathanalysis.RobustDelta{}
	for _, delta := range deltas {
		byDimension[delta.Dimension] = append(byDimension[delta.Dimension], delta)
	}
	order := []string{"Маршрут", "Экран", "Источник", "Пользовательская метрика", "Счетчик", "Память", "Контекст"}
	seen := map[string]struct{}{}
	var groups []robustDeltaGroup
	for _, dimension := range order {
		items := byDimension[dimension]
		if len(items) == 0 {
			continue
		}
		seen[dimension] = struct{}{}
		groups = append(groups, robustDeltaGroup{Title: dimension, Items: items})
	}
	var rest []string
	for dimension := range byDimension {
		if _, ok := seen[dimension]; !ok {
			rest = append(rest, dimension)
		}
	}
	sort.Strings(rest)
	for _, dimension := range rest {
		groups = append(groups, robustDeltaGroup{Title: dimension, Items: byDimension[dimension]})
	}
	return groups
}

func metricHelp(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "gc.bytes_allocated.delta"):
		return "Сколько байт было выделено за интервал или сценарий. 4092288 — примерно 4 МБ новой памяти: само по себе это не всегда плохо, но рост рядом с подтормаживаниями интерфейса, сборкой мусора или падением свободной памяти указывает на давление памяти."
	case strings.Contains(lower, "gc"):
		return "Показатель сборки мусора или выделения памяти. Смотрите не только абсолютное значение, но и совпадение с подтормаживаниями интерфейса, паузами главного потока и ростом занимаемой процессом памяти."
	case strings.Contains(lower, "queue") || strings.Contains(lower, "executor"):
		return "Очередь или исполнитель задач. Рост значения означает накопление работы; если рядом падает FPS или растут паузы главного потока, очередь может быть причиной задержек."
	case strings.Contains(lower, "network") || strings.Contains(lower, "http") || strings.Contains(lower, "retry") || strings.Contains(lower, "connect"):
		return "Пользовательский сетевой показатель. Высокие значения стоит сопоставлять с границей верхних 5% HTTP-задержек, поиском адреса, соединением, временем до первого байта и сетевыми циклами."
	case strings.Contains(lower, "jank") || strings.Contains(lower, "frame"):
		return "Метрика кадров или подтормаживаний. Чем выше значение рядом с пользовательским действием, тем выше риск видимой просадки интерфейса."
	default:
		return "Пользовательский показатель из приложения. Для счётчика важна сумма за сценарий, для изменяемого во времени показателя — его уровень. Интерпретируйте значение рядом с сетью, интерфейсом, памятью и контекстом устройства."
	}
}

func memoryMetricHelp(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "pss"):
		return "PSS — proportional set size, пропорциональный размер памяти процесса. Он учитывает долю разделяемых страниц и лучше показывает вклад приложения в потребление RAM, чем одна куча объектов."
	case strings.Contains(lower, "java"):
		return "Куча Java — память объектов JVM/ART. Рост может приводить к более частой сборке мусора и паузам, особенно если одновременно растёт выделение памяти."
	case strings.Contains(lower, "native"):
		return "Нативная куча — память нативных аллокаций. Рост может идти от bitmap, JNI, графики или библиотек и не всегда виден в куче Java."
	case strings.Contains(lower, "avail") || strings.Contains(lower, "free"):
		return "Свободная оперативная память показывает запас системы. Низкий запас усиливает давление памяти: сборку мусора, вытеснение кэшей и риск завершения процесса системой."
	default:
		return "Метрика памяти. Смотрите тренд вместе с PSS, кучей Java, нативной кучей, удержанными объектами и свободной RAM."
	}
}

func integralHelp(id string) string {
	switch id {
	case "network_failure_burn":
		return "Условная накопленная нагрузка сетевых повторов объединяет HTTP-ошибки, всплески DNS/соединений и кандидаты циклов. Это не время, трафик или расход батареи; число нужно использовать только для ранжирования одинаковых сценариев."
	case "memory_pressure_area":
		return "Площадь давления памяти объединяет рост занимаемой процессом памяти относительно базового уровня и длительность низкого запаса свободной памяти. Это интегральная оценка риска частой сборки мусора, вытеснения кэшей и завершения процесса системой."
	case "recovery_debt":
		return "Долг восстановления растет, когда плохие временные окна идут подряд. Он показывает, как долго пользователь остается в деградировавшем состоянии."
	case "latency_pain_area":
		return "Накопленная сетевая задержка выше целевого порога. Учитывает не только пик, но и длительность медленного периода."
	case "main_thread_stall_burden":
		return "Сумма превышений максимальной паузы главного потока над 100 мс в каждом интервале. Это нижняя оценка нагрузки пауз, а не их полная длительность, потому что временная шкала хранит один максимум на интервал."
	case "jank_pressure_area":
		return "Накопленная доля подтормаживающих кадров интерфейса во времени. Длинная умеренная просадка может быть важнее короткого пика."
	default:
		return "Интегральная оценка: значение сигнала умножается на длительность временного интервала и суммируется по сценарию."
	}
}

func integralCriteria(id string) string {
	switch id {
	case "jank_pressure_area":
		return "Стартовый ориентир: до 60 %*с — спокойно, 60–180 — проверить, от 180 — высокий приоритет. Порог не является универсальным целевым нормативом и зависит от длительности сценария."
	case "latency_pain_area":
		return "Стартовый ориентир: до 500 мс*с — спокойно, 500–2000 — проверить, от 2000 — высокий приоритет. Считается только часть границы верхних 5% HTTP-задержек выше 300 мс."
	case "main_thread_stall_burden":
		return "Стартовый ориентир: до 500 мс — спокойно, 500–2000 — проверить, от 2000 — высокий приоритет. Суммируется только превышение максимальной паузы над 100 мс в каждом интервале."
	case "network_failure_burn":
		return "Стартовый ориентир: до 5 усл. ед. — спокойно, 5–20 — проверить, от 20 — высокий приоритет. Значение условное и сравнимо только для одинаковых сценариев."
	case "memory_pressure_area":
		return "Стартовый ориентир: до 128 МБ*с — спокойно, 128–1024 — проверить, от 1024 — высокий приоритет. Базовый PSS — p10 замеров текущего прогона."
	case "recovery_debt":
		return "Стартовый ориентир: до 8 с² — спокойно, 8–30 — проверить, от 30 — высокий приоритет. Значение растет быстрее, когда плохие интервалы идут подряд."
	default:
		return "Чем выше значение, тем выше накопленная нагрузка. Порог для этой оценки не задан."
	}
}

func scoreHelp(kind string) string {
	switch kind {
	case "change":
		return "Оценка точки изменения показывает, насколько сильный сдвиг сигнала виден на фоне локального шума. Примерно до 3 — слабый сигнал, 3–6 — заметный, выше 6 — сильный. Для задержек, памяти и подтормаживаний больше обычно хуже."
	case "influence":
		return "Оценка влияния — приоритет расследования внутри этого прогона. Она растёт от границы верхних 5% HTTP-задержек, пауз главного потока, подтормаживаний интерфейса, памяти, избытка сообщений журнала, проблемных окон и связей сценария. Меньше 5 — низкий риск, от 5 до 15 — средний, от 15 — высокий. Статическая связь без сигнала во время выполнения не доказывает влияние на производительность."
	case "network_burn":
		return "Условная нагрузка кандидата сетевого цикла растет от числа и размера повторяющихся всплесков и уверенности детектора. Это не миллисекунды, байты или расход батареи. До 5 — слабый сигнал, 5–20 — проверить, выше 20 — высокий приоритет."
	case "confidence":
		return "Доверие лежит в диапазоне 0..1. Чем ближе к 1, тем лучше сигнал подтвержден повторяемостью, количеством наблюдений или совпадением нескольких методов."
	case "path_cost":
		return "Условная стоимость цепочки статистических связей: меньше означает более короткую цепочку с более уверенными связями. Она не является вероятностью и не доказывает направление причины."
	case "integral":
		return "Накопленная оценка суммирует площадь симптома по времени или превышение инженерного порога. Точная формула и единица указаны в строке; разные оценки нельзя складывать между собой."
	default:
		return "Оценка — относительный приоритет внутри текущего отчета. Смотрите рядом критерии, доверие, размер выборки и связанный контекст."
	}
}

func scoreGuideHTML(kind string) template.HTML {
	switch kind {
	case "code":
		return template.HTML(`<div class="score-guide"><div class="score-guide-card"><strong>Шкала реестра кода</strong><span class="score-band sev-ok">0-5: низкий риск</span><span class="score-band sev-medium">5-15: предупреждение</span><span class="score-band sev-high">15+: критично</span><p>Каждое семейство событий оценивается один раз через основной сигнал: проблемное окно, удержание, запись журнала или вызов во время выполнения. Производные источник/операция/граф представляют контекст и повторно оценку не увеличивают.</p></div></div>`)
	case "leak":
		return template.HTML(`<div class="score-guide"><div class="score-guide-card"><strong>Шкала сигналов удержания</strong><span class="score-band sev-ok">до 7: наблюдать</span><span class="score-band sev-medium">7-16: проверить</span><span class="score-band sev-high">16+: высокий приоритет</span><p>time_only получает меньший вес, after_explicit_gc — средний, confirmed_hprof/path — полный. Оценка задает порядок расследования и сама по себе не доказывает утечку.</p></div></div>`)
	case "math":
		return template.HTML(`<div class="score-guide"><div class="score-guide-card"><strong>Шкала математических оценок</strong><span class="score-band sev-ok">0-3: слабый сигнал</span><span class="score-band sev-medium">3-6: проверить</span><span class="score-band sev-high">6+: высокий приоритет</span><p>Шкалы ранжируют наблюдения внутри сопоставимых сценариев и не являются универсальным целевым нормативом. Уверенность показывает поддержку данными, а не вероятность ошибки в коде.</p></div></div>`)
	case "compare":
		return template.HTML(`<div class="score-guide"><div class="score-guide-card"><strong>Шкала сравнения</strong><span class="score-band sev-ok">зелёный: ухудшение не подтверждено</span><span class="score-band sev-medium">жёлтый: нужна проверка</span><span class="score-band sev-high">красный: сильное ухудшение</span><p>Вывод учитывает направление метрики, размер эффекта и выборку. Для пользовательской метрики направление неизвестно, поэтому изменение не считается ухудшением автоматически.</p></div></div>`)
	default:
		return template.HTML(`<div class="score-guide"><div class="score-guide-card"><strong>Как читать оценку</strong><p>Оценка - это относительный приоритет внутри текущего отчета. Смотрите рядом критерии, доверие, размер выборки и контекст.</p></div></div>`)
	}
}
