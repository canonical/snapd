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
	// The following are the allowed path pattern suffixes, in order of precedence.
	// Complete valid path patterns are a base path, which cannot contain wildcards,
	// groups, or character classes, followed by either one of these suffixes or a
	// group over multiple suffixes. In the latter case, each suffix in the group
	// may be preceded by one or more path components, similar to the base path.
	allowableSuffixPatternsByPrecedence = []string{
		``,                 //           (no suffix, pattern is exact match)
		`/\*(\.\w+)+`,      // /*.ext    (any file matching given extension in base path directory)
		`/\*\*/\*(\.\w+)+`, // /**/*.ext (any file matching given extension in any subdirectory of base path)
		`/\.\*`,            // /.*       (dotfiles in base path directory)
		`/\*\*/\.\*`,       // /**/.*    (dotfiles in any subdirectory of base path)
		`/\*`,              // /*        (any file in base path directory)
		`/\*\*`,            // /**       (any file in any subdirectory of base path)
	}

	problematicChars = `,*{}\[\]\\`
	// All of the following patterns must begin with '(' and end with ')'.
	// The above problematic chars must be escaped if used as literals in a path pattern:
	safePathChar              = fmt.Sprintf(`([^/%s]|\\[%s])`, problematicChars, problematicChars)
	basePathPattern           = fmt.Sprintf(`((/%s+)*)`, safePathChar)
	anySuffixPattern          = fmt.Sprintf(`(%s)`, strings.Join(allowableSuffixPatternsByPrecedence, "|"))
	anySuffixesInGroupPattern = fmt.Sprintf(`(\{%s%s(,%s%s)+\})`, basePathPattern, anySuffixPattern, basePathPattern, anySuffixPattern)
	// The following is the regexp which all client-provided path patterns must match
	allowablePathPatternRegexp = regexp.MustCompile(fmt.Sprintf(`^(/|%s(%s|%s))$`, basePathPattern, anySuffixPattern, anySuffixesInGroupPattern))

	patternPrecedenceRegexps = buildPrecedenceRegexps()
)

func buildPrecedenceRegexps() []*regexp.Regexp {
	precedenceRegexps := make([]*regexp.Regexp, 0, len(allowableSuffixPatternsByPrecedence)+1)
	precedenceRegexps = append(precedenceRegexps, regexp.MustCompile(`^/$`))
	for _, suffix := range allowableSuffixPatternsByPrecedence {
		re := regexp.MustCompile(fmt.Sprintf(`^%s%s$`, basePathPattern, suffix))
		precedenceRegexps = append(precedenceRegexps, re)
	}
	return precedenceRegexps
}

// Expands a group, if it exists, in the path pattern, and creates a new
// string for every option in that group.
func ExpandPathPattern(pattern string) ([]string, error) {
	errPrefix := "invalid path pattern"
	var basePattern string
	groupStrings := make([]string, 0, strings.Count(pattern, ",")+1)
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
			if basePattern != "" {
				return nil, fmt.Errorf(`%s: multiple unescaped '{' characters: %q`, errPrefix, pattern)
			}
			if index == len(pattern)-1 {
				return nil, fmt.Errorf(`%s: trailing unescaped '{' character: %q`, errPrefix, pattern)
			}
			basePattern = pattern[:index]
			currGroupStart = index + 1
		case '}':
			if basePattern == "" {
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
	if basePattern == "" {
		return []string{pattern}, nil
	}
	if currGroupStart != -1 {
		return nil, fmt.Errorf(`%s: unmatched '{' character: %q`, errPrefix, pattern)
	}
	expanded := make([]string, len(groupStrings))
	for i, str := range groupStrings {
		expanded[i] = basePattern + str
	}
	return expanded, nil
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
	highestPrecedence := len(patternPrecedenceRegexps)
	highestPrecedencePatterns := make(map[string]string)
PATTERN_LOOP:
	for _, pattern := range patterns {
		for i, re := range patternPrecedenceRegexps {
			matches := re.FindStringSubmatch(pattern)
			if matches == nil {
				continue
			}
			if i < highestPrecedence {
				highestPrecedence = i
				highestPrecedencePatterns = make(map[string]string)
			}
			if i == highestPrecedence {
				matchedBasePath := matches[1]
				highestPrecedencePatterns[pattern] = matchedBasePath
			}
			continue PATTERN_LOOP
		}
		return "", fmt.Errorf("pattern does not match any suffix, cannot establish precedence: %s", pattern)
	}
	if len(highestPrecedencePatterns) == 0 {
		// Should never occur
		return "", ErrNoPatterns
	}
	longestPattern := ""
	longestBasePath := ""
	for pattern, basePath := range highestPrecedencePatterns {
		if len(basePath) < len(longestBasePath) {
			continue
		}
		if len(basePath) == len(longestBasePath) {
			if len(pattern) < len(longestPattern) {
				continue
			}
			if len(pattern) == len(longestPattern) {
				// Should not be able to have two paths with the same
				// precedence and equal length base path and full pattern,
				// since that implies they must have the same suffix, so the
				// patterns must be identical, which should be impossible.
				return "", fmt.Errorf("multiple highest-precedence patterns with the same base path and length: %s and %s", longestPattern, pattern)
			}
		}
		longestPattern = pattern
		longestBasePath = basePath
	}
	return longestPattern, nil
}

// ValidatePathPattern returns nil if the pattern is valid, otherwise an error.
func ValidatePathPattern(pattern string) error {
	if pattern == "" || !allowablePathPatternRegexp.MatchString(pattern) {
		return fmt.Errorf("invalid path pattern: %q", pattern)
	}
	return nil
}

func StripTrailingSlashes(path string) string {
	for path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	return path
}

func PathPatternMatches(pathPattern string, path string) (bool, error) {
	path = StripTrailingSlashes(path)
	matched, err := doublestar.Match(pathPattern, path)
	if err != nil {
		return false, err
	}
	if matched {
		return true, nil
	}
	return doublestar.Match(pathPattern, path+"/")
}
