// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package apparmor

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/strutil"
)

// For mocking in tests
var (
	osMkdirAll        = os.MkdirAll
	osutilAtomicWrite = osutil.AtomicWrite
)

// ValidateNoAppArmorRegexp will check that the given string does not
// contain AppArmor regular expressions (AARE), double quotes or \0.
// Note that to check the inverse of this, that is that a string has
// valid AARE, one should use interfaces/utils.NewPathPattern().
func ValidateNoAppArmorRegexp(s string) error {
	const AARE = `?*[]{}^"` + "\x00"

	if strings.ContainsAny(s, AARE) {
		return fmt.Errorf("%q contains a reserved apparmor char from %s", s, AARE)
	}
	return nil
}

func generateAAREExclusionPatternsSingle(excludePattern []rune, opts *AAREExclusionPatternsOptions) (string, error) {
	// no error checking of input as that was already done in
	// GenerateAAREExclusionPatterns

	// TODO: this logic could be combined with some of the more complex logic in
	// GenerateAAREExclusionPatternsGenericImpl but those loops are
	// subtly more complex so that is left for another time
	builder := &strings.Builder{}
	for i := 1; i < len(excludePattern); i++ {
		c := excludePattern[i]
		switch c {
		case '*':
			// skip this element, this is the regular expression
			continue
		case '/':
			// check if the previous element was a "*" in which case we
			// skip this one too
			if excludePattern[i-1] == '*' {
				continue
			}
		}
		res := fmt.Sprintf("%s%s[^%c]**%s\n", opts.Prefix, string(excludePattern[:i]), c, opts.Suffix)
		builder.WriteString(res)
	}
	return builder.String(), nil
}

type AAREExclusionPatternsOptions struct {
	// Prefix is a string to include on every line in the permutations before
	// the exclusion permutation itself.
	Prefix string
	// Suffix is a string to include on every line in the permutations after
	// the exclusion permutation itself.
	Suffix string

	// TODO: add options for generating non-filepaths like what we need for the
	// unconfined profile transition exclusion in snap-confine's profile as well
	// as an option for adding an extra character to the very first rule like we
	// need in the home interface
}

// GenerateAAREExclusionPatterns generates a series of valid AppArmor
// regular expression negation rules such that anything except the specific
// excludePatterns will match with the specified prefix and suffix rules. For
// example to allow reading any file except those matching /usr/*/foo, you would
// call this function with "/usr/*/foo" as the first argument, "" as the prefix
// and " r," as the suffix (the suffix being the main part of the read rule) and
// this function would return the following multi-line string with the relevant
// rules:
//
// /[^u]** r,
// /u[^s]** r,
// /us[^r]** r,
// /usr[^/]** r,
// /usr/*/[^f]** r,
// /usr/*/f[^o]** r,
// /usr/*/fo[^o]** r,
//
// This function only treats '*' specially in the string and does not handle any
// other alternations etc. that AARE may more generally support, and all
// patterns provided must be absolute filepaths that are at least 2 runes long.
//
// This function also works with multiple exclude patterns such as specifying to
// exclude "/usr/lib/snapd" and "/var/lib/snapd" with suffix " r," would yield:
//
// /[^uv]** r,
// /{u[^s],v[^a]}** r,
// /{us[^r],va[^r]}** r,
// /{usr[^/],var[^/]}** r,
// /{usr/[^l],var/[^l]}** r,
// /{usr/l[^i],var/l[^i]}** r,
// /{usr/li[^b],var/li[^b]}** r,
// /{usr/lib[^/],var/lib[^/]}** r,
// /{usr/lib/[^s],var/lib/[^s]}** r,
// /{usr/lib/s[^n],var/lib/s[^n]}** r,
// /{usr/lib/sn[^a],var/lib/sn[^a]}** r,
// /{usr/lib/sna[^p],var/lib/sna[^p]}** r,
// /{usr/lib/snap[^d],var/lib/snap[^d]}** r,
//
func GenerateAAREExclusionPatterns(excludePatterns []string, opts *AAREExclusionPatternsOptions) (string, error) {
	seen := map[string]bool{}
	runeSlices := make([][]rune, 0, len(excludePatterns))
	for _, patt := range excludePatterns {
		// check for duplicates
		if seen[patt] {
			return "", errors.New("exclude patterns contain duplicates")
		}
		seen[patt] = true

		// check if it is at least legnth 1
		if len(patt) < 2 {
			return "", errors.New("exclude patterns must be at least length 2")
		}

		// check that it starts as an absolute path
		if patt[0] != '/' {
			return "", errors.New("exclude patterns must be absolute filepaths")
		}

		// TODO: we use runes here because Go makes it easy but does AppArmor
		// actually understand UTF-8/Unicode properly? perhaps we should
		// validate that all runes in the pattern are supported by apparmor roo?

		// TODO: should we also validate that the only character in the pattern
		// from AARE is "*" ?

		runeSlices = append(runeSlices, []rune(patt))
	}
	if opts == nil {
		opts = &AAREExclusionPatternsOptions{}
	}
	return generateAAREExclusionPatternsGenericImpl(runeSlices, opts)
}

