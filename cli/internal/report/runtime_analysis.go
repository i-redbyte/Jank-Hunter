package report

func asyncKindLabel(kind string) string {
	switch kind {
	case "handler_runnable":
		return "Задача обработчика"
	case "coroutine":
		return "Корутина"
	default:
		return kind
	}
}

func hundredths(value uint64) float64 {
	return float64(value) / 100
}
