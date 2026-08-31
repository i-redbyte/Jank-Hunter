package report

import (
	"bufio"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/atomicfile"
	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
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

func MathReportPath(path string) string {
	return canonicalCompanionReportPath(path, "math")
}

func LeakReportPath(path string) string {
	return canonicalCompanionReportPath(path, "leaks")
}

func InfluenceReportPath(path string) string {
	return canonicalCompanionReportPath(path, "influence")
}

func DiagnosticsReportPath(path string) string {
	return canonicalCompanionReportPath(path, "diagnostics")
}

func DependencyInjectionReportPath(path string) string {
	return canonicalCompanionReportPath(path, "di")
}

func companionReportPath(primary, suffix string) string {
	ext := filepath.Ext(primary)
	if ext == "" {
		return primary + "-" + suffix + ".html"
	}
	return strings.TrimSuffix(primary, ext) + "-" + suffix + ext
}

func canonicalCompanionReportPath(path, suffix string) string {
	ext := filepath.Ext(path)
	base := path
	if ext == "" {
		ext = ".html"
	} else {
		base = strings.TrimSuffix(path, ext)
	}
	for {
		trimmed := false
		for _, known := range []string{"math", "leaks", "influence", "diagnostics", "di"} {
			knownSuffix := "-" + known
			if strings.HasSuffix(base, knownSuffix) {
				base = strings.TrimSuffix(base, knownSuffix)
				trimmed = true
				break
			}
		}
		if !trimmed {
			break
		}
	}
	return base + "-" + suffix + ext
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
	if strings.IndexAny(value, `&<'">`) < 0 {
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

func writeOptionalMillisCell(target *strings.Builder, sample, value uint64) {
	if sample == 0 {
		writeTextCell(target, "нет данных")
		return
	}
	writeMillisCell(target, value)
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

func reportTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"reportCSS": func(includeMath bool) template.CSS {
			return template.CSS(reportStylesheet(includeMath))
		},
		"reportJS": func() template.JS {
			return template.JS(reportJS)
		},
		"problemSearchJS": func() template.JS {
			return template.JS(problemSearchJS)
		},
		"logGrowthCSS": func() template.CSS {
			return template.CSS(logGrowthCSS)
		},
		"logGrowthJS": func() template.JS {
			return template.JS(logGrowthJS)
		},
		"pctWidth": func(value float64) template.CSS {
			return template.CSS(fmt.Sprintf("width:%.2f%%", clampPct(value)))
		},
		"msWidth": func(value uint64) template.CSS {
			width := float64(value) * 100 / 2000
			if width > 100 {
				width = 100
			}
			if width < 1 && value > 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"deltaWidth": func(value float64) template.CSS {
			width := math.Abs(value)
			if width > 100 {
				width = 100
			}
			if width < 1 && value != 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"scoreWidth": func(value float64) template.CSS {
			width := value * 12.5
			if width > 100 {
				width = 100
			}
			if width < 1 && value > 0 {
				width = 1
			}
			return template.CSS(fmt.Sprintf("width:%.2f%%", width))
		},
		"ringStyle": func(value float64) template.CSS {
			value = clampPct(value)
			capStyle := "round"
			if value < 1 {
				capStyle = "butt"
			}
			return template.CSS(fmt.Sprintf("--value:%.2f;--cap:%s", value, capStyle))
		},
		"rate": func(part int, total int) float64 {
			if total <= 0 {
				return 0
			}
			return float64(part) * 100 / float64(total)
		},
		"sub": func(left, right int) int {
			return max(0, left-right)
		},
		"fpsScore": func(value float64) float64 {
			return clampPct(value * 100 / 60)
		},
		"severityClass": severityCSSClass,
		"statusLabel": func(value string) string {
			switch value {
			case "high":
				return "критично"
			case "medium":
				return "предупреждение"
			case "ok":
				return "готово"
			case "pending":
				return "ожидает данных"
			default:
				return "каркас"
			}
		},
		"sparkline": func(series mathanalysis.Series) template.HTML {
			return sparklineSVG(series)
		},
		"seriesMax": func(series mathanalysis.Series) float64 {
			return seriesMax(series)
		},
		"seriesLast": func(series mathanalysis.Series) float64 {
			return seriesLast(series)
		},
		"bucketRange": func(bucket mathanalysis.TimelineBucket) string {
			return fmt.Sprintf("%.1f-%.1fs", float64(bucket.StartMS)/1000, float64(bucket.EndMS)/1000)
		},
		"humanDuration":                   humanDuration,
		"humanMicros":                     humanMicroseconds,
		"unixMillisTime":                  unixMillisTime,
		"formatDurationNs":                formatDurationNs,
		"dataSize":                        humanDataSizeKB,
		"ruCount":                         russianCount,
		"tip":                             tooltipHTML,
		"metricHelp":                      metricHelp,
		"memoryHelp":                      memoryMetricHelp,
		"integralHelp":                    integralHelp,
		"scoreHelp":                       scoreHelp,
		"scoreGuide":                      scoreGuideHTML,
		"integralCriteria":                integralCriteria,
		"ownerKind":                       ownerKindLabel,
		"frameSourceLabel":                frameSourceLabel,
		"httpPhaseLabel":                  httpPhaseLabel,
		"webSocketFailureLabel":           webSocketFailureLabel,
		"problemKind":                     problemKindLabel,
		"codeProblemLocation":             codeProblemLocation,
		"codeProblemDrillPath":            codeProblemDrillPath,
		"codeProblemMetric":               codeProblemMetric,
		"codeProblemCategoryOptions":      codeProblemCategoryOptions,
		"codeProblemCategories":           codeProblemCategoryStats,
		"codeProblemSeverities":           codeProblemSeverityStats,
		"codeProblemEvidenceKey":          codeProblemEvidenceKey,
		"codeProblemEvidenceArchive":      codeProblemEvidenceArchive,
		"codeProblemCompareArchive":       codeProblemCompareArchive,
		"limitRows":                       limitRows,
		"rowLimitNote":                    rowLimitNote,
		"leakObjectKindOptions":           leakObjectKindOptions,
		"leakObjectKindLabel":             leakObjectKindLabel,
		"leakGraphSVG":                    leakGraphSVG,
		"leakModeLabel":                   leakModeLabel,
		"leakDeltaStatusClass":            leakDeltaStatusClass,
		"codeProblemCompareRows":          codeProblemCompareRows,
		"memoryLeakCompareRows":           memoryLeakCompareRows,
		"deltaGroups":                     compareDeltaGroups,
		"deltaLabel":                      compareDeltaLabel,
		"deltaHelp":                       compareDeltaHelp,
		"deltaValue":                      compareDeltaValue,
		"deltaChange":                     compareDeltaChange,
		"deltaInterval":                   compareDeltaInterval,
		"problemDeltas":                   problemDeltas,
		"databaseProblemRows":             databaseProblemRows,
		"databaseObservationRows":         databaseObservationRows,
		"databaseTransactionRows":         databaseTransactionRows,
		"databaseScenarioRows":            databaseScenarioRows,
		"databaseScenarioScopeLabel":      databaseScenarioScopeLabel,
		"databasePlanKindLabel":           databasePlanKindLabel,
		"databaseProblemTransactionRows":  databaseProblemTransactionRows,
		"databaseObservedTransactionRows": databaseObservedTransactionRows,
		"databaseTaxonomyLabel":           databaseTaxonomyLabel,
		"severityLabel":                   severityLabel,
		"codeSeverityLabel":               codeSeverityLabel,
		"confidenceLabel": func(value string) string {
			return confidenceLabel(value)
		},
		"problemCategoryLabel":          problemCategoryLabel,
		"problemCoverageStatusLabel":    problemCoverageStatusLabel,
		"evidenceQualityStatusLabel":    evidenceQualityStatusLabel,
		"problemStatusLabel":            problemStatusLabel,
		"problemClaimLabel":             problemClaimLabel,
		"problemLocationText":           problemLocationText,
		"problemEvidenceLabel":          problemEvidenceLabel,
		"problemEvidenceHelp":           problemEvidenceHelp,
		"problemEvidenceDisplay":        problemEvidenceDisplay,
		"problemEvidenceUnit":           problemEvidenceUnit,
		"problemEvidenceThreshold":      problemEvidenceThreshold,
		"problemPriorityComponentLabel": problemPriorityComponentLabel,
		"problemPriorityBandLabel":      problemPriorityBandLabel,
		"diTermLabel":                   dependencyInjectionTermLabel,
		"problemPrimaryRecommendation": func(value analyze.ProblemFinding) *analyze.ProblemRecommendation {
			if len(value.Recommendations) == 0 {
				return nil
			}
			return &value.Recommendations[0]
		},
		"problemDeltaFinding": func(value analyze.ProblemDelta) analyze.ProblemFinding {
			if value.Candidate != nil {
				return *value.Candidate
			}
			if value.Baseline != nil {
				return *value.Baseline
			}
			return analyze.ProblemFinding{}
		},
		"problemDiagnosisData":              problemDiagnosisData,
		"problemDeltaDiagnosisData":         problemDeltaDiagnosisData,
		"problemOrientedCollectionWarnings": problemOrientedCollectionWarnings,
		"priorityWidth": func(value int) template.CSS {
			return template.CSS(fmt.Sprintf("width:%d%%", min(100, max(0, value))))
		},
		"diagnosticCompletenessLevelLabel": diagnosticCompletenessLevelLabel,
		"processScopeLabel": func(value string) string {
			return processScopeLabel(value)
		},
		"influenceRoleLabel":         influenceRoleLabel,
		"influenceGraphData":         influenceGraphData,
		"influenceEvidenceLabel":     influenceEvidenceLabel,
		"routeCompareRows":           routeCompareRows,
		"screenCompareRows":          screenCompareRows,
		"ownerCompareRows":           ownerCompareRows,
		"signalContextCompareRows":   signalContextCompareRows,
		"operationKindLabel":         operationKindLabel,
		"operationOutcomeLabel":      operationOutcomeLabel,
		"operationStatusLabel":       operationStatusLabel,
		"databaseCompareStatusLabel": databaseCompareStatusLabel,
		"uiScreenInsights":           uiScreenInsights,
		"uiProblemCount":             uiProblemCount,
		"hasComposeWork":             hasComposeWork,
		"hasRoomWork":                hasRoomWork,
		"composeWorkReport":          composeWorkReport,
		"roomWorkRows":               roomWorkRows,
		"databaseStatementRows":      databaseStatementRows,
		"databaseCoverage":           databaseCoverage,
		"workerRows":                 workerRows,
		"criticalIORows":             criticalIORows,
		"asyncKindLabel":             asyncKindLabel,
		"hundredths":                 hundredths,
		"humanBytesPerSecond":        humanBytesPerSecond,
		"ordinaryRuntimeCalls":       ordinaryRuntimeCalls,
		"operationContextInsights":   operationContextInsights,
		"customMetricInsights":       customMetricInsights,
		"primaryCategoryCoverage":    primaryCategoryCoverage,
		"findingCategoryCoverage":    findingCategoryCoverage,
		"problemCards": func(summary analyze.Summary) []analyze.ProblemFinding {
			if len(summary.ProblemIncidents) > 0 {
				return summary.ProblemIncidents
			}
			return summary.Problems
		},
		"hiddenCoverageSummary":  hiddenCoverageSummary,
		"collectionWindowNotice": collectionWindowNotice,
		"collectorCapabilities":  collectorCapabilities,
		"summaryLogSpam":         summaryLogSpamTotal,
		"summaryProblemWindows":  summaryProblemWindowTotal,
		"perMinute":              perMinute,
		"signedMS":               signedMS,
		"signedDuration":         signedDuration,
		"signedFloat":            signedFloat,
		"networkBucketClass": func(bucket mathanalysis.TimelineBucket) string {
			if zeroNetworkBucket(bucket) {
				return "bucket-zero"
			}
			return ""
		},
		"uiBucketClass": func(bucket mathanalysis.TimelineBucket) string {
			if zeroUIBucket(bucket) {
				return "bucket-zero"
			}
			return ""
		},
		"memoryBucketClass": func(bucket mathanalysis.TimelineBucket) string {
			if zeroMemoryBucket(bucket) {
				return "bucket-zero"
			}
			return ""
		},
		"robustGroups":                 robustStatGroups,
		"robustDeltaGroups":            robustDeltaGroups,
		"causalEdges":                  uniqueCausalEdges,
		"causalPaths":                  uniqueCausalPaths,
		"causalGraphSVG":               causalGraphSVG,
		"influenceStatus":              influenceStatusLabel,
		"influenceSeverity":            influenceSeverityLabel,
		"topInfluenceNodes":            topInfluenceNodes,
		"mathHeuristic":                inspectMathHeuristic,
		"compareMathHeuristic":         compareMathHeuristic,
		"significantMathFindings":      significantMathFindings,
		"significantReportFindings":    significantReportFindings,
		"significantMarkovStates":      significantMarkovStates,
		"hiddenMarkovStates":           hiddenMarkovStates,
		"significantMarkovTransitions": significantMarkovTransitions,
		"hiddenMarkovTransitions":      hiddenMarkovTransitions,
		"significantMarkovDeltas":      significantMarkovDeltas,
		"hiddenMarkovDeltas":           hiddenMarkovDeltas,
		"join": func(values []string, separator string) string {
			return strings.Join(values, separator)
		},
		"seconds": func(ms uint64) float64 {
			return float64(ms) / 1000
		},
		"jankPct": func(jankyFrames uint64, frames uint64) float64 {
			if frames == 0 {
				return 0
			}
			return float64(jankyFrames) * 100 / float64(frames)
		},
		"motifText": func(tokens []string) string {
			return mathanalysis.NetworkLoopMotifText(tokens)
		},
		"pathText": func(path mathanalysis.GraphPath) string {
			if len(path.Nodes) == 0 {
				return ""
			}
			return strings.Join(path.Nodes, " -> ")
		},
		"markovState": func(state string) string {
			return mathanalysis.MarkovStateLabel(state)
		},
		"markovConfidence": func(confidence string) string {
			return mathanalysis.MarkovConfidenceLabel(confidence)
		},
		"causalKind": func(kind string) string {
			return mathanalysis.CausalKindLabel(kind)
		},
		"percent01": func(value float64) float64 {
			return value * 100
		},
		"fallback": func(value string, fallback string) string {
			if isUnknownReportValue(value) {
				return fallback
			}
			return value
		},
		"contextHint":             contextValueHint,
		"databaseSourceQueryHint": databaseSourceQueryHint,
		"growthFreshnessReason":   localizedGrowthFreshnessReason,
		"reportValue":             reportValue,
		"reportHint":              reportValueHint,
		"cohortHint":              cohortValueHint,
		"signalContextHint":       signalContextLabelHint,
		"bodyClass":               bodyClass,
	}
}

func httpPhaseLabel(value string) string {
	switch value {
	case "queue":
		return "Очередь"
	case "dns":
		return "Поиск адреса (DNS)"
	case "connect":
		return "Соединение"
	case "tls":
		return "Защищённое соединение (TLS)"
	case "request":
		return "Отправка"
	case "ttfb":
		return "Ожидание первого байта (TTFB)"
	case "response":
		return "Получение"
	default:
		return value
	}
}

func webSocketFailureLabel(value string) string {
	switch value {
	case "timeout":
		return "тайм-аут"
	case "connection":
		return "соединение"
	case "tls":
		return "TLS"
	case "protocol":
		return "протокол"
	case "io":
		return "ввод-вывод"
	case "other":
		return "другая"
	default:
		return value
	}
}

func operationKindLabel(value string) string {
	switch value {
	case "user":
		return "действие пользователя"
	case "screen":
		return "открытие экрана"
	case "background":
		return "фоновая работа"
	case "system":
		return "системная работа"
	case "stage":
		return "этап"
	default:
		return reportValue(value, "тип не указан")
	}
}

func operationOutcomeLabel(value string) string {
	switch value {
	case "success":
		return "успешно"
	case "failure":
		return "ошибка"
	case "cancelled":
		return "отменено"
	case "timeout":
		return "превышено время ожидания"
	default:
		return reportValue(value, "результат не указан")
	}
}

func operationStatusLabel(value string) string {
	switch value {
	case "regressed":
		return "ухудшение"
	case "improved":
		return "улучшение"
	case "stable":
		return "без заметного изменения"
	case "new":
		return "есть только в кандидате"
	case "removed":
		return "есть только в базе"
	case "insufficient_data":
		return "недостаточно данных"
	default:
		return reportValue(value, "статус не определён")
	}
}

func databaseCompareStatusLabel(value string) string {
	switch value {
	case "compared":
		return "сопоставлено"
	case "new":
		return "только в кандидате"
	case "removed":
		return "только в базе"
	case "insufficient_data":
		return "недостаточно данных"
	case "not_comparable":
		return "наборы несопоставимы"
	default:
		return reportValue(value, "статус не определён")
	}
}

type influenceHTMLData struct {
	Views          []influenceHTMLView
	ViewNodes      []influenceHTMLNode
	ViewEdges      []influenceHTMLEdge
	Workspace      influenceHTMLWorkspace
	HotPaths       []analyze.InfluencePath
	MethodHotspots []analyze.InfluenceMethod
}

type influenceHTMLView struct {
	ID              string
	Mode            string
	Title           string
	Explanation     string
	Filters         analyze.InfluenceGraphFilters
	NodeIDs         []string
	EdgeIDs         []string
	TotalNodes      int
	TotalEdges      int
	ShownNodes      int
	ShownEdges      int
	OmittedNodes    int
	OmittedEdges    int
	OmissionReasons []string
	Limits          analyze.InfluenceGraphLimits
	Legend          []analyze.InfluenceGraphLegend
}

type influenceHTMLWorkspace struct {
	Nodes           []influenceHTMLNode
	Edges           []influenceHTMLEdge
	TotalNodes      int
	TotalEdges      int
	ShownNodes      int
	ShownEdges      int
	OmittedNodes    int
	OmittedEdges    int
	Contexts        []analyze.InfluenceGraphContext
	TotalContexts   int
	ShownContexts   int
	OmissionReasons []string
}

type influenceHTMLNode struct {
	ViewKey              string   `json:"_Key,omitempty"`
	ClassName            string   `json:",omitempty"`
	Label                string   `json:",omitempty"`
	Score                float64  `json:",omitempty"`
	Severity             string   `json:",omitempty"`
	Status               string   `json:",omitempty"`
	RuntimeEvidence      bool     `json:",omitempty"`
	Problems             uint64   `json:",omitempty"`
	LogSpam              uint64   `json:",omitempty"`
	MainThreadMS         uint64   `json:",omitempty"`
	RuntimeWallMS        uint64   `json:",omitempty"`
	NetworkMS            uint64   `json:",omitempty"`
	MemoryPressure       uint64   `json:",omitempty"`
	UIJank               uint64   `json:",omitempty"`
	Retained             uint64   `json:",omitempty"`
	HeapEvidence         bool     `json:",omitempty"`
	Operations           []string `json:",omitempty"`
	Screens              []string `json:",omitempty"`
	Routes               []string `json:",omitempty"`
	Reasons              []string `json:",omitempty"`
	ID                   string   `json:",omitempty"`
	Kind                 string   `json:",omitempty"`
	Package              string   `json:",omitempty"`
	Breadcrumbs          []string `json:",omitempty"`
	Aggregate            bool     `json:",omitempty"`
	Connector            bool     `json:",omitempty"`
	ChildCount           int      `json:",omitempty"`
	RuntimeClassCount    int      `json:",omitempty"`
	StaticOnlyClassCount int      `json:",omitempty"`
	ProblemClassCount    int      `json:",omitempty"`
	Children             []string `json:",omitempty"`
	Explanation          string   `json:",omitempty"`
}

type influenceHTMLEdge struct {
	ViewKey      string  `json:"_Key,omitempty"`
	From         string  `json:",omitempty"`
	To           string  `json:",omitempty"`
	RuntimeCount uint64  `json:",omitempty"`
	StaticCount  uint64  `json:",omitempty"`
	Influence    float64 `json:",omitempty"`
	Evidence     string  `json:",omitempty"`
	Aggregate    bool    `json:",omitempty"`
}

func influenceGraphData(influence analyze.InfluenceSummary) template.JS {
	payload, err := json.Marshal(buildInfluenceHTMLData(influence))
	if err != nil {
		return template.JS(`{}`)
	}
	return template.JS(payload)
}

func buildInfluenceHTMLData(influence analyze.InfluenceSummary) influenceHTMLData {
	views := make([]influenceHTMLView, 0, len(influence.Views))
	viewNodes := make([]influenceHTMLNode, 0)
	viewEdges := make([]influenceHTMLEdge, 0)
	seenNodes := map[string]string{}
	seenEdges := map[string]string{}
	for _, view := range influence.Views {
		nodeIDs := make([]string, 0, len(view.Nodes))
		for _, node := range view.Nodes {
			key := node.ID
			if node.Connector {
				key = "connector:" + key
			}
			if existing, exists := seenNodes[key]; exists {
				nodeIDs = append(nodeIDs, existing)
				continue
			}
			payloadKey := fmt.Sprintf("n%d", len(viewNodes))
			seenNodes[key] = payloadKey
			nodeIDs = append(nodeIDs, payloadKey)
			compact := compactInfluenceHTMLNode(node, node.Aggregate)
			compact.ViewKey = payloadKey
			viewNodes = append(viewNodes, compact)
		}
		edgeIDs := make([]string, 0, len(view.Edges))
		for _, edge := range view.Edges {
			key := fmt.Sprintf("%s:%t:%d:%d", edge.ID, edge.Aggregate, edge.RuntimeCount, edge.StaticCount)
			if existing, exists := seenEdges[key]; exists {
				edgeIDs = append(edgeIDs, existing)
				continue
			}
			payloadKey := fmt.Sprintf("e%d", len(viewEdges))
			seenEdges[key] = payloadKey
			edgeIDs = append(edgeIDs, payloadKey)
			compact := compactInfluenceHTMLEdge(edge)
			compact.ViewKey = payloadKey
			viewEdges = append(viewEdges, compact)
		}
		views = append(views, influenceHTMLView{
			ID:              view.ID,
			Mode:            view.Mode,
			Title:           view.Title,
			Explanation:     view.Explanation,
			Filters:         view.Filters,
			NodeIDs:         nodeIDs,
			EdgeIDs:         edgeIDs,
			TotalNodes:      view.TotalNodes,
			TotalEdges:      view.TotalEdges,
			ShownNodes:      view.ShownNodes,
			ShownEdges:      view.ShownEdges,
			OmittedNodes:    view.OmittedNodes,
			OmittedEdges:    view.OmittedEdges,
			OmissionReasons: view.OmissionReasons,
			Limits:          view.Limits,
			Legend:          view.Legend,
		})
	}
	workspaceNodes := make([]influenceHTMLNode, 0, len(influence.Workspace.Nodes))
	for _, node := range influence.Workspace.Nodes {
		workspaceNodes = append(workspaceNodes, compactInfluenceHTMLNode(node, true))
	}
	workspace := influence.Workspace
	return influenceHTMLData{
		Views:     views,
		ViewNodes: viewNodes,
		ViewEdges: viewEdges,
		Workspace: influenceHTMLWorkspace{
			Nodes:           workspaceNodes,
			Edges:           influenceHTMLEdges(workspace.Edges),
			TotalNodes:      workspace.TotalNodes,
			TotalEdges:      workspace.TotalEdges,
			ShownNodes:      workspace.ShownNodes,
			ShownEdges:      workspace.ShownEdges,
			OmittedNodes:    workspace.OmittedNodes,
			OmittedEdges:    workspace.OmittedEdges,
			Contexts:        workspace.Contexts,
			TotalContexts:   workspace.TotalContexts,
			ShownContexts:   workspace.ShownContexts,
			OmissionReasons: workspace.OmissionReasons,
		},
		HotPaths:       influence.HotPaths,
		MethodHotspots: influence.MethodHotspots,
	}
}

func compactInfluenceHTMLNode(node analyze.InfluenceGraphNode, detailed bool) influenceHTMLNode {
	result := influenceHTMLNode{
		Score:           node.Score,
		Severity:        node.Severity,
		RuntimeEvidence: node.RuntimeEvidence,
		HeapEvidence:    node.HeapEvidence,
		ID:              node.ID,
		Aggregate:       node.Aggregate,
		Connector:       node.Connector,
	}
	if len(node.Reasons) > 0 {
		limit := len(node.Reasons)
		if !detailed && limit > 2 {
			limit = 2
		}
		result.Reasons = node.Reasons[:limit]
	}
	if node.Aggregate || detailed {
		result.Package = node.Package
	}
	if node.Connector {
		result.Kind = node.Kind
	}
	if !detailed {
		return result
	}
	result.Problems = node.Problems
	result.LogSpam = node.LogSpam
	result.MainThreadMS = node.MainThreadMS
	result.RuntimeWallMS = node.RuntimeWallMS
	result.NetworkMS = node.NetworkMS
	result.MemoryPressure = node.MemoryPressure
	result.UIJank = node.UIJank
	result.Retained = node.Retained
	result.Operations = node.Operations
	result.Screens = node.Screens
	result.Routes = node.Routes
	result.ChildCount = node.ChildCount
	result.RuntimeClassCount = node.RuntimeClassCount
	result.StaticOnlyClassCount = node.StaticOnlyClassCount
	result.ProblemClassCount = node.ProblemClassCount
	return result
}

func influenceHTMLEdges(edges []analyze.InfluenceGraphEdge) []influenceHTMLEdge {
	result := make([]influenceHTMLEdge, 0, len(edges))
	for _, edge := range edges {
		result = append(result, compactInfluenceHTMLEdge(edge))
	}
	return result
}

func compactInfluenceHTMLEdge(edge analyze.InfluenceGraphEdge) influenceHTMLEdge {
	return influenceHTMLEdge{
		From:         edge.From,
		To:           edge.To,
		RuntimeCount: edge.RuntimeCount,
		StaticCount:  edge.StaticCount,
		Influence:    edge.Influence,
		Evidence:     edge.Evidence,
		Aggregate:    edge.Aggregate,
	}
}

func bodyClass(base string, presentation bool, animated bool) string {
	classes := make([]string, 0, 3)
	for _, className := range strings.Fields(base) {
		classes = append(classes, className)
	}
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

type routeCompareRow struct {
	Route             string
	BaselineCount     int
	CandidateCount    int
	BaselineFailures  int
	CandidateFailures int
	BaselineP95MS     uint64
	CandidateP95MS    uint64
	DeltaP95MS        int64
	BaselineOwner     string
	CandidateOwner    string
	Severity          string
	Comparable        bool
	ComparisonNote    string
	BaselinePresent   bool
	CandidatePresent  bool
}

func routeCompareRows(baseline, candidate analyze.Summary) []routeCompareRow {
	base := map[string]analyze.RouteStats{}
	cand := map[string]analyze.RouteStats{}
	names := map[string]struct{}{}
	for _, route := range baseline.Routes {
		base[route.Route] = route
		names[route.Route] = struct{}{}
	}
	for _, route := range candidate.Routes {
		cand[route.Route] = route
		names[route.Route] = struct{}{}
	}
	rows := make([]routeCompareRow, 0, len(names))
	for name := range names {
		b, hasBaseline := base[name]
		c, hasCandidate := cand[name]
		delta := saturatingSignedDelta(c.P95MS, b.P95MS)
		comparable := hasBaseline && hasCandidate && b.Count > 0 && c.Count > 0
		severity := "ok"
		note := comparePresenceNote(hasBaseline, hasCandidate, "маршрут")
		if comparable {
			severity = latencyDeltaSeverity(b.P95MS, c.P95MS)
			if min(b.Count, c.Count) < 3 {
				severity = capSeverity(severity, "medium")
				note = "меньше трех запросов хотя бы в одном прогоне"
			}
		}
		rows = append(rows, routeCompareRow{
			Route:             name,
			BaselineCount:     b.Count,
			CandidateCount:    c.Count,
			BaselineFailures:  b.Failures,
			CandidateFailures: c.Failures,
			BaselineP95MS:     b.P95MS,
			CandidateP95MS:    c.P95MS,
			DeltaP95MS:        delta,
			BaselineOwner:     b.OwnerSample,
			CandidateOwner:    c.OwnerSample,
			Severity:          severity,
			Comparable:        comparable,
			ComparisonNote:    note,
			BaselinePresent:   hasBaseline,
			CandidatePresent:  hasCandidate,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		if int64Magnitude(rows[i].DeltaP95MS) != int64Magnitude(rows[j].DeltaP95MS) {
			return int64Magnitude(rows[i].DeltaP95MS) > int64Magnitude(rows[j].DeltaP95MS)
		}
		return rows[i].Route < rows[j].Route
	})
	return rows
}

type screenCompareRow struct {
	Screen                       string
	BaselineFrames               uint64
	CandidateFrames              uint64
	BaselineJankPct              float64
	CandidateJankPct             float64
	DeltaJankPct                 float64
	BaselineAvgFPS               float64
	CandidateAvgFPS              float64
	DeltaFPS                     float64
	BaselineFPSAvailable         bool
	CandidateFPSAvailable        bool
	FPSComparable                bool
	BaselineFrameP95MS           uint64
	CandidateFrameP95MS          uint64
	FrameTailComparisonAvailable bool
	Severity                     string
	Comparable                   bool
	ComparisonNote               string
	BaselinePresent              bool
	CandidatePresent             bool
}

func screenCompareRows(baseline, candidate analyze.Summary) []screenCompareRow {
	base := map[string]analyze.ScreenStats{}
	cand := map[string]analyze.ScreenStats{}
	names := map[string]struct{}{}
	for _, screen := range baseline.Screens {
		base[screen.Screen] = screen
		names[screen.Screen] = struct{}{}
	}
	for _, screen := range candidate.Screens {
		cand[screen.Screen] = screen
		names[screen.Screen] = struct{}{}
	}
	rows := make([]screenCompareRow, 0, len(names))
	for name := range names {
		b, hasBaseline := base[name]
		c, hasCandidate := cand[name]
		deltaJank := c.JankRatePct - b.JankRatePct
		baselineFPSAvailable := b.AvgFPS > 0
		candidateFPSAvailable := c.AvgFPS > 0
		fpsComparable := baselineFPSAvailable && candidateFPSAvailable
		deltaFPS := 0.0
		if fpsComparable {
			deltaFPS = c.AvgFPS - b.AvgFPS
		}
		comparable := hasBaseline && hasCandidate && b.Frames > 0 && c.Frames > 0
		severity := "ok"
		note := comparePresenceNote(hasBaseline, hasCandidate, "экран")
		if comparable {
			severity = screenDeltaSeverity(deltaJank, deltaFPS)
			if min(b.Frames, c.Frames) < 120 {
				severity = capSeverity(severity, "medium")
				note = "меньше 120 кадров хотя бы в одном прогоне"
			}
		}
		rows = append(rows, screenCompareRow{
			Screen:                       name,
			BaselineFrames:               b.Frames,
			CandidateFrames:              c.Frames,
			BaselineJankPct:              b.JankRatePct,
			CandidateJankPct:             c.JankRatePct,
			DeltaJankPct:                 deltaJank,
			BaselineAvgFPS:               b.AvgFPS,
			CandidateAvgFPS:              c.AvgFPS,
			DeltaFPS:                     deltaFPS,
			BaselineFPSAvailable:         baselineFPSAvailable,
			CandidateFPSAvailable:        candidateFPSAvailable,
			FPSComparable:                fpsComparable,
			BaselineFrameP95MS:           b.FrameP95MS,
			CandidateFrameP95MS:          c.FrameP95MS,
			FrameTailComparisonAvailable: b.FrameDistributionState == "mergeable_histogram_v2" && c.FrameDistributionState == "mergeable_histogram_v2",
			Severity:                     severity,
			Comparable:                   comparable,
			ComparisonNote:               note,
			BaselinePresent:              hasBaseline,
			CandidatePresent:             hasCandidate,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		if math.Abs(rows[i].DeltaJankPct) != math.Abs(rows[j].DeltaJankPct) {
			return math.Abs(rows[i].DeltaJankPct) > math.Abs(rows[j].DeltaJankPct)
		}
		return rows[i].Screen < rows[j].Screen
	})
	return rows
}

type ownerCompareRow struct {
	Owner            string
	Kind             string
	BaselineCount    int
	CandidateCount   int
	BaselineMaxMS    uint64
	CandidateMaxMS   uint64
	DeltaMaxMS       int64
	BaselineTotalMS  uint64
	CandidateTotalMS uint64
	Severity         string
	Comparable       bool
	ComparisonNote   string
	BaselinePresent  bool
	CandidatePresent bool
}

func ownerCompareRows(baseline, candidate analyze.Summary) []ownerCompareRow {
	base := map[string]analyze.OwnerStats{}
	cand := map[string]analyze.OwnerStats{}
	names := map[string]struct{}{}
	for _, owner := range baseline.Owners {
		base[owner.Owner] = owner
		names[owner.Owner] = struct{}{}
	}
	for _, owner := range candidate.Owners {
		cand[owner.Owner] = owner
		names[owner.Owner] = struct{}{}
	}
	rows := make([]ownerCompareRow, 0, len(names))
	for name := range names {
		b, hasBaseline := base[name]
		c, hasCandidate := cand[name]
		kind := firstNonEmpty(c.Kind, b.Kind)
		delta := saturatingSignedDelta(c.MaxMS, b.MaxMS)
		comparable := hasBaseline && hasCandidate && b.Count > 0 && c.Count > 0
		severity := "ok"
		note := comparePresenceNote(hasBaseline, hasCandidate, "источник")
		if comparable {
			severity = latencyDeltaSeverity(b.MaxMS, c.MaxMS)
			if min(b.Count, c.Count) < 3 {
				severity = capSeverity(severity, "medium")
				note = "меньше трех событий хотя бы в одном прогоне"
			}
		}
		rows = append(rows, ownerCompareRow{
			Owner:            name,
			Kind:             kind,
			BaselineCount:    b.Count,
			CandidateCount:   c.Count,
			BaselineMaxMS:    b.MaxMS,
			CandidateMaxMS:   c.MaxMS,
			DeltaMaxMS:       delta,
			BaselineTotalMS:  b.TotalMS,
			CandidateTotalMS: c.TotalMS,
			Severity:         severity,
			Comparable:       comparable,
			ComparisonNote:   note,
			BaselinePresent:  hasBaseline,
			CandidatePresent: hasCandidate,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		if int64Magnitude(rows[i].DeltaMaxMS) != int64Magnitude(rows[j].DeltaMaxMS) {
			return int64Magnitude(rows[i].DeltaMaxMS) > int64Magnitude(rows[j].DeltaMaxMS)
		}
		return rows[i].Owner < rows[j].Owner
	})
	return rows
}

type signalContextCompareRow struct {
	Screen                string
	Operation             string
	Owner                 string
	BaselineProblems      uint64
	CandidateProblems     uint64
	DeltaProblems         int64
	BaselineLogSpam       uint64
	CandidateLogSpam      uint64
	DeltaLogSpam          int64
	BaselineHTTPP95MS     uint64
	CandidateHTTPP95MS    uint64
	DeltaHTTPP95MS        int64
	BaselineStallMaxMS    uint64
	CandidateStallMaxMS   uint64
	DeltaStallMaxMS       int64
	BaselineJankPct       float64
	CandidateJankPct      float64
	DeltaJankPct          float64
	Severity              string
	Comparable            bool
	ComparisonNote        string
	BaselinePresent       bool
	CandidatePresent      bool
	BaselineHTTPPresent   bool
	CandidateHTTPPresent  bool
	BaselineUIPresent     bool
	CandidateUIPresent    bool
	BaselineStallPresent  bool
	CandidateStallPresent bool
	CountsComparable      bool
}

func signalContextCompareRows(baseline, candidate analyze.Summary) []signalContextCompareRow {
	base := map[string]analyze.SignalContextStats{}
	cand := map[string]analyze.SignalContextStats{}
	keys := map[string]struct{}{}
	for _, context := range baseline.SignalContexts {
		key := signalContextStatsKey(context)
		base[key] = context
		keys[key] = struct{}{}
	}
	for _, context := range candidate.SignalContexts {
		key := signalContextStatsKey(context)
		cand[key] = context
		keys[key] = struct{}{}
	}
	rows := make([]signalContextCompareRow, 0, len(keys))
	for key := range keys {
		b, hasBaseline := base[key]
		c, hasCandidate := cand[key]
		countsComparable := hasBaseline && hasCandidate && durationComparableForReport(baseline.DurationMS, candidate.DurationMS)
		httpComparable := hasBaseline && hasCandidate && b.HTTPCount > 0 && c.HTTPCount > 0
		stallComparable := hasBaseline && hasCandidate && b.StallCount > 0 && c.StallCount > 0
		uiComparable := hasBaseline && hasCandidate && b.UIFrames > 0 && c.UIFrames > 0
		problemDelta := int64(0)
		logDelta := int64(0)
		httpDelta := int64(0)
		stallDelta := int64(0)
		jankDelta := float64(0)
		if countsComparable {
			problemDelta = saturatingSignedDelta(c.ProblemCount, b.ProblemCount)
			logDelta = saturatingSignedDelta(c.LogSpam, b.LogSpam)
		}
		if httpComparable {
			httpDelta = saturatingSignedDelta(c.HTTPP95MS, b.HTTPP95MS)
		}
		if stallComparable {
			stallDelta = saturatingSignedDelta(c.StallMaxMS, b.StallMaxMS)
		}
		if uiComparable {
			jankDelta = c.UIJankPct - b.UIJankPct
		}
		comparable := countsComparable || httpComparable || stallComparable || uiComparable
		severity := "ok"
		if comparable {
			severity = signalContextDeltaSeverity(problemDelta, logDelta, httpDelta, stallDelta, jankDelta)
		}
		note := comparePresenceNote(hasBaseline, hasCandidate, "контекст операции")
		if hasBaseline && hasCandidate && !comparable {
			note = "нет метрик с сопоставимым покрытием"
		} else if hasBaseline && hasCandidate && !countsComparable {
			note = "количества не сравниваются из-за разной длительности; статус рассчитан по доступным latency/UI метрикам"
		}
		rows = append(rows, signalContextCompareRow{
			Screen:                firstNonEmpty(c.Screen, b.Screen),
			Operation:             firstNonEmpty(c.Operation, b.Operation),
			Owner:                 firstNonEmpty(c.Owner, b.Owner),
			BaselineProblems:      b.ProblemCount,
			CandidateProblems:     c.ProblemCount,
			DeltaProblems:         problemDelta,
			BaselineLogSpam:       b.LogSpam,
			CandidateLogSpam:      c.LogSpam,
			DeltaLogSpam:          logDelta,
			BaselineHTTPP95MS:     b.HTTPP95MS,
			CandidateHTTPP95MS:    c.HTTPP95MS,
			DeltaHTTPP95MS:        httpDelta,
			BaselineStallMaxMS:    b.StallMaxMS,
			CandidateStallMaxMS:   c.StallMaxMS,
			DeltaStallMaxMS:       stallDelta,
			BaselineJankPct:       b.UIJankPct,
			CandidateJankPct:      c.UIJankPct,
			DeltaJankPct:          jankDelta,
			Severity:              severity,
			Comparable:            comparable,
			ComparisonNote:        note,
			BaselinePresent:       hasBaseline,
			CandidatePresent:      hasCandidate,
			BaselineHTTPPresent:   b.HTTPCount > 0,
			CandidateHTTPPresent:  c.HTTPCount > 0,
			BaselineUIPresent:     b.UIFrames > 0,
			CandidateUIPresent:    c.UIFrames > 0,
			BaselineStallPresent:  b.StallCount > 0,
			CandidateStallPresent: c.StallCount > 0,
			CountsComparable:      countsComparable,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if severityRank(rows[i].Severity) != severityRank(rows[j].Severity) {
			return severityRank(rows[i].Severity) > severityRank(rows[j].Severity)
		}
		left := signalContextDeltaSortScore(rows[i])
		right := signalContextDeltaSortScore(rows[j])
		if left != right {
			return left > right
		}
		return signalContextLabel(rows[i].Screen, rows[i].Operation, rows[i].Owner) < signalContextLabel(rows[j].Screen, rows[j].Operation, rows[j].Owner)
	})
	return rows
}

func comparePresenceNote(hasBaseline, hasCandidate bool, entity string) string {
	switch {
	case hasBaseline && hasCandidate:
		return ""
	case hasCandidate:
		return "новый " + entity + " кандидата; дельта не вычисляется"
	default:
		return entity + " есть только в базе; дельта не вычисляется"
	}
}

func capSeverity(value, maximum string) string {
	if severityRank(value) > severityRank(maximum) {
		return maximum
	}
	return value
}

func durationComparableForReport(baselineMS, candidateMS uint64) bool {
	if baselineMS == 0 || candidateMS == 0 {
		return false
	}
	shorter := baselineMS
	longer := candidateMS
	if shorter > longer {
		shorter, longer = longer, shorter
	}
	return float64(longer-shorter)/float64(shorter) <= 0.2
}

func signalContextDeltaSortScore(row signalContextCompareRow) uint64 {
	score := saturatingMulUint64(int64Magnitude(row.DeltaProblems), 10_000)
	score = saturatingAddUint64(score, saturatingMulUint64(int64Magnitude(row.DeltaLogSpam), 10))
	score = saturatingAddUint64(score, int64Magnitude(row.DeltaStallMaxMS))
	return saturatingAddUint64(score, int64Magnitude(row.DeltaHTTPP95MS))
}

func signalContextStatsKey(context analyze.SignalContextStats) string {
	return strings.Join([]string{context.Screen, context.Operation, context.Owner}, "\x00")
}

func signalContextLabel(screen, operation, owner string) string {
	parts := compactReportParts(screen, operation, owner)
	if len(parts) == 0 {
		return "контекст не задан"
	}
	return strings.Join(parts, " / ")
}

func signalContextLabelHint(screen, operation, owner string) template.HTML {
	label := signalContextLabel(screen, operation, owner)
	hint := signalContextHint(screen, operation, owner)
	if hint == "" {
		return inlineHTMLText(label)
	}
	return tooltipHTML(label, hint)
}

func contextValueHint(value string, field string) template.HTML {
	return reportValueHint(value, "нет данных", field)
}

func databaseSourceQueryHint(source, query string) template.HTML {
	body := strings.TrimSpace(query)
	if isUnknownReportValue(body) {
		body = missingDataHint("query")
	}
	return tooltipHTML(reportValue(source, "место вызова не записано"), body)
}

func reportValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if isUnknownReportValue(value) {
		return fallback
	}
	return displayDateText(value)
}

func displayDateText(value string) string {
	return isoDatePattern.ReplaceAllStringFunc(value, func(candidate string) string {
		parsed, err := time.Parse("2006-01-02", candidate)
		if err != nil {
			return candidate
		}
		return parsed.Format("02.01.2006")
	})
}

func reportValueHint(value string, fallback string, field string) template.HTML {
	label := reportValue(value, fallback)
	if !valueNeedsMissingHint(value, field) {
		return inlineHTMLText(label)
	}
	if hint := missingDataHint(field); hint != "" {
		return tooltipHTML(label, hint)
	}
	return inlineHTMLText(label)
}

func valueNeedsMissingHint(value string, field string) bool {
	if isUnknownReportValue(value) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "session", "device", "app", "build", "sdk", "android", "process", "network":
		normalized := datavalue.NormalizeUnknown(value)
		return strings.Contains(normalized, "неизвест") || strings.Contains(normalized, "нет данных")
	default:
		return false
	}
}

func compactReportParts(values ...string) []string {
	parts := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if isUnknownReportValue(value) {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		parts = append(parts, value)
	}
	return parts
}

func isUnknownReportValue(value string) bool {
	return datavalue.IsUnknown(value)
}

func cohortValue(value string) string {
	value = strings.TrimSpace(value)
	if isUnknownReportValue(value) {
		return "неизвестно"
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "неизвестно"
	}
	for i, field := range fields {
		key, raw, ok := strings.Cut(field, "=")
		if !ok {
			fields[i] = reportValue(field, "неизвестно")
			continue
		}
		fields[i] = key + "=" + reportValue(raw, "неизвестно")
	}
	return strings.Join(fields, " ")
}

func cohortValueHint(value string) template.HTML {
	label := cohortValue(value)
	if !cohortHasMissingPart(value) {
		return inlineHTMLText(label)
	}
	return tooltipHTML(label, missingDataHint("cohort"))
}

func cohortHasMissingPart(value string) bool {
	if isUnknownReportValue(value) {
		return true
	}
	for _, field := range strings.Fields(value) {
		_, raw, ok := strings.Cut(field, "=")
		if ok && isUnknownReportValue(raw) {
			return true
		}
	}
	return false
}

func signalContextHint(screen, operation, owner string) string {
	missing := make([]string, 0, 3)
	if isUnknownReportValue(screen) {
		missing = append(missing, "экран: "+missingDataHint("screen"))
	}
	if isUnknownReportValue(operation) {
		missing = append(missing, "операция: "+missingDataHint("operation"))
	}
	if isUnknownReportValue(owner) {
		missing = append(missing, "источник: "+missingDataHint("owner"))
	}
	return strings.Join(missing, "\n\n")
}

func localizedGrowthFreshnessReason(value string) string {
	return strings.NewReplacer(
		"growth checkpoint", "контрольная точка роста журнала",
		"committed events", "зафиксированные события",
		"live checkpoint", "текущая контрольная точка",
		"session", "сессия",
		"freshness", "актуальность",
		"lag=", "отставание ",
	).Replace(value)
}

func missingDataHint(field string) string {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "screen":
		return "Экран определяется по обратным вызовам жизненного цикла Activity. Если он не записался, ActivityTracker не подключился к Application или событие произошло до первого обратного вызова Activity."
	case "operation":
		return "Операция появляется из startOperation/traceOperation, @JankHunterOperation или автоматического измерения взаимодействий. Если её нет, участок не был размечен либо инструментирование не попало в пакет."
	case "owner":
		return "Источник приходит из встроенных символов преобразования байткода, @JankHunterOwner, @JankHunterOperation, withOwner или ownerHint. Если его нет, участок выполнился без атрибуции."
	case "log-source":
		return "Источник логов определяется при инструментировании вызовов Log/Timber. Если его нет, вызов прошёл без ASM-инструментации или сигнатура логгера не поддержана текущим адаптером."
	case "route", "network-route":
		return "Маршрут определяется при инструментировании OkHttp/HTTP. Если он отсутствует при сетевых симптомах, проверьте настройки instrument.okhttp и includePackages, полный охват приложения и поддержку используемой версии OkHttp."
	case "query", "sql":
		return "SQL-шаблон не записан. Откройте указанное рядом место вызова DAO или SQLite и проверьте выполняемый им запрос."
	case "call", "caller", "callee":
		return "Узел графа вызовов определяется инструментированием runtimeCallGraph. Если его нет, вызов не попал в указанные пакеты или связь не удалось сохранить из-за ограничения объёма данных."
	case "stack":
		return "Подсказка стека берется из верхнего пользовательского кадра при фиксации работы. Если ее нет, стек не содержал подходящего кадра."
	case "holder":
		return "Держатель утечки восстанавливается из ownerHint, текущего владельца или имени класса. Если он не определён, watchObject был вызван без ownerHint, имени класса и активного контекста."
	case "device", "session":
		return "Метаданные устройства записываются при начале запуска Jank Hunter. Если они пустые, Jank Hunter не успел запуститься до событий или инициализация прошла без контекста Application."
	case "app", "build":
		return "Версия приложения берётся из PackageInfo при запуске Jank Hunter. Если её нет, начальное событие не записалось или PackageInfo был недоступен в этом процессе."
	case "sdk", "android":
		return "Версия Android и SDK записываются в снимке устройства при запуске Jank Hunter. Если их нет, начальное событие не попало в журнал."
	case "process":
		return "Имя процесса записывается в начале запуска. Если его нет, Jank Hunter не смог получить имя процесса или начальные сведения не попали в журнал."
	case "network":
		return "Тип сети записывается в снимке контекста через ConnectivityManager/NetworkCapabilities. Если его нет, снимок не успел выполниться или система не вернула активную сеть."
	case "cohort":
		return "Когорта собирается из сведений об устройстве, приложении, сборке, процессе и сети. Неопределённая часть означает, что соответствующее начальное событие или снимок контекста не попал в журнал."
	default:
		return ""
	}
}

func signalContextDeltaSeverity(problemDelta, logDelta, httpDelta, stallDelta int64, jankDelta float64) string {
	if problemDelta >= 10 || stallDelta >= 500 || httpDelta >= 500 || jankDelta >= 3 {
		return "high"
	}
	if problemDelta > 0 || logDelta >= 50 || stallDelta >= 100 || httpDelta >= 100 || jankDelta >= 1 {
		return "medium"
	}
	return "ok"
}

func summaryLogSpamTotal(summary analyze.Summary) uint64 {
	var total uint64
	for _, item := range summary.LogSpam {
		total = saturatingAddUint64(total, item.Count)
	}
	return total
}

func summaryProblemWindowTotal(summary analyze.Summary) uint64 {
	var total uint64
	for _, item := range summary.ProblemWindows {
		total = saturatingAddUint64(total, uint64(item.Windows))
	}
	return total
}

func perMinute(value, durationMS uint64) float64 {
	if durationMS == 0 {
		return 0
	}
	return float64(value) * 60_000 / float64(durationMS)
}

func latencyDeltaSeverity(baseline, candidate uint64) string {
	if baseline == 0 && candidate > 0 {
		if candidate >= 1000 {
			return "high"
		}
		return "medium"
	}
	if candidate <= baseline {
		return "ok"
	}
	delta := candidate - baseline
	pct := 0.0
	if baseline > 0 {
		pct = float64(delta) * 100 / float64(baseline)
	}
	if delta >= 500 || pct >= 50 {
		return "high"
	}
	if delta >= 100 || pct >= 15 {
		return "medium"
	}
	return "ok"
}

func screenDeltaSeverity(deltaJankPct, deltaFPS float64) string {
	if deltaJankPct >= 3 || deltaFPS <= -5 {
		return "high"
	}
	if deltaJankPct >= 1 || deltaFPS <= -2 {
		return "medium"
	}
	return "ok"
}

func signedMS(value int64) string {
	if value == 0 {
		return "0 мс"
	}
	return fmt.Sprintf("%+d мс", value)
}

func signedDuration(value int64) string {
	if value == 0 {
		return "0 мс"
	}
	sign := "+"
	if value < 0 {
		sign = "-"
	}
	return sign + humanDuration(int64Magnitude(value))
}

func signedFloat(value float64, unit string) string {
	if value == 0 {
		return "0 " + unit
	}
	return fmt.Sprintf("%+.2f %s", value, unit)
}

const (
	maxSignedInt64 = int64(^uint64(0) >> 1)
	minSignedInt64 = -maxSignedInt64 - 1
)

func saturatingSignedDelta(after, before uint64) int64 {
	if after >= before {
		difference := after - before
		if difference > uint64(maxSignedInt64) {
			return maxSignedInt64
		}
		return int64(difference)
	}
	difference := before - after
	if difference > uint64(maxSignedInt64) {
		return minSignedInt64
	}
	return -int64(difference)
}

func int64Magnitude(value int64) uint64 {
	if value >= 0 {
		return uint64(value)
	}
	return uint64(-(value + 1)) + 1
}

func saturatingAddUint64(left, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

func saturatingMulUint64(left, right uint64) uint64 {
	if left == 0 || right == 0 {
		return 0
	}
	if left > ^uint64(0)/right {
		return ^uint64(0)
	}
	return left * right
}

func nonNegativeInt(value int) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

type heuristicCard struct {
	Severity string
	Title    string
	Detail   string
}

type heuristicSummary struct {
	Severity string
	Status   string
	Summary  string
	Cards    []heuristicCard
}

func inspectMathHeuristic(report mathanalysis.MathReport) heuristicSummary {
	summary := heuristicSummary{Severity: "ok", Status: "Математический профиль спокоен", Summary: "Критичных математических сигналов не найдено."}
	for _, section := range report.Sections {
		if severityRank(section.Status) > severityRank(summary.Severity) {
			summary.Severity = section.Status
		}
	}
	switch summary.Severity {
	case "high":
		summary.Status = "Требуется разбор"
		summary.Summary = "Есть сильные математические сигналы деградации. Начните с карточек ниже и проверьте связанные маршруты, источники и контекст."
	case "medium":
		summary.Status = "Есть сигналы для проверки"
		summary.Summary = "Обнаружены предупреждения. Их стоит подтвердить повторным прогоном и связать с конкретными владельцами работ."
	}
	if len(report.NetworkLoops) > 0 {
		loop := report.NetworkLoops[0]
		target := firstNonEmpty(loop.Route, loop.Owner, "сетевой сценарий")
		summary.Cards = append(summary.Cards, heuristicCard{Severity: networkLoopCardSeverity(loop.Confidence, loop.BurnScore), Title: "Признак сетевого цикла", Detail: fmt.Sprintf("Проверьте %s: предполагаемый период %.1f сек, уверенность %.2f, условная нагрузка %.1f. Это гипотеза, а не доказанная причина.", target, float64(loop.PeriodMS)/1000, loop.Confidence, loop.BurnScore)})
	}
	if len(report.CausalGraph.OwnerScores) > 0 {
		owner := report.CausalGraph.OwnerScores[0]
		ownerLabel := reportValue(owner.Owner, "место запуска не записано")
		detail := fmt.Sprintf("%s чаще других совпадало по времени с плохими состояниями и сетевыми циклами; условная оценка %.2f. Проверьте трассировку и код: совпадение не доказывает причину.", ownerLabel, owner.Score)
		if isUnknownReportValue(owner.Owner) {
			detail += " Чтобы восстановить место запуска, проверьте инструментирование сетевого клиента и включение пакета с кодом, который запускает запрос."
		}
		summary.Cards = append(summary.Cards, heuristicCard{Severity: "medium", Title: "Место запуска, чаще связанное с проблемами", Detail: detail})
	}
	if context, ok := topProblemSignalContext(report.Summary); ok {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: signalContextCardSeverity(context), Title: "Операция со связанными сигналами", Detail: fmt.Sprintf("%s: проблем %d, лишних сообщений журнала %d, граница верхних 5%% HTTP-задержек %d мс, максимальная пауза %d мс.", signalContextLabel(context.Screen, context.Operation, context.Owner), context.ProblemCount, context.LogSpam, context.HTTPP95MS, context.StallMaxMS)})
	}
	if score, ok := topIntegralScore(report.IntegralScores); ok {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: score.Severity, Title: score.Title, Detail: fmt.Sprintf("%.1f %s. %s", score.Value, score.Unit, score.Explanation)})
	}
	if len(summary.Cards) == 0 {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: "ok", Title: "Что проверить первым", Detail: "Используйте отчёт как контрольную точку. При предупреждении начните с измеренного сигнала, затем проверьте временную шкалу, исходные события и граф связей."})
	}
	return summary
}

func compareMathHeuristic(report mathanalysis.CompareMathReport) heuristicSummary {
	summary := heuristicSummary{Severity: "ok", Status: "Сравнение выглядит стабильным", Summary: "Сильных математических ухудшений между базой и кандидатом не найдено."}
	for _, section := range report.Sections {
		if severityRank(section.Status) > severityRank(summary.Severity) {
			summary.Severity = section.Status
		}
	}
	switch summary.Severity {
	case "high":
		summary.Status = "Проверяемый прогон требует расследования"
		summary.Summary = "Есть сильные математические дельты. Проверьте, совпадают ли они с изменениями маршрутов, экранов, памяти или контекста устройства."
	case "medium":
		summary.Status = "Есть предупреждения по кандидату"
		summary.Summary = "Найдены умеренные отличия. Подтвердите их повторным прогоном перед инженерным выводом."
	}
	if len(report.RobustDeltas) > 0 {
		for _, delta := range report.RobustDeltas {
			if delta.Severity == "high" || delta.Severity == "medium" {
				detail := delta.Summary
				if delta.Comparable && delta.DeltaPctAvailable {
					detail = fmt.Sprintf("%s / %s: p95 изменился на %+.1f %s (%+.1f%%), доверие %s.", delta.Dimension, delta.Metric, delta.P95Delta, delta.Unit, delta.P95DeltaPct, delta.Confidence)
				}
				summary.Cards = append(summary.Cards, heuristicCard{Severity: delta.Severity, Title: "Распределение изменилось", Detail: detail})
				break
			}
		}
	}
	if len(report.NetworkLoopDeltas) > 0 {
		delta := report.NetworkLoopDeltas[0]
		target := firstNonEmpty(delta.Route, delta.Owner, "сетевой цикл")
		summary.Cards = append(summary.Cards, heuristicCard{Severity: delta.Severity, Title: "Изменение кандидата сетевого цикла", Detail: fmt.Sprintf("%s: изменение условной нагрузки %+.1f, изменение уверенности %+.2f.", target, delta.BurnDelta, delta.ConfidenceDelta)})
	}
	if len(report.CausalDeltas) > 0 {
		delta := report.CausalDeltas[0]
		summary.Cards = append(summary.Cards, heuristicCard{Severity: delta.Severity, Title: "Граф связей изменился", Detail: delta.Summary})
	}
	if row, ok := topSignalContextDelta(report.Comparison.Baseline, report.Comparison.Candidate); ok && row.Severity != "ok" {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: row.Severity, Title: "Связанные сигналы операции ухудшились", Detail: fmt.Sprintf("%s: изменение числа проблем %d, лишних сообщений журнала %d, границы верхних 5%% HTTP-задержек %d мс, подтормаживаний интерфейса %+.2f п.п.", signalContextLabel(row.Screen, row.Operation, row.Owner), row.DeltaProblems, row.DeltaLogSpam, row.DeltaHTTPP95MS, row.DeltaJankPct)})
	}
	if len(summary.Cards) == 0 {
		summary.Cards = append(summary.Cards, heuristicCard{Severity: "ok", Title: "Что проверить первым", Detail: "Сохраните сравнение как контрольную точку. При следующем ухудшении начните с распределений, временной шкалы и исходных событий, а граф связей используйте только как список гипотез."})
	}
	return summary
}

