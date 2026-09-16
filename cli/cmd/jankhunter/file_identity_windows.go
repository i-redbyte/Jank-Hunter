package main

import (
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var fileInformationByHandleEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileInformationByHandleEx")

// FILE_ID_INFO combines the volume serial number and full 128-bit file ID;
// the latter avoids truncating ReFS identities to the older 64-bit file index.
// https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_id_info
const fileIdentityObjectBytes = 16

func nativeFileIdentity(path string, _ os.FileInfo) (physicalFileID, bool) {
	if fileInformationByHandleEx.Find() != nil {
		return physicalFileID{}, false
	}
	if !strings.HasPrefix(path, `\\?\`) {
		if strings.HasPrefix(path, `\\`) {
			path = `\\?\UNC\` + path[2:]
		} else {
			path = `\\?\` + path
		}
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return physicalFileID{}, false
	}
	// No data-read access or exclusive sharing is needed for identity metadata.
	handle, err := syscall.CreateFile(name, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return physicalFileID{}, false
	}
	defer syscall.CloseHandle(handle)
	var id physicalFileID
	const fileIDInfo = 18
	ok, _, _ := fileInformationByHandleEx.Call(uintptr(handle), fileIDInfo, uintptr(unsafe.Pointer(&id)), unsafe.Sizeof(id))
	return id, ok != 0
}
