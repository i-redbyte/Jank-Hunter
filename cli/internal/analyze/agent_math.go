package analyze

func firstNonZero(values ...uint64) uint64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func saturatingAdd(left, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

func absInt64(value int64) int64 {
	if value == -1<<63 {
		return 1<<63 - 1
	}
	if value < 0 {
		return -value
	}
	return value
}

func int64Distance(left, right int64) uint64 {
	if left > right {
		left, right = right, left
	}
	if left < 0 && right >= 0 {
		return saturatingAdd(uint64(-(left+1))+1, uint64(right))
	}
	return uint64(right - left)
}

func signedDifference(left, right uint64) int64 {
	const maxSigned = uint64(1<<63 - 1)
	if left >= right {
		delta := left - right
		if delta > maxSigned {
			return int64(maxSigned)
		}
		return int64(delta)
	}
	delta := right - left
	if delta > maxSigned {
		return -int64(maxSigned)
	}
	return -int64(delta)
}

func saturatingMultiply(value, multiplier uint64) uint64 {
	if multiplier != 0 && value > ^uint64(0)/multiplier {
		return ^uint64(0)
	}
	return value * multiplier
}

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
