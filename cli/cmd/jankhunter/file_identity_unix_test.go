//go:build !windows && !plan9

package main

import (
	"os"
	"testing"
)

func TestNativeFileIdentityUsesExistingStatWithoutFilesystemRead(t *testing.T) {
	paths := identityTestFiles(t, 1)
	info, err := os.Stat(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	before, known := nativeFileIdentity(paths[0], info)
	if !known {
		t.Fatal("native stat identity unavailable")
	}
	if err = os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	after, known := nativeFileIdentity(paths[0], info)
	if !known || after != before {
		t.Fatal("identity extraction reread a deleted path")
	}
}
