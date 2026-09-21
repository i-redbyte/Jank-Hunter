package retrace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

const maxMappingBytes int64 = 1 << 30

// The JVM consumes a private snapshot whose bytes match the mapping already parsed
// and validated by the caller. Replacing the user's original file cannot change it.
func snapshotMapping(ctx context.Context, path, expectedSHA256 string) (string, func(), error) {
	digest, err := hex.DecodeString(expectedSHA256)
	if err != nil || len(digest) != sha256.Size {
		return "", nil, fmt.Errorf("Retrace requires a validated mapping SHA-256")
	}
	source, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxMappingBytes {
		return "", nil, fmt.Errorf("Retrace mapping must be a regular file within %d bytes", maxMappingBytes)
	}
	target, err := os.CreateTemp("", "jankhunter-retrace-mapping-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(target.Name()) }
	succeeded := false
	defer func() {
		if !succeeded {
			_ = target.Close()
			cleanup()
		}
	}()
	hash := sha256.New()
	reader := &contextReader{ctx: ctx, reader: io.LimitReader(source, maxMappingBytes+1)}
	size, err := io.CopyBuffer(io.MultiWriter(target, hash), reader, make([]byte, 64<<10))
	if err != nil {
		return "", nil, err
	}
	if size > maxMappingBytes || hex.EncodeToString(hash.Sum(nil)) != expectedSHA256 {
		return "", nil, fmt.Errorf("Retrace mapping SHA-256 changed since validation")
	}
	if err := target.Close(); err != nil {
		return "", nil, err
	}
	succeeded = true
	return target.Name(), cleanup, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
