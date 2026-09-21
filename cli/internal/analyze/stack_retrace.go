package analyze

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"github.com/i-redbyte/jank-hunter/cli/internal/retrace"
)

type StackRetraceStatus string

const (
	StackRetraceResolved  StackRetraceStatus = "resolved"
	StackRetraceUnchanged StackRetraceStatus = "unchanged"
	StackRetraceAmbiguous StackRetraceStatus = "ambiguous"
	StackRetraceRemoved   StackRetraceStatus = "removed"
)

// StackRetraceEvidence retains the raw observation and every official alternative.
// An inline chain describes mapping, not proof of causal ownership of the stall.
type StackRetraceEvidence struct {
	Truncated bool
	Raw       string
	Status    StackRetraceStatus
	Groups    []retrace.FrameGroup
}

func (m *NameMapping) RetraceStacks(ctx context.Context, stacks []string) ([]StackRetraceEvidence, error) {
	if m == nil || m.path == "" {
		return nil, fmt.Errorf("stack Retrace requires a loaded mapping file")
	}
	backend, err := retrace.Discover()
	if err != nil {
		return nil, err
	}
	requests := make([]retrace.Request, len(stacks))
	for index, stack := range stacks {
		requests[index] = retrace.StackRequest{Text: standardStack(stack)}
	}
	results, err := backend.Run(ctx, m.path, m.digest, requests)
	if err != nil {
		return nil, err
	}
	evidence := make([]StackRetraceEvidence, len(stacks))
	for index, result := range results {
		stack := result.(retrace.StackResult)
		item := StackRetraceEvidence{Raw: stacks[index], Groups: stack.Groups, Status: StackRetraceResolved, Truncated: stacks[index] == "!" || strings.Contains(stacks[index], "[Jank Hunter: stack truncated]")}
		lines := 0
		for _, group := range stack.Groups {
			if group.Ambiguous || len(group.Alternatives) > 1 {
				item.Status = StackRetraceAmbiguous
			}
			for _, alternative := range group.Alternatives {
				lines += len(alternative)
			}
		}
		if lines == 0 {
			item.Status = StackRetraceRemoved
		} else if item.Status != StackRetraceAmbiguous && item.Text() == standardStack(stacks[index]) {
			item.Status = StackRetraceUnchanged
		}
		evidence[index] = item
	}
	return evidence, nil
}

// Watchdog legacy records contain a bare StackTraceElement rather than a Java "at" line.
func standardStack(stack string) string {
	lines := strings.Split(strings.TrimSpace(stack), "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "at ") && strings.Contains(trimmed, "(") && strings.HasSuffix(trimmed, ")") {
			lines[index] = "\tat " + trimmed
		}
	}
	return strings.Join(lines, "\n")
}

func (e StackRetraceEvidence) Text() string {
	var lines []string
	for _, group := range e.Groups {
		for index, alternative := range group.Alternatives {
			if len(group.Alternatives) > 1 {
				lines = append(lines, fmt.Sprintf("[alternative %d/%d]", index+1, len(group.Alternatives)))
			}
			lines = append(lines, alternative...)
		}
	}
	if len(lines) == 0 {
		return "[frame removed by R8 mapping]"
	}
	return strings.Join(lines, "\n")
}

func (c *collector) retraceStackHints() error {
	if c.nameMap == nil {
		return nil
	}
	// The report keeps one representative stack per owner/kind. Sort/deduplicate those
	// retained samples, not the unbounded event stream, and invoke one bounded JVM batch.
	samples := make(map[string]struct{})
	for _, owner := range c.ownerStats {
		if owner.StackHint != "" && owner.StackOrigin == jhlog.SymbolOriginRuntimeStack {
			if _, exists := samples[owner.StackHint]; !exists && len(samples) >= retrace.MaxRequests {
				return fmt.Errorf("stack Retrace exceeds %d distinct samples", retrace.MaxRequests)
			}
			samples[owner.StackHint] = struct{}{}
		}
	}
	if len(samples) == 0 {
		return nil
	}
	stacks := make([]string, 0, len(samples))
	for stack := range samples {
		stacks = append(stacks, stack)
	}
	sort.Strings(stacks)
	results, err := c.nameMap.RetraceStacks(context.Background(), stacks)
	if err != nil {
		return err
	}
	byRaw := make(map[string]*StackRetraceEvidence, len(results))
	for index := range results {
		byRaw[results[index].Raw] = &results[index]
	}
	for _, owner := range c.ownerStats {
		if result := byRaw[owner.StackHint]; result != nil && owner.StackOrigin == jhlog.SymbolOriginRuntimeStack {
			owner.StackRetrace = result
			owner.StackHint = result.Text()
		}
	}
	return nil
}

func (e StackRetraceEvidence) StatusLabel() string {
	if e.Truncated {
		return "Retrace: исходный стек обрезан; показаны доступные кадры"
	}
	switch e.Status {
	case StackRetraceResolved:
		return "Retrace: восстановленные кадры"
	case StackRetraceAmbiguous:
		return "Retrace: неоднозначно, показаны все варианты"
	case StackRetraceRemoved:
		return "Retrace: кадр удалён согласно mapping"
	default:
		return "Retrace: исходная запись не изменилась"
	}
}
