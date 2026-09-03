package datavalue

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

var englishUnknownValues = [...]string{
	"unknown",
	"unknown unknown",
	"unknown build",
}

var russianUnknownValues = [...]string{
	"неизвестно",
	"неизвестен",
	"неизвестна",
	"не определен",
	"нет данных",
	"версия приложения неизвестна",
	"неизвестное устройство",
	"контекст выполнения недоступен",
	"контекст недоступен",
}

func IsUnknown(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	first := value[0]
	if first < utf8.RuneSelf {
		if first >= 'A' && first <= 'Z' {
			first += 'a' - 'A'
		}
		switch first {
		case 'u':
			return matchesUnknownValue(value, englishUnknownValues[:])
		case 'a':
			return equalFoldFields(value, "android неизвестен")
		default:
			return false
		}
	}
	return matchesUnknownValue(value, russianUnknownValues[:])
}

func matchesUnknownValue(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if equalFoldFields(value, candidate) {
			return true
		}
	}
	return false
}

func equalFoldFields(value string, candidate string) bool {
	valueIndex := 0
	candidateIndex := 0
	for {
		for valueIndex < len(value) {
			r, size := utf8.DecodeRuneInString(value[valueIndex:])
			if !unicode.IsSpace(r) {
				break
			}
			valueIndex += size
		}
		for candidateIndex < len(candidate) && candidate[candidateIndex] == ' ' {
			candidateIndex++
		}
		if valueIndex == len(value) || candidateIndex == len(candidate) {
			return valueIndex == len(value) && candidateIndex == len(candidate)
		}
		valueStart := valueIndex
		for valueIndex < len(value) {
			r, size := utf8.DecodeRuneInString(value[valueIndex:])
			if unicode.IsSpace(r) {
				break
			}
			valueIndex += size
		}
		candidateStart := candidateIndex
		for candidateIndex < len(candidate) && candidate[candidateIndex] != ' ' {
			candidateIndex++
		}
		if !strings.EqualFold(value[valueStart:valueIndex], candidate[candidateStart:candidateIndex]) {
			return false
		}
	}
}

func HumanUnknown(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if IsUnknown(value) {
		return fallback
	}
	return value
}

func NormalizeUnknown(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
