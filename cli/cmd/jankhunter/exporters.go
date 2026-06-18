package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

type problemsDataset string

const (
	datasetCodeProblems problemsDataset = "code-problems"
	datasetLeaks        problemsDataset = "leaks"
	datasetInfluence    problemsDataset = "influence"
	datasetMathFindings problemsDataset = "math-findings"
)

func parseProblemsDataset(raw string) (problemsDataset, error) {
	switch problemsDataset(strings.ToLower(strings.TrimSpace(raw))) {
	case "", datasetCodeProblems:
		return datasetCodeProblems, nil
	case datasetLeaks:
		return datasetLeaks, nil
	case datasetInfluence:
		return datasetInfluence, nil
	case datasetMathFindings:
		return datasetMathFindings, nil
	default:
		return "", fmt.Errorf("unsupported problems dataset %q", raw)
	}
}

func writeProblemsDatasetJSON(
	file *os.File,
	dataset problemsDataset,
	summary analyze.Summary,
	mathReport *mathanalysis.MathReport,
) error {
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	switch dataset {
	case datasetCodeProblems:
		return encoder.Encode(summary.CodeProblems)
	case datasetLeaks:
		return encoder.Encode(summary.MemoryLeaks)
	case datasetInfluence:
		return encoder.Encode(summary.Influence)
	case datasetMathFindings:
		return encoder.Encode(mathFindingRows(mathReport))
	default:
		return fmt.Errorf("unsupported problems dataset %q", dataset)
	}
}

func writeProblemsDatasetCSV(
	file *os.File,
	dataset problemsDataset,
	summary analyze.Summary,
	mathReport *mathanalysis.MathReport,
) error {
	switch dataset {
	case datasetCodeProblems:
		return writeCSVTable(file, codeProblemsTable(summary.CodeProblems))
	case datasetLeaks:
		return writeCSVTable(file, leakSuspectsTable(summary.MemoryLeaks))
	case datasetInfluence:
		return writeCSVTable(file, influenceTable(summary.Influence))
	case datasetMathFindings:
		return writeCSVTable(file, mathFindingsTable(mathReport))
	default:
		return fmt.Errorf("unsupported problems dataset %q", dataset)
	}
}

type csvTable struct {
	header []string
	rows   [][]string
}

