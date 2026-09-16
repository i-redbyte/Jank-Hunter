package analyze

// Version 1 makes Diagnostics authoritative. Warnings remains a compatibility
// projection for older consumers. Unversioned warnings have unknown impact.
const HeapDiagnosticsVersion = 1

type HeapDiagnosticSeverity string
type HeapDiagnosticImpact string

const (
	HeapDiagnosticInfo    HeapDiagnosticSeverity = "info"
	HeapDiagnosticWarning HeapDiagnosticSeverity = "warning"
	HeapImpactNone        HeapDiagnosticImpact   = "none"
	HeapImpactGraph       HeapDiagnosticImpact   = "graph_incomplete"
	HeapImpactSize        HeapDiagnosticImpact   = "retained_size"
	HeapImpactDisplay     HeapDiagnosticImpact   = "display"
	HeapImpactCoverage    HeapDiagnosticImpact   = "analysis_incomplete"
	HeapImpactUnknown     HeapDiagnosticImpact   = "unknown"
)

type HeapDiagnostic struct {
	Code     string                 `json:"code"`
	Severity HeapDiagnosticSeverity `json:"severity"`
	Impact   HeapDiagnosticImpact   `json:"impact"`
	Source   string                 `json:"source,omitempty"`
	Message  string                 `json:"message"`
}

// Informational requires an explicitly neutral impact; unknown future impacts fail closed.
func (d HeapDiagnostic) Informational() bool {
	return d.Severity == HeapDiagnosticInfo && d.Impact == HeapImpactNone
}

func (d HeapDiagnostic) affectsGraphEvidence() bool {
	if d.Severity != HeapDiagnosticInfo && d.Severity != HeapDiagnosticWarning {
		return true
	}
	switch d.Impact {
	case HeapImpactNone, HeapImpactSize, HeapImpactDisplay:
		return false
	default:
		return true
	}
}

func (h *HeapEvidence) effectiveDiagnostics() []HeapDiagnostic {
	if h == nil {
		return nil
	}
	if h.DiagnosticsVersion == HeapDiagnosticsVersion {
		return h.Diagnostics
	}
	// Container Sources never proved which dump produced an untyped warning.
	items := make([]HeapDiagnostic, 0, len(h.Warnings)+1)
	for _, warning := range h.Warnings {
		items = append(items, HeapDiagnostic{Code: "legacy_warning", Severity: HeapDiagnosticWarning, Impact: HeapImpactUnknown, Message: warning})
	}
	if h.DiagnosticsVersion != 0 || len(h.Diagnostics) > 0 {
		items = append(items, HeapDiagnostic{Code: "unsupported_diagnostics", Severity: HeapDiagnosticWarning, Impact: HeapImpactUnknown, Message: "Версия диагностик HPROF не поддерживается; влияние на полноту неизвестно."})
	}
	return items
}

func (h *HeapEvidence) AddDiagnostic(d HeapDiagnostic) {
	if h.DiagnosticsVersion != HeapDiagnosticsVersion {
		h.Diagnostics = h.effectiveDiagnostics()
		h.DiagnosticsVersion = HeapDiagnosticsVersion
	}
	h.Diagnostics = append(h.Diagnostics, d)
	if !d.Informational() {
		h.Warnings = append(h.Warnings, d.Message)
	}
}

func heapParserDiagnosticImpact(code string) HeapDiagnosticImpact {
	switch code {
	case "strings", "string-record-bytes", "string-bytes", "roots", "class-fields", "deferred-instances", "unresolved-instance-classes", "nodes", "classes", "edges", "missing_roots":
		return HeapImpactGraph
	case "unresolved-instance-sizes", "retained-traversal":
		return HeapImpactSize
	case "reference-path-depth", "retained-sample":
		return HeapImpactDisplay
	case "target-objects", "analysis-work", "evidence-targets":
		return HeapImpactCoverage
	default:
		return HeapImpactUnknown
	}
}

// Index diagnostic sources once per summary, so candidate lookup does not scan
// every diagnostic from every input dump for each retained class.
func (q retentionDataQuality) forHeap(heap *HeapLeakEvidence) retentionDataQuality {
	if heap == nil || heap.Source == "" || q.heapNotesBySource == nil {
		return q
	}
	common, specific := q.heapNotesBySource[""], q.heapNotesBySource[heap.Source]
	q.heapDegraded = len(common)+len(specific) > 0
	switch {
	case len(common) == 0:
		q.heapNotes = specific
	case len(specific) == 0:
		q.heapNotes = common
	default:
		q.heapNotes = make([]string, 0, len(common)+len(specific))
		q.heapNotes = append(q.heapNotes, common...)
		q.heapNotes = append(q.heapNotes, specific...)
	}
	return q
}