func topProblemSignalContext(summary analyze.Summary) (analyze.SignalContextStats, bool) {
	if len(summary.SignalContexts) == 0 {
		return analyze.SignalContextStats{}, false
	}
	best := summary.SignalContexts[0]
	for _, context := range summary.SignalContexts[1:] {
		if signalContextProblemScore(context) > signalContextProblemScore(best) {
			best = context
		}
	}
	if signalContextProblemScore(best) == 0 {
		return analyze.SignalContextStats{}, false
	}
	return best, true
}

func signalContextProblemScore(context analyze.SignalContextStats) uint64 {
	score := saturatingMulUint64(context.ProblemCount, 10_000)
	score = saturatingAddUint64(score, saturatingMulUint64(context.LogSpam, 10))
	score = saturatingAddUint64(score, saturatingMulUint64(nonNegativeInt(context.StallCount), 1_000))
	score = saturatingAddUint64(score, context.StallMaxMS)
	score = saturatingAddUint64(score, context.HTTPP95MS)
	return saturatingAddUint64(score, context.UIJank)
}

func signalContextCardSeverity(context analyze.SignalContextStats) string {
	if context.ProblemCount >= 10 || context.StallMaxMS >= 1000 || context.HTTPP95MS >= 1500 {
		return "high"
	}
	if context.ProblemCount > 0 || context.LogSpam >= 50 || context.StallMaxMS >= 250 || context.HTTPP95MS >= 500 {
		return "medium"
	}
	return "ok"
}

