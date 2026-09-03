package report

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/atomicfile"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

type LogReport struct {
	Name    string
	Anchor  string
	Summary analyze.Summary
}

type logReportGroup struct {
	Title string
	Empty string
	Logs  []LogReport
}

type ReportOptions struct {
	PresentationMode   bool
	AnimatedBackground bool
	GeneratedAt        string
	Links              ReportLinks
	// TransientOutput skips durability barriers for companion pages created inside a private
	// temporary directory and immediately consumed by WriteBundle. Standalone and final reports
	// keep the default durable atomic write path.
	TransientOutput bool
}

// ReportLinks contains only artifacts that were successfully generated. Keeping links explicit
// prevents a standalone writer from advertising files that do not exist and lets the CLI omit a
// failed optional companion without rendering the primary report twice.
type ReportLinks struct {
	Main                string
	Math                string
	Leaks               string
	Influence           string
	Diagnostics         string
	DependencyInjection string
}

type ReportPaths struct {
	Main                string
	Math                string
	Leaks               string
	Influence           string
	Diagnostics         string
	DependencyInjection string
}

func PathsFor(primary string) ReportPaths {
	return ReportPaths{
		Main:                primary,
		Math:                companionReportPath(primary, "math"),
		Leaks:               companionReportPath(primary, "leaks"),
		Influence:           companionReportPath(primary, "influence"),
		Diagnostics:         companionReportPath(primary, "diagnostics"),
		DependencyInjection: companionReportPath(primary, "di"),
	}
}

func (p ReportPaths) MainLink() ReportLinks {
	return ReportLinks{Main: filepath.Base(p.Main)}
}

func WriteInspectWithOptions(path string, summary analyze.Summary, options ReportOptions) error {
	lang := reportLanguage()
	return execute(path, cachedInspectTemplate, map[string]any{
		"GeneratedAt":                   options.generatedAt(),
		"Summary":                       summary,
		"LogGrowthJSON":                 logGrowthJSON(summary.LogGrowth),
		"Analysis":                      inspectAnalysis(summary, lang),
		"MathReportHref":                options.Links.Math,
		"LeakReportHref":                options.Links.Leaks,
		"InfluenceReportHref":           options.Links.Influence,
		"DiagnosticsReportHref":         options.Links.Diagnostics,
		"DependencyInjectionReportHref": options.Links.DependencyInjection,
		"PresentationMode":              options.PresentationMode,
		"AnimatedBackground":            options.AnimatedBackground,
	}, options.TransientOutput)
}

func logGrowthJSON(summary analyze.LogGrowthSummary) template.JS {
	raw, err := json.Marshal(summary)
	if err != nil {
		return template.JS("null")
	}
	return template.JS(raw)
}

func WriteCompareReportWithOptions(path string, comparison analyze.Comparison, baselineLogs, candidateLogs []LogReport, options ReportOptions) error {
	lang := reportLanguage()
	return execute(path, cachedCompareTemplate, map[string]any{
		"GeneratedAt": options.generatedAt(),
		"Comparison":  comparison,
		"LogGroups": []logReportGroup{
			{Title: "Логи базы", Empty: "Детали логов базы не встроены.", Logs: baselineLogs},
			{Title: "Логи кандидата", Empty: "Детали логов кандидата не встроены.", Logs: candidateLogs},
		},
		"Analysis":                      compareAnalysis(comparison, lang),
		"MathReportHref":                options.Links.Math,
		"LeakReportHref":                options.Links.Leaks,
		"InfluenceReportHref":           options.Links.Influence,
		"DiagnosticsReportHref":         options.Links.Diagnostics,
		"DependencyInjectionReportHref": options.Links.DependencyInjection,
		"PresentationMode":              options.PresentationMode,
		"AnimatedBackground":            options.AnimatedBackground,
	}, options.TransientOutput)
}

func WriteMathInspectWithOptions(path string, mathReport mathanalysis.MathReport, options ReportOptions) error {
	return execute(path, cachedMathInspectTemplate, map[string]any{
		"GeneratedAt":         options.generatedAt(),
		"Math":                mathReport,
		"MethodReferences":    mathanalysis.MethodReferences(),
		"MainReportHref":      options.Links.Main,
		"InfluenceReportHref": options.Links.Influence,
		"PresentationMode":    options.PresentationMode,
		"AnimatedBackground":  options.AnimatedBackground,
	}, options.TransientOutput)
}

