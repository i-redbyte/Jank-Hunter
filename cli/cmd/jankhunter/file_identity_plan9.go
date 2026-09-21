package main

import "os"

// Plan 9 and FileInfo implementations without a supported identity use SameFile.
const fileIdentityObjectBytes = 8

func nativeFileIdentity(_ string, _ os.FileInfo) (physicalFileID, bool) {
	return physicalFileID{}, false
}
