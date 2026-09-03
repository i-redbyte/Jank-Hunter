package report

import (
	"math"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type memoryLeakCompareRow struct {
	Candidate       analyze.MemoryLeakSuspect
	HasBaseline     bool
	BaselineScore   float64
	BaselineCount   uint64
	BaselineAgeMS   uint64
	DeltaScore      float64
	DeltaCount      int64
	DeltaAgeMS      int64
	Status          string
	Severity        string
	Explanation     string
	MatchConfidence string
}

func memoryLeakCompareRows(comparison analyze.Comparison) []memoryLeakCompareRow {
	report := analyze.BuildLeakCompareReport(comparison)
	out := make([]memoryLeakCompareRow, 0, len(report.Deltas))
	for _, delta := range report.Deltas {
		if !delta.HasCandidate {
			continue
		}
		out = append(out, memoryLeakCompareRow{
			Candidate:       delta.Candidate,
			HasBaseline:     delta.HasBaseline,
			BaselineScore:   delta.ScoreBefore,
			BaselineCount:   delta.CountBefore,
			BaselineAgeMS:   delta.AgeBeforeMS,
			DeltaScore:      delta.DeltaScore,
			DeltaCount:      delta.DeltaCount,
			DeltaAgeMS:      delta.DeltaAgeMS,
			Status:          delta.StatusLabel,
			Severity:        delta.Severity,
			Explanation:     delta.Explanation,
			MatchConfidence: delta.MatchConfidence,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity == out[j].Severity {
			if out[i].DeltaScore == out[j].DeltaScore {
				return out[i].Candidate.Score > out[j].Candidate.Score
			}
			return out[i].DeltaScore > out[j].DeltaScore
		}
		return severityRank(out[i].Severity) > severityRank(out[j].Severity)
	})
	return out
}

type codeProblemCompareRow struct {
	Candidate     analyze.CodeProblemStats `json:"candidate"`
	HasBaseline   bool                     `json:"has_baseline"`
	Comparable    bool                     `json:"comparable"`
	BaselineScore float64                  `json:"baseline_score"`
	DeltaScore    float64                  `json:"delta_score"`
	Status        string                   `json:"status"`
	Severity      string                   `json:"severity"`
}

func codeProblemCompareRows(comparison analyze.Comparison) []codeProblemCompareRow {
	baseline := comparison.Baseline.CodeProblems
	if len(baseline) == 0 {
		baseline = analyze.BuildCodeProblemRegistry(comparison.Baseline)
	}
	candidate := comparison.Candidate.CodeProblems
	if len(candidate) == 0 {
		candidate = analyze.BuildCodeProblemRegistry(comparison.Candidate)
	}
	baselineByLocation := map[string]analyze.CodeProblemStats{}
	for _, row := range baseline {
		baselineByLocation[codeProblemLocation(row)] = row
	}
	out := make([]codeProblemCompareRow, 0, len(candidate))
	for _, row := range candidate {
		before, found := baselineByLocation[codeProblemLocation(row)]
		comparable := found && durationComparableForReport(comparison.Baseline.DurationMS, comparison.Candidate.DurationMS)
		delta := 0.0
		if comparable {
			delta = row.Score - before.Score
		}
		status := "без сильного изменения"
		severity := row.Severity
		switch {
		case !found:
			status = "новая точка для проверки"
		case !comparable:
			status = "оценка не сравнивается: длительность прогонов различается"
		case delta >= 8:
			status = "сильное усиление"
			severity = "high"
		case delta >= 3:
			status = "усиление"
			if severity == "ok" {
				severity = "medium"
			}
		case delta <= -3:
			status = "оценка снизилась"
			severity = "ok"
		}
		out = append(out, codeProblemCompareRow{
			Candidate:     row,
			HasBaseline:   found,
			Comparable:    comparable,
			BaselineScore: before.Score,
			DeltaScore:    math.Round(delta*10) / 10,
			Status:        status,
			Severity:      severity,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity == out[j].Severity {
			if out[i].DeltaScore == out[j].DeltaScore {
				return out[i].Candidate.Score > out[j].Candidate.Score
			}
			return out[i].DeltaScore > out[j].DeltaScore
		}
		return severityRank(out[i].Severity) > severityRank(out[j].Severity)
	})
	return out
}

type compareDeltaGroup struct {
	Title  string
	Detail string
	Items  []analyze.Delta
}

func compareDeltaGroups(deltas []analyze.Delta) []compareDeltaGroup {
	order := []string{"network", "ui", "memory", "context", "other"}
	titles := map[string]string{
		"network": "Сеть и трафик",
		"ui":      "Интерфейс и главный поток",
		"memory":  "Память и удержания",
		"context": "Контекст и когорты",
		"other":   "Остальные сигналы",
	}
	details := map[string]string{
		"network": "HTTP-задержки, ошибки и UID-трафик.",
		"ui":      "Плавность интерфейса, FPS и максимальные паузы главного потока.",
		"memory":  "PSS, свободная RAM и удержанные объекты.",
		"context": "Версия приложения, SDK, устройство, процесс, сеть и рут-доступ.",
		"other":   "Сигналы без отдельной категории.",
	}
	byCategory := map[string][]analyze.Delta{}
	for _, delta := range deltas {
		category := compareDeltaCategory(delta.Name)
		byCategory[category] = append(byCategory[category], delta)
	}
	var groups []compareDeltaGroup
	for _, category := range order {
		items := byCategory[category]
		if len(items) == 0 {
			continue
		}
		groups = append(groups, compareDeltaGroup{
			Title:  titles[category],
			Detail: details[category],
			Items:  items,
		})
	}
	return groups
}

func problemDeltas(deltas []analyze.Delta) []analyze.Delta {
	var out []analyze.Delta
	for _, delta := range deltas {
		if delta.Comparable && delta.Severity != "" && delta.Severity != "ok" {
			out = append(out, delta)
		}
	}
	return out
}

func compareDeltaCategory(name string) string {
	switch name {
	case "HTTP p95", "HTTP failure rate", "UID RX delta", "UID TX delta", "Network mix":
		return "network"
	case "UI jank rate", "UI avg FPS", "Main-thread stall max", "Log spam", "Problem windows":
		return "ui"
	case "Max PSS", "Min available memory", "Retained objects":
		return "memory"
	case "Process mix", "App version mix", "SDK mix", "Device mix", "Cohort mix":
		return "context"
	default:
		return "other"
	}
}

func compareDeltaLabel(name string) string {
	switch name {
	case "HTTP p95":
		return "Граница верхних 5% HTTP-задержек"
	case "HTTP failure rate":
		return "Доля HTTP-ошибок"
	case "UI jank rate":
		return "Доля подтормаживаний интерфейса"
	case "UI avg FPS":
		return "Средняя частота кадров интерфейса"
	case "Main-thread stall max":
		return "Макс. пауза главного потока"
	case "Max PSS":
		return "Макс. PSS"
	case "Min available memory":
		return "Мин. свободная память"
	case "UID RX delta":
		return "Входящий трафик приложения за прогон"
	case "UID TX delta":
		return "Исходящий трафик приложения за прогон"
	case "Retained objects":
		return "Удержанные объекты"
	case "Log spam":
		return "Спам логами"
	case "Problem windows":
		return "Проблемные окна"
	case "DB main-thread p95":
		return "Граница верхних 5% SQL на главном потоке"
	case "DB background p95":
		return "Граница верхних 5% фонового SQL"
	case "DB calls per minute":
		return "Вызовов БД в минуту"
	case "DB calls per operation":
		return "Вызовов БД на операцию приложения"
	case "DB main-thread rate":
		return "Доля вызовов БД на главном потоке"
	case "DB failure rate":
		return "Доля ошибок вызовов БД"
	case "DB rapid-repeat rate":
		return "Доля быстрых повторов SQL"
	case "DB wall per minute":
		return "Суммарное время БД в минуту"
	case "DB wall per operation":
		return "Суммарное время БД на операцию приложения"
	case "DB transactions per minute":
		return "Завершённых транзакций БД в минуту"
	case "DB transaction p95":
		return "Граница верхних 5% транзакций БД"
	case "DB main-thread transaction rate":
		return "Доля транзакций БД на главном потоке"
	case "DB transaction failure rate":
		return "Доля неуспешных транзакций БД"
	case "DB transaction rollback rate":
		return "Доля отменённых транзакций БД"
	case "DB incomplete transaction rate":
		return "Доля незавершённых транзакций БД"
	case "DB statements per transaction":
		return "Среднее число SQL-вызовов на транзакцию"
	case "DB repeated calls per operation scope":
		return "Повторных вызовов БД на операцию приложения"
	case "DB repeated calls per transaction scope":
		return "Повторных вызовов БД на транзакцию"
	case "DB batch-candidate calls per transaction scope":
		return "Кандидатов на пакетную запись БД на транзакцию"
	case "Service failure rate":
		return "Доля ошибок методов службы"
	case "Service timeout rate":
		return "Доля тайм-аутов службы"
	case "Service slow callback rate":
		return "Доля долгих методов службы"
	case "Receiver failure rate":
		return "Доля ошибок BroadcastReceiver"
	case "Receiver async deadline risk rate":
		return "Доля async Receiver у системного дедлайна"
	case "Receiver sync slow rate":
		return "Доля долгих синхронных onReceive"
	case "Binder client p95":
		return "Граница верхних 5% задержек клиента Binder"
	case "Binder slow main-thread rate":
		return "Доля медленных Binder-вызовов на главном потоке"
	case "Binder failure rate":
		return "Доля ошибок на границе Binder"
	case "Binder unhandled rate":
		return "Доля необработанных кодов транзакций Binder"
	case "Binder correlation coverage":
		return "Полнота связи клиента и сервера Binder"
	case "Hidden foreground-service share":
		return "Доля службы переднего плана при скрытом интерфейсе"
	case "Process mix":
		return "Состав процессов"
	case "App version mix":
		return "Состав версий приложения"
	case "SDK mix":
		return "Состав SDK"
	case "Device mix":
		return "Состав устройств"
	case "Network mix":
		return "Состав сети"
	case "Cohort mix":
		return "Состав когорт"
	default:
		return strings.ReplaceAll(name, "_", " ")
	}
}

func compareDeltaHelp(name string) string {
	switch name {
	case "HTTP p95":
		return "95% HTTP-запросов завершились не дольше этого значения. Рост обычно означает, что самые медленные запросы стали хуже."
	case "HTTP failure rate":
		return "Доля HTTP-вызовов с транспортной ошибкой или статусом 5xx. Сравнивается процент, а не сырое количество, поэтому разное число запросов не создает ложную регрессию. На малой выборке результат нужно подтвердить повтором."
	case "UI jank rate":
		return "Доля медленных кадров интерфейса. Рост в процентных пунктах показывает, что интерфейс стал чаще дёргаться."
	case "UI avg FPS":
		return "Средняя частота кадров. Для FPS ухудшением считается падение значения."
	case "Main-thread stall max":
		return "Самая длинная зафиксированная пауза главного потока. Даже один большой пик может быть причиной риска АНР."
	case "Max PSS":
		return "Максимальный PSS процесса. Рост показывает больший вклад приложения в потребление RAM."
	case "Min available memory":
		return "Минимум свободной RAM. Здесь ухудшением считается падение, потому что запас памяти стал меньше."
	case "UID RX delta", "UID TX delta":
		return "Рост счетчика трафика UID между первым и последним снимком каждого лога. Большее значение не считается доказательством проблемы без одинакового сценария и ожидаемого объема данных."
	case "Retained objects":
		return "Количество удержанных объектов. Рост может указывать на утечки или слишком долгие ссылки."
	case "Log spam":
		return "Частота вызовов android.util.Log.* и Timber.* в минуту. Нормализация по времени не дает более длинному прогону автоматически выглядеть хуже."
	case "Problem windows":
		return "Частота агрегированных проблемных окон в минуту. Окно объединяет близкие симптомы, но не доказывает их общую первопричину."
	case "DB main-thread p95", "DB background p95":
		return "Граница верхних 5% сравнивается отдельно для главного и фонового потоков и только при наличии не менее 20 вызовов каждого класса в обоих прогонах."
	case "DB calls per minute", "DB wall per minute":
		return "Нагрузка нормирована по длительности прогона, поэтому более длинный сценарий не выглядит хуже только из-за времени наблюдения."
	case "DB calls per operation", "DB wall per operation":
		return "Нагрузка делится на число завершённых операций приложения и сравнивается только при достаточном числе наблюдений."
	case "DB main-thread rate", "DB failure rate", "DB rapid-repeat rate":
		return "Сравнивается доля от всех DB-вызовов, а не сырое количество."
	case "DB transactions per minute":
		return "Число завершённых транзакций нормировано по длительности прогона."
	case "DB transaction p95":
		return "Граница верхних 5% рассчитывается только по завершённым транзакциям; незавершённым транзакциям не назначается искусственная длительность."
	case "DB main-thread transaction rate", "DB transaction failure rate", "DB transaction rollback rate", "DB incomplete transaction rate":
		return "Сравнивается доля от всех наблюдаемых транзакций, а не абсолютное количество."
	case "DB statements per transaction":
		return "Число SQL-вызовов делится на число завершённых транзакций. Рост означает больше вызовов внутри транзакции, но сам по себе не доказывает N+1."
	case "DB repeated calls per operation scope", "DB repeated calls per transaction scope", "DB batch-candidate calls per transaction scope":
		return "Повторы одного нормализованного SQL-шаблона делятся на число наблюдаемых операций или транзакций. Это проверяемая гипотеза, а не доказанный N+1."
	case "Service failure rate", "Service timeout rate", "Service slow callback rate":
		return "Сравнивается доля от завершённых методов службы; работа, не успевшая завершиться до конца записи, в знаменатель не подставляется."
	case "Receiver failure rate", "Receiver async deadline risk rate", "Receiver sync slow rate":
		return "Сравниваются однородные завершённые Receiver flows: async и sync не смешиваются для time-budget метрик."
	case "Binder client p95":
		return "Задержка измеряется на вызывающей стороне Binder: это полное время вызова, а не чистое процессорное время серверной стороны."
	case "Binder slow main-thread rate", "Binder failure rate", "Binder unhandled rate":
		return "Сравнивается доля типизированных событий на границе Binder, а не абсолютное число событий."
	case "Binder correlation coverage":
		return "Доля клиентских вызовов с единственным серверным кандидатом сравнима только при одинаковом полном охвате процессов; связь остаётся сопоставлением, а не точным доказательством."
	case "Hidden foreground-service share":
		return "Описывает состав сценария: служба переднего плана при скрытом окне Activity не означает активный интерфейс и сама по себе не является регрессией."
	case "Process mix", "App version mix", "SDK mix", "Device mix", "Network mix", "Cohort mix":
		return "Проверка честности сравнения: база и кандидат должны быть собраны в сопоставимых условиях."
	default:
		return "Сравнительная метрика: смотрите направление изменения, доверие и размер выборки."
	}
}

func compareDeltaValue(value string) string {
	replacer := strings.NewReplacer(
		" ms", " мс",
		" count", " шт",
		" bytes", " байт",
		" kb", " КБ",
		" fps", " FPS",
		" pp", " п.п.",
		"same", "без изменений",
		"changed", "изменилось",
		"+new", "появилось",
	)
	return replacer.Replace(value)
}

func compareDeltaChange(value string) string {
	return compareDeltaValue(value)
}

func compareDeltaInterval(value string) string {
	if value == "" {
		return "нет интервала"
	}
	replacer := strings.NewReplacer(
		"approx", "примерно",
		" ms", " мс",
		" count", " шт",
		" bytes", " байт",
		" kb", " КБ",
		" fps", " FPS",
		" pp", " п.п.",
	)
	return replacer.Replace(value)
}
