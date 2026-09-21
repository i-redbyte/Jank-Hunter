package report

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func WriteAgentInspectWithOptions(path string, summary analyze.Summary, options ReportOptions) error {
	return execute(path, cachedAgentInspectTemplate, map[string]any{
		"GeneratedAt":        options.generatedAt(),
		"Summary":            summary,
		"MainReportHref":     options.Links.Main,
		"PresentationMode":   options.PresentationMode,
		"AnimatedBackground": options.AnimatedBackground,
	}, options.TransientOutput)
}

func WriteAgentCompareWithOptions(path string, comparison analyze.Comparison, options ReportOptions) error {
	return execute(path, cachedAgentCompareTemplate, map[string]any{
		"GeneratedAt":        options.generatedAt(),
		"Comparison":         comparison,
		"MainReportHref":     options.Links.Main,
		"PresentationMode":   options.PresentationMode,
		"AnimatedBackground": options.AnimatedBackground,
	}, options.TransientOutput)
}
