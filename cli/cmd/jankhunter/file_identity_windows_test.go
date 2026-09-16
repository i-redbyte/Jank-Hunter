package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsNativeIdentityPreservesFullIDAndLongPaths(t *testing.T) {
	dir := t.TempDir()
	for len(dir) < 280 {
		dir = filepath.Join(dir, strings.Repeat("directory", 5))
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	first, alias, other := filepath.Join(dir, "first"), filepath.Join(dir, "alias"), filepath.Join(dir, "other")
	for _, p := range []string{first, other} {
		if err := os.WriteFile(p, []byte("same"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(first, alias); err != nil {
		t.Skipf("hardlinks unsupported: %v", err)
	}
	ids := make([]physicalFileID, 0, 3)
	for _, p := range []string{first, alias, other} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		id, known := nativeFileIdentity(p, info)
		if !known {
			t.Skip("FileIdInfo unsupported on this filesystem; fallback is tested separately")
		}
		ids = append(ids, id)
	}
	if ids[0] != ids[1] || ids[0] == ids[2] {
		t.Fatal("Windows physical identities disagree with hardlinks/distinct files")
	}
}
