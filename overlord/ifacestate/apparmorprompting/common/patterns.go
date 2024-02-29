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
	problematicChars = `,*{}\[\]\\`
	// A single safe or escaped unsafe char in a path
	safePathChar = fmt.Sprintf(`([^/%s]|\\[%s])`, problematicChars, problematicChars)
	// Some path components which may contain or begin with but not end with '/'
	optionalPathComponents = fmt.Sprintf(`(%s*(/%s+)*)`, safePathChar, safePathChar)

	// The following defines a "base path" which is combined with one of the
	// suffixes defined below to form a path pattern. The base path must not
	// end in a '/' character, and the base path pattern must be enclosed in
	// parentheses.
	basePathPattern = fmt.Sprintf(`((/%s+)+)`, safePathChar)

	// The following are the allowed path pattern suffixes. Complete valid path
	// patterns are a base path, which cannot contain wildcards, groups, or
	// character classes, followed by either one of these suffixes or a group
	// over multiple suffixes. In the latter case, each suffix in the group may
	// be preceded by one or more path components, similar to the base path.
	// The suffixes are given in rough order of precedence, though precedence
	// is determined by the length of the base path, followed by the length of
	// the next explicit path component before the following '*' (if any), and
	// so on. The first time a path component in one pattern is longer than the
	// corresponding path component in the other pattern, the former has
	// precedence over the latter.
	allowableSuffixPatterns = []string{
		// Suffixes with exact match have highest precedence
		/*   alignment  */ `/?`,
		// Suffixes with a single '*' character prioritize base pattern length, then suffix length
		fmt.Sprintf( /* */ `/?\*%s/?`, optionalPathComponents), // single '*' anywhere
		// Suffixes with '/**/' substrings prioritize base pattern length,
		// followed by pattern length between '/**/' and next '*', if any.
		fmt.Sprintf(`/\*\*%s/?`, basePathPattern),                               // '/**/' followed by more exact path
		fmt.Sprintf(`/\*\*%s/?\*%s/?`, basePathPattern, optionalPathComponents), // '/**/[^*]' followed by more path with '*' anywhere
		fmt.Sprintf(`/\*\*/\*(/|%s)%s/?`, safePathChar, optionalPathComponents), // '/**/*' followed by path components
		/* align */ `/\*\*/?`,
	}

	// All of the following patterns must begin with '(' and end with ')'.
	anySuffixPattern          = fmt.Sprintf(`(%s)`, strings.Join(allowableSuffixPatterns, "|"))
	anySuffixesInGroupPattern = fmt.Sprintf(`(\{%s%s(,%s%s)+\})`, optionalPathComponents, anySuffixPattern, optionalPathComponents, anySuffixPattern)
	// The following is the regexp which all client-provided path patterns must match
	allowablePathPatternRegexp = regexp.MustCompile(fmt.Sprintf(`^(%s?(%s|%s))$`, basePathPattern, anySuffixPattern, anySuffixesInGroupPattern))

	patternPrecedenceRegexps = buildPrecedenceRegexps()
)

func buildPrecedenceRegexps() []*regexp.Regexp {
	precedenceRegexps := make([]*regexp.Regexp, 0, len(allowableSuffixPatterns)+1)
	precedenceRegexps = append(precedenceRegexps, regexp.MustCompile(`^/$`))
	for _, suffix := range allowableSuffixPatterns {
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
				return "", fmt.Errorf("multiple highest-precedence patterns: %s and %s", patternLongestRemaining, pattern)
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
