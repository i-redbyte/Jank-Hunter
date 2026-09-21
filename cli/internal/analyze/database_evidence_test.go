package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

func TestLoadDatabaseEvidenceAcceptsSanitizedTypedPlan(t *testing.T) {
	path := writeDatabaseEvidenceFixture(t, `{
		"format": 1,
		"kind": "jankhunter-database-evidence",
		"sanitized": true,
		"statements": [{
			"statement_fingerprint": "a430d84680aabd0b",
			"operation": "query",
			"schema": {"complete": true, "tables": [{
				"name": "message",
				"indexes": [{"name": "message_by_chat", "columns": ["chat_id", "created_at"]}]
			}]},
			"plan": [
				{"kind": "scan", "table": "message"},
				{"kind": "temp_btree", "purpose": "order_by"}
			]
		}]
	}`)

	evidence, err := LoadDatabaseEvidence(path)
	if err != nil {
		t.Fatalf("LoadDatabaseEvidence() error = %v", err)
	}
	if evidence == nil || len(evidence.Statements) != 1 {
		t.Fatalf("evidence = %+v", evidence)
	}
	statement := evidence.Statements[0]
	if statement.StatementFingerprint != 0xa430d84680aabd0b || len(statement.Plan) != 2 {
		t.Fatalf("statement = %+v", statement)
	}
}

func TestLoadDatabaseEvidenceRejectsUnsafeOrAmbiguousInput(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "not explicitly sanitized",
			body: `{"format":1,"kind":"jankhunter-database-evidence","sanitized":false,"statements":[]}`,
			want: "sanitized",
		},
		{
			name: "raw explain detail is not part of the contract",
			body: `{"format":1,"kind":"jankhunter-database-evidence","sanitized":true,"statements":[{"statement_fingerprint":"a430d84680aabd0b","operation":"query","plan":[{"kind":"scan","detail":"SCAN message"}]}]}`,
			want: "unknown field",
		},
		{
			name: "unsafe identifier",
			body: `{"format":1,"kind":"jankhunter-database-evidence","sanitized":true,"statements":[{"statement_fingerprint":"a430d84680aabd0b","operation":"query","plan":[{"kind":"scan","table":"message; DROP TABLE message"}]}]}`,
			want: "identifier",
		},
		{
			name: "duplicate identity",
			body: `{"format":1,"kind":"jankhunter-database-evidence","sanitized":true,"statements":[{"statement_fingerprint":"a430d84680aabd0b","operation":"query"},{"statement_fingerprint":"a430d84680aabd0b","operation":"query"}]}`,
			want: "duplicate",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadDatabaseEvidence(writeDatabaseEvidenceFixture(t, test.body))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestDatabaseEvidenceMatchesFingerprintAndBuildsObservedFindings(t *testing.T) {
	evidence := &DatabaseEvidence{Statements: []DatabaseStatementEvidence{{
		StatementFingerprint: jhlog.DatabaseStatementFingerprint("hello"),
		Operation:            "query",
		Schema: DatabaseSchemaEvidence{Complete: true, Tables: []DatabaseTableEvidence{{
			Name: "message",
		}}},
		Plan: []DatabasePlanStep{
			{Kind: "scan", Table: "message"},
			{Kind: "temp_btree", Purpose: "order_by"},
		},
	}}}
	analysis := &DatabaseAnalysis{Statements: []DatabaseStatementStats{{
		Query: "hello", Operation: "чтение", OperationCode: "query", Overall: DatabaseExecutionStats{Calls: 3},
	}}}

	applyDatabaseEvidence(analysis, evidence)

	if analysis.Evidence == nil {
		t.Fatal("database evidence analysis is nil")
	}
	if analysis.Evidence.MatchedStatements != 1 || analysis.Evidence.PlanStatements != 1 ||
		analysis.Evidence.UnmatchedStatements != 0 {
		t.Fatalf("evidence totals = %+v", analysis.Evidence)
	}
	if len(analysis.Evidence.Findings) != 2 {
		t.Fatalf("findings = %+v", analysis.Evidence.Findings)
	}
	for _, finding := range analysis.Evidence.Findings {
		if finding.ClaimLevel != "observed" || finding.Query != "hello" {
			t.Fatalf("finding = %+v", finding)
		}
	}
}

func TestDatabaseEvidenceNeverInventsPlanFinding(t *testing.T) {
	evidence := &DatabaseEvidence{Statements: []DatabaseStatementEvidence{{
		StatementFingerprint: jhlog.DatabaseStatementFingerprint("hello"),
		Operation:            "query",
		Schema:               DatabaseSchemaEvidence{Complete: true, Tables: []DatabaseTableEvidence{{Name: "message"}}},
	}}}
	analysis := &DatabaseAnalysis{Statements: []DatabaseStatementStats{{
		Query: "hello", Operation: "чтение", OperationCode: "query",
	}}}

	applyDatabaseEvidence(analysis, evidence)

	if analysis.Evidence == nil || analysis.Evidence.MatchedStatements != 1 {
		t.Fatalf("evidence = %+v", analysis.Evidence)
	}
	if analysis.Evidence.PlanStatements != 0 || len(analysis.Evidence.Findings) != 0 {
		t.Fatalf("plan findings without plan = %+v", analysis.Evidence)
	}
}

func TestDatabaseEvidenceRejectsAmbiguousFingerprintMatch(t *testing.T) {
	evidence := &DatabaseEvidence{Statements: []DatabaseStatementEvidence{{
		StatementFingerprint: 77, Operation: "query",
		Plan: []DatabasePlanStep{{Kind: "scan", Table: "message"}},
	}}}
	analysis := &DatabaseAnalysis{Statements: []DatabaseStatementStats{
		{Query: "SELECT first", Operation: "чтение", OperationCode: "query", StatementFingerprint: 77},
		{Query: "SELECT second", Operation: "чтение", OperationCode: "query", StatementFingerprint: 77},
	}}

	applyDatabaseEvidence(analysis, evidence)

	if analysis.Evidence == nil || analysis.Evidence.AmbiguousStatements != 1 ||
		analysis.Evidence.MatchedStatements != 0 || len(analysis.Evidence.Findings) != 0 {
		t.Fatalf("ambiguous evidence = %+v", analysis.Evidence)
	}
}

func writeDatabaseEvidenceFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "database-evidence.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
