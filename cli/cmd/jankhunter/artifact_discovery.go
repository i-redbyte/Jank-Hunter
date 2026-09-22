package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func discoverAndroidArtifactDirectory(projectRoots []string) string {
	var bundles []coherentAndroidArtifactBundle
	for _, root := range projectRoots {
		modulePattern := root
		for depth := 0; depth <= 4; depth++ {
			pattern := filepath.Join(modulePattern, "build", "generated", "jankhunter", "*")
			matches, _ := filepath.Glob(pattern)
			for _, match := range matches {
				if bundle, err := loadCoherentAndroidArtifactBundle(match); err == nil {
					bundles = append(bundles, bundle)
				}
			}
			modulePattern = filepath.Join(modulePattern, "*")
		}
	}
	if len(bundles) == 0 {
		return ""
	}
	sort.Slice(bundles, func(left, right int) bool {
		if bundles[left].lastModifiedAt.Equal(bundles[right].lastModifiedAt) {
			return bundles[left].directory < bundles[right].directory
		}
		return bundles[left].lastModifiedAt.After(bundles[right].lastModifiedAt)
	})
	return bundles[0].directory
}

func integratedProjectRoots() []string {
	roots := linkedStringSet{}
	if executable, err := os.Executable(); err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
			executable = resolved
		}
		binDirectory := filepath.Dir(executable)
		integrationDirectory := filepath.Dir(binDirectory)
		if filepath.Base(binDirectory) == "bin" && filepath.Base(integrationDirectory) == ".jankhunter" {
			roots.add(filepath.Dir(integrationDirectory))
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		for current, depth := cwd, 0; depth < 8; depth++ {
			candidate := filepath.Join(current, ".jankhunter", "bin", "jankhunter")
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				roots.add(current)
				break
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return roots.values
}

type coherentAndroidArtifactBundle struct {
	directory      string
	ownerMap       string
	classGraph     string
	diagnostics    string
	diCatalog      string
	lastModifiedAt time.Time
}

func loadCoherentAndroidArtifactBundle(directory string) (coherentAndroidArtifactBundle, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return coherentAndroidArtifactBundle{}, fmt.Errorf("resolve artifact directory %q: %w", directory, err)
	}
	bundle := coherentAndroidArtifactBundle{
		directory:  absolute,
		ownerMap:   filepath.Join(absolute, "owner-map.json"),
		classGraph: filepath.Join(absolute, "class-graph.jsonl"),
		diagnostics: filepath.Join(
			absolute,
			"instrumentation-diagnostics.jsonl",
		),
	}
	for _, required := range []struct {
		label string
		path  string
	}{
		{label: "owner-map.json", path: bundle.ownerMap},
		{label: "class-graph.jsonl", path: bundle.classGraph},
		{label: "instrumentation-diagnostics.jsonl", path: bundle.diagnostics},
	} {
		label := required.label
		path := required.path
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() || info.Size() == 0 {
			return coherentAndroidArtifactBundle{}, fmt.Errorf(
				"invalid Jank Hunter artifact directory %q: required %s is missing or empty",
				directory,
				label,
			)
		}
		if info.ModTime().After(bundle.lastModifiedAt) {
			bundle.lastModifiedAt = info.ModTime()
		}
	}
	diCatalog := filepath.Join(absolute, "di-catalog.jsonl")
	if info, statErr := os.Stat(diCatalog); statErr == nil && !info.IsDir() && info.Size() > 0 {
		bundle.diCatalog = diCatalog
		if info.ModTime().After(bundle.lastModifiedAt) {
			bundle.lastModifiedAt = info.ModTime()
		}
	}
	return bundle, nil
}
