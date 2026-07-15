package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

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
	profile, err := benchfixture.ProfileByName(profileName)
	if err != nil {
		fatal(err)
	}
	metadata, err := benchfixture.Write(out, profile)
	if err != nil {
		fatal(err)
	}
	if metadataOut != "" {
		file, err := os.Create(metadataOut)
		if err != nil {
			fatal(err)
		}
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		encodeErr := encoder.Encode(metadata)
		closeErr := file.Close()
		if encodeErr != nil {
			fatal(encodeErr)
		}
		if closeErr != nil {
			fatal(closeErr)
		}
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

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "jhfixture:", err)
	os.Exit(1)
}
