package mathanalysis

import "strings"

func classifyNetworkMetric(name string) metricNetworkSignal {
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, "network.") && !strings.HasPrefix(lower, "websocket.") {
		return metricNetworkSignal{}
	}
	route := networkMetricRoute(lower)
	owner := ""
	if strings.HasPrefix(lower, "websocket.") {
		prefix := websocketMetricPrefix(lower)
		if prefix != "" {
			if looksLikeMetricRoute(prefix) {
				route = prefix
			} else {
				owner = prefix
			}
		}
		switch {
		case strings.Contains(lower, "reconnect.count"):
			return metricNetworkSignal{kind: "websocket", route: route, owner: owner, tokens: []string{"websocket_reconnect", "reconnect_high"}, ok: true}
		case strings.Contains(lower, "failure"):
			return metricNetworkSignal{kind: "websocket", route: route, owner: owner, tokens: []string{"websocket_failure", "http_failed"}, ok: true}
		default:
			return metricNetworkSignal{}
		}
	}
	switch {
	case strings.Contains(lower, "retry_or_reconnect"):
		return metricNetworkSignal{kind: "retry", route: route, tokens: []string{"reconnect_high"}, ok: true}
	case strings.Contains(lower, "dns.lookup") || strings.Contains(lower, "dns_attempts"):
		return metricNetworkSignal{kind: "dns", route: route, tokens: []string{"dns_high"}, ok: true}
	case strings.Contains(lower, "connect.attempt") || strings.Contains(lower, "connect_attempts") || strings.Contains(lower, "phase.connect.failure"):
		return metricNetworkSignal{kind: "connect", route: route, tokens: []string{"connect_high"}, ok: true}
	case strings.Contains(lower, "failed.count") || strings.Contains(lower, "failure.count") || strings.Contains(lower, ".failure."):
		tokens := []string{"http_failed"}
		if strings.Contains(lower, "5xx") {
			tokens = append(tokens, "http_5xx")
		}
		return metricNetworkSignal{kind: "failure", route: route, tokens: tokens, ok: true}
	case route != "" && (strings.Contains(lower, ".started.count") || strings.Contains(lower, ".finished.count")):
		return metricNetworkSignal{kind: "route", route: route, tokens: []string{"route:" + route}, ok: true}
	default:
		return metricNetworkSignal{}
	}
}

