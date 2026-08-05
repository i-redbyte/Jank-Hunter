package mathanalysis

import (
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type mathSymbolResolver struct {
	embedded map[uint64]string
	ownerMap *analyze.OwnerMap
	nameMap  *analyze.NameMapping
}

func newMathSymbolResolver(options analyze.Options) *mathSymbolResolver {
	return &mathSymbolResolver{
		embedded: map[uint64]string{},
		ownerMap: options.OwnerMap,
		nameMap:  options.ObfuscationMap,
	}
}

func (r *mathSymbolResolver) observe(event jhlog.Event) {
	if event.Dictionary == nil || event.Dictionary.Kind != jhlog.DictStableSymbol || event.Dictionary.Value == "" {
		return
	}
	if _, exists := r.embedded[event.Dictionary.ID]; !exists {
		r.embedded[event.Dictionary.ID] = event.Dictionary.Value
	}
}

func (r *mathSymbolResolver) resolve(dict map[uint64]string, ref jhlog.SymbolRef, legacyID uint64) string {
	if ref.IsUnknown() {
		ref = jhlog.LocalSymbol(legacyID)
	}
	value := ""
	if ref.Stable {
		value = r.embedded[ref.StableID]
	}
	if value == "" {
		value = jhlog.ResolveSymbol(dict, ref)
		value = analyze.ResolveOwnerAlias(r.ownerMap, value)
	}
	return r.nameMap.Deobfuscate(value)
}

func isMathDiagnosticStall(event jhlog.Event, dict map[uint64]string, symbols *mathSymbolResolver) bool {
	if event.Stall == nil {
		return false
	}
	owner := symbols.resolve(dict, event.Stall.OwnerRef, event.Stall.OwnerID)
	contextOwner := symbols.resolve(dict, event.Attribution.Owner, 0)
	flow := symbols.resolve(dict, event.Attribution.Flow, 0)
	step := symbols.resolve(dict, event.Attribution.Step, 0)
	return isMathDiagnosticValue(owner) || isMathDiagnosticValue(contextOwner) || strings.EqualFold(flow, "jankhunter.diagnostics") || strings.EqualFold(step, "heap_dump")
}

func isMathDiagnosticValue(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "jankhunter.heap_dump" || strings.HasPrefix(value, "jankhunter.heap_dump.")
}
