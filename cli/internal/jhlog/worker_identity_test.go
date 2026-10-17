package jhlog

import (
	"path/filepath"
	"testing"
)

func TestWorkerIdentityPreservesAllUUIDAndPrivateIDBits(t *testing.T) {
	value := &WorkerIdentityEvent{Version: 1, InstanceID: ^uint64(0), UUIDMost: 1 << 63, UUIDLeast: ^uint64(0)}
	path := filepath.Join(t.TempDir(), "identity.jhlog")
	writeClosedEvents(t, path, []Event{{Type: EventWorkerIdentity, DiagnosticEvent: &DiagnosticEvent{WorkerIdentity: value}}})
	log, err := readLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Events) != 1 || *log.Events[0].WorkerIdentity != *value || value.UUID() != "80000000-0000-0000-ffff-ffffffffffff" {
		t.Fatalf("identity damaged: %+v", log.Events)
	}
}
