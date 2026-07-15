package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteBundleEmbedsPagesAndNavigationBridge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	err := WriteBundle(path, []BundlePage{
		{
			ID:    "overview",
			Title: "Обзор",
			Href:  "report.html",
			HTML:  []byte(`<!doctype html><html><body><a href="#network">Сеть</a><a href="report-math.html">Математика</a><section id="network">Сеть</section></body></html>`),
		},
		{
			ID:    "math",
			Title: "Математический анализ",
			Href:  "report-math.html",
			HTML:  []byte(`<!doctype html><html><body><a href="report.html">Обзор</a></body></html>`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, marker := range []string{
		`data-jankhunter-single-html`,
		`id="jankhunter-report-pages"`,
		`"id":"overview"`,
		`"id":"math"`,
		`jankhunter-report:navigate`,
		`scrollToFragment`,
		`#page=`,
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("bundle does not contain %q", marker)
		}
	}
	if strings.Count(html, bundledPageBridge) != 0 {
		t.Fatal("embedded bridge must be JSON escaped inside the bundle payload")
	}

	payloadStart := strings.Index(html, `type="application/json">`)
	if payloadStart < 0 {
		t.Fatal("bundle payload script not found")
	}
	payloadStart += len(`type="application/json">`)
	payloadEnd := strings.Index(html[payloadStart:], `</script>`)
	if payloadEnd < 0 {
		t.Fatal("bundle payload closing script not found")
	}
	payloadEnd += payloadStart
	var pages []encodedBundlePage
	if err := json.Unmarshal([]byte(html[payloadStart:payloadEnd]), &pages); err != nil {
		t.Fatalf("decode embedded pages: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("embedded pages = %d, want 2", len(pages))
	}
	if !strings.Contains(pages[0].HTML, bundledPageBridge) || !strings.Contains(pages[1].HTML, bundledPageBridge) {
		t.Fatal("navigation bridge is not injected into every embedded page")
	}
}

func TestWriteBundleRejectsDuplicatePageIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.html")
	err := WriteBundle(path, []BundlePage{
		{ID: "overview", Title: "Обзор", Href: "report.html", HTML: []byte("first")},
		{ID: "overview", Title: "Еще обзор", Href: "other.html", HTML: []byte("second")},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate report bundle page id") {
		t.Fatalf("WriteBundle error = %v, want duplicate id error", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("invalid bundle output exists: %v", statErr)
	}
}
