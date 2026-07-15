package analyze

import "fmt"

const (
	OwnerMapFormat                   = 4
	ClassGraphFormat                 = 1
	InstrumentationDiagnosticsFormat = 1
	DependencyInjectionCatalogFormat = 1
)

func validateArtifactFormat(path, artifact string, got, want int) error {
	if got == 0 {
		return fmt.Errorf("%s: missing %s format, expected %d", path, artifact, want)
	}
	if got != want {
		return fmt.Errorf("%s: unsupported %s format %d, cli supports %d", path, artifact, got, want)
	}
	return nil
}
