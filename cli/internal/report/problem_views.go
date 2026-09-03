package report

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type registryStat struct {
	Name     string
	Count    int
	Score    float64
	Severity string
}

var codeProblemCategoryFilterOptions = []string{
	"Сеть",
	"UI",
	"Главный поток",
	"Память",
	"Логи",
	"Выполнение",
	"Граф влияния",
	"Риск ANR",
	"Риск OOM",
	"Давление GC",
	"Дублирование сети",
	"Утечка жизненного цикла",
	"Спам логами",
	"Ввод-вывод на главном потоке",
}

func codeProblemCategoryOptions(items []analyze.CodeProblemStats) template.HTML {
	categories := make([]string, 0, len(codeProblemCategoryFilterOptions))
	seen := map[string]struct{}{}
	for _, category := range codeProblemCategoryFilterOptions {
		if category == "" {
			continue
		}
		seen[category] = struct{}{}
		categories = append(categories, category)
	}
	var dynamic []string
	for _, item := range items {
		for _, category := range item.Categories {
			if category == "" {
				continue
			}
			if _, ok := seen[category]; ok {
				continue
			}
			seen[category] = struct{}{}
			dynamic = append(dynamic, category)
		}
	}
	sort.Strings(dynamic)
	categories = append(categories, dynamic...)

	var out strings.Builder
	for _, category := range categories {
		escaped := template.HTMLEscapeString(category)
		fmt.Fprintf(&out, `<option value="%s">%s</option>`, escaped, escaped)
	}
	return template.HTML(out.String())
}

type selectOption struct {
	Value string
	Label string
}

var leakObjectKindFilterOptions = []string{
	"экран / Activity",
	"Fragment",
	"ViewModel",
	"Service",
	"Dialog",
	"RecyclerView ViewHolder",
	"adapter",
	"Context",
	"View / binding",
	"ресурс",
	"системный объект",
	"пользовательский объект",
}

func leakObjectKindOptions() []selectOption {
	options := make([]selectOption, 0, len(leakObjectKindFilterOptions))
	for _, value := range leakObjectKindFilterOptions {
		options = append(options, selectOption{Value: value, Label: leakObjectKindLabel(value)})
	}
	return options
}

func leakObjectKindLabel(value string) string {
	switch value {
	case "экран / Activity":
		return "Экран / Activity"
	case "Fragment":
		return "Fragment"
	case "ViewModel":
		return "ViewModel"
	case "Service":
		return "Service"
	case "Dialog":
		return "Dialog"
	case "RecyclerView ViewHolder":
		return "RecyclerView ViewHolder"
	case "adapter":
		return "Adapter"
	case "Context":
		return "Context"
	case "View / binding":
		return "View / binding"
	case "ресурс":
		return "Ресурс"
	case "системный объект":
		return "Системный объект"
	case "пользовательский объект":
		return "Пользовательский объект"
	default:
		return value
	}
}

func codeProblemCategoryStats(items []analyze.CodeProblemStats) []registryStat {
	stats := map[string]*registryStat{}
	for _, item := range items {
		for _, category := range item.Categories {
			if category == "" {
				continue
			}
			stat := stats[category]
			if stat == nil {
				stat = &registryStat{Name: category, Severity: "ok"}
				stats[category] = stat
			}
			stat.Count++
			stat.Score += item.Score
			stat.Severity = maxSeverity(stat.Severity, item.Severity)
		}
	}
	return sortedRegistryStats(stats)
}

func codeProblemSeverityStats(items []analyze.CodeProblemStats) []registryStat {
	stats := map[string]*registryStat{
		"high":   {Name: "high", Severity: "high"},
		"medium": {Name: "medium", Severity: "medium"},
		"ok":     {Name: "ok", Severity: "ok"},
	}
	for _, item := range items {
		key := item.Severity
		if key == "" {
			key = "ok"
		}
		stat := stats[key]
		if stat == nil {
			stat = &registryStat{Name: key, Severity: key}
			stats[key] = stat
		}
		stat.Count++
		stat.Score += item.Score
	}
	ordered := []registryStat{}
	for _, key := range []string{"high", "medium", "ok"} {
		if stat := stats[key]; stat != nil && stat.Count > 0 {
			ordered = append(ordered, *stat)
		}
	}
	return ordered
}