func generateAAREExclusionPatternsGenericImpl(excludePatterns [][]rune, opts *AAREExclusionPatternsOptions) (string, error) {
	switch len(excludePatterns) {
	case 0:
		return "", fmt.Errorf("no patterns provided")
	case 1:
		// single pattern generation is simpler and doesn't need to consider
		// generating alternatives like in /foo/{a[^b],c[^d]} etc. so we do that
		// case separately for easier reasoning about the generic case
		return generateAAREExclusionPatternsSingle(excludePatterns[0], opts)
	}

	// figure out the shortest and longest strings for setting loop ranges below
	shortestStrLen := len(excludePatterns[0])
	longestStrLen := len(excludePatterns[0])
	for _, patt := range excludePatterns {
		// save the shortest pattern length for computing the longest common substring below
		if len(patt) < shortestStrLen {
			shortestStrLen = len(patt)
		}
		if len(patt) > longestStrLen {
			longestStrLen = len(patt)
		}
	}

	builder := &strings.Builder{}

	// find the longest common prefix in all the patterns
	endCommonPrefix := 0

commonSubStrLoop:
	for i := 0; i < shortestStrLen; i++ {
		// ok to use 0 index here since by definition the first exclude
		// pattern's common prefix is common with all other patterns
		pattChar := excludePatterns[0][i]
		for _, patt := range excludePatterns[1:] {
			if patt[i] != pattChar {
				break commonSubStrLoop
			}
		}
		endCommonPrefix = i + 1
	}

	// for keeping track of the last rule for the common case before we get to
	// the overlapping cases
	var lastCommonPrefix string

	// iterate up to the longest string we have - saving the iteration variable
	// for the case when we get past the common substrings and do a separate
	// kind of loop below, mainly just for readability to avoid nesting things
	// too deeply
	var i int
charLoop:
	for i = 0; i < longestStrLen; i++ {
		switch {
		case i < endCommonPrefix:
			// then generate common simple rules
			c := excludePatterns[0][i]
			switch c {
			case '*':
				// skip this element, this is the wildcard
				continue
			case '/':
				// check if this is the first character or if the previous
				// element was a "*" in which case we skip this one too
				if i == 0 {
					// this is for the case where the only common character is
					// actually the first "/" character
					lastCommonPrefix = string(c)
					continue
				}
				if excludePatterns[0][i-1] == '*' {
					// this is for the case where the last common characters are
					// "*/" if there are other common characters after this, it
					// will get overwritten on the next iteration
					lastCommonPrefix = lastCommonPrefix + "*/"
					continue
				}
			}
			// snippet up to this character so far - again since this is the
			// common prefix we can just use the first pattern
			snippet := string(excludePatterns[0][:i])
			res := fmt.Sprintf("%s%s[^%c]**%s\n", opts.Prefix, snippet, c, opts.Suffix)

			builder.WriteString(res)

			// save this as the most recent common sub string pattern
			lastCommonPrefix = snippet + string(c)
		case i == endCommonPrefix:
			// this is the last bit of the common substring, so get all the
			// characters for each pattern and just make a negative match for
			// all of those characters

			// note we use a map because there could be duplicate characters,
			// for example we could have 3 strings like "abcd1", "abcd2", "abceee",
			// where after the first common characters among the 3, there is
			// another common character with first 2 exclude patterns not in the
			// third
			chars := map[rune]bool{}
			for _, pattern := range excludePatterns {
				if i >= len(pattern) {
					continue
				}
				chars[pattern[i]] = true
			}
			if len(chars) == 0 {
				// shouldn't happen in practice, we check for duplicates above
				// but just be on the safe side
				return "", fmt.Errorf("internal erorr: all excluded patterns are the same")
			}

			negGroup := []rune{}
			for c := range chars {
				negGroup = append(negGroup, c)
			}
			// make sure the runs are sorted for consistency
			sort.Slice(negGroup, func(i, j int) bool {
				return negGroup[i] < negGroup[j]
			})
			negGroup = append([]rune("[^"), append(negGroup, ']')...)

			res := fmt.Sprintf("%s%s%s**%s\n", opts.Prefix, lastCommonPrefix, string(negGroup), opts.Suffix)
			builder.WriteString(res)

		case i > endCommonPrefix:
			// we break and use a separate loop because here we now need to
			// handle indexes per pattern because "*" could be in different
			// locations in each of the exclude patterns and we can no longer
			// use one index for all patterns
			break charLoop
		}
	}

	// each pattern starts at i
	indexPerPattern := make([]int, len(excludePatterns))
	for i2 := range excludePatterns {
		indexPerPattern[i2] = i
	}

	// this loop iterates up until the longestStrLen is reached or until we
	// reach only one pattern left which then collapses into a simpler special
	// case
	for j := i; j < longestStrLen; j++ {
		// check what patterns still have characters left
		stillPresentPatterns := map[int][]rune{}
		for patternIndex, excludePattern := range excludePatterns {
			charIndex := indexPerPattern[patternIndex]
			if charIndex >= len(excludePattern) {
				continue
			}
			stillPresentPatterns[patternIndex] = excludePattern
		}

		// maybe we only have one pattern left which means we can take a
		// shortcut
		if len(stillPresentPatterns) == 1 {
			// there is only one group left from here and we can do the simple
			// generation with this pattern for the rest of the rules

			// we know it's of length one so this is a short cut to get the vars
			var patternIndex int
			var excludePattern []rune
			for patternIndex, excludePattern = range stillPresentPatterns {
				break
			}

			// TODO: when AAREExclusionPatternsOptions has an option for
			// supporting non-absolute filepaths, use that option here to
			// generate the rules with generateAAREExclusionPatternsSingle to
			// reuse that loop by effectively dropping the part of
			// excludePattern up to "k" we calculate below, and adding that to
			// the prefix passed to generateAAREExclusionPatternsSingle and then
			// this is the same exact case

			for k := indexPerPattern[patternIndex]; k < len(excludePattern); k++ {
				c := excludePattern[k]
				switch c {
				case '*':
					// skip this element, this is the wildcard
					continue
				case '/':
					// check if the previous element was a "*" in which case we
					// skip this one too
					if excludePattern[k-1] == '*' {
						continue
					}
				}
				res := fmt.Sprintf("%s%s[^%c]**%s\n", opts.Prefix, string(excludePattern[:k]), c, opts.Suffix)
				builder.WriteString(res)
			}

			// TODO: maybe a return here would be more readable?
			// all rules have been generated we are done
			break
		}

		// otherwise there are still multiple patterns left so collect the
		// previous characters up to this alternation and this specific
		// character for each of the exclude patterns
		negCharsByAllowChars := map[string]*strutil.OrderedSet{}
		// meh if Go had deterministic loop iteration we wouldn't need this var
		allPrevs := strutil.OrderedSet{}
		for patternIndex := range excludePatterns {
			excludePattern, ok := stillPresentPatterns[patternIndex]
			if !ok {
				// not present
				continue
			}

			charIndex := indexPerPattern[patternIndex]
			c := excludePattern[charIndex]
			// increment this character, regardless of whether it is a wildcard
			// or not we consumed it this iteration
			indexPerPattern[patternIndex]++

			switch c {
			case '*':
				// skip this element, this is the wildcard
				continue
			case '/':
				// check if the previous element was a "*" in which case we
				// skip this one too
				if excludePattern[charIndex-1] == '*' {
					continue
				}
			}

			// trim the common prefix from the exclude pattern since it is
			// common and will be outside the alternation leaving us just with
			// the unique previous characters for this exclude pattern
			prev := strings.TrimPrefix(string(excludePattern[:charIndex]), lastCommonPrefix)

			// append into a list of runes to be excluded since there could be
			// multiple individual characters which are excluded which share the
			// same previous character, this shows up for example with "/a/bc"
			// and "/a/bd" - AppArmor does not like this pattern:
			// /a/{b[^c],b[^d]}
			// but finds this one acceptable:
			// /a/b[^cd]
			if negCharsByAllowChars[prev] == nil {
				negCharsByAllowChars[prev] = &strutil.OrderedSet{}
			}
			negCharsByAllowChars[prev].Put(string(c))
			allPrevs.Put(prev)
		}

		// now actually generate the sub rule either with a {<FOO>,<BAR>}
		// or just with [FOO] or nothing if the previous characters were "*/"
		var subPattern string
		switch len(negCharsByAllowChars) {
		case 0:
			// no characters at all, meaning that the "*" likely just happened
			// at the same spot for multiple patterns
			continue
		case 1:
			// only one pattern meaning we don't use "{}"
			var prevAllowChar string
			var negCharsSet *strutil.OrderedSet
			for prevAllowChar, negCharsSet = range negCharsByAllowChars {
				break
			}
			allNegChars := ""
			for _, negChar := range negCharsSet.Items() {
				allNegChars += string(negChar)
			}
			subPattern = fmt.Sprintf("%s[^%s]", prevAllowChar, allNegChars)
		default:
			// join the various alternations together with {}
			alternations := []string{}
			// sort for a consistent order
			allPrevsSlice := allPrevs.Items()
			sort.Strings(allPrevsSlice)
			for _, prev := range allPrevsSlice {
				negCharsSet := negCharsByAllowChars[prev]
				allNegChars := ""
				for _, negChar := range negCharsSet.Items() {
					allNegChars += string(negChar)
				}
				alternations = append(alternations, fmt.Sprintf("%s[^%s]", prev, allNegChars))
			}
			subPattern = fmt.Sprintf("{%s}", strings.Join(alternations, ","))
		}

		res := fmt.Sprintf("%s%s%s**%s\n", opts.Prefix, lastCommonPrefix, subPattern, opts.Suffix)
		builder.WriteString(res)
	}

	return builder.String(), nil
}

