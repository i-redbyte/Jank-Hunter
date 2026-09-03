package analyze

import (
	"fmt"
	"math"
	"sort"
)

type operationDurationSummary struct {
	exact         []uint64
	approximation *operationQuantileApproximation
	count         uint64
	max           uint64
	sorted        bool
}

type operationQuantileApproximation struct {
	p50 p2Quantile
	p90 p2Quantile
	p95 p2Quantile
}

type p2Quantile struct {
	probability float64
	count       uint64
	initial     [5]float64
	heights     [5]float64
	positions   [5]int64
	desired     [5]float64
}

func (s *operationDurationSummary) add(value uint64) {
	s.count++
	s.max = maxUint64(s.max, value)
	if s.approximation != nil {
		s.approximation.add(value)
		return
	}
	if len(s.exact) < operationExactDurationLimit {
		s.exact = append(s.exact, value)
		s.sorted = false
		return
	}
	s.approximation = newOperationQuantileApproximation()
	for _, exact := range s.exact {
		s.approximation.add(exact)
	}
	s.exact = nil
	s.approximation.add(value)
}

func (s *operationDurationSummary) percentile(probability float64) uint64 {
	if s.count == 0 {
		return 0
	}
	if s.approximation != nil {
		p50, p90, p95 := s.approximation.quantiles(s.max)
		switch probability {
		case 0.50:
			return p50
		case 0.90:
			return p90
		case 0.95:
			return p95
		default:
			panic(fmt.Sprintf("unsupported operation percentile %.4f", probability))
		}
	}
	if !s.sorted {
		sort.Slice(s.exact, func(i, j int) bool { return s.exact[i] < s.exact[j] })
		s.sorted = true
	}
	target := int(math.Ceil(float64(len(s.exact)) * probability))
	target = max(1, min(target, len(s.exact)))
	return s.exact[target-1]
}

func (s *operationDurationSummary) approximated() bool {
	return s.approximation != nil
}

func newOperationQuantileApproximation() *operationQuantileApproximation {
	return &operationQuantileApproximation{
		p50: p2Quantile{probability: 0.50},
		p90: p2Quantile{probability: 0.90},
		p95: p2Quantile{probability: 0.95},
	}
}

func (a *operationQuantileApproximation) add(value uint64) {
	a.p50.add(value)
	a.p90.add(value)
	a.p95.add(value)
}

func (a *operationQuantileApproximation) quantiles(maximum uint64) (uint64, uint64, uint64) {
	p50 := minUint64(a.p50.value(), maximum)
	p90 := maxUint64(p50, minUint64(a.p90.value(), maximum))
	p95 := maxUint64(p90, minUint64(a.p95.value(), maximum))
	return p50, p90, p95
}

func (q *p2Quantile) add(value uint64) {
	x := float64(value)
	if q.count < uint64(len(q.initial)) {
		q.initial[q.count] = x
		q.count++
		if q.count == uint64(len(q.initial)) {
			q.initialize()
		}
		return
	}

	q.count++
	cell := 0
	switch {
	case x < q.heights[0]:
		q.heights[0] = x
	case x >= q.heights[4]:
		q.heights[4] = x
		cell = 3
	default:
		for cell < 3 && x >= q.heights[cell+1] {
			cell++
		}
	}
	for index := cell + 1; index < len(q.positions); index++ {
		q.positions[index]++
	}
	increments := [5]float64{0, q.probability / 2, q.probability, (1 + q.probability) / 2, 1}
	for index := range q.desired {
		q.desired[index] += increments[index]
	}
	for index := 1; index < len(q.heights)-1; index++ {
		delta := q.desired[index] - float64(q.positions[index])
		direction := int64(0)
		if delta >= 1 && q.positions[index+1]-q.positions[index] > 1 {
			direction = 1
		} else if delta <= -1 && q.positions[index-1]-q.positions[index] < -1 {
			direction = -1
		}
		if direction == 0 {
			continue
		}
		candidate := q.parabolic(index, direction)
		if candidate > q.heights[index-1] && candidate < q.heights[index+1] {
			q.heights[index] = candidate
		} else {
			neighbor := index + int(direction)
			q.heights[index] += float64(direction) *
				(q.heights[neighbor] - q.heights[index]) /
				float64(q.positions[neighbor]-q.positions[index])
		}
		q.positions[index] += direction
	}
}

func (q *p2Quantile) initialize() {
	sort.Float64s(q.initial[:])
	copy(q.heights[:], q.initial[:])
	q.positions = [5]int64{1, 2, 3, 4, 5}
	q.desired = [5]float64{
		1,
		1 + 2*q.probability,
		1 + 4*q.probability,
		3 + 2*q.probability,
		5,
	}
}

func (q *p2Quantile) parabolic(index int, direction int64) float64 {
	position := q.positions[index]
	leftPosition := q.positions[index-1]
	rightPosition := q.positions[index+1]
	height := q.heights[index]
	adjustment := (float64(position-leftPosition+direction)*(q.heights[index+1]-height)/float64(rightPosition-position) +
		float64(rightPosition-position-direction)*(height-q.heights[index-1])/float64(position-leftPosition))
	return height + float64(direction)/float64(rightPosition-leftPosition)*adjustment
}

func (q *p2Quantile) value() uint64 {
	if q.count == 0 {
		return 0
	}
	if q.count < uint64(len(q.initial)) {
		copy := q.initial
		values := copy[:q.count]
		sort.Float64s(values)
		target := int(math.Ceil(float64(q.count)*q.probability)) - 1
		return uint64(values[max(0, min(target, len(values)-1))])
	}
	value := math.Round(q.heights[2])
	if value <= 0 {
		return 0
	}
	if value >= math.Ldexp(1, 64) {
		return ^uint64(0)
	}
	return uint64(value)
}
