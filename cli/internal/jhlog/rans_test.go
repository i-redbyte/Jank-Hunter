package jhlog

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestRANSRoundTripsCompressibleDistributions(t *testing.T) {
	inputs := [][]byte{
		bytes.Repeat([]byte{0}, 4096),
		bytes.Repeat([]byte{0, 0, 0, 1}, 2048),
		bytes.Repeat([]byte("jank-hunter-column"), 512),
	}
	var encoder ransEncoder
	for index, input := range inputs {
		frame, compressed := encoder.encode(input)
		if !compressed {
			t.Fatalf("case %d did not select rANS", index)
		}
		decoded, err := decodeRANS(frame, len(input))
		if err != nil {
			t.Fatalf("case %d: %v", index, err)
		}
		if !bytes.Equal(decoded, input) {
			t.Fatalf("case %d did not round-trip", index)
		}
	}
}

func TestRANSFallsBackForHighEntropyInput(t *testing.T) {
	input := make([]byte, 4096)
	_, _ = rand.New(rand.NewSource(32815)).Read(input)
	var encoder ransEncoder
	if _, compressed := encoder.encode(input); compressed {
		t.Fatal("high-entropy input unexpectedly selected rANS")
	}
}

func TestRANSRejectsTruncatedStream(t *testing.T) {
	input := bytes.Repeat([]byte{0, 0, 1}, 1024)
	var encoder ransEncoder
	frame, compressed := encoder.encode(input)
	if !compressed {
		t.Fatal("test input did not select rANS")
	}
	if _, err := decodeRANS(frame[:len(frame)-1], len(input)); err == nil {
		t.Fatal("truncated rANS stream was accepted")
	}
}

func TestRANSRejectsNonCanonicalSymbolTable(t *testing.T) {
	frame := []byte{
		2,             // symbol count
		1, 0x80, 0x10, // symbol 1, frequency 2048
		1, 0x80, 0x10, // duplicate symbol 1
		0, 0, 0, 0, // unreachable state
	}
	if _, err := decodeRANS(frame, 32); err == nil {
		t.Fatal("duplicate rANS symbol was accepted")
	}
}
