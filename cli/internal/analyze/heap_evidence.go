package analyze

import (
	"fmt"
	"sort"
)

func (p *hprofParser) evidence() *HeapEvidence {
	return p.evidenceWithBudget(&heapTraversalBudget{remaining: maxHprofRetainedWork})
}

func (p *hprofParser) evidenceWithBudget(exactBudget *heapTraversalBudget) *HeapEvidence {
	evidence := &HeapEvidence{Sources: []string{p.path}}
	defer p.applyParseQuality(evidence)
	var parent *heapParentIndex
	if len(p.roots) == 0 {
		evidence.AddDiagnostic(HeapDiagnostic{Code: "missing_roots", Severity: HeapDiagnosticWarning, Impact: HeapImpactGraph, Source: p.path, Message: "HPROF не содержит распознанных корней GC."})
	} else {
		parent = p.rootBFS()
	}
	targetsByClass := p.targetNodes()
	rootIDs := p.rootIDs()
	var reachabilityScratch *heapReachabilityScratch
	var dominators *heapDominatorIndex
	reachableTargets := 0
	for _, ids := range targetsByClass {
		for _, id := range ids {
			if _, ok := p.parentFor(parent, id); ok {
				reachableTargets++
			}
		}
	}
	useDominators := reachableTargets > 4
	if parent != nil && reachableTargets > 0 {
		if useDominators {
			dominators = p.dominators(exactBudget, parent.reachable)
		} else if exactBudget.takeN(2 * len(p.nodes)) {
			reachabilityScratch = newHeapReachabilityScratch(len(p.nodes))
		}
	}
	evidenceTargets := 0
	skippedEvidenceTargets := 0
	limitedRetainedTargets := 0
	type selectedHeapTarget struct {
		evidenceIndex int
		targetID      uint64
	}
	selectedTargets := make([]selectedHeapTarget, 0, len(targetsByClass))
	classNames := make([]string, 0, len(targetsByClass))
	for className := range targetsByClass {
		classNames = append(classNames, className)
	}
	sort.Strings(classNames)
	for _, className := range classNames {
		ids := targetsByClass[className]
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		reachableCount := 0
		for _, id := range ids {
			if _, reachable := p.parentFor(parent, id); reachable {
				ids[reachableCount] = id
				reachableCount++
			}
		}
		reachableIDs := ids[:reachableCount]
		if len(reachableIDs) > maxHprofTargets {
			reachableIDs = reachableIDs[:maxHprofTargets]
			p.degrade("target-objects", fmt.Sprintf(
				"HPROF содержит больше %d достижимых подозрительных объектов одного класса: размер и пути рассчитаны по ограниченной выборке.",
				maxHprofTargets,
			))
		}
		if len(reachableIDs) == 0 {
			reachability := EvidenceNegative
			confidence := "высокое: объект найден в HPROF, но не достижим от распознанных корней GC"
			if len(p.roots) == 0 {
				reachability = EvidenceUnknown
				confidence = "низкое: объект найден в HPROF, но нет распознанных GC root для проверки достижимости"
			}
			evidence.Leaks = append(evidence.Leaks, HeapLeakEvidence{
				Reachability:       reachability,
				RetainedSizeState:  HeapSizeUnknown,
				ReferencePathState: HeapPathUnknown,
				ClassName:          className,
				Source:             p.path,
				Confidence:         confidence,
			})
			continue
		}
		best := HeapLeakEvidence{ClassName: className, Source: p.path, RetainedSizeState: HeapSizeUnknown, ReferencePathState: HeapPathUnknown}
		bestTargetID := uint64(0)
		bestRetainedExact := false
		for _, id := range reachableIDs {
			if evidenceTargets >= maxHprofEvidenceTargets {
				skippedEvidenceTargets++
				continue
			}
			evidenceTargets++
			var retainedSize, retainedCount uint64
			var retainedSample []string
			var exact bool
			if useDominators {
				if dominators != nil {
					v := dominators.dfs[p.nodeIndexes[id]]
					retainedSize, retainedCount, exact = dominators.size[v], dominators.count[v], true
				} else {
					retainedSize, retainedCount, retainedSample, exact = p.shallowRetainedFallback(id)
				}
			} else {
				retainedSize, retainedCount, retainedSample, exact = p.retainedSizeForLimited(id, rootIDs, parent, reachabilityScratch, exactBudget)
			}

			path := p.referencePath(parent, id)
			rootInfo, _ := p.pathRoot(parent, id)
			root := rootInfo.kind
			holder := heapHolder(path, className)
			holderField := heapHolderField(path, className)
			rootCategory := heapRootCategory(root)
			referenceMatchers := heapReferenceMatchers(path)
			pattern := heapLeakPattern(className, holder, holderField, rootCategory, path)
			confidence := heapEvidenceConfidence(exact, referenceMatchers)
			candidate := HeapLeakEvidence{
				Reachability:        EvidencePositive,
				ReferencePathState:  heapReferencePathState(path),
				RetainedSizeState:   heapRetainedSizeState(exact),
				ClassName:           className,
				Holder:              holder,
				HolderField:         holderField,
				GCRoot:              root,
				GCRootObjectID:      fmt.Sprintf("0x%x", rootInfo.id),
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
		if bestTargetID != 0 && useDominators && bestRetainedExact {
			_, _, best.DominatorTree, _ = p.retainedByDominator(bestTargetID, dominators, exactBudget)
		}
		if bestTargetID != 0 && !bestRetainedExact {
			limitedRetainedTargets++
		}
		if best.RetainedObjectCount == 0 {
			best.RetainedObjectCount = uint64(len(reachableIDs))
		}
		normalizeHeapLeak(&best)
		evidence.Leaks = append(evidence.Leaks, best)
		if bestTargetID != 0 {
			selectedTargets = append(selectedTargets, selectedHeapTarget{
				evidenceIndex: len(evidence.Leaks) - 1,
				targetID:      bestTargetID,
			})
		}
	}
	// Alternative paths are supplemental. Build reverse edges only for nodes on selected primary
	// paths instead of duplicating the complete HPROF edge set in memory.
	if len(selectedTargets) > 0 {
		relevantTargets := map[uint64]struct{}{}
		for _, selected := range selectedTargets {
			for _, id := range p.heapPathNodeIDs(parent, selected.targetID) {
				relevantTargets[id] = struct{}{}
			}
		}
		incoming := p.incomingEdges(relevantTargets, exactBudget)
		for _, selected := range selectedTargets {
			leak := &evidence.Leaks[selected.evidenceIndex]
			leak.AlternativePaths = p.alternativeReferencePaths(
				parent,
				incoming,
				selected.targetID,
				leak.ReferencePath,
				exactBudget,
			)
		}
	}
	if exactBudget.exhausted {
		p.degrade("analysis-work", "Исчерпан общий бюджет работы HPROF: повторные рёбра, обходы и полные сканирования учитываются для всех объектов; незавершённые результаты явно ограничены.")
	}
	if limitedRetainedTargets > 0 {
		p.degrade("retained-traversal", fmt.Sprintf(
			"Точный удержанный размер ограничен: для %d объектов использована безопасная оценка, чтобы большой HPROF не зависал.",
			limitedRetainedTargets,
		))
	}
	if skippedEvidenceTargets > 0 {
		p.degrade("evidence-targets", fmt.Sprintf(
			"HPROF содержит слишком много подозрительных удержаний: пропущено %d объектов после ограничения %d.",
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
