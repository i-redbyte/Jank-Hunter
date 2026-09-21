package retrace

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

type responseReader struct {
	*bytes.Reader
	items int
	err   error
}

func (r *responseReader) integer() uint32 {
	var value uint32
	if r.err == nil {
		r.err = binary.Read(r.Reader, binary.BigEndian, &value)
	}
	return value
}
func (r *responseReader) count() int {
	value := r.integer()
	if value > maxItems || int(value) > r.items {
		r.err = errors.New("Retrace response cardinality limit exceeded")
		return 0
	}
	r.items -= int(value)
	return int(value)
}
func (r *responseReader) byteValue() byte {
	if r.err != nil {
		return 0
	}
	value, err := r.ReadByte()
	r.err = err
	return value
}
func (r *responseReader) boolean() bool {
	value := r.byteValue()
	if value > 1 {
		r.err = errors.New("invalid Retrace boolean")
	}
	return value == 1
}
func (r *responseReader) text() string {
	size := r.integer()
	if r.err != nil {
		return ""
	}
	if size > maxString || uint64(size) > uint64(r.Len()) {
		r.err = errors.New("invalid Retrace string length")
		return ""
	}
	data := make([]byte, int(size))
	_, r.err = io.ReadFull(r.Reader, data)
	if !utf8.Valid(data) {
		r.err = errors.New("invalid UTF-8 in Retrace response")
	}
	return string(data)
}

func decode(data []byte, requests []Request) ([]Result, error) {
	if len(data) > maxOutput {
		return nil, errors.New("Retrace response byte limit exceeded")
	}
	in := responseReader{Reader: bytes.NewReader(data), items: 4 * maxItems}
	if in.integer() != protocolMagic {
		return nil, errors.New("unsupported Retrace response protocol")
	}
	count := in.count()
	if count != len(requests) {
		return nil, errors.New("Retrace response count mismatch")
	}
	results := make([]Result, 0, count)
	for _, request := range requests {
		kind := in.byteValue()
		switch request.(type) {
		case StackRequest:
			if kind != 1 {
				return nil, errors.New("Retrace response kind mismatch")
			}
			groups := make([]FrameGroup, in.count())
			for index := range groups {
				groups[index].Ambiguous = in.boolean()
				alternatives := make([][]string, in.count())
				for candidate := range alternatives {
					chain := make([]string, in.count())
					for frame := range chain {
						chain[frame] = in.text()
					}
					alternatives[candidate] = chain
				}
				groups[index].Alternatives = alternatives
			}
			results = append(results, StackResult{Groups: groups})
		case FieldRequest:
			if kind != 2 {
				return nil, errors.New("Retrace response kind mismatch")
			}
			result := FieldResult{Ambiguous: in.boolean()}
			result.Alternatives = make([]FieldCandidate, in.count())
			for index := range result.Alternatives {
				candidate := FieldCandidate{Known: in.boolean(), Owner: in.text(), Name: in.text(), Descriptor: in.text()}
				if candidate.Owner == "" || candidate.Name == "" || candidate.Known && candidate.Descriptor == "" {
					in.err = errors.New("incomplete Retrace field candidate")
				}
				result.Alternatives[index] = candidate
			}
			results = append(results, result)
		default:
			return nil, errors.New("unsupported Retrace request")
		}
		if in.err != nil {
			return nil, fmt.Errorf("invalid Retrace response: %w", in.err)
		}
	}
	if in.err != nil {
		return nil, in.err
	}
	if in.Len() != 0 {
		return nil, errors.New("trailing Retrace response data")
	}
	return results, nil
}
