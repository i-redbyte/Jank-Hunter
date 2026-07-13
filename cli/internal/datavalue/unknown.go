package datavalue

import "strings"

func IsUnknown(value string) bool {
	switch NormalizeUnknown(value) {
	case "",
		"unknown",
		"unknown unknown",
		"unknown build",
		"неизвестно",
		"неизвестен",
		"неизвестна",
		"не определен",
		"нет данных",
		"android неизвестен",
		"версия приложения неизвестна",
		"неизвестное устройство",
		"контекст выполнения недоступен",
		"контекст недоступен":
		return true
	default:
		return false
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
