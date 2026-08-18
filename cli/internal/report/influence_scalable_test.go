package report_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

func TestInfluenceReportContainsScalableNavigationAndEvidenceModel(t *testing.T) {
	influence := buildReportInfluenceFixture()
	path := filepath.Join(t.TempDir(), "influence.html")
	if err := report.WriteInfluenceWithOptions(path, influence, "Граф влияния", report.ReportOptions{}); err != nil {
		t.Fatalf("WriteInfluenceWithOptions() error = %v", err)
	}
	html := readInfluenceHTML(t, path)
	for _, expected := range []string{
		`data-influence-view="problems"`,
		`data-influence-view="runtime"`,
		`data-influence-view="packages"`,
		`data-influence-view="neighborhood"`,
		`data-influence-view="context"`,
		`data-influence-search`,
		`data-influence-package-depth`,
		`data-influence-direction`,
		`data-influence-depth`,
		`data-influence-runtime-only`,
		`data-influence-center`,
		`data-influence-detail`,
		`data-influence-legend`,
		`history.replaceState`,
		`URLSearchParams`,
		`buildNeighborhood`,
		`expandPackage`,
		`fitSVGText`,
		`data-ticket-label="Как получена оценка"`,
		`prefers-reduced-motion`,
		`Показано`,
		`Исключено`,
		`RuntimeCount`,
		`StaticCount`,
		`"ID":"packages:3"`,
		`"Kind":"connector"`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("influence report does not contain %q", expected)
		}
	}
}

func TestInfluenceReportStaticDOMIDsAreUnique(t *testing.T) {
	path := filepath.Join(t.TempDir(), "influence.html")
	if err := report.WriteInfluenceWithOptions(path, buildReportInfluenceFixture(), "Граф влияния", report.ReportOptions{}); err != nil {
		t.Fatalf("WriteInfluenceWithOptions() error = %v", err)
	}
	html := readInfluenceHTML(t, path)
	matches := regexp.MustCompile(`\bid="([^"]+)"`).FindAllStringSubmatch(html, -1)
	seen := map[string]struct{}{}
	for _, match := range matches {
		if _, duplicate := seen[match[1]]; duplicate {
			t.Fatalf("duplicate DOM id %q", match[1])
		}
		seen[match[1]] = struct{}{}
	}
}

func TestInfluenceReportHandlesEmptyGraph(t *testing.T) {
	emptyPath := filepath.Join(t.TempDir(), "empty-graph.html")
	if err := report.WriteInfluenceWithOptions(emptyPath, analyze.InfluenceSummary{}, "Пустой граф", report.ReportOptions{}); err != nil {
		t.Fatalf("WriteInfluenceWithOptions(empty) error = %v", err)
	}
	emptyHTML := readInfluenceHTML(t, emptyPath)
	if !strings.Contains(emptyHTML, "По текущим фильтрам узлы не найдены") || !strings.Contains(emptyHTML, `"TotalNodes":0`) {
		t.Fatal("empty graph state is missing")
	}
}

func TestLargeInfluenceReportUsesBoundedStandalonePayload(t *testing.T) {
	started := time.Now()
	influence := buildLargeReportInfluence(8_000)
	analysisDuration := time.Since(started)
	path := os.Getenv("JH_LARGE_INFLUENCE_OUT")
	if path == "" {
		path = filepath.Join(t.TempDir(), "large-influence.html")
	}
	writeStarted := time.Now()
	if err := report.WriteInfluenceWithOptions(path, influence, "Большой граф влияния", report.ReportOptions{}); err != nil {
		t.Fatalf("WriteInfluenceWithOptions() error = %v", err)
	}
	writeDuration := time.Since(writeStarted)
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat large influence report: %v", err)
	}
	if stat.Size() > 12*1024*1024 {
		t.Fatalf("large standalone influence report = %d bytes, want <= 12 MiB", stat.Size())
	}
	if influence.Workspace.ShownNodes >= influence.Workspace.TotalNodes || influence.Workspace.OmittedNodes == 0 {
		t.Fatalf("large workspace is not bounded: %+v", influence.Workspace)
	}
	t.Logf("large influence analysis=%s write=%s html=%d bytes nodes=%d/%d edges=%d/%d", analysisDuration, writeDuration, stat.Size(), influence.Workspace.ShownNodes, influence.Workspace.TotalNodes, influence.Workspace.ShownEdges, influence.Workspace.TotalEdges)
}

func buildReportInfluenceFixture() analyze.InfluenceSummary {
	summary := analyze.Summary{
		ProblemWindows: []analyze.ProblemWindowStats{
			{Owner: "com.app.checkout.Presenter.render", Screen: "Checkout", Flow: "checkout.pay", Kind: "ui_jank", Count: 2, MaxMS: 70},
			{Owner: "com.app.checkout.Repository.load", Screen: "Checkout", Flow: "checkout.pay", Kind: "http_slow_or_failed", Count: 2, MaxMS: 900},
		},
		RuntimeCalls: []analyze.RuntimeCallStats{{
			Caller:  "com.app.checkout.Presenter.submit",
			Callee:  "com.app.checkout.Repository.load",
			Screen:  "Checkout",
			Flow:    "checkout.pay",
			Count:   3,
			TotalMS: 720,
			MaxMS:   280,
		}},
		Flows: []analyze.FlowStats{
			{Owner: "com.app.checkout.Presenter.render", Screen: "Checkout", Flow: "checkout.pay", RouteSample: "POST /checkout", UIJank: 2},
			{Owner: "com.app.checkout.Repository.load", Screen: "Checkout", Flow: "checkout.pay", RouteSample: "POST /checkout", HTTPP95MS: 900},
		},
	}
	graph := &analyze.ClassGraph{
		Classes: map[string]analyze.ClassGraphClass{},
		Edges: []analyze.ClassGraphEdge{
			{From: "com.app.checkout.Presenter", To: "com.app.shared.Connector", Count: 2},
			{From: "com.app.shared.Connector", To: "com.app.checkout.Repository", Count: 2},
			{From: "com.app.checkout.Repository", To: "com.app.network.Api", Count: 3},
			{From: "com.app.static.Only", To: "com.app.network.Api", Count: 1},
		},
	}
	return analyze.BuildInfluence(summary, graph)
}

func buildLargeReportInfluence(nodeCount int) analyze.InfluenceSummary {
	graph := &analyze.ClassGraph{Classes: map[string]analyze.ClassGraphClass{}}
	for index := 0; index < nodeCount; index++ {
		name := fmt.Sprintf("com.synthetic.feature%05d.subsystem.ClassWithLongProductionName%05d", index, index)
		graph.Classes[name] = analyze.ClassGraphClass{Name: name}
		for step := 1; step <= 3 && index+step < nodeCount; step++ {
			graph.Edges = append(graph.Edges, analyze.ClassGraphEdge{
				From:  name,
				To:    fmt.Sprintf("com.synthetic.feature%05d.subsystem.ClassWithLongProductionName%05d", index+step, index+step),
				Count: uint64(4 - step),
			})
		}
	}
	return analyze.BuildInfluence(analyze.Summary{}, graph)
}

func readInfluenceHTML(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