func sortedRegistryStats(stats map[string]*registryStat) []registryStat {
	result := make([]registryStat, 0, len(stats))
	for _, stat := range stats {
		result = append(result, *stat)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func maxSeverity(a, b string) string {
	rank := map[string]int{"ok": 1, "medium": 2, "high": 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func ownerKindLabel(kind string) string {
	switch kind {
	case "http":
		return "HTTP"
	case "main_thread_stall":
		return "пауза главного потока"
	case "retained_object":
		return "удержанный объект"
	default:
		return strings.ReplaceAll(kind, "_", " ")
	}
}

func problemKindLabel(kind string) string {
	switch kind {
	case "http_slow_or_failed":
		return "медленный или ошибочный HTTP"
	case "main_thread_stall":
		return "пауза главного потока"
	case "ui_jank":
		return "подтормаживания UI"
	case "wrapped_runnable":
		return "долгая Runnable-задача"
	case "wrapped_callable":
		return "долгая Callable-задача"
	case "wrapped_coroutine":
		return "долгая coroutine-задача"
	case "wrapped_executor":
		return "долгая executor-задача"
	case "wrapped_click":
		return "долгий click-handler"
	case "retained_object":
		return "удержанный объект"
	case "main_thread_dispatch":
		return "медленный dispatch главного потока"
	case "log_spam":
		return "спам логами"
	default:
		return strings.ReplaceAll(kind, "_", " ")
	}
}

func codeProblemLocation(row analyze.CodeProblemStats) string {
	if row.Method == "" {
		return row.ClassName
	}
	return row.ClassName + "." + row.Method
}

// limitRows bounds presentation-only evidence in autonomous HTML reports. The complete slices
// remain in the caller-owned analysis model and all aggregate calculations; analyzer-derived
// report types also retain them in JSON output. The report keeps the highest-ranked rows because
// analyzers sort these collections before rendering. Accepting any slice keeps the policy uniform
// across report-only view types without copying the often large backing arrays.
func limitRows(rows any, limit int) any {
	value := reflect.ValueOf(rows)
	if !value.IsValid() || value.Kind() != reflect.Slice {
		return rows
	}
	if limit < 0 {
		limit = 0
	}
	if value.Len() <= limit {
		return rows
	}
	return value.Slice(0, limit).Interface()
}

func rowLimitNote(label string, total, limit int) template.HTML {
	if limit < 0 {
		limit = 0
	}
	if total <= limit {
		return ""
	}
	return template.HTML(fmt.Sprintf(
		`<p class="report-limit-note"><strong>%s:</strong> показано %d из %d наиболее значимых строк; еще %d учтены в сводных метриках и оценках. Полный машинный набор доступен в JSON-выводе команды с <code>--json</code>.</p>`,
		template.HTMLEscapeString(label),
		limit,
		total,
		total-limit,
	))
}

func russianCount(value any, singular, paucal, plural string) string {
	number := reflect.ValueOf(value)
	absolute := uint64(0)
	rendered := fmt.Sprint(value)
	switch number.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		signed := number.Int()
		if signed < 0 {
			absolute = uint64(-(signed + 1)) + 1
		} else {
			absolute = uint64(signed)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		absolute = number.Uint()
	default:
		return rendered + " " + plural
	}
	lastTwo := absolute % 100
	last := absolute % 10
	form := plural
	if lastTwo < 11 || lastTwo > 14 {
		switch {
		case last == 1:
			form = singular
		case last >= 2 && last <= 4:
			form = paucal
		}
	}
	return rendered + " " + form
}

func codeProblemDrillPath(drill analyze.CodeProblemDrillDown) string {
	location := drill.ClassName
	if drill.Method != "" {
		location += "." + drill.Method
	}
	if location == "" {
		location = "класс не определен"
	}
	context := []string{}
	if drill.Screen != "" {
		context = append(context, "экран "+drill.Screen)
	}
	if drill.Operation != "" {
		context = append(context, "операция "+drill.Operation)
	}
	if drill.Route != "" {
		context = append(context, "маршрут "+drill.Route)
	}
	if len(context) == 0 {
		return location
	}
	return location + " -> " + strings.Join(context, " -> ")
}

func codeProblemMetric(signal analyze.CodeProblemSignal) string {
	parts := []string{}
	if signal.Count > 0 {
		parts = append(parts, fmt.Sprintf("кол-во %d", signal.Count))
	}
	if signal.TotalMS > 0 {
		parts = append(parts, fmt.Sprintf("итого %s", humanDuration(signal.TotalMS)))
	}
	if signal.MaxMS > 0 {
		parts = append(parts, fmt.Sprintf("макс. %d мс", signal.MaxMS))
	}
	if signal.Value > 0 {
		unit := signal.Unit
		if unit == "" {
			unit = "значение"
		}
		parts = append(parts, fmt.Sprintf("%d %s", signal.Value, unit))
	}
	if len(parts) == 0 {
		return "сигнал"
	}
	return strings.Join(parts, " · ")
}

func codeProblemEvidenceKey(problem analyze.CodeProblemStats) string {
	return fmt.Sprintf("%d:%s%s", len(problem.ClassName), problem.ClassName, problem.Method)
}

// codeProblemEvidenceArchive stores the complete registry model once instead of repeating deeply
// nested evidence markup in every autonomous report row. The gzip/base64 stream is inert until a
// user searches the complete registry or opens a row; the visible table remains paginated.
type codeProblemEvidencePayload struct {
	Mode     string                     `json:"mode"`
	Problems []analyze.CodeProblemStats `json:"problems,omitempty"`
	Rows     []codeProblemCompareRow    `json:"rows,omitempty"`
}

func encodeCodeProblemEvidenceArchive(payload codeProblemEvidencePayload) template.JS {
	var output strings.Builder
	encoded := base64.NewEncoder(base64.RawURLEncoding, &output)
	compressed, err := gzip.NewWriterLevel(encoded, gzip.BestSpeed)
	if err != nil {
		_ = encoded.Close()
		return ""
	}
	encodeErr := writeCodeProblemEvidencePayload(compressed, payload)
	compressErr := compressed.Close()
	base64Err := encoded.Close()
	if encodeErr != nil || compressErr != nil || base64Err != nil {
		return ""
	}
	return template.JS(output.String())
}

// writeCodeProblemEvidencePayload encodes the large registry one record at a time. json.Encoder
// otherwise marshals the complete top-level slice into an intermediate buffer before the first
// gzip write; a representative registry is tens of megabytes before compression and used to
// dominate report-generation peak RSS. Marshaling one record at a time preserves the compact wire
// representation while keeping the largest transient allocation bounded by one evidence record.
func writeCodeProblemEvidencePayload(target io.Writer, payload codeProblemEvidencePayload) error {
	mode, err := json.Marshal(payload.Mode)
	if err != nil {
		return fmt.Errorf("encode code problem evidence mode: %w", err)
	}
	if err := writeAll(target, []byte(`{"mode":`)); err != nil {
		return err
	}
	if err := writeAll(target, mode); err != nil {
		return err
	}
	if len(payload.Problems) > 0 {
		if err := writeAll(target, []byte(`,"problems":[`)); err != nil {
			return err
		}
		for index := range payload.Problems {
			if index > 0 {
				if err := writeAll(target, []byte{','}); err != nil {
					return err
				}
			}
			row, err := json.Marshal(&payload.Problems[index])
			if err != nil {
				return fmt.Errorf("encode code problem evidence row %d: %w", index, err)
			}
			if err := writeAll(target, row); err != nil {
				return err
			}
		}
		if err := writeAll(target, []byte{']'}); err != nil {
			return err
		}
	}
	if len(payload.Rows) > 0 {
		if err := writeAll(target, []byte(`,"rows":[`)); err != nil {
			return err
		}
		for index := range payload.Rows {
			if index > 0 {
				if err := writeAll(target, []byte{','}); err != nil {
					return err
				}
			}
			row, err := json.Marshal(&payload.Rows[index])
			if err != nil {
				return fmt.Errorf("encode code problem comparison row %d: %w", index, err)
			}
			if err := writeAll(target, row); err != nil {
				return err
			}
		}
		if err := writeAll(target, []byte{']'}); err != nil {
			return err
		}
	}
	return writeAll(target, []byte("}\n"))
}

func codeProblemEvidenceArchive(problems []analyze.CodeProblemStats) template.JS {
	return encodeCodeProblemEvidenceArchive(codeProblemEvidencePayload{Mode: "inspect", Problems: problems})
}

func codeProblemCompareArchive(comparison analyze.Comparison) template.JS {
	rows := codeProblemCompareRows(comparison)
	return encodeCodeProblemEvidenceArchive(codeProblemEvidencePayload{Mode: "compare", Rows: rows})
}

func severityLabel(value string) string {
	switch value {
	case "critical":
		return "критично"
	case "high":
		return "высокий риск"
	case "medium":
		return "средний риск"
	case "low":
		return "низкий риск"
	case "info":
		return "информация"
	case "ok":
		return "норма"
	default:
		return "норма"
	}
}

func severityCSSClass(value string) string {
	switch value {
	case "critical":
		return "sev-critical"
	case "high":
		return "sev-high"
	case "medium":
		return "sev-medium"
	case "low":
		return "sev-low"
	default:
		return "sev-ok"
	}
}

func problemPriorityBandLabel(value string) string {
	switch value {
	case "critical":
		return "критичный приоритет"
	case "high":
		return "высокий приоритет"
	case "medium":
		return "средний приоритет"
	case "low":
		return "низкий приоритет"
	default:
		return "информационный приоритет"
	}
}

func problemCategoryLabel(value string) string {
	switch value {
	case analyze.ProblemCategoryStability:
		return "Стабильность"
	case analyze.ProblemCategoryOperations:
		return "Операции приложения"
	case analyze.ProblemCategoryUI:
		return "Интерфейс и главный поток"
	case analyze.ProblemCategoryNetwork:
		return "Сеть"
	case analyze.ProblemCategoryMemory:
		return "Память и сборка мусора"
	case analyze.ProblemCategoryIO:
		return "Файлы и база данных"
	case analyze.ProblemCategoryCPU:
		return "Процессор и фоновые задачи"
	case analyze.ProblemCategoryPower:
		return "Энергия и нагрев"
	case analyze.ProblemCategoryLogs:
		return "Логи"
	case analyze.ProblemCategoryAndroidComponents:
		return "Компоненты Android и IPC"
	case analyze.ProblemCategoryDependencyInjection:
		return "DI"
	default:
		return value
	}
}

func dependencyInjectionTermLabel(value string) string {
	switch value {
	case "consumer":
		return "потребитель"
	case "dependency":
		return "зависимость"
	case "module":
		return "модуль"
	case "component":
		return "компонент"
	case "entry_point":
		return "точка входа"
	case "factory":
		return "фабрика"
	case "constructor":
		return "конструктор"
	case "field":
		return "поле"
	case "method":
		return "метод"
	case "generated_factory":
		return "созданная фабрика"
	case "declared":
		return "объявлено в коде"
	case "generated_confirmed":
		return "подтверждено созданным кодом"
	default:
		return strings.ReplaceAll(value, "_", " ")
	}
}

func problemCoverageStatusLabel(value string) string {
	switch value {
	case "healthy":
		return "проблем не найдено"
	case "problems_found":
		return "есть проблемы"
	case "not_measured":
		return "данные не собирались"
	case "insufficient_data":
		return "нужно больше данных"
	case "collection_degraded":
		return "часть данных потеряна"
	default:
		return value
	}
}

func evidenceQualityStatusLabel(value string) string {
	switch value {
	case analyze.EvidenceQualityComplete:
		return "данных достаточно"
	case analyze.EvidenceQualityDegraded:
		return "есть ограничения сбора"
	case analyze.EvidenceQualityInsufficient:
		return "данных недостаточно"
	case analyze.EvidenceQualityNotMeasured:
		return "не проверялось"
	case analyze.EvidenceQualityNotCalibrated:
		return "нет эталонной проверки"
	default:
		return value
	}
}

func problemStatusLabel(value string) string {
	switch value {
	case "new":
		return "новая"
	case "regressed":
		return "ухудшилась"
	case "persistent":
		return "сохранилась"
	case "improved":
		return "улучшилась"
	case "resolved":
		return "исправлена"
	default:
		return "зафиксирована"
	}
}

func problemClaimLabel(value string) string {
	switch value {
	case "linked":
		return "связь подтверждена данными"
	case "correlated":
		return "события совпали по контексту"
	case "hypothesis":
		return "вероятная причина"
	default:
		return "причину нужно проверить"
	}
}

func problemEvidenceLabel(value analyze.ProblemEvidence) string {
	lower := strings.ToLower(value.Name)
	if strings.Contains(lower, "p95") {
		if value.Sample != nil && *value.Sample == 1 {
			return "Длительность вызова"
		}
		if value.Sample != nil && *value.Sample < 20 {
			return "Задержка верхней части выборки"
		}
		return "Задержка верхних 5%"
	}
	replacer := strings.NewReplacer(
		"Jank rate", "Доля медленных кадров",
		"Frame deadline", "Целевое время кадра",
		"Retained size", "Оценка удержанной памяти",
		"Low-memory samples", "Сигналы нехватки памяти",
		"Max PSS", "Максимальный PSS",
	)
	return replacer.Replace(value.Name)
}

func problemEvidenceHelp(value analyze.ProblemEvidence) string {
	lower := strings.ToLower(value.Name)
	if strings.Contains(lower, "p95") {
		if value.Sample != nil && *value.Sample == 1 {
			return "Длительность единственного записанного вызова. Процентиль для одного наблюдения не используется, потому что он вводит в заблуждение."
		}
		if value.Sample != nil && *value.Sample < 20 {
			return "Оценка задержки среди самых медленных вызовов. Выборка небольшая, поэтому значение нужно подтвердить повторным прогоном."
		}
		return "95-й процентиль: 95% вызовов завершились не дольше этого времени, а оставшиеся 5% были медленнее."
	}
	if strings.Contains(lower, "jank rate") {
		return "Доля кадров, которые не уложились в целевое время отображения и могли выглядеть как рывок интерфейса."
	}
	return "Измеренное значение, на котором основана карточка проблемы. Сравните его с ориентиром ниже."
}

func problemEvidenceUnit(value string) string {
	replacer := strings.NewReplacer(
		"requests/s", "запросов/с",
		"events/s", "операций/с",
		"calls/boundary", "вызовов/границу",
		"calls", "вызовов",
		"requests", "запросов",
		"attempts", "попыток",
		"events", "событий",
		"samples", "замеров",
		"bytes", "байт",
		"ms", "мс",
		"KB", "КБ",
	)
	return replacer.Replace(value)
}

func problemEvidenceDisplay(value analyze.ProblemEvidence) string {
	observed := strings.TrimSpace(value.Observed)
	if count, err := strconv.ParseUint(observed, 10, 64); err == nil {
		switch value.Unit {
		case "requests":
			return russianCount(count, "запрос", "запроса", "запросов")
		case "attempts":
			return russianCount(count, "попытка", "попытки", "попыток")
		case "events":
			return russianCount(count, "событие", "события", "событий")
		case "calls":
			return russianCount(count, "вызов", "вызова", "вызовов")
		case "samples":
			return russianCount(count, "замер", "замера", "замеров")
		}
	}
	if value.Unit == "" {
		return observed
	}
	return observed + " " + problemEvidenceUnit(value.Unit)
}

func problemEvidenceThreshold(value string) string {
	localized := problemEvidenceUnit(value)
	if strings.HasPrefix(localized, "< ") {
		return "обычно меньше " + strings.TrimPrefix(localized, "< ")
	}
	return "ориентир " + localized
}

func problemPriorityComponentLabel(value string) string {
	switch value {
	case "impact":
		return "Влияние на пользователя"
	case "magnitude":
		return "Величина отклонения"
	case "exposure":
		return "Повторяемость / охват запусков"
	case "breadth":
		return "Широта локализации"
	case "compounding":
		return "Сочетание сигналов"
	default:
		return value
	}
}

func problemLocationText(values []analyze.ProblemLocation) string {
	type location struct {
		fields map[string]string
		text   string
	}

	candidates := make([]location, 0, len(values))
	for _, value := range values {
		parts := make([]string, 0, 8)
		fields := make(map[string]string, 8)
		for _, item := range []struct {
			name  string
			label string
			value string
		}{
			{name: "screen", label: "экран", value: value.Screen},
			{name: "operation", label: "операция", value: value.Operation},
			{name: "route", label: "маршрут", value: value.Route},
			{name: "owner", label: "источник", value: value.Owner},
			{name: "class", label: "класс", value: value.Class},
			{name: "method", label: "метод", value: value.Method},
			{name: "process", label: "процесс", value: value.Process},
		} {
			item.value = strings.TrimSpace(item.value)
			if !isUnknownReportValue(item.value) {
				fields[item.name] = item.value
				parts = append(parts, item.label+" "+item.value)
			}
		}
		if len(parts) > 0 {
			candidates = append(candidates, location{fields: fields, text: strings.Join(parts, " · ")})
		}
	}
	locations := make([]string, 0, len(candidates))
	for index, candidate := range candidates {
		contained := false
		for otherIndex, other := range candidates {
			if index == otherIndex || len(candidate.fields) >= len(other.fields) {
				continue
			}
			if problemLocationContainedBy(candidate.fields, other.fields) {
				contained = true
				break
			}
		}
		if !contained {
			locations = append(locations, candidate.text)
		}
	}
	locations = uniqueProblemStrings(locations)
	if len(locations) == 0 {
		return "точное место не определено"
	}
	return strings.Join(locations, "; ")
}

func problemLocationContainedBy(candidate, other map[string]string) bool {
	for name, value := range candidate {
		otherValue, ok := other[name]
		if !ok || !strings.EqualFold(value, otherValue) {
			return false
		}
	}
	return true
}

func uniqueProblemStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func codeSeverityLabel(value string) string {
	if value == "ok" {
		return "низкий риск"
	}
	return severityLabel(value)
}

func confidenceLabel(value string) string {
	switch value {
	case "high":
		return "высокое"
	case "medium":
		return "среднее"
	case "low":
		return "низкое"
	default:
		return "неизвестно"
	}
}

func diagnosticCompletenessLevelLabel(value string) string {
	switch value {
	case "excellent":
		return "максимальная"
	case "high":
		return "высокая"
	case "sufficient":
		return "достаточная"
	case "limited":
		return "ограниченная"
	case "low":
		return "низкая"
	default:
		return "неизвестно"
	}
}

func processScopeLabel(value string) string {
	switch value {
	case "all_processes":
		return "все процессы"
	case "main_process_only":
		return "только основной процесс"
	case "process_allowlist":
		return "разрешённый список процессов"
	case "mixed":
		return "смешанный"
	default:
		return "неизвестный"
	}
}

func frameSourceLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "mixed":
		return "смешанный"
	case "choreographer":
		return "Choreographer"
	case "jankstats":
		return "JankStats"
	case "", "unknown":
		return "не записан"
	default:
		return value
	}
}
