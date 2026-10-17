package jhlog

import "fmt"

// Bounds apply to one segment, including repeated definitions. Front coding and
// dictionary tokens can expand a few wire bytes into megabytes of retained text.
const maxDecodedDictionaryBytes uint64 = 256 << 20
const maxDecodedDictionaryEntries uint64 = 1 << 20

func (s *segmentDecodeState) checkDictionaryBudget(additional uint64) error {
	if s.dictionaryEntries >= maxDecodedDictionaryEntries || additional > maxDecodedDictionaryBytes ||
		s.dictionaryDecodedBytes > maxDecodedDictionaryBytes-additional {
		return fmt.Errorf("dictionary decoding limit exceeded: at most %d entries and %d expanded bytes per segment",
			maxDecodedDictionaryEntries, maxDecodedDictionaryBytes)
	}
	return nil
}