// LevelType encodes the kind of support for apparmor
// found on this system.
type LevelType int

const (
	// Unknown indicates that apparmor was not probed yet.
	Unknown LevelType = iota
	// Unsupported indicates that apparmor is not enabled.
	Unsupported
	// Unusable indicates that apparmor is enabled but cannot be used.
	Unusable
	// Partial indicates that apparmor is enabled but some
	// features are missing.
	Partial
	// Full indicates that all features are supported.
	Full
)

func setupConfCacheDirs(newrootdir string) {
	ConfDir = filepath.Join(newrootdir, "/etc/apparmor.d")
	CacheDir = filepath.Join(newrootdir, "/var/cache/apparmor")

	SystemCacheDir = filepath.Join(ConfDir, "cache")
	exists, isDir, _ := osutil.DirExists(SystemCacheDir)
	if !exists || !isDir {
		// some systems use a single cache dir instead of splitting
		// out the system cache
		// TODO: it seems Solus has a different setup too, investigate this
		SystemCacheDir = CacheDir
	}

	snapConfineDir := "snap-confine"
	if _, internal, err := AppArmorParser(); err == nil {
		if internal {
			snapConfineDir = "snap-confine.internal"
		}
	}
	SnapConfineAppArmorDir = filepath.Join(dirs.SnapdStateDir(newrootdir), "apparmor", snapConfineDir)
}

