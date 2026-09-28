package sessioninput

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	maxContainerArtifacts           = 4096
	maxDirectoryDepth               = 4
	maxArchiveEntryNameBytes        = 1024
	maxArchiveEntryBytes     uint64 = 4 << 30
	maxArchiveTotalBytes     uint64 = 8 << 30
	maxCompressionRatio      uint64 = 200
)

// Resolved contains ordinary filesystem paths so existing streaming readers stay allocation-free.
// Archive artifacts live in private temporary directories until Close is called.
type Resolved struct {
	Logs      []string
	HeapDumps []string
	Sources   []string
	// MultiRunEpochs identifies one connected process lifetime spanning runtime reconfiguration.
	MultiRunEpochs bool
	temporary      []string
	closed         bool
}

// Resolve accepts standalone JHLOG files, connected process-lifetime directories, and session ZIP archives.
func Resolve(inputs []string) (*Resolved, error) {
	resolved := &Resolved{}
	seenSources := make(map[string]struct{}, len(inputs))
	seenLogs := make(map[string]struct{})
	seenHeaps := make(map[string]struct{})
	for _, input := range inputs {
		canonical, info, err := canonicalInput(input)
		if err != nil {
			resolved.Close()
			return nil, err
		}
		if _, exists := seenSources[canonical]; exists {
			continue
		}
		seenSources[canonical] = struct{}{}
		resolved.Sources = append(resolved.Sources, canonical)
		var logs, heaps []string
		switch {
		case info.IsDir():
			logs, heaps, err = resolveDirectory(canonical)
		case info.Mode().IsRegular() && strings.HasSuffix(strings.ToLower(canonical), ".jhlog.zip"):
			logs, heaps, err = resolved.resolveArchive(canonical)
		case info.Mode().IsRegular():
			// Preserve the historical contract: content validation, rather than a filename
			// extension, decides whether a standalone file is a readable JHLOG.
			logs = []string{canonical}
		default:
			err = fmt.Errorf("log input %q must be a .jhlog file, session directory, or .jhlog.zip archive", canonical)
		}
		if err != nil {
			resolved.Close()
			return nil, err
		}
		multipleRuns, err := validateConnectedRuns(canonical, logs)
		if err != nil {
			resolved.Close()
			return nil, err
		}
		resolved.MultiRunEpochs = resolved.MultiRunEpochs || multipleRuns
		appendUnique(&resolved.Logs, seenLogs, logs)
		appendUnique(&resolved.HeapDumps, seenHeaps, heaps)
	}
	sort.Strings(resolved.Logs)
	sort.Strings(resolved.HeapDumps)
	return resolved, nil
}

