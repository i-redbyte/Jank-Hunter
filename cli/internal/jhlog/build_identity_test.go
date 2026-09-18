package jhlog

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadVersionedMappingIdentityHeader(t *testing.T) {
	header := DefaultSegmentHeader()
	header.Schema = 2
	encoded, _, err := encodeFileHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Clone(encoded[len(Magic)+8:])
	payload[0] = 3
	payload = append(payload, 2, 32)
	payload = append(payload, bytes.Repeat([]byte{0xab}, 32)...)
	payload = append(payload, 0)
	decoded, err := decodeHeaderPayload(payload)
	if err != nil {
		t.Fatalf("cannot read versioned mapping identity: %v", err)
	}
	if decoded.BuildIdentity.State != BuildIdentityMapped || decoded.BuildIdentity.MappingSHA256 != strings.Repeat("ab", 32) {
		t.Fatalf("identity lost: %+v", decoded.BuildIdentity)
	}
}

func TestBuildIdentityRoundTripAndInvalidStates(t *testing.T) {
	for _, identity := range []BuildIdentity{{}, {State: BuildIdentityUnknown, Reason: 5}, {State: BuildIdentityUnminified}, {State: BuildIdentityMapped, MappingSHA256: strings.Repeat("ab", 32)}} {
		header := DefaultSegmentHeader()
		header.BuildIdentity = identity
		encoded, _, err := encodeFileHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeHeaderPayload(encoded[len(Magic)+8:])
		if err != nil {
			t.Fatal(err)
		}
		if decoded.BuildIdentity != identity {
			t.Fatalf("round trip: got %+v want %+v", decoded.BuildIdentity, identity)
		}
	}
	for _, identity := range []BuildIdentity{
		{State: 3}, {State: BuildIdentityMapped}, {State: BuildIdentityMapped, MappingSHA256: strings.Repeat("AB", 32)},
		{State: BuildIdentityMapped, MappingSHA256: strings.Repeat("ab", 32), Reason: 1},
		{State: BuildIdentityUnminified, MappingSHA256: strings.Repeat("ab", 32)}, {Reason: 6},
	} {
		header := DefaultSegmentHeader()
		header.BuildIdentity = identity
		if _, _, err := encodeFileHeader(header); err == nil {
			t.Fatalf("accepted invalid identity: %+v", identity)
		}
	}
}

func TestLegacyHeaderCannotSilentlyDiscardIdentity(t *testing.T) {
	header := DefaultSegmentHeader()
	header.Schema = HeaderSchemaV2
	encoded, _, err := encodeFileHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := decodeHeaderPayload(encoded[len(Magic)+8:])
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Schema != 2 || legacy.BuildIdentity != (BuildIdentity{}) {
		t.Fatalf("legacy identity: %+v", legacy)
	}
	header.BuildIdentity = BuildIdentity{State: BuildIdentityMapped, MappingSHA256: strings.Repeat("ab", 32)}
	if _, _, err := encodeFileHeader(header); err == nil {
		t.Fatal("legacy writer discarded mapping identity")
	}
}