func init() {
	dirs.AddRootDirCallback(setupConfCacheDirs)
	setupConfCacheDirs(dirs.GlobalRootDir)
}

var (
	ConfDir                string
	CacheDir               string
	SystemCacheDir         string
	SnapConfineAppArmorDir string
)

func (level LevelType) String() string {
	switch level {
	case Unknown:
		return "unknown"
	case Unsupported:
		return "none"
	case Unusable:
		return "unusable"
	case Partial:
		return "partial"
	case Full:
		return "full"
	}
	return fmt.Sprintf("AppArmorLevelType:%d", level)
}

// appArmorAssessment represents what is supported AppArmor-wise by the system.
var appArmorAssessment = &appArmorAssess{appArmorProber: &appArmorProbe{}}

// ProbedLevel quantifies how well apparmor is supported on the current
// kernel. The computation is costly to perform. The result is cached internally.
func ProbedLevel() LevelType {
	appArmorAssessment.assess()
	return appArmorAssessment.level
}

// Summary describes how well apparmor is supported on the current
// kernel. The computation is costly to perform. The result is cached
// internally.
func Summary() string {
	appArmorAssessment.assess()
	return appArmorAssessment.summary
}

// KernelFeatures returns a sorted list of apparmor features like
// []string{"dbus", "network"}. The result is cached internally.
func KernelFeatures() ([]string, error) {
	return appArmorAssessment.KernelFeatures()
}

