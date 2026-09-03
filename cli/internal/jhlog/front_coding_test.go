package jhlog

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestDictionaryFrontCodingRoundTripsPerKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "front-coded.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	events := []Event{
		{Type: EventDictionary, Dictionary: &DictionaryEntry{Kind: DictOwner, ID: 1, Value: "com.example.feed.First"}},
		{Type: EventDictionary, Dictionary: &DictionaryEntry{Kind: DictOwner, ID: 2, Value: "com.example.feed.Second"}},
		{Type: EventDictionary, Dictionary: &DictionaryEntry{Kind: DictRoute, ID: 3, Value: "com.example.feed.Route"}},
		{Type: EventDictionary, Dictionary: &DictionaryEntry{Kind: DictOwner, ID: 4, Value: ""}},
		{Type: EventDictionary, Dictionary: &DictionaryEntry{Kind: DictOwner, ID: 5, Value: "com.example.feed.AfterEmpty"}},
	}
	for _, event := range events {
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
	if len(log.Dict) != len(events) {
		t.Fatalf("dictionary = %d, want %d", len(log.Dict), len(events))
	}
	for index := range events {
		entry := events[index].Dictionary
		if log.Dict[entry.ID] != entry.Value || log.Kinds[entry.ID] != entry.Kind {
			t.Fatalf("dictionary %d = %q kind %d", index, log.Dict[entry.ID], log.Kinds[entry.ID])
		}
	}
	first, _, err := prepareDictionaryFront(*events[0].Dictionary, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := prepareDictionaryFront(*events[1].Dictionary, first.frontData)
	if err != nil {
		t.Fatal(err)
	}
	if second.frontPrefix != uint64(len("com.example.feed.")) {
		t.Fatalf("front prefix = %d", second.frontPrefix)
	}
}

func TestDictionaryFrontCodingKeepsUTF8BoundaryAndRejectsNonCanonicalPrefix(t *testing.T) {
	if got := commonUTF8Prefix([]byte("prefix-А"), []byte("prefix-Б")); got != len("prefix-") {
		t.Fatalf("UTF-8 prefix = %d, want %d", got, len("prefix-"))
	}
	segmentState := decodeSegmentState(nil)
	segmentState.dictionaryPrevious[DictOwner] = []byte("alpha")
	var body bytes.Buffer
	for _, value := range []uint64{uint64(EventDictionary), 0, uint64(DictOwner), 1, 2, 5} {
		if err := writeUvarint(&body, value); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = body.WriteString("lpine")
	_, _, err := decodeRecord(
		body.Bytes(), recordDecodeState{}, "", "", "front", RecordPosition{}, nil, segmentState,
	)
	if err == nil || !strings.Contains(err.Error(), "non-canonical") {
		t.Fatalf("non-canonical prefix error = %v", err)
	}
}
