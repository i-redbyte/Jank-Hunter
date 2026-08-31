package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestInspectTemplateContainsNoLegacyDatabaseIOProjection(t *testing.T) {
	for _, stale := range []string{
		"Существующие I/O события БД",
		"отдельная аналитика БД будет реализована в другой задаче",
	} {
		if strings.Contains(inspectTemplate, stale) {
			t.Fatalf("inspect template retains legacy database copy %q", stale)
		}
	}
}

func TestDatabaseStatementRowsExposeSlowFrequentMainThreadQuery(t *testing.T) {
	summary := analyze.Summary{DatabaseAnalysis: &analyze.DatabaseAnalysis{Statements: []analyze.DatabaseStatementStats{{
		Query:              "SELECT * FROM messages WHERE chat_id = ?",
		Operation:          "чтение",
		Overall:            analyze.DatabaseExecutionStats{Calls: 40, P95DurationUS: 45_000},
		Main:               analyze.DatabaseExecutionStats{Calls: 40, P95DurationUS: 45_000},
		PeakCallsPerSecond: 20, RapidRepeats: 30,
		Contexts: []analyze.DatabaseStatementContextStats{{
			Source: "MessagesDao.load", Framework: "Room", Screen: "Chat", ContextOperation: "chat.open",
		}},
	}}}}

	rows := databaseStatementRows(summary)
	if len(rows) != 1 || rows[0].Severity != "high" || rows[0].Source != "MessagesDao.load" {
		t.Fatalf("database rows = %+v", rows)
	}
}

func TestDatabaseStatementRowsDoNotUseBackgroundP95ForMainThreadStatus(t *testing.T) {
	summary := analyze.Summary{DatabaseAnalysis: &analyze.DatabaseAnalysis{Statements: []analyze.DatabaseStatementStats{{
		Query:      "SELECT value FROM samples",
		Operation:  "чтение",
		Overall:    analyze.DatabaseExecutionStats{Calls: 26, P95DurationUS: 200_000, MaxDurationUS: 200_000},
		Main:       analyze.DatabaseExecutionStats{Calls: 1, P95DurationUS: 1_000, MaxDurationUS: 1_000},
		Background: analyze.DatabaseExecutionStats{Calls: 25, P95DurationUS: 200_000, MaxDurationUS: 200_000},
		Contexts: []analyze.DatabaseStatementContextStats{{
			Source: "SamplesDao.load", Framework: "Room", Screen: "Samples", ContextOperation: "samples.open",
		}},
	}}}}

	rows := databaseStatementRows(summary)
	if len(rows) != 1 || rows[0].Severity != "medium" || rows[0].Status != "медленный фоновый вызов" {
		t.Fatalf("database rows = %+v", rows)
	}
}

func TestDatabaseStatementActionDoesNotRecommendQueryPlanForBackgroundWrite(t *testing.T) {
	statement := analyze.DatabaseStatementStats{
		Query: "UPDATE messages SET state = ? WHERE id = ?", Operation: "обновление",
		Overall:    analyze.DatabaseExecutionStats{Calls: 8, P95DurationUS: 250_000, MaxDurationUS: 300_000},
		Background: analyze.DatabaseExecutionStats{Calls: 8, P95DurationUS: 250_000, MaxDurationUS: 300_000},
	}
	action := databaseStatementAction(statement, analyze.DefaultProblemDetectorConfig())
	if strings.Contains(action, "план запроса") || strings.Contains(action, "объём выборки") ||
		(!strings.Contains(strings.ToLower(action), "транзак") && !strings.Contains(strings.ToLower(action), "batch")) {
		t.Fatalf("slow write action is not operation-aware: %q", action)
	}
}

func TestDatabaseCoverageUsesExactRatio(t *testing.T) {
	if got := databaseCoverage(2, 3); got != "66.7%" {
		t.Fatalf("databaseCoverage() = %q", got)
	}
}

