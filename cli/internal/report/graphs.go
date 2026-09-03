package report

import (
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/mathanalysis"
)

func leakGraphSVG(namespace string, graph analyze.LeakGraph) template.HTML {
	if len(graph.Nodes) == 0 {
		return template.HTML(`<div class="leak-graph-empty">Нет данных для графа.</div>`)
	}
	const (
		nodeW         = 280.0
		nodeH         = 96.0
		gapX          = 190.0
		topY          = 60.0
		bandY         = 180.0
		leftX         = 32.0
		minH          = 340.0
		maxCols       = 3
		edgeInset     = 14.0
		retainedGapX  = 24.0
		retainedBandY = 112.0
	)
	mainNodes := make([]analyze.LeakGraphNode, 0, len(graph.Nodes))
	retainedNodes := make([]analyze.LeakGraphNode, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if node.Kind == "retained" {
			retainedNodes = append(retainedNodes, node)
			continue
		}
		mainNodes = append(mainNodes, node)
	}
	if len(mainNodes) == 0 {
		mainNodes = graph.Nodes
	}
	cols := len(mainNodes)
	if cols < 1 {
		cols = 1
	}
	if cols > maxCols {
		cols = maxCols
	}
	width := math.Max(1080, leftX*2+float64(cols)*nodeW+float64(cols-1)*gapX)
	mainRows := math.Ceil(float64(len(mainNodes)) / maxCols)
	if mainRows < 1 {
		mainRows = 1
	}
	retainedRows := math.Ceil(float64(len(retainedNodes)) / 3)
	height := math.Max(minH, topY+mainRows*bandY+nodeH+96+retainedRows*retainedBandY)
	positions := map[string]graphPoint{}
	var builder strings.Builder
	fmt.Fprintf(&builder, `<svg class="leak-graph-svg" viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">`, width, height, template.HTMLEscapeString(graph.Title))
	scope := graphClass(firstNonEmpty(namespace, "leak-graph"))
	identity := graphClass(firstNonEmpty(graph.TargetID, graph.RootID, graph.Title))
	arrowID := "leak-arrow-" + scope + "-" + identity
	gradientID := "leak-edge-gradient-" + scope + "-" + identity
	fmt.Fprintf(
		&builder,
		`<defs><linearGradient id="%s" x1="0" x2="1"><stop offset="0" stop-color="#6ff7ff"/><stop offset="1" stop-color="#ff4fd8"/></linearGradient><marker id="%s" markerUnits="userSpaceOnUse" markerWidth="12" markerHeight="12" refX="10" refY="6" orient="auto" overflow="visible"><path d="M0,0 L12,6 L0,12 Z" fill="#6ff7ff" opacity="0.88"/></marker></defs>`,
		template.HTMLEscapeString(gradientID),
		template.HTMLEscapeString(arrowID),
	)
	fmt.Fprintf(&builder, `<text x="32" y="30" class="leak-graph-title">%s</text>`, template.HTMLEscapeString(graph.Title))
	for index, node := range mainNodes {
		x := leftX + float64(index%maxCols)*(nodeW+gapX)
		y := topY + float64(index/maxCols)*bandY
		positions[node.ID] = graphPoint{x: x, y: y}
	}
	target := positions[graph.TargetID]
	if graph.TargetID == "" {
		target = positions[mainNodes[len(mainNodes)-1].ID]
	}
	for index, node := range retainedNodes {
		col := index % 3
		row := index / 3
		x := math.Min(width-nodeW-leftX, math.Max(leftX, target.x-150+float64(col)*(nodeW+retainedGapX)))
		y := target.y + nodeH + 76 + float64(row)*retainedBandY
		positions[node.ID] = graphPoint{x: x, y: y}
	}
	for _, edge := range graph.Edges {
		from, okFrom := positions[edge.From]
		to, okTo := positions[edge.To]
		if !okFrom || !okTo {
			continue
		}
		x1 := from.x + nodeW + edgeInset
		y1 := from.y + nodeH/2
		x2 := to.x - edgeInset
		y2 := to.y + nodeH/2
		if to.y > from.y+nodeH {
			x1 = from.x + nodeW/2
			y1 = from.y + nodeH + edgeInset
			x2 = to.x + nodeW/2
			y2 = to.y - edgeInset
		}
		if x2 < x1 && math.Abs(to.y-from.y) < nodeH {
			x1 = from.x - edgeInset
			x2 = to.x + nodeW + edgeInset
		}
		midX := (x1 + x2) / 2
		midY := (y1 + y2) / 2
		fmt.Fprintf(&builder, `<path class="leak-graph-edge edge-%s" style="stroke:url(#%s)" d="M%.1f %.1f C%.1f %.1f %.1f %.1f %.1f %.1f" marker-end="url(#%s)"/>`, graphClass(edge.Kind), template.HTMLEscapeString(gradientID), x1, y1, midX, y1, midX, y2, x2, y2, template.HTMLEscapeString(arrowID))
		if edge.Label != "" {
			label := template.HTMLEscapeString(edge.Label)
			shortLabel := shortGraphLabel(edge.Label, 24)
			labelX := midX
			labelY := math.Min(y1, y2) - 14
			anchor := "middle"
			if math.Abs(y2-y1) > nodeH/2 {
				labelX = midX + 20
				labelY = midY - 10
				anchor = "start"
			}
			labelWidth := math.Min(176, math.Max(48, float64(len([]rune(shortLabel)))*6.8+14))
			backdropX := labelX - labelWidth/2
			if anchor == "start" {
				backdropX = labelX - 7
			}
			fmt.Fprintf(
				&builder,
				`<rect x="%.1f" y="%.1f" width="%.1f" height="18" rx="5" class="leak-graph-edge-label-bg"/><text x="%.1f" y="%.1f" text-anchor="%s" class="leak-graph-edge-label" aria-label="%s"><title>%s</title>%s</text>`,
				backdropX,
				labelY-13,
				labelWidth,
				labelX,
				labelY,
				anchor,
				label,
				label,
				template.HTMLEscapeString(shortLabel),
			)
		}
	}
	for _, node := range graph.Nodes {
		point, ok := positions[node.ID]
		if !ok {
			continue
		}
		classes := "leak-graph-node node-" + graphClass(node.Kind)
		if node.ID == graph.TargetID {
			classes += " is-target"
		}
		if node.ID == graph.RootID {
			classes += " is-root"
		}
		tip := strings.TrimSpace(node.Label)
		if node.Detail != "" {
			tip += " · " + node.Detail
		}
		fmt.Fprintf(
			&builder,
			`<g class="%s" transform="translate(%.1f %.1f)" data-leak-node data-tip="%s" tabindex="0" role="button">`,
			classes,
			point.x,
			point.y,
			template.HTMLEscapeString(tip),
		)
		fmt.Fprintf(&builder, `<title>%s</title>`, template.HTMLEscapeString(tip))
		fmt.Fprintf(&builder, `<rect width="%.0f" height="%.0f" rx="8"/>`, nodeW, nodeH)
		textY := 24.0
		for _, line := range graphLabelLines(node.Label, 30, 2) {
			fmt.Fprintf(&builder, `<text x="14" y="%.0f" class="node-title">%s</text>`, textY, template.HTMLEscapeString(line))
			textY += 15
		}
		if node.Detail != "" {
			textY += 4
			for _, line := range graphLabelLines(node.Detail, 34, 2) {
				fmt.Fprintf(&builder, `<text x="14" y="%.0f" class="node-detail">%s</text>`, textY, template.HTMLEscapeString(line))
				textY += 14
			}
		}
		builder.WriteString(`</g>`)
	}
	builder.WriteString(`</svg>`)
	return template.HTML(builder.String())
}

