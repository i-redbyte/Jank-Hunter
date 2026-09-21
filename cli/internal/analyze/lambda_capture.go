package analyze

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	lambdaCaptureMaxRecords   = 200_000
	lambdaCaptureMaxFileBytes = 256 << 20
	lambdaCaptureMaxLineBytes = 8 << 20
	lambdaCaptureMaxPerRecord = 4_096
	lambdaCaptureMaxArguments = 255
	lambdaCaptureMaxFields    = 4_096
	lambdaCaptureMaxSinks     = 16
	lambdaCaptureMaxTextBytes = 4_096
)

type LambdaCaptureCatalog struct {
	sourceIdentity artifactSourceIdentity
	Available      bool
	Source         string
	Captures       []LambdaCapture
}

type LambdaCapture struct {
	CallsiteID          string                `json:"callsiteId"`
	Owner               string                `json:"owner"`
	Implementation      string                `json:"implementation"`
	FunctionalInterface string                `json:"functionalInterface"`
	Representation      string                `json:"representation"`
	Values              []LambdaCapturedValue `json:"values"`
	Sinks               []string              `json:"sinks"`
	Line                int                   `json:"line,omitempty"`
	Desugared           bool                  `json:"desugared,omitempty"`
	WeakDereference     string                `json:"weakDereference,omitempty"`
	Suppressed          bool                  `json:"suppressed,omitempty"`
	SuppressionReason   string                `json:"suppressionReason,omitempty"`
}

type LambdaCapturedValue struct {
	Type     string `json:"type"`
	Role     string `json:"role"`
	Strength string `json:"strength"`
}

type LambdaCaptureAnalysis struct {
	Available      bool   `json:"available"`
	Source         string `json:"source,omitempty"`
	Total          int    `json:"total"`
	Strong         int    `json:"strong"`
	Weak           int    `json:"weak"`
	Suppressed     int    `json:"suppressed"`
	RiskyCallsites int    `json:"risky_callsites"`
	Desugared      int    `json:"desugared"`
}

type lambdaCaptureRecord struct {
	Format   int             `json:"format"`
	Class    string          `json:"class"`
	Captures []LambdaCapture `json:"captures"`
}

func LoadLambdaCaptureCatalog(path string) (*LambdaCaptureCatalog, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	digest := sha256.New()
	input, err := openBoundedTextInput(
		path,
		"lambda capture catalog",
		lambdaCaptureMaxFileBytes,
		64*1024,
		lambdaCaptureMaxLineBytes,
		digest,
	)
	if err != nil {
		return nil, err
	}
	defer input.Close()

	catalog := &LambdaCaptureCatalog{Available: true, Source: path}
	seen := make(map[string]struct{})
	scanner := input.Scanner
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record lambdaCaptureRecord
		if err := decodeStrictJSON(line, &record); err != nil {
			return nil, fmt.Errorf("parse lambda capture catalog line %d: %w", lineNumber, err)
		}
		if err := validateArtifactFormat(path, "lambda capture catalog", record.Format, LambdaCaptureFormat); err != nil {
			return nil, fmt.Errorf("parse lambda capture catalog line %d: %w", lineNumber, err)
		}
		if err := validateLambdaText("class", record.Class, true); err != nil {
			return nil, fmt.Errorf("parse lambda capture catalog line %d: %w", lineNumber, err)
		}
		if len(record.Captures) == 0 || len(record.Captures) > lambdaCaptureMaxPerRecord {
			return nil, fmt.Errorf("parse lambda capture catalog line %d: captures count must be between 1 and %d", lineNumber, lambdaCaptureMaxPerRecord)
		}
		for index := range record.Captures {
			if len(catalog.Captures) >= lambdaCaptureMaxRecords {
				return nil, fmt.Errorf("%s: lambda capture catalog exceeds record limit %d", path, lambdaCaptureMaxRecords)
			}
			capture := record.Captures[index]
			if err := validateLambdaCapture(capture); err != nil {
				return nil, fmt.Errorf("parse lambda capture catalog line %d capture %d: %w", lineNumber, index, err)
			}
			if _, duplicate := seen[capture.CallsiteID]; duplicate {
				return nil, fmt.Errorf("parse lambda capture catalog line %d: duplicate callsiteId %q", lineNumber, capture.CallsiteID)
			}
			seen[capture.CallsiteID] = struct{}{}
			capture.Sinks = sortUniqueStringsInPlace(capture.Sinks)
			catalog.Captures = append(catalog.Captures, capture)
		}
	}
	if err := input.Err(); err != nil {
		return nil, err
	}
	sort.Slice(catalog.Captures, func(i, j int) bool {
		return catalog.Captures[i].CallsiteID < catalog.Captures[j].CallsiteID
	})
	catalog.sourceIdentity = artifactSourceIdentity{path: path, digest: hex.EncodeToString(digest.Sum(nil))}
	return catalog, nil
}