// ParserFeatures returns a sorted list of apparmor parser features
// like []string{"unsafe", ...}. The computation is costly to perform. The
// result is cached internally.
func ParserFeatures() ([]string, error) {
	return appArmorAssessment.ParserFeatures()
}

// ParserMtime returns the mtime of the AppArmor parser, else 0.
func ParserMtime() int64 {
	var mtime int64
	mtime = 0

	if cmd, _, err := AppArmorParser(); err == nil {
		if fi, err := os.Stat(cmd.Path); err == nil {
			mtime = fi.ModTime().Unix()
		}
	}
	return mtime
}

// probe related code

var (
	// requiredParserFeatures denotes the features that must be present in the parser.
	// Absence of any of those features results in the effective level be at most UnusableAppArmor.
	requiredParserFeatures = []string{
		"unsafe",
	}
	// preferredParserFeatures denotes the features that should be present in the parser.
	// Absence of any of those features results in the effective level be at most PartialAppArmor.
	preferredParserFeatures = []string{
		"unsafe",
	}
	// requiredKernelFeatures denotes the features that must be present in the kernel.
	// Absence of any of those features results in the effective level be at most UnusableAppArmor.
	requiredKernelFeatures = []string{
		// For now, require at least file and simply prefer the rest.
		"file",
	}
	// preferredKernelFeatures denotes the features that should be present in the kernel.
	// Absence of any of those features results in the effective level be at most PartialAppArmor.
	preferredKernelFeatures = []string{
		"caps",
		"dbus",
		"domain",
		"file",
		"mount",
		"namespaces",
		"network",
		"ptrace",
		"signal",
	}
	// Since AppArmorParserMtime() will be called by generateKey() in
	// system-key and that could be called by different users on the
	// system, use a predictable search path for finding the parser.
	parserSearchPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

	// Filesystem root defined locally to avoid dependency on the
	// 'dirs' package
	rootPath = "/"
)

// Each apparmor feature is manifested as a directory entry.
const featuresSysPath = "sys/kernel/security/apparmor/features"

type appArmorProber interface {
	KernelFeatures() ([]string, error)
	ParserFeatures() ([]string, error)
}

type appArmorAssess struct {
	appArmorProber
	// level contains the assessment of the "level" of apparmor support.
	level LevelType
	// summary contains a human readable description of the assessment.
	summary string

	once sync.Once
}

func (aaa *appArmorAssess) assess() {
	aaa.once.Do(func() {
		aaa.level, aaa.summary = aaa.doAssess()
	})
}

