package analyze

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const ComparisonSnapshotSchema = "jankhunter.comparison-snapshot/v1"
const ComparisonSnapshotDocumentSchema = "jankhunter.report-comparison-snapshot/v1"
const ComparisonSnapshotInspect = "inspect"
const ComparisonSnapshotCompare = "compare"
const comparisonSnapshotEncoding = "gzip-base64"
const comparisonSnapshotSummaryMaxBytes = 32 << 20

type ComparisonSnapshotDocument struct {
	Schema    string               `json:"schema"`
	Kind      string               `json:"kind"`
	Snapshots []ComparisonSnapshot `json:"snapshots"`
}

type ComparisonSnapshot struct {
	Schema       string                       `json:"schema"`
	Hash         string                       `json:"sha256"`
	Provenance   ComparisonSnapshotProvenance `json:"provenance"`
	Capabilities []string                     `json:"capabilities"`
	Runs         []ComparisonSnapshotRun      `json:"runs"`
	Encoding     string                       `json:"encoding"`
	Payload      string                       `json:"summary"`
}

type ComparisonSnapshotProvenance struct {
	Generator            string `json:"generator"`
	GeneratedAt          string `json:"generated_at"`
	ComparisonSchema     string `json:"comparison_schema"`
	ProblemSchemaVersion string `json:"problem_schema_version,omitempty"`
}

type ComparisonSnapshotRun struct {
	Logs              int    `json:"logs"`
	Events            int    `json:"events"`
	DurationMS        uint64 `json:"duration_ms"`
	IndependentGroups int    `json:"independent_groups"`
	IdentityComplete  bool   `json:"identity_complete"`
}

type comparisonSnapshotHashInput struct {
	Schema       string                       `json:"schema"`
	Provenance   ComparisonSnapshotProvenance `json:"provenance"`
	Capabilities []string                     `json:"capabilities"`
	Runs         []ComparisonSnapshotRun      `json:"runs"`
	Encoding     string                       `json:"encoding"`
	Payload      string                       `json:"summary"`
}

func NewComparisonSnapshot(summary Summary, generatedAt string) (ComparisonSnapshot, error) {
	if _, err := time.Parse(time.RFC3339, generatedAt); err != nil {
		return ComparisonSnapshot{}, fmt.Errorf("comparison snapshot generated_at: %w", err)
	}
	payload, err := safeComparisonSummaryJSON(summary)
	if err != nil {
		return ComparisonSnapshot{}, err
	}
	compressed, err := compressComparisonSnapshot(payload)
	if err != nil {
		return ComparisonSnapshot{}, err
	}
	acquisition := AcquisitionEvidenceFor(summary)
	snapshot := ComparisonSnapshot{
		Schema: ComparisonSnapshotSchema,
		Provenance: ComparisonSnapshotProvenance{
			Generator:            "jankhunter-cli",
			GeneratedAt:          generatedAt,
			ComparisonSchema:     ComparisonSchemaVersion,
			ProblemSchemaVersion: summary.ProblemSchemaVersion,
		},
		Capabilities: comparisonSnapshotCapabilities(summary),
		Runs: []ComparisonSnapshotRun{{
			Logs: summary.LogCount, Events: summary.EventCount, DurationMS: summary.DurationMS,
			IndependentGroups: acquisition.IndependentGroups, IdentityComplete: acquisition.IdentityComplete,
		}},
		Encoding: comparisonSnapshotEncoding,
		Payload:  compressed,
	}
	hash, err := comparisonSnapshotHash(snapshot)
	if err != nil {
		return ComparisonSnapshot{}, err
	}
	snapshot.Hash = hash
	return snapshot, nil
}