func validateLambdaCapture(capture LambdaCapture) error {
	if err := validateLambdaText("callsiteId", capture.CallsiteID, true); err != nil {
		return err
	}
	if err := validateLambdaText("owner", capture.Owner, true); err != nil {
		return err
	}
	if err := validateLambdaText("implementation", capture.Implementation, true); err != nil {
		return err
	}
	if err := validateLambdaText("functionalInterface", capture.FunctionalInterface, true); err != nil {
		return err
	}
	if err := validateLambdaCallsiteID(capture.CallsiteID); err != nil {
		return err
	}
	if capture.Line < 0 {
		return fmt.Errorf("line must not be negative")
	}
	if capture.Representation != "invokedynamic" && capture.Representation != "class" && capture.Representation != "coroutine" {
		return fmt.Errorf("unsupported representation %q", capture.Representation)
	}
	if capture.WeakDereference != "" && capture.WeakDereference != "force_unwrap" {
		return fmt.Errorf("unsupported weakDereference %q", capture.WeakDereference)
	}
	maxValues := lambdaCaptureMaxArguments
	if capture.Representation == "class" || capture.Representation == "coroutine" {
		maxValues = lambdaCaptureMaxFields
	}
	if len(capture.Values) == 0 || len(capture.Values) > maxValues {
		return fmt.Errorf("values count must be between 1 and %d", maxValues)
	}
	for index, value := range capture.Values {
		if err := validateLambdaText(fmt.Sprintf("values[%d].type", index), value.Type, true); err != nil {
			return err
		}
		if err := validateLambdaText(fmt.Sprintf("values[%d].role", index), value.Role, true); err != nil {
			return err
		}
		if value.Strength != "strong" && value.Strength != "weak" && value.Strength != "value" {
			return fmt.Errorf("values[%d].strength is unsupported: %q", index, value.Strength)
		}
	}
	if len(capture.Sinks) > lambdaCaptureMaxSinks {
		return fmt.Errorf("sinks exceed limit %d", lambdaCaptureMaxSinks)
	}
	for index, sink := range capture.Sinks {
		if !isKnownLambdaSink(sink) {
			return fmt.Errorf("sinks[%d] is unsupported: %q", index, sink)
		}
	}
	if capture.Suppressed {
		if err := validateLambdaText("suppressionReason", capture.SuppressionReason, true); err != nil {
			return err
		}
	} else if capture.SuppressionReason != "" {
		return fmt.Errorf("suppressionReason requires suppressed=true")
	}
	return nil
}

func validateLambdaCallsiteID(value string) error {
	const prefix = "stable:0x"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+16 {
		return fmt.Errorf("callsiteId must be stable:0x followed by 16 lowercase hexadecimal digits")
	}
	hexValue := value[len(prefix):]
	id, err := strconv.ParseUint(hexValue, 16, 64)
	if err != nil || id == 0 || fmt.Sprintf("%016x", id) != hexValue {
		return fmt.Errorf("callsiteId must be a non-zero canonical stable ID")
	}
	return nil
}

func validateLambdaText(field, value string, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > lambdaCaptureMaxTextBytes {
		return fmt.Errorf("%s exceeds %d bytes", field, lambdaCaptureMaxTextBytes)
	}
	return nil
}

func BuildLambdaCaptureAnalysis(catalog *LambdaCaptureCatalog) *LambdaCaptureAnalysis {
	if catalog == nil || !catalog.Available {
		return nil
	}
	analysis := &LambdaCaptureAnalysis{Available: true, Source: catalog.Source, Total: len(catalog.Captures)}
	for _, capture := range catalog.Captures {
		if capture.Suppressed {
			analysis.Suppressed++
		}
		if capture.Desugared {
			analysis.Desugared++
		}
		for _, value := range capture.Values {
			switch value.Strength {
			case "strong":
				analysis.Strong++
			case "weak":
				analysis.Weak++
			}
		}
		if lambdaCaptureHasRisk(capture) {
			analysis.RiskyCallsites++
		}
	}
	return analysis
}

func sortUniqueStringsInPlace(values []string) []string {
	if len(values) < 2 {
		return values
	}
	sort.Strings(values)
	writeIndex := 1
	for readIndex := 1; readIndex < len(values); readIndex++ {
		if values[readIndex] == values[writeIndex-1] {
			continue
		}
		values[writeIndex] = values[readIndex]
		writeIndex++
	}
	return values[:writeIndex]
}

func isKnownLambdaSink(sink string) bool {
	switch sink {
	case "compose.remember", "compose.derived_state", "compose.provider",
		"flow.operator", "flow.callback", "flow.global_scope", "flow.lifecycle_scope",
		"flow.repeat_on_lifecycle", "flow.lifecycle_launch", "coroutine.builder",
		"coroutine.global_scope", "handler.queue", "executor.queue",
		"listener.registration", "static.field", "instance.field":
		return true
	default:
		return false
	}
}
