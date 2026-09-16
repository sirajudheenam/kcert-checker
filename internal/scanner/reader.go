package scanner

import "context"

// FileReader is the interface used by the Scanner to read files from containers.
// ContainerReader implements this interface; tests can provide a fake.
type FileReader interface {
	ReadFile(ctx context.Context, namespace, pod, container, path string) ([]byte, error)
}
