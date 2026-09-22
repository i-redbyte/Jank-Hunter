package analyze

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	stableOwnerIDPrefix    = "stable:0x"
	stableOwnerIDHexDigits = 16
	lowercaseHex           = "0123456789abcdef"
)

type OwnerMap struct {
	Entries         map[string]string
	SymbolNamespace []byte
}

func ResolveOwnerAlias(ownerMap *OwnerMap, owner string) string {
	if ownerMap == nil || owner == "" {
		return owner
	}
	if len(ownerMap.SymbolNamespace) != ownerMapNamespaceBytes || len(ownerMap.Entries) == 0 {
		return owner
	}
	if mapped, ok := ownerMap.Entries[owner]; ok {
		return mapped
	}
	if namespace, canonicalID, ok := namespacedStableOwnerID(owner); ok {
		if !matchesSymbolNamespace(namespace, ownerMap.SymbolNamespace) {
			return owner
		}
		if mapped, found := ownerMap.Entries[canonicalID]; found {
			return mapped
		}
	}
	return owner
}

func matchesSymbolNamespace(encoded string, namespace []byte) bool {
	if len(encoded) != len(namespace)*2 {
		return false
	}
	for index, value := range namespace {
		unsigned := int(value)
		if encoded[index*2] != lowercaseHex[unsigned>>4] || encoded[index*2+1] != lowercaseHex[unsigned&0x0f] {
			return false
		}
	}
	return true
}

func validateOwnerMapNamespace(ownerMap *OwnerMap, header jhlog.SegmentHeader, source string) error {
	if ownerMap == nil {
		return nil
	}
	if len(ownerMap.SymbolNamespace) != ownerMapNamespaceBytes {
		return fmt.Errorf("owner map symbolNamespace must contain exactly %d bytes", ownerMapNamespaceBytes)
	}
	if len(header.SymbolNamespace) != ownerMapNamespaceBytes {
		return fmt.Errorf(
			".jhlog %q symbol namespace must contain exactly %d bytes, got %d",
			source,
			ownerMapNamespaceBytes,
			len(header.SymbolNamespace),
		)
	}
	if !bytes.Equal(ownerMap.SymbolNamespace, header.SymbolNamespace) {
		return fmt.Errorf(
			"owner map symbol namespace %s does not match .jhlog %q namespace %s",
			hexOrEmpty(ownerMap.SymbolNamespace),
			source,
			hexOrEmpty(header.SymbolNamespace),
		)
	}
	return nil
}

func hexOrEmpty(value []byte) string {
	if len(value) == 0 {
		return "<empty>"
	}
	return hex.EncodeToString(value)
}

func isCanonicalStableOwnerID(id string) bool {
	if len(id) != len(stableOwnerIDPrefix)+stableOwnerIDHexDigits || !strings.HasPrefix(id, stableOwnerIDPrefix) {
		return false
	}
	for _, digit := range id[len(stableOwnerIDPrefix):] {
		if (digit < '0' || digit > '9') && (digit < 'a' || digit > 'f') {
			return false
		}
	}
	return true
}

func namespacedStableOwnerID(owner string) (string, string, bool) {
	const namespacedPrefix = "stable:"
	if !strings.HasPrefix(owner, namespacedPrefix) || isCanonicalStableOwnerID(owner) {
		return "", "", false
	}
	valueIndex := strings.LastIndex(owner, ":0x")
	if valueIndex <= len(namespacedPrefix) {
		return "", "", false
	}
	namespace := owner[len(namespacedPrefix):valueIndex]
	if namespace == "" || len(namespace)%2 != 0 {
		return "", "", false
	}
	for _, digit := range namespace {
		if (digit < '0' || digit > '9') && (digit < 'a' || digit > 'f') {
			return "", "", false
		}
	}
	canonicalID := stableOwnerIDPrefix + owner[valueIndex+3:]
	if !isCanonicalStableOwnerID(canonicalID) {
		return "", "", false
	}
	return namespace, canonicalID, true
}
