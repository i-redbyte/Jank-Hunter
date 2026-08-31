package analyze

import (
	"fmt"
	"os"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const (
	databaseEvidenceFormat        = 1
	maxDatabaseEvidenceBytes      = 4 << 20
	maxDatabaseEvidenceStatements = 4_096
	maxDatabaseEvidenceTables     = 128
	maxDatabaseEvidenceIndexes    = 256
	maxDatabaseEvidenceColumns    = 32
	maxDatabaseEvidencePlanSteps  = 64
	maxDatabaseIdentifierBytes    = 128
)

// DatabaseEvidence is a developer-provided, pre-sanitized offline artifact. It deliberately has
// no raw SQL or free-form EXPLAIN text and is never used to connect to an application database.
type DatabaseEvidence struct {
	Format     int                         `json:"format"`
	Kind       string                      `json:"kind"`
	Sanitized  bool                        `json:"sanitized"`
	Statements []DatabaseStatementEvidence `json:"statements"`
}

type DatabaseStatementEvidence struct {
	StatementFingerprint uint64                 `json:"-"`
	FingerprintHex       string                 `json:"statement_fingerprint"`
	Operation            string                 `json:"operation"`
	Schema               DatabaseSchemaEvidence `json:"schema,omitempty"`
	Plan                 []DatabasePlanStep     `json:"plan,omitempty"`
}

type DatabaseSchemaEvidence struct {
	Complete bool                    `json:"complete"`
	Tables   []DatabaseTableEvidence `json:"tables,omitempty"`
}

type DatabaseTableEvidence struct {
	Name    string                  `json:"name"`
	Indexes []DatabaseIndexEvidence `json:"indexes,omitempty"`
}

type DatabaseIndexEvidence struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique,omitempty"`
	Partial bool     `json:"partial,omitempty"`
}

type DatabasePlanStep struct {
	Kind    string `json:"kind"`
	Table   string `json:"table,omitempty"`
	Index   string `json:"index,omitempty"`
	Purpose string `json:"purpose,omitempty"`
}

type DatabaseEvidenceAnalysis struct {
	LoadedStatements    uint64                `json:"loaded_statements"`
	MatchedStatements   uint64                `json:"matched_statements"`
	UnmatchedStatements uint64                `json:"unmatched_statements"`
	AmbiguousStatements uint64                `json:"ambiguous_statements"`
	SchemaStatements    uint64                `json:"schema_statements"`
	PlanStatements      uint64                `json:"plan_statements"`
	Findings            []DatabasePlanFinding `json:"findings,omitempty"`
}

type DatabasePlanFinding struct {
	Kind                 string `json:"kind"`
	ClaimLevel           string `json:"claim_level"`
	StatementFingerprint uint64 `json:"statement_fingerprint"`
	Query                string `json:"query"`
	Operation            string `json:"operation"`
	Table                string `json:"table,omitempty"`
	Index                string `json:"index,omitempty"`
	Purpose              string `json:"purpose,omitempty"`
	Summary              string `json:"summary"`
	Action               string `json:"action"`
}

