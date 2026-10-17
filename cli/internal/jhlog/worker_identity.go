package jhlog

import "fmt"

// UUID words are raw unsigned 64-bit halves; InstanceID is private to the runtime process.
type WorkerIdentityEvent struct {
	Version, InstanceID, UUIDMost, UUIDLeast uint64
}

func (v WorkerIdentityEvent) UUID() string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", v.UUIDMost>>32, (v.UUIDMost>>16)&0xffff, v.UUIDMost&0xffff, v.UUIDLeast>>48, v.UUIDLeast&0xffffffffffff)
}
func (e eventPayloadEncoder) encodeWorkerIdentity(v *WorkerIdentityEvent) error {
	if v == nil || v.Version != 1 || v.InstanceID == 0 {
		return fmt.Errorf("invalid worker identity")
	}
	return e.writeValues(v.Version, v.InstanceID, v.UUIDMost, v.UUIDLeast)
}
func decodeWorkerIdentity(r *recordReader, event *Event) error {
	var values [4]uint64
	for i := range values {
		value, err := r.readUvarint()
		if err != nil {
			return err
		}
		values[i] = value
	}
	if values[0] != 1 || values[1] == 0 {
		return fmt.Errorf("invalid worker identity")
	}
	event.DiagnosticEvent = &DiagnosticEvent{WorkerIdentity: &WorkerIdentityEvent{values[0], values[1], values[2], values[3]}}
	return nil
}
