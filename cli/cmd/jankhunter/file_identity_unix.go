//go:build !windows && !plan9

package main

import (
	"encoding/binary"
	"os"
	"syscall"
)

// Matches os.SameFile's device/inode identity using the already obtained stat.
const fileIdentityObjectBytes = 8

func nativeFileIdentity(_ string, info os.FileInfo) (physicalFileID, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return physicalFileID{}, false
	}
	id := physicalFileID{volume: uint64(stat.Dev)}
	binary.LittleEndian.PutUint64(id.object[:8], uint64(stat.Ino))
	return id, true
}