type operationContextInsight struct {
	Title       string
	Severity    string
	Status      string
	Context     string
	Summary     string
	Impact      string
	FirstCheck  string
	SignalCount int
	Tooltip     string
}

func operationContextInsights(summary analyze.Summary) []operationContextInsight {
	insights := make([]operationContextInsight, 0, len(summary.SignalContexts))
	for _, context := range summary.SignalContexts {
		if !signalContextNeedsAttention(context) {
			continue
		}
		severity, status := operationContextVerdict(context)
		signals := operationContextSignalSummary(context)
		insights = append(insights, operationContextInsight{
			Title:       operationContextTitle(context),
			Severity:    severity,
			Status:      status,
			Context:     operationContextDescription(context),
			Summary:     strings.Join(signals, " "),
			Impact:      operationContextImpact(context),
			FirstCheck:  operationContextFirstCheck(context),
			SignalCount: operationContextSignalCount(context),
			Tooltip:     "Карточка объединяет сигналы, записанные для одной операции и экрана, и предлагает первый практический шаг проверки.",
		})
	}
	sort.SliceStable(insights, func(i, j int) bool {
		left := severityOrder(insights[i].Severity)
		right := severityOrder(insights[j].Severity)
		if left != right {
			return left > right
		}
		if insights[i].SignalCount != insights[j].SignalCount {
			return insights[i].SignalCount > insights[j].SignalCount
		}
		return insights[i].Title < insights[j].Title
	})
	if len(insights) > 8 {
		return insights[:8]
	}
	return insights
}

