package main

import (
	"fmt"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
	"github.com/i-redbyte/jank-hunter/cli/internal/report"
)

func buildLogReports(group string, paths []string, options analyze.Options, combined analyze.Summary) ([]report.LogReport, error) {
	reports := make([]report.LogReport, 0, len(paths))
	for i, path := range paths {
		summary := combined
		if len(paths) != 1 {
			var err error
			summary, err = analyze.InspectFilesWithOptions(path, []string{path}, options)
			if err != nil {
				return nil, err
			}
		}
		reports = append(reports, report.LogReport{
			Name:    path,
			Anchor:  fmt.Sprintf("%s-log-%d", group, i+1),
			Summary: summary,
		})
	}
	return reports, nil
}

func takeFilterFlags(args []string) (analyze.Filter, []string, error) {
	route, remaining, err := takeStringFlag(args, "route", "")
	if err != nil {
		return analyze.Filter{}, nil, err
	}
	screen, remaining, err := takeStringFlag(remaining, "screen", "")
	if err != nil {
		return analyze.Filter{}, nil, err
	}
	owner, remaining, err := takeStringFlag(remaining, "owner", "")
	if err != nil {
		return analyze.Filter{}, nil, err
	}
	className, remaining, err := takeStringFlag(remaining, "class", "")
	if err != nil {
		return analyze.Filter{}, nil, err
	}
	return analyze.Filter{RouteContains: route, ScreenContains: screen, OwnerContains: owner, ClassContains: className}, remaining, nil
}
