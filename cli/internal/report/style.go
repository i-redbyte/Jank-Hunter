package report

import (
	"fmt"
	"strings"
)

type ReportStyle string

const (
	ReportStyleModern ReportStyle = "modern"
	ReportStyleLegacy ReportStyle = "legacy"
)

var (
	compactBaseCSS   = compactStylesheet(baseCSS)
	compactMathCSS   = compactStylesheet(mathCSS)
	compactModernCSS = compactStylesheet(modernCSS)
)

func ParseReportStyle(value string) (ReportStyle, error) {
	switch ReportStyle(strings.ToLower(strings.TrimSpace(value))) {
	case "", ReportStyleModern:
		return ReportStyleModern, nil
	case ReportStyleLegacy:
		return ReportStyleLegacy, nil
	default:
		return "", fmt.Errorf("unknown report style %q; expected modern or legacy", value)
	}
}

func (s ReportStyle) normalized() ReportStyle {
	if s == ReportStyleLegacy {
		return ReportStyleLegacy
	}
	return ReportStyleModern
}

func reportStylesheet(style ReportStyle, includeMath bool) string {
	length := len(compactBaseCSS)
	if includeMath {
		length += len(compactMathCSS)
	}
	if style.normalized() == ReportStyleModern {
		length += len(compactModernCSS)
	}
	var builder strings.Builder
	builder.Grow(length)
	builder.WriteString(compactBaseCSS)
	if includeMath {
		builder.WriteString(compactMathCSS)
	}
	if style.normalized() == ReportStyleModern {
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
