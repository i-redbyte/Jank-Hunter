package analyze

import (
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"testing"
)

func TestUnknownSymbolOriginDoesNotAuthorizeMapping(t *testing.T) {
	mapping := &NameMapping{classes: map[string]string{"a": "original.Other"}}
	c := newCollector("unknown-origin", 1, Options{ObfuscationMap: mapping})
	if got := c.resolveOwnerRef(map[uint64]string{7: "a"}, jhlog.LocalSymbol(7)); got != "a" {
		t.Fatalf("unknown caller label rewritten as runtime class: %q", got)
	}
}

func TestHeapOnlyClassIsMappedExactlyOnce(t *testing.T) {
	mapping := &NameMapping{classes: map[string]string{"a": "b", "b": "original.Other"}}
	c := newCollector("heap-origin", 1, Options{ObfuscationMap: mapping, HeapEvidence: &HeapEvidence{Leaks: []HeapLeakEvidence{{ClassName: "a", RetainedObjectCount: 1}}}})
	c.addHeapOnlyMemoryLeaks()
	if _, ok := c.retainedClasses["b"]; !ok {
		t.Fatalf("mapped heap class rewritten a second time: %+v", c.retainedClasses)
	}
}

func TestUnknownAndSourceStacksNeverInvokeRetrace(t *testing.T) {
	c := newCollector("unknown-stack", 1, Options{ObfuscationMap: &NameMapping{path: "missing-mapping"}})
	for _, origin := range []jhlog.SymbolOrigin{jhlog.SymbolOriginUnknown, jhlog.SymbolOriginSourceLabel} {
		addOwner(c.ownerStats, fmt.Sprint(origin), "main_thread_stall", 1, "at a.b(Source.java:1)", origin)
	}
	if err := c.retraceStackHints(); err != nil {
		t.Fatalf("retraced a source or unknown stack: %v", err)
	}
	for _, owner := range c.ownerStats {
		if owner.StackRetrace != nil || owner.StackHint != "at a.b(Source.java:1)" {
			t.Fatalf("rewrote non-runtime stack: %+v", owner)
		}
	}
}
