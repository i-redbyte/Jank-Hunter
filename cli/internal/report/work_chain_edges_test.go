package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestInspectShowsDeclaredWorkChainScopeWithoutClaimingEnqueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chain.html")
	summary := analyze.Summary{WorkChainEdges: &analyze.WorkChainAnalysis{CapturedEdges: 1}}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"Наблюдённые зависимости цепочек", "не подтверждает постановку", "data-work-chain"} {
		if !strings.Contains(string(data), fragment) {
			t.Fatalf("missing %q", fragment)
		}
	}
}

func TestCompareKeepsAbsentWorkChainSourceUnknown(t *testing.T) {
	comparison := analyze.Compare(analyze.Summary{}, analyze.Summary{
		WorkChainEdges: &analyze.WorkChainAnalysis{CapturedEdges: 3, CapturedRoots: 2},
	})
	path := filepath.Join(t.TempDir(), "chain-compare.html")
	if err := WriteCompareReportWithOptions(path, comparison, nil, nil, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"Зависимости цепочек", "база: неизвестно", "проверяемый прогон: наблюдений рёбер: 3", "корней: 2", "UUID не сопоставляются"} {
		if !strings.Contains(string(data), fragment) {
			t.Fatalf("missing %q", fragment)
		}
	}
}