func signalContextNeedsAttention(context analyze.SignalContextStats) bool {
	return context.ProblemCount > 0 || context.HTTPFailed > 0 || context.HTTPP95MS >= 700 ||
		context.StallCount > 0 || context.UIJank > 0 || context.LogSpam >= 10 || context.MemoryMaxKB >= 256*1024
}

func operationContextVerdict(context analyze.SignalContextStats) (string, string) {
	switch {
	case context.StallMaxMS >= 1500 || context.UIJankPct >= 20 || context.HTTPFailed >= 3:
		return "critical", "критично"
	case context.StallMaxMS >= 700 || context.UIJankPct >= 10 || context.HTTPP95MS >= 1500 || context.HTTPFailed > 0:
		return "high", "высокий риск"
	case context.StallCount > 0 || context.UIJankPct >= 5 || context.HTTPP95MS >= 700 || context.LogSpam >= 50 || context.ProblemCount > 0:
		return "medium", "нужно проверить"
	default:
		return "low", "наблюдение"
	}
}

func operationContextTitle(context analyze.SignalContextStats) string {
	parts := make([]string, 0, 2)
	if !isUnknownReportValue(context.Operation) {
		parts = append(parts, context.Operation)
	} else if !isUnknownReportValue(context.Screen) {
		parts = append(parts, context.Screen)
	} else if !isUnknownReportValue(context.Owner) {
		parts = append(parts, context.Owner)
	} else if !isUnknownReportValue(context.RouteSample) {
		parts = append(parts, context.RouteSample)
	} else {
		parts = append(parts, "Операция без названия")
	}
	return strings.Join(parts, " · ")
}