func WriteMathCompareWithOptions(path string, mathReport mathanalysis.CompareMathReport, options ReportOptions) error {
	return execute(path, cachedMathCompareTemplate, map[string]any{
		"GeneratedAt":         options.generatedAt(),
		"Math":                mathReport,
		"MethodReferences":    mathanalysis.MethodReferences(),
		"MainReportHref":      options.Links.Main,
		"InfluenceReportHref": options.Links.Influence,
		"PresentationMode":    options.PresentationMode,
		"AnimatedBackground":  options.AnimatedBackground,
	}, options.TransientOutput)
}

func WriteLeakInspectWithOptions(path string, leakReport analyze.LeakReport, options ReportOptions) error {
	return execute(path, cachedLeaksInspectTemplate, map[string]any{
		"GeneratedAt":        options.generatedAt(),
		"LeakReport":         leakReport,
		"MainReportHref":     options.Links.Main,
		"PresentationMode":   options.PresentationMode,
		"AnimatedBackground": options.AnimatedBackground,
	}, options.TransientOutput)
}

func WriteLeakCompareWithOptions(path string, leakReport analyze.LeakCompareReport, options ReportOptions) error {
	return execute(path, cachedLeaksCompareTemplate, map[string]any{
		"GeneratedAt":        options.generatedAt(),
		"LeakReport":         leakReport,
		"MainReportHref":     options.Links.Main,
		"PresentationMode":   options.PresentationMode,
		"AnimatedBackground": options.AnimatedBackground,
	}, options.TransientOutput)
}

func WriteInfluenceWithOptions(path string, influence analyze.InfluenceSummary, title string, options ReportOptions) error {
	return execute(path, cachedInfluenceTemplate, map[string]any{
		"GeneratedAt":        options.generatedAt(),
		"Title":              title,
		"Influence":          influence,
		"MainReportHref":     options.Links.Main,
		"PresentationMode":   options.PresentationMode,
		"AnimatedBackground": options.AnimatedBackground,
	}, options.TransientOutput)
}

func WriteInstrumentationDiagnosticsWithOptions(
	path string,
	diagnostics analyze.InstrumentationDiagnostics,
	options ReportOptions,
) error {
	return execute(path, cachedDiagnosticsTemplate, map[string]any{
		"GeneratedAt":        options.generatedAt(),
		"Diagnostics":        diagnostics,
		"MainReportHref":     options.Links.Main,
		"PresentationMode":   options.PresentationMode,
		"AnimatedBackground": options.AnimatedBackground,
	}, options.TransientOutput)
}

func WriteDependencyInjectionWithOptions(
	path string,
	dependencyInjection analyze.DependencyInjectionReport,
	options ReportOptions,
) error {
	return execute(path, cachedDependencyInjectionTemplate, map[string]any{
		"GeneratedAt":         options.generatedAt(),
		"DependencyInjection": dependencyInjection,
		"MainReportHref":      options.Links.Main,
		"PresentationMode":    options.PresentationMode,
		"AnimatedBackground":  options.AnimatedBackground,
	}, options.TransientOutput)
}

func companionReportPath(primary, suffix string) string {
	ext := filepath.Ext(primary)
	if ext == "" {
		return primary + "-" + suffix + ".html"
	}
	return strings.TrimSuffix(primary, ext) + "-" + suffix + ext
}

func (o ReportOptions) generatedAt() string {
	value := o.GeneratedAt
	if value == "" {
		value = time.Now().Format(time.RFC3339)
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.Format("02.01.2006, 15:04:05")
	}
	return displayDateText(value)
}

type cachedReportTemplate struct {
	name   string
	source string
	once   sync.Once
	tmpl   *template.Template
	err    error
}

var sharedReportTemplateFuncs = reportTemplateFuncs()
var isoDatePattern = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

func newCachedReportTemplate(name, source string) *cachedReportTemplate {
	return &cachedReportTemplate{name: name, source: source}
}

