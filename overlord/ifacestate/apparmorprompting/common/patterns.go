package common

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	doublestar "github.com/bmatcuk/doublestar/v4"
)

var ErrNoPatterns = errors.New("no patterns given, cannot establish precedence")

var (
	// The following must be escaped if used as literals in a path pattern:
	problematicChars = `{}\[\]\?`
	// A single safe or escaped unsafe char in a path
	safePathChar = fmt.Sprintf(`([^%s]|\\[%s])`, problematicChars, problematicChars)

	// The following matches valid path patterns
	allowablePathPattern       = fmt.Sprintf(`^/%s*([^\\]?{%s*(,%s*)+})?$`, safePathChar, safePathChar, safePathChar)
	allowablePathPatternRegexp = regexp.MustCompile(allowablePathPattern)
)

// Expands a group, if it exists, in the path pattern, and creates a new
// string for every option in that group.
func ExpandPathPattern(pattern string) ([]string, error) {
	errPrefix := "invalid path pattern"
	var basePattern string
	groupStrings := make([]string, 0, strings.Count(pattern, ",")+1)
	sawGroup := false
	var currGroupStart int
	index := 0
	for index < len(pattern) {
		switch pattern[index] {
		case '\\':
			index += 1
			if index == len(pattern) {
				return nil, fmt.Errorf(`%s: trailing non-escaping '\' character: %q`, errPrefix, pattern)
			}
		case '{':
			if sawGroup {
				return nil, fmt.Errorf(`%s: multiple unescaped '{' characters: %q`, errPrefix, pattern)
			}
			if index == len(pattern)-1 {
				return nil, fmt.Errorf(`%s: trailing unescaped '{' character: %q`, errPrefix, pattern)
			}
			basePattern = pattern[:index]
			sawGroup = true
			currGroupStart = index + 1
		case '}':
			if !sawGroup || currGroupStart == -1 {
				return nil, fmt.Errorf(`%s: unmatched '}' character: %q`, errPrefix, pattern)
			}
			if index != len(pattern)-1 {
				return nil, fmt.Errorf(`%s: characters after group closed by '}': %s`, errPrefix, pattern)
			}
			currGroup := pattern[currGroupStart:index]
			groupStrings = append(groupStrings, currGroup)
			currGroupStart = -1
		case ',':
			currGroup := pattern[currGroupStart:index]
			groupStrings = append(groupStrings, currGroup)
			currGroupStart = index + 1
		}
		index += 1
	}
	if !sawGroup {
		return []string{trimDuplicates(pattern)}, nil
	}
	if currGroupStart != -1 {
		return nil, fmt.Errorf(`%s: unmatched '{' character: %q`, errPrefix, pattern)
	}
	expanded := make([]string, len(groupStrings))
	for i, str := range groupStrings {
		combined := basePattern + str
		expanded[i] = trimDuplicates(combined)
	}
	return expanded, nil
}

var (
	duplicateSlashes   = regexp.MustCompile(`(^|[^\\])/+`)
	trailingDoublestar = regexp.MustCompile(`([^/\\])\*\*`)
	leadingDoublestar  = regexp.MustCompile(`([^\\])\*\*([^/])`)
)

func trimDuplicates(pattern string) string {
	pattern = duplicateSlashes.ReplaceAllString(pattern, `${1}/`)
	pattern = trailingDoublestar.ReplaceAllString(pattern, `${1}*`)
	pattern = leadingDoublestar.ReplaceAllString(pattern, `${1}*${2}`)
	return pattern
}

