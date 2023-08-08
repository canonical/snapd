package common

import (
	"errors"
	"regexp"
	"strings"
)

var ErrInvalidPathPattern = errors.New("the given path pattern is not allowed")
var ErrNoPatterns = errors.New("no patterns given, cannot establish precedence")

var allowablePathPatternRegexp = regexp.MustCompile(`^(/|(/[^/*{}]+)*(/\*|(/\*\*)?(/\*\.[^/*{}]+)?)?)$`)

// Checks that the given path pattern is valid.  Returns nil if so, otherwise
// returns ErrInvalidPathPattern.
func ValidatePathPattern(pattern string) error {
	if !allowablePathPatternRegexp.MatchString(pattern) {
		return ErrInvalidPathPattern
	}
	return nil
}

// Determines which of the path patterns in the given patterns list is the
// most specific, and thus has the highest priority.  Assumes that all of the
// given patterns satisfy ValidatePathPattern(), so this is not verified as
// part of this function.
//
// Exact matches always have the highest priority.  Then, the pattern with the
// most specific file extension has priority.  If no matching patterns have
// file extensions (or if multiple share the most specific file extension),
// then the longest pattern (excluding trailing * wildcards) is the most
// specific.  Lastly, the priority order is: .../foo > .../foo/* > .../foo/**
func GetHighestPrecedencePattern(patterns []string) (string, error) {
	if len(patterns) == 0 {
		return "", ErrNoPatterns
	}
	// First find rules with extensions, if any exist -- these are most specific
	// longer file extensions are more specific than longer paths, so
	// /foo/bar/**/*.tar.gz is more specific than /foo/bar/baz/**/*.gz
	extensions := make(map[string][]string)
	for _, pattern := range patterns {
		if strings.Index(pattern, "*") == -1 {
			// Exact match, has highest precedence
			return pattern, nil
		}
		segments := strings.Split(pattern, "/")
		finalSegment := segments[len(segments)-1]
		extPrefix := "*."
		if !strings.HasPrefix(finalSegment, extPrefix) {
			continue
		}
		extension := finalSegment[len(extPrefix):]
		extensions[extension] = append(extensions[extension], pattern)
	}
	longestExtension := ""
	for extension, extPatterns := range extensions {
		if len(extension) > len(longestExtension) {
			longestExtension = extension
			patterns = extPatterns
		}
	}
	// Either patterns all have same extension, or patterns have no extension
	// (but possibly trailing /* or /**).
	// Prioritize longest patterns (excluding /** or /*).
	longestCleanedLength := 0
	longestCleanedPatterns := make([]string, 0)
	for _, pattern := range patterns {
		cleanedPattern := strings.ReplaceAll(pattern, "/**", "")
		cleanedPattern = strings.ReplaceAll(cleanedPattern, "/*", "")
		length := len(cleanedPattern)
		if length < longestCleanedLength {
			continue
		}
		if length > longestCleanedLength {
			longestCleanedLength = length
			longestCleanedPatterns = longestCleanedPatterns[:0] // clear but preserve allocated memory
		}
		longestCleanedPatterns = append(longestCleanedPatterns, pattern)
	}
	// longestCleanedPatterns is all the most-specific patterns that match.
	// Now, want to prioritize .../foo over .../foo/* over .../foo/**, so take shortest of these
	shortestPattern := longestCleanedPatterns[0]
	for _, pattern := range longestCleanedPatterns {
		if len(pattern) < len(shortestPattern) {
			shortestPattern = pattern
		}
	}
	return shortestPattern, nil
}
