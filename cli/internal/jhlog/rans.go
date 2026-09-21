package jhlog

import (
	"encoding/binary"
	"fmt"
)

const (
	ransAlphabetSize      = 256
	ransScaleBits         = 12
	ransScale             = 1 << ransScaleBits
	ransLowerBound        = uint64(1 << 23)
	ransRenormalizeBase   = (ransLowerBound >> ransScaleBits) << 8
	ransMinInputBytes     = 32
	ransMaxBytesPerSymbol = 2
)

type ransEncoder struct {
	counts      [ransAlphabetSize]int
	frequencies [ransAlphabetSize]int
	starts      [ransAlphabetSize]int
	reverse     []byte
	frame       []byte
}

func (codec *ransEncoder) encode(input []byte) ([]byte, bool) {
	if len(input) < ransMinInputBytes {
		return nil, false
	}
	clear(codec.counts[:])
	symbolCount := 0
	for _, symbol := range input {
		if codec.counts[symbol] == 0 {
			symbolCount++
		}
		codec.counts[symbol]++
	}
	codec.normalize(len(input), symbolCount)
	required := len(input)*ransMaxBytesPerSymbol + 4
	if cap(codec.reverse) < required {
		codec.reverse = make([]byte, required)
	} else {
		codec.reverse = codec.reverse[:required]
	}
	pointer := len(codec.reverse)
	state := ransLowerBound
	for index := len(input) - 1; index >= 0; index-- {
		symbol := input[index]
		frequency := uint64(codec.frequencies[symbol])
		maximum := ransRenormalizeBase * frequency
		for state >= maximum {
			pointer--
			codec.reverse[pointer] = byte(state)
			state >>= 8
		}
		state = ((state / frequency) << ransScaleBits) + state%frequency + uint64(codec.starts[symbol])
	}
	pointer -= 4
	binary.LittleEndian.PutUint32(codec.reverse[pointer:pointer+4], uint32(state))

	codec.frame = codec.frame[:0]
	codec.frame = appendUvarint(codec.frame, uint64(symbolCount))
	for symbol, frequency := range codec.frequencies {
		if frequency == 0 {
			continue
		}
		codec.frame = append(codec.frame, byte(symbol))
		codec.frame = appendUvarint(codec.frame, uint64(frequency))
	}
	codec.frame = append(codec.frame, codec.reverse[pointer:]...)
	if len(codec.frame)+uvarintSize(uint64(len(codec.frame))) >= len(input) {
		return nil, false
	}
	return codec.frame, true
}

func (codec *ransEncoder) normalize(length, symbolCount int) {
	clear(codec.frequencies[:])
	remainingCount := length
	remainingFrequency := ransScale
	remainingSymbols := symbolCount
	cumulative := 0
	for symbol, count := range codec.counts {
		codec.starts[symbol] = cumulative
		if count == 0 {
			continue
		}
		frequency := remainingFrequency
		if remainingSymbols != 1 {
			frequency = (count*remainingFrequency + remainingCount/2) / remainingCount
			frequency = max(1, min(frequency, remainingFrequency-remainingSymbols+1))
		}
		codec.frequencies[symbol] = frequency
		cumulative += frequency
		remainingCount -= count
		remainingFrequency -= frequency
		remainingSymbols--
	}
	if cumulative != ransScale {
		panic("rANS normalization did not fill the probability scale")
	}
}

func decodeRANS(frame []byte, decodedLength int) ([]byte, error) {
	if decodedLength < ransMinInputBytes {
		return nil, fmt.Errorf("rANS decoded length %d is below minimum %d", decodedLength, ransMinInputBytes)
	}
	reader := recordReader{data: frame}
	symbolCount, err := reader.readUvarint()
	if err != nil {
		return nil, fmt.Errorf("rANS symbol count: %w", err)
	}
	if symbolCount == 0 || symbolCount > ransAlphabetSize {
		return nil, fmt.Errorf("rANS symbol count %d is outside 1..%d", symbolCount, ransAlphabetSize)
	}
	var frequencies [ransAlphabetSize]uint64
	var starts [ransAlphabetSize]uint64
	var decodeTable [ransScale]byte
	previousSymbol := -1
	cumulative := uint64(0)
	for index := uint64(0); index < symbolCount; index++ {
		if reader.Len() == 0 {
			return nil, fmt.Errorf("rANS symbol %d: unexpected end", index)
		}
		symbol := int(reader.data[reader.offset])
		reader.offset++
		if symbol <= previousSymbol {
			return nil, fmt.Errorf("rANS symbols are not strictly increasing")
		}
		frequency, err := reader.readUvarint()
		if err != nil {
			return nil, fmt.Errorf("rANS symbol %d frequency: %w", symbol, err)
		}
		if frequency == 0 || frequency > ransScale-cumulative {
			return nil, fmt.Errorf("rANS symbol %d frequency %d exceeds remaining scale", symbol, frequency)
		}
		starts[symbol] = cumulative
		frequencies[symbol] = frequency
		for slot := cumulative; slot < cumulative+frequency; slot++ {
			decodeTable[slot] = byte(symbol)
		}
		cumulative += frequency
		previousSymbol = symbol
	}
	if cumulative != ransScale {
		return nil, fmt.Errorf("rANS frequency sum %d differs from %d", cumulative, ransScale)
	}
	if reader.Len() < 4 {
		return nil, fmt.Errorf("rANS state is truncated")
	}
	state := uint64(binary.LittleEndian.Uint32(reader.data[reader.offset : reader.offset+4]))
	reader.offset += 4
	if state < ransLowerBound || state >= ransLowerBound<<8 {
		return nil, fmt.Errorf("rANS initial state %d is outside canonical range", state)
	}
	decoded := make([]byte, decodedLength)
	for index := range decoded {
		slot := state & (ransScale - 1)
		symbol := decodeTable[slot]
		decoded[index] = symbol
		state = frequencies[symbol]*(state>>ransScaleBits) + slot - starts[symbol]
		for state < ransLowerBound {
			if reader.Len() == 0 {
				return nil, fmt.Errorf("rANS stream is truncated at output byte %d", index)
			}
			state = (state << 8) | uint64(reader.data[reader.offset])
			reader.offset++
		}
	}
	if state != ransLowerBound || reader.Len() != 0 {
		return nil, fmt.Errorf("rANS stream has non-canonical terminal state or trailing bytes")
	}
	return decoded, nil
}

func appendUvarint(destination []byte, value uint64) []byte {
	for value >= 0x80 {
		destination = append(destination, byte(value)|0x80)
		value >>= 7
	}
	return append(destination, byte(value))
}