// Determines which of the given path patterns is the most specific (top priority).
//
// Assumes that all of the given patterns satisfy ValidatePathPattern(), so this
// is not verified as part of this function. Additionally, also assumes that the
// patterns have been previously expanded using ExpandPathPattern(), so there
// are no groups in any of the patterns.
//
// For patterns ending in /** or file extensions, multiple patterns may match
// a suffix of the same precedence. In this case, since there are no groups or
// internal wildcard characters, the pattern with the longest base path must
// have the highest precedence, with the longest total pattern length breaking
// ties.
// For example:
// - /foo/bar/** has higher precedence than /foo/**
// - /foo/bar/*.gz has higher precedence than /foo/*.tar.gz
// - /foo/bar/**/*.tar.gz has higher precedence than /foo/bar/**/*.gz
func GetHighestPrecedencePattern(patterns []string) (string, error) {
	if len(patterns) == 0 {
		return "", ErrNoPatterns
	}
	remainingPatterns := make(map[string]string, len(patterns))
	for _, pattern := range patterns {
		remainingPatterns[pattern] = pattern
	}
	for len(remainingPatterns) > 0 {
		nextRemaining := make(map[string]string, len(remainingPatterns))
		longest := 0 // Set to -1 when a pattern with no remaining '*' is found
		for pattern, remaining := range remainingPatterns {
			index := strings.Index(remaining, "*")
			if longest == -1 {
				if index == -1 {
					nextRemaining[pattern] = remaining
				}
				continue
			}
			if index == -1 || index > longest {
				nextRemaining = make(map[string]string, len(remainingPatterns))
				longest = index
			} else if index < longest {
				continue
			}
			nextRemaining[pattern] = remaining[index+1:]
		}
		if len(nextRemaining) == 1 {
			for pattern := range nextRemaining {
				return pattern, nil
			}
		}
		remainingPatterns = nextRemaining
		if longest != -1 {
			continue
		}
		// nextRemaining only contains remaining which have no '*' characters.
		// Choose the pattern with the longest remaining.
		patternLongestRemaining := ""
		for pattern, remaining := range nextRemaining {
			if len(remaining) < longest {
				continue
			}
			if len(remaining) == longest {
				// Impossible, since patterns must be duplicates but were in map
				return "", fmt.Errorf("internal error: multiple highest-precedence patterns: %s and %s", patternLongestRemaining, pattern)
			}
			patternLongestRemaining = pattern
			longest = len(remaining)
		}
		return patternLongestRemaining, nil
	}
	// This should not occur
	return "", fmt.Errorf("internal error: ran out of path patterns")
}

// ValidatePathPattern returns nil if the pattern is valid, otherwise an error.
func ValidatePathPattern(pattern string) error {
	if !doublestar.ValidatePattern(pattern) {
		return fmt.Errorf("invalid path pattern: %q", pattern)
	}
	if pattern == "" || pattern[0] != '/' {
		return fmt.Errorf("invalid path pattern: must start with '/': %q", pattern)
	}
	if !allowablePathPatternRegexp.MatchString(pattern) {
		return fmt.Errorf("invalid path pattern: cannot contain unescaped '[', ']', or '?',  or >1 unescaped '{' or '}': %q", pattern)
	}
	return nil
}

func StripTrailingSlashes(path string) string {
	for path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	return path
}

// PathPatternMatches returns true if the given pattern matches the given path.
//
// The pattern should not contain groups, and should likely have been an output
// of ExpandPathPattern.
//
// The doublestar package has special cases for patterns ending in `/**`, `**/`,
// and `/**/`: `/foo/**`, and `/foo/**/` both match `/foo`, but not `/foo/`.
//
// Since paths to directories are received with trailing slashes, we want to
// ensure that patterns without trailing slashes match paths with trailing
// slashes. However, patterns with trailing slashes should not match paths
// without trailing slashes.
func PathPatternMatches(pattern string, path string) (bool, error) {
	matched, err := doublestar.Match(pattern, path)
	if err != nil {
		return false, err
	}
	if matched {
		return true, nil
	}
	patternSlash := doublestarSuffix.ReplaceAllString(pattern, `/`)
	return doublestar.Match(patternSlash, path)
}

var doublestarSuffix = regexp.MustCompile(`(/\*\*)?/?$`)
