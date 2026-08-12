package jhlog

// CanonicalEventStream is the storage-neutral CLI input boundary. Storage
// adapters decode into canonical events and preserve their source, position,
// producer time and quality metadata in StreamResult. A future ring adapter
// implements this interface; analyzers do not depend on its physical format.
type CanonicalEventStream interface {
	Stream(EventHandler) (StreamResult, error)
}

type FileEventStream struct {
	Path string
}

func (stream FileEventStream) Stream(handle EventHandler) (StreamResult, error) {
	return StreamFileWithResult(stream.Path, handle)
}
