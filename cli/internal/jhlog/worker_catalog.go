package jhlog

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

const maxWorkerCatalogBytes = 128 * 1024

// Original build names describe pre-R8 candidates, not guaranteed installed runtime classes.
type WorkerCatalogEvent struct {
	Schema            uint64   `json:"schema"`
	Status            string   `json:"status"`
	Scope             string   `json:"scope,omitempty"`
	Namespace         string   `json:"namespace,omitempty"`
	DynamicModules    string   `json:"dynamic_modules,omitempty"`
	UnresolvedClasses uint64   `json:"unresolved_classes,omitempty"`
	OmittedWorkers    uint64   `json:"omitted_workers,omitempty"`
	Classes           []string `json:"classes,omitempty"`
}

func validateWorkerCatalog(c *WorkerCatalogEvent) error {
	if c == nil || c.Schema != 1 || len(c.Classes) > 4096 {
		return fmt.Errorf("unsupported worker catalog schema or size")
	}
	switch c.Status {
	case "available":
		namespace, err := hex.DecodeString(c.Namespace)
		if err != nil || len(namespace) != 16 || c.Scope != "application-and-packaged-dependencies-pre-r8" || c.DynamicModules != "outside-base-scope" {
			return fmt.Errorf("invalid worker catalog coverage")
		}
	case "missing", "unreadable", "oversized":
		if len(c.Classes) > 0 || c.Scope != "" || c.Namespace != "" || c.DynamicModules != "" || c.UnresolvedClasses != 0 || c.OmittedWorkers != 0 {
			return fmt.Errorf("unavailable worker catalog contains inventory")
		}
	default:
		return fmt.Errorf("unknown worker catalog status")
	}
	seen := make(map[string]bool, len(c.Classes))
	for _, name := range c.Classes {
		if name == "" || len(name) > 1024 || !utf8.ValidString(name) || seen[name] {
			return fmt.Errorf("invalid or duplicate worker class")
		}
		seen[name] = true
	}
	return nil
}

func (e eventPayloadEncoder) encodeWorkerCatalog(c *WorkerCatalogEvent) error {
	if err := validateWorkerCatalog(c); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(data) > maxWorkerCatalogBytes {
		return fmt.Errorf("worker catalog exceeds byte limit")
	}
	if err = e.writeValues(uint64(len(data))); err != nil {
		return err
	}
	return writeAll(e.writer, data)
}

func decodeWorkerCatalog(r *recordReader, event *Event) error {
	data, err := readThreadString(r, maxWorkerCatalogBytes)
	if err != nil {
		return err
	}
	var catalog WorkerCatalogEvent
	if err = json.Unmarshal([]byte(data), &catalog); err != nil {
		return fmt.Errorf("worker catalog: %w", err)
	}
	if err = validateWorkerCatalog(&catalog); err != nil {
		return err
	}
	event.DiagnosticEvent = &DiagnosticEvent{WorkerCatalog: &catalog}
	return nil
}
