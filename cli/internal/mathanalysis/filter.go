package mathanalysis

import (
	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func mathEventMatchesFilter(event jhlog.Event, dict map[uint64]string, filter analyze.Filter, symbols *mathSymbolResolver) bool {
	// These signals are global in the summary too; no execution context is inferred
	// from a metric's display name. The report explicitly declares this scope.
	if event.Context != nil || event.Metric != nil {
		return true
	}
	if !filter.Active() {
		return true
	}
	var route, screen, owner string
	var ownerRef, screenRef jhlog.SymbolRef
	if event.Attribution.Present {
		ownerRef, screenRef = event.Attribution.Owner, event.Attribution.Screen
	}
	if filter.ScreenContains != "" {
		screen = symbols.resolve(dict, screenRef)
	}
	if filter.OwnerContains != "" {
		owner = symbols.resolve(dict, ownerRef)
	}
	if event.HTTP != nil {
		if filter.RouteContains != "" {
			route = symbols.resolveRaw(dict, event.HTTP.RouteRef)
		}
		initiator := ""
		if filter.OwnerContains != "" {
			initiator = symbols.resolve(dict, event.HTTP.InitiatorRef)
		}
		return filter.Matches(route, screen, owner, nil, initiator)
	}
	if event.Retained != nil {
		className, holder := "", ""
		if filter.ClassContains != "" {
			className = symbols.resolve(dict, event.Retained.ClassRef)
		}
		if filter.OwnerContains != "" {
			holder = symbols.resolve(dict, event.Retained.HolderRef)
		}
		return filter.Matches("", screen, owner, []string{className}, holder)
	}
	return filter.Matches("", screen, owner, nil)
}
