package report

import (
	"strings"
)

var (
	compactBaseCSS   = compactStylesheet(baseCSS)
	compactMathCSS   = compactStylesheet(mathCSS)
	compactModernCSS = compactStylesheet(modernCSS)
)

func reportStylesheet(includeMath bool) string {
	length := len(compactBaseCSS) + len(compactModernCSS)
	if includeMath {
		length += len(compactMathCSS)
	}
	var builder strings.Builder
	builder.Grow(length)
	builder.WriteString(compactBaseCSS)
	if includeMath {
		builder.WriteString(compactMathCSS)
	}
	builder.WriteString(compactModernCSS)
	return builder.String()
}

func compactStylesheet(source string) string {
	var builder strings.Builder
	builder.Grow(len(source))
	for _, line := range strings.Split(source, "\n") {
		builder.WriteString(strings.TrimSpace(line))
	}
	return builder.String()
}