func writeCSVTable(file *os.File, table csvTable) error {
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write(table.header); err != nil {
		return err
	}
	for _, row := range table.rows {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return writer.Error()
}

func codeProblemsTable(rows []analyze.CodeProblemStats) csvTable {
	table := csvTable{
		header: []string{
			"class",
			"method",
			"severity",
			"score",
			"categories",
			"problems",
			"screen",
			"flow",
			"step",
			"route",
			"evidence",
			"recommendation",
		},
	}
	for _, row := range rows {
		drillDown := row.DrillDown
		if len(drillDown) == 0 {
			drillDown = []analyze.CodeProblemDrillDown{{
				ClassName:      row.ClassName,
				Method:         row.Method,
				Evidence:       row.Evidence,
				Recommendation: row.Recommendation,
			}}
		}
		for _, drill := range drillDown {
			table.rows = append(table.rows, []string{
				firstNonEmpty(drill.ClassName, row.ClassName),
				firstNonEmpty(drill.Method, row.Method),
				row.Severity,
				fmt.Sprintf("%.1f", row.Score),
				strings.Join(row.Categories, "|"),
				strings.Join(row.Problems, "|"),
				drill.Screen,
				drill.Flow,
				drill.Step,
				drill.Route,
				firstNonEmpty(drill.Evidence, row.Evidence),
				firstNonEmpty(drill.Recommendation, row.Recommendation),
			})
		}
	}
	return table
}

func leakSuspectsTable(rows []analyze.MemoryLeakSuspect) csvTable {
	table := csvTable{
		header: []string{
			"class",
			"holder",
			"screen",
			"flow",
			"step",
			"severity",
			"score",
			"count",
			"max_age_ms",
			"estimated_retained_kb",
			"heap_evidence",
			"gc_root",
			"holder_field",
			"dominator_path",
			"evidence",
			"recommendation",
		},
	}
	for _, row := range rows {
		table.rows = append(table.rows, []string{
			row.ClassName,
			row.Holder,
			row.Screen,
			row.Flow,
			row.Step,
			row.Severity,
			fmt.Sprintf("%.1f", row.Score),
			fmt.Sprintf("%d", row.Count),
			fmt.Sprintf("%d", row.MaxAgeMS),
			fmt.Sprintf("%d", row.EstimatedRetainedKB),
			fmt.Sprintf("%t", row.HeapEvidence),
			row.GCRoot,
			row.HolderField,
			strings.Join(row.DominatorPath, " -> "),
			row.Evidence,
			row.Recommendation,
		})
	}
	return table
}

func influenceTable(influence analyze.InfluenceSummary) csvTable {
	table := csvTable{
		header: []string{
			"record_type",
			"from",
			"to",
			"severity",
			"score",
			"status",
			"runtime_confirmed",
			"count",
			"screens",
			"flows",
			"routes",
			"evidence",
		},
	}
	for _, node := range influence.TopNodes {
		table.rows = append(table.rows, []string{
			"node",
			node.ClassName,
			"",
			node.Severity,
			fmt.Sprintf("%.1f", node.Score),
			node.Status,
			fmt.Sprintf("%t", node.RuntimeEvidence),
			fmt.Sprintf("%d", node.Problems),
			strings.Join(node.Screens, "|"),
			strings.Join(node.Flows, "|"),
			strings.Join(node.Routes, "|"),
			strings.Join(node.Reasons, "|"),
		})
	}
	for _, edge := range influence.TopEdges {
		table.rows = append(table.rows, []string{
			"edge",
			edge.From,
			edge.To,
			"",
			fmt.Sprintf("%.1f", edge.Influence),
			"",
			fmt.Sprintf("%t", edge.RuntimeConfirmed),
			fmt.Sprintf("%d", edge.Count),
			"",
			"",
			"",
			edge.Reason,
		})
	}
	for _, method := range influence.MethodHotspots {
		table.rows = append(table.rows, []string{
			"method",
			method.ClassName,
			method.Method,
			"",
			fmt.Sprintf("%.1f", method.Weight),
			method.Role,
			fmt.Sprintf("%t", method.RuntimeTouched),
			fmt.Sprintf("%d", method.Count),
			"",
			"",
			"",
			"method-level hotspot",
		})
	}
	for _, path := range influence.HotPaths {
		table.rows = append(table.rows, []string{
			"path",
			strings.Join(path.Nodes, " -> "),
			"",
			"",
			fmt.Sprintf("%.1f", path.Weight),
			"",
			fmt.Sprintf("%t", path.RuntimeTarget),
			"",
			"",
			"",
			"",
			path.Reason,
		})
	}
	for _, finding := range influence.Heuristic {
		table.rows = append(table.rows, []string{
			"finding",
			finding.Title,
			"",
			finding.Severity,
			"",
			"",
			"",
			"",
			"",
			"",
			"",
			finding.Detail,
		})
	}
	return table
}

type mathFindingExportRow struct {
	Section        string   `json:"section"`
	Severity       string   `json:"severity"`
	Title          string   `json:"title"`
	Detail         string   `json:"detail"`
	Evidence       []string `json:"evidence,omitempty"`
	Recommendation string   `json:"recommendation,omitempty"`
}

func mathFindingRows(report *mathanalysis.MathReport) []mathFindingExportRow {
	if report == nil {
		return nil
	}
	rows := make([]mathFindingExportRow, 0, len(report.Findings))
	for _, finding := range report.Findings {
		rows = append(rows, mathFindingExportRow{
			Section:        "итог",
			Severity:       finding.Severity,
			Title:          finding.Title,
			Detail:         finding.Detail,
			Evidence:       finding.Evidence,
			Recommendation: finding.Recommendation,
		})
	}
	for _, section := range report.Sections {
		for _, finding := range section.Findings {
			rows = append(rows, mathFindingExportRow{
				Section:        firstNonEmpty(section.Title, section.ID),
				Severity:       finding.Severity,
				Title:          finding.Title,
				Detail:         finding.Detail,
				Evidence:       finding.Evidence,
				Recommendation: finding.Recommendation,
			})
		}
	}
	return rows
}

func mathFindingsTable(report *mathanalysis.MathReport) csvTable {
	table := csvTable{
		header: []string{"section", "severity", "title", "detail", "evidence", "recommendation"},
	}
	for _, row := range mathFindingRows(report) {
		table.rows = append(table.rows, []string{
			row.Section,
			row.Severity,
			row.Title,
			row.Detail,
			strings.Join(row.Evidence, "|"),
			row.Recommendation,
		})
	}
	return table
}
