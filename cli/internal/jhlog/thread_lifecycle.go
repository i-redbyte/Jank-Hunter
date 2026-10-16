package jhlog

import "fmt"

type ThreadLifecycleStage uint64

const (
	ThreadRunObserved     ThreadLifecycleStage = 1
	ThreadStartReturned   ThreadLifecycleStage = 2
	ThreadFactoryReturned ThreadLifecycleStage = 3
)

// Point observations only; Thread.start may return after the target's execution.
type ThreadLifecycleEvent struct {
	Version              uint64               `json:"version"`
	Stage                ThreadLifecycleStage `json:"stage"`
	JavaID               uint64               `json:"java_id,string,omitempty"`
	TargetTID            uint64               `json:"target_tid,omitempty"`
	ObservedElapsedNanos uint64               `json:"observed_elapsed_nanos,string"`
	Daemon               bool                 `json:"daemon"`
	Name                 string               `json:"name"`
	Source               string               `json:"source"`
	Factory              string               `json:"factory,omitempty"`
}

func validThreadLifecycle(v *ThreadLifecycleEvent) bool {
	if v == nil || v.Version != 1 || v.Stage < ThreadRunObserved || v.Stage > ThreadFactoryReturned {
		return false
	}
	if (v.Stage == ThreadRunObserved) != (v.TargetTID > 0) {
		return false
	}
	return len(v.Name) <= 1024 && len(v.Source) <= 1024 && len(v.Factory) <= 1024
}
func (e eventPayloadEncoder) encodeThreadLifecycle(v *ThreadLifecycleEvent) error {
	if !validThreadLifecycle(v) {
		return fmt.Errorf("invalid thread lifecycle observation")
	}
	daemon := uint64(0)
	if v.Daemon {
		daemon = 1
	}
	if err := e.writeValues(v.Version, uint64(v.Stage), v.JavaID, v.TargetTID, v.ObservedElapsedNanos, daemon); err != nil {
		return err
	}
	for _, s := range []string{v.Name, v.Source, v.Factory} {
		if err := e.threadString(s); err != nil {
			return err
		}
	}
	return nil
}
func decodeThreadLifecycle(r *recordReader, event *Event) error {
	var scratch [6]uint64
	v, err := readPayloadValues(r, scratch[:], "schema", "thread stage", "Java ID", "target TID", "observation nanos", "daemon")
	if err != nil {
		return err
	}
	if v[5] > 1 {
		return fmt.Errorf("unknown thread daemon flag")
	}
	value := &ThreadLifecycleEvent{Version: v[0], Stage: ThreadLifecycleStage(v[1]), JavaID: v[2], TargetTID: v[3], ObservedElapsedNanos: v[4], Daemon: v[5] == 1}
	for _, field := range []*string{&value.Name, &value.Source, &value.Factory} {
		*field, err = readThreadString(r, 1024)
		if err != nil {
			return err
		}
	}
	if !validThreadLifecycle(value) {
		return fmt.Errorf("invalid thread lifecycle observation")
	}
	event.DiagnosticEvent = &DiagnosticEvent{ThreadLifecycle: value}
	return nil
}