func graphClass(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	if out.Len() == 0 {
		return "unknown"
	}
	return out.String()
}

func shortGraphLabel(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}

func graphLabelLines(value string, limit int, maxLines int) []string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" || limit <= 0 || maxLines <= 0 {
		return nil
	}
	runes := []rune(value)
	lines := make([]string, 0, maxLines)
	for len(runes) > 0 && len(lines) < maxLines {
		if len(runes) <= limit {
			lines = append(lines, string(runes))
			break
		}
		if len(lines) == maxLines-1 {
			lines = append(lines, shortGraphLabel(string(runes), limit))
			break
		}
		cut := graphLineBreak(runes, limit)
		line := strings.Trim(string(runes[:cut]), " ./#_$-→:")
		if line == "" {
			line = string(runes[:cut])
		}
		lines = append(lines, line)
		runes = []rune(strings.TrimLeft(string(runes[cut:]), " ./#_$-→:"))
	}
	return lines
}

func graphLineBreak(runes []rune, limit int) int {
	if len(runes) <= limit {
		return len(runes)
	}
	lower := limit / 2
	if lower < 8 {
		lower = 8
	}
	for i := limit; i >= lower; i-- {
		if graphBreakRune(runes[i-1]) {
			return i
		}
	}
	return limit
}

func graphBreakRune(r rune) bool {
	switch r {
	case '.', '/', '$', '#', '_', '-', ' ', '→', ':':
		return true
	default:
		return false
	}
}

