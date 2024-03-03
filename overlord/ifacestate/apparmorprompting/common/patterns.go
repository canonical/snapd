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
	problematicChars = `\[\]\?`
	// A single safe or escaped unsafe char in a path
	safePathChar = fmt.Sprintf(`([^%s]|\\[%s])`, problematicChars, problematicChars)

	// The following matches valid path patterns
	allowablePathPattern       = fmt.Sprintf(`^/%s*$`, safePathChar)
	allowablePathPatternRegexp = regexp.MustCompile(allowablePathPattern)
)

// Expands all groups in the given path pattern. Groups are enclosed by '{' '}'.
// Returns a list of all the expanded path patterns, or an error if the given
// pattern is invalid.
func ExpandPathPattern(pattern string) ([]string, error) {
	patternLen := len(pattern)
	expanded := defaultPrefixes
	currLiteralStart := 0
	for i := 0; i < patternLen; i++ {
		if pattern[i] == '\\' {
			i += 1
			if i == patternLen {
				return nil, fmt.Errorf(`invalid path pattern: trailing unescaped '\' character: %q`, pattern)
			}
			continue
		}
		if pattern[i] == '}' {
			return nil, fmt.Errorf(`invalid path pattern: unmatched '}' character: %q`, pattern)
		}
		if pattern[i] != '{' {
			continue
		}
		infix := pattern[currLiteralStart:i]
		groupExpanded, groupEnd, err := expandPathPatternFromIndex(pattern, i+1, patternLen)
		if err != nil {
			return nil, err
		}
		newExpanded := make([]string, 0, len(expanded)*len(groupExpanded))
		for _, prefix := range expanded {
			for _, suffix := range groupExpanded {
				newExpanded = append(newExpanded, prefix+infix+suffix)
			}
		}
		expanded = newExpanded
		currLiteralStart = groupEnd + 1
		i = groupEnd // let for loop increment to index after '}'
	}
	if len(expanded) == 1 && expanded[0] == "" {
		// Didn't expand any groups, so return whole pattern.
		return []string{cleanPattern(pattern)}, nil
	}
	// Append trailing literal string, if any, to all previously-expanded
	// patterns, and clean the resulting patterns.
	newExpanded := make([]string, 0, len(expanded))
	suffix := pattern[currLiteralStart:patternLen]
	for _, prefix := range expanded {
		newExpanded = append(newExpanded, cleanPattern(prefix+suffix))
	}
	expanded = newExpanded
	return expanded, nil
}

var defaultPrefixes = []string{""}

// Expands the contents of a group in the given path pattern, beginning at the
// given index, until a '}' is seen. The given index should be the index of the
// first character after the opening '{' of the group. Returns the list of
// expanded strings, as well as the index of the closing '}' character.
// Whenever a ',' character is encountered, cuts off the current sub-pattern
// and begins a new one. Any '\'-escaped '{', ',', and '}' characters are
// treated as literals. If the pattern terminates before a non-escaped '}' is
// seen, returns an error.
func expandPathPatternFromIndex(pattern string, index, patternLen int) (expanded []string, end int, err error) {
	// Record total list of expanded patterns, to which other lists are appended
	expanded = []string{}
	// Within the current group option, record the current list of previously-
	// expanded prefixes, and the start index of the subpattern following the
	// most recent group.
	currPrefixes := defaultPrefixes
	currSubpatternStart := index
	for i := index; i < patternLen; i++ {
		if pattern[i] == '\\' {
			i += 1
			if i == patternLen {
				return nil, 0, fmt.Errorf(`invalid path pattern: trailing unescaped '\' character: %q`, pattern)
			}
			continue
		}
		if pattern[i] == '{' {
			infix := pattern[currSubpatternStart:i]
			groupExpanded, groupEnd, err := expandPathPatternFromIndex(pattern, i+1, patternLen)
			if err != nil {
				return nil, 0, err
			}
			newPrefixes := make([]string, 0, len(currPrefixes)*len(groupExpanded))
			for _, prefix := range currPrefixes {
				for _, suffix := range groupExpanded {
					newPrefixes = append(newPrefixes, prefix+infix+suffix)
				}
			}
			currPrefixes = newPrefixes
			currSubpatternStart = groupEnd + 1
			i = groupEnd // let for loop increment to index after '}'
			continue
		}
		if pattern[i] == ',' || pattern[i] == '}' {
			suffix := pattern[currSubpatternStart:i]
			newExpanded := make([]string, len(expanded), len(expanded)+len(currPrefixes))
			copy(newExpanded, expanded)
			for _, prefix := range currPrefixes {
				newExpanded = append(newExpanded, prefix+suffix)
			}
			expanded = newExpanded
			currPrefixes = defaultPrefixes
			currSubpatternStart = i + 1
		}
		if pattern[i] == '}' {
			return expanded, i, nil
		}
	}
	return nil, 0, fmt.Errorf(`invalid path pattern: unmatched '{' character: %q`, pattern)
}

var (
	duplicateSlashes    = regexp.MustCompile(`(^|[^\\])/+`)
	charsDoublestar     = regexp.MustCompile(`([^/\\])\*\*`)
	doublestarChars     = regexp.MustCompile(`([^\\])\*\*([^/])`)
	duplicateDoublestar = regexp.MustCompile(`/\*\*(/\*\*)+`)
)

func cleanPattern(pattern string) string {
	pattern = duplicateSlashes.ReplaceAllString(pattern, `${1}/`)
	pattern = charsDoublestar.ReplaceAllString(pattern, `${1}*`)
	pattern = doublestarChars.ReplaceAllString(pattern, `${1}*${2}`)
	pattern = duplicateDoublestar.ReplaceAllString(pattern, `/**`)
	if strings.HasSuffix(pattern, "/**/*") {
		// Strip trailing "/*" from suffix
		return pattern[:len(pattern)-2]
	}
	return pattern
}

