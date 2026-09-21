package jhlog

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

type BuildIdentityState uint64

const (
	BuildIdentityUnknown BuildIdentityState = iota
	BuildIdentityUnminified
	BuildIdentityMapped
)

type BuildIdentity struct {
	State         BuildIdentityState `json:"state"`
	MappingSHA256 string             `json:"mapping_sha256,omitempty"`
	Reason        uint64             `json:"reason,omitempty"`
}

func (identity BuildIdentity) validate() error {
	switch identity.State {
	case BuildIdentityMapped:
		hash, err := hex.DecodeString(identity.MappingSHA256)
		if err != nil || len(hash) != 32 || identity.MappingSHA256 != hex.EncodeToString(hash) || identity.Reason != 0 {
			return fmt.Errorf("invalid mapped build identity")
		}
	case BuildIdentityUnminified:
		if identity.MappingSHA256 != "" || identity.Reason != 0 {
			return fmt.Errorf("invalid unminified build identity")
		}
	case BuildIdentityUnknown:
		if identity.MappingSHA256 != "" || identity.Reason > 5 {
			return fmt.Errorf("invalid unknown build identity")
		}
	default:
		return fmt.Errorf("unsupported build identity state %d", identity.State)
	}
	return nil
}

func writeBuildIdentity(payload *bytes.Buffer, header SegmentHeader) error {
	identity := header.BuildIdentity
	if header.Schema == HeaderSchemaV2 {
		if identity != (BuildIdentity{}) {
			return fmt.Errorf("legacy header cannot carry build identity")
		}
		return nil
	}
	if err := identity.validate(); err != nil {
		return err
	}
	if err := writeUvarint(payload, uint64(identity.State)); err != nil {
		return err
	}
	hash, _ := hex.DecodeString(identity.MappingSHA256)
	if err := writeLengthDelimited(payload, hash); err != nil {
		return err
	}
	return writeUvarint(payload, identity.Reason)
}

func readBuildIdentity(reader *bytes.Reader) (BuildIdentity, error) {
	state, err := binary.ReadUvarint(reader)
	if err != nil {
		return BuildIdentity{}, fmt.Errorf("build identity state: %w", err)
	}
	hash, err := readBoundedBytes(reader, "mapping SHA-256", 32)
	if err != nil {
		return BuildIdentity{}, err
	}
	reason, err := binary.ReadUvarint(reader)
	if err != nil {
		return BuildIdentity{}, fmt.Errorf("build identity reason: %w", err)
	}
	identity := BuildIdentity{State: BuildIdentityState(state), MappingSHA256: hex.EncodeToString(hash), Reason: reason}
	if err := identity.validate(); err != nil {
		return BuildIdentity{}, err
	}
	return identity, nil
}
