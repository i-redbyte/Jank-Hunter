package mathanalysis

import (
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type pendingMathStall struct {
	storageBytes uint64
	event        jhlog.Event
	dict         map[uint64]string
}

type mathStallLifecycle struct {
	account *collectionAccount
	tracker jhlog.StallTracker
	pending map[uint64]pendingMathStall
}

func (lifecycle *mathStallLifecycle) stream(path string, symbols *mathSymbolResolver, consume jhlog.EventHandler) error {
	_, err := lifecycle.streamWithResult(path, symbols, consume)
	return err
}

func (lifecycle *mathStallLifecycle) streamWithResult(path string, symbols *mathSymbolResolver, consume jhlog.EventHandler) (jhlog.StreamResult, error) {
	return jhlog.StreamFileWithResult(path, func(event jhlog.Event, dict map[uint64]string) error {
		if event.Stall == nil || event.Stall.IncidentID == 0 {
			return consume(event, dict)
		}
		if !lifecycle.tracker.HasIncident(event.Stall.IncidentID) && !lifecycle.account.reserve(mathMapEntryBytes) {
			return lifecycle.account.budget.err()
		}
		pending, err := lifecycle.tracker.Observe(event.Stall)
		if err != nil {
			return err
		}
		if !pending {
			lifecycle.account.release(lifecycle.pending[event.Stall.IncidentID].storageBytes)
			delete(lifecycle.pending, event.Stall.IncidentID)
			return consume(event, dict)
		}
		if lifecycle.pending == nil {
			if !lifecycle.account.reserve(2*mathMapBaseBytes + uint64(jhlog.MaxPendingStallIncidents)*mathMapEntryBytes) {
				return lifecycle.account.budget.err()
			}
			lifecycle.pending = make(map[uint64]pendingMathStall)
		}
		// A later segment has another dictionary. Preserve only the three referenced names,
		// resolving stable symbols before that segment's resolver is discarded.
		refs := [...]jhlog.SymbolRef{event.Stall.StackRef, event.Attribution.Screen, event.Attribution.Owner}
		var resolved [3]string
		storageBytes := uint64(1024)
		for index, ref := range refs {
			if !ref.IsUnknown() {
				resolved[index] = symbols.resolve(dict, ref)
				storageBytes += uint64(len(resolved[index]))
			}
		}
		if !lifecycle.account.reserve(storageBytes) {
			return lifecycle.account.budget.err()
		}
		names := make(map[uint64]string, 3)
		capture := func(id uint64, ref jhlog.SymbolRef) jhlog.SymbolRef {
			if ref.IsUnknown() {
				return ref
			}
			names[id] = resolved[id-1]
			return jhlog.LocalSymbol(id)
		}
		value := *event.Stall
		value.StackRef = capture(1, value.StackRef)
		event.Stall = &value
		event.Attribution.Screen = capture(2, event.Attribution.Screen)
		event.Attribution.Owner = capture(3, event.Attribution.Owner)
		lifecycle.account.release(lifecycle.pending[value.IncidentID].storageBytes)
		lifecycle.pending[value.IncidentID] = pendingMathStall{event: event, dict: names, storageBytes: storageBytes}
		return nil
	})
}

func (lifecycle *mathStallLifecycle) finish(consume jhlog.EventHandler) error {
	if !lifecycle.account.reserveItems(len(lifecycle.pending), 8) {
		return lifecycle.account.budget.err()
	}
	ids := make([]uint64, 0, len(lifecycle.pending))
	for id := range lifecycle.pending {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		value := lifecycle.pending[id]
		if err := consume(value.event, value.dict); err != nil {
			return err
		}
		delete(lifecycle.pending, id)
	}
	lifecycle.tracker = jhlog.StallTracker{}
	lifecycle.pending = nil
	lifecycle.account.close()
	return nil
}

func mathSessionEnds(inputs []analyze.SessionInput, index int) bool {
	return index+1 == len(inputs) || inputs[index].Header.SessionID.IsZero() ||
		inputs[index].Header.SessionID != inputs[index+1].Header.SessionID
}
