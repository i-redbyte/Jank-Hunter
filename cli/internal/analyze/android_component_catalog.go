package analyze

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	androidComponentCatalogMaxRecords      = 100_000
	androidComponentCatalogMaxLineBytes    = 1 << 20
	androidComponentCatalogMaxEntryPoints  = 128
	androidComponentCatalogMaxTransactions = 4_096
	androidComponentCatalogMaxTextBytes    = 4_096
)

type AndroidComponentCatalog struct {
	Available  bool
	Source     string
	Components []AndroidComponentCatalogEntry
	byID       map[uint64]int
	aidl       map[androidAIDLTransactionKey]string
}

type AndroidComponentCatalogEntry struct {
	ClassName               string
	ComponentID             uint64
	Kind                    string
	Abstract                bool
	Coverage                string
	EntryPoints             []string
	InstrumentedEntryPoints []string
	UncoveredEntryPoints    []string
	AIDLDescriptor          string
	Transactions            []AndroidAIDLTransaction
}

type AndroidAIDLTransaction struct {
	Code   uint32
	Method string
}

type androidAIDLTransactionKey struct {
	descriptor string
	code       uint32
}

type androidComponentCatalogRecord struct {
	Format                  int                     `json:"format"`
	ClassName               string                  `json:"class"`
	ComponentID             string                  `json:"componentId"`
	Kind                    string                  `json:"kind"`
	Abstract                bool                    `json:"abstract"`
	Coverage                string                  `json:"coverage"`
	EntryPoints             []string                `json:"entryPoints"`
	InstrumentedEntryPoints []string                `json:"instrumentedEntryPoints"`
	UncoveredEntryPoints    []string                `json:"uncoveredEntryPoints"`
	AIDLDescriptor          string                  `json:"aidlDescriptor"`
	Transactions            []androidTransactionRaw `json:"transactions"`
}

type androidTransactionRaw struct {
	Code   uint64 `json:"code"`
	Method string `json:"method"`
}

func LoadAndroidComponentCatalog(path string) (*AndroidComponentCatalog, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	catalog := &AndroidComponentCatalog{
		Available: true,
		Source:    path,
		byID:      make(map[uint64]int),
		aidl:      make(map[androidAIDLTransactionKey]string),
	}
	classes := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), androidComponentCatalogMaxLineBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if len(catalog.Components) >= androidComponentCatalogMaxRecords {
			return nil, fmt.Errorf("%s: Android component catalog exceeds record limit %d", path, androidComponentCatalogMaxRecords)
		}
		var record androidComponentCatalogRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("parse Android component catalog line %d: %w", lineNumber, err)
		}
		entry, err := androidComponentEntry(path, record)
		if err != nil {
			return nil, fmt.Errorf("parse Android component catalog line %d: %w", lineNumber, err)
		}
		if _, duplicate := classes[entry.ClassName]; duplicate {
			return nil, fmt.Errorf("parse Android component catalog line %d: duplicate class %q", lineNumber, entry.ClassName)
		}
		if _, duplicate := catalog.byID[entry.ComponentID]; duplicate {
			return nil, fmt.Errorf("parse Android component catalog line %d: duplicate componentId %s", lineNumber, record.ComponentID)
		}
		classes[entry.ClassName] = struct{}{}
		catalog.byID[entry.ComponentID] = len(catalog.Components)
		catalog.Components = append(catalog.Components, entry)
		for _, transaction := range entry.Transactions {
			key := androidAIDLTransactionKey{descriptor: entry.AIDLDescriptor, code: transaction.Code}
			if key.descriptor == "" {
				continue
			}
			if previous, exists := catalog.aidl[key]; exists && previous != transaction.Method {
				return nil, fmt.Errorf(
					"parse Android component catalog line %d: conflicting AIDL transaction %s/%d: %q and %q",
					lineNumber, key.descriptor, key.code, previous, transaction.Method,
				)
			}
			catalog.aidl[key] = transaction.Method
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(catalog.Components, func(i, j int) bool {
		return catalog.Components[i].ClassName < catalog.Components[j].ClassName
	})
	clear(catalog.byID)
	for index := range catalog.Components {
		catalog.byID[catalog.Components[index].ComponentID] = index
	}
	return catalog, nil
}

func (c *AndroidComponentCatalog) Component(id uint64) (AndroidComponentCatalogEntry, bool) {
	if c == nil {
		return AndroidComponentCatalogEntry{}, false
	}
	index, ok := c.byID[id]
	if !ok {
		return AndroidComponentCatalogEntry{}, false
	}
	return c.Components[index], true
}

func (c *AndroidComponentCatalog) AIDLMethod(descriptor string, code uint32) (string, bool) {
	if c == nil {
		return "", false
	}
	method, ok := c.aidl[androidAIDLTransactionKey{descriptor: descriptor, code: code}]
	return method, ok
}