func (aaa *appArmorAssess) doAssess() (level LevelType, summary string) {
	// First, quickly check if apparmor is available in the kernel at all.
	kernelFeatures, err := aaa.KernelFeatures()
	if os.IsNotExist(err) {
		return Unsupported, "apparmor not enabled"
	}
	// Then check that the parser supports the required parser features.
	// If we have any missing required features then apparmor is unusable.
	parserFeatures, err := aaa.ParserFeatures()
	if os.IsNotExist(err) {
		return Unsupported, "apparmor_parser not found"
	}
	var missingParserFeatures []string
	for _, feature := range requiredParserFeatures {
		if !strutil.SortedListContains(parserFeatures, feature) {
			missingParserFeatures = append(missingParserFeatures, feature)
		}
	}
	if len(missingParserFeatures) > 0 {
		summary := fmt.Sprintf("apparmor_parser is available but required parser features are missing: %s",
			strings.Join(missingParserFeatures, ", "))
		return Unusable, summary
	}

	// Next, check that the kernel supports the required kernel features.
	var missingKernelFeatures []string
	for _, feature := range requiredKernelFeatures {
		if !strutil.SortedListContains(kernelFeatures, feature) {
			missingKernelFeatures = append(missingKernelFeatures, feature)
		}
	}
	if len(missingKernelFeatures) > 0 {
		summary := fmt.Sprintf("apparmor is enabled but required kernel features are missing: %s",
			strings.Join(missingKernelFeatures, ", "))
		return Unusable, summary
	}

	// Next check that the parser supports preferred parser features.
	// If we have any missing preferred features then apparmor is partially enabled.
	for _, feature := range preferredParserFeatures {
		if !strutil.SortedListContains(parserFeatures, feature) {
			missingParserFeatures = append(missingParserFeatures, feature)
		}
	}
	if len(missingParserFeatures) > 0 {
		summary := fmt.Sprintf("apparmor_parser is available but some features are missing: %s",
			strings.Join(missingParserFeatures, ", "))
		return Partial, summary
	}

	// Lastly check that the kernel supports preferred kernel features.
	for _, feature := range preferredKernelFeatures {
		if !strutil.SortedListContains(kernelFeatures, feature) {
			missingKernelFeatures = append(missingKernelFeatures, feature)
		}
	}
	if len(missingKernelFeatures) > 0 {
		summary := fmt.Sprintf("apparmor is enabled but some kernel features are missing: %s",
			strings.Join(missingKernelFeatures, ", "))
		return Partial, summary
	}

	// If we got here then all features are available and supported.
	note := ""
	if strutil.SortedListContains(parserFeatures, "snapd-internal") {
		note = " (using snapd provided apparmor_parser)"
	}
	return Full, "apparmor is enabled and all features are available" + note
}

type appArmorProbe struct {
	// kernelFeatures contains a list of kernel features that are supported.
	kernelFeatures []string
	// kernelError contains an error, if any, encountered when
	// discovering available kernel features.
	kernelError error
	// parserFeatures contains a list of parser features that are supported.
	parserFeatures []string
	// parserError contains an error, if any, encountered when
	// discovering available parser features.
	parserError error

	probeKernelOnce sync.Once
	probeParserOnce sync.Once
}

func (aap *appArmorProbe) KernelFeatures() ([]string, error) {
	aap.probeKernelOnce.Do(func() {
		aap.kernelFeatures, aap.kernelError = probeKernelFeatures()
	})
	return aap.kernelFeatures, aap.kernelError
}

func (aap *appArmorProbe) ParserFeatures() ([]string, error) {
	aap.probeParserOnce.Do(func() {
		aap.parserFeatures, aap.parserError = probeParserFeatures()
	})
	return aap.parserFeatures, aap.parserError
}

func probeKernelFeatures() ([]string, error) {
	// note that ioutil.ReadDir() is already sorted
	dentries, err := ioutil.ReadDir(filepath.Join(rootPath, featuresSysPath))
	if err != nil {
		return []string{}, err
	}
	features := make([]string, 0, len(dentries))
	for _, fi := range dentries {
		if fi.IsDir() {
			features = append(features, fi.Name())
			// also read any sub-features
			subdenties, err := ioutil.ReadDir(filepath.Join(rootPath, featuresSysPath, fi.Name()))
			if err != nil {
				return []string{}, err
			}
			for _, subfi := range subdenties {
				if subfi.IsDir() {
					features = append(features, fi.Name()+":"+subfi.Name())
				}
			}
		}
	}
	return features, nil
}

