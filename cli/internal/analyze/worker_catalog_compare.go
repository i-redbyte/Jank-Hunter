package analyze

import "sort"

type WorkerCatalogComparison struct {
	BaselineCount, CandidateCount *int
	Comparable                    bool
	OnlyBaseline, OnlyCandidate   []string
	OmittedNames                  int
	Note                          string
}

func compareWorkerCatalogs(before, after *WorkerCatalogAnalysis) WorkerCatalogComparison {
	inventory := func(a *WorkerCatalogAnalysis) (map[string]bool, bool, bool) {
		classes := make(map[string]bool)
		known := false
		if a == nil {
			return classes, false, false
		}
		complete := a.DroppedDocuments == 0 && len(a.Observations) > 0
		for _, o := range a.Observations {
			c := o.Catalog
			if c == nil || c.Status != "available" {
				complete = false
				continue
			}
			known = true
			complete = complete && c.OmittedWorkers == 0 && c.UnresolvedClasses == 0
			for _, name := range c.Classes {
				classes[name] = true
			}
		}
		return classes, known, complete
	}
	left, knownLeft, completeLeft := inventory(before)
	right, knownRight, completeRight := inventory(after)
	result := WorkerCatalogComparison{Comparable: knownLeft && knownRight && completeLeft && completeRight,
		Note: "Исходные имена до R8; классы динамических модулей вне базового приложения не охвачены. Отличие каталога не означает изменение числа зарегистрированных заданий или запусков."}
	if knownLeft {
		n := len(left)
		result.BaselineCount = &n
	}
	if knownRight {
		n := len(right)
		result.CandidateCount = &n
	}
	if !result.Comparable {
		result.Note += " Каталог отсутствует или неполон: отсутствие класса не доказывает его удаление."
	}
	if knownLeft && knownRight {
		for name := range left {
			if !right[name] {
				result.OnlyBaseline = append(result.OnlyBaseline, name)
			}
		}
		for name := range right {
			if !left[name] {
				result.OnlyCandidate = append(result.OnlyCandidate, name)
			}
		}
		sort.Strings(result.OnlyBaseline)
		sort.Strings(result.OnlyCandidate)
		for _, names := range []*[]string{&result.OnlyBaseline, &result.OnlyCandidate} {
			if len(*names) > 100 {
				result.OmittedNames += len(*names) - 100
				*names = (*names)[:100]
			}
		}
	}
	return result
}
