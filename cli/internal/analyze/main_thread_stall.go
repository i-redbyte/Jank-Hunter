package analyze

import "strings"

type MainThreadStallDiagnosis struct {
	Title       string
	Explanation string
	Action      string
}

func BestMainThreadStallStack(owners []OwnerStats, owner string) string {
	owner = strings.TrimSpace(owner)
	var stack string
	var maxMS uint64
	for _, item := range owners {
		if item.Kind != "main_thread_stall" ||
			!strings.EqualFold(strings.TrimSpace(item.Owner), owner) ||
			strings.TrimSpace(item.StackHint) == "" {
			continue
		}
		if item.MaxMS >= maxMS {
			maxMS = item.MaxMS
			stack = item.StackHint
		}
	}
	return stack
}

func DiagnoseMainThreadStall(owner, stack string) MainThreadStallDiagnosis {
	location := strings.ToLower(owner + " " + stack)
	switch {
	case strings.Contains(location, "componentfactoryimpl.create"):
		return MainThreadStallDiagnosis{
			Title:       "Пауза произошла во время создания DI-компонента",
			Explanation: "Пауза главного потока измерена напрямую, а снимок стека попал в ComponentFactoryImpl.create. Ветвление фабрики обычно лишь открывает путь к методам предоставления и получения зависимостей: задержку могут создавать синхронная инициализация графа, блокировка или I/O внутри создаваемого компонента. Снимок не доказывает, что всё время потрачено именно в строке фабрики.",
			Action:      "Профилируйте вызываемую из этой ветки цепочку предоставления зависимостей и дочерние конструкторы. Отдельно измерьте ожидание блокировок, I/O и тяжёлую инициализацию; безопасную подготовку перенесите с главного потока или разделите граф на ленивые зависимости.",
		}
	case strings.Contains(location, "dagger"), strings.Contains(location, "$builder.build"):
		return MainThreadStallDiagnosis{
			Title:       "Пауза произошла во время сборки Dagger-компонента",
			Explanation: "Пауза измерена на главном потоке, а снимок стека попал в метод build созданного Dagger-компонента. Созданный Dagger сборщик обычно только запускает построение графа; фактическую стоимость могут создавать методы модулей, предоставляющие зависимости, конструкторы, блокировки или синхронный I/O.",
			Action:      "Раскройте создаваемую этой веткой часть Dagger-графа и отдельно профилируйте методы предоставления зависимостей и конструкторы. Уберите I/O и ожидания, тяжёлую безопасную подготовку перенесите с главного потока, а необязательные ветви графа сделайте ленивыми.",
		}
	case strings.Contains(location, "arraylinkedvariables.add"), strings.Contains(location, "constraintlayout.core"):
		return MainThreadStallDiagnosis{
			Title:       "Пауза произошла во время работы решателя ConstraintLayout",
			Explanation: "Пауза измерена на главном потоке, а снимок пришёлся на решатель ограничений ConstraintLayout. Метод библиотеки — место выполнения, но первопричина обычно находится в компоновке приложения: сложной системе ограничений, повторных requestLayout или многократных проходах измерения и размещения за один кадр.",
			Action:      "Найдите компоновку и действие приложения, вызвавшие этот проход: запишите Perfetto/System Trace с этапами измерения и размещения View, посчитайте повторные requestLayout, упростите ограничения и иерархию, затем сравните число проходов и максимальную паузу.",
		}
	case strings.Contains(location, ".ondraw"), strings.Contains(location, ".dispatchdraw"):
		return MainThreadStallDiagnosis{
			Title:       "Пользовательский View блокировал отрисовку в onDraw",
			Explanation: "Пауза главного потока измерена, а снимок стека попал в onDraw/dispatchDraw. Это делает тяжёлые вычисления, создание объектов или сложную геометрию в этом методе сильным кандидатом, но полную длительность последнего метода стека нужно подтвердить трассой.",
			Action:      "Откройте указанный onDraw/dispatchDraw: вынесите вычисления, не создавайте объекты на каждом кадре, кэшируйте Path/Bitmap/Shader и проверьте частоту invalidate.",
		}
	case strings.Contains(location, ".onmeasure"), strings.Contains(location, ".onlayout"):
		return MainThreadStallDiagnosis{
			Title:       "Измерение или размещение View блокировало главный поток",
			Explanation: "Пауза главного потока измерена, а снимок стека попал в onMeasure/onLayout. Это указывает на повторные requestLayout, тяжёлое измерение или слишком сложную иерархию View, но вклад конкретного прохода нужно подтвердить трассой.",
			Action:      "Проверьте число onMeasure/onLayout за кадр, сократите вложенность и не вызывайте requestLayout без изменения размеров.",
		}
	case strings.Contains(location, ".<init>"), strings.Contains(location, "<clinit>"):
		return MainThreadStallDiagnosis{
			Title:       "Пауза произошла во время конструктора или инициализатора класса",
			Explanation: "Пауза измерена на главном потоке, а снимок стека попал в <init>/<clinit>. Причиной может быть большой конструктор, инициализация перечисления или статического состояния, создание коллекций, блокировка или скрытая работа вызываемых зависимостей; последний метод стека остаётся точкой входа, а не доказательством полной стоимости.",
			Action:      "Профилируйте конструктор и статический инициализатор вместе с вызываемыми методами: вынесите тяжёлые вычисления и I/O, сократите преждевременное создание коллекций и объектов и заранее подготовьте неизменяемые данные вне критического UI-сценария.",
		}
	default:
		return MainThreadStallDiagnosis{
			Title:       "Главный поток не обрабатывал кадры",
			Explanation: "Это прямое подтверждение подвисания главного потока. Строка стека показывает место, где поток находился при снимке, но не заменяет полную трассу вызовов.",
			Action:      "Откройте указанный метод и его вызывающую цепочку: ищите синхронный I/O, ожидание блокировки, тяжёлое вычисление или большую работу измерения, размещения и отрисовки.",
		}
	}
}
