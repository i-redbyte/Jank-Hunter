package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/i-redbyte/jank-hunter/cli/internal/atomicfile"
	"github.com/i-redbyte/jank-hunter/cli/internal/benchfixture"
)

func main() {
	var out string
	var metadataOut string
	var profileName string
	flag.StringVar(&out, "out", "", "output .jhlog path")
	flag.StringVar(&metadataOut, "metadata", "", "optional metadata JSON path")
	flag.StringVar(&profileName, "profile", "representative", "fixture profile: smoke or representative")
	flag.Parse()

	if out == "" {
		fatal(fmt.Errorf("--out is required"))
	}
	metadata, err := writeFixture(out, metadataOut, profileName)
	if err != nil {
		fatal(err)
	}
	fmt.Printf(
		"generated %s fixture: events=%d records=%d dictionary=%d bytes=%d\n",
		metadata.Profile,
		metadata.Events,
		metadata.TotalRecords,
		metadata.DictionaryEntries,
		metadata.CompressedBytes,
	)
}

func writeFixture(out, metadataOut, profileName string) (benchfixture.Metadata, error) {
	if err := rejectFixtureOutputOverlap(out, metadataOut); err != nil {
		return benchfixture.Metadata{}, err
	}
	profile, err := benchfixture.ProfileByName(profileName)
	if err != nil {
		return benchfixture.Metadata{}, err
	}
	metadata, err := benchfixture.Write(out, profile)
	if err != nil {
		return benchfixture.Metadata{}, err
	}
	if metadataOut != "" {
		// A previously absent path may now resolve to the newly created capture
		// through a dangling symlink or a symlinked directory.
		if err := rejectFixtureOutputOverlap(out, metadataOut); err != nil {
			return benchfixture.Metadata{}, err
		}
		if err := atomicfile.Write(metadataOut, 0o644, func(file *os.File) error {
			encoder := json.NewEncoder(file)
			encoder.SetIndent("", "  ")
			return encoder.Encode(metadata)
		}); err != nil {
			return benchfixture.Metadata{}, err
		}
	}
	return metadata, nil
}

func rejectFixtureOutputOverlap(out, metadata string) error {
	if metadata == "" {
		return nil
	}
	capturePath, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	metadataPath, err := filepath.Abs(metadata)
	if err != nil {
		return err
	}
	captureInfo, captureErr := os.Stat(out)
	metadataInfo, metadataErr := os.Stat(metadata)
	if captureErr != nil && !os.IsNotExist(captureErr) {
		return captureErr
	}
	if metadataErr != nil && !os.IsNotExist(metadataErr) {
		return metadataErr
	}
	if capturePath == metadataPath || captureErr == nil && metadataErr == nil && os.SameFile(captureInfo, metadataInfo) {
		return fmt.Errorf("fixture output %q and metadata %q overlap; choose separate files", out, metadata)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "jhfixture:", err)
	os.Exit(1)
}
