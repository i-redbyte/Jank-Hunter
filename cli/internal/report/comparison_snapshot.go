package report

import "github.com/i-redbyte/jank-hunter/cli/internal/analyze"

const comparisonSnapshotElementID = "jankhunter-comparison-snapshot"
const comparisonSnapshotSizeLimit = 32 << 20

func WriteBundleWithComparisonSnapshot(path string, pages []BundlePage, document analyze.ComparisonSnapshotDocument) error {
	if _, err := analyze.ValidateComparisonSnapshotDocument(document); err != nil {
		return err
	}
	return writeBundle(path, pages, &document)
}