func sparklineSVG(series mathanalysis.Series) template.HTML {
	const (
		width  = 360.0
		height = 86.0
		pad    = 5.0
	)
	if len(series.Points) == 0 {
		return template.HTML(`<svg class="sparkline" viewBox="0 0 360 86" role="img" aria-label="нет данных"></svg>`)
	}
	validPoints := make([]float64, 0, len(series.Points))
	for index, point := range series.Points {
		if seriesPointPresent(series, index) {
			validPoints = append(validPoints, point)
		}
	}
	if len(validPoints) == 0 {
		return template.HTML(`<svg class="sparkline" viewBox="0 0 360 86" role="img" aria-label="нет замеров"></svg>`)
	}
	maxValue := validPoints[0]
	minValue := validPoints[0]
	for _, point := range validPoints[1:] {
		if point < minValue {
			minValue = point
		}
		if point > maxValue {
			maxValue = point
		}
	}
	if maxValue == minValue {
		minValue = 0
	}
	if maxValue == minValue {
		maxValue = minValue + 1
	}
	scaleY := func(value float64) float64 {
		return height - pad - ((value - minValue) * (height - 2*pad) / (maxValue - minValue))
	}
	step := width - 2*pad
	if len(series.Points) > 1 {
		step = (width - 2*pad) / float64(len(series.Points)-1)
	}
	barWidth := step * 0.62
	if barWidth < 1.2 {
		barWidth = 1.2
	}
	if barWidth > 11 {
		barWidth = 11
	}

	var bars strings.Builder
	var lines []string
	var line strings.Builder
	flushLine := func() {
		if line.Len() > 0 {
			lines = append(lines, line.String())
			line.Reset()
		}
	}
	for i, point := range series.Points {
		if !seriesPointPresent(series, i) {
			flushLine()
			continue
		}
		x := pad
		if len(series.Points) > 1 {
			x += float64(i) * step
		}
		y := scaleY(point)
		barHeight := height - pad - y
		if barHeight < 1 && point > 0 {
			barHeight = 1
		}
		if point > 0 {
			fmt.Fprintf(&bars, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="1.4"></rect>`, x-barWidth/2, height-pad-barHeight, barWidth, barHeight)
		}
		if line.Len() > 0 {
			line.WriteByte(' ')
		}
		fmt.Fprintf(&line, "%.2f,%.2f", x, y)
	}
	flushLine()

	var out strings.Builder
	fmt.Fprintf(&out, `<svg class="sparkline" viewBox="0 0 %.0f %.0f" role="img" aria-label="%s">`, width, height, template.HTMLEscapeString(series.Name))
	out.WriteString(`<line class="spark-axis" x1="5" y1="81" x2="355" y2="81"></line>`)
	out.WriteString(`<g class="spark-bars">`)
	out.WriteString(bars.String())
	out.WriteString(`</g><g class="spark-lines">`)
	for _, points := range lines {
		out.WriteString(`<polyline class="spark-line" points="`)
		out.WriteString(points)
		out.WriteString(`"></polyline>`)
	}
	out.WriteString(`</g></svg>`)
	return template.HTML(out.String())
}

func causalGraphSVG(graph mathanalysis.CausalGraph) template.HTML {
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		return template.HTML(`<div class="muted">Недостаточно узлов и связей для визуального графа.</div>`)
	}
	const (
		maxEdges = 48
		maxNodes = 30
		width    = 960.0
		height   = 460.0
	)
	edges := uniqueCausalEdges(graph.Edges)
	if len(edges) > maxEdges {
		edges = edges[:maxEdges]
	}
	nodeByID := map[string]mathanalysis.CausalNode{}
	for _, node := range graph.Nodes {
		nodeByID[node.ID] = node
	}
	used := map[string]struct{}{}
	for _, edge := range edges {
		used[edge.From] = struct{}{}
		used[edge.To] = struct{}{}
		if len(used) >= maxNodes {
			break
		}
	}
	var nodes []mathanalysis.CausalNode
	for id := range used {
		if node, ok := nodeByID[id]; ok {
			nodes = append(nodes, node)
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		if graphKindColumn(nodes[i].Kind) != graphKindColumn(nodes[j].Kind) {
			return graphKindColumn(nodes[i].Kind) < graphKindColumn(nodes[j].Kind)
		}
		return nodes[i].Label < nodes[j].Label
	})
	position := layoutCausalNodes(nodes, width, height)
	var out strings.Builder
	out.WriteString(`<div class="causal-graph-card">`)
	out.WriteString(`<svg class="causal-graph" viewBox="0 0 960 460" role="img" aria-label="Обзор статистических связей">`)
	for _, edge := range edges {
		from, okFrom := position[edge.From]
		to, okTo := position[edge.To]
		if !okFrom || !okTo {
			continue
		}
		opacity := 0.28 + clampPct(edge.Confidence*100)/140
		fmt.Fprintf(&out, `<line class="causal-edge" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" opacity="%.2f"><title>%s ↔ %s · %s · уверенность %.2f</title></line>`,
			from.x+112, from.y+24, to.x, to.y+24, opacity,
			template.HTMLEscapeString(edge.FromLabel),
			template.HTMLEscapeString(edge.ToLabel),
			template.HTMLEscapeString(mathanalysis.CausalKindLabel(edge.Kind)),
			edge.Confidence,
		)
	}
	for _, node := range nodes {
		pos := position[node.ID]
		label := truncateRunes(node.Label, 28)
		kind := mathanalysis.CausalKindLabel(node.Kind)
		fmt.Fprintf(&out, `<g class="causal-node" transform="translate(%.1f %.1f)"><title>%s · %s</title><rect width="124" height="48"></rect><text x="10" y="19">%s</text><text class="kind" x="10" y="36">%s</text></g>`,
			pos.x, pos.y,
			template.HTMLEscapeString(node.Label),
			template.HTMLEscapeString(kind),
			template.HTMLEscapeString(label),
			template.HTMLEscapeString(truncateRunes(kind, 22)),
		)
	}
	out.WriteString(`</svg>`)
	allEdges := uniqueCausalEdges(graph.Edges)
	if len(allEdges) > len(edges) || len(graph.Nodes) > len(nodes) {
		fmt.Fprintf(&out, `<div class="help-text">Показаны самые сильные статистические связи: %d из %d и %d из %d узлов. Обратные дубликаты скрыты.</div>`, len(edges), len(allEdges), len(nodes), len(graph.Nodes))
	}
	out.WriteString(`</div>`)
	return template.HTML(out.String())
}

func uniqueCausalEdges(edges []mathanalysis.CausalEdge) []mathanalysis.CausalEdge {
	seen := make(map[string]struct{}, len(edges))
	unique := make([]mathanalysis.CausalEdge, 0, len(edges)/2+1)
	for _, edge := range edges {
		left := edge.From
		right := edge.To
		if left > right {
			left, right = right, left
		}
		key := left + "\x00" + right + "\x00" + edge.Kind
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, edge)
	}
	return unique
}

