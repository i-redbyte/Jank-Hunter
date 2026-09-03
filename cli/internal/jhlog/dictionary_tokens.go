package jhlog

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

const (
	maxSegmentDictionaryTokens     = 8_192
	maxSegmentDictionaryTokenBytes = 256 * 1024
	maxDictionaryTokenAtoms        = 256
	dictionaryTokenBootstrapDebt   = 1_024
	dictionaryAtomTagMask          = 3
	dictionaryAtomReference        = 0
	dictionaryAtomDefinition       = 1
	dictionaryAtomSeparator        = 2
	dictionaryAtomLiteral          = 3
)

const dictionarySeparators = ".$/#()[];:<> ,-=?!@+*'\"\\&|"

type dictionaryTokenRange struct {
	offset int
	length int
}

type dictionaryTokenEncoder struct {
	tokens             map[string]uint64
	tokenBytes         int
	balance            int
	scratch            []byte
	pending            []dictionaryTokenRange
	pendingBytes       int
	pendingBalanceGain int
}

func (encoder *dictionaryTokenEncoder) prepare(
	kind DictKind,
	data []byte,
	frontPrefix int,
) ([]byte, bool) {
	encoder.pending = encoder.pending[:0]
	encoder.pendingBytes = 0
	encoder.pendingBalanceGain = 0
	if !dictionaryTokenKind(kind) || !dictionaryCodeName(data) {
		return nil, false
	}
	atoms := dictionaryAtomCount(data)
	if atoms == 0 || atoms > maxDictionaryTokenAtoms {
		return nil, false
	}
	encoder.scratch = appendUvarint(encoder.scratch[:0], uint64(atoms)<<1|1)
	for offset := 0; offset < len(data); {
		if !dictionaryIdentifierByte(data[offset]) {
			if code, ok := dictionarySeparatorCode(data[offset]); ok {
				encoder.scratch = appendUvarint(encoder.scratch, uint64(code)<<2|dictionaryAtomSeparator)
			} else {
				encoder.scratch = appendUvarint(encoder.scratch, uint64(1)<<2|dictionaryAtomLiteral)
				encoder.scratch = append(encoder.scratch, data[offset])
			}
			offset++
			continue
		}
		end := offset + 1
		for end < len(data) && dictionaryIdentifierByte(data[end]) {
			end++
		}
		encoder.appendIdentifier(data, offset, end)
		offset = end
	}
	frontBytes := uvarintSize(uint64(frontPrefix)<<1) +
		uvarintSize(uint64(len(data)-frontPrefix)) + len(data) - frontPrefix
	gain := frontBytes - len(encoder.scratch)
	if encoder.balance+gain < -dictionaryTokenBootstrapDebt {
		encoder.pending = encoder.pending[:0]
		encoder.pendingBytes = 0
		return nil, false
	}
	encoder.pendingBalanceGain = gain
	return encoder.scratch, true
}

func (encoder *dictionaryTokenEncoder) appendIdentifier(data []byte, offset, end int) {
	identifier := data[offset:end]
	if id := encoder.lookup(identifier); id != 0 {
		encoder.scratch = appendUvarint(encoder.scratch, id<<2|dictionaryAtomReference)
		return
	}
	if id := encoder.lookupPending(data, identifier); id != 0 {
		encoder.scratch = appendUvarint(encoder.scratch, id<<2|dictionaryAtomReference)
		return
	}
	length := end - offset
	canDefine := len(encoder.tokens)+len(encoder.pending) < maxSegmentDictionaryTokens &&
		encoder.tokenBytes+encoder.pendingBytes+length <= maxSegmentDictionaryTokenBytes
	if !canDefine {
		encoder.scratch = appendUvarint(encoder.scratch, uint64(length)<<2|dictionaryAtomLiteral)
		encoder.scratch = append(encoder.scratch, identifier...)
		return
	}
	id := uint64(len(encoder.tokens) + len(encoder.pending) + 1)
	encoder.scratch = appendUvarint(encoder.scratch, id<<2|dictionaryAtomDefinition)
	encoder.scratch = appendUvarint(encoder.scratch, uint64(length))
	encoder.scratch = append(encoder.scratch, identifier...)
	encoder.pending = append(encoder.pending, dictionaryTokenRange{offset: offset, length: length})
	encoder.pendingBytes += length
}

func (encoder *dictionaryTokenEncoder) lookup(identifier []byte) uint64 {
	if len(encoder.tokens) == 0 {
		return 0
	}
	return encoder.tokens[string(identifier)]
}

func (encoder *dictionaryTokenEncoder) lookupPending(data, identifier []byte) uint64 {
	for index, token := range encoder.pending {
		if bytes.Equal(data[token.offset:token.offset+token.length], identifier) {
			return uint64(len(encoder.tokens) + index + 1)
		}
	}
	return 0
}

