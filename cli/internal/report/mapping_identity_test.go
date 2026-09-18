package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestInspectLabelsUnverifiedRestoredNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	summary := analyze.Summary{Title: "mapping", MappingIdentity: analyze.MappingIdentityEvidence{Status: analyze.MappingUnverified, Applied: true, SHA256: strings.Repeat("a", 64)}}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "непроверен") || strings.Contains(string(data), "Соответствие mapping журналу не подтверждено") {
		t.Fatal("unverified restored names look verified in HTML")
	}
	mathPath := filepath.Join(t.TempDir(), "math.html")
	if err := WriteMathInspectWithOptions(mathPath, sampleMathReport(summary), ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	mathData, err := os.ReadFile(mathPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mathData), "Соответствие mapping журналу не подтверждено") {
		t.Fatal("mapping diagnostic is absent from mathematical analysis")
	}
}

func TestVerifiedMappingDoesNotClaimAllSymbolNamesVerified(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	summary := analyze.Summary{Title: "mapping", MappingIdentity: analyze.MappingIdentityEvidence{Status: analyze.MappingVerified, Applied: true, UnknownOriginReferences: 1}}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Mapping <strong>подтверждён") || strings.Contains(string(data), "Имена <strong>проверены") {
		t.Fatal("build identity misrepresented as symbol provenance")
	}
}