func uniqueCausalPaths(paths []mathanalysis.GraphPath) []mathanalysis.GraphPath {
	seen := make(map[string]struct{}, len(paths))
	unique := make([]mathanalysis.GraphPath, 0, len(paths))
	for _, path := range paths {
		left := path.From
		right := path.To
		if left > right {
			left, right = right, left
		}
		key := left + "\x00" + right
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, path)
	}
	return unique
}

func influenceStatusLabel(value string) string {
	switch value {
	case "runtime":
		return "есть данные выполнения"
	case "static_only":
		return "только статические данные"
	default:
		return "нет данных"
	}
}

func influenceEvidenceLabel(value string) string {
	switch value {
	case "runtime":
		return "выполнение"
	case "mixed":
		return "выполнение + статика"
	case "static":
		return "только статика"
	default:
		return "нет данных"
	}
}

func influenceRoleLabel(value string) string {
	switch value {
	case "caller":
		return "вызывающий"
	case "callee":
		return "вызываемый"
	default:
		return value
	}
}

func influenceSeverityLabel(value string) string {
	switch value {
	case "high":
		return "высокий риск"
	case "medium":
		return "средний риск"
	default:
		return "низкий риск"
	}
}

func topInfluenceNodes(influence analyze.InfluenceSummary, limit int) []analyze.InfluenceNode {
	if limit <= 0 || len(influence.TopNodes) <= limit {
		return influence.TopNodes
	}
	return influence.TopNodes[:limit]
}