func networkMetricRoute(name string) string {
	const prefix = "network.route."
	if !strings.HasPrefix(name, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(name, prefix)
	suffixes := []string{
		".dns.lookup.count",
		".connect.attempt.count",
		".retry_or_reconnect.count",
		".failed.count",
		".started.count",
		".finished.count",
		".reused_connection.count",
		".phase.",
	}
	for _, suffix := range suffixes {
		if index := strings.Index(rest, suffix); index >= 0 {
			return rest[:index]
		}
	}
	if index := strings.IndexByte(rest, '.'); index >= 0 {
		return rest[:index]
	}
	return rest
}

func websocketMetricPrefix(name string) string {
	rest := strings.TrimPrefix(name, "websocket.")
	parts := strings.Split(rest, ".")
	if len(parts) < 2 {
		return ""
	}
	if isWebsocketEventPart(parts[0]) {
		return ""
	}
	return parts[0]
}

func isWebsocketEventPart(part string) bool {
	switch part {
	case "open", "response_code", "reconnect", "message", "close_code", "closing", "closed", "failure", "lifetime_ms":
		return true
	default:
		return false
	}
}

func looksLikeMetricRoute(value string) bool {
	for _, prefix := range []string{"get_", "post_", "put_", "patch_", "delete_", "head_", "options_"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func networkMetricTitle(kind, route, owner string) string {
	target := ""
	if route != "" {
		target = " " + route
	} else if owner != "" {
		target = " " + owner
	}
	switch kind {
	case "dns":
		return "DNS-метрика" + target
	case "connect":
		return "Метрика соединения" + target
	case "retry":
		return "Метрика повторов/переподключений" + target
	case "websocket":
		return "WebSocket-метрика" + target
	case "failure":
		return "Метрика сетевой ошибки" + target
	case "route":
		return "Метрика маршрута" + target
	default:
		return "Сетевая метрика" + target
	}
}

func networkLoopKindToken(kind string) string {
	switch kind {
	case "dns":
		return "dns_high"
	case "connect":
		return "connect_high"
	case "retry":
		return "reconnect_high"
	case "websocket":
		return "websocket_reconnect"
	case "failure":
		return "http_failed"
	default:
		return ""
	}
}

func networkLoopProbableCause(kind, route, owner string) string {
	target := networkLoopTarget(route, owner)
	ownerAction := ""
	if !analysisOwnerIsKnown(owner) {
		ownerAction = " " + missingOwnerAction()
	}
	switch kind {
	case "dns":
		return "Гипотеза для проверки: периодическое DNS-разрешение или потеря DNS-кеша" + target + ". Проверьте TTL, кеш, OkHttp DNS и сетевой слой." + ownerAction
	case "connect":
		return "Гипотеза для проверки: повторные попытки соединения или TLS" + target + ". Проверьте пул соединений, прокси/VPN, TLS и достижимость сети." + ownerAction
	case "retry":
		return "Гипотеза для проверки: контур повторов или переподключений" + target + ". Проверьте задержку повторов, отмену работы и владельца обновления." + ownerAction
	case "websocket":
		return "Гипотеза для проверки: шторм WebSocket-переподключений" + target + ". Проверьте жизненный цикл, проверку живости соединения и задержку переподключения." + ownerAction
	case "failure":
		return "Гипотеза для проверки: повторяющиеся сетевые ошибки" + target + ". Проверьте статус сервера, обработку IOException и правила повторов." + ownerAction
	case "owner":
		return "Гипотеза для проверки: источник регулярно запускает сетевую работу" + target + ". Проверьте планирование корутин и задач и подавление частых повторов." + ownerAction
	case "route":
		return "Гипотеза для проверки: периодический опрос или шквал запросов" + target + ". Проверьте таймеры, запуск обновления и правила кеширования." + ownerAction
	default:
		return "Гипотеза для проверки: повторяющаяся последовательность сетевых событий" + target + "." + ownerAction
	}
}

func networkLoopTarget(route, owner string) string {
	parts := []string{}
	if route != "" {
		parts = append(parts, "маршрута "+route)
	}
	if analysisOwnerIsKnown(owner) {
		parts = append(parts, "места запуска "+owner)
	}
	if len(parts) == 0 {
		return ""
	}
	return " для " + strings.Join(parts, " и ")
}

func networkLoopPath(kind, route, owner string, motif []string, confidence float64) GraphPath {
	nodes := []string{"симптом: сетевой цикл"}
	if owner != "" {
		nodes = append(nodes, "место запуска: "+analysisOwnerLabel(owner))
	}
	if route != "" {
		nodes = append(nodes, "маршрут: "+route)
	}
	nodes = append(nodes, networkLoopKindLabel(kind))
	for _, token := range motif {
		label := networkLoopTokenLabel(token)
		if label != "" && !stringSliceContains(nodes, label) {
			nodes = append(nodes, label)
		}
	}
	to := nodes[len(nodes)-1]
	return GraphPath{
		From:       nodes[0],
		To:         to,
		Nodes:      nodes,
		Cost:       1 - clamp01(confidence),
		Confidence: clamp01(confidence),
	}
}

func networkLoopKey(loop NetworkLoopFinding) string {
	return loop.Route + "|" + loop.Owner + "|" + motifClass(loop.Motif)
}

func motifClass(motif []string) string {
	for _, token := range motif {
		switch token {
		case "dns_high", "connect_high", "reconnect_high", "websocket_reconnect", "websocket_failure", "http_5xx", "http_failed":
			return token
		}
	}
	if len(motif) > 0 {
		return motif[0]
	}
	return "unknown"
}

func networkLoopSpecificity(loop NetworkLoopFinding) int {
	score := 0
	if loop.Route != "" {
		score++
	}
	if loop.Owner != "" {
		score++
	}
	return score
}

func uniqueMotifValue(motif []string, prefix string) string {
	value := ""
	for _, token := range motif {
		if !strings.HasPrefix(token, prefix) {
			continue
		}
		candidate := strings.TrimPrefix(token, prefix)
		if value != "" && candidate != value {
			return ""
		}
		value = candidate
	}
	return value
}

func tokenPriority(token string) int {
	switch {
	case token == "dns_high":
		return 0
	case token == "connect_high":
		return 1
	case token == "reconnect_high" || token == "websocket_reconnect":
		return 2
	case token == "websocket_failure" || token == "http_5xx" || token == "http_failed":
		return 3
	case strings.HasPrefix(token, "route:"):
		return 4
	case strings.HasPrefix(token, "owner:"):
		return 5
	default:
		return 6
	}
}

func metricAwareContains(value, filter string) bool {
	if timelineContainsFilter(value, filter) {
		return true
	}
	normalized := normalizeMetricFragment(filter)
	return normalized != "" && strings.Contains(strings.ToLower(value), normalized)
}

func normalizeMetricFragment(value string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func NetworkLoopMotifText(tokens []string) string {
	if len(tokens) == 0 {
		return "повторяющаяся последовательность не выделена"
	}
	labels := make([]string, 0, len(tokens))
	for _, token := range tokens {
		labels = append(labels, networkLoopTokenLabel(token))
	}
	return strings.Join(labels, ", ")
}

func networkLoopTokenLabel(token string) string {
	switch token {
	case "dns_high":
		return "DNS-всплеск"
	case "connect_high":
		return "всплеск соединений"
	case "reconnect_high":
		return "всплеск повторов/переподключений"
	case "websocket_reconnect":
		return "WebSocket-переподключение"
	case "websocket_failure":
		return "WebSocket-ошибка"
	case "http_5xx":
		return "HTTP 5xx"
	case "http_failed":
		return "HTTP-ошибка"
	}
	if strings.HasPrefix(token, "route:") {
		return "маршрут: " + strings.TrimPrefix(token, "route:")
	}
	if strings.HasPrefix(token, "owner:") {
		return "место запуска: " + analysisOwnerLabel(strings.TrimPrefix(token, "owner:"))
	}
	return token
}

func networkLoopKindLabel(kind string) string {
	switch kind {
	case "dns":
		return "фаза: DNS"
	case "connect":
		return "фаза: соединение"
	case "retry":
		return "фаза: повторы/переподключения"
	case "websocket":
		return "фаза: WebSocket"
	case "failure":
		return "фаза: сетевые ошибки"
	case "owner":
		return "фаза: всплеск источника"
	case "route":
		return "фаза: шквал запросов"
	default:
		return "фаза: сеть"
	}
}
