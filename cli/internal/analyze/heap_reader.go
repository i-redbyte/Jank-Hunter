package analyze

import (
	"encoding/binary"
	"fmt"
	"io"
)

func (r *hprofReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.read += uint64(n)
	return n, err
}

func (r *hprofReader) remaining() uint64 {
	if r.read >= r.limit {
		return 0
	}
	return r.limit - r.read
}

func (r *hprofReader) require(n uint64) error {
	if n > r.remaining() {
		return fmt.Errorf("need %d bytes, only %d remain in the HPROF record", n, r.remaining())
	}
	return nil
}

func (r *hprofReader) readByte() (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(r, b[:])
	return b[0], err
}

func (r *hprofReader) readU2() (uint16, error) {
	var b [2]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b[:]), nil
}

func (r *hprofReader) readU4() (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

func (r *hprofReader) readID(idSize int) (uint64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[8-idSize:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(buf[:]), nil
}

func (r *hprofReader) skip(n uint64) error {
	if n == 0 {
		return nil
	}
	if err := r.require(n); err != nil {
		return err
	}
	count, err := checkedInt64(n, "HPROF skip length")
	if err != nil {
		return err
	}
	if _, err := io.CopyN(io.Discard, r, count); err != nil {
		return fmt.Errorf("skip %d HPROF bytes: %w", n, err)
	}
	return nil
}

func checkedInt(value uint64, description string) (int, error) {
	maxInt := uint64(^uint(0) >> 1)
	if value > maxInt {
		return 0, fmt.Errorf("%s %d overflows int", description, value)
	}
	return int(value), nil
}

func checkedInt64(value uint64, description string) (int64, error) {
	const maxInt64 = uint64(^uint64(0) >> 1)
	if value > maxInt64 {
		return 0, fmt.Errorf("%s %d overflows int64", description, value)
	}
	return int64(value), nil
}

func checkedAddUint64(left, right uint64, description string) (uint64, error) {
	if ^uint64(0)-left < right {
		return 0, fmt.Errorf("%s overflows uint64: %d + %d", description, left, right)
	}
	return left + right, nil
}

func checkedMulUint64(left, right uint64, description string) (uint64, error) {
	if left != 0 && right > ^uint64(0)/left {
		return 0, fmt.Errorf("%s overflows uint64: %d * %d", description, left, right)
	}
	return left * right, nil
}

func bytesToKB(value uint64) uint64 {
	kilobytes := value / 1024
	if value%1024 != 0 {
		kilobytes++
	}
	return kilobytes
}

func readU4(reader io.Reader) (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(reader, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

func readU8(reader io.Reader) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(reader, b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(b[:]), nil
}
