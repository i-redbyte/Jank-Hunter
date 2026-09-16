package mathanalysis

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
)

const (
	loopContextObserved  = "observed"
	loopContextAmbiguous = "ambiguous"
	loopContextUnknown   = "unknown"
)

type loopContextConsensus struct {
	value          string
	missing, mixed bool
}

func (c *loopContextConsensus) observe(value string) {
	if datavalue.IsUnknown(value) {
		c.missing = true
		return
	}
	if c.value != "" && c.value != value {
		c.mixed = true
	}
	c.value = value
}

func (c loopContextConsensus) result() (string, string) {
	if c.mixed {
		return "", loopContextAmbiguous
	}
	if c.missing || c.value == "" {
		return "", loopContextUnknown
	}
	return c.value, loopContextObserved
}

// Only the full token population of detected bursts establishes context.
// No map, sorting, majority guess, or truncated display motif is needed.
func networkLoopBurstContexts(signal *networkLoopSignal, bursts []int) (route, owner, routeStatus, ownerStatus string) {
	var routes, owners loopContextConsensus
	for _, index := range bursts {
		routeSeen, ownerSeen := false, false
		for token, count := range signal.tokens[index+signal.bucketOffset] {
			if count <= 0 {
				continue
			}
			switch {
			case strings.HasPrefix(token, "route:"):
				routeSeen = true
				routes.observe(strings.TrimPrefix(token, "route:"))
			case strings.HasPrefix(token, "owner:"):
				ownerSeen = true
				owners.observe(strings.TrimPrefix(token, "owner:"))
			}
		}
		routes.missing = routes.missing || !routeSeen
		owners.missing = owners.missing || !ownerSeen
	}
	route, routeStatus = routes.result()
	owner, ownerStatus = owners.result()
	return
}

// Detailed limitations are rendered only in mathematical data-quality.
func NetworkLoopAttributionExplanation(loops []NetworkLoopFinding) string {
	var mixedOwner, unknownOwner, mixedRoute, unknownRoute int
	for _, loop := range loops {
		switch loop.OwnerAttributionStatus {
		case loopContextAmbiguous:
			mixedOwner++
		case loopContextUnknown:
			unknownOwner++
		case "":
			if !analysisOwnerIsKnown(loop.Owner) {
				unknownOwner++
			}
		}
		switch loop.RouteAttributionStatus {
		case loopContextAmbiguous:
			mixedRoute++
		case loopContextUnknown:
			unknownRoute++
		case "":
			if datavalue.IsUnknown(loop.Route) {
				unknownRoute++
			}
		}
	}
	if mixedOwner+unknownOwner+mixedRoute+unknownRoute == 0 {
		return ""
	}
	text := fmt.Sprintf("Контекст сетевых циклов: циклы с несколькими владельцами: %d; с неполным контекстом владельца: %d; с несколькими маршрутами: %d; с неполным контекстом маршрута: %d. Однозначная связь допускается только при одинаковом известном значении во всех событиях выбранных всплесков. Смешанный или неполный контекст не назначается одному источнику; это ограничение связи с источником, а не доказательство потери событий.", mixedOwner, unknownOwner, mixedRoute, unknownRoute)
	if unknownOwner > 0 {
		text += " " + missingOwnerAction()
	}
	return text
}
