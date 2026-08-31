package analyze

const databaseStablePercentileSample = 20

type DatabaseStatementAssessment struct {
	MainThreadSlow        bool
	BackgroundSlow        bool
	Storm                 bool
	Repeated              bool
	Failures              bool
	MainDurationUS        uint64
	BackgroundDurationUS  uint64
	MainUsesMaximum       bool
	BackgroundUsesMaximum bool
	FailureRate           float64
}

type DatabaseTransactionAssessment struct {
	MainThreadSlow bool
	BackgroundSlow bool
	Failure        bool
	Rollback       bool
	ManyStatements bool
	Incomplete     bool
	Nested         bool
}

func AssessDatabaseTransaction(
	transaction DatabaseTransactionStats,
	cfg ProblemDetectorConfig,
) DatabaseTransactionAssessment {
	return DatabaseTransactionAssessment{
		MainThreadSlow: transaction.Complete && transaction.MainThread &&
			transaction.DurationUS >= cfg.DatabaseTransactionMainMS*1_000,
		BackgroundSlow: transaction.Complete && !transaction.MainThread &&
			transaction.DurationUS >= cfg.DatabaseTransactionBackgroundMS*1_000,
		Failure:        transaction.Outcome == "failure",
		Rollback:       transaction.Outcome == "rollback",
		ManyStatements: transaction.StatementCount >= cfg.DatabaseTransactionStatements,
		Incomplete:     !transaction.Complete,
		Nested:         transaction.ParentID != 0,
	}
}

func (a DatabaseTransactionAssessment) IsProblem() bool {
	return a.MainThreadSlow || a.BackgroundSlow || a.Failure || a.Rollback ||
		a.ManyStatements || a.Incomplete || a.Nested
}

func databaseFailureDisplayName(value string) string {
	switch value {
	case "busy_locked":
		return "БД занята или заблокирована"
	case "disk_full":
		return "диск заполнен"
	case "read_only":
		return "только чтение"
	case "none", "":
		return "не указан"
	default:
		return value
	}
}

func AssessDatabaseStatement(
	statement DatabaseStatementStats,
	cfg ProblemDetectorConfig,
) DatabaseStatementAssessment {
	mainDuration, mainUsesMaximum := databaseAssessmentDuration(statement.Main)
	backgroundDuration, backgroundUsesMaximum := databaseAssessmentDuration(statement.Background)
	failureRate := problemRatio(statement.Overall.Failures, statement.Overall.Calls)
	callEstimate := DatabaseStatementCallEstimate(statement)
	return DatabaseStatementAssessment{
		MainThreadSlow: statement.Main.Calls > 0 &&
			mainDuration >= cfg.DatabaseMainThreadMS*1_000,
		BackgroundSlow: statement.Background.Calls > 0 &&
			backgroundDuration >= cfg.DatabaseBackgroundMS*1_000,
		Storm: callEstimate >= cfg.DatabaseStormMinCount &&
			statement.PeakCallsPerSecond >= cfg.DatabaseStormRate,
		Repeated: statement.RapidRepeats >= cfg.DatabaseRapidRepeatCount,
		Failures: statement.Overall.Failures > 0 &&
			(statement.Overall.Failures >= 2 || failureRate >= cfg.DatabaseFailureRate),
		MainDurationUS: mainDuration, BackgroundDurationUS: backgroundDuration,
		MainUsesMaximum: mainUsesMaximum, BackgroundUsesMaximum: backgroundUsesMaximum,
		FailureRate: failureRate,
	}
}

func DatabaseStatementCallEstimate(statement DatabaseStatementStats) uint64 {
	return maxUint64(statement.Overall.Calls, statement.EstimatedCalls)
}

func (a DatabaseStatementAssessment) IsProblem() bool {
	return a.MainThreadSlow || a.BackgroundSlow || a.Storm || a.Repeated || a.Failures
}

func databaseAssessmentDuration(stats DatabaseExecutionStats) (uint64, bool) {
	if stats.Calls == 0 {
		return 0, false
	}
	if stats.Calls < databaseStablePercentileSample {
		return stats.MaxDurationUS, true
	}
	return stats.P95DurationUS, false
}