func operationContextDescription(context analyze.SignalContextStats) string {
	parts := make([]string, 0, 4)
	if !isUnknownReportValue(context.Screen) {
		parts = append(parts, "экран "+context.Screen)
	}
	if !isUnknownReportValue(context.Owner) {
		parts = append(parts, "источник "+context.Owner)
	}
	if !isUnknownReportValue(context.RouteSample) {
		parts = append(parts, "маршрут "+context.RouteSample)
	}
	if len(parts) == 0 {
		return "Точное место не размечено; добавьте экран, операцию или источник работ."
	}
	return strings.Join(parts, " · ")
}

func operationContextSignalSummary(context analyze.SignalContextStats) []string {
	parts := make([]string, 0, 6)
	if context.UIFrames > 0 && context.UIJank > 0 {
		parts = append(parts, fmt.Sprintf("Медленными были %s из %s (%.1f%%).", russianCount(context.UIJank, "кадр", "кадра", "кадров"), russianCount(context.UIFrames, "кадра", "кадров", "кадров"), context.UIJankPct))
	}
	if context.StallCount > 0 {
		parts = append(parts, fmt.Sprintf("Главный поток останавливался %s; максимум — %d мс.", russianCount(context.StallCount, "раз", "раза", "раз"), context.StallMaxMS))
	}
	if context.HTTPCount > 0 {
		parts = append(parts, operationContextHTTPText(context))
	}
	if context.LogSpam > 0 {
		parts = append(parts, fmt.Sprintf("Лишних записей в лог — %d.", context.LogSpam))
	}
	if context.ProblemCount > 0 {
		parts = append(parts, fmt.Sprintf("Объединённых проблемных сигналов — %d.", context.ProblemCount))
	}
	if context.MemoryMaxKB > 0 {
		parts = append(parts, fmt.Sprintf("Память в этом контексте доходила до %s.", humanDataSizeKB(context.MemoryMaxKB)))
	}
	return parts
}

