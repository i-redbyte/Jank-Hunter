package jhlog

import (
	"os"
	"path/filepath"
	"testing"
)

func readWireGolden(t *testing.T, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("..", "..", "..", "wire", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
