package analyze

import "fmt"

// Format 1 has no pass provenance. Mixing it with versioned records would hide
// duplicate transforms, so it is accepted only as a single record per class.
func validateInstrumentationPass(record instrumentationDiagnosticsRecord) error {
	switch record.Format {
	case 1:
		if record.Pass != "" {
			return fmt.Errorf("format 1 must not contain pass identity")
		}
	case InstrumentationDiagnosticsFormat:
		if record.Pass != "main" && record.Pass != "lifecycle" {
			return fmt.Errorf("format 2 requires pass main or lifecycle, got %q", record.Pass)
		}
	default:
		return validateArtifactFormat("", "instrumentation diagnostics", record.Format, InstrumentationDiagnosticsFormat)
	}
	return nil
}

func instrumentationPassBit(pass string) uint8 {
	switch pass {
	case "main":
		return 2
	case "lifecycle":
		return 4
	default:
		return 1
	}
}

// There are at most two versioned records per class and the loader bounds both
// records and details. Leaf pass records retain all counters and provenance;
// combined method/annotation/filter totals describe only the main pass.
func combineInstrumentationPasses(records []InstrumentationClassDiagnostic) []InstrumentationClassDiagnostic {
	versioned := false
	for _, record := range records {
		if record.Pass != "" {
			versioned = true
			break
		}
	}
	if !versioned {
		return records
	}
	indices := make(map[string]int, len(records))
	out := make([]InstrumentationClassDiagnostic, 0, len(records))
	for _, record := range records {
		index, exists := indices[record.ClassName]
		if !exists {
			index = len(out)
			indices[record.ClassName] = index
			out = append(out, InstrumentationClassDiagnostic{ClassName: record.ClassName})
		}
		out[index].Passes = append(out[index].Passes, record)
	}
	for index := range out {
		passes := out[index].Passes
		if len(passes) == 2 && passes[0].Pass == "lifecycle" {
			passes[0], passes[1] = passes[1], passes[0]
		}
		combined := InstrumentationClassDiagnostic{ClassName: out[index].ClassName}
		for _, pass := range passes {
			if pass.Pass != "lifecycle" {
				combined = pass
				break
			}
		}
		// Fresh slices avoid aliasing the immutable per-pass evidence.
		combined.Hooks = nil
		combined.Decisions = nil
		combined.HookCount = 0
		for _, pass := range passes {
			combined.Hooks = append(combined.Hooks, pass.Hooks...)
			combined.Decisions = append(combined.Decisions, pass.Decisions...)
			combined.HookCount = saturatingUint64Sum(combined.HookCount, pass.HookCount)
		}
		sortHookSummaries(combined.Hooks)
		sortDecisionSummaries(combined.Decisions)
		if len(passes) != 1 || passes[0].Pass != "" {
			combined.Passes = passes
		}
		combined.Pass = ""
		out[index] = combined
	}
	return out
}
