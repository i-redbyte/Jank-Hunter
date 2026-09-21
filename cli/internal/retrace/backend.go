// Package retrace runs the pinned official R8 engine once per bounded offline batch.
package retrace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const Version = "9.0.32"
const JarSHA256 = "a561da8b3d2419f3c0b1936546f6a756790112653f834563f65cebc4fce0bddd"
const protocolMagic uint32 = 0x4a485231
const MaxRequests = 16384
const maxItems = MaxRequests
const maxString = 4 << 20
const maxInput = 16 << 20
const maxOutput = 64 << 20

// Request alternatives preserve symbol provenance: original ASM labels never enter this API.
type Request interface{ retraceRequest() }
type StackRequest struct{ Text string }

func (StackRequest) retraceRequest() {}

// Owner is the declaring runtime class, not a guessed runtime subclass. Descriptor may be
// empty when HPROF has lost the declared reference type; all alternatives then remain visible.
type FieldRequest struct{ Owner, Name, Descriptor string }

func (FieldRequest) retraceRequest() {}

type Result interface{ retraceResult() }
type StackResult struct{ Groups []FrameGroup }

func (StackResult) retraceResult() {}

type FrameGroup struct {
	Ambiguous    bool
	Alternatives [][]string // Each alternative is an ordered inline chain, never a single chosen frame.
}
type FieldResult struct {
	Ambiguous    bool
	Alternatives []FieldCandidate
}

func (FieldResult) retraceResult() {}

type FieldCandidate struct {
	Known                   bool
	Owner, Name, Descriptor string
}

type Backend struct{ Java, R8Jar, BridgeJar string }

func (b Backend) Run(ctx context.Context, mapping string, mappingSHA256 string, requests []Request) ([]Result, error) {
	// An empty batch still validates the mapping through the official command API.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	input, err := encode(requests)
	if err != nil {
		return nil, err
	}
	if err := verifyJar(ctx, b.R8Jar); err != nil {
		return nil, err
	}
	snapshot, cleanup, err := snapshotMapping(ctx, mapping, mappingSHA256)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	java := b.Java
	if java == "" {
		java = "java"
		if home := os.Getenv("JAVA_HOME"); home != "" {
			java = filepath.Join(home, "bin", "java")
		}
	}
	cmd := exec.CommandContext(ctx, java, "-Xmx512m", "-XX:MaxMetaspaceSize=128m", "-XX:ActiveProcessorCount=2",
		"-Dfile.encoding=UTF-8", "-Duser.language=en", "-cp", b.BridgeJar+string(os.PathListSeparator)+b.R8Jar,
		"io.jankhunter.retrace.Main", snapshot)
	// Ambient JVM injection could override the memory bound or modify the pinned classpath.
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		if name != "JAVA_TOOL_OPTIONS" && name != "JDK_JAVA_OPTIONS" && name != "_JAVA_OPTIONS" {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	output, stderr := limitedBuffer{limit: maxOutput}, limitedBuffer{limit: 64 << 10}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(input), &output, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("official Retrace deadline: %w", ctx.Err())
		}
		return nil, fmt.Errorf("official Retrace %s failed: %w: %s", Version, err, strings.TrimSpace(stderr.String()))
	}
	return decode(output.Bytes(), requests)
}

func verifyJar(ctx context.Context, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open bundled official Retrace %s: %w", Version, err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, &contextReader{ctx: ctx, reader: io.LimitReader(file, (32<<20)+1)})
	if err != nil {
		return err
	}
	if size > 32<<20 || hex.EncodeToString(hash.Sum(nil)) != JarSHA256 {
		return errors.New("bundled official Retrace SHA-256 mismatch")
	}
	return nil
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Len() int       { return b.buffer.Len() }
func (b *limitedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedBuffer) String() string { return b.buffer.String() }

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.Len() {
		return 0, errors.New("Retrace batch byte limit exceeded")
	}
	return b.buffer.Write(data)
}

func encode(requests []Request) ([]byte, error) {
	if len(requests) > maxItems {
		return nil, errors.New("too many Retrace requests")
	}
	out := limitedBuffer{limit: maxInput}
	writeInt := func(value uint32) error { return binary.Write(&out, binary.BigEndian, value) }
	writeString := func(value string) error {
		if len(value) > maxString || !utf8.ValidString(value) {
			return errors.New("invalid or oversized Retrace string")
		}
		if err := writeInt(uint32(len(value))); err != nil {
			return err
		}
		_, err := out.Write([]byte(value))
		return err
	}
	if err := writeInt(protocolMagic); err != nil {
		return nil, err
	}
	if err := writeInt(uint32(len(requests))); err != nil {
		return nil, err
	}
	for _, request := range requests {
		var values []string
		var kind byte
		switch value := request.(type) {
		case StackRequest:
			kind = 1
			values = []string{value.Text}
		case FieldRequest:
			if value.Owner == "" || value.Name == "" {
				return nil, errors.New("field retrace requires declaring owner and name")
			}
			kind = 2
			values = []string{value.Owner, value.Name, value.Descriptor}
		default:
			return nil, errors.New("unsupported Retrace request")
		}
		if _, err := out.Write([]byte{kind}); err != nil {
			return nil, err
		}
		for _, value := range values {
			if err := writeString(value); err != nil {
				return nil, err
			}
		}
	}
	return out.Bytes(), nil
}
