package report

import (
	"strings"
	"testing"
)

func TestReportStylesheetContainsCurrentTheme(t *testing.T) {
	stylesheet := reportStylesheet(true)
	for _, marker := range []string{
		"--forest: #006400",
		"--attention: #FF4500",
		"--paper: #FFFFE0",
		"--critical: #8B0000",
		"--danger: #DC143C",
		"--medium: #FF8A3D",
		"--low: #F4D35E",
		"--locator: #5BC0EB",
		`--font-ui: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Roboto, Arial, sans-serif`,
		`--font-code: "SFMono-Regular", "Cascadia Mono", "Roboto Mono", "Liberation Mono", Menlo, Consolas, monospace`,
		`font-variant-ligatures: none`,
		"padding: 8px !important",
		"--space-phi-3: 1.309rem",
		"grid-template-columns: minmax(0, 1.618fr) minmax(280px, 1fr)",
		"font-size: clamp(30px, 3.4vw, 48px)",
		"overflow-wrap: anywhere",
		"@media (prefers-reduced-motion: reduce)",
		".table-cell-clip",
		".data-ticket",
		"--data-ticket-label",
		"Как получена оценка",
	} {
		if !strings.Contains(stylesheet, marker) {
			t.Fatalf("stylesheet does not contain %q", marker)
		}
	}
	for _, forbidden := range []string{
		"#4B0082", "#4b0082", "#a98bc0", "#c084fc",
		"rgba(75, 0, 130", "rgba(167,139,250", "rgba(192,132,252",
		"rgba(217,70,239", "rgba(126,34,206", "rgba(88,28,135",
	} {
		if strings.Contains(stylesheet, forbidden) {
			t.Fatalf("stylesheet still contains removed purple color %q", forbidden)
		}
	}
	assertCSSBlockContains(t, modernCSS, `.nav a.active {`, "border-inline: 3px solid var(--low)")
	assertCSSBlockContains(t, modernCSS, `.problem-location-row {`, "border-inline-start: 4px solid var(--locator)")
	assertCSSBlockContains(t, modernCSS, `.problem-verdict-counts > .sev-medium {`, "border-top-color: var(--medium)")
	assertCSSBlockContains(t, modernCSS, `.problem-verdict-counts > .sev-low {`, "border-top-color: var(--low)")
	assertCSSBlockContains(t, modernCSS, `.problem-score.sev-medium {`, "color: var(--medium)")
	assertCSSBlockContains(t, modernCSS, `.problem-score.sev-low {`, "color: var(--low)")
	assertCSSBlockContains(t, modernCSS, `.problem-impact p,`, "color: #f7f2dc !important")
	if !strings.Contains(stylesheet, compactStylesheet(mathCSS)) {
		t.Fatal("stylesheet is missing math CSS")
	}
}

func TestCompactStylesheetPreservesStringsAndSelectorBoundaries(t *testing.T) {
	stylesheet := compactStylesheet("  .first,\n  .second {\n    content: \"two words\";\n    color: red;\n  }\n")
	want := `.first,.second {content: "two words";color: red;}`
	if stylesheet != want {
		t.Fatalf("compactStylesheet() = %q, want %q", stylesheet, want)
	}
}

func assertCurrentReportStyle(t *testing.T, path string) {
	t.Helper()
	assertHTMLContains(
		t,
		path,
		"--forest: #006400",
		"--paper: #FFFFE0",
		`--font-ui: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans", Roboto, Arial, sans-serif`,
		"padding: 8px !important",
	)
	assertHTMLNotContains(t, path, "data-report-style", "modernReport")
}

func TestReportScriptProvidesAccessibleHelpAndStableSectionNavigation(t *testing.T) {
	for _, marker := range []string{
		"const reportTermHelp = new Map",
		"const enhanceReportHelp =",
		"querySelectorAll('th')",
		"header.dataset.tip =",
		"tooltip.setAttribute('role', 'tooltip')",
		"describedBy.push(tooltip.id)",
		"const revealHashTarget =",
		"details.open = true",
		"window.addEventListener('hashchange'",
	} {
		if !strings.Contains(reportJS, marker) {
			t.Fatalf("report script does not contain %q", marker)
		}
	}
}

