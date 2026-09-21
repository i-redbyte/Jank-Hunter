package mathanalysis

const bucketPageSize = 32

// Pages keep isolated observations sparse while storing dense stretches as primitive arrays.
// Storage depends on occupied pages, not the duration of the entire recording.
type bucketSeries struct {
	pages           map[int]*[bucketPageSize]float64
	positiveBuckets int
	total           float64
}

func (s *bucketSeries) add(index int, value float64, account *collectionAccount) bool {
	pageIndex := index / bucketPageSize
	page := s.pages[pageIndex]
	if page == nil {
		bytes := mathMapEntryBytes + bucketPageSize*8
		if s.pages == nil {
			bytes += mathMapBaseBytes
		}
		if !account.reserve(bytes) {
			return false
		}
		if s.pages == nil {
			s.pages = make(map[int]*[bucketPageSize]float64)
		}
		page = new([bucketPageSize]float64)
		s.pages[pageIndex] = page
	}
	position := index % bucketPageSize
	if page[position] == 0 {
		s.positiveBuckets++
	}
	page[position] += value
	s.total += value
	return true
}

// The caller supplies a zeroed destination, which can be reused for each candidate.
func (s *bucketSeries) writeDense(points []float64) {
	for index, page := range s.pages {
		start := index * bucketPageSize
		if start < len(points) {
			copy(points[start:], page[:])
		}
	}
}