func (c *cachedReportTemplate) parsed() (*template.Template, error) {
	c.once.Do(func() {
		funcs := make(template.FuncMap, len(sharedReportTemplateFuncs)+1)
		for name, function := range sharedReportTemplateFuncs {
			funcs[name] = function
		}
		var parsed *template.Template
		funcs["deferredRows"] = func(rowTemplate string, rows any, initial, chunkSize, columns int, label string) (template.HTML, error) {
			return renderDeferredRows(parsed, rowTemplate, rows, initial, chunkSize, columns, label)
		}
		funcs["deferredOperationRows"] = renderDeferredOperationRows
		parsed, c.err = template.New(c.name).Funcs(funcs).Parse(sharedComponentsTemplate + c.source)
		c.tmpl = parsed
	})
	if c.err != nil {
		return nil, fmt.Errorf("parse %s report template: %w", c.name, c.err)
	}
	return c.tmpl, nil
}

type deferredRow struct {
	Index int
	Value any
}

// renderDeferredRows keeps only the first page of a large table in the live DOM. Remaining pages
// are independently encoded raw-text payloads and are parsed only after explicit user interaction.
// It intentionally retains the source slice without copying its backing array while rendering.
func renderDeferredRows(
	tmpl *template.Template,
	rowTemplate string,
	rows any,
	initial int,
	chunkSize int,
	columns int,
	label string,
) (template.HTML, error) {
	value := reflect.ValueOf(rows)
	if tmpl == nil || !value.IsValid() || value.Kind() != reflect.Slice {
		return "", fmt.Errorf("deferred rows require a parsed template and a slice")
	}
	renderRange := func(target *strings.Builder, start, end int) error {
		for index := start; index < end; index++ {
			if err := tmpl.ExecuteTemplate(target, rowTemplate, deferredRow{Index: index, Value: value.Index(index).Interface()}); err != nil {
				return fmt.Errorf("render deferred row %d with %s: %w", index, rowTemplate, err)
			}
		}
		return nil
	}
	return renderDeferredRowPages(value.Len(), initial, chunkSize, columns, label, renderRange)
}

func renderDeferredRowPages(
	total int,
	initial int,
	chunkSize int,
	columns int,
	label string,
	renderRange func(*strings.Builder, int, int) error,
) (template.HTML, error) {
	if initial < 0 {
		initial = 0
	}
	if chunkSize <= 0 {
		return "", fmt.Errorf("deferred row chunk size must be positive")
	}
	if columns < 1 {
		columns = 1
	}
	if initial > total {
		initial = total
	}

	var firstPage strings.Builder
	if err := renderRange(&firstPage, 0, initial); err != nil {
		return "", err
	}
	estimatedRowBytes := 256
	if initial > 0 {
		estimatedRowBytes = max(estimatedRowBytes, (firstPage.Len()+initial-1)/initial)
	}
	var output strings.Builder
	growDeferredBuilder(&output, estimatedRowBytes, total)
	output.WriteString(firstPage.String())
	for start := initial; start < total; start += chunkSize {
		end := min(start+chunkSize, total)
		var chunk strings.Builder
		growDeferredBuilder(&chunk, estimatedRowBytes, end-start)
		if err := renderRange(&chunk, start, end); err != nil {
			return "", err
		}
		var encoded strings.Builder
		encoded.Grow(chunk.Len() + 2)
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(chunk.String()); err != nil {
			return "", fmt.Errorf("encode deferred rows %d..%d: %w", start, end, err)
		}
		payload := strings.TrimSpace(encoded.String())
		// JSON allows escaping a slash. Escaping every closing tag guarantees that an otherwise safe
		// row can never terminate the raw-text script container after future template changes.
		payload = strings.ReplaceAll(payload, "</", "<\\/")
		fmt.Fprintf(
			&output,
			`<script type="application/json" data-table-chunk data-table-chunk-size="%d">%s</script>`,
			end-start,
			payload,
		)
	}
	if total > initial {
		next := min(chunkSize, total-initial)
		fmt.Fprintf(
			&output,
			`<tr class="deferred-table-loader" data-deferred-loader data-deferred-total="%d" data-deferred-loaded="%d"><td colspan="%d"><button type="button" class="deferred-table-button" data-load-more-rows>Показать ещё %d</button><span data-deferred-remaining>Осталось строк: %d</span><span class="sr-only"> в таблице %s</span></td></tr>`,
			total,
			initial,
			columns,
			next,
			total-initial,
			template.HTMLEscapeString(label),
		)
	}
	return template.HTML(output.String()), nil
}

