package scanner

import "context"

// FileReader is the interface used by the Scanner to read files from containers.
// ContainerReader implements this interface; tests can provide a fake.
//
// ListFiles expands a glob pattern (e.g. /etc/certs/*) into concrete paths by
// running `ls -1 <pattern>` inside the container. ReadFile then reads each path.
// Keeping the two operations separate lets tests control expansion independently
// of file content.
type FileReader interface {
	ListFiles(ctx context.Context, namespace, pod, container, pattern string) ([]string, error)
	ReadFile(ctx context.Context, namespace, pod, container, path string) ([]byte, error)
}
