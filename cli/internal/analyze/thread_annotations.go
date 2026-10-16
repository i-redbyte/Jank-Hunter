package analyze

import (
	"fmt"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A point on a common recording timeline, never a causal link to a thread.
type ThreadAnnotation struct {
	TimeBasis                               string
	Scope, Kind, Label, Screen, OperationID string
	TimeMS, DurationMS                      uint64
}
type threadRawAnnotation struct {
	log   uint64
	value ThreadAnnotation
}

func annotationText(value string) string {
	if len(value) <= 256 {
		return value
	}
	end := 256
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return strings.Clone(value[:end]) + "…"
}
func (c *collector) recordThreadAnnotation(dict map[uint64]string, event jhlog.Event) {
	mark := ThreadAnnotation{TimeMS: event.TimeMS, TimeBasis: "record_time"}
	if at := observationUnixMS(c.workRegistry.anchorElapsedMS, c.workRegistry.anchorUnixMS, event.TimeMS); at != nil {
		mark.TimeMS = *at
		mark.TimeBasis = "observation_clock"
	}
	switch {
	case event.Operation != nil:
		if event.Operation.Phase == jhlog.OperationPhaseStarted {
			mark.Kind = "operation_start"
		} else if event.Operation.Phase == jhlog.OperationPhaseFinished {
			mark.Kind = "operation_finish"
		} else {
			return
		}
		mark.Label = annotationText(c.resolveSymbol(dict, event.Operation.NameRef))
		mark.OperationID = strconv.FormatUint(event.Operation.ID, 10)
	case event.Worker != nil && event.Worker.Stage == jhlog.WorkerStageStarted:
		mark.Kind = "worker_start"
		mark.Label = annotationText(c.resolveOwnerRef(dict, event.Worker.WorkerRef))
	case event.ProcessState != nil:
		mark.Kind = "ui_visibility"
		switch event.ProcessState.UIVisibility {
		case jhlog.ProcessUIVisible:
			mark.Label = "UI видим"
		case jhlog.ProcessUIHidden:
			mark.Label = "UI скрыт"
		default:
			mark.Label = "видимость UI неизвестна"
		}
	case event.Stall != nil:
		mark.Kind = "ui_stall"
		mark.DurationMS = event.Stall.DurationMS
		mark.Label = fmt.Sprintf("наблюдение зависания UI; состояние %d", event.Stall.State)
	case event.UIWindow != nil && event.UIWindow.JankCount > 0:
		mark.Kind = "ui_jank_window"
		mark.DurationMS = event.UIWindow.WindowMS
		mark.Label = fmt.Sprintf("проблемных кадров: %d", event.UIWindow.JankCount)
	case event.Context != nil && event.Attribution.Present && c.currentAttrScreen != "":
		mark.Kind = "screen_observation"
		mark.Label = annotationText(c.currentAttrScreen)
	default:
		return
	}
	if event.Attribution.Present {
		mark.Screen = annotationText(c.currentAttrScreen)
		if mark.OperationID == "" && event.Attribution.OperationID != 0 {
			mark.OperationID = strconv.FormatUint(event.Attribution.OperationID, 10)
		}
	}
	s := &c.threadAnalysis
	if len(s.annotations) >= 5000 {
		s.droppedAnnotations = saturatingUint64Sum(s.droppedAnnotations, 1)
		return
	}
	s.annotations = append(s.annotations, threadRawAnnotation{c.currentLogIndex, mark})
}
func (s *threadCollectorState) finalizeAnnotations(result *ThreadAnalysis) {
	result.DroppedAnnotations = s.droppedAnnotations
	for _, raw := range s.annotations {
		mark := raw.value
		mark.Scope = s.identities[raw.log]
		if mark.Scope == "" {
			mark.Scope = fmt.Sprintf("log:%d", raw.log)
		}
		result.Annotations = append(result.Annotations, mark)
	}
}