// Close removes only temporary directories created while extracting archives.
func (r *Resolved) Close() error {
	if r == nil || r.closed {
		return nil
	}
	r.closed = true
	var first error
	for _, temporary := range r.temporary {
		if err := os.RemoveAll(temporary); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func canonicalInput(input string) (string, fs.FileInfo, error) {
	absolute, err := filepath.Abs(filepath.Clean(input))
	if err != nil {
		return "", nil, fmt.Errorf("resolve log path %q: %w", input, err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", nil, fmt.Errorf("resolve log path %q: %w", input, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", nil, fmt.Errorf("stat log path %q: %w", canonical, err)
	}
	return filepath.Clean(canonical), info, nil
}

func resolveDirectory(root string) ([]string, []string, error) {
	logs := make([]string, 0, 4)
	heaps := make([]string, 0, 1)
	artifacts := 0
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if relative != "." && len(strings.Split(relative, string(filepath.Separator))) > maxDirectoryDepth {
			return fmt.Errorf("session directory depth exceeds %d", maxDirectoryDepth)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("session directory contains symlink %q", current)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("session directory contains non-regular artifact %q", current)
		}
		lower := strings.ToLower(entry.Name())
		if !strings.HasSuffix(lower, ".jhlog") && !strings.HasSuffix(lower, ".hprof") {
			return nil
		}
		artifacts++
		if artifacts > maxContainerArtifacts {
			return fmt.Errorf("session directory contains more than %d artifacts", maxContainerArtifacts)
		}
		if strings.HasSuffix(lower, ".jhlog") {
			logs = append(logs, current)
		} else {
			heaps = append(heaps, current)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read session directory %q: %w", root, err)
	}
	if len(logs) == 0 {
		return nil, nil, fmt.Errorf("session directory %q contains no .jhlog files", root)
	}
	return logs, heaps, nil
}

func (r *Resolved) resolveArchive(archivePath string) ([]string, []string, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open session archive %q: %w", archivePath, err)
	}
	defer archive.Close()
	if len(archive.File) == 0 || len(archive.File) > maxContainerArtifacts {
		return nil, nil, fmt.Errorf("session archive entry count must be between 1 and %d", maxContainerArtifacts)
	}
	var total uint64
	names := make(map[string]struct{}, len(archive.File))
	for _, entry := range archive.File {
		if err := validateArchiveEntry(entry, names); err != nil {
			return nil, nil, fmt.Errorf("session archive %q: %w", archivePath, err)
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if entry.UncompressedSize64 > maxArchiveEntryBytes || total > maxArchiveTotalBytes-entry.UncompressedSize64 {
			return nil, nil, fmt.Errorf("session archive exceeds bounded uncompressed size")
		}
		total += entry.UncompressedSize64
		if entry.UncompressedSize64 > 0 &&
			(entry.CompressedSize64 == 0 || entry.UncompressedSize64/entry.CompressedSize64 > maxCompressionRatio) {
			return nil, nil, fmt.Errorf("session archive entry %q exceeds compression ratio limit", entry.Name)
		}
	}
	temporary, err := os.MkdirTemp("", "jankhunter-session-")
	if err != nil {
		return nil, nil, fmt.Errorf("create session archive workspace: %w", err)
	}
	r.temporary = append(r.temporary, temporary)
	logs := make([]string, 0, len(archive.File))
	heaps := make([]string, 0, 1)
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(temporary, filepath.FromSlash(entry.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return nil, nil, err
		}
		if err := extractEntry(entry, target); err != nil {
			return nil, nil, fmt.Errorf("extract session archive entry %q: %w", entry.Name, err)
		}
		if strings.HasSuffix(strings.ToLower(entry.Name), ".jhlog") {
			logs = append(logs, target)
		} else {
			heaps = append(heaps, target)
		}
	}
	if len(logs) == 0 {
		return nil, nil, fmt.Errorf("session archive %q contains no .jhlog files", archivePath)
	}
	return logs, heaps, nil
}

func validateArchiveEntry(entry *zip.File, names map[string]struct{}) error {
	name := entry.Name
	if len(name) == 0 || len(name) > maxArchiveEntryNameBytes || strings.Contains(name, "\\") || path.IsAbs(name) {
		return fmt.Errorf("unsafe archive entry name %q", name)
	}
	clean := path.Clean(name)
	if clean != name || clean == "." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("unsafe archive entry name %q", name)
	}
	if _, duplicate := names[name]; duplicate {
		return fmt.Errorf("duplicate archive entry %q", name)
	}
	names[name] = struct{}{}
	if entry.Flags&1 != 0 {
		return fmt.Errorf("encrypted archive entry %q is unsupported", name)
	}
	mode := entry.Mode()
	if !entry.FileInfo().IsDir() && !mode.IsRegular() {
		return fmt.Errorf("archive entry %q is not a regular file", name)
	}
	if entry.FileInfo().IsDir() {
		return nil
	}
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("unsafe archive entry layout %q", name)
	}
	lower := strings.ToLower(parts[1])
	if !strings.HasSuffix(lower, ".jhlog") && !strings.HasSuffix(lower, ".hprof") {
		return fmt.Errorf("unsupported session archive artifact %q", name)
	}
	return nil
}

func extractEntry(entry *zip.File, target string) error {
	source, err := entry.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(destination, io.LimitReader(source, int64(entry.UncompressedSize64)+1))
	closeErr := destination.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if uint64(written) != entry.UncompressedSize64 {
		return fmt.Errorf("uncompressed size mismatch")
	}
	return nil
}

type processAnchor struct {
	index int
	name  string
}

func validateConnectedRuns(source string, logs []string) (bool, error) {
	parents := make([]int, len(logs))
	sizes := make([]int, len(logs))
	byRun := make(map[jhlog.ID128]int, len(logs))
	byProcess := make(map[jhlog.ID128]processAnchor, len(logs))
	for index, logPath := range logs {
		header, err := jhlog.ReadSessionHeader(logPath)
		if err != nil {
			return false, fmt.Errorf("validate %q from %q: %w", logPath, source, err)
		}
		parents[index] = index
		sizes[index] = 1
		if previous, found := byRun[header.RunID]; found {
			unionRuns(parents, sizes, index, previous)
		} else {
			byRun[header.RunID] = index
		}
		if previous, found := byProcess[header.ProcessInstanceID]; found {
			if previous.name != header.ProcessName {
				return false, fmt.Errorf("session input %q has conflicting process identity", source)
			}
			unionRuns(parents, sizes, index, previous.index)
		} else {
			byProcess[header.ProcessInstanceID] = processAnchor{index, header.ProcessName}
		}
	}
	for index := 1; index < len(logs); index++ {
		if findRun(parents, index) != findRun(parents, 0) {
			return false, fmt.Errorf("session input %q contains multiple run IDs from unrelated processes", source)
		}
	}
	return len(byRun) > 1, nil
}

func findRun(parents []int, index int) int {
	for parents[index] != index {
		parents[index] = parents[parents[index]]
		index = parents[index]
	}
	return index
}

func unionRuns(parents, sizes []int, left, right int) {
	left = findRun(parents, left)
	right = findRun(parents, right)
	if left == right {
		return
	}
	if sizes[left] < sizes[right] {
		left, right = right, left
	}
	parents[right] = left
	sizes[left] += sizes[right]
}

func appendUnique(target *[]string, seen map[string]struct{}, values []string) {
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		*target = append(*target, value)
	}
}
