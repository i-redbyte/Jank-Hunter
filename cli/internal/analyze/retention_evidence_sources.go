package analyze

type EvidenceState string

const (
	EvidencePositive EvidenceState = "positive"
	EvidenceNegative EvidenceState = "negative"
	EvidenceUnknown  EvidenceState = "unknown"
)

// Runtime observations and reachability of instances of the class in a heap snapshot are
// independent evidence. Class reachability does not establish watched-object identity.
type RetentionEvidenceSources struct {
	RuntimeAfterDelay     EvidenceState
	RuntimeAfterGC        EvidenceState
	HeapClassReachability EvidenceState
}

func retainedEvidenceSources(item memoryLeakStats, heap *HeapLeakEvidence) RetentionEvidenceSources {
	sources := RetentionEvidenceSources{RuntimeAfterDelay: EvidenceUnknown, RuntimeAfterGC: EvidenceUnknown, HeapClassReachability: EvidenceUnknown}
	if item.timeOnlyCount > 0 {
		sources.RuntimeAfterDelay = EvidencePositive
	}
	if item.afterExplicitGCCount > 0 {
		sources.RuntimeAfterGC = EvidencePositive
	}
	if heap != nil {
		switch heap.Reachability {
		case EvidencePositive, EvidenceNegative:
			sources.HeapClassReachability = heap.Reachability
		default:
			if confirmedHeapReferencePath(heap) {
				sources.HeapClassReachability = EvidencePositive
			}
		}
	}
	return sources
}

func retainedPrimaryEvidenceQuality(kind string, quality retentionDataQuality) string {
	degraded := quality.runtimeMayBeIncomplete || quality.dictionaryDegraded
	if kind == RetentionEvidenceConfirmedHPROFPath {
		degraded = quality.heapDegraded
	}
	if degraded {
		return "degraded"
	}
	return "complete"
}

// Return a detached snapshot: report rendering and caller-owned inputs cannot mutate each other.
func cloneHeapClassEvidence(heap *HeapLeakEvidence) *HeapLeakEvidence {
	if heap == nil {
		return nil
	}
	copy := *heap
	copy.ReferencePath = cloneHeapPath(heap.ReferencePath)
	copy.AlternativePaths = cloneHeapPaths(heap.AlternativePaths)
	copy.DominatorTree = append([]string(nil), heap.DominatorTree...)
	copy.ReferenceMatchers = append([]string(nil), heap.ReferenceMatchers...)
	// Loading already normalizes sizes and fingerprints. Direct caller evidence only
	// needs the legacy missing reachability state, without rebuilding its fingerprint.
	copy.Reachability = retainedEvidenceSources(memoryLeakStats{}, heap).HeapClassReachability
	if copy.RetainedSizeState != HeapSizeExact && copy.RetainedSizeState != HeapSizeEstimated {
		copy.RetainedSizeState = HeapSizeUnknown
	}
	return &copy
}
