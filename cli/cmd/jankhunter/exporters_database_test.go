package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

func TestDatabaseFindingsAreEquivalentInProblemsJSONAndCSV(t *testing.T) {
	summary := analyze.Summary{
		DurationMS: 30_000,
		DatabaseAnalysis: &analyze.DatabaseAnalysis{
			Transactions: &analyze.DatabaseTransactionAnalysis{
				Completed: 1, Failures: 1,
				Transactions: []analyze.DatabaseTransactionStats{{
					Source: "SyncStore.replace", Process: "main", Screen: "Inbox",
					ContextOperation: "sync.apply", TransactionID: 7, Complete: true,
					Outcome: "failure", FailureKind: "busy_locked", DurationUS: 250_000,
					StatementCount: 12,
				}},
			},
			Scenarios: analyze.DatabaseScenarioAnalysis{
				ObservedOperationScopes: 10,
				Candidates: []analyze.DatabaseScenarioStats{{
					Kind: "possible_n_plus_one_or_duplicate", ClaimLevel: "hypothesis",
					ScopeKind: "operation", Query: "SELECT item FROM feed WHERE id = ?",
					Source: "FeedDao.load", Screen: "Feed", ContextOperation: "feed.open",
					AffectedScopes: 2, ObservedScopes: 10, EstimatedCalls: 12, RetainedCalls: 12,
					MaxCallsPerScope: 7, TotalDurationUS: 24_000,
				}},
			},
		},
	}
	problemReport, err := analyze.BuildProblemReport(summary)
	if err != nil {
		t.Fatal(err)
	}
	summary.ProblemSchemaVersion = problemReport.Summary.SchemaVersion
	summary.ProblemSummary = problemReport.Summary
	summary.Problems = problemReport.Problems
	summary.ProblemIncidents = problemReport.Incidents
	summary.CategoryCoverage = problemReport.Coverage
	summary.Detectors = problemReport.Registry

	var jsonOutput bytes.Buffer
	if err := writeProblemsDatasetJSON(&jsonOutput, datasetProblems, summary, nil); err != nil {
		t.Fatal(err)
	}
	var csvOutput bytes.Buffer
	if err := writeProblemsDatasetCSV(&csvOutput, datasetProblems, summary, nil); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"io.database_transaction", "SyncStore.replace", "БД занята или заблокирована", "12 SQL-вызовов",
		"io.database_repeated_in_scope", "FeedDao.load", "hypothesis", "SELECT item FROM feed WHERE id = ?",
	} {
		if !strings.Contains(jsonOutput.String(), expected) {
			t.Fatalf("problems JSON does not contain %q:\n%s", expected, jsonOutput.String())
		}
		if !strings.Contains(csvOutput.String(), expected) {
			t.Fatalf("problems CSV does not contain %q:\n%s", expected, csvOutput.String())
		}
	}
}
