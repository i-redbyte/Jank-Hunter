package analyze

import "github.com/i-redbyte/jank-hunter/cli/internal/jhlog"

// A boundary snapshot observes incomplete work; it does not establish a lost delivery count.
// A missing object preserves the historical meaning: the input did not report these observations.
func asyncLifecycleQuality(counters map[uint64]uint64) *AsyncLifecycleQuality {
	quality := AsyncLifecycleQuality{
		StaleCompletions:               counters[jhlog.QualityAsyncCompletionStale],
		DuplicateCompletions:           counters[jhlog.QualityAsyncCompletionDuplicate],
		InvalidCompletions:             counters[jhlog.QualityAsyncCompletionInvalid],
		FeatureDisabledCompletions:     counters[jhlog.QualityAsyncCompletionFeatureDisabled],
		CapacityRejected:               counters[jhlog.QualityAsyncTokenCapacityRejected],
		IdentityExhausted:              counters[jhlog.QualityAsyncTokenIdExhausted],
		UnfinishedHTTP:                 counters[jhlog.QualityAsyncUnfinishedHTTP],
		UnfinishedDatabase:             counters[jhlog.QualityAsyncUnfinishedDatabase],
		UnfinishedWorker:               counters[jhlog.QualityAsyncUnfinishedWorker],
		UnfinishedDatabaseTransactions: counters[jhlog.QualityAsyncUnfinishedDatabaseTransaction],
		CompletionsInProgressAtStop:    counters[jhlog.QualityAsyncCompletionInProgressAtStop],
		LegacyHTTPCompletions:          counters[jhlog.QualityHTTPLegacyContextCompletion],
	}
	if quality == (AsyncLifecycleQuality{}) {
		return nil
	}
	return &quality
}
