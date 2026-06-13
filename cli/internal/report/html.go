package report

import (
	"fmt"
	"html/template"
	"math"
	"os"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type LogReport struct {
	Name    string
	Anchor  string
	Summary analyze.Summary
}

func WriteInspect(path string, summary analyze.Summary) error {
	return execute(path, inspectTemplate, map[string]any{
		"GeneratedAt": time.Now().Format(time.RFC3339),
		"Summary":     summary,
	})
}

func WriteCompare(path string, comparison analyze.Comparison) error {
	return WriteCompareReport(path, comparison, nil, nil)
}

func WriteCompareReport(path string, comparison analyze.Comparison, baselineLogs, candidateLogs []LogReport) error {
	return execute(path, compareTemplate, map[string]any{
		"GeneratedAt":   time.Now().Format(time.RFC3339),
		"Comparison":    comparison,
		"BaselineLogs":  baselineLogs,
		"CandidateLogs": candidateLogs,
	})
}

func execute(path, source string, data any) error {
	tmpl, err := template.New("report").Funcs(template.FuncMap{
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
		"ringStyle": func(value float64) template.CSS {
			return template.CSS(fmt.Sprintf("--value:%.2f", clampPct(value)))
		},
		"rate": func(part int, total int) float64 {
			if total <= 0 {
				return 0
			}
			return float64(part) * 100 / float64(total)
		},
		"fpsScore": func(value float64) float64 {
			return clampPct(value * 100 / 60)
		},
		"severityClass": func(value string) string {
			switch value {
			case "high":
				return "sev-high"
			case "medium":
				return "sev-medium"
			default:
				return "sev-ok"
			}
		},
		"notOK": func(value string) bool {
			return value != "" && value != "ok"
		},
	}).Parse(source)
	if err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return tmpl.Execute(file, data)
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