func growDeferredBuilder(target *strings.Builder, estimatedRowBytes, rows int) {
	const maxPreallocation = 16 * 1024 * 1024
	if estimatedRowBytes <= 0 || rows <= 0 || rows > maxPreallocation/estimatedRowBytes {
		return
	}
	target.Grow(estimatedRowBytes * rows)
}

func renderDeferredOperationRows(
	rows any,
	initial int,
	chunkSize int,
	columns int,
	label string,
) (template.HTML, error) {
	total, renderRange, err := operationRowRenderer(rows)
	if err != nil {
		return "", err
	}
	return renderDeferredRowPages(total, initial, chunkSize, columns, label, renderRange)
}

func operationRowRenderer(rows any) (int, func(*strings.Builder, int, int) error, error) {
	switch values := rows.(type) {
	case []analyze.OperationStats:
		return len(values), func(target *strings.Builder, start, end int) error {
			for index := start; index < end; index++ {
				writeOperationStatsRow(target, values[index])
			}
			return nil
		}, nil
	case []analyze.OperationTimeSlot:
		return len(values), func(target *strings.Builder, start, end int) error {
			for index := start; index < end; index++ {
				writeOperationTimeSlotRow(target, values[index])
			}
			return nil
		}, nil
	case []analyze.OperationDimensionStats:
		return len(values), func(target *strings.Builder, start, end int) error {
			for index := start; index < end; index++ {
				writeOperationDimensionRow(target, values[index])
			}
			return nil
		}, nil
	case []analyze.OperationStageStats:
		return len(values), func(target *strings.Builder, start, end int) error {
			for index := start; index < end; index++ {
				writeOperationStageRow(target, values[index])
			}
			return nil
		}, nil
	case []analyze.OperationIncident:
		return len(values), func(target *strings.Builder, start, end int) error {
			for index := start; index < end; index++ {
				writeOperationIncidentRow(target, values[index])
			}
			return nil
		}, nil
	case []analyze.OperationDelta:
		return len(values), func(target *strings.Builder, start, end int) error {
			for index := start; index < end; index++ {
				writeOperationDeltaRow(target, values[index])
			}
			return nil
		}, nil
	default:
		return 0, nil, fmt.Errorf("unsupported operation row slice %T", rows)
	}
}

func writeOperationStatsRow(target *strings.Builder, row analyze.OperationStats) {
	target.WriteString(`<tr>`)
	writeCodeCell(target, row.Operation)
	writeTextCell(target, operationKindLabel(row.Kind))
	writeCodeCell(target, reportValue(row.Screen, "любой экран"))
	writeUintCell(target, row.Count)
	writeTripleUintCell(target, row.Failures, row.Cancelled, row.Timeouts)
	writeQuantileMillisCell(target, row.P50MS, row.QuantilesApproximated)
	writeQuantileMillisCell(target, row.P90MS, row.QuantilesApproximated)
	writeQuantileMillisCell(target, row.P95MS, row.QuantilesApproximated)
	writeMillisCell(target, row.MaxMS)
	writeBudgetCell(target, row.Budgeted, row.BudgetBreaches, row.BudgetBreachRatePct, "бюджет не задан")
	writeOperationNetworkCell(target, row.CorrelatedHTTP, row.CorrelatedHTTPFailures, row.CorrelatedHTTPDurationMS)
	writePairUintCell(target, row.CorrelatedStalls, row.CorrelatedStallMaxMS, " мс")
	writeOperationIOCell(target, row.CorrelatedIO, row.CorrelatedIODurationUS, row.CorrelatedIOBytes)
	writeUintCell(target, row.CorrelatedProblems)
	writeOperationRuntimeLogCell(target, row.CorrelatedRuntimeCalls, row.CorrelatedLogRecords, row.CorrelatedRuntimeTotalMS)
	writeOperationMemoryMetricCell(target, row.CorrelatedRetainedObjects, row.MaxPSSKB, row.CorrelatedMetricEvents)
	writeOperationUICell(target, row.CorrelatedUIFrames, row.CorrelatedUIJank, row.CorrelatedUIJankRatePct)
	target.WriteString(`</tr>`)
}

