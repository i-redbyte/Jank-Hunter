package analyze

import (
	"fmt"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type MappingIdentityStatus string

const (
	MappingNotRequested MappingIdentityStatus = "not_requested"
	MappingVerified     MappingIdentityStatus = "verified"
	MappingUnverified   MappingIdentityStatus = "unverified"
)

type MappingIdentityEvidence struct {
	Status                  MappingIdentityStatus
	Applied                 bool
	SHA256                  string
	VerifiedLogs            int
	UnverifiedLogs          int
	UnknownOriginReferences uint64
}

// ValidateMappingInputs runs before any mapping-dependent filtering or symbol transformation.
// The trust override permits missing evidence; it never overrides contradictory evidence.
func ValidateMappingInputs(inputs []SessionInput, mapping *NameMapping, allowUnverified bool) (MappingIdentityEvidence, error) {
	evidence := MappingIdentityEvidence{Status: MappingNotRequested}
	if mapping == nil {
		return evidence, nil
	}
	evidence.SHA256 = mapping.digest
	for _, input := range inputs {
		identity := input.Header.BuildIdentity
		switch {
		case input.Header.Schema == jhlog.HeaderSchemaV3 && identity.State == jhlog.BuildIdentityMapped:
			if mapping.digest == "" || mapping.digest != identity.MappingSHA256 {
				return MappingIdentityEvidence{}, fmt.Errorf("mapping identity mismatch for %q: log=%s mapping=%s", input.Path, identity.MappingSHA256, mapping.digest)
			}
			evidence.VerifiedLogs++
		case input.Header.Schema == jhlog.HeaderSchemaV3 && identity.State == jhlog.BuildIdentityUnminified:
			return MappingIdentityEvidence{}, fmt.Errorf("mapping identity mismatch for %q: build explicitly declares unminified symbols", input.Path)
		default:
			evidence.UnverifiedLogs++
		}
	}
	evidence.Status = MappingVerified
	if evidence.UnverifiedLogs > 0 || len(inputs) == 0 {
		if !allowUnverified {
			return MappingIdentityEvidence{}, fmt.Errorf("mapping identity is unverified; use --allow-unverified-mapping only to explicitly trust legacy or unknown build identity")
		}
		evidence.Status = MappingUnverified
	}
	if err := mapping.validateSemantics(); err != nil {
		return MappingIdentityEvidence{}, fmt.Errorf("validate mapping semantics: %w", err)
	}
	evidence.Applied = true
	return evidence, nil
}
