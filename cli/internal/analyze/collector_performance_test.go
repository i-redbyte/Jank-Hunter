package analyze

import (
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestSignalContextKeyDoesNotAllocate(t *testing.T) {
	collector := newCollector("allocation test", 1, Options{})
	collector.ensureSignalContext(collector.contextKey("Screen", "Owner"))

	allocations := testing.AllocsPerRun(1_000, func() {
		collector.ensureSignalContext(collector.contextKey("Screen", "Owner"))
	})
	if allocations != 0 {
		t.Fatalf("signal-context lookup allocates %.2f objects, want zero", allocations)
	}
}

func TestSignalContextKeyCannotCollideOnFieldSeparator(t *testing.T) {
	collector := newCollector("collision test", 1, Options{})
	collector.currentAttrScreen = "screen\x00unknown"
	collector.currentAttrOwner = "owner"
	first := collector.contextKey("", "")

	collector.currentAttrScreen = "screen"
	collector.currentAttrOwner = "unknown\x00owner"
	second := collector.contextKey("", "")

	if first == second {
		t.Fatal("different signal contexts produced the same aggregation key")
	}
}

func TestMemoryLeakKeyCannotCollideOnFieldSeparator(t *testing.T) {
	collector := newCollector("collision test", 1, Options{})
	collector.addMemoryLeakSuspect(
		"class\x00holder", "screen",
		SignalContextStats{Screen: "operation", Operation: "owner"},
		1, 1, jhlog.RetentionEvidenceTimeOnly, true,
	)
	collector.addMemoryLeakSuspect(
		"class", "holder",
		SignalContextStats{Screen: "screen", Operation: "operation\x00owner"},
		1, 1, jhlog.RetentionEvidenceTimeOnly, true,
	)

	if got := len(collector.memoryLeakStats); got != 2 {
		t.Fatalf("memory-leak aggregation has %d entries, want two distinct contexts", got)
	}
}