func TestInspectDatabaseUsesProblemFirstFullWidthSQLCards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database.html")
	query := "SELECT contact.* FROM contact_data AS contact LEFT JOIN chat_data AS chat ON chat.sn = contact.sn WHERE contact.is_temporary = ? OR contact.is_suspicious = ? ORDER BY last_message_time DESC"
	summary := analyze.Summary{
		Title: "database-layout",
		DatabaseCoverage: analyze.DatabaseCoverage{
			Status: "observed", StatusLabel: "данные собраны", Explanation: "SQL telemetry активна",
		},
		DatabaseAnalysis: &analyze.DatabaseAnalysis{
			Overall: analyze.DatabaseExecutionStats{Calls: 2},
			Statements: []analyze.DatabaseStatementStats{
				{
					Query: query, Operation: "query",
					Overall: analyze.DatabaseExecutionStats{Calls: 1, MaxDurationUS: 60_000},
					Main:    analyze.DatabaseExecutionStats{Calls: 1, MaxDurationUS: 60_000},
					Contexts: []analyze.DatabaseStatementContextStats{{
						Source:    "ru.mail.persistence.room.dao.ContactDataDao_Impl.findContactsForLocalSearch",
						Framework: "Room", Screen: "Contacts", ContextOperation: "contacts.search",
					}},
				},
				{
					Query: "SELECT 1", Operation: "query",
					Overall:    analyze.DatabaseExecutionStats{Calls: 1, MaxDurationUS: 100},
					Background: analyze.DatabaseExecutionStats{Calls: 1, MaxDurationUS: 100},
				},
			},
		},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, expected := range []string{
		`class="database-problem-list"`,
		`class="database-statement-card`,
		`class="database-sql-template"><code>`,
		query,
		"Почему это важно",
		"Контексты выполнения",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("database report does not contain %q", expected)
		}
	}
	if strings.Contains(html, `class="database-query-table"`) {
		t.Fatal("SQL is still rendered inside the legacy wide table")
	}
	problemIndex := strings.Index(html, query)
	overviewIndex := strings.Index(html, "Всего вызовов БД")
	observationIndex := strings.Index(html, "SELECT 1")
	if problemIndex < 0 || overviewIndex < 0 || observationIndex < 0 ||
		problemIndex > overviewIndex || overviewIndex > observationIndex {
		t.Fatalf("database narrative is not problem -> overview -> observations: problem=%d overview=%d observation=%d", problemIndex, overviewIndex, observationIndex)
	}
}

func TestInspectDatabaseExplainsTransactionsResultsAndMeasuredPhases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database-transactions.html")
	summary := analyze.Summary{
		Title: "database-transactions",
		DatabaseAnalysis: &analyze.DatabaseAnalysis{
			Overall: analyze.DatabaseExecutionStats{Calls: 2, Failures: 1},
			MainCorrelation: analyze.DatabaseCorrelationStats{
				HTTPOverlaps: 1, WorkerOverlaps: 2, FileIOOverlaps: 3, GCOverlaps: 4,
			},
			Scenarios: analyze.DatabaseScenarioAnalysis{
				ObservedOperationScopes: 5,
				Candidates: []analyze.DatabaseScenarioStats{{
					Kind: "possible_n_plus_one_or_duplicate", ClaimLevel: "hypothesis",
					ScopeKind: "operation", Query: "SELECT item FROM feed WHERE id = ?",
					Source: "FeedDao.load", ContextOperation: "feed.open", Operation: "query",
					AffectedScopes: 2, ObservedScopes: 5, EstimatedCalls: 12, RetainedCalls: 12,
					MaxCallsPerScope: 7, CallsPerObservedScope: 2.4, TotalDurationUS: 24_000,
				}},
			},
			Telemetry: analyze.DatabaseTelemetryStats{
				ResultKnownCalls: 1, TransactionLinkedCalls: 2, PreparedExecutionCalls: 1,
				PhaseMeasuredCalls: 1,
				LockWait:           analyze.DatabasePhaseStats{Samples: 1, P95DurationUS: 4_000, MaxDurationUS: 4_000},
				FailureKinds:       []analyze.NamedValue{{Name: "busy_locked", Value: 1}},
				ResultCountBuckets: []analyze.NamedValue{{Name: "2_10", Value: 1}},
			},
			Transactions: &analyze.DatabaseTransactionAnalysis{
				Begun: 2, Completed: 1, Incomplete: 1, Rollbacks: 1, MainThread: 1,
				P95DurationUS: 180_000, MaxDurationUS: 180_000, MaxStatementCount: 42,
				Transactions: []analyze.DatabaseTransactionStats{{
					Source: "SyncStore.replace", Process: "main", Screen: "Inbox",
					ContextOperation: "sync.apply", TransactionID: 7, ParentID: 3,
					MainThread: true, Complete: true, Mode: "immediate", Outcome: "rollback",
					DurationUS: 180_000, StatementCount: 42, ReadCount: 40, WriteCount: 2,
					Correlation: analyze.DatabaseCorrelationStats{
						HTTPOverlaps: 1, WorkerOverlaps: 2, FileIOOverlaps: 3, GCOverlaps: 4,
					},
				}},
			},
		},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, expected := range []string{
		"Сценарные гипотезы", "возможный N&#43;1 или повторный запрос", "до 7 раз внутри одной границы сценария",
		`<pre class="database-sql-template"><code>SELECT item FROM feed WHERE id = ?`,
		"N&#43;1 или одинаковые параметры не доказаны", "пакетную обработку, объединённый запрос и кэширование",
		"HTTP / фоновые задачи / файлы / GC", "1 / 2 / 3 / 4", "совпадение по времени",
		"Проблемные транзакции", "SyncStore.replace", "откат", "42</strong>",
		"Результат известен", "БД занята или заблокирована", "ожидание блокировки", "только при явном измерении",
		"Cursor.getCount()", "beginDatabasePhase", "общая длительность не считается ожиданием блокировки",
		"Незавершённые транзакции не получают искусственную длительность",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("database transaction report does not contain %q", expected)
		}
	}
}

