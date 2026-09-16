package mathanalysis

import (
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

type mathSymbolResolver struct {
	account  *collectionAccount
	embedded map[uint64]string
	nameMap  *analyze.NameMapping
}

func newMathSymbolResolver(options analyze.Options) *mathSymbolResolver {
	return &mathSymbolResolver{
		embedded: map[uint64]string{},
		nameMap:  options.ObfuscationMap,
	}
}

func (r *mathSymbolResolver) observe(event jhlog.Event) {
	if event.Dictionary == nil || event.Dictionary.Kind != jhlog.DictStableSymbol || event.Dictionary.Value == "" {
		return
	}
	if _, exists := r.embedded[event.Dictionary.ID]; !exists {
		if !r.account.reserve(mathMapEntryBytes + uint64(len(event.Dictionary.Value))) {
			return
		}
		r.embedded[event.Dictionary.ID] = event.Dictionary.Value
	}
}

func (r *mathSymbolResolver) resolve(dict map[uint64]string, ref jhlog.SymbolRef) string {
	return r.nameMap.Deobfuscate(r.resolveRaw(dict, ref))
}

func (r *mathSymbolResolver) resolveRaw(dict map[uint64]string, ref jhlog.SymbolRef) string {
	value := ""
	if ref.Stable {
		value = r.embedded[ref.ID]
	}
	if value == "" {
		value = jhlog.ResolveSymbol(dict, ref)
	}
	return value
}

func isMathDiagnosticStall(event jhlog.Event, dict map[uint64]string, symbols *mathSymbolResolver) bool {
	if event.Stall == nil {
		return false
	}
	owner := symbols.resolve(dict, event.Attribution.Owner)
	return isMathDiagnosticValue(owner)
}

func isMathDiagnosticValue(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "jankhunter.heap_dump" || strings.HasPrefix(value, "jankhunter.heap_dump.")
}
