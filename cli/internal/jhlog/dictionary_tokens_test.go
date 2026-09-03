package jhlog

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestSegmentDictionaryTokensRoundTripCodeNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dictionary-tokens.jhlog")
	file, writer, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	entries := []DictionaryEntry{
		{Kind: DictClass, ID: 1, Value: "ru.mail.im.feature.feed.FeedPresenter"},
		{Kind: DictOwner, ID: 2, Value: "ru.mail.im.feature.feed.FeedPresenter.render"},
		{Kind: DictStableSymbol, ID: 0x51, Value: "ru.mail.im.feature.feed.FeedPresenter.render(kotlin.Int)"},
		{Kind: DictClass, ID: 3, Value: "ru.mail.im.feature.chat.ChatPresenter"},
		{Kind: DictRoute, ID: 4, Value: "GET /feature/feed"},
	}
	for index := range entries {
		if err := writer.WriteEvent(Event{Type: EventDictionary, Dictionary: &entries[index]}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	decoded := make([]DictionaryEntry, 0, len(entries))
	if _, err := StreamFileWithResult(path, func(event Event, _ map[uint64]string) error {
		if event.Dictionary != nil {
			decoded = append(decoded, *event.Dictionary)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(entries) {
		t.Fatalf("decoded entries = %d, want %d", len(decoded), len(entries))
	}
	for index := range entries {
		if decoded[index].ID != entries[index].ID || decoded[index].Kind != entries[index].Kind ||
			decoded[index].Value != entries[index].Value {
			t.Fatalf("token value %d = %+v, want %+v", index, decoded[index], entries[index])
		}
	}
}

func TestSegmentDictionaryTokensReuseComponentsAndBoundBootstrapDebt(t *testing.T) {
	var encoder dictionaryTokenEncoder
	previous := []byte(nil)
	frontBytes := 0
	tokenBytes := 0
	for index, value := range []string{
		"ru.mail.im.feature.feed.FeedPresenter.render",
		"com.google.android.material.button.MaterialButton.draw",
		"ru.mail.im.feature.feed.FeedPresenter.bind",
		"com.google.android.material.button.MaterialButton.measure",
	} {
		data := []byte(value)
		prefix := commonUTF8Prefix(previous, data)
		front := uvarintSize(uint64(prefix)<<1) + uvarintSize(uint64(len(data)-prefix)) + len(data) - prefix
		encoded, ok := encoder.prepare(DictStableSymbol, data, prefix)
		if !ok {
			t.Fatalf("row %d unexpectedly stayed front-coded", index)
		}
		frontBytes += front
		tokenBytes += len(encoded)
		encoder.commit(data)
		previous = append(previous[:0], data...)
	}
	if tokenBytes >= frontBytes {
		t.Fatalf("token bytes = %d, tagged front bytes = %d", tokenBytes, frontBytes)
	}
	if encoder.balance < -dictionaryTokenBootstrapDebt {
		t.Fatalf("token bootstrap balance = %d", encoder.balance)
	}
}

func TestSegmentDictionaryTokensRejectUnknownReference(t *testing.T) {
	var body bytes.Buffer
	for _, value := range []uint64{
		uint64(EventDictionary), 0, uint64(DictClass), 1,
		3, 4,
	} {
		if err := writeUvarint(&body, value); err != nil {
			t.Fatal(err)
		}
	}
	_, _, err := decodeRecord(
		body.Bytes(), recordDecodeState{}, "", "", "tokens", RecordPosition{}, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "references token") {
		t.Fatalf("unknown token reference error = %v", err)
	}
}