func probeParserFeatures() ([]string, error) {
	var featureProbes = []struct {
		feature string
		flags   []string
		probe   string
	}{
		{
			feature: "unsafe",
			probe:   "change_profile unsafe /**,",
		},
		{
			feature: "include-if-exists",
			probe:   `#include if exists "/foo"`,
		},
		{
			feature: "qipcrtr-socket",
			probe:   "network qipcrtr dgram,",
		},
		{
			feature: "mqueue",
			probe:   "mqueue,",
		},
		{
			feature: "cap-bpf",
			probe:   "capability bpf,",
		},
		{
			feature: "cap-audit-read",
			probe:   "capability audit_read,",
		},
		{
			feature: "xdp",
			probe:   "network xdp,",
		},
		{
			feature: "userns",
			probe:   "userns,",
		},
		{
			feature: "unconfined",
			flags:   []string{"unconfined"},
			probe:   "# test unconfined",
		},
	}
	_, internal, err := AppArmorParser()
	if err != nil {
		return []string{}, err
	}
	features := make([]string, 0, len(featureProbes)+1)
	for _, fp := range featureProbes {
		// recreate the Cmd each time so we can exec it each time
		cmd, _, _ := AppArmorParser()
		if tryAppArmorParserFeature(cmd, fp.flags, fp.probe) {
			features = append(features, fp.feature)
		}
	}
	if internal {
		features = append(features, "snapd-internal")
	}
	sort.Strings(features)
	return features, nil
}

func systemAppArmorLoadsSnapPolicy() bool {
	// on older Ubuntu systems the system installed apparmor may try and
	// load snapd generated apparmor policy (LP: #2024637)
	f, err := os.Open(filepath.Join(dirs.GlobalRootDir, "/lib/apparmor/functions"))
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Debugf("cannot open apparmor functions file: %v", err)
		}
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, dirs.SnapAppArmorDir) {
			return true
		}
	}
	if scanner.Err() != nil {
		logger.Debugf("cannot scan apparmor functions file: %v", scanner.Err())
	}

	return false
}

func snapdAppArmorSupportsReexecImpl() bool {
	hostInfoDir := filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir)
	_, flags, err := snapdtool.SnapdVersionFromInfoFile(hostInfoDir)
	return err == nil && flags["SNAPD_APPARMOR_REEXEC"] == "1"
}

var snapdAppArmorSupportsReexec = snapdAppArmorSupportsReexecImpl

// AppArmorParser returns an exec.Cmd for the apparmor_parser binary, and a
// boolean to indicate whether this is internal to snapd (ie is provided by
// snapd)
func AppArmorParser() (cmd *exec.Cmd, internal bool, err error) {
	// first see if we have our own internal copy which could come from the
	// snapd snap (likely) or be part of the snapd distro package (unlikely)
	// - but only use the internal one when we know that the system
	// installed snapd-apparmor support re-exec
	if path, err := snapdtool.InternalToolPath("apparmor_parser"); err == nil {
		if osutil.IsExecutable(path) && snapdAppArmorSupportsReexec() && !systemAppArmorLoadsSnapPolicy() {
			prefix := strings.TrimSuffix(path, "apparmor_parser")
			// when using the internal apparmor_parser also use
			// its own configuration and includes etc plus
			// also ensure we use the 3.0 feature ABI to get
			// the widest array of policy features across the
			// widest array of kernel versions
			args := []string{
				"--config-file", filepath.Join(prefix, "/apparmor/parser.conf"),
				"--base", filepath.Join(prefix, "/apparmor.d"),
				"--policy-features", filepath.Join(prefix, "/apparmor.d/abi/3.0"),
			}
			return exec.Command(path, args...), true, nil
		}
	}

	// now search for one in the configured parserSearchPath
	for _, dir := range filepath.SplitList(parserSearchPath) {
		path := filepath.Join(dir, "apparmor_parser")
		if _, err := os.Stat(path); err == nil {
			return exec.Command(path), false, nil
		}
	}

	return nil, false, os.ErrNotExist
}

// tryAppArmorParserFeature attempts to pre-process a bit of apparmor syntax with a given parser.
func tryAppArmorParserFeature(cmd *exec.Cmd, flags []string, rule string) bool {
	cmd.Args = append(cmd.Args, "--preprocess")
	flagSnippet := ""
	if len(flags) > 0 {
		flagSnippet = fmt.Sprintf("flags=(%s) ", strings.Join(flags, ","))
	}
	cmd.Stdin = bytes.NewBufferString(fmt.Sprintf("profile snap-test %s{\n %s\n}",
		flagSnippet, rule))
	output, err := cmd.CombinedOutput()
	// older versions of apparmor_parser can exit with success even
	// though they fail to parse
	if err != nil || strings.Contains(string(output), "parser error") {
		return false
	}
	return true
}