func TestInspectDatabaseExplainsOfflinePlanEvidenceWithoutOverclaimingIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database-plan.html")
	summary := analyze.Summary{
		Title: "database-plan",
		DatabaseAnalysis: &analyze.DatabaseAnalysis{
			Overall: analyze.DatabaseExecutionStats{Calls: 3},
			Evidence: &analyze.DatabaseEvidenceAnalysis{
				LoadedStatements: 2, MatchedStatements: 1, UnmatchedStatements: 1,
				AmbiguousStatements: 1, SchemaStatements: 1, PlanStatements: 1,
				Findings: []analyze.DatabasePlanFinding{
					{
						Kind: "scan", ClaimLevel: "observed", Query: "SELECT value FROM message WHERE chat_id = ?",
						Operation: "query", Table: "message",
						Summary: "Импортированный план подтверждает SCAN message.",
						Action:  "Проверьте селективность; индекс — только проверяемый кандидат.",
					},
					{
						Kind: "temp_btree", ClaimLevel: "observed", Query: "SELECT value FROM message WHERE chat_id = ?",
						Operation: "query", Purpose: "order_by",
						Summary: "Импортированный план подтверждает временное B-дерево для ORDER BY.",
						Action:  "Сравните план после изменения.",
					},
				},
			},
		},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, expected := range []string{
		"Отдельно переданные данные схемы и плана SQL-запроса",
		"зафиксировано · SCAN",
		"временное B-дерево",
		`<pre class="database-sql-template"><code>SELECT value FROM message WHERE chat_id = ?`,
		"1 из 2",
		"неоднозначных отпечатков — 1",
		"не выполняет SQL, EXPLAIN или PRAGMA",
		"не доказывает, что индекс нужен",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("database plan report does not contain %q", expected)
		}
	}
}

func TestInspectDatabaseCoverageExplainsEnabledCollectorWithoutEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database-coverage.html")
	summary := analyze.Summary{
		Title: "database-coverage",
		DatabaseCoverage: analyze.DatabaseCoverage{
			Status: "no_observations", StatusLabel: "нет наблюдений",
			Explanation:            "Сборщик включён и ASM hooks найдены, но SQL-вызовы не наблюдались.",
			Action:                 "Повторите целевой сценарий с обращением к базе.",
			RuntimeEnabledSessions: 1, CollectorSessions: 1, InstrumentedHooks: 4,
		},
	}
	if err := WriteInspectWithOptions(path, summary, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, expected := range []string{
		"Сборщик включён и ASM hooks найдены, но SQL-вызовы не наблюдались.",
		"Повторите целевой сценарий с обращением к базе.",
		"databaseTracing.set(true)",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("coverage report does not contain %q", expected)
		}
	}
}

func TestCompareReportRendersDatabaseMetricsAndCanonicalStatements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database-compare.html")
	comparison := analyze.Comparison{Database: analyze.DatabaseComparison{
		Comparable: true,
		Metrics: []analyze.Delta{
			{
				Name: "DB calls per minute", Baseline: "100.00 выз./мин", Candidate: "125.00 выз./мин",
				Change: "+25.0%", Severity: "high", Confidence: "high", Comparable: true, SampleSize: 100,
				ComparisonNote: "нормировано по длительности",
			},
			{
				Name: "DB transaction p95", Baseline: "40.00 мс", Candidate: "80.00 мс",
				Change: "+100.0%", Severity: "high", Confidence: "high", Comparable: true, SampleSize: 40,
				ComparisonNote: "только completed lifecycle",
			},
		},
		Statements: []analyze.DatabaseStatementDelta{{
			Query: "SELECT value FROM sample WHERE id = ?", Operation: "query",
			BaselinePresent: true, CandidatePresent: true, Comparable: true, LatencyComparable: true,
			Status: "compared", Severity: "high", Confidence: "high",
			BaselineCalls: 100, CandidateCalls: 125,
			BaselineCallsPerMinute: 100, CandidateCallsPerMinute: 125,
			BaselineMainP95US: 20_000, CandidateMainP95US: 40_000,
			BaselineFailureRatePct: 1, CandidateFailureRatePct: 4,
			BaselineWallMSPerOperation: 20, CandidateWallMSPerOperation: 35,
		}},
	}}
	if err := WriteCompareReportWithOptions(path, comparison, nil, nil, ReportOptions{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, expected := range []string{
		"Сравнение базы данных", "Вызовов БД в минуту",
		"Граница верхних 5% транзакций БД", "только по завершённым транзакциям",
		"SELECT value FROM sample WHERE id = ?", "нормировано по длительности", "сопоставлено",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("database compare does not contain %q", expected)
		}
	}
}