func TestLeakExplorerKeepsStablePositionBeforeDeferredRegistry(t *testing.T) {
	if strings.Contains(reportJS, "insertBefore(registry, explorer)") {
		t.Fatal("leak registry is moved before the explorer and shifts the graph target while deferred rows are rendered")
	}
}

func TestReportScriptFallbackIdleBudgetExpiresWhileChunkRuns(t *testing.T) {
	for _, marker := range []string{
		"const idleStartedAt = performance.now()",
		"Math.max(0, 8 - (performance.now() - idleStartedAt))",
	} {
		if !strings.Contains(reportJS, marker) {
			t.Fatalf("report script does not contain %q", marker)
		}
	}
	if strings.Contains(reportJS, "timeRemaining: () => 8") {
		t.Fatal("fallback idle budget never expires and can block the page on large reports")
	}
}

func TestArchivedCodeSearchReusesNormalizedIndex(t *testing.T) {
	for _, marker := range []string{
		"const normalizeCodeEvidenceSearch =",
		"normalized: true",
	} {
		if !strings.Contains(reportJS, marker) {
			t.Fatalf("report script does not contain %q", marker)
		}
	}
	if strings.Contains(reportJS, "JSON.stringify(record).toLowerCase()") {
		t.Fatal("archived evidence keeps an extra, incompletely normalized search string")
	}
	if !strings.Contains(problemSearchJS, "entry.normalized ? entry.text : normalize(entry.text)") {
		t.Fatal("global search renormalizes the archived evidence index")
	}
}

func TestInfluenceGraphUsesLinearTimeQueues(t *testing.T) {
	if strings.Contains(influenceTemplate, "queue.shift()") {
		t.Fatal("influence graph breadth-first traversal shifts the whole queue on every node")
	}
	if strings.Count(influenceTemplate, "let queueHead = 0") < 2 {
		t.Fatal("influence graph breadth-first traversals must use queue cursors")
	}
}

func TestDeferredRowsAreNotRetainedInASecondArchiveArray(t *testing.T) {
	for _, forbidden := range []string{
		"const added = [];\n      const step",
		"resolve(added);",
	} {
		if strings.Contains(reportJS, forbidden) {
			t.Fatalf("deferred row materialization retains every inserted DOM row via %q", forbidden)
		}
	}
	if !strings.Contains(reportJS, "materializeDeferredChunk(tbody, true)") {
		t.Fatal("deferred row materialization must notify indexes incrementally")
	}
}

func TestProblemSearchReleasesMaterializedDeferredScripts(t *testing.T) {
	for _, marker := range []string{
		"deferredEntry.deferredScript = null;",
		"deferredEntry.deferredBody = null;",
		"deferredEntries.delete(searchID);",
		"deferredScripts.length = 0;",
	} {
		if !strings.Contains(problemSearchJS, marker) {
			t.Fatalf("problem search retains materialized deferred markup: missing %q", marker)
		}
	}
}

func TestCodeRegistryKeepsOneLiveRowIndex(t *testing.T) {
	for _, forbidden := range []string{
		"let rows = Array.from(registry.querySelectorAll('[data-code-problem-row]'));",
		"let sortedRows = rows.slice();",
		"rows = rows.concat(additions);",
	} {
		if strings.Contains(reportJS, forbidden) {
			t.Fatalf("code registry retains a redundant row array via %q", forbidden)
		}
	}
}

func TestDeferredReportLoadsCanRecoverAfterFailure(t *testing.T) {
	for _, marker := range []string{
		"deferredLoads.delete(tbody)",
		"button.disabled = false",
		"codeEvidenceArchives.delete(registry)",
		"void applyArchive().catch(reportCodeArchiveFailure)",
	} {
		if !strings.Contains(reportJS, marker) {
			t.Fatalf("report script cannot recover after an asynchronous load failure: missing %q", marker)
		}
	}
}
