package analyze

// Matches applies the same contextual AND predicate in summaries and math.
// Class/owner alternatives are disjunctions inside their respective dimensions.
// An absent dimension cannot satisfy a nonempty filter. Global device context
// and custom metrics bypass this predicate, as declared by filterWarnings.
func (filter Filter) Matches(route, screen, owner string, classCandidates []string, ownerCandidates ...string) bool {
	if !containsFilter(route, filter.RouteContains) {
		return false
	}
	if !containsFilter(screen, filter.ScreenContains) {
		return false
	}
	if filter.ClassContains != "" && !containsAnyFilter(filter.ClassContains, classCandidates...) {
		return false
	}
	if filter.OwnerContains != "" {
		if !containsFilter(owner, filter.OwnerContains) &&
			!containsAnyFilter(filter.OwnerContains, ownerCandidates...) {
			return false
		}
	}
	return true
}

// Active only tests string lengths; the disabled predicate needs no event decoding.
func (filter Filter) Active() bool {
	return filter.RouteContains != "" || filter.ScreenContains != "" || filter.OwnerContains != "" || filter.ClassContains != ""
}
