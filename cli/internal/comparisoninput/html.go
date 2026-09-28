package comparisoninput

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

const MaxHTMLInputBytes int64 = 256 << 20
const MaxSnapshotBytes = 32 << 20
const MaxSnapshotJSONDepth = 32

const comparisonSnapshotID = "jankhunter-comparison-snapshot"

func ReadComparisonSnapshot(path string) (analyze.Summary, error) {
	document, summaries, err := ReadComparisonSnapshotDocument(path)
	if err != nil {
		return analyze.Summary{}, err
	}
	if document.Kind == analyze.ComparisonSnapshotCompare {
		return analyze.Summary{}, fmt.Errorf("compare report contains two input snapshots and is ambiguous; use an inspect report")
	}
	return summaries[0], nil
}

func ReadComparisonSnapshotDocument(path string) (analyze.ComparisonSnapshotDocument, []analyze.Summary, error) {
	file, err := os.Open(path)
	if err != nil {
		return analyze.ComparisonSnapshotDocument{}, nil, fmt.Errorf("open HTML report: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return analyze.ComparisonSnapshotDocument{}, nil, fmt.Errorf("stat HTML report: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return analyze.ComparisonSnapshotDocument{}, nil, fmt.Errorf("HTML input is not a regular file")
	}
	if info.Size() > MaxHTMLInputBytes {
		_ = file.Close()
		return analyze.ComparisonSnapshotDocument{}, nil, fmt.Errorf("HTML input exceeds %d bytes", MaxHTMLInputBytes)
	}
	payload, readErr := io.ReadAll(io.LimitReader(file, MaxHTMLInputBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return analyze.ComparisonSnapshotDocument{}, nil, fmt.Errorf("read HTML report: %w", readErr)
	}
	if closeErr != nil {
		return analyze.ComparisonSnapshotDocument{}, nil, fmt.Errorf("close HTML report: %w", closeErr)
	}
	if int64(len(payload)) > MaxHTMLInputBytes {
		return analyze.ComparisonSnapshotDocument{}, nil, fmt.Errorf("HTML input exceeds %d bytes", MaxHTMLInputBytes)
	}
	document, err := parseComparisonSnapshotHTML(payload)
	if err != nil {
		return analyze.ComparisonSnapshotDocument{}, nil, err
	}
	summaries, err := analyze.ValidateComparisonSnapshotDocument(document)
	if err != nil {
		return analyze.ComparisonSnapshotDocument{}, nil, err
	}
	return document, summaries, nil
}

func parseComparisonSnapshotHTML(input []byte) (analyze.ComparisonSnapshotDocument, error) {
	if int64(len(input)) > MaxHTMLInputBytes {
		return analyze.ComparisonSnapshotDocument{}, fmt.Errorf("HTML input exceeds %d bytes", MaxHTMLInputBytes)
	}
	found := false
	var result analyze.ComparisonSnapshotDocument
	for offset := 0; offset < len(input); {
		relative := asciiFoldIndex(input[offset:], []byte("<script"))
		if relative < 0 {
			break
		}
		start := offset + relative
		nameEnd := start + len("<script")
		if nameEnd < len(input) && !htmlSpace(input[nameEnd]) && input[nameEnd] != '>' {
			offset = nameEnd
			continue
		}
		tagEnd := htmlTagEnd(input, nameEnd)
		if tagEnd < 0 {
			return result, fmt.Errorf("unterminated script start tag")
		}
		id, scriptType, err := scriptIdentity(input[nameEnd:tagEnd])
		if err != nil {
			return result, err
		}
		bodyStart := tagEnd + 1
		closeRelative := asciiFoldIndex(input[bodyStart:], []byte("</script"))
		if closeRelative < 0 {
			return result, fmt.Errorf("unterminated script element")
		}
		bodyEnd := bodyStart + closeRelative
		closeEnd := htmlTagEnd(input, bodyEnd+len("</script"))
		if closeEnd < 0 {
			return result, fmt.Errorf("unterminated script closing tag")
		}
		if id == comparisonSnapshotID {
			if found {
				return result, fmt.Errorf("HTML report contains more than one comparison snapshot")
			}
			found = true
			if !strings.EqualFold(scriptType, "application/json") {
				return result, fmt.Errorf("comparison snapshot script type must be application/json")
			}
			body := bytes.TrimSpace(input[bodyStart:bodyEnd])
			if len(body) > MaxSnapshotBytes {
				return result, fmt.Errorf("comparison snapshot exceeds %d bytes", MaxSnapshotBytes)
			}
			if err := validateJSONDepth(body, MaxSnapshotJSONDepth); err != nil {
				return result, err
			}
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&result); err != nil {
				return result, fmt.Errorf("decode comparison snapshot: %w", err)
			}
			var trailing any
			if err := decoder.Decode(&trailing); err != io.EOF {
				return result, fmt.Errorf("comparison snapshot contains trailing JSON")
			}
		}
		offset = closeEnd + 1
	}
	if !found {
		return result, fmt.Errorf("HTML report does not contain a comparison snapshot; regenerate it with a current Jank Hunter CLI")
	}
	return result, nil
}

func scriptIdentity(attributes []byte) (string, string, error) {
	var id, scriptType string
	seenID, seenType := false, false
	for offset := 0; offset < len(attributes); {
		for offset < len(attributes) && htmlSpace(attributes[offset]) {
			offset++
		}
		if offset == len(attributes) || attributes[offset] == '/' {
			break
		}
		nameStart := offset
		for offset < len(attributes) && !htmlSpace(attributes[offset]) && attributes[offset] != '=' && attributes[offset] != '/' {
			offset++
		}
		if nameStart == offset {
			return "", "", fmt.Errorf("invalid script attribute")
		}
		name := strings.ToLower(string(attributes[nameStart:offset]))
		for offset < len(attributes) && htmlSpace(attributes[offset]) {
			offset++
		}
		value := ""
		if offset < len(attributes) && attributes[offset] == '=' {
			offset++
			for offset < len(attributes) && htmlSpace(attributes[offset]) {
				offset++
			}
			if offset >= len(attributes) {
				return "", "", fmt.Errorf("script attribute %q has no value", name)
			}
			quote := attributes[offset]
			if quote == '\'' || quote == '"' {
				offset++
				valueStart := offset
				for offset < len(attributes) && attributes[offset] != quote {
					offset++
				}
				if offset >= len(attributes) {
					return "", "", fmt.Errorf("unterminated script attribute %q", name)
				}
				value = string(attributes[valueStart:offset])
				offset++
			} else {
				valueStart := offset
				for offset < len(attributes) && !htmlSpace(attributes[offset]) && attributes[offset] != '/' {
					offset++
				}
				value = string(attributes[valueStart:offset])
			}
		}
		switch name {
		case "id":
			if seenID {
				return "", "", fmt.Errorf("script has duplicate id attribute")
			}
			id, seenID = value, true
		case "type":
			if seenType {
				return "", "", fmt.Errorf("script has duplicate type attribute")
			}
			scriptType, seenType = value, true
		}
	}
	return id, scriptType, nil
}

func validateJSONDepth(payload []byte, limit int) error {
	depth := 0
	inString, escaped := false, false
	for _, value := range payload {
		if inString {
			if escaped {
				escaped = false
			} else if value == '\\' {
				escaped = true
			} else if value == '"' {
				inString = false
			}
			continue
		}
		switch value {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > limit {
				return fmt.Errorf("comparison snapshot JSON depth exceeds %d", limit)
			}
		case '}', ']':
			depth--
			if depth < 0 {
				return fmt.Errorf("comparison snapshot JSON has unbalanced containers")
			}
		}
	}
	if inString || depth != 0 {
		return fmt.Errorf("comparison snapshot JSON is incomplete")
	}
	return nil
}

func htmlTagEnd(input []byte, offset int) int {
	var quote byte
	for ; offset < len(input); offset++ {
		value := input[offset]
		if quote != 0 {
			if value == quote {
				quote = 0
			}
			continue
		}
		if value == '\'' || value == '"' {
			quote = value
		} else if value == '>' {
			return offset
		}
	}
	return -1
}

func asciiFoldIndex(input, target []byte) int {
	if len(target) == 0 {
		return 0
	}
	for start := 0; start+len(target) <= len(input); start++ {
		matched := true
		for index, value := range target {
			left := input[start+index]
			if left >= 'A' && left <= 'Z' {
				left += 'a' - 'A'
			}
			if value >= 'A' && value <= 'Z' {
				value += 'a' - 'A'
			}
			if left != value {
				matched = false
				break
			}
		}
		if matched {
			return start
		}
	}
	return -1
}

func htmlSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
}
