package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

func printBuildIdentity(out io.Writer, info *debug.BuildInfo) {
	revision, modified := "unknown", "unknown"
	if info != nil {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if setting.Value != "" {
					revision = setting.Value
				}
			case "vcs.modified":
				if setting.Value == "true" || setting.Value == "false" {
					modified = setting.Value
				}
			}
		}
	}
	fmt.Fprintf(out, "build: revision=%s modified=%s\n", revision, modified)
}
