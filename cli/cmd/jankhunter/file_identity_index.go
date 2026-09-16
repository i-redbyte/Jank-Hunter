package main

import "os"

// The native identity is exact, not a content hash. Unknown identities retain
// os.SameFile semantics, including comparisons against previously indexed files.
type physicalFileID struct {
	volume uint64
	object [fileIdentityObjectBytes]byte
}

type fileIdentityIndex struct {
	files        []canonicalLogInput
	byID         map[physicalFileID]int
	unkeyed      []int
	firstIndexed bool
	identity     func(string, os.FileInfo) (physicalFileID, bool)
	sameFile     func(os.FileInfo, os.FileInfo) bool
}

func newFileIdentityIndex(capacity int, sameFile func(os.FileInfo, os.FileInfo) bool) *fileIdentityIndex {
	return &fileIdentityIndex{files: make([]canonicalLogInput, 0, capacity), identity: nativeFileIdentity, sameFile: sameFile}
}

func (s *fileIdentityIndex) record(position int, id physicalFileID, known bool) {
	if known {
		if s.byID == nil {
			s.byID = make(map[physicalFileID]int)
		}
		s.byID[id] = position
	} else {
		s.unkeyed = append(s.unkeyed, position)
	}
}

func (s *fileIdentityIndex) find(path string, info os.FileInfo) (int, physicalFileID, bool) {
	if len(s.files) == 0 {
		return -1, physicalFileID{}, false
	}
	// A singleton needs no identity comparison (or extra Windows metadata handle).
	if !s.firstIndexed && len(s.files) > 0 {
		id, known := s.identity(s.files[0].path, s.files[0].info)
		s.record(0, id, known)
		s.firstIndexed = true
	}
	id, known := s.identity(path, info)
	if known {
		if position, ok := s.byID[id]; ok {
			return position, id, known
		}
		for _, position := range s.unkeyed {
			if s.sameFile(s.files[position].info, info) {
				return position, id, known
			}
		}
	} else {
		for position := range s.files {
			if s.sameFile(s.files[position].info, info) {
				return position, id, known
			}
		}
	}
	return -1, id, known
}

func (s *fileIdentityIndex) add(path string, info os.FileInfo) {
	if len(s.files) == 0 {
		s.files = append(s.files, canonicalLogInput{path: path, info: info})
		return
	}
	position, id, known := s.find(path, info)
	if position >= 0 {
		return
	}
	s.record(len(s.files), id, known)
	s.files = append(s.files, canonicalLogInput{path: path, info: info})
}