func writeOperationTimeSlotRow(target *strings.Builder, row analyze.OperationTimeSlot) {
	target.WriteString(`<tr>`)
	writeTextCell(target, row.Label)
	writeCodeCell(target, row.Operation)
	writeTextCell(target, operationKindLabel(row.Kind))
	writeCodeCell(target, reportValue(row.Screen, "любой экран"))
	writeUintCell(target, row.Count)
	writeUintCell(target, row.Failures)
	writeQuantileMillisCell(target, row.P50MS, row.QuantilesApproximated)
	writeQuantileMillisCell(target, row.P90MS, row.QuantilesApproximated)
	writeQuantileMillisCell(target, row.P95MS, row.QuantilesApproximated)
	writeMillisCell(target, row.MaxMS)
	writeBudgetCell(target, row.Budgeted, row.BudgetBreaches, row.BudgetBreachRatePct, "—")
	writeOperationNetworkCell(target, row.CorrelatedHTTP, row.CorrelatedHTTPFailures, row.CorrelatedHTTPDurationMS)
	writePairUintCell(target, row.CorrelatedStalls, row.CorrelatedStallMaxMS, " мс")
	writeOperationIOCell(target, row.CorrelatedIO, row.CorrelatedIODurationUS, row.CorrelatedIOBytes)
	writeUintCell(target, row.CorrelatedProblems)
	writeOperationRuntimeLogCell(target, row.CorrelatedRuntimeCalls, row.CorrelatedLogRecords, row.CorrelatedRuntimeTotalMS)
	writeOperationMemoryMetricCell(target, row.CorrelatedRetainedObjects, row.MaxPSSKB, row.CorrelatedMetricEvents)
	writeOperationUICell(target, row.CorrelatedUIFrames, row.CorrelatedUIJank, row.CorrelatedUIJankRatePct)
	target.WriteString(`</tr>`)
}

func writeOperationDimensionRow(target *strings.Builder, row analyze.OperationDimensionStats) {
	target.WriteString(`<tr>`)
	writeCodeCell(target, row.Operation)
	writeTextCell(target, operationKindLabel(row.Kind))
	writeCodeCell(target, reportValue(row.Screen, "любой экран"))
	writeCodeCell(target, row.Key)
	writeCodeCell(target, row.Value)
	writeUintCell(target, row.Count)
	writeUintCell(target, row.Failures)
	writeQuantileMillisCell(target, row.P90MS, row.QuantilesApproximated)
	writeQuantileMillisCell(target, row.P95MS, row.QuantilesApproximated)
	writeMillisCell(target, row.MaxMS)
	writeBudgetCell(target, row.Budgeted, row.BudgetBreaches, row.BudgetBreachRatePct, "—")
	target.WriteString(`</tr>`)
}

func writeOperationStageRow(target *strings.Builder, row analyze.OperationStageStats) {
	target.WriteString(`<tr>`)
	writeCodeCell(target, row.ParentOperation)
	writeTextCell(target, operationKindLabel(row.ParentKind))
	writeCodeCell(target, reportValue(row.Screen, "любой экран"))
	writeCodeCell(target, row.Stage)
	writeUintCell(target, row.Count)
	writeQuantileMillisCell(target, row.P50MS, row.QuantilesApproximated)
	writeQuantileMillisCell(target, row.P90MS, row.QuantilesApproximated)
	writeQuantileMillisCell(target, row.P95MS, row.QuantilesApproximated)
	writeMillisCell(target, row.MaxMS)
	writeTextCell(target, humanDuration(row.TotalMS))
	target.WriteString(`<td>`)
	writeFloat2(target, row.SharePct)
	target.WriteString(`%</td></tr>`)
}

