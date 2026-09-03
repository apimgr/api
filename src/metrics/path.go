package metrics

import "regexp"

// uuidRegex matches canonical UUID formatting used in path segments.
var uuidRegex = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// numericIDRegex matches a purely-numeric path segment.
var numericIDRegex = regexp.MustCompile(`/\d+(?:/|$)`)

// NormalizePath replaces UUID and numeric-ID path segments with ":id" so
// per-path metric labels stay low cardinality, per AI.md PART 20's
// mandatory path normalization rule. Callers MUST normalize any raw request
// path before passing it to RecordRequest or using it as a metric label.
func NormalizePath(path string) string {
	path = uuidRegex.ReplaceAllString(path, ":id")
	path = numericIDRegex.ReplaceAllString(path, "/:id/")
	if len(path) > 1 {
		for len(path) > 1 && path[len(path)-1] == '/' {
			path = path[:len(path)-1]
		}
	}
	return path
}
