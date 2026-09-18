package retrace

import (
	"fmt"
	"os"
	"path/filepath"
)

// Discover locates bundled files only; it never downloads code or starts Java.
func Discover() (Backend, error) {
	if directory := os.Getenv("JANK_HUNTER_RETRACE_HOME"); directory != "" {
		return fromDirectory(directory)
	}
	executable, err := os.Executable()
	if err != nil {
		return Backend{}, fmt.Errorf("locate offline Retrace bundle: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	directory := filepath.Dir(executable)
	for _, candidate := range []string{filepath.Join(directory, "jankhunter-retrace"), filepath.Join(directory, "..", "lib", "jankhunter", "retrace")} {
		if backend, err := fromDirectory(candidate); err == nil {
			return backend, nil
		}
	}
	return Backend{}, fmt.Errorf("offline Retrace %s bundle missing; install the complete CLI archive or set JANK_HUNTER_RETRACE_HOME", Version)
}

func fromDirectory(directory string) (Backend, error) {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return Backend{}, err
	}
	backend := Backend{R8Jar: filepath.Join(directory, "r8lib-"+Version+".jar"), BridgeJar: filepath.Join(directory, "jankhunter-retrace.jar")}
	for _, path := range []string{backend.R8Jar, backend.BridgeJar} {
		info, err := os.Stat(path)
		if err != nil {
			return Backend{}, fmt.Errorf("offline Retrace bundle: %w", err)
		}
		if !info.Mode().IsRegular() {
			return Backend{}, fmt.Errorf("offline Retrace bundle is not a regular file: %s", path)
		}
	}
	return backend, nil
}
