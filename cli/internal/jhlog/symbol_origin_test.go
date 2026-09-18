package jhlog

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSymbolOriginSurvivesWireRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "origins.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, raw := range []string{
		`{"kind":4,"id":1,"value":"a","origin":2}`,
		`{"kind":1,"id":2,"value":"a","origin":1}`,
	} {
		var entry DictionaryEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatal(err)
		}
	}
	event := Event{Type: EventRetained, Retained: &RetainedEvent{ClassRef: LocalSymbol(1), HolderRef: LocalSymbol(2), Count: 1}}
	if err := writer.WriteEvent(event); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range log.Events {
		if event.Retained == nil {
			continue
		}
		class, _ := json.Marshal(event.Retained.ClassRef)
		holder, _ := json.Marshal(event.Retained.HolderRef)
		if !strings.Contains(string(class), `"origin":2`) || !strings.Contains(string(holder), `"origin":1`) {
			t.Fatalf("symbol provenance lost: class=%s holder=%s", class, holder)
		}
		return
	}
	t.Fatal("missing retained event")
}

func TestStableAliasesSeparateOrigins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stable-origins.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, origin := range []SymbolOrigin{SymbolOriginUnknown, SymbolOriginSourceLabel, SymbolOriginRuntimeClass} {
		entry := DictionaryEntry{Kind: DictStableSymbol, ID: 17, Value: "a", Origin: origin}
		if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entry}); err != nil {
			t.Fatal(err)
		}
		ref := StableSymbol(17)
		ref.Origin = origin
		if err := writer.WriteEvent(Event{Type: EventRetained, Retained: &RetainedEvent{ClassRef: ref, Count: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	var got []SymbolOrigin
	for _, event := range log.Events {
		if event.Retained != nil {
			got = append(got, event.Retained.ClassRef.Origin)
		}
	}
	if len(got) != 3 || got[0] != SymbolOriginUnknown || got[1] != SymbolOriginSourceLabel || got[2] != SymbolOriginRuntimeClass {
		t.Fatalf("origin aliases = %v", got)
	}
}

func TestTypedStableReferenceRequiresMatchingAlias(t *testing.T) {
	ref := StableSymbol(17)
	ref.Origin = SymbolOriginRuntimeClass
	if err := writeSymbolRef(io.Discard, ref, stableAliasTable{{ID: 17}: 1}); err == nil {
		t.Fatal("typed symbol silently used unknown alias")
	}
}

func TestColumnarOriginsStayWithEachReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "columnar-origins.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	entries := []DictionaryEntry{
		{Kind: DictScreen, ID: 1, Value: "a", Origin: SymbolOriginSourceLabel},
		{Kind: DictStableSymbol, ID: 17, Value: "a", Origin: SymbolOriginUnknown},
		{Kind: DictStableSymbol, ID: 17, Value: "a", Origin: SymbolOriginSourceLabel},
	}
	for i := range entries {
		if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entries[i]}); err != nil {
			t.Fatal(err)
		}
	}
	caller := StableSymbol(17)
	callee := caller
	callee.Origin = SymbolOriginSourceLabel
	for i := 0; i < 1024; i++ {
		event := Event{Type: EventRuntimeCall, TimeUS: uint64(i + 1), Attribution: AttributionContext{Present: true, Owner: caller, Screen: SymbolRef{ID: 1, Origin: SymbolOriginSourceLabel}}, RuntimeCall: &RuntimeCallEvent{CalleeRef: callee, Count: 1}}
		if err := writer.WriteEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range log.Events {
		if event.RuntimeCall != nil {
			count++
			if event.Attribution.Owner.Origin != SymbolOriginUnknown || event.Attribution.Screen.Origin != SymbolOriginSourceLabel || event.RuntimeCall.CalleeRef.Origin != SymbolOriginSourceLabel {
				t.Fatalf("lost columnar origin: %+v %+v", event.Attribution, event.RuntimeCall)
			}
		}
	}
	if count != 1024 {
		t.Fatalf("rows = %d", count)
	}
}

func TestOriginRequiresFeatureAndLegacyRemainsUnknown(t *testing.T) {
	header := DefaultSegmentHeader()
	header.RequiredFeatures &^= FeatureSymbolOrigin
	var buffer bytes.Buffer
	writer, err := NewWriterWithHeader(&buffer, header)
	if err != nil {
		t.Fatal(err)
	}
	entry := DictionaryEntry{Kind: DictClass, ID: 1, Value: "a", Origin: SymbolOriginRuntimeClass}
	if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entry}); err == nil {
		t.Fatal("typed definition accepted without required feature")
	}
	entry.Origin = SymbolOriginUnknown
	if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entry}); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteEvent(Event{Type: EventRetained, Retained: &RetainedEvent{ClassRef: LocalSymbol(1), Count: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "legacy-origin.jhlog")
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range log.Events {
		if event.Retained != nil && event.Retained.ClassRef.Origin != SymbolOriginUnknown {
			t.Fatal("inferred origin in legacy log")
		}
	}
}
