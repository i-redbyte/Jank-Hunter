package jhlog

// SymbolOrigin is explicit producer provenance, independent of names and stable IDs.
type SymbolOrigin uint8

const (
	SymbolOriginUnknown SymbolOrigin = iota
	SymbolOriginSourceLabel
	SymbolOriginRuntimeClass
	SymbolOriginRuntimeStack
)

type symbolOriginTable struct {
	local         map[uint64]SymbolOrigin
	stableAliases map[uint64]SymbolOrigin
}

func (t *symbolOriginTable) define(id, alias uint64, origin SymbolOrigin) {
	if origin == SymbolOriginUnknown {
		if alias != 0 {
			delete(t.stableAliases, alias)
		} else {
			delete(t.local, id)
		}
		return
	}
	if alias != 0 {
		if t.stableAliases == nil {
			t.stableAliases = make(map[uint64]SymbolOrigin)
		}
		t.stableAliases[alias] = origin
	} else {
		if t.local == nil {
			t.local = make(map[uint64]SymbolOrigin)
		}
		t.local[id] = origin
	}
}

// stableSymbolKey keeps aliases for old untyped callers separate from typed producers.
type stableSymbolKey struct {
	ID     uint64
	Origin SymbolOrigin
}
type stableAliasTable map[stableSymbolKey]uint64

func (r SymbolRef) stableKey() stableSymbolKey { return stableSymbolKey{r.ID, r.Origin} }