type graphPoint struct {
	x float64
	y float64
}

func layoutCausalNodes(nodes []mathanalysis.CausalNode, width, height float64) map[string]graphPoint {
	byColumn := map[int][]mathanalysis.CausalNode{}
	for _, node := range nodes {
		col := graphKindColumn(node.Kind)
		byColumn[col] = append(byColumn[col], node)
	}
	xs := []float64{32, 196, 360, 524, 688, 804}
	positions := map[string]graphPoint{}
	for col, items := range byColumn {
		x := xs[len(xs)-1]
		if col >= 0 && col < len(xs) {
			x = xs[col]
		}
		step := (height - 84) / float64(len(items)+1)
		for i, node := range items {
			positions[node.ID] = graphPoint{x: x, y: 28 + step*float64(i+1)}
		}
	}
	return positions
}

func graphKindColumn(kind string) int {
	switch kind {
	case "state", "symptom":
		return 0
	case "network", "phase":
		return 1
	case "loop":
		return 2
	case "route":
		return 3
	case "owner":
		return 4
	case "screen":
		return 5
	default:
		return 2
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func seriesMax(series mathanalysis.Series) float64 {
	var maxValue float64
	found := false
	for index, point := range series.Points {
		if !seriesPointPresent(series, index) {
			continue
		}
		if !found || point > maxValue {
			maxValue = point
			found = true
		}
	}
	return maxValue
}

func seriesLast(series mathanalysis.Series) float64 {
	for index := len(series.Points) - 1; index >= 0; index-- {
		if seriesPointPresent(series, index) {
			return series.Points[index]
		}
	}
	return 0
}

func seriesPointPresent(series mathanalysis.Series, index int) bool {
	return index >= 0 && index < len(series.Points) && (len(series.Present) != len(series.Points) || series.Present[index])
}

func reportLanguage() string {
	return "ru"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