func ValidateComparisonSnapshot(snapshot ComparisonSnapshot) (Summary, error) {
	if snapshot.Schema != ComparisonSnapshotSchema {
		return Summary{}, fmt.Errorf("unsupported comparison snapshot schema %q", snapshot.Schema)
	}
	if snapshot.Provenance.Generator != "jankhunter-cli" || snapshot.Provenance.ComparisonSchema != ComparisonSchemaVersion {
		return Summary{}, fmt.Errorf("unsupported comparison snapshot provenance")
	}
	if _, err := time.Parse(time.RFC3339, snapshot.Provenance.GeneratedAt); err != nil {
		return Summary{}, fmt.Errorf("invalid comparison snapshot generated_at: %w", err)
	}
	if len(snapshot.Runs) != 1 || !sortedUniqueNonEmpty(snapshot.Capabilities) {
		return Summary{}, fmt.Errorf("invalid comparison snapshot capabilities or run summary")
	}
	if snapshot.Encoding != comparisonSnapshotEncoding {
		return Summary{}, fmt.Errorf("unsupported comparison snapshot encoding %q", snapshot.Encoding)
	}
	expected, err := comparisonSnapshotHash(snapshot)
	if err != nil {
		return Summary{}, err
	}
	provided, decodeErr := hex.DecodeString(snapshot.Hash)
	want, wantErr := hex.DecodeString(expected)
	if decodeErr != nil || wantErr != nil || len(provided) != len(want) || subtle.ConstantTimeCompare(provided, want) != 1 {
		return Summary{}, fmt.Errorf("comparison snapshot hash mismatch")
	}
	payload, err := decompressComparisonSnapshot(snapshot.Payload)
	if err != nil {
		return Summary{}, err
	}
	var summary Summary
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&summary); err != nil {
		return Summary{}, fmt.Errorf("decode comparison snapshot summary: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Summary{}, fmt.Errorf("comparison snapshot summary contains trailing data")
	}
	return summary, nil
}

func ValidateComparisonSnapshotDocument(document ComparisonSnapshotDocument) ([]Summary, error) {
	if document.Schema != ComparisonSnapshotDocumentSchema {
		return nil, fmt.Errorf("unsupported report comparison snapshot schema %q", document.Schema)
	}
	want := 0
	switch document.Kind {
	case ComparisonSnapshotInspect:
		want = 1
	case ComparisonSnapshotCompare:
		want = 2
	default:
		return nil, fmt.Errorf("unsupported report comparison snapshot kind %q", document.Kind)
	}
	if len(document.Snapshots) != want {
		return nil, fmt.Errorf("report comparison snapshot kind %q needs %d input snapshots", document.Kind, want)
	}
	result := make([]Summary, len(document.Snapshots))
	for index, snapshot := range document.Snapshots {
		summary, err := ValidateComparisonSnapshot(snapshot)
		if err != nil {
			return nil, fmt.Errorf("validate report comparison snapshot input %d: %w", index, err)
		}
		result[index] = summary
	}
	return result, nil
}

func comparisonSnapshotCapabilities(summary Summary) []string {
	result := []string{"aggregate-metrics", "environment", "quality", "run-summary"}
	if summary.OperationAnalysis != nil {
		result = append(result, "operation-profiles")
	}
	if len(summary.Problems) > 0 || len(summary.Detectors) > 0 {
		result = append(result, "problem-findings")
	}
	if summary.DatabaseAnalysis != nil {
		result = append(result, "database-aggregates")
	}
	if summary.AndroidComponents != nil {
		result = append(result, "android-components")
	}
	sort.Strings(result)
	return result
}

