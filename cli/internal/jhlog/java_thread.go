package jhlog

import "fmt"

// Registered on the executing Java thread; joined to a live kernel observation by Android.
// The first registration, whether an app run hook or an owned collector registration,
// is not object creation or Thread.start time.
type JavaThreadObservation struct {
	ID                        uint64 `json:"id,string"`
	Name                      string `json:"name"`
	State                     string `json:"state"`
	Daemon                    bool   `json:"daemon"`
	FirstObservedElapsedNanos uint64 `json:"first_observed_elapsed_nanos,string"`
	Source                    string `json:"source"`
	OwnedByJankHunter         *bool  `json:"owned_by_jank_hunter,omitempty"`
}

func validJavaThread(j *JavaThreadObservation) bool {
	if j.ID == 0 || len(j.Name) > 1024 || len(j.Source) > 1024 {
		return false
	}
	switch j.State {
	case "NEW", "RUNNABLE", "BLOCKED", "WAITING", "TIMED_WAITING", "TERMINATED":
		return true
	default:
		return false
	}
}
func (e eventPayloadEncoder) encodeJavaThread(j *JavaThreadObservation, version uint64) error {
	if j == nil {
		return e.writeValues(0)
	}
	if !validJavaThread(j) {
		return fmt.Errorf("invalid Java thread observation")
	}
	daemon := uint64(0)
	if j.Daemon {
		daemon = 1
	}
	if err := e.writeValues(1, j.ID, daemon, j.FirstObservedElapsedNanos); err != nil {
		return err
	}
	for _, s := range []string{j.Name, j.State, j.Source} {
		if err := e.threadString(s); err != nil {
			return err
		}
	}
	if version == 3 {
		if j.OwnedByJankHunter == nil {
			return fmt.Errorf("missing Java thread ownership")
		}
		if *j.OwnedByJankHunter {
			return e.writeValues(1)
		}
		return e.writeValues(0)
	}
	if j.OwnedByJankHunter != nil {
		return fmt.Errorf("ownership requires schema 3")
	}
	return nil
}
func decodeJavaThread(r *recordReader, version uint64) (*JavaThreadObservation, error) {
	present, err := r.readUvarint()
	if err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	if present != 1 {
		return nil, fmt.Errorf("unknown Java thread presence")
	}
	var scratch [3]uint64
	values, err := readPayloadValues(r, scratch[:], "Java ID", "daemon", "first observation")
	if err != nil {
		return nil, err
	}
	if values[1] > 1 {
		return nil, fmt.Errorf("unknown Java daemon flag")
	}
	j := &JavaThreadObservation{ID: values[0], Daemon: values[1] == 1, FirstObservedElapsedNanos: values[2]}
	for _, field := range []struct {
		p     *string
		limit uint64
	}{{&j.Name, 1024}, {&j.State, 32}, {&j.Source, 1024}} {
		*field.p, err = readThreadString(r, field.limit)
		if err != nil {
			return nil, err
		}
	}
	if version == 3 {
		flag, err := r.readUvarint()
		if err != nil {
			return nil, err
		}
		if flag > 1 {
			return nil, fmt.Errorf("unknown Java thread ownership flag")
		}
		owned := flag == 1
		j.OwnedByJankHunter = &owned
	}
	if !validJavaThread(j) {
		return nil, fmt.Errorf("invalid Java thread observation")
	}
	return j, nil
}
