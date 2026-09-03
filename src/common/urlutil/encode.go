// Package urlutil provides the shared URL construction, encoding, and safe
// remote-image fetching helpers required by PART 32 (URL Encoding) and
// PART 16 (Image Sources / Remote URL Fetching).
package urlutil

import (
	"net/url"
	"strings"
)

// BuildAPIURL constructs a properly encoded API URL.
// ALWAYS use this function - NEVER fmt.Sprintf with user input.
func BuildAPIURL(baseURL, path string, pathParams map[string]string, queryParams map[string]string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}

	// Substitute {name} placeholders with path-escaped values so a user-supplied
	// id can never introduce an extra path segment or a query string.
	encodedPath := path
	for key, value := range pathParams {
		placeholder := "{" + key + "}"
		encodedPath = strings.ReplaceAll(encodedPath, placeholder, url.PathEscape(value))
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + encodedPath

	if len(queryParams) > 0 {
		q := u.Query()
		for key, value := range queryParams {
			q.Set(key, value)
		}
		u.RawQuery = q.Encode()
	}

	return u.String()
}

// EncodePathSegment encodes a single path segment.
// Use for: slugs, resource IDs, filenames.
func EncodePathSegment(segment string) string {
	return url.PathEscape(segment)
}

// EncodeQueryValue encodes a query parameter value.
// Use for: search terms, filter values, pagination.
func EncodeQueryValue(value string) string {
	return url.QueryEscape(value)
}

// BuildQueryString builds an encoded query string from a map.
func BuildQueryString(params map[string]string) string {
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	return values.Encode()
}
