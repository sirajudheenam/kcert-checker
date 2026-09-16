// Package filter provides glob-pattern matching helpers used by the scanner
// to skip namespaces, containers, secrets, and paths based on config exclusions.
package filter

import "path"

// matchesAny reports whether name matches any of the given exact names or
// glob patterns. Glob syntax follows filepath.Match (e.g. "openshift-*",
// "sh.helm.release.*").
// A match means the caller should EXCLUDE the resource — callers are named
// "Namespace", "Container" etc. and all return true to mean "skip this".
func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if name == p {
			return true
		}
		matched, err := path.Match(p, name)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// Namespace reports whether the given namespace name should be skipped.
func Namespace(name string, excludePatterns []string) bool {
	return matchesAny(name, excludePatterns)
}

// Container reports whether the given container name should be skipped.
func Container(name string, excludePatterns []string) bool {
	return matchesAny(name, excludePatterns)
}

// Secret reports whether the given secret name should be skipped.
func Secret(name string, excludePatterns []string) bool {
	return matchesAny(name, excludePatterns)
}

// Pod reports whether the given pod name should be skipped.
func Pod(name string, excludePatterns []string) bool {
	return matchesAny(name, excludePatterns)
}