func LoadDatabaseEvidence(path string) (*DatabaseEvidence, error) {
	if path == "" {
		return nil, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxDatabaseEvidenceBytes {
		return nil, fmt.Errorf(
			"%s: database evidence size must be between 1 and %d bytes",
			path,
			maxDatabaseEvidenceBytes,
		)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var evidence DatabaseEvidence
	if err := decodeArtifactMetadataRecord(data, &evidence); err != nil {
		return nil, fmt.Errorf("%s: parse database evidence: %w", path, err)
	}
	if err := validateDatabaseEvidence(&evidence); err != nil {
		return nil, fmt.Errorf("%s: invalid database evidence: %w", path, err)
	}
	return &evidence, nil
}

func validateDatabaseEvidence(evidence *DatabaseEvidence) error {
	if evidence.Format != databaseEvidenceFormat {
		return fmt.Errorf("format must be %d", databaseEvidenceFormat)
	}
	if evidence.Kind != "jankhunter-database-evidence" {
		return fmt.Errorf("kind must be %q", "jankhunter-database-evidence")
	}
	if !evidence.Sanitized {
		return fmt.Errorf("sanitized must be true; raw SQL and raw EXPLAIN text are not accepted")
	}
	if len(evidence.Statements) > maxDatabaseEvidenceStatements {
		return fmt.Errorf("statements exceed limit %d", maxDatabaseEvidenceStatements)
	}
	identities := make(map[databaseEvidenceKey]struct{}, len(evidence.Statements))
	for index := range evidence.Statements {
		statement := &evidence.Statements[index]
		if err := validateDatabaseStatementEvidence(statement); err != nil {
			return fmt.Errorf("statements[%d]: %w", index, err)
		}
		key := databaseEvidenceKey{fingerprint: statement.StatementFingerprint, operation: statement.Operation}
		if _, exists := identities[key]; exists {
			return fmt.Errorf("statements[%d]: duplicate statement_fingerprint and operation", index)
		}
		identities[key] = struct{}{}
	}
	return nil
}

func validateDatabaseStatementEvidence(statement *DatabaseStatementEvidence) error {
	fingerprint, err := parseDatabaseEvidenceFingerprint(statement.FingerprintHex)
	if err != nil {
		return err
	}
	statement.StatementFingerprint = fingerprint
	if !validDatabaseEvidenceOperation(statement.Operation) {
		return fmt.Errorf("operation %q is not supported", statement.Operation)
	}
	if len(statement.Schema.Tables) > maxDatabaseEvidenceTables {
		return fmt.Errorf("schema tables exceed limit %d", maxDatabaseEvidenceTables)
	}
	if len(statement.Plan) > maxDatabaseEvidencePlanSteps {
		return fmt.Errorf("plan steps exceed limit %d", maxDatabaseEvidencePlanSteps)
	}
	tableNames := make(map[string]struct{}, len(statement.Schema.Tables))
	for index := range statement.Schema.Tables {
		table := &statement.Schema.Tables[index]
		if !validDatabaseEvidenceIdentifier(table.Name) {
			return fmt.Errorf("schema.tables[%d].name is not a sanitized identifier", index)
		}
		if _, exists := tableNames[table.Name]; exists {
			return fmt.Errorf("schema.tables[%d].name is duplicate", index)
		}
		tableNames[table.Name] = struct{}{}
		if err := validateDatabaseIndexes(table.Indexes, index); err != nil {
			return err
		}
	}
	for index := range statement.Plan {
		if err := validateDatabasePlanStep(statement.Plan[index]); err != nil {
			return fmt.Errorf("plan[%d]: %w", index, err)
		}
	}
	return nil
}

func validateDatabaseIndexes(indexes []DatabaseIndexEvidence, tableIndex int) error {
	if len(indexes) > maxDatabaseEvidenceIndexes {
		return fmt.Errorf("schema.tables[%d].indexes exceed limit %d", tableIndex, maxDatabaseEvidenceIndexes)
	}
	names := make(map[string]struct{}, len(indexes))
	for index := range indexes {
		candidate := &indexes[index]
		if !validDatabaseEvidenceIdentifier(candidate.Name) {
			return fmt.Errorf("schema.tables[%d].indexes[%d].name is not a sanitized identifier", tableIndex, index)
		}
		if _, exists := names[candidate.Name]; exists {
			return fmt.Errorf("schema.tables[%d].indexes[%d].name is duplicate", tableIndex, index)
		}
		names[candidate.Name] = struct{}{}
		if len(candidate.Columns) == 0 || len(candidate.Columns) > maxDatabaseEvidenceColumns {
			return fmt.Errorf(
				"schema.tables[%d].indexes[%d].columns must contain 1..%d identifiers",
				tableIndex,
				index,
				maxDatabaseEvidenceColumns,
			)
		}
		for columnIndex, column := range candidate.Columns {
			if !validDatabaseEvidenceIdentifier(column) {
				return fmt.Errorf(
					"schema.tables[%d].indexes[%d].columns[%d] is not a sanitized identifier",
					tableIndex,
					index,
					columnIndex,
				)
			}
		}
	}
	return nil
}

func validateDatabasePlanStep(step DatabasePlanStep) error {
	switch step.Kind {
	case "scan", "search", "automatic_index":
		if !validDatabaseEvidenceIdentifier(step.Table) {
			return fmt.Errorf("table is not a sanitized identifier")
		}
	case "temp_btree":
		if !validDatabaseEvidencePurpose(step.Purpose) {
			return fmt.Errorf("purpose %q is not supported", step.Purpose)
		}
	case "compound", "scalar_subquery", "correlated_subquery":
	default:
		return fmt.Errorf("kind %q is not supported", step.Kind)
	}
	if step.Table != "" && !validDatabaseEvidenceIdentifier(step.Table) {
		return fmt.Errorf("table is not a sanitized identifier")
	}
	if step.Index != "" && !validDatabaseEvidenceIdentifier(step.Index) {
		return fmt.Errorf("index is not a sanitized identifier")
	}
	if step.Purpose != "" && !validDatabaseEvidencePurpose(step.Purpose) {
		return fmt.Errorf("purpose %q is not supported", step.Purpose)
	}
	return nil
}

func parseDatabaseEvidenceFingerprint(value string) (uint64, error) {
	if len(value) != 16 {
		return 0, fmt.Errorf("statement_fingerprint must contain exactly 16 lowercase hexadecimal characters")
	}
	var result uint64
	for index := 0; index < len(value); index++ {
		char := value[index]
		var nibble byte
		switch {
		case char >= '0' && char <= '9':
			nibble = char - '0'
		case char >= 'a' && char <= 'f':
			nibble = char - 'a' + 10
		default:
			return 0, fmt.Errorf("statement_fingerprint must contain exactly 16 lowercase hexadecimal characters")
		}
		result = result<<4 | uint64(nibble)
	}
	if result == 0 {
		return 0, fmt.Errorf("statement_fingerprint must be non-zero")
	}
	return result, nil
}

func validDatabaseEvidenceOperation(value string) bool {
	switch value {
	case "query", "insert", "update", "delete", "execute", "statement":
		return true
	default:
		return false
	}
}

func validDatabaseEvidencePurpose(value string) bool {
	switch value {
	case "order_by", "group_by", "distinct", "compound", "union":
		return true
	default:
		return false
	}
}

func validDatabaseEvidenceIdentifier(value string) bool {
	if len(value) == 0 || len(value) > maxDatabaseIdentifierBytes {
		return false
	}
	segmentStart := true
	for index := 0; index < len(value); index++ {
		char := value[index]
		if char == '.' {
			if segmentStart || index+1 == len(value) {
				return false
			}
			segmentStart = true
			continue
		}
		if segmentStart {
			if !asciiIdentifierStart(char) {
				return false
			}
			segmentStart = false
			continue
		}
		if !asciiIdentifierPart(char) {
			return false
		}
	}
	return true
}

func asciiIdentifierStart(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func asciiIdentifierPart(value byte) bool {
	return asciiIdentifierStart(value) || value >= '0' && value <= '9' || value == '$'
}

type databaseEvidenceKey struct {
	fingerprint uint64
	operation   string
}

func applyDatabaseEvidence(analysis *DatabaseAnalysis, evidence *DatabaseEvidence) {
	if analysis == nil || evidence == nil {
		return
	}
	result := &DatabaseEvidenceAnalysis{LoadedStatements: uint64(len(evidence.Statements))}
	statements := make(map[databaseEvidenceKey]*DatabaseStatementStats, len(analysis.Statements))
	for index := range analysis.Statements {
		statement := &analysis.Statements[index]
		if statement.StatementFingerprint == 0 {
			statement.StatementFingerprint = databaseStatementFingerprint(statement.Query)
		}
		operation := databaseEvidenceOperationCode(firstNonEmpty(statement.OperationCode, statement.Operation))
		key := databaseEvidenceKey{
			fingerprint: statement.StatementFingerprint,
			operation:   operation,
		}
		if _, exists := statements[key]; exists {
			statements[key] = nil
			continue
		}
		statements[key] = statement
	}
	for index := range evidence.Statements {
		artifact := &evidence.Statements[index]
		statement, exists := statements[databaseEvidenceKey{
			fingerprint: artifact.StatementFingerprint,
			operation:   artifact.Operation,
		}]
		if !exists {
			result.UnmatchedStatements++
			continue
		}
		if statement == nil {
			result.AmbiguousStatements++
			continue
		}
		result.MatchedStatements++
		if artifact.Schema.Complete || len(artifact.Schema.Tables) > 0 {
			result.SchemaStatements++
		}
		if len(artifact.Plan) > 0 {
			result.PlanStatements++
		}
		for _, step := range artifact.Plan {
			if finding, ok := databasePlanFinding(*statement, artifact, step); ok {
				result.Findings = append(result.Findings, finding)
			}
		}
	}
	analysis.Evidence = result
}

func databaseEvidenceOperationCode(value string) string {
	switch value {
	case "query", "чтение":
		return "query"
	case "insert", "вставка":
		return "insert"
	case "update", "обновление":
		return "update"
	case "delete", "удаление":
		return "delete"
	case "execute", "выполнение":
		return "execute"
	case "statement", "подготовка":
		return "statement"
	default:
		return value
	}
}

func databaseStatementFingerprint(query string) uint64 {
	if query == "" || query == "unknown" {
		return 0
	}
	return jhlog.DatabaseStatementFingerprint(query)
}

func databasePlanFinding(
	statement DatabaseStatementStats,
	artifact *DatabaseStatementEvidence,
	step DatabasePlanStep,
) (DatabasePlanFinding, bool) {
	finding := DatabasePlanFinding{
		Kind: step.Kind, ClaimLevel: "observed",
		StatementFingerprint: artifact.StatementFingerprint,
		Query:                statement.Query, Operation: statement.Operation,
		Table: step.Table, Index: step.Index, Purpose: step.Purpose,
	}
	switch step.Kind {
	case "scan":
		finding.Summary = fmt.Sprintf("Импортированный план подтверждает SCAN %s.", step.Table)
		if step.Index != "" {
			finding.Summary = fmt.Sprintf("Импортированный план подтверждает SCAN %s через индекс %s.", step.Table, step.Index)
		}
		finding.Action = "Проверьте, соответствует ли полный проход ожидаемой селективности; индекс рассматривайте только после проверки предикатов, объёма и стоимости записи."
		if artifact.Schema.Complete && databaseEvidenceTableHasNoIndexes(artifact.Schema, step.Table) {
			finding.Summary += " В полном приложенном снимке схемы для таблицы индексы не объявлены."
		}
		return finding, true
	case "temp_btree":
		finding.Summary = fmt.Sprintf(
			"Импортированный план подтверждает временное B-tree для %s.",
			databaseEvidencePurposeLabel(step.Purpose),
		)
		finding.Action = "Проверьте возможность выполнить сортировку или группировку подходящим индексом и сравните план и latency на том же наборе данных."
		return finding, true
	case "automatic_index":
		finding.Summary = fmt.Sprintf("Импортированный план подтверждает automatic index для %s.", step.Table)
		finding.Action = "Проверьте устойчивый явный индекс на реальной нагрузке и стоимость его поддержки при записи."
		return finding, true
	default:
		return DatabasePlanFinding{}, false
	}
}

func databaseEvidenceTableHasNoIndexes(schema DatabaseSchemaEvidence, tableName string) bool {
	for index := range schema.Tables {
		if schema.Tables[index].Name == tableName {
			return len(schema.Tables[index].Indexes) == 0
		}
	}
	return false
}

func databaseEvidencePurposeLabel(value string) string {
	switch value {
	case "order_by":
		return "ORDER BY"
	case "group_by":
		return "GROUP BY"
	case "distinct":
		return "DISTINCT"
	case "compound":
		return "compound query"
	case "union":
		return "UNION"
	default:
		return value
	}
}
