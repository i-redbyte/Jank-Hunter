package report

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var unclearReportCopy = []string{
	"автономн",
	"атрибуц",
	"атрибутир",
	"боляч",
	"вердикт",
	"дёрг",
	"деградац",
	"доверие",
	"достоверност",
	"доминатор",
	"агрегат",
	"агрегир",
	"агрегац",
	"инцидент",
	"инструментир",
	"кандидат",
	"кардинальн",
	"квантил",
	"когорт",
	"липк",
	"локализ",
	"находк",
	"перехватчик",
	"ретеншн",
	"робаст",
	"рывк",
	"серьезност",
	"серьёзност",
	"сильной связност",
	"телеметр",
	"уверенн",
	"экспозиц",
	"эвристическ",
	"эврист",
	"build-time",
	"cpu time",
	"gauge-метрик",
	"manifest declaration",
	"privacy boundary",
	"process roster",
	"quality vector",
	"runtime-реестр",
	"runtime-событ",
	"scope процессов",
	"wiring",
	"typed outcome",
	"доступа к payload",
	"method counters",
	"writer",
	"runtime-граф",
	"process scope",
	"process allowlist",
	"quality snapshot",
	"producer page",
	"producer thread",
	"admission lock",
	"backpressure",
	"data records",
	"committed chunks",
	"run cohort",
	"final seal",
	"identity сегмент",
	"storage_budget",
	"элементов evidence",
	"evidence противоречат",
	"retained-событ",
	"retained/hprof",
	"runtime-сигнал",
	"уровень evidence",
	"fail-open границ",
	"с включенным instrumentation",
	"asm-hooks",
	"сбои hooks",
	"какие hooks",
	" hooks",
	"hook ",
	"capture ",
	" capture",
	"callsite",
	"runtime/hprof",
	"runtime overhead",
	"runtime crash",
	"runtime подтверж",
	"static ",
	"bytecode",
	"state machine",
	"suspension point",
	"process-wide",
	"scope mismatch",
	"external queue",
	"heap path",
	"retained-под",
	"словарь/control",
	"db telemetry",
	"runtime coverage",
	"scenario rate",
	" scopes",
	"bounded scenario",
	"normalized fingerprint",
	"hypothesis",
	"latency",
	"thread-specific",
	"instrument matcher",
	"session-событ",
	"device snapshot",
}

func TestReportTemplatesUseJuniorFriendlyCopy(t *testing.T) {
	for name, source := range reportCopySources() {
		assertClearReportCopy(t, name, source)
	}
}

func reportCopySources() map[string]string {
	return map[string]string{
		"components":           sharedComponentsTemplate,
		"inspect":              inspectTemplate,
		"compare":              compareTemplate,
		"math-inspect":         mathInspectTemplate,
		"math-compare":         mathCompareTemplate,
		"leaks-inspect":        leaksInspectTemplate,
		"leaks-compare":        leaksCompareTemplate,
		"influence":            influenceTemplate,
		"diagnostics":          diagnosticsTemplate,
		"dependency-injection": dependencyInjectionTemplate,
		"report-js":            reportJS,
		"problem-search-js":    problemSearchJS,
		"log-growth-js":        logGrowthJS,
	}
}

func TestReportPresentationStringsUseJuniorFriendlyCopy(t *testing.T) {
	patterns := []string{"*.go", "../analyze/*.go", "../mathanalysis/*.go", "../../cmd/jankhunter/report_sets.go"}
	var files []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && containsCyrillic(value) {
				assertClearReportCopy(t, path, value)
			}
			return true
		})
	}
}

func TestGeneratedReportCopyUsesShortHyphens(t *testing.T) {
	for name, source := range reportCopySources() {
		if strings.ContainsAny(source, "\u2014\u2013") {
			t.Errorf("%s contains a long dash", name)
		}
	}

	patterns := []string{"*.go", "../analyze/*.go", "../mathanalysis/*.go", "../../cmd/jankhunter/report_sets.go"}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range matches {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err == nil && strings.ContainsAny(value, "\u2014\u2013") {
					t.Errorf("%s contains a long dash in generated copy: %q", path, value)
				}
				return true
			})
		}
	}
}

func containsCyrillic(value string) bool {
	for _, char := range value {
		if char >= '\u0400' && char <= '\u04ff' {
			return true
		}
	}
	return false
}

func assertClearReportCopy(t *testing.T, sourceName, source string) {
	t.Helper()
	normalized := strings.ToLower(source)
	for _, forbidden := range unclearReportCopy {
		if strings.Contains(normalized, forbidden) {
			t.Errorf("%s contains unclear report copy %q", sourceName, forbidden)
		}
	}
}

func TestMainReportCopyLeadsFromProblemToAction(t *testing.T) {
	for _, required := range []string{
		"Что произошло",
		"Почему это важно",
		"Где искать",
		"Что проверить",
		"Как проверить исправление",
	} {
		if !strings.Contains(sharedComponentsTemplate+inspectTemplate, required) {
			t.Errorf("problem-oriented report is missing %q", required)
		}
	}
}

func TestLeakEvidenceCellAllowsLongTextToWrap(t *testing.T) {
	assertCSSBlockContains(t, modernCSS, `.leak-card-table .leak-card-row > td {`, "white-space: normal")
	assertCSSBlockContains(t, modernCSS, `.leak-card-table .leak-card-row > td[data-leak-field="detail"] {`, "white-space: normal")
}

func TestLeakReportsHideInternalEvidenceCodes(t *testing.T) {
	for name, source := range map[string]string{
		"inspect":       inspectTemplate,
		"compare":       compareTemplate,
		"math-compare":  mathCompareTemplate,
		"leaks-inspect": leaksInspectTemplate,
		"leaks-compare": leaksCompareTemplate,
	} {
		if strings.Contains(source, ".EvidenceKind}}") {
			t.Errorf("%s exposes an internal retention evidence code", name)
		}
	}
}

func TestLogSummaryUsesReadableRecordLabels(t *testing.T) {
	if strings.Contains(compareTemplate, "}} control") {
		t.Fatal("compare report exposes the internal control record name")
	}
}

func assertCSSBlockContains(t *testing.T, stylesheet, selector, declaration string) {
	t.Helper()
	start := strings.Index(stylesheet, selector)
	if start < 0 {
		t.Fatalf("modern report CSS does not define %s", selector)
	}
	end := strings.Index(stylesheet[start:], "}")
	if end < 0 || !strings.Contains(stylesheet[start:start+end], declaration) {
		t.Fatalf("CSS block %s does not contain %q", selector, declaration)
	}
}
