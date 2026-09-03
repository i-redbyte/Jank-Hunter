package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func takeStringFlag(args []string, name, fallback string) (string, []string, error) {
	long := "--" + name
	short := "-" + name
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == long || arg == short {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("%s needs a value", long)
			}
			value := args[i+1]
			remaining := append([]string{}, args[:i]...)
			remaining = append(remaining, args[i+2:]...)
			return value, remaining, nil
		}
		if strings.HasPrefix(arg, long+"=") {
			remaining := append([]string{}, args[:i]...)
			remaining = append(remaining, args[i+1:]...)
			return strings.TrimPrefix(arg, long+"="), remaining, nil
		}
	}
	return fallback, args, nil
}

func takeBoolFlag(args []string, name string) (bool, []string, error) {
	long := "--" + name
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == long {
			remaining := append([]string{}, args[:i]...)
			remaining = append(remaining, args[i+1:]...)
			return true, remaining, nil
		}
		if strings.HasPrefix(arg, long+"=") {
			value := strings.TrimPrefix(arg, long+"=")
			remaining := append([]string{}, args[:i]...)
			remaining = append(remaining, args[i+1:]...)
			switch value {
			case "1", "true", "yes":
				return true, remaining, nil
			case "0", "false", "no":
				return false, remaining, nil
			default:
				return false, nil, fmt.Errorf("%s expects true or false", long)
			}
		}
	}
	return false, args, nil
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printReportPath(jsonOut bool, path string) {
	if jsonOut {
		fmt.Fprintf(os.Stderr, "report: %s\n", path)
		return
	}
	fmt.Printf("report: %s\n", path)
}

func expandArgs(args []string) []string {
	var out []string
	for _, arg := range args {
		out = append(out, expandOne(arg)...)
	}
	return out
}

func expandComma(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, expandOne(part)...)
	}
	return out
}

func expandOne(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err == nil && len(matches) > 0 {
		return matches
	}
	return []string{pattern}
}

type canonicalLogInput struct {
	path string
	info os.FileInfo
}

func resolveLogArgs(args []string) ([]string, error) {
	if err := rejectUnknownOptions(args); err != nil {
		return nil, err
	}
	return canonicalizeLogInputs(expandArgs(args))
}

func rejectUnexpectedArgs(args []string) error {
	if err := rejectUnknownOptions(args); err != nil {
		return err
	}
	if len(args) > 0 {
		return fmt.Errorf("unexpected argument %s", args[0])
	}
	return nil
}

func rejectUnknownOptions(args []string) error {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("unknown option %s", arg)
		}
	}
	return nil
}

func resolveLogComma(raw string) ([]string, error) {
	return canonicalizeLogInputs(expandComma(raw))
}

func canonicalizeLogInputs(paths []string) ([]string, error) {
	return canonicalizeFileInputs(paths, "log")
}

func canonicalizeFileInputs(paths []string, kind string) ([]string, error) {
	resolved := make([]canonicalLogInput, 0, len(paths))
	for _, input := range paths {
		absolute, err := filepath.Abs(filepath.Clean(input))
		if err != nil {
			return nil, fmt.Errorf("resolve %s path %q: %w", kind, input, err)
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, fmt.Errorf("resolve %s path %q: %w", kind, input, err)
		}
		canonical = filepath.Clean(canonical)
		info, err := os.Stat(canonical)
		if err != nil {
			return nil, fmt.Errorf("stat %s path %q: %w", kind, canonical, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s input %q is not a regular file", kind, canonical)
		}
		duplicate := false
		for _, existing := range resolved {
			if os.SameFile(existing.info, info) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		resolved = append(resolved, canonicalLogInput{path: canonical, info: info})
	}
	out := make([]string, len(resolved))
	for index := range resolved {
		out[index] = resolved[index].path
	}
	return out, nil
}

func rejectLogInputOverlap(leftName string, left []string, rightName string, right []string) error {
	leftFiles := make([]canonicalLogInput, 0, len(left))
	for _, path := range left {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s log %q: %w", leftName, path, err)
		}
		leftFiles = append(leftFiles, canonicalLogInput{path: path, info: info})
	}
	for _, path := range right {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s log %q: %w", rightName, path, err)
		}
		for _, candidate := range leftFiles {
			if os.SameFile(candidate.info, info) {
				return fmt.Errorf(
					"%s and %s log sets overlap: %q and %q refer to the same file; use independent runs for comparison",
					leftName,
					rightName,
					candidate.path,
					path,
				)
			}
		}
	}
	return nil
}
