package jhlog

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strings"
)

// ProcessRosterFingerprint returns the canonical SHA-256 of sorted unique process names.
func ProcessRosterFingerprint(processNames []string) []byte {
	unique := make(map[string]struct{}, len(processNames))
	for _, processName := range processNames {
		if normalized := strings.TrimSpace(processName); normalized != "" {
			unique[normalized] = struct{}{}
		}
	}
	names := make([]string, 0, len(unique))
	for processName := range unique {
		names = append(names, processName)
	}
	sort.Strings(names)
	digest := sha256.New()
	var length [4]byte
	for _, processName := range names {
		binary.BigEndian.PutUint32(length[:], uint32(len(processName)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write([]byte(processName))
	}
	return digest.Sum(nil)
}