func writeOperationIncidentRow(target *strings.Builder, row analyze.OperationIncident) {
	target.WriteString(`<tr>`)
	writeTextCell(target, unixMillisTime(row.StartUnixMS))
	writeCodeCell(target, row.Operation)
	writeTextCell(target, operationKindLabel(row.Kind))
	writeCodeCell(target, reportValue(row.Screen, "любой экран"))
	writeTextCell(target, operationOutcomeLabel(row.Outcome))
	writeTextCell(target, humanDuration(row.DurationMS))
	if row.BudgetMS == 0 {
		writeTextCell(target, "не задан")
	} else {
		writePairUintCell(target, row.BudgetMS, row.BudgetExceededMS, " мс")
	}
	writeOperationNetworkCell(target, row.CorrelatedHTTP, row.HTTPFailures, row.HTTPDurationMS)
	writePairUintCell(target, row.CorrelatedStalls, row.StallMaxMS, " мс")
	writeOperationIOCell(target, row.CorrelatedIO, row.IODurationUS, row.IOBytes)
	writeUintCell(target, row.CorrelatedProblems)
	writeOperationRuntimeLogCell(target, row.CorrelatedRuntimeCalls, row.CorrelatedLogRecords, row.CorrelatedRuntimeTotalMS)
	writeOperationMemoryMetricCell(target, row.CorrelatedRetainedObjects, row.MaxPSSKB, row.CorrelatedMetricEvents)
	uiJankRate := float64(0)
	if row.CorrelatedUIFrames > 0 {
		uiJankRate = float64(row.CorrelatedUIJank) * 100 / float64(row.CorrelatedUIFrames)
	}
	writeOperationUICell(target, row.CorrelatedUIFrames, row.CorrelatedUIJank, uiJankRate)
	target.WriteString(`</tr>`)
}

func writeOperationDeltaRow(target *strings.Builder, row analyze.OperationDelta) {
	target.WriteString(`<tr>`)
	writeCodeCell(target, row.Operation)
	writeTextCell(target, operationKindLabel(row.Kind))
	writeCodeCell(target, reportValue(row.Screen, "любой экран"))
	writeUintCell(target, row.BaselineCount)
	writeUintCell(target, row.CandidateCount)
	writeOptionalQuantileMillisCell(target, row.BaselineCount, row.BaselineP95MS, row.BaselineQuantilesApproximated)
	writeOptionalQuantileMillisCell(target, row.CandidateCount, row.CandidateP95MS, row.CandidateQuantilesApproximated)
	if row.Comparable {
		target.WriteString(`<td>`)
		writeEscaped(target, signedFloat(row.P95ChangeMS, "мс"))
		target.WriteString(" · ")
		writeEscaped(target, signedFloat(row.P95ChangePct, "%"))
		target.WriteString(`</td>`)
	} else {
		writeTextCell(target, "не вычисляется")
	}
	writeOptionalBudgetPercentCell(target, row.BaselineBudgeted, row.BaselineBudgetBreachPct)
	writeOptionalBudgetPercentCell(target, row.CandidateBudgeted, row.CandidateBudgetBreachPct)
	if row.Comparable {
		writeTextCell(target, signedFloat(row.FailureRateChangePP, "п.п."))
	} else {
		writeTextCell(target, "не вычисляется")
	}
	writeOperationDatabaseDeltaCell(target, row)
	target.WriteString(`<td class="`)
	writeEscaped(target, severityCSSClass(row.Severity))
	target.WriteString(`">`)
	writeEscaped(target, operationStatusLabel(row.Status))
	target.WriteString(`</td>`)
	writeTextCell(target, confidenceLabel(row.Confidence))
	writeTextCell(target, row.Note)
	target.WriteString(`</tr>`)
}

func writeOperationDatabaseDeltaCell(target *strings.Builder, row analyze.OperationDelta) {
	if !row.DatabaseComparable {
		writeTextCell(target, "нет сопоставимых DB-событий")
		return
	}
	target.WriteString(`<td><div>выз./операцию `)
	writeFloat2(target, row.BaselineDatabaseCallsPerOperation)
	target.WriteString(` → `)
	writeFloat2(target, row.CandidateDatabaseCallsPerOperation)
	target.WriteString(`</div><div>главный поток `)
	writeFloat2(target, row.BaselineDatabaseMainRatePct)
	target.WriteString(`% → `)
	writeFloat2(target, row.CandidateDatabaseMainRatePct)
	target.WriteString(`%</div><div>ошибки `)
	writeFloat2(target, row.BaselineDatabaseFailureRatePct)
	target.WriteString(`% → `)
	writeFloat2(target, row.CandidateDatabaseFailureRatePct)
	target.WriteString(`%</div><div>полное время `)
	writeFloat2(target, row.BaselineDatabaseWallMSPerOperation)
	target.WriteString(` → `)
	writeFloat2(target, row.CandidateDatabaseWallMSPerOperation)
	target.WriteString(` мс/операцию</div></td>`)
}