func operationContextHTTPText(context analyze.SignalContextStats) string {
	var text string
	switch {
	case context.HTTPCount == 1:
		text = fmt.Sprintf("Единственный сетевой вызов занял до %d мс", context.HTTPP95MS)
	case context.HTTPCount < 20:
		text = fmt.Sprintf("В небольшой выборке из %d сетевых вызовов верхняя задержка составила %d мс", context.HTTPCount, context.HTTPP95MS)
	default:
		text = fmt.Sprintf("У 95%% из %d сетевых вызовов длительность не превышала %d мс", context.HTTPCount, context.HTTPP95MS)
	}
	if context.HTTPFailed > 0 {
		text += fmt.Sprintf("; с ошибкой завершилось %d", context.HTTPFailed)
	}
	return text + "."
}

func operationContextSignalCount(context analyze.SignalContextStats) int {
	count := 0
	for _, present := range []bool{
		context.UIJank > 0,
		context.StallCount > 0,
		context.HTTPCount > 0,
		context.LogSpam > 0,
		context.ProblemCount > 0,
		context.MemoryMaxKB > 0,
	} {
		if present {
			count++
		}
	}
	return count
}

func operationContextImpact(context analyze.SignalContextStats) string {
	impacts := make([]string, 0, 3)
	if context.UIJank > 0 || context.StallCount > 0 {
		impacts = append(impacts, "рывки интерфейса и задержка реакции на действие")
	}
	if context.HTTPFailed > 0 {
		impacts = append(impacts, "ошибка или незавершённый пользовательский сценарий")
	} else if context.HTTPP95MS >= 700 {
		impacts = append(impacts, "долгое ожидание данных")
	}
	if context.MemoryMaxKB >= 256*1024 {
		impacts = append(impacts, "рост числа сборок мусора и риск нехватки памяти")
	}
	if context.LogSpam >= 50 {
		impacts = append(impacts, "лишняя нагрузка на процессор и хранилище из-за частого ведения журнала")
	}
	if len(impacts) == 0 {
		return "Влияние на пользователя по текущим данным невелико, но сигнал стоит перепроверить."
	}
	return "Для пользователя это может означать: " + strings.Join(impacts, "; ") + "."
}

func operationContextFirstCheck(context analyze.SignalContextStats) string {
	place := "в этой операции"
	if !isUnknownReportValue(context.Owner) {
		place = "в " + context.Owner
	}
	switch {
	case context.StallCount > 0:
		return "Сначала откройте трассу главного потока " + place + " и найдите синхронную работу вокруг самой длинной паузы."
	case context.HTTPFailed > 0:
		return "Сначала проверьте ошибки, повторные вызовы и обработку ответа маршрута " + reportValue(context.RouteSample, "в этой операции") + "."
	case context.HTTPP95MS >= 700:
		return "Сначала проверьте длительный и повторный сетевой вызов маршрута " + reportValue(context.RouteSample, "в этой операции") + "."
	case context.UIJank > 0:
		return "Сначала профилируйте отрисовку экрана и работу " + place + " во время медленных кадров."
	case context.LogSpam >= 10:
		return "Сначала сократите частое логирование " + place + " и повторите операцию."
	default:
		return "Повторите операцию с теми же входными данными и сравните длительность, память и плавность."
	}
}

func severityOrder(value string) int {
	switch value {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func primaryCategoryCoverage(items []analyze.CategoryCoverage) []analyze.CategoryCoverage {
	visible := make([]analyze.CategoryCoverage, 0, len(items))
	for _, item := range items {
		if item.FindingCount > 0 || item.Status == "healthy" {
			visible = append(visible, item)
		}
	}
	return visible
}

func findingCategoryCoverage(items []analyze.CategoryCoverage) []analyze.CategoryCoverage {
	visible := make([]analyze.CategoryCoverage, 0, len(items))
	for _, item := range items {
		if item.FindingCount > 0 || item.Category == analyze.ProblemCategoryDependencyInjection {
			visible = append(visible, item)
		}
	}
	return visible
}

func hiddenCoverageSummary(items []analyze.CategoryCoverage) string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		if item.FindingCount == 0 && item.Status != "healthy" {
			labels = append(labels, item.Label)
		}
	}
	if len(labels) == 0 {
		return ""
	}
	return "Не проверено из-за отсутствия или малого объёма данных: " + strings.Join(labels, ", ") + ". Эти категории не показаны как нулевые, потому что ноль проблем здесь не доказан."
}

func collectionWindowNotice(summary analyze.Summary) string {
	reason := ""
	const prefix = "jankhunter.runtime.enabled.reason."
	const suffix = ".count"
	for _, item := range summary.Counters {
		if item.Value == 0 || !strings.HasPrefix(item.Name, prefix) || !strings.HasSuffix(item.Name, suffix) {
			continue
		}
		reason = strings.TrimSuffix(strings.TrimPrefix(item.Name, prefix), suffix)
		break
	}
	if reason == "" {
		return ""
	}
	inactiveBeforeStartMS := uint64(0)
	for _, item := range summary.Gauges {
		if item.Name == "jankhunter.runtime.collection_inactive_before_start_ms" {
			inactiveBeforeStartMS = item.Value
			break
		}
	}
	message := "Сбор был включён переключателем «" + reason + "» уже во время работы приложения. В отчёт вошли только события за " + humanDuration(summary.DurationMS) + " после включения; более ранние действия отсутствуют в журнале."
	if inactiveBeforeStartMS > 0 {
		message = "До включения сбора приложение работало " + humanDuration(inactiveBeforeStartMS) + ". Затем переключатель «" + reason + "» включил Jank Hunter, и в отчёт вошли события за следующие " + humanDuration(summary.DurationMS) + "."
	}
	return message
}

type customMetricInsight struct {
	Title    string
	Severity string
	Count    int
	Names    string
	Relation string
	Action   string
	Tooltip  string
}

func customMetricInsights(summary analyze.Summary) []customMetricInsight {
	type metricGroup struct {
		id    string
		title string
		names []string
	}
	groups := []metricGroup{
		{id: "memory", title: "Память и сборка мусора"},
		{id: "network", title: "Сеть"},
		{id: "ui", title: "Интерфейс и отрисовка"},
		{id: "tasks", title: "Очереди и задачи"},
		{id: "io", title: "Файлы и база данных"},
		{id: "other", title: "Остальные показатели"},
	}
	add := func(item analyze.NamedValue) {
		id := customMetricGroupID(item.Name)
		for index := range groups {
			if groups[index].id == id {
				groups[index].names = append(groups[index].names, item.Name)
				return
			}
		}
	}
	for _, item := range summary.Counters {
		add(item)
	}
	for _, item := range summary.Gauges {
		add(item)
	}
	for _, item := range summary.JankStats {
		add(item)
	}

	insights := make([]customMetricInsight, 0, len(groups))
	for _, group := range groups {
		if len(group.names) == 0 {
			continue
		}
		names := group.names
		if len(names) > 4 {
			names = names[:4]
		}
		nameSummary := strings.Join(names, ", ")
		if omitted := len(group.names) - len(names); omitted > 0 {
			nameSummary += fmt.Sprintf(" и ещё %d", omitted)
		}
		severity, relation, action := customMetricRelation(group.id, summary)
		insights = append(insights, customMetricInsight{
			Title:    group.title,
			Severity: severity,
			Count:    len(group.names),
			Names:    nameSummary,
			Relation: relation,
			Action:   action,
			Tooltip:  "Группа объединяет пользовательские показатели по смыслу и подсказывает, с какими основными сигналами отчёта их проверять вместе.",
		})
	}
	return insights
}

