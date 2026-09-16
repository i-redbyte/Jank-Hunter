package mathanalysis

func (b *causalGraphBuilder) addHTTPObservations(observations []HTTPRouteObservation, slow bool) {
	b.httpWindow++
	for _, observation := range observations {
		if !b.work.canReserve(0) {
			return
		}
		if observation.Route == "" || observation.Count <= 0 {
			continue
		}
		routeID := b.httpContextNode("route", observation.Route)
		if observation.Owner != "" {
			ownerID := b.httpContextNode("owner", observation.Owner)
			b.addHTTPObservationEdge(ownerID, routeID, "owner-route", observation.Count, "HTTP-событие записано с этим контекстом источника и маршрутом")
		}
		b.addHTTPObservationEdge(routeID, "phase:HTTP", "route-phase", observation.Count, "HTTP-запросы этого маршрута")
		if observation.DNSCount > 0 {
			phaseID := "phase:DNS"
			b.addHTTPObservationEdge(routeID, phaseID, "route-phase", observation.DNSCount, "DNS измерен в HTTP-запросах этого маршрута")
			if slow {
				b.addHTTPObservationEdge(phaseID, "symptom:network_slow", "phase-symptom", observation.DNSCount, "DNS наблюдался в интервале медленной сети; причина замедления не установлена")
			}
		}
		if observation.ConnectCount > 0 {
			phaseID := "phase:connect"
			b.addHTTPObservationEdge(routeID, phaseID, "route-phase", observation.ConnectCount, "Соединение измерено в HTTP-запросах этого маршрута")
			if slow {
				b.addHTTPObservationEdge(phaseID, "symptom:network_slow", "phase-symptom", observation.ConnectCount, "Соединение наблюдалось в интервале медленной сети; причина замедления не установлена")
			}
		}
		if observation.FailedCount > 0 {
			b.addHTTPObservationEdge(routeID, "symptom:network_slow", "route-symptom", observation.FailedCount, "Ошибки записаны в HTTP-запросах этого маршрута")
		}
	}
}

func (b *causalGraphBuilder) httpContextNode(kind, value string) string {
	// Composite lookup keys avoid temporary strings even for long owner names.
	key := causalContextKey{kind, value}
	if id, ok := b.httpNodes[key]; ok {
		return id
	}
	if b.httpNodes == nil {
		if !b.work.reserve(mathMapBaseBytes) {
			return ""
		}
		b.httpNodes = make(map[causalContextKey]string)
	}
	if !b.work.reserve(mathMapEntryBytes + 48) {
		return ""
	}
	id := causalNodeID(kind, value)
	label := causalFallbackLabel(kind, value)
	b.addNode(id, label, kind)
	if !b.work.canReserve(0) {
		return ""
	}
	b.httpNodes[key] = id
	return id
}

func (b *causalGraphBuilder) addHTTPObservationEdge(from, to, kind string, count int, description string) {
	// Count is the number of actual HTTP events. Confidence still uses one
	// observation window, and is a heuristic graph weight, not a causal probability.
	for _, pair := range [2][2]string{{from, to}, {to, from}} {
		edge := b.edge(pair[0], pair[1], kind, description)
		if edge == nil {
			return
		}
		edge.count += count
		if edge.httpWindow != b.httpWindow {
			edge.httpWindow = b.httpWindow
			edge.strength++
		}
	}
}
