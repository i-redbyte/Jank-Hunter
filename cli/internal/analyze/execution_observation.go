package analyze

import "strings"

type ExecutionObservationEvidence struct {
	TaskMode, CoroutineMode, RequestedTaskMode, RequestedCoroutineMode string
	Known, PartialLinks                                                bool
}

func executionObservationEvidence(counters []NamedValue) *ExecutionObservationEvidence {
	const prefix = "jankhunter.observation."
	groups := [4]map[string]bool{}
	prefixes := [4]string{"tasks.active.", "coroutines.active.", "tasks.requested.", "coroutines.requested."}
	found, partial := false, false
	for _, counter := range counters {
		if counter.Value == 0 || !strings.HasPrefix(counter.Name, prefix) {
			continue
		}
		found = true
		name := strings.TrimPrefix(counter.Name, prefix)
		partial = partial || strings.HasPrefix(name, "coverage.")
		for i, key := range prefixes {
			if !strings.HasPrefix(name, key) || !strings.HasSuffix(name, ".count") {
				continue
			}
			mode := strings.TrimSuffix(strings.TrimPrefix(name, key), ".count")
			if groups[i] == nil {
				groups[i] = make(map[string]bool)
			}
			groups[i][mode] = true
		}
	}
	if !found {
		return nil
	}
	mode := func(values map[string]bool) string {
		if len(values) > 1 {
			return "mixed"
		}
		for value := range values {
			if value == "WRAPPERS" || value == "IN_PLACE" {
				return value
			}
		}
		return "unknown"
	}
	e := &ExecutionObservationEvidence{TaskMode: mode(groups[0]), CoroutineMode: mode(groups[1]),
		RequestedTaskMode: mode(groups[2]), RequestedCoroutineMode: mode(groups[3]), PartialLinks: partial}
	e.Known = e.TaskMode != "unknown" && e.TaskMode != "mixed" && e.CoroutineMode != "unknown" && e.CoroutineMode != "mixed"
	return e
}

func executionObservationCompatible(before, after Summary) bool {
	a, b := executionObservationEvidence(before.Counters), executionObservationEvidence(after.Counters)
	// Preserve legacy comparison behavior when neither capture declares the new contract.
	if a == nil && b == nil {
		return true
	}
	return a != nil && b != nil && a.Known && b.Known && a.TaskMode == b.TaskMode && a.CoroutineMode == b.CoroutineMode
}
