package jhlog

import (
	"fmt"
	"unicode/utf8"
)

// ThreadSnapshotEvent is a bounded, non-atomic scan. Complete means all enumerated
// entries were read; it does not imply coverage of threads between scans.
type ThreadSnapshotEvent struct {
	DroppedJavaIdentities uint64              `json:"dropped_java_identities"`
	Version               uint64              `json:"version"`
	ElapsedMS             uint64              `json:"elapsed_ms"`
	ScanDurationMS        uint64              `json:"scan_duration_ms"`
	IntervalMS            uint64              `json:"interval_ms"`
	TicksPerSecond        uint64              `json:"ticks_per_second"`
	Complete              bool                `json:"complete"`
	FailedReads           uint64              `json:"failed_reads"`
	Reason                string              `json:"reason,omitempty"`
	Threads               []ThreadObservation `json:"threads"`
}

type ThreadObservation struct {
	Java        *JavaThreadObservation `json:"java,omitempty"`
	TID         uint64                 `json:"tid"`
	StartTicks  uint64                 `json:"start_ticks"`
	Name        string                 `json:"name"`
	OSState     string                 `json:"os_state"`
	UserTicks   uint64                 `json:"user_ticks"`
	SystemTicks uint64                 `json:"system_ticks"`
	Priority    int64                  `json:"priority"`
	Nice        int64                  `json:"nice"`
}

func (e eventPayloadEncoder) encodeThreadSnapshot(s *ThreadSnapshotEvent) error {
	if s == nil || (s.Version < 1 || s.Version > 3) || (s.Version == 1 && s.DroppedJavaIdentities != 0) || len(s.Threads) > 2048 || len(s.Reason) > 128 || (s.Complete && (s.FailedReads != 0 || s.Reason != "")) {
		return fmt.Errorf("invalid thread snapshot")
	}
	complete := uint64(0)
	if s.Complete {
		complete = 1
	}
	if err := e.writeValues(s.Version, s.ElapsedMS, s.ScanDurationMS, s.IntervalMS, s.TicksPerSecond, complete, s.FailedReads); err != nil {
		return err
	}
	if err := e.threadString(s.Reason); err != nil {
		return err
	}
	if err := e.writeValues(uint64(len(s.Threads))); err != nil {
		return err
	}
	seen := make(map[uint64]bool, len(s.Threads))
	for _, r := range s.Threads {
		if seen[r.TID] {
			return fmt.Errorf("duplicate thread ID")
		}
		seen[r.TID] = true
		if s.Version == 1 && r.Java != nil {
			return fmt.Errorf("Java observation requires schema 2")
		}
		if r.TID == 0 || len(r.Name) > 1024 || len(r.OSState) != 1 {
			return fmt.Errorf("invalid thread row")
		}
		if err := e.writeValues(r.TID, r.StartTicks); err != nil {
			return err
		}
		if err := e.threadString(r.Name); err != nil {
			return err
		}
		if err := e.writeValues(uint64(r.OSState[0]), r.UserTicks, r.SystemTicks, uint64(r.Priority<<1)^uint64(r.Priority>>63), uint64(r.Nice<<1)^uint64(r.Nice>>63)); err != nil {
			return err
		}
		if s.Version >= 2 {
			if err := e.encodeJavaThread(r.Java, s.Version); err != nil {
				return err
			}
		}
	}
	if s.Version >= 2 {
		return e.writeValues(s.DroppedJavaIdentities)
	}
	return nil
}

func (e eventPayloadEncoder) threadString(s string) error {
	if !utf8.ValidString(s) {
		return fmt.Errorf("invalid UTF-8")
	}
	if err := e.writeValues(uint64(len(s))); err != nil {
		return err
	}
	return writeAll(e.writer, []byte(s))
}

func decodeThreadSnapshot(r *recordReader, event *Event) error {
	var scratch [7]uint64
	v, err := readPayloadValues(r, scratch[:], "version", "elapsed", "scan duration", "interval", "tick frequency", "complete", "failed reads")
	if err != nil {
		return err
	}
	if (v[0] < 1 || v[0] > 3) || v[5] > 1 {
		return fmt.Errorf("unsupported thread snapshot version/flags")
	}
	s := &ThreadSnapshotEvent{Version: v[0], ElapsedMS: v[1], ScanDurationMS: v[2], IntervalMS: v[3], TicksPerSecond: v[4], Complete: v[5] == 1, FailedReads: v[6]}
	s.Reason, err = readThreadString(r, 128)
	if err != nil {
		return err
	}
	if s.Complete && (s.FailedReads != 0 || s.Reason != "") {
		return fmt.Errorf("inconsistent thread completeness")
	}
	count, err := r.readUvarint()
	if err != nil {
		return err
	}
	if count > 2048 {
		return fmt.Errorf("thread snapshot limit exceeded")
	}
	s.Threads = make([]ThreadObservation, 0, count)
	seen := make(map[uint64]bool, count)
	for i := uint64(0); i < count; i++ {
		tid, err := r.readUvarint()
		if err != nil {
			return err
		}
		start, err := r.readUvarint()
		if err != nil {
			return err
		}
		name, err := readThreadString(r, 1024)
		if err != nil {
			return err
		}
		v, err := readPayloadValues(r, scratch[:], "OS state", "user ticks", "system ticks", "priority", "nice")
		if err != nil {
			return err
		}
		if seen[tid] {
			return fmt.Errorf("duplicate thread ID")
		}
		seen[tid] = true
		if tid == 0 || v[0] > 127 || v[0] == 0 {
			return fmt.Errorf("invalid thread identity/state")
		}
		row := ThreadObservation{TID: tid, StartTicks: start, Name: name, OSState: string(rune(v[0])), UserTicks: v[1], SystemTicks: v[2], Priority: int64(v[3]>>1) ^ -int64(v[3]&1), Nice: int64(v[4]>>1) ^ -int64(v[4]&1)}
		if s.Version >= 2 {
			row.Java, err = decodeJavaThread(r, s.Version)
			if err != nil {
				return err
			}
		}
		s.Threads = append(s.Threads, row)
	}
	if s.Version >= 2 {
		s.DroppedJavaIdentities, err = r.readUvarint()
		if err != nil {
			return err
		}
	}
	event.DiagnosticEvent = &DiagnosticEvent{ThreadSnapshot: s}
	return nil
}

func readThreadString(r *recordReader, limit uint64) (string, error) {
	n, err := r.readUvarint()
	if err != nil {
		return "", err
	}
	if n > limit {
		return "", fmt.Errorf("thread string limit exceeded")
	}
	b := make([]byte, n)
	if err = r.readFull(b); err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", fmt.Errorf("invalid thread UTF-8")
	}
	return string(b), nil
}