func androidComponentEntry(path string, record androidComponentCatalogRecord) (AndroidComponentCatalogEntry, error) {
	if err := validateArtifactFormat(path, "Android component catalog", record.Format, AndroidComponentCatalogFormat); err != nil {
		return AndroidComponentCatalogEntry{}, err
	}
	if err := validateAndroidCatalogText("class", record.ClassName, true); err != nil {
		return AndroidComponentCatalogEntry{}, err
	}
	componentID, err := parseAndroidComponentID(record.ComponentID)
	if err != nil {
		return AndroidComponentCatalogEntry{}, err
	}
	if !validAndroidComponentKind(record.Kind) {
		return AndroidComponentCatalogEntry{}, fmt.Errorf("unsupported kind %q", record.Kind)
	}
	if !validAndroidComponentCoverage(record.Coverage) {
		return AndroidComponentCatalogEntry{}, fmt.Errorf("unsupported coverage %q", record.Coverage)
	}
	if err := validateAndroidCatalogText("aidlDescriptor", record.AIDLDescriptor, false); err != nil {
		return AndroidComponentCatalogEntry{}, err
	}
	if len(record.EntryPoints) > androidComponentCatalogMaxEntryPoints ||
		len(record.InstrumentedEntryPoints) > androidComponentCatalogMaxEntryPoints ||
		len(record.UncoveredEntryPoints) > androidComponentCatalogMaxEntryPoints {
		return AndroidComponentCatalogEntry{}, fmt.Errorf("entry points exceed limit %d", androidComponentCatalogMaxEntryPoints)
	}
	entryPoints, err := validatedAndroidCatalogStrings("entryPoints", record.EntryPoints)
	if err != nil {
		return AndroidComponentCatalogEntry{}, err
	}
	instrumented, err := validatedAndroidCatalogStrings("instrumentedEntryPoints", record.InstrumentedEntryPoints)
	if err != nil {
		return AndroidComponentCatalogEntry{}, err
	}
	uncovered, err := validatedAndroidCatalogStrings("uncoveredEntryPoints", record.UncoveredEntryPoints)
	if err != nil {
		return AndroidComponentCatalogEntry{}, err
	}
	if err := validateAndroidCoverage(record.Coverage, entryPoints, instrumented, uncovered); err != nil {
		return AndroidComponentCatalogEntry{}, err
	}
	if len(record.Transactions) > androidComponentCatalogMaxTransactions {
		return AndroidComponentCatalogEntry{}, fmt.Errorf("transactions exceed limit %d", androidComponentCatalogMaxTransactions)
	}
	transactions := make([]AndroidAIDLTransaction, 0, len(record.Transactions))
	seenTransactions := make(map[uint32]string, len(record.Transactions))
	for index, transaction := range record.Transactions {
		if transaction.Code > math.MaxUint32 {
			return AndroidComponentCatalogEntry{}, fmt.Errorf("transactions[%d].code exceeds uint32", index)
		}
		if err := validateAndroidCatalogText(fmt.Sprintf("transactions[%d].method", index), transaction.Method, true); err != nil {
			return AndroidComponentCatalogEntry{}, err
		}
		code := uint32(transaction.Code)
		if previous, duplicate := seenTransactions[code]; duplicate {
			return AndroidComponentCatalogEntry{}, fmt.Errorf("duplicate transaction code %d (%q and %q)", code, previous, transaction.Method)
		}
		seenTransactions[code] = transaction.Method
		transactions = append(transactions, AndroidAIDLTransaction{Code: code, Method: transaction.Method})
	}
	sort.Slice(transactions, func(i, j int) bool { return transactions[i].Code < transactions[j].Code })
	return AndroidComponentCatalogEntry{
		ClassName: record.ClassName, ComponentID: componentID, Kind: record.Kind,
		Abstract: record.Abstract, Coverage: record.Coverage,
		EntryPoints: entryPoints, InstrumentedEntryPoints: instrumented, UncoveredEntryPoints: uncovered,
		AIDLDescriptor: record.AIDLDescriptor, Transactions: transactions,
	}, nil
}

func parseAndroidComponentID(value string) (uint64, error) {
	const prefix = "stable:0x"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+16 {
		return 0, fmt.Errorf("componentId must be stable:0x followed by 16 lowercase hexadecimal digits")
	}
	hexValue := value[len(prefix):]
	id, err := strconv.ParseUint(hexValue, 16, 64)
	if err != nil || id == 0 || fmt.Sprintf("%016x", id) != hexValue {
		return 0, fmt.Errorf("componentId must be a non-zero canonical stable ID")
	}
	return id, nil
}

func validateAndroidCatalogText(field, value string, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > androidComponentCatalogMaxTextBytes {
		return fmt.Errorf("%s exceeds %d bytes", field, androidComponentCatalogMaxTextBytes)
	}
	return nil
}

func validatedAndroidCatalogStrings(field string, values []string) ([]string, error) {
	result := append([]string(nil), values...)
	seen := make(map[string]struct{}, len(result))
	for index, value := range result {
		if err := validateAndroidCatalogText(fmt.Sprintf("%s[%d]", field, index), value, true); err != nil {
			return nil, err
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, fmt.Errorf("%s contains duplicate %q", field, value)
		}
		seen[value] = struct{}{}
	}
	sort.Strings(result)
	return result, nil
}

func validateAndroidCoverage(coverage string, entries, instrumented, uncovered []string) error {
	entrySet := make(map[string]struct{}, len(entries))
	for _, value := range entries {
		entrySet[value] = struct{}{}
	}
	for _, values := range [][]string{instrumented, uncovered} {
		for _, value := range values {
			if _, exists := entrySet[value]; !exists {
				return fmt.Errorf("coverage entry %q is absent from entryPoints", value)
			}
		}
	}
	expected := "partial"
	switch {
	case len(instrumented) == 0:
		expected = "none"
	case len(uncovered) == 0:
		expected = "full"
	}
	if coverage != expected {
		return fmt.Errorf("coverage %q contradicts entry point coverage %q", coverage, expected)
	}
	if len(instrumented)+len(uncovered) != len(entries) {
		return fmt.Errorf("instrumented and uncovered entry points must partition entryPoints")
	}
	return nil
}

func validAndroidComponentKind(value string) bool {
	switch value {
	case "service", "receiver", "aidl_interface", "aidl_stub", "aidl_proxy", "binder":
		return true
	default:
		return false
	}
}

func validAndroidComponentCoverage(value string) bool {
	return value == "none" || value == "partial" || value == "full"
}
