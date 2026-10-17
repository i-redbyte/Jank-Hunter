package report

import (
	"encoding/json"
	"fmt"
	"html/template"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func workChainEdgesJSON(a *analyze.WorkChainAnalysis) (template.JS, error) {
	data, err := json.Marshal(a)
	return template.JS(data), err
}

func workChainCount(a *analyze.WorkChainAnalysis) string {
	if a == nil {
		return "неизвестно"
	}
	return fmt.Sprintf("наблюдений рёбер: %d; корней: %d; пропущено анализатором рёбер: %d, корней: %d",
		a.CapturedEdges, a.CapturedRoots, a.DroppedEdges, a.DroppedRoots)
}
