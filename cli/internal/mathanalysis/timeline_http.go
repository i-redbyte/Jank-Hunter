package mathanalysis

import (
	"slices"
	"strings"
	"unsafe"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

// HTTPRouteObservation aggregates attributes from the same HTTP events. It
// does not turn an attribution context into proof of the immediate caller.
type HTTPRouteObservation struct {
	Owner, Route                               string
	Count, DNSCount, ConnectCount, FailedCount int
}

type httpRouteContext struct{ owner, route string }
type httpRouteCounts struct{ count, dns, connect, failed int }

func (b *timelineBucketAgg) observeHTTPRoute(owner, route string, event jhlog.Event) {
	if route == "" {
		return
	}
	key := httpRouteContext{owner, route}
	value, exists := b.httpRoutes[key]
	if !exists {
		if b.httpRoutes == nil {
			if !b.account.reserve(mathMapBaseBytes) {
				return
			}
			b.httpRoutes = make(map[httpRouteContext]httpRouteCounts)
		}
		if !b.account.reserve(mathMapEntryBytes + uint64(unsafe.Sizeof(value)) + uint64(len(owner)+len(route))) {
			return
		}
	}
	value.count++
	if event.HTTP.DNSMS > 0 {
		value.dns++
	}
	if event.HTTP.ConnectMS > 0 {
		value.connect++
	}
	if event.Flags&uint64(jhlog.FlagHTTPFailed) != 0 || event.HTTP.Status == jhlog.Status5xx {
		value.failed++
	}
	b.httpRoutes[key] = value
}

func (b *timelineBucketAgg) httpRouteObservations(results *collectionAccount) []HTTPRouteObservation {
	if len(b.httpRoutes) == 0 {
		return nil
	}
	if !results.reserveItems(len(b.httpRoutes), uint64(unsafe.Sizeof(HTTPRouteObservation{}))) {
		return nil
	}
	observations := make([]HTTPRouteObservation, 0, len(b.httpRoutes))
	for key, value := range b.httpRoutes {
		if !results.reserve(uint64(len(key.owner) + len(key.route))) {
			return nil
		}
		observations = append(observations, HTTPRouteObservation{Owner: key.owner, Route: key.route, Count: value.count, DNSCount: value.dns, ConnectCount: value.connect, FailedCount: value.failed})
	}
	slices.SortFunc(observations, func(a, b HTTPRouteObservation) int {
		if order := strings.Compare(a.Route, b.Route); order != 0 {
			return order
		}
		return strings.Compare(a.Owner, b.Owner)
	})
	return observations
}