func writeEscaped(target *strings.Builder, value string) {
	if !strings.ContainsAny(value, `&<'">`) {
		target.WriteString(value)
		return
	}
	target.WriteString(template.HTMLEscapeString(value))
}

func writeTextCell(target *strings.Builder, value string) {
	target.WriteString(`<td>`)
	writeEscaped(target, value)
	target.WriteString(`</td>`)
}

func writeCodeCell(target *strings.Builder, value string) {
	target.WriteString(`<td><code>`)
	writeEscaped(target, value)
	target.WriteString(`</code></td>`)
}

func writeUintCell(target *strings.Builder, value uint64) {
	target.WriteString(`<td>`)
	writeUint(target, value)
	target.WriteString(`</td>`)
}

func writeMillisCell(target *strings.Builder, value uint64) {
	target.WriteString(`<td>`)
	writeUint(target, value)
	target.WriteString(` мс</td>`)
}

func writeQuantileMillisCell(target *strings.Builder, value uint64, approximated bool) {
	target.WriteString(`<td>`)
	if approximated {
		target.WriteString(`≈`)
	}
	writeUint(target, value)
	target.WriteString(` мс</td>`)
}

func writeOptionalQuantileMillisCell(target *strings.Builder, sample, value uint64, approximated bool) {
	if sample == 0 {
		writeTextCell(target, "нет данных")
		return
	}
	writeQuantileMillisCell(target, value, approximated)
}

func writeOptionalBudgetPercentCell(target *strings.Builder, budgeted uint64, value float64) {
	if budgeted == 0 {
		writeTextCell(target, "бюджет не задан")
		return
	}
	target.WriteString(`<td>`)
	writeFloat2(target, value)
	target.WriteString(`%</td>`)
}

func writeBudgetCell(target *strings.Builder, budgeted, breaches uint64, rate float64, fallback string) {
	if budgeted == 0 {
		writeTextCell(target, fallback)
		return
	}
	target.WriteString(`<td>`)
	writeUint(target, breaches)
	target.WriteString(" из ")
	writeUint(target, budgeted)
	target.WriteString(" · ")
	writeFloat2(target, rate)
	target.WriteString(`%</td>`)
}

func writePairUintCell(target *strings.Builder, left, right uint64, suffix string) {
	target.WriteString(`<td>`)
	writeUint(target, left)
	target.WriteString(" / ")
	writeUint(target, right)
	target.WriteString(suffix)
	target.WriteString(`</td>`)
}

func writeTripleUintCell(target *strings.Builder, first, second, third uint64) {
	target.WriteString(`<td>`)
	writeUint(target, first)
	target.WriteString(" / ")
	writeUint(target, second)
	target.WriteString(" / ")
	writeUint(target, third)
	target.WriteString(`</td>`)
}

func writeOperationNetworkCell(target *strings.Builder, calls, failures, durationMS uint64) {
	target.WriteString(`<td>`)
	writeUint(target, calls)
	target.WriteString(" / ")
	writeUint(target, failures)
	target.WriteString(" / ")
	writeUint(target, durationMS)
	target.WriteString(` мс</td>`)
}

func writeOperationIOCell(target *strings.Builder, operations, durationUS, bytes uint64) {
	target.WriteString(`<td>`)
	writeUint(target, operations)
	target.WriteString(" / ")
	writeMicroseconds(target, durationUS)
	target.WriteString(" / ")
	writeDataSizeBytes(target, bytes)
	target.WriteString(`</td>`)
}

func writeOperationRuntimeLogCell(target *strings.Builder, calls, records, durationMS uint64) {
	target.WriteString(`<td>`)
	writeUint(target, calls)
	target.WriteString(" / ")
	writeUint(target, records)
	target.WriteString(" / ")
	writeUint(target, durationMS)
	target.WriteString(` мс</td>`)
}

