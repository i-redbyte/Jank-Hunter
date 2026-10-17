package report

import (
	"encoding/json"
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"html/template"
)

func workRegistryJSON(a *analyze.WorkRegistryAnalysis) (template.JS, error) {
	data, err := json.Marshal(a)
	return template.JS(data), err // encoding/json escapes user-controlled tags, URIs and Data keys.
}