func customMetricGroupID(name string) string {
	value := strings.ToLower(name)
	value = strings.TrimPrefix(value, "jankhunter.")
	switch {
	case strings.Contains(value, "gc"), strings.Contains(value, "alloc"), strings.Contains(value, "heap"), strings.Contains(value, "memory"), strings.Contains(value, "pss"), strings.Contains(value, "object_watcher"), strings.Contains(value, "retained"), strings.Contains(value, "leak"):
		return "memory"
	case strings.Contains(value, "http"), strings.Contains(value, "network"), strings.Contains(value, "request"), strings.Contains(value, "socket"):
		return "network"
	case strings.Contains(value, "frame"), strings.Contains(value, "jank"), strings.Contains(value, "fps"), strings.Contains(value, "render"), strings.Contains(value, "layout"):
		return "ui"
	case strings.Contains(value, "queue"), strings.Contains(value, "thread"), strings.Contains(value, "task"), strings.Contains(value, "cpu"), strings.Contains(value, "dispatcher"):
		return "tasks"
	case strings.Contains(value, "storage"), strings.Contains(value, "disk"), strings.Contains(value, "file"), strings.Contains(value, "database"), strings.Contains(value, "sqlite"), strings.Contains(value, "io."):
		return "io"
	default:
		return "other"
	}
}

func customMetricRelation(group string, summary analyze.Summary) (string, string, string) {
	switch group {
	case "memory":
		if summary.Retained > 0 || summary.MemoryMaxKB > 0 {
			return "medium", fmt.Sprintf("В этом же прогоне: удержанных объектов — %d, максимум занятой процессом памяти — %s. Смотрите эти значения вместе с выделением памяти и частотой сборки мусора.", summary.Retained, humanDataSizeKB(summary.MemoryMaxKB)), "Повторите сценарий и проверьте, возвращаются ли память и число удержаний к исходному уровню."
		}
		return "low", "Основных событий памяти рядом не записано.", "Используйте эти показатели для сравнения одинаковых сценариев между прогонами."
	case "network":
		if summary.HTTPCount > 0 {
			return "medium", fmt.Sprintf("В этом же прогоне записано %s; задержка верхней части выборки — %d мс.", russianCount(summary.HTTPCount, "сетевой вызов", "сетевых вызова", "сетевых вызовов"), summary.HTTPP95MS), "Сопоставьте пользовательские показатели сети с маршрутом и сценарием в разделе проблем."
		}
		return "low", "Основных сетевых событий рядом не записано.", "Проверьте, записывает ли сценарий маршрут и источник сетевой работы."
	case "ui":
		if summary.UIFrames > 0 {
			return "medium", fmt.Sprintf("Основной сборщик интерфейса увидел %.1f%% медленных кадров из %d.", summary.UIJankPct, summary.UIFrames), "Сверьте название показателя с экраном и причиной в разделе «Плавность интерфейса»."
		}
		return "low", "Основные события интерфейса в этом прогоне не записаны.", "Для связи с экраном повторите сценарий со включённым сбором кадров."
	case "tasks":
		if summary.StallCount > 0 {
			return "medium", fmt.Sprintf("В этом же прогоне главный поток останавливался %s; максимум — %d мс.", russianCount(summary.StallCount, "раз", "раза", "раз"), summary.StallMaxMS), "Ищите рост очереди рядом с длинными задачами главного потока."
		}
		return "low", "Длинные паузы главного потока в этом прогоне не записаны.", "Сравнивайте размер очереди в одинаковых сценариях и на одинаковом устройстве."
	case "io":
		if count := totalTypedIOOperations(summary); count > 0 {
			return "medium", fmt.Sprintf("Типизированных файловых операций в прогоне — %d.", count), "Сопоставьте рост показателя с операцией, потоком и источником в подробном анализе хранилища."
		}
		return "low", "Типизированные файловые операции в этом прогоне не записаны.", "Для точной привязки добавьте запись операции с потоком и источником."
	default:
		return "low", "Эти показатели пока не относятся к известной группе Jank Hunter.", "Добавьте понятное имя и описание единицы измерения, затем сравнивайте одинаковые сценарии."
	}
}

func topSignalContextDelta(baseline, candidate analyze.Summary) (signalContextCompareRow, bool) {
	rows := signalContextCompareRows(baseline, candidate)
	if len(rows) == 0 {
		return signalContextCompareRow{}, false
	}
	return rows[0], true
}

func networkLoopCardSeverity(confidence, burn float64) string {
	if confidence >= 0.70 && burn >= 8 {
		return "high"
	}
	if confidence >= 0.45 || burn >= 4 {
		return "medium"
	}
	return "ok"
}

func topIntegralScore(scores []mathanalysis.IntegralScore) (mathanalysis.IntegralScore, bool) {
	if len(scores) == 0 {
		return mathanalysis.IntegralScore{}, false
	}
	best := scores[0]
	for _, score := range scores[1:] {
		if severityRank(score.Severity) > severityRank(best.Severity) || (score.Severity == best.Severity && score.Value > best.Value) {
			best = score
		}
	}
	return best, true
}

func leakModeLabel(mode string) string {
	switch mode {
	case analyze.LeakModeHeap:
		return "режим дампа кучи"
	default:
		return "легкий режим"
	}
}

func leakDeltaStatusClass(status string) string {
	switch status {
	case analyze.LeakDeltaNew, analyze.LeakDeltaWorse, analyze.LeakDeltaRegressed:
		return "sev-high"
	case analyze.LeakDeltaBetter, analyze.LeakDeltaResolved, analyze.LeakDeltaImproved:
		return "sev-ok"
	default:
		return "sev-medium"
	}
}

func leakGraphSVG(namespace string, graph analyze.LeakGraph) template.HTML {
	if len(graph.Nodes) == 0 {
		return template.HTML(`<div class="leak-graph-empty">Нет данных для графа.</div>`)
	}
	const (
		nodeW         = 280.0
		nodeH         = 96.0
		gapX          = 190.0
		topY          = 60.0
		bandY         = 180.0
		leftX         = 32.0
		minH          = 340.0
		maxCols       = 3
		edgeInset     = 14.0
		retainedGapX  = 24.0
		retainedBandY = 112.0
	)
	mainNodes := make([]analyze.LeakGraphNode, 0, len(graph.Nodes))
	retainedNodes := make([]analyze.LeakGraphNode, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if node.Kind == "retained" {
			retainedNodes = append(retainedNodes, node)
			continue
		}
		mainNodes = append(mainNodes, node)
	}
	if len(mainNodes) == 0 {
		mainNodes = graph.Nodes
	}
	cols := len(mainNodes)
	if cols < 1 {
		cols = 1
	}
	if cols > maxCols {
		cols = maxCols
	}
	width := math.Max(1080, leftX*2+float64(cols)*nodeW+float64(cols-1)*gapX)
	mainRows := math.Ceil(float64(len(mainNodes)) / maxCols)
	if mainRows < 1 {
		mainRows = 1
	}
	retainedRows := math.Ceil(float64(len(retainedNodes)) / 3)
	height := math.Max(minH, topY+mainRows*bandY+nodeH+96+retainedRows*retainedBandY)
	positions := map[string]graphPoint{}
	var builder strings.Builder
	fmt.Fprintf(&builder, `<svg class="leak-graph-svg" viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">`, width, height, template.HTMLEscapeString(graph.Title))
	scope := graphClass(firstNonEmpty(namespace, "leak-graph"))
	identity := graphClass(firstNonEmpty(graph.TargetID, graph.RootID, graph.Title))
	arrowID := "leak-arrow-" + scope + "-" + identity
	gradientID := "leak-edge-gradient-" + scope + "-" + identity
	fmt.Fprintf(
		&builder,
		`<defs><linearGradient id="%s" x1="0" x2="1"><stop offset="0" stop-color="#6ff7ff"/><stop offset="1" stop-color="#ff4fd8"/></linearGradient><marker id="%s" markerUnits="userSpaceOnUse" markerWidth="12" markerHeight="12" refX="10" refY="6" orient="auto" overflow="visible"><path d="M0,0 L12,6 L0,12 Z" fill="#6ff7ff" opacity="0.88"/></marker></defs>`,
		template.HTMLEscapeString(gradientID),
		template.HTMLEscapeString(arrowID),
	)
	fmt.Fprintf(&builder, `<text x="32" y="30" class="leak-graph-title">%s</text>`, template.HTMLEscapeString(graph.Title))
	for index, node := range mainNodes {
		x := leftX + float64(index%maxCols)*(nodeW+gapX)
		y := topY + float64(index/maxCols)*bandY
		positions[node.ID] = graphPoint{x: x, y: y}
	}
	target := positions[graph.TargetID]
	if graph.TargetID == "" {
		target = positions[mainNodes[len(mainNodes)-1].ID]
	}
	for index, node := range retainedNodes {
		col := index % 3
		row := index / 3
		x := math.Min(width-nodeW-leftX, math.Max(leftX, target.x-150+float64(col)*(nodeW+retainedGapX)))
		y := target.y + nodeH + 76 + float64(row)*retainedBandY
		positions[node.ID] = graphPoint{x: x, y: y}
	}
	for _, edge := range graph.Edges {
		from, okFrom := positions[edge.From]
		to, okTo := positions[edge.To]
		if !okFrom || !okTo {
			continue
		}
		x1 := from.x + nodeW + edgeInset
		y1 := from.y + nodeH/2
		x2 := to.x - edgeInset
		y2 := to.y + nodeH/2
		if to.y > from.y+nodeH {
			x1 = from.x + nodeW/2
			y1 = from.y + nodeH + edgeInset
			x2 = to.x + nodeW/2
			y2 = to.y - edgeInset
		}
		if x2 < x1 && math.Abs(to.y-from.y) < nodeH {
			x1 = from.x - edgeInset
			x2 = to.x + nodeW + edgeInset
		}
		midX := (x1 + x2) / 2
		midY := (y1 + y2) / 2
		fmt.Fprintf(&builder, `<path class="leak-graph-edge edge-%s" style="stroke:url(#%s)" d="M%.1f %.1f C%.1f %.1f %.1f %.1f %.1f %.1f" marker-end="url(#%s)"/>`, graphClass(edge.Kind), template.HTMLEscapeString(gradientID), x1, y1, midX, y1, midX, y2, x2, y2, template.HTMLEscapeString(arrowID))
		if edge.Label != "" {
			label := template.HTMLEscapeString(edge.Label)
			shortLabel := shortGraphLabel(edge.Label, 24)
			labelX := midX
			labelY := math.Min(y1, y2) - 14
			anchor := "middle"
			if math.Abs(y2-y1) > nodeH/2 {
				labelX = midX + 20
				labelY = midY - 10
				anchor = "start"
			}
			labelWidth := math.Min(176, math.Max(48, float64(len([]rune(shortLabel)))*6.8+14))
			backdropX := labelX - labelWidth/2
			if anchor == "start" {
				backdropX = labelX - 7
			}
			fmt.Fprintf(
				&builder,
				`<rect x="%.1f" y="%.1f" width="%.1f" height="18" rx="5" class="leak-graph-edge-label-bg"/><text x="%.1f" y="%.1f" text-anchor="%s" class="leak-graph-edge-label" aria-label="%s"><title>%s</title>%s</text>`,
				backdropX,
				labelY-13,
				labelWidth,
				labelX,
				labelY,
				anchor,
				label,
				label,
				template.HTMLEscapeString(shortLabel),
			)
		}
	}
	for _, node := range graph.Nodes {
		point, ok := positions[node.ID]
		if !ok {
			continue
		}
		classes := "leak-graph-node node-" + graphClass(node.Kind)
		if node.ID == graph.TargetID {
			classes += " is-target"
		}
		if node.ID == graph.RootID {
			classes += " is-root"
		}
		tip := strings.TrimSpace(node.Label)
		if node.Detail != "" {
			tip += " · " + node.Detail
		}
		fmt.Fprintf(
			&builder,
			`<g class="%s" transform="translate(%.1f %.1f)" data-leak-node data-tip="%s" tabindex="0" role="button">`,
			classes,
			point.x,
			point.y,
			template.HTMLEscapeString(tip),
		)
		fmt.Fprintf(&builder, `<title>%s</title>`, template.HTMLEscapeString(tip))
		fmt.Fprintf(&builder, `<rect width="%.0f" height="%.0f" rx="8"/>`, nodeW, nodeH)
		textY := 24.0
		for _, line := range graphLabelLines(node.Label, 30, 2) {
			fmt.Fprintf(&builder, `<text x="14" y="%.0f" class="node-title">%s</text>`, textY, template.HTMLEscapeString(line))
			textY += 15
		}
		if node.Detail != "" {
			textY += 4
			for _, line := range graphLabelLines(node.Detail, 34, 2) {
				fmt.Fprintf(&builder, `<text x="14" y="%.0f" class="node-detail">%s</text>`, textY, template.HTMLEscapeString(line))
				textY += 14
			}
		}
		builder.WriteString(`</g>`)
	}
	builder.WriteString(`</svg>`)
	return template.HTML(builder.String())
}

