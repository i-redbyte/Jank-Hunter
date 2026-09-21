package report

import (
	"strings"
)

var (
	compactBaseCSS   = compactStylesheet(baseCSS)
	compactMathCSS   = compactStylesheet(mathCSS)
	compactModernCSS = compactStylesheet(modernCSS)
)

func reportStylesheet(includeMath, includeModern bool) string {
	length := len(compactBaseCSS)
	if includeMath {
		length += len(compactMathCSS)
	}
	if includeModern {
		length += len(compactModernCSS)
	}
	var builder strings.Builder
	builder.Grow(length)
	builder.WriteString(compactBaseCSS)
	if includeMath {
		builder.WriteString(compactMathCSS)
	}
	if includeModern {
		builder.WriteString(compactModernCSS)
	}
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