type priorityType int

const (
	worstPriority priorityType = iota
	priorityGlobDoublestar
	priorityTerminalDoublestar
	priorityDoublestar
	priorityGlob
	prioritySinglestar
	priorityTerminated
	priorityLiteral
)

type nextPatternsContainer struct {
	currPriority    priorityType
	nextPatternsMap map[string]int
}

func (np *nextPatternsContainer) addWithPriority(priority priorityType, pattern string, e int) {
	if priority < np.currPriority {
		return
	}
	if priority > np.currPriority {
		np.nextPatternsMap = make(map[string]int)
		np.currPriority = priority
	}
	np.nextPatternsMap[pattern] = e
}

func (np *nextPatternsContainer) nextPatterns() map[string]int {
	return np.nextPatternsMap
}

// GetHighestPrecedencePattern determines which of the given path patterns is
// the most specific.
//
// Assumes that all of the given patterns satisfy ValidatePathPattern(), so this
// is not verified as part of this function. Additionally, also assumes that the
// patterns have been previously expanded using ExpandPathPattern(), so there
// are no groups in any of the patterns.
//
// Below are some sample patterns, in order of precedence, though precedence is
// only guaranteed between two patterns which may match the same path:
//
//	# literals
//	- /foo/bar/baz
//	- /foo/bar/
//	# terminated
//	- /foo/bar
//	# singlestars
//	- /foo/bar/*/baz
//	- /foo/bar/*/
//	- /foo/bar/*/*baz
//	- /foo/bar/*/*
//	- /foo/bar/*
//	- /foo/bar/*/**
//	# glob
//	- /foo/bar*baz
//	- /foo/bar*/baz
//	- /foo/bar*/baz/**
//	- /foo/bar*/
//	- /foo/bar*/*baz
//	- /foo/bar*/*/baz
//	- /foo/bar*/*/
//	- /foo/bar*/*
//	- /foo/bar*
//	# doublestars
//	- /foo/bar/**/baz
//	- /foo/bar/**/*baz/
//	- /foo/bar/**/*baz
//	# terminal doublestar
//	- /foo/bar/**/        # These are tough... usually, /foo/bar/**/ would have precedence over
//	- /foo/bar/**/*       # precedence over /foo/bar/**/*baz, but in this case,
//	- /foo/bar/**         # the trailing *baz adds more specificity.
//	# glob with immediate doublestar
//	- /foo/bar*/**/baz
//	- /foo/bar*/**/
//	- /foo/bar*/**
func GetHighestPrecedencePattern(patterns []string) (string, error) {
	if len(patterns) == 0 {
		return "", ErrNoPatterns
	}
	// Map pattern to number of escaped characters which have been seen
	remainingPatterns := make(map[string]int, len(patterns))
	for _, pattern := range patterns {
		remainingPatterns[pattern] = 0
	}
	// Loop over index into each pattern until only one pattern left
	for i := 0; len(remainingPatterns) > 1; i++ {
		nextPatterns := nextPatternsContainer{}
		for pattern, e := range remainingPatterns {
			// For each pattern, e is number of escaped chars, thus the number
			// which should be added to the index to compare equivalent indices
			// into all patterns
			patternLen := len(pattern)
			if i+e == patternLen {
				nextPatterns.addWithPriority(priorityTerminated, pattern, e)
				continue
			}
			// Check for '*' before '\\', since "\\*" is literal '*'
			if pattern[i+e] == '*' {
				if patternLen >= i+e+4 && pattern[i+e+1:i+e+4] == "/**" {
					// Next parts of pattern are "*/**"
					nextPatterns.addWithPriority(priorityGlobDoublestar, pattern, e)
					continue
				}
				nextPatterns.addWithPriority(priorityGlob, pattern, e)
				continue
			}
			if pattern[i+e] == '\\' {
				e += 1
				if patternLen == i+e {
					return "", fmt.Errorf(`invalid path pattern: trailing '\' character: %q`, pattern)
				}
			}
			// Can safely check for '/' after '\\', since it is '/' either way
			if pattern[i+e] != '/' || patternLen < i+e+2 || pattern[i+e+1] != '*' {
				// Next parts of pattern are not "/*" or "/**"
				nextPatterns.addWithPriority(priorityLiteral, pattern, e)
				continue
			}
			// pattern[i+e:i+e+2] must be "/*"
			if patternLen < i+e+3 || pattern[i+e+2] != '*' {
				// pattern[i+e:i+e+3] must not be "/**"
				nextPatterns.addWithPriority(prioritySinglestar, pattern, e)
				continue
			}
			if patternLen == i+e+3 || (pattern[i+e+3] == '/' && (patternLen == i+e+4 || (patternLen == i+e+5 && pattern[i+e+4] == '*'))) {
				// pattern[1+e:i+e+3 must terminate with "/**" or "/**/".
				// Doesn't matter if pattern ends in "/**/*", that will be
				// caught in the usual case later.
				nextPatterns.addWithPriority(priorityTerminalDoublestar, pattern, e)
				continue
			}
			// pattern has non-terminal "/**" next
			nextPatterns.addWithPriority(priorityDoublestar, pattern, e)
		}
		remainingPatterns = nextPatterns.nextPatterns()
	}
	p := ""
	for pattern := range remainingPatterns {
		p = pattern
	}
	return p, nil
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
		return fmt.Errorf("invalid path pattern: cannot contain unescaped '[', ']', or '?': %q", pattern)
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
