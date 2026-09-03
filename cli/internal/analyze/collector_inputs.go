package analyze

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func isJankStatsMetric(name string) bool {
	return strings.HasPrefix(name, "jankstats.")
}

func (c *collector) analysisInputCompleteness(summary Summary) AnalysisInputCompleteness {
	runtimeEvidence := summary.LogCount > 0 && summary.DataRecordCount > 0
	classGraph := summary.Influence.HasClassGraph
	diagnostics := c.diagnostics != nil && c.diagnostics.Available && c.diagnostics.ClassCount > 0
	missing := make([]string, 0, 4)
	if !runtimeEvidence {
		missing = append(missing, "события выполнения")
	}
	if !classGraph {
		missing = append(missing, "class-graph.jsonl")
	}
	if !diagnostics {
		missing = append(missing, "instrumentation-diagnostics.jsonl")
	}
	artifactIdentityVerified := len(c.artifactNamespace) == symbolNamespaceBytes
	if (classGraph || diagnostics) && !artifactIdentityVerified {
		missing = append(missing, "совпадающее пространство имён артефактов")
	}
	complete := len(missing) == 0
	status := "complete"
	explanation := "данные выполнения, статический граф классов и диагностика ASM подключены"
	if !complete {
		status = "partial"
		explanation = "часть входных данных анализа отсутствует; соответствующие выводы и дополнительные отчёты ограничены"
		if runtimeEvidence && !classGraph && !diagnostics {
			status = "runtime_only"
			explanation = "доступны данные выполнения, но статический граф, горячие пути, циклы и диагностика ASM неполны"
		}
	}
	return AnalysisInputCompleteness{
		Status:                     status,
		Complete:                   complete,
		RuntimeEvidence:            runtimeEvidence,
		ClassGraph:                 classGraph,
		InstrumentationDiagnostics: diagnostics,
		HeapEvidence:               c.heap != nil && len(c.heap.Sources) > 0,
		ArtifactDirectory:          c.artifactDirectory,
		ArtifactsAutoDiscovered:    c.artifactAuto,
		ArtifactIdentityVerified:   artifactIdentityVerified,
		Missing:                    missing,
		Explanation:                explanation,
	}
}

func validateArtifactNamespace(namespace []byte, header jhlog.SegmentHeader, source, directory string) error {
	if len(namespace) == 0 {
		return nil
	}
	if len(namespace) != symbolNamespaceBytes || !bytes.Equal(namespace, header.SymbolNamespace) {
		return fmt.Errorf(
			"artifact bundle %q does not match .jhlog %q symbol namespace; rebuild the same app variant or pass its exact --artifacts-dir",
			directory,
			source,
		)
	}
	return nil
}

func (c *collector) telemetryHealthWarnings(summary Summary) []string {
	warnings := c.runtimeQualityWarnings()
	warnings = append(warnings, c.artifactIdentityWarnings()...)
	warnings = append(warnings, c.instrumentationQualityWarnings()...)
	warnings = append(warnings, c.attributionQualityWarnings(summary)...)
	return warnings
}

func (c *collector) artifactIdentityWarnings() []string {
	hasClassGraph := c.classGraph != nil && len(c.classGraph.Edges) > 0
	hasDiagnostics := c.diagnostics != nil && c.diagnostics.Available && c.diagnostics.ClassCount > 0
	if (!hasClassGraph && !hasDiagnostics) || len(c.artifactNamespace) == symbolNamespaceBytes {
		return nil
	}
	return []string{
		"Совместимость class graph/ASM diagnostics с выбранными .jhlog не подтверждена: " +
			"передайте файлы рядом с соответствующим artifact-metadata.json либо используйте --artifacts-dir.",
	}
}

func (c *collector) runtimeQualityWarnings() []string {
	var warnings []string
	for _, item := range runtimeQualityCounterWarnings {
		if value := c.counterValues[item.name]; value > 0 {
			warnings = append(warnings, fmt.Sprintf("Качество сбора: %s: %d.", item.label, value))
		}
	}
	quality := c.latestQualityTotals()
	dictionaryOverflow := uint64(c.dictionaryOverflow)
	if quality[jhlog.QualityDictionaryOverflowTotal] > dictionaryOverflow {
		dictionaryOverflow = quality[jhlog.QualityDictionaryOverflowTotal]
	}
	if dictionaryOverflow > 0 {
		warnings = append(warnings, fmt.Sprintf("Качество сбора: словарь .jhlog использовал overflow-ссылки: %d; соответствующие имена могли стать неразличимыми.", dictionaryOverflow))
	}
	exactAdmission := true
	for _, result := range c.streamResults {
		if result.Header.RequiredFeatures&jhlog.FeatureExactEventAdmission == 0 {
			exactAdmission = false
			break
		}
	}
	warnings = append(warnings, qualityCounterWarnings(quality, exactAdmission)...)
	return warnings
}

func (c *collector) latestQualityTotals() map[uint64]uint64 {
	totals := map[uint64]uint64{}
	for _, state := range c.qualitySnapshots {
		for id, value := range state.snapshot.Counters {
			totals[id] = saturatingUint64Sum(totals[id], value)
		}
	}
	return totals
}
