package report

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func concurrencyEntityLabel(kind uint64) string {
	switch jhlog.CausalEntityKind(kind) {
	case jhlog.CausalThread:
		return "Поток"
	case jhlog.CausalExecutorTask:
		return "Задача Executor"
	case jhlog.CausalHandlerPost:
		return "Постановка Handler"
	case jhlog.CausalCoroutine:
		return "Корутина"
	case jhlog.CausalWorkerAttempt:
		return "Попытка Worker"
	case jhlog.CausalResource:
		return "Ресурс"
	case jhlog.CausalOperation:
		return "Операция"
	case jhlog.CausalSeries:
		return "Периодическая серия"
	case jhlog.CausalExecutor:
		return "Executor"
	case jhlog.CausalDirectExecution:
		return "Наблюдавшееся выполнение Runnable/Callable"
	default:
		return "Неизвестная сущность"
	}
}

func concurrencyPhaseLabel(kind, phase uint64) string {
	labels := map[jhlog.CausalEntityKind][]string{
		jhlog.CausalThread:          {"", "Конструктор вернулся", "Фабрика вернула", "Вызван start", "start вернулся", "start выбросил исключение", "Начался корневой run", "Корневой run завершился", "Запрошен interrupt", "interrupt вернулся", "interrupt выбросил исключение", "Завершение наблюдалось"},
		jhlog.CausalExecutorTask:    {"", "Передана в Executor", "Вызов передачи вернулся", "Вызов передачи выбросил исключение", "Началось выполнение", "Выполнение завершилось", "Запрошена отмена", "Вызов отмены вернулся", "Терминальный исход наблюдался"},
		jhlog.CausalHandlerPost:     {"", "Вызван post", "post вернулся", "Начался dispatch", "dispatch завершился", "Запрошено удаление", "Вызов удаления вернулся", "post выбросил исключение", "remove выбросил исключение"},
		jhlog.CausalCoroutine:       {"", "Создание наблюдалось", "Возобновилась", "Приостановилась", "Запрошена отмена", "Исход наблюдался", "Впервые наблюдалась корутина; создание неизвестно"},
		jhlog.CausalWorkerAttempt:   {"", "Привязана к WorkManager", "Началась", "Возвращён retry", "Запрошена остановка", "Исход наблюдался", "Метод Worker наблюдался"},
		jhlog.CausalResource:        {"", "Попытка захвата", "Захвачен", "Захват не удался", "Освобождён", "Ожидание измерено", "Передан другому scope", "Передан в use; приобретение и открытость неизвестны"},
		jhlog.CausalOperation:       {"", "Стоимость измерена"},
		jhlog.CausalSeries:          {"", "Серия объявлена", "Завершение наблюдалось", "Запрошена отмена серии", "Вызов отмены серии вернулся"},
		jhlog.CausalExecutor:        {"", "Конструктор вернулся", "Фабрика вернула", "Запрошен shutdown", "shutdown вернулся", "Завершение наблюдалось", "Впервые наблюдался", "shutdown выбросил исключение"},
		jhlog.CausalDirectExecution: {"", "Выполнение началось", "Выполнение завершилось"},
	}
	list := labels[jhlog.CausalEntityKind(kind)]
	if phase > 0 && phase < uint64(len(list)) {
		return list[phase]
	}
	return fmt.Sprintf("Этап %d", phase)
}

func concurrencyResponsibilityLabel(value string) string {
	switch value {
	case "app_direct":
		return "Код приложения"
	case "library_initiated_by_app":
		return "Библиотека по вызову приложения (DEPENDENCY)"
	case "app_initiated_origin_unknown":
		return "Инициировано приложением; источник работы неизвестен"
	default:
		return "Ответственность неизвестна"
	}
}

func concurrencyKnownCost(eventKnown, bit, value uint64, unit string) string {
	if eventKnown&bit == 0 {
		return "не измерено"
	}
	if unit == "мс" {
		return fmt.Sprintf("%.3f мс", float64(value)/1e6)
	}
	return fmt.Sprintf("%d %s", value, strings.TrimSpace(unit))
}

func concurrencyHasKnownField(eventKnown, bit uint64) bool {
	return eventKnown&bit != 0
}

func concurrencyOperationLabel(value uint64) string {
	switch value {
	case jhlog.CausalOperationFileRead:
		return "Чтение файла"
	case jhlog.CausalOperationFileWrite:
		return "Запись файла"
	case jhlog.CausalOperationFileSync:
		return "Синхронизация файла"
	case jhlog.CausalOperationDatabase:
		return "База данных"
	case jhlog.CausalOperationHTTP:
		return "HTTP"
	case jhlog.CausalOperationLockWait:
		return "Ожидание lock"
	case jhlog.CausalOperationMonitorWait:
		return "Ожидание monitor"
	case jhlog.CausalOperationWakeLock:
		return "WakeLock"
	case jhlog.CausalOperationContentRead:
		return "Чтение ContentResolver"
	case jhlog.CausalOperationContentWrite:
		return "Запись ContentResolver"
	default:
		return "Операция не классифицирована"
	}
}

func concurrencyResourceLabel(value uint64) string {
	switch value {
	case jhlog.CausalResourceAutoCloseable:
		return "AutoCloseable"
	case jhlog.CausalResourceLock:
		return "Lock"
	case jhlog.CausalResourceMonitor:
		return "Monitor"
	case jhlog.CausalResourceWakeLock:
		return "WakeLock"
	default:
		return "Ресурс не классифицирован"
	}
}

func concurrencyCallResultLabel(value uint64) string {
	switch value {
	case 1:
		return "вызов вернул true / завершился нормально"
	case 2:
		return "вызов вернул false"
	case 3:
		return "вызов выбросил исключение"
	default:
		return "результат неизвестен"
	}
}

func concurrencyOutcomeLabel(value uint64) string {
	switch value {
	case 1:
		return "успех"
	case 2:
		return "ошибка"
	case 3:
		return "отменено"
	case 4:
		return "остановлено"
	case 5:
		return "отклонено"
	default:
		return "итог неизвестен"
	}
}

func concurrencyCadenceLabel(value uint64) string {
	switch value {
	case 1:
		return "однократно"
	case 2:
		return "fixed-rate"
	case 3:
		return "fixed-delay"
	default:
		return "режим неизвестен"
	}
}