func (encoder *dictionaryTokenEncoder) commit(data []byte) {
	if len(encoder.pending) != 0 && encoder.tokens == nil {
		encoder.tokens = make(map[string]uint64, maxSegmentDictionaryTokens)
	}
	for _, token := range encoder.pending {
		id := uint64(len(encoder.tokens) + 1)
		value := string(data[token.offset : token.offset+token.length])
		encoder.tokens[value] = id
		encoder.tokenBytes += token.length
	}
	encoder.balance += encoder.pendingBalanceGain
	encoder.pending = encoder.pending[:0]
	encoder.pendingBytes = 0
	encoder.pendingBalanceGain = 0
}

func decodeDictionaryTokens(
	reader *recordReader,
	kind DictKind,
	atomCount uint64,
	state *segmentDecodeState,
) ([]byte, error) {
	if !dictionaryTokenKind(kind) || atomCount == 0 || atomCount > maxDictionaryTokenAtoms {
		return nil, fmt.Errorf("dictionary kind %d has invalid token atom count %d", kind, atomCount)
	}
	data := make([]byte, 0, min(reader.Len(), maxRawChunkSize))
	for atom := uint64(0); atom < atomCount; atom++ {
		header, err := reader.readUvarint()
		if err != nil {
			return nil, fmt.Errorf("dictionary atom %d: %w", atom, err)
		}
		tag := header & dictionaryAtomTagMask
		value := header >> 2
		switch tag {
		case dictionaryAtomReference:
			if value == 0 || value > uint64(len(state.dictionaryTokens)) {
				return nil, fmt.Errorf("dictionary atom %d references token %d of %d", atom, value, len(state.dictionaryTokens))
			}
			data = append(data, state.dictionaryTokens[value-1]...)
		case dictionaryAtomDefinition:
			if value != uint64(len(state.dictionaryTokens)+1) || len(state.dictionaryTokens) >= maxSegmentDictionaryTokens {
				return nil, fmt.Errorf("dictionary atom %d defines non-canonical token %d", atom, value)
			}
			length, err := reader.readUvarint()
			if err != nil || length == 0 || length > uint64(reader.Len()) ||
				state.dictionaryTokenBytes+int(length) > maxSegmentDictionaryTokenBytes {
				return nil, fmt.Errorf("dictionary atom %d has invalid token length %d", atom, length)
			}
			token := append([]byte(nil), reader.data[reader.offset:reader.offset+int(length)]...)
			reader.offset += int(length)
			if !dictionaryIdentifier(token) {
				return nil, fmt.Errorf("dictionary atom %d defines a non-identifier token", atom)
			}
			state.dictionaryTokens = append(state.dictionaryTokens, token)
			state.dictionaryTokenBytes += len(token)
			data = append(data, token...)
		case dictionaryAtomSeparator:
			separator, ok := dictionarySeparator(value)
			if !ok {
				return nil, fmt.Errorf("dictionary atom %d has invalid separator %d", atom, value)
			}
			data = append(data, separator)
		case dictionaryAtomLiteral:
			if value == 0 || value > uint64(reader.Len()) {
				return nil, fmt.Errorf("dictionary atom %d has invalid literal length %d", atom, value)
			}
			data = append(data, reader.data[reader.offset:reader.offset+int(value)]...)
			reader.offset += int(value)
		}
		if len(data) > maxRawChunkSize {
			return nil, fmt.Errorf("dictionary token value exceeds %d bytes", maxRawChunkSize)
		}
	}
	if !utf8.Valid(data) || !dictionaryCodeName(data) {
		return nil, fmt.Errorf("dictionary token value is not a valid code name")
	}
	return data, nil
}

func dictionaryTokenKind(kind DictKind) bool {
	switch kind {
	case DictOwner, DictClass, DictStack, DictLogSource, DictStableSymbol, DictOperation:
		return true
	default:
		return false
	}
}

func dictionaryCodeName(data []byte) bool {
	for _, value := range data {
		switch value {
		case '.', '$', '/', '#':
			return true
		}
	}
	return false
}

func dictionaryAtomCount(data []byte) int {
	atoms := 0
	for offset := 0; offset < len(data); {
		atoms++
		if !dictionaryIdentifierByte(data[offset]) {
			offset++
			continue
		}
		offset++
		for offset < len(data) && dictionaryIdentifierByte(data[offset]) {
			offset++
		}
	}
	return atoms
}

func dictionaryIdentifier(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	for _, value := range data {
		if !dictionaryIdentifierByte(value) {
			return false
		}
	}
	return true
}

func dictionaryIdentifierByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' || value == '_' || value >= utf8.RuneSelf
}

func dictionarySeparatorCode(value byte) (int, bool) {
	for index := range len(dictionarySeparators) {
		if dictionarySeparators[index] == value {
			return index, true
		}
	}
	return 0, false
}

func dictionarySeparator(code uint64) (byte, bool) {
	if code >= uint64(len(dictionarySeparators)) {
		return 0, false
	}
	return dictionarySeparators[code], true
}