func graphClass(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	if out.Len() == 0 {
		return "unknown"
	}
	return out.String()
}

func shortGraphLabel(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}

func graphLabelLines(value string, limit int, maxLines int) []string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" || limit <= 0 || maxLines <= 0 {
		return nil
	}
	runes := []rune(value)
	lines := make([]string, 0, maxLines)
	for len(runes) > 0 && len(lines) < maxLines {
		if len(runes) <= limit {
			lines = append(lines, string(runes))
			break
		}
		if len(lines) == maxLines-1 {
			lines = append(lines, shortGraphLabel(string(runes), limit))
			break
		}
		cut := graphLineBreak(runes, limit)
		line := strings.Trim(string(runes[:cut]), " ./#_$-→:")
		if line == "" {
			line = string(runes[:cut])
		}
		lines = append(lines, line)
		runes = []rune(strings.TrimLeft(string(runes[cut:]), " ./#_$-→:"))
	}
	return lines
}

func graphLineBreak(runes []rune, limit int) int {
	if len(runes) <= limit {
		return len(runes)
	}
	lower := limit / 2
	if lower < 8 {
		lower = 8
	}
	for i := limit; i >= lower; i-- {
		if graphBreakRune(runes[i-1]) {
			return i
		}
	}
	return limit
}

func graphBreakRune(r rune) bool {
	switch r {
	case '.', '/', '$', '#', '_', '-', ' ', '→', ':':
		return true
	default:
		return false
	}
}

func sparklineSVG(series mathanalysis.Series) template.HTML {
	const (
		width  = 360.0
		height = 86.0
		pad    = 5.0
	)
	if len(series.Points) == 0 {
		return template.HTML(`<svg class="sparkline" viewBox="0 0 360 86" role="img" aria-label="нет данных"></svg>`)
	}
	validPoints := make([]float64, 0, len(series.Points))
	for index, point := range series.Points {
		if seriesPointPresent(series, index) {
			validPoints = append(validPoints, point)
		}
	}
	if len(validPoints) == 0 {
		return template.HTML(`<svg class="sparkline" viewBox="0 0 360 86" role="img" aria-label="нет замеров"></svg>`)
	}
	maxValue := validPoints[0]
	minValue := validPoints[0]
	for _, point := range validPoints[1:] {
		if point < minValue {
			minValue = point
		}
		if point > maxValue {
			maxValue = point
		}
	}
	if maxValue == minValue {
		minValue = 0
	}
	if maxValue == minValue {
		maxValue = minValue + 1
	}
	scaleY := func(value float64) float64 {
		return height - pad - ((value - minValue) * (height - 2*pad) / (maxValue - minValue))
	}
	step := width - 2*pad
	if len(series.Points) > 1 {
		step = (width - 2*pad) / float64(len(series.Points)-1)
	}
	barWidth := step * 0.62
	if barWidth < 1.2 {
		barWidth = 1.2
	}
	if barWidth > 11 {
		barWidth = 11
	}

	var bars strings.Builder
	var lines []string
	var line strings.Builder
	flushLine := func() {
		if line.Len() > 0 {
			lines = append(lines, line.String())
			line.Reset()
		}
	}
	for i, point := range series.Points {
		if !seriesPointPresent(series, i) {
			flushLine()
			continue
		}
		x := pad
		if len(series.Points) > 1 {
			x += float64(i) * step
		}
		y := scaleY(point)
		barHeight := height - pad - y
		if barHeight < 1 && point > 0 {
			barHeight = 1
		}
		if point > 0 {
			fmt.Fprintf(&bars, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="1.4"></rect>`, x-barWidth/2, height-pad-barHeight, barWidth, barHeight)
		}
		if line.Len() > 0 {
			line.WriteByte(' ')
		}
		fmt.Fprintf(&line, "%.2f,%.2f", x, y)
	}
	flushLine()

	var out strings.Builder
	fmt.Fprintf(&out, `<svg class="sparkline" viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">`, width, height, template.HTMLEscapeString(series.Name))
	out.WriteString(`<line class="spark-axis" x1="5" y1="81" x2="355" y2="81"></line>`)
	out.WriteString(`<g class="spark-bars">`)
	out.WriteString(bars.String())
	out.WriteString(`</g><g class="spark-lines">`)
	for _, points := range lines {
		out.WriteString(`<polyline class="spark-line" points="`)
		out.WriteString(points)
		out.WriteString(`"></polyline>`)
	}
	out.WriteString(`</g></svg>`)
	return template.HTML(out.String())
}

func causalGraphSVG(graph mathanalysis.CausalGraph) template.HTML {
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		return template.HTML(`<div class="muted">Недостаточно узлов и связей для визуального графа.</div>`)
	}
	const (
		maxEdges = 48
		maxNodes = 30
		width    = 960.0
		height   = 460.0
	)
	edges := uniqueCausalEdges(graph.Edges)
	if len(edges) > maxEdges {
		edges = edges[:maxEdges]
	}
	nodeByID := map[string]mathanalysis.CausalNode{}
	for _, node := range graph.Nodes {
		nodeByID[node.ID] = node
	}
	used := map[string]struct{}{}
	for _, edge := range edges {
		used[edge.From] = struct{}{}
		used[edge.To] = struct{}{}
		if len(used) >= maxNodes {
			break
		}
	}
	var nodes []mathanalysis.CausalNode
	for id := range used {
		if node, ok := nodeByID[id]; ok {
			nodes = append(nodes, node)
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		if graphKindColumn(nodes[i].Kind) != graphKindColumn(nodes[j].Kind) {
			return graphKindColumn(nodes[i].Kind) < graphKindColumn(nodes[j].Kind)
		}
		return nodes[i].Label < nodes[j].Label
	})
	position := layoutCausalNodes(nodes, width, height)
	var out strings.Builder
	out.WriteString(`<div class="causal-graph-card">`)
	out.WriteString(`<svg class="causal-graph" viewBox="0 0 960 460" role="img" aria-label="Обзор статистических связей">`)
	for _, edge := range edges {
		from, okFrom := position[edge.From]
		to, okTo := position[edge.To]
		if !okFrom || !okTo {
			continue
		}
		opacity := 0.28 + clampPct(edge.Confidence*100)/140
		fmt.Fprintf(&out, `<line class="causal-edge" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" opacity="%.2f"><title>%s ↔ %s · %s · уверенность %.2f</title></line>`,
			from.x+112, from.y+24, to.x, to.y+24, opacity,
			template.HTMLEscapeString(edge.FromLabel),
			template.HTMLEscapeString(edge.ToLabel),
			template.HTMLEscapeString(mathanalysis.CausalKindLabel(edge.Kind)),
			edge.Confidence,
		)
	}
	for _, node := range nodes {
		pos := position[node.ID]
		label := truncateRunes(node.Label, 28)
		kind := mathanalysis.CausalKindLabel(node.Kind)
		fmt.Fprintf(&out, `<g class="causal-node" transform="translate(%.1f %.1f)"><title>%s · %s</title><rect width="124" height="48"></rect><text x="10" y="19">%s</text><text class="kind" x="10" y="36">%s</text></g>`,
			pos.x, pos.y,
			template.HTMLEscapeString(node.Label),
			template.HTMLEscapeString(kind),
			template.HTMLEscapeString(label),
			template.HTMLEscapeString(truncateRunes(kind, 22)),
		)
	}
	out.WriteString(`</svg>`)
	allEdges := uniqueCausalEdges(graph.Edges)
	if len(allEdges) > len(edges) || len(graph.Nodes) > len(nodes) {
		fmt.Fprintf(&out, `<div class="help-text">Показаны самые сильные статистические связи: %d из %d и %d из %d узлов. Обратные дубликаты скрыты.</div>`, len(edges), len(allEdges), len(nodes), len(graph.Nodes))
	}
	out.WriteString(`</div>`)
	return template.HTML(out.String())
}

func uniqueCausalEdges(edges []mathanalysis.CausalEdge) []mathanalysis.CausalEdge {
	seen := make(map[string]struct{}, len(edges))
	unique := make([]mathanalysis.CausalEdge, 0, len(edges)/2+1)
	for _, edge := range edges {
		left := edge.From
		right := edge.To
		if left > right {
			left, right = right, left
		}
		key := left + "\x00" + right + "\x00" + edge.Kind
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, edge)
	}
	return unique
}

func uniqueCausalPaths(paths []mathanalysis.GraphPath) []mathanalysis.GraphPath {
	seen := make(map[string]struct{}, len(paths))
	unique := make([]mathanalysis.GraphPath, 0, len(paths))
	for _, path := range paths {
		left := path.From
		right := path.To
		if left > right {
			left, right = right, left
		}
		key := left + "\x00" + right
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, path)
	}
	return unique
}

func influenceStatusLabel(value string) string {
	switch value {
	case "runtime":
		return "есть данные выполнения"
	case "static_only":
		return "только статические данные"
	default:
		return "нет данных"
	}
}

func influenceEvidenceLabel(value string) string {
	switch value {
	case "runtime":
		return "выполнение"
	case "mixed":
		return "выполнение + статика"
	case "static":
		return "только статика"
	default:
		return "нет данных"
	}
}

func influenceRoleLabel(value string) string {
	switch value {
	case "caller":
		return "вызывающий"
	case "callee":
		return "вызываемый"
	default:
		return value
	}
}

func influenceSeverityLabel(value string) string {
	switch value {
	case "high":
		return "высокий риск"
	case "medium":
		return "средний риск"
	default:
		return "низкий риск"
	}
}

func topInfluenceNodes(influence analyze.InfluenceSummary, limit int) []analyze.InfluenceNode {
	if limit <= 0 || len(influence.TopNodes) <= limit {
		return influence.TopNodes
	}
	return influence.TopNodes[:limit]
}

type graphPoint struct {
	x float64
	y float64
}

func layoutCausalNodes(nodes []mathanalysis.CausalNode, width, height float64) map[string]graphPoint {
	byColumn := map[int][]mathanalysis.CausalNode{}
	for _, node := range nodes {
		col := graphKindColumn(node.Kind)
		byColumn[col] = append(byColumn[col], node)
	}
	xs := []float64{32, 196, 360, 524, 688, 804}
	positions := map[string]graphPoint{}
	for col, items := range byColumn {
		x := xs[len(xs)-1]
		if col >= 0 && col < len(xs) {
			x = xs[col]
		}
		step := (height - 84) / float64(len(items)+1)
		for i, node := range items {
			positions[node.ID] = graphPoint{x: x, y: 28 + step*float64(i+1)}
		}
	}
	return positions
}

func graphKindColumn(kind string) int {
	switch kind {
	case "state", "symptom":
		return 0
	case "network", "phase":
		return 1
	case "loop":
		return 2
	case "route":
		return 3
	case "owner":
		return 4
	case "screen":
		return 5
	default:
		return 2
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func seriesMax(series mathanalysis.Series) float64 {
	var maxValue float64
	found := false
	for index, point := range series.Points {
		if !seriesPointPresent(series, index) {
			continue
		}
		if !found || point > maxValue {
			maxValue = point
			found = true
		}
	}
	return maxValue
}

func seriesLast(series mathanalysis.Series) float64 {
	for index := len(series.Points) - 1; index >= 0; index-- {
		if seriesPointPresent(series, index) {
			return series.Points[index]
		}
	}
	return 0
}

func seriesPointPresent(series mathanalysis.Series, index int) bool {
	return index >= 0 && index < len(series.Points) && (len(series.Present) != len(series.Points) || series.Present[index])
}

func reportLanguage() string {
	return "ru"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
