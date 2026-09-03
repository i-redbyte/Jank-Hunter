package analyze

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func operationEventUnixMS(header jhlog.SegmentHeader, eventElapsedMS uint64) uint64 {
	segmentElapsedMS := header.SegmentStartElapsedUS / 1_000
	if eventElapsedMS <= segmentElapsedMS {
		return header.SegmentStartUnixMS
	}
	return saturatingUint64Sum(header.SegmentStartUnixMS, eventElapsedMS-segmentElapsedMS)
}

func operationRecoveredStartUnixMS(
	header jhlog.SegmentHeader,
	eventElapsedMS uint64,
	durationUS uint64,
) uint64 {
	finishedUnixMS := operationEventUnixMS(header, eventElapsedMS)
	durationMS := microsecondsToMillisecondsCeil(durationUS)
	if durationMS >= finishedUnixMS {
		return 0
	}
	return finishedUnixMS - durationMS
}

func operationSlot(unixMS uint64, offsetMin int64) uint64 {
	offsetMS, validOffset := operationTimezoneOffsetMagnitudeMS(offsetMin)
	if !validOffset {
		return unixMS - unixMS%operationHourMS
	}
	if offsetMin >= 0 {
		if unixMS > ^uint64(0)-offsetMS {
			return unixMS - unixMS%operationHourMS
		}
		localHour := unixMS + offsetMS
		localHour -= localHour % operationHourMS
		if localHour < offsetMS {
			return 0
		}
		return localHour - offsetMS
	}
	if unixMS < offsetMS {
		return 0
	}
	localHour := unixMS - offsetMS
	localHour -= localHour % operationHourMS
	return saturatingUint64Sum(localHour, offsetMS)
}

func operationSlotLabel(startUnixMS uint64, offsetMin int64) string {
	date := "время вне диапазона"
	if localMS, ok := operationLocalUnixMS(startUnixMS, offsetMin); ok {
		date = time.UnixMilli(localMS).UTC().Format("2006-01-02 15:00")
	}
	sign := "+"
	offset := offsetMin
	if offset < 0 {
		sign = "-"
		if offset == math.MinInt64 {
			return fmt.Sprintf("%s (смещение вне диапазона)", date)
		}
		offset = -offset
	}
	return fmt.Sprintf("%s (UTC%s%02d:%02d)", date, sign, offset/60, offset%60)
}

func operationTimezoneOffsetMagnitudeMS(offsetMin int64) (uint64, bool) {
	if offsetMin > math.MaxInt64/60_000 || offsetMin < math.MinInt64/60_000 {
		return 0, false
	}
	offsetMS := offsetMin * 60_000
	if offsetMS >= 0 {
		return uint64(offsetMS), true
	}
	return uint64(-(offsetMS + 1)) + 1, true
}

func operationLocalUnixMS(startUnixMS uint64, offsetMin int64) (int64, bool) {
	if startUnixMS > math.MaxInt64 ||
		offsetMin > math.MaxInt64/60_000 || offsetMin < math.MinInt64/60_000 {
		return 0, false
	}
	start := int64(startUnixMS)
	offsetMS := offsetMin * 60_000
	if offsetMS > 0 && start > math.MaxInt64-offsetMS {
		return 0, false
	}
	if offsetMS < 0 && start < math.MinInt64-offsetMS {
		return 0, false
	}
	return start + offsetMS, true
}

func operationKindName(kind jhlog.OperationKind) string {
	switch kind {
	case jhlog.OperationKindUser:
		return "user"
	case jhlog.OperationKindScreen:
		return "screen"
	case jhlog.OperationKindBackground:
		return "background"
	case jhlog.OperationKindSystem:
		return "system"
	case jhlog.OperationKindStage:
		return "stage"
	default:
		return "unknown"
	}
}

func operationKindFromName(name string) jhlog.OperationKind {
	switch name {
	case "user":
		return jhlog.OperationKindUser
	case "screen":
		return jhlog.OperationKindScreen
	case "background":
		return jhlog.OperationKindBackground
	case "system":
		return jhlog.OperationKindSystem
	case "stage":
		return jhlog.OperationKindStage
	default:
		return jhlog.OperationKindUnknown
	}
}

func operationOutcomeName(outcome jhlog.OperationOutcome) string {
	switch outcome {
	case jhlog.OperationOutcomeSuccess:
		return "success"
	case jhlog.OperationOutcomeFailure:
		return "failure"
	case jhlog.OperationOutcomeCancelled:
		return "cancelled"
	case jhlog.OperationOutcomeTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

func sortOperationAnalysis(result *OperationAnalysis) {
	sort.Slice(result.Operations, func(i, j int) bool {
		left, right := result.Operations[i], result.Operations[j]
		if left.P95MS != right.P95MS {
			return left.P95MS > right.P95MS
		}
		if left.Count != right.Count {
			return left.Count > right.Count
		}
		return operationGroupLess(
			operationGroupKey{name: left.Operation, kind: left.Kind, screen: left.Screen},
			operationGroupKey{name: right.Operation, kind: right.Kind, screen: right.Screen},
		)
	})
	sort.Slice(result.TimeSlots, func(i, j int) bool {
		left, right := result.TimeSlots[i], result.TimeSlots[j]
		if left.StartUnixMS != right.StartUnixMS {
			return left.StartUnixMS < right.StartUnixMS
		}
		return operationGroupLess(
			operationGroupKey{name: left.Operation, kind: left.Kind, screen: left.Screen},
			operationGroupKey{name: right.Operation, kind: right.Kind, screen: right.Screen},
		)
	})
	sort.Slice(result.Dimensions, func(i, j int) bool {
		left, right := result.Dimensions[i], result.Dimensions[j]
		if left.P95MS != right.P95MS {
			return left.P95MS > right.P95MS
		}
		if left.Operation != right.Operation {
			return left.Operation < right.Operation
		}
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		if left.Value != right.Value {
			return left.Value < right.Value
		}
		return left.Screen < right.Screen
	})
	sort.Slice(result.Stages, func(i, j int) bool {
		left, right := result.Stages[i], result.Stages[j]
		if left.TotalMS != right.TotalMS {
			return left.TotalMS > right.TotalMS
		}
		if left.ParentOperation != right.ParentOperation {
			return left.ParentOperation < right.ParentOperation
		}
		if left.Stage != right.Stage {
			return left.Stage < right.Stage
		}
		return left.Screen < right.Screen
	})
}

func operationGroupLess(left, right operationGroupKey) bool {
	if left.name != right.name {
		return left.name < right.name
	}
	if left.kind != right.kind {
		return left.kind < right.kind
	}
	return left.screen < right.screen
}