func writeOperationMemoryMetricCell(target *strings.Builder, retained, pssKB, metrics uint64) {
	target.WriteString(`<td>`)
	writeUint(target, retained)
	target.WriteString(" / ")
	writeDataSizeKB(target, pssKB)
	target.WriteString(" / ")
	writeUint(target, metrics)
	target.WriteString(`</td>`)
}

func writeOperationUICell(target *strings.Builder, frames, jank uint64, jankRatePct float64) {
	if frames == 0 {
		writeTextCell(target, "нет кадров")
		return
	}
	target.WriteString(`<td>`)
	writeUint(target, jank)
	target.WriteString(" из ")
	writeUint(target, frames)
	target.WriteString(" · ")
	writeFloat2(target, jankRatePct)
	target.WriteString(`%</td>`)
}

func writeUint(target *strings.Builder, value uint64) {
	var buffer [20]byte
	target.Write(strconv.AppendUint(buffer[:0], value, 10))
}

func writeFloat2(target *strings.Builder, value float64) {
	var buffer [32]byte
	target.Write(strconv.AppendFloat(buffer[:0], value, 'f', 2, 64))
}

func writeFloat1(target *strings.Builder, value float64) {
	var buffer [32]byte
	target.Write(strconv.AppendFloat(buffer[:0], value, 'f', 1, 64))
}

func writeMicroseconds(target *strings.Builder, value uint64) {
	if value < 1_000 {
		writeUint(target, value)
		target.WriteString(" мкс")
		return
	}
	writeFloat2(target, float64(value)/1_000)
	target.WriteString(" мс")
}

func writeDataSizeBytes(target *strings.Builder, value uint64) {
	const (
		kib = uint64(1024)
		mib = 1024 * kib
		gib = 1024 * mib
	)
	switch {
	case value >= gib:
		writeFloat1(target, float64(value)/float64(gib))
		target.WriteString(" ГБ")
	case value >= mib:
		writeFloat1(target, float64(value)/float64(mib))
		target.WriteString(" МБ")
	case value >= kib:
		writeFloat1(target, float64(value)/float64(kib))
		target.WriteString(" КБ")
	default:
		writeUint(target, value)
		target.WriteString(" Б")
	}
}

func writeDataSizeKB(target *strings.Builder, value uint64) {
	const kibPerMib = uint64(1024)
	const kibPerGib = 1024 * kibPerMib
	switch {
	case value >= kibPerGib:
		writeFloat1(target, float64(value)/float64(kibPerGib))
		target.WriteString(" ГБ")
	case value >= kibPerMib:
		writeFloat1(target, float64(value)/float64(kibPerMib))
		target.WriteString(" МБ")
	default:
		writeUint(target, value)
		target.WriteString(" КБ")
	}
}

func execute(path string, cached *cachedReportTemplate, data any, transient bool) error {
	tmpl, err := cached.parsed()
	if err != nil {
		return err
	}
	write := func(file *os.File) error {
		// Pagination deliberately emits small script-safe fragments around every closing HTML tag.
		// Buffering keeps that safety property without turning a large report into hundreds of
		// thousands of file system calls. The buffer is flushed only after both rendering stages
		// succeed, so the durable atomic writer never publishes a partial report.
		buffered := bufio.NewWriterSize(file, 256*1024)
		reader, writer := io.Pipe()
		rendered := make(chan error, 1)
		go func() {
			renderErr := tmpl.Execute(writer, data)
			if renderErr != nil {
				renderErr = fmt.Errorf("render %s report: %w", cached.name, renderErr)
			}
			_ = writer.CloseWithError(renderErr)
			rendered <- renderErr
		}()
		paginateErr := paginateReportTables(reader, buffered)
		_ = reader.CloseWithError(paginateErr)
		renderErr := <-rendered
		if paginateErr != nil {
			return fmt.Errorf("paginate %s report tables: %w", cached.name, paginateErr)
		}
		if renderErr != nil {
			return renderErr
		}
		if err := buffered.Flush(); err != nil {
			return fmt.Errorf("flush %s report: %w", cached.name, err)
		}
		return nil
	}
	if !transient {
		return atomicfile.Write(path, 0o644, write)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create transient %s report: %w", cached.name, err)
	}
	return errors.Join(write(file), file.Close())
}
