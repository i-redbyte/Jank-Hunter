package jhlog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
)

type SizeProfile struct {
	Files    []SizeProfileFile `json:"files"`
	Types    []SizeProfileType `json:"types"`
	Warnings []string          `json:"warnings,omitempty"`
}

type SizeProfileFile struct {
	Path            string        `json:"path"`
	Format          string        `json:"format"`
	Status          SegmentStatus `json:"status"`
	Sealed          bool          `json:"sealed"`
	TailBytes       uint64        `json:"tail_bytes,omitempty"`
	CommittedChunks uint32        `json:"committed_chunks,omitempty"`
	FileBytes       uint64        `json:"file_bytes"`
	BodyBytes       uint64        `json:"body_bytes"`
	StoredBytes     uint64        `json:"stored_bytes,omitempty"`
	Records         uint64        `json:"records"`
	Events          uint64        `json:"events"`
	DataRecords     uint64        `json:"data_records"`
	Dictionary      uint64        `json:"dictionary_records"`
	Control         uint64        `json:"control_records"`
	Warnings        []string      `json:"warnings,omitempty"`
	CompressionRate float64       `json:"compression_rate,omitempty"`
}

type SizeProfileType struct {
	Type     EventType `json:"type"`
	Name     string    `json:"name"`
	Events   uint64    `json:"events"`
	Bytes    uint64    `json:"bytes"`
	AvgBytes float64   `json:"avg_bytes"`
	Percent  float64   `json:"percent"`
	Files    uint64    `json:"files"`
}

func ProfileFiles(paths []string) (SizeProfile, error) {
	profile := SizeProfile{}
	byType := map[EventType]*SizeProfileType{}
	seenInFile := map[EventType]struct{}{}
	var totalBody uint64
	for _, path := range paths {
		fileProfile, typeBytes, typeEvents, err := ProfileFile(path)
		if err != nil {
			return SizeProfile{}, err
		}
		profile.Files = append(profile.Files, fileProfile)
		profile.Warnings = append(profile.Warnings, fileProfile.Warnings...)
		totalBody += fileProfile.BodyBytes
		clear(seenInFile)
		for eventType, byteCount := range typeBytes {
			row := byType[eventType]
			if row == nil {
				row = &SizeProfileType{Type: eventType, Name: EventTypeName(eventType)}
				byType[eventType] = row
			}
			row.Bytes += byteCount
			row.Events += typeEvents[eventType]
			if _, seen := seenInFile[eventType]; !seen {
				row.Files++
				seenInFile[eventType] = struct{}{}
			}
		}
	}

	profile.Types = make([]SizeProfileType, 0, len(byType))
	for _, row := range byType {
		if row.Events > 0 {
			row.AvgBytes = roundFloat(float64(row.Bytes) / float64(row.Events))
		}
		if totalBody > 0 {
			row.Percent = roundFloat(float64(row.Bytes) / float64(totalBody) * 100)
		}
		profile.Types = append(profile.Types, *row)
	}
	sort.Slice(profile.Types, func(i, j int) bool {
		if profile.Types[i].Bytes == profile.Types[j].Bytes {
			return profile.Types[i].Name < profile.Types[j].Name
		}
		return profile.Types[i].Bytes > profile.Types[j].Bytes
	})
	return profile, nil
}

func ProfileFile(path string) (SizeProfileFile, map[EventType]uint64, map[EventType]uint64, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return SizeProfileFile{}, nil, nil, err
	}
	format, err := detectProfileFormat(path)
	if err != nil {
		return SizeProfileFile{}, nil, nil, err
	}
	result, err := StreamFileWithResult(path, nil)
	if err != nil {
		return SizeProfileFile{}, nil, nil, err
	}
	fileProfile := SizeProfileFile{
		Path:            path,
		Format:          format,
		Status:          result.Status,
		Sealed:          result.Sealed,
		TailBytes:       result.TailBytes,
		CommittedChunks: result.CommittedChunks,
		FileBytes:       uint64(stat.Size()),
		BodyBytes:       result.RawRecordBytes,
		StoredBytes:     result.StoredChunkBytes,
		Records:         result.TotalRecords,
		Events:          result.Events,
		DataRecords:     result.DataRecords,
		Dictionary:      result.DictionaryRecords,
		Control:         result.ControlRecords,
		Warnings:        append([]string(nil), result.Warnings...),
	}
	fileProfile.CompressionRate = compressionRate(fileProfile.BodyBytes, fileProfile.FileBytes)
	return fileProfile, result.RecordBytesByType, result.RecordsByType, nil
}

func detectProfileFormat(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var prefix [magicSize]byte
	n, err := io.ReadFull(file, prefix[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	if n == len(Magic) && bytes.Equal(prefix[:7], Magic[:7]) {
		return "binary-v9-chunked", nil
	}
	return "jsonl", nil
}

func EventTypeName(eventType EventType) string {
	switch eventType {
	case EventDictionary:
		return "dictionary"
	case EventSession:
		return "session"
	case EventContext:
		return "context"
	case EventHTTP:
		return "http"
	case EventUIWindow:
		return "ui_window"
	case EventStall:
		return "stall"
	case EventMemory:
		return "memory"
	case EventRetained:
		return "retained"
	case EventCounter:
		return "counter"
	case EventGauge:
		return "gauge"
	case EventFlow:
		return "flow_transition"
	case EventLogSpam:
		return "log_spam"
	case EventProblem:
		return "problem"
	case EventRuntimeCall:
		return "runtime_call"
	case EventQualitySnapshot:
		return "quality_snapshot"
	case EventSegmentEnd:
		return "segment_end"
	default:
		return fmt.Sprintf("event_%d", eventType)
	}
}

func compressionRate(bodyBytes, fileBytes uint64) float64 {
	if fileBytes == 0 {
		return 0
	}
	return roundFloat(float64(bodyBytes) / float64(fileBytes))
}

func roundFloat(value float64) float64 {
	return math.Round(value*10) / 10
}
