package analyze

import (
	"fmt"
	"sort"
)

func (p *hprofParser) evidence() *HeapEvidence {
	evidence := &HeapEvidence{Sources: []string{p.path}}
	defer p.applyParseQuality(evidence)
	if len(p.roots) == 0 {
		evidence.Warnings = append(evidence.Warnings, "HPROF не содержит распознанных корней GC.")
		return evidence
	}
	parent := p.rootBFS()
	incoming := p.incomingEdges()
	targetsByClass := p.targetNodes(parent)
	rootIDs := p.rootIDs()
	exactBudget := &heapTraversalBudget{remaining: maxHprofRetainedVisits}
	reachabilityScratch := newHeapReachabilityScratch(len(parent))
	exactTargets := 0
	evidenceTargets := 0
	skippedEvidenceTargets := 0
	limitedRetainedTargets := 0
	classNames := make([]string, 0, len(targetsByClass))
	for className := range targetsByClass {
		classNames = append(classNames, className)
	}
	sort.Strings(classNames)
	for _, className := range classNames {
		ids := targetsByClass[className]
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		if len(ids) > maxHprofTargets {
			ids = ids[:maxHprofTargets]
			p.degrade("target-objects", fmt.Sprintf(
				"HPROF содержит больше %d достижимых target-объектов одного класса: размер и пути рассчитаны по ограниченной выборке.",
				maxHprofTargets,
			))
		}
		best := HeapLeakEvidence{ClassName: className, Source: p.path}
		bestTargetID := uint64(0)
		bestRetainedExact := false
		for _, id := range ids {
			if evidenceTargets >= maxHprofEvidenceTargets {
				skippedEvidenceTargets++
				continue
			}
			evidenceTargets++
			retainedSize, retainedCount, retainedSample, exact := p.retainedSizeForLimited(
				id,
				rootIDs,
				parent,
				reachabilityScratch,
				exactBudget,
				exactTargets < maxHprofExactTargets,
			)
			if exact {
				exactTargets++
			}
			path := p.referencePath(parent, id)
			root := heapRootLabel(path)
			holder := heapHolder(path, className)
			holderField := heapHolderField(path, className)
			rootCategory := heapRootCategory(root)
			referenceMatchers := heapReferenceMatchers(path)
			pattern := heapLeakPattern(className, holder, holderField, rootCategory, path)
			confidence := heapEvidenceConfidence(exact, referenceMatchers)
			candidate := HeapLeakEvidence{
				ClassName:           className,
				Holder:              holder,
				HolderField:         holderField,
				GCRoot:              root,
				GCRootCategory:      rootCategory,
				ChainFingerprint:    heapChainFingerprint(className, holder, holderField, rootCategory, path),
				RetainedSizeKB:      bytesToKB(retainedSize),
				RetainedSizeBytes:   retainedSize,
				RetainedObjectCount: retainedCount,
				ReferencePath:       path,
				DominatorTree:       retainedSample,
				LeakPattern:         pattern,
				ReferenceMatchers:   referenceMatchers,
				Source:              p.path,
				Confidence:          confidence,
			}
			if betterHeapLeak(candidate, best) {
				best = candidate
				bestTargetID = id
				bestRetainedExact = exact
			}
		}
		// Alternative paths are supplemental output and do not choose the primary suspect. Searching
		// only for the selected object prevents discarded, highly connected objects from exhausting
		// the bounded path-state budget and falsely degrading the evidence that is actually returned.
		if bestTargetID != 0 {
			best.AlternativePaths = p.alternativeReferencePaths(
				parent,
				incoming,
				bestTargetID,
				best.ReferencePath,
			)
			if !bestRetainedExact {
				limitedRetainedTargets++
			}
		}
		if best.RetainedObjectCount == 0 {
			best.RetainedObjectCount = uint64(len(ids))
		}
		normalizeHeapLeak(&best)
		evidence.Leaks = append(evidence.Leaks, best)
	}
	if limitedRetainedTargets > 0 {
		p.degrade("retained-traversal", fmt.Sprintf(
			"Точный удержанный размер ограничен: для %d объектов использована безопасная оценка, чтобы большой HPROF не зависал.",
			limitedRetainedTargets,
		))
	}
	if skippedEvidenceTargets > 0 {
		p.degrade("evidence-targets", fmt.Sprintf(
			"HPROF содержит слишком много кандидатов удержания: пропущено %d объектов после лимита %d.",
			skippedEvidenceTargets,
			maxHprofEvidenceTargets,
		))
	}
	sort.Slice(evidence.Leaks, func(i, j int) bool {
		if evidence.Leaks[i].RetainedSizeKB == evidence.Leaks[j].RetainedSizeKB {
			return evidence.Leaks[i].ClassName < evidence.Leaks[j].ClassName
		}
		return evidence.Leaks[i].RetainedSizeKB > evidence.Leaks[j].RetainedSizeKB
	})
	return evidence
}