// UpdateHomedirsTunable sets the AppArmor HOMEDIRS tunable to the list of the
// specified directories. This directly affects the value of the AppArmor
// @{HOME} variable. See the "/etc/apparmor.d/tunables/home" file for more
// information.
func UpdateHomedirsTunable(homedirs []string) error {
	homeTunableDir := filepath.Join(ConfDir, "tunables/home.d")
	tunableFilePath := filepath.Join(homeTunableDir, "snapd")

	// If the file is not there and `homedirs` is empty, do nothing; this is
	// not just an optimisation, but a necessity in Ubuntu Core: the
	// /etc/apparmor.d/ tree is read-only, and attempting to create the file
	// would generate an error.
	if len(homedirs) == 0 && !osutil.FileExists(tunableFilePath) {
		return nil
	}

	if err := osMkdirAll(homeTunableDir, 0755); err != nil {
		return fmt.Errorf("cannot create AppArmor tunable directory: %v", err)
	}

	contents := &bytes.Buffer{}
	fmt.Fprintln(contents, "# Generated by snapd -- DO NOT EDIT!")
	if len(homedirs) > 0 {
		contents.Write([]byte("@{HOMEDIRS}+="))
		separator := ""
		for _, dir := range homedirs {
			fmt.Fprintf(contents, `%s"%s"`, separator, dir)
			separator = " "
		}
		contents.Write([]byte("\n"))
	}
	return osutilAtomicWrite(tunableFilePath, contents, 0644, 0)
}

// mocking

type mockAppArmorProbe struct {
	kernelFeatures []string
	kernelError    error
	parserFeatures []string
	parserError    error
}

func (m *mockAppArmorProbe) KernelFeatures() ([]string, error) {
	return m.kernelFeatures, m.kernelError
}

func (m *mockAppArmorProbe) ParserFeatures() ([]string, error) {
	return m.parserFeatures, m.parserError
}

// MockAppArmorLevel makes the system believe it has certain level of apparmor
// support.
//
// AppArmor kernel and parser features are set to unrealistic values that do
// not match the requested level. Use this function to observe behavior that
// relies solely on the apparmor level value.
func MockLevel(level LevelType) (restore func()) {
	oldAppArmorAssessment := appArmorAssessment
	mockProbe := &mockAppArmorProbe{
		kernelFeatures: []string{"mocked-kernel-feature"},
		parserFeatures: []string{"mocked-parser-feature"},
	}
	appArmorAssessment = &appArmorAssess{
		appArmorProber: mockProbe,
		level:          level,
		summary:        fmt.Sprintf("mocked apparmor level: %s", level),
	}
	appArmorAssessment.once.Do(func() {})
	return func() {
		appArmorAssessment = oldAppArmorAssessment
	}
}

// MockAppArmorFeatures makes the system believe it has certain kernel and
// parser features.
//
// AppArmor level and summary are automatically re-assessed as needed
// on both the change and the restore process. Use this function to
// observe real assessment of arbitrary features.
func MockFeatures(kernelFeatures []string, kernelError error, parserFeatures []string, parserError error) (restore func()) {
	oldAppArmorAssessment := appArmorAssessment
	mockProbe := &mockAppArmorProbe{
		kernelFeatures: kernelFeatures,
		kernelError:    kernelError,
		parserFeatures: parserFeatures,
		parserError:    parserError,
	}
	appArmorAssessment = &appArmorAssess{
		appArmorProber: mockProbe,
	}
	appArmorAssessment.assess()
	return func() {
		appArmorAssessment = oldAppArmorAssessment
	}

}

func MockParserSearchPath(new string) (restore func()) {
	oldAppArmorParserSearchPath := parserSearchPath
	parserSearchPath = new
	return func() {
		parserSearchPath = oldAppArmorParserSearchPath
	}
}