func comparisonSnapshotHash(snapshot ComparisonSnapshot) (string, error) {
	payload, err := json.Marshal(comparisonSnapshotHashInput{
		Schema: snapshot.Schema, Provenance: snapshot.Provenance, Capabilities: snapshot.Capabilities,
		Runs: snapshot.Runs, Encoding: snapshot.Encoding, Payload: snapshot.Payload,
	})
	if err != nil {
		return "", fmt.Errorf("encode comparison snapshot hash input: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func compressComparisonSnapshot(payload []byte) (string, error) {
	if len(payload) > comparisonSnapshotSummaryMaxBytes {
		return "", fmt.Errorf("comparison snapshot summary exceeds %d bytes", comparisonSnapshotSummaryMaxBytes)
	}
	var compressed bytes.Buffer
	encoded := base64.NewEncoder(base64.StdEncoding, &compressed)
	writer, err := gzip.NewWriterLevel(encoded, gzip.DefaultCompression)
	if err != nil {
		_ = encoded.Close()
		return "", fmt.Errorf("create comparison snapshot compressor: %w", err)
	}
	_, writeErr := writer.Write(payload)
	closeErr := writer.Close()
	encodeErr := encoded.Close()
	if writeErr != nil || closeErr != nil || encodeErr != nil {
		return "", fmt.Errorf("compress comparison snapshot summary: write=%v close=%v encode=%v", writeErr, closeErr, encodeErr)
	}
	return compressed.String(), nil
}

func decompressComparisonSnapshot(payload string) ([]byte, error) {
	decoded := base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload))
	reader, err := gzip.NewReader(decoded)
	if err != nil {
		return nil, fmt.Errorf("decode comparison snapshot summary: %w", err)
	}
	result, readErr := io.ReadAll(io.LimitReader(reader, comparisonSnapshotSummaryMaxBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read comparison snapshot summary: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close comparison snapshot summary: %w", closeErr)
	}
	if len(result) > comparisonSnapshotSummaryMaxBytes {
		return nil, fmt.Errorf("comparison snapshot summary exceeds %d bytes", comparisonSnapshotSummaryMaxBytes)
	}
	return result, nil
}

func safeComparisonSummaryJSON(summary Summary) (json.RawMessage, error) {
	compact := comparisonSnapshotSummary(summary)
	raw, err := json.Marshal(compact)
	if err != nil {
		return nil, fmt.Errorf("encode comparison snapshot summary: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("normalize comparison snapshot summary: %w", err)
	}
	sanitizeComparisonSnapshotValue("", value)
	result, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode safe comparison snapshot summary: %w", err)
	}
	return result, nil
}

func comparisonSnapshotSummary(summary Summary) Summary {
	result := summary
	result.HeapDiagnostics = nil
	result.RuntimeCalls = nil
	result.CodeProblems = nil
	result.MemoryLeaks = nil
	result.Influence.TopNodes = nil
	result.Influence.TopEdges = nil
	result.Influence.Views = nil
	result.Influence.Workspace = InfluenceGraphWorkspace{}
	result.Influence.HotPaths = nil
	result.Influence.MethodHotspots = nil
	result.Influence.Cycles = nil
	result.Influence.Heuristic = nil
	if summary.DatabaseAnalysis != nil {
		database := *summary.DatabaseAnalysis
		database.Statements = nil
		result.DatabaseAnalysis = &database
	}
	return result
}

func sanitizeComparisonSnapshotValue(parent string, value any) {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			if comparisonSnapshotUnsafeField(normalized) {
				delete(current, key)
				continue
			}
			if text, ok := child.(string); ok {
				current[key] = sanitizeComparisonSnapshotString(normalized, text)
				continue
			}
			sanitizeComparisonSnapshotValue(normalized, child)
		}
	case []any:
		for _, child := range current {
			sanitizeComparisonSnapshotValue(parent, child)
		}
	}
}

func comparisonSnapshotUnsafeField(key string) bool {
	switch key {
	case "artifactdirectory", "heapdiagnostics", "memoryleaks", "statements", "worstdatabasestatements",
		"query", "stackhint", "stackretrace", "path", "heapdump", "heapevidence":
		return true
	default:
		return false
	}
}

var windowsAbsolutePath = regexp.MustCompile(`(?i)(^|[[:space:]"'])[a-z]:[\\/]`)

func sanitizeComparisonSnapshotString(field, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return value
	}
	if filepath.IsAbs(trimmed) || windowsAbsolutePath.MatchString(value) ||
		strings.Contains(value, "/Users/") || strings.Contains(value, "/home/") || strings.Contains(value, "/private/") {
		return ""
	}
	upper := strings.ToUpper(trimmed)
	for _, token := range []string{"SELECT ", "INSERT ", "UPDATE ", "DELETE ", "CREATE TABLE ", "ALTER TABLE ", "DROP TABLE "} {
		if strings.Contains(upper, token) {
			return ""
		}
	}
	if separator := strings.IndexByte(value, '?'); separator >= 0 && (field == "route" || strings.Contains(value, "://")) {
		return value[:separator]
	}
	return value
}

func sortedUniqueNonEmpty(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for index, value := range values {
		if strings.TrimSpace(value) == "" || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}
