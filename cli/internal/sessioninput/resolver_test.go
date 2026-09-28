package sessioninput

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestResolveDirectoryCollectsOneRunAndHeapDump(t *testing.T) {
	root := t.TempDir()
	process := filepath.Join(root, "process")
	if err := os.Mkdir(process, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(process, "first.jhlog")
	if err := jhlog.WriteSample(first); err != nil {
		t.Fatal(err)
	}
	copyFile(t, first, filepath.Join(process, "second.jhlog"))
	heap := filepath.Join(process, "retained-1-Example-1.hprof")
	if err := os.WriteFile(heap, []byte("heap"), 0o600); err != nil {
		t.Fatal(err)
	}

	resolved, err := Resolve([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	defer resolved.Close()
	if len(resolved.Logs) != 2 || len(resolved.HeapDumps) != 1 {
		t.Fatalf("resolved = logs:%v heaps:%v", resolved.Logs, resolved.HeapDumps)
	}
}

func TestResolveArchiveExtractsBoundedArtifactsAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "sample.jhlog")
	if err := jhlog.WriteSample(log); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "20270101T000000.000Z_0_0123456789abcdef0123456789abcdef.jhlog.zip")
	writeArchive(t, archive, []archiveEntry{
		{name: "process/sample.jhlog", source: log},
		{name: "process/retained-1-Example-1.hprof", data: []byte("heap")},
	})

	resolved, err := Resolve([]string{archive})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Logs) != 1 || len(resolved.HeapDumps) != 1 {
		t.Fatalf("resolved = logs:%v heaps:%v", resolved.Logs, resolved.HeapDumps)
	}
	extractedRoot := filepath.Dir(filepath.Dir(resolved.Logs[0]))
	if err := resolved.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(extractedRoot); !os.IsNotExist(err) {
		t.Fatalf("temporary extraction root remains: %v", err)
	}
}

func TestResolveArchiveRejectsTraversalDuplicateAndCompressionBomb(t *testing.T) {
	dir := t.TempDir()
	for _, test := range []struct {
		name    string
		entries []archiveEntry
		want    string
	}{
		{name: "traversal", entries: []archiveEntry{{name: "../escape.jhlog", data: []byte("x")}}, want: "unsafe"},
		{name: "duplicate", entries: []archiveEntry{{name: "p/a.jhlog", data: []byte("x")}, {name: "p/a.jhlog", data: []byte("x")}}, want: "duplicate"},
		{name: "bomb", entries: []archiveEntry{{name: "p/a.jhlog", data: make([]byte, 1<<20), method: zip.Deflate}}, want: "compression ratio"},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := filepath.Join(dir, test.name+".jhlog.zip")
			writeArchive(t, archive, test.entries)
			if _, err := Resolve([]string{archive}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Resolve() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateArchiveEntryRejectsEncryption(t *testing.T) {
	entry := &zip.File{FileHeader: zip.FileHeader{Name: "process/log.jhlog", Flags: 1, Method: zip.Store}}
	if err := validateArchiveEntry(entry, map[string]struct{}{}); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("validateArchiveEntry() error = %v", err)
	}
}

func TestResolveContainerRejectsMultipleRunIDs(t *testing.T) {
	root := t.TempDir()
	writeLogWithRun(t, filepath.Join(root, "first.jhlog"), 1, 1)
	writeLogWithRun(t, filepath.Join(root, "second.jhlog"), 2, 2)
	if _, err := Resolve([]string{root}); err == nil || !strings.Contains(err.Error(), "multiple run IDs") {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestResolveContainerRejectsConflictingProcessIdentity(t *testing.T) {
	root := t.TempDir()
	writeLogWithRun(t, filepath.Join(root, "first.jhlog"), 1, 1)
	writeLogWithRun(t, filepath.Join(root, "second.jhlog"), 2, 1, "remote")
	if _, err := Resolve([]string{root}); err == nil || !strings.Contains(err.Error(), "process identity") {
		t.Fatalf("Resolve() error = %v, want conflicting process identity", err)
	}
}

func TestResolveContainerAcceptsConnectedConfigurationEpochs(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.jhlog")
	remote := filepath.Join(root, "remote.jhlog")
	second := filepath.Join(root, "second.jhlog")
	writeLogWithRun(t, first, 1, 1)
	writeLogWithRun(t, remote, 1, 2)
	writeLogWithRun(t, second, 2, 1)
	archivePath := filepath.Join(t.TempDir(), "session-epochs.jhlog.zip")
	writeArchive(t, archivePath, []archiveEntry{
		{name: "main/first.jhlog", source: first},
		{name: "remote/remote.jhlog", source: remote},
		{name: "main/second.jhlog", source: second},
	})
	for _, input := range []string{root, archivePath} {
		resolved, err := Resolve([]string{input})
		if err != nil {
			t.Fatalf("Resolve(%q): %v", input, err)
		}
		if len(resolved.Logs) != 3 || !resolved.MultiRunEpochs {
			t.Fatalf("Resolve(%q) = %d logs, multi-run=%v", input, len(resolved.Logs), resolved.MultiRunEpochs)
		}
		if err := resolved.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func writeLogWithRun(t *testing.T, path string, run, process byte, processName ...string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	header := jhlog.DefaultSegmentHeader()
	header.RunID[0] = run
	header.ProcessInstanceID[0] = process
	header.SessionID[0] = run
	if len(processName) > 0 {
		header.ProcessName = processName[0]
	}
	writer, err := jhlog.NewWriterWithHeader(file, header)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

type archiveEntry struct {
	name   string
	source string
	data   []byte
	method uint16
}

func writeArchive(t *testing.T, path string, entries []archiveEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: entry.method}
		if header.Method == 0 {
			header.Method = zip.Store
		}
		target, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		data := entry.data
		if entry.source != "" {
			data, err = os.ReadFile(entry.source)
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err := target.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkResolveSessionInputs(b *testing.B) {
	root := b.TempDir()
	process := filepath.Join(root, "process")
	if err := os.Mkdir(process, 0o755); err != nil {
		b.Fatal(err)
	}
	log := filepath.Join(process, "segment.jhlog")
	if err := jhlog.WriteSample(log); err != nil {
		b.Fatal(err)
	}
	archive := filepath.Join(root, "session.jhlog.zip")
	writeArchiveForBenchmark(b, archive, log)
	for _, input := range []struct {
		name string
		path string
	}{
		{name: "directory", path: process},
		{name: "archive", path: archive},
	} {
		b.Run(input.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				resolved, err := Resolve([]string{input.path})
				if err != nil {
					b.Fatal(err)
				}
				if err := resolved.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func writeArchiveForBenchmark(b *testing.B, path, log string) {
	b.Helper()
	file, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: "process/segment.jhlog", Method: zip.Store})
	if err != nil {
		b.Fatal(err)
	}
	source, err := os.Open(log)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := io.Copy(entry, source); err != nil {
		b.Fatal(err)
	}
	if err := source.Close(); err != nil {
		b.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		b.Fatal(err)
	}
	if err := file.Close(); err != nil {
		b.Fatal(err)
	}
}
