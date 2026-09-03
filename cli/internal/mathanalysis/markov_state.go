package mathanalysis

import "fmt"

func markovConfidence(states []MarkovBucketState, transitions []MarkovTransition, badEpisodes int) (string, string) {
	sampleCount := len(states)
	transitionCount := markovTransitionEventCount(transitions)
	switch {
	case sampleCount < 2:
		return "low", "меньше двух временных интервалов: матрица переходов не строится"
	case sampleCount < 4 || transitionCount < 3:
		return "low", fmt.Sprintf("окон=%d, переходов=%d: вероятности будут скачкообразными", sampleCount, transitionCount)
	case badEpisodes == 0 && sampleCount >= 10:
		return "high", fmt.Sprintf("окон=%d, плохих эпизодов нет: вывод об отсутствии деградации устойчивее", sampleCount)
	case badEpisodes == 0:
		return "medium", fmt.Sprintf("окон=%d, плохих эпизодов нет: для спокойного сценария данных достаточно, для метрик восстановления нет", sampleCount)
	case sampleCount < 10 || badEpisodes < 2:
		return "medium", fmt.Sprintf("интервалов=%d, плохих эпизодов=%d: восстановление и повторение плохих состояний лучше подтвердить повтором", sampleCount, badEpisodes)
	default:
		return "high", fmt.Sprintf("окон=%d, плохих эпизодов=%d, переходов=%d: выборка достаточна для марковских выводов", sampleCount, badEpisodes, transitionCount)
	}
}

func MarkovStateLabel(state string) string {
	switch state {
	case markovHealthy:
		return "Здоровое"
	case markovNetworkLoop:
		return "Сетевой цикл"
	case markovNetworkSlow:
		return "Медленная сеть"
	case markovJanky:
		return "Подтормаживание UI"
	case markovStalled:
		return "Пауза главного потока"
	case markovMemoryPressure:
		return "Давление памяти"
	case markovRecovering:
		return "Восстановление"
	default:
		return state
	}
}

func markovIsBadState(state string) bool {
	switch state {
	case markovNetworkLoop, markovNetworkSlow, markovJanky, markovStalled, markovMemoryPressure:
		return true
	default:
		return false
	}
}

func markovStateRank(state string) int {
	for index, item := range markovStateOrder {
		if item == state {
			return index
		}
	}
	return len(markovStateOrder)
}
